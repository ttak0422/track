//! Internal-only recovery foundation. No filesystem, engine writes, timers or UI.
//! A driver must execute `next_operation` serially and report completion before
//! requesting another operation. Pending bytes are not crash-durable.
use serde::{Deserialize, Serialize};

pub const BODY_LIMIT: usize = 2 * 1024 * 1024;
pub const ARTIFACT_LIMIT: usize = 3 * 1024 * 1024;
pub const MAX_METADATA_BYTES: usize = 4096;
const INVALID: &str = "Invalid fixture draft recovery record.";

// Intentionally not Debug: diagnostics must never print draft bodies.
#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Record {
    pub schema: u32,
    pub binding: String,
    pub scope: String,
    pub vault: String,
    pub note_id: u64,
    pub kind: String,
    pub base_etag: String,
    pub body: String,
    pub revision: u64,
    pub title: Option<String>,
}
impl Record {
    pub fn validate(&self, binding: &str, registry: &[String]) -> Result<(), String> {
        let metadata = [&self.binding, &self.vault, &self.kind, &self.base_etag];
        if self.schema != 1
            || self.binding != binding
            || binding.is_empty()
            || self.note_id == 0
            || self.note_id > i64::MAX as u64
            || self.revision == 0
            || self.kind != "note"
            || metadata
                .iter()
                .any(|s| s.is_empty() || s.len() > MAX_METADATA_BYTES || s.contains('\0'))
            || self
                .title
                .as_ref()
                .is_some_and(|s| s.len() > MAX_METADATA_BYTES || s.contains('\0'))
            || self.scope.contains('\0')
            || self.scope.len() > MAX_METADATA_BYTES
            || self.body.len() > BODY_LIMIT
            || !registry.contains(&self.scope)
        {
            return Err(INVALID.into());
        }
        // Escaping can make JSON substantially larger than its UTF-8 body.
        if serde_json::to_vec(self).map_err(|_| INVALID)?.len() > ARTIFACT_LIMIT {
            return Err(INVALID.into());
        }
        Ok(())
    }
    pub fn decode(bytes: &[u8], binding: &str, registry: &[String]) -> Result<Self, String> {
        if bytes.len() > ARTIFACT_LIMIT {
            return Err(INVALID.into());
        }
        let record: Self = serde_json::from_slice(bytes).map_err(|_| INVALID)?;
        record.validate(binding, registry)?;
        Ok(record)
    }
    pub fn encode(&self, binding: &str, registry: &[String]) -> Result<Vec<u8>, String> {
        self.validate(binding, registry)?;
        serde_json::to_vec(self).map_err(|_| INVALID.into())
    }
    fn same_source(&self, other: &Self) -> bool {
        self.binding == other.binding
            && self.scope == other.scope
            && self.vault == other.vault
            && self.note_id == other.note_id
            && self.kind == other.kind
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum LocalStatus {
    Empty,
    Available,
    Pending,
    SavedLocally,
    Failed,
}

/// Fresh accepted note identity, plus every epoch invalidating a recovery action.
#[derive(Clone, PartialEq, Eq)]
pub struct RecoveryContext {
    pub binding: String,
    pub scope: String,
    pub vault: String,
    pub note_id: u64,
    pub kind: String,
    pub etag: String,
    pub request: u64,
    pub launch: u64,
    pub storage: u64,
    pub editing_ready: bool,
    pub storage_ready: bool,
    pub draft_busy: bool,
}
#[derive(Clone)]
pub struct RecoveryToken {
    generation: u64,
    context: RecoveryContext,
}

#[derive(Clone)]
pub enum OperationKind {
    Write(Record),
    Clear,
}
#[derive(Clone)]
pub struct Operation {
    pub token: u64,
    pub kind: OperationKind,
}

pub struct RecoveryModel {
    binding: String,
    registry: Vec<String>,
    latest: Option<Record>,
    unresolved: bool,
    generation: u64,
    high_water: u64,
    uncertain: Option<Operation>,
    next_token: u64,
    pending: Option<OperationKind>,
    in_flight: Option<Operation>,
    durable: Option<Record>,
    failed: bool,
}
impl RecoveryModel {
    /// Startup only retains validated bytes. It does not fetch or install a note.
    pub fn new(
        binding: String,
        registry: Vec<String>,
        prior: Option<Record>,
    ) -> Result<Self, String> {
        if binding.is_empty() || binding.len() > MAX_METADATA_BYTES {
            return Err(INVALID.into());
        }
        if let Some(record) = &prior {
            record.validate(&binding, &registry)?;
        }
        Ok(Self {
            binding,
            registry,
            unresolved: prior.is_some(),
            latest: prior.clone(),
            high_water: prior.as_ref().map_or(0, |r| r.revision),
            uncertain: None,
            durable: prior,
            generation: 0,
            next_token: 0,
            pending: None,
            in_flight: None,
            failed: false,
        })
    }
    pub fn latest(&self) -> Option<&Record> {
        self.latest.as_ref()
    }
    pub fn status(&self) -> LocalStatus {
        if self.failed {
            return LocalStatus::Failed;
        }
        if self.unresolved {
            return LocalStatus::Available;
        }
        if self.pending.is_some() || self.in_flight.is_some() {
            return LocalStatus::Pending;
        }
        match (&self.latest, &self.durable) {
            (None, None) => LocalStatus::Empty,
            (Some(latest), Some(durable)) if latest == durable => LocalStatus::SavedLocally,
            _ => LocalStatus::Pending,
        }
    }
    pub fn checkpoint(&mut self, record: Record) -> Result<(), String> {
        record.validate(&self.binding, &self.registry)?;
        if record.revision <= self.high_water {
            return Err("Fixture draft revision is stale.".into());
        }
        if self.unresolved {
            return Err("Resolve the retained fixture draft first.".into());
        }
        if let Some(latest) = &self.latest {
            if !record.same_source(latest)
                || record.base_etag != latest.base_etag
                || record.revision <= latest.revision
            {
                return Err("Fixture draft source or revision changed.".into());
            }
        } else if self.pending.is_some() || self.in_flight.is_some() || self.uncertain.is_some() {
            return Err("Wait for fixture draft removal.".into());
        }
        self.generation = self.generation.checked_add(1).ok_or(INVALID)?;
        self.high_water = record.revision;
        self.latest = Some(record.clone());
        self.pending = Some(OperationKind::Write(record));
        Ok(())
    }
    pub fn offer_recovery(&self, context: &RecoveryContext) -> Option<RecoveryToken> {
        let r = self.latest.as_ref()?;
        if self.failed
            || !self.unresolved
            || !context.editing_ready
            || !context.storage_ready
            || context.draft_busy
            || context.binding != r.binding
            || context.scope != r.scope
            || context.vault != r.vault
            || context.note_id != r.note_id
            || context.kind != r.kind
            || context.etag != r.base_etag
        {
            return None;
        }
        Some(RecoveryToken {
            generation: self.generation,
            context: context.clone(),
        })
    }
    /// Returned text must be installed DIRTY by a future editor adapter, never saved.
    pub fn recover(&mut self, token: &RecoveryToken, fresh: &RecoveryContext) -> Option<Record> {
        if token.generation != self.generation
            || &token.context != fresh
            || self.offer_recovery(fresh).is_none()
        {
            return None;
        }
        self.generation = self.generation.checked_add(1)?;
        self.unresolved = false;
        self.latest.clone()
    }
    /// Only an exact authoritative engine acknowledgement may request this clear.
    /// Unknown/failed saves and conflicts must not call this method.
    pub fn acknowledge_saved(&mut self, acknowledged: &Record) -> bool {
        if self.unresolved || self.latest.as_ref() != Some(acknowledged) {
            return false;
        }
        self.request_clear()
    }
    /// Caller must obtain explicit confirmation for this exact retained record.
    pub fn discard(&mut self, confirmed: &Record) -> bool {
        if self.latest.as_ref() != Some(confirmed) {
            return false;
        }
        self.request_clear()
    }
    fn request_clear(&mut self) -> bool {
        let Some(generation) = self.generation.checked_add(1) else {
            return false;
        };
        self.generation = generation;
        self.latest = None;
        self.unresolved = false;
        // Replace queued writes, but allow the sole active write to finish first.
        self.pending = Some(OperationKind::Clear);
        true
    }
    pub fn next_operation(&mut self) -> Option<Operation> {
        if self.failed || self.in_flight.is_some() {
            return None;
        }
        let token = self.next_token.checked_add(1)?;
        let kind = self.pending.take()?;
        self.next_token = token;
        let op = Operation {
            token: self.next_token,
            kind,
        };
        self.in_flight = Some(op.clone());
        Some(op)
    }
    /// Stale/duplicate/out-of-order completions cannot change status or queue.
    /// Errors are boolean to prevent accidental plaintext diagnostics.
    pub fn complete(&mut self, token: u64, success: bool) -> bool {
        if self.in_flight.as_ref().map(|op| op.token) != Some(token) {
            return false;
        }
        let op = self.in_flight.take().unwrap();
        if success {
            self.durable = match op.kind {
                OperationKind::Write(r) => Some(r),
                OperationKind::Clear => None,
            };
            if self.pending.is_none() {
                self.failed = false;
            }
        } else {
            self.failed = true;
            self.uncertain = Some(op);
        }
        true
    }
    /// Explicit driver reconciliation only: `persisted` must be established by
    /// protected storage and durability verification, not merely a readable file.
    /// No failure, including a failed older write, permits automatic retry.
    pub fn reconcile_failure(&mut self, token: u64, persisted: bool) -> bool {
        if self.uncertain.as_ref().map(|op| op.token) != Some(token) {
            return false;
        }
        let op = self.uncertain.take().unwrap();
        if persisted {
            self.durable = match op.kind {
                OperationKind::Write(r) => Some(r),
                OperationKind::Clear => None,
            };
        } else if self.pending.is_none() {
            self.pending = Some(op.kind);
        }
        self.failed = false;
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn registry() -> Vec<String> {
        vec!["alias".into(), "".into()]
    }
    fn record(revision: u64) -> Record {
        Record {
            schema: 1,
            binding: "fixture".into(),
            scope: "alias".into(),
            vault: "vault".into(),
            note_id: 1,
            kind: "note".into(),
            base_etag: "etag".into(),
            body: "日本語 e\u{301}\n".into(),
            revision,
            title: None,
        }
    }
    fn model() -> RecoveryModel {
        RecoveryModel::new("fixture".into(), registry(), None).unwrap()
    }
    fn context() -> RecoveryContext {
        RecoveryContext {
            binding: "fixture".into(),
            scope: "alias".into(),
            vault: "vault".into(),
            note_id: 1,
            kind: "note".into(),
            etag: "etag".into(),
            request: 1,
            launch: 1,
            storage: 1,
            editing_ready: true,
            storage_ready: true,
            draft_busy: false,
        }
    }
    #[test]
    fn bounded_exact_bytes_and_schema() {
        for body in [
            String::new(),
            "日本語 e\u{301}\n\n".into(),
            "x".repeat(BODY_LIMIT),
        ] {
            let mut r = record(1);
            r.body = body;
            let bytes = r.encode("fixture", &registry()).unwrap();
            assert!(Record::decode(&bytes, "fixture", &registry()).unwrap() == r);
        }
        let mut r = record(1);
        r.body = "x".repeat(BODY_LIMIT + 1);
        assert!(r.validate("fixture", &registry()).is_err());
        r.body = "\0".repeat(BODY_LIMIT);
        assert!(r.validate("fixture", &registry()).is_err());
        r.body.clear();
        r.schema = 2;
        assert!(r.validate("fixture", &registry()).is_err());
        for bytes in [b"{}".as_slice(), b"\xff", b"{\"schema\":1,\"schema\":1}"] {
            assert!(Record::decode(bytes, "fixture", &registry()).is_err());
        }
        assert!(Record::decode(&vec![b' '; ARTIFACT_LIMIT + 1], "fixture", &registry()).is_err());
    }
    #[test]
    fn metadata_identity_and_empty_default_scope() {
        let mut r = record(1);
        r.scope.clear();
        r.title = Some("x".repeat(MAX_METADATA_BYTES));
        assert!(r.validate("fixture", &registry()).is_ok());
        r.title.as_mut().unwrap().push('x');
        assert!(r.validate("fixture", &registry()).is_err());
        r.title = None;
        assert!(r.validate("foreign", &registry()).is_err());
        r.scope = "unregistered".into();
        assert!(r.validate("fixture", &registry()).is_err());
        r.scope = "alias".into();
        r.note_id = i64::MAX as u64 + 1;
        assert!(r.validate("fixture", &registry()).is_err());
        r.note_id = 1;
        r.title = Some("bad\0title".into());
        assert!(r.validate("fixture", &registry()).is_err());
        r.title = None;
        r.body = "\0".into();
        assert!(r.validate("fixture", &registry()).is_ok());
        r.kind = "sidecar".into();
        assert!(r.validate("fixture", &registry()).is_err());
    }
    #[test]
    fn old_success_never_marks_new_revision_durable() {
        let mut m = model();
        m.checkpoint(record(1)).unwrap();
        let first = m.next_operation().unwrap();
        m.checkpoint(record(2)).unwrap();
        m.complete(first.token, true);
        assert_eq!(m.status(), LocalStatus::Pending);
        let second = m.next_operation().unwrap();
        m.complete(second.token, false);
        assert_eq!(m.status(), LocalStatus::Failed);
        assert!(!m.complete(first.token, true));
        assert_eq!(m.status(), LocalStatus::Failed);
        assert!(m.next_operation().is_none());
        assert!(m.reconcile_failure(second.token, false));
        let retry = m.next_operation().unwrap();
        m.complete(retry.token, true);
        assert_eq!(m.status(), LocalStatus::SavedLocally);
    }
    #[test]
    fn single_writer_coalesces_and_rejects_stale_completions() {
        let mut m = model();
        m.checkpoint(record(1)).unwrap();
        let one = m.next_operation().unwrap();
        m.checkpoint(record(2)).unwrap();
        m.checkpoint(record(3)).unwrap();
        assert!(m.next_operation().is_none());
        assert!(!m.complete(one.token + 1, true));
        assert!(m.complete(one.token, false));
        assert_eq!(m.status(), LocalStatus::Failed);
        assert!(m.next_operation().is_none());
        assert!(m.reconcile_failure(one.token, false));
        let three = m.next_operation().unwrap();
        assert!(matches!(&three.kind, OperationKind::Write(r) if r.revision == 3));
        assert!(!m.complete(one.token, true));
        m.complete(three.token, true);
        assert_eq!(m.status(), LocalStatus::SavedLocally);
    }
    #[test]
    fn save_and_discard_cannot_resurrect_old_write() {
        let mut m = model();
        m.checkpoint(record(1)).unwrap();
        let one = m.next_operation().unwrap();
        m.checkpoint(record(2)).unwrap();
        assert!(!m.acknowledge_saved(&record(1)));
        assert!(m.discard(&record(2)));
        assert!(m.checkpoint(record(3)).is_err());
        m.complete(one.token, true);
        assert_eq!(m.status(), LocalStatus::Pending);
        let clear = m.next_operation().unwrap();
        assert!(matches!(clear.kind, OperationKind::Clear));
        m.complete(clear.token, true);
        assert!(!m.complete(one.token, true));
        assert_eq!(m.status(), LocalStatus::Empty);
        assert!(m.checkpoint(record(2)).is_err());
        assert!(m.checkpoint(record(3)).is_ok());
    }
    #[test]
    fn recovery_requires_explicit_fresh_source_and_epochs() {
        let mut m = RecoveryModel::new("fixture".into(), registry(), Some(record(1))).unwrap();
        assert_eq!(m.status(), LocalStatus::Available);
        assert!(m.next_operation().is_none());
        assert!(m.checkpoint(record(2)).is_err());
        let fresh = context();
        let token = m.offer_recovery(&fresh).unwrap();
        for changed in 0..8 {
            let mut c = fresh.clone();
            match changed {
                0 => c.request += 1,
                1 => c.launch += 1,
                2 => c.storage += 1,
                3 => c.scope = "other".into(),
                4 => c.vault = "other".into(),
                5 => c.etag = "new".into(),
                6 => c.binding = "other".into(),
                _ => c.draft_busy = true,
            }
            assert!(m.recover(&token, &c).is_none());
        }
        assert!(m.recover(&token, &fresh).unwrap() == record(1));
        assert!(m.recover(&token, &fresh).is_none());
        assert!(m.acknowledge_saved(&record(1)));
    }
    #[test]
    fn failure_retains_latest_and_clear_retries_before_new_draft() {
        let mut m = model();
        m.checkpoint(record(1)).unwrap();
        let op = m.next_operation().unwrap();
        m.complete(op.token, false);
        assert_eq!(m.status(), LocalStatus::Failed);
        assert!(m.latest().unwrap() == &record(1));
        m.checkpoint(record(2)).unwrap();
        assert!(m.next_operation().is_none());
        assert!(m.reconcile_failure(op.token, false));
        let op = m.next_operation().unwrap();
        m.complete(op.token, true);
        assert_eq!(m.status(), LocalStatus::SavedLocally);
        m.acknowledge_saved(&record(2));
        let op = m.next_operation().unwrap();
        m.complete(op.token, false);
        assert_eq!(m.status(), LocalStatus::Failed);
        assert!(m.checkpoint(record(3)).is_err());
        assert!(m.next_operation().is_none());
        assert!(m.reconcile_failure(op.token, false));
        let op = m.next_operation().unwrap();
        m.complete(op.token, true);
        assert_eq!(m.status(), LocalStatus::Empty);
        assert!(m.checkpoint(record(2)).is_err());
        assert!(m.checkpoint(record(3)).is_ok());
    }
}
