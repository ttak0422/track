//! In-memory existing-note drafts. Explicit saves only; no persistence or automatic retry.
use crate::live::{Error, NoteKey, NoteResponse, SaveAck, SaveDisposition, SaveRequest, Storage};
use serde_json::Value;
use std::sync::atomic::{AtomicU64, Ordering};
static NEXT_ATTEMPT: AtomicU64 = AtomicU64::new(1);

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum DraftState {
    Clean,
    Dirty,
    Saving,
    Conflict(String),
    NotSent(String),
    Rejected(String),
    Uncertain(String),
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum LeaveChoice {
    Save,
    Discard,
    Cancel,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum LeavePrompt {
    Leave,
    AskSaveDiscardCancel,
    WaitForSave,
    WaitForComposition,
}
#[derive(Clone, Debug)]
pub struct PreparedSave {
    pub attempt: u64,
    pub request: SaveRequest,
}
#[derive(Clone, Debug)]
pub enum LeaveAction {
    Stay,
    Leave,
    Save(PreparedSave),
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum SaveOutcome {
    Saved { leave: bool },
    Retained,
    StaleResponse,
}
/// GET hides terminal LF bytes. Equivalence is not proof of exact disk bytes.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ComparisonMatch {
    Exact,
    TerminalLfUnobservable,
    Different,
}
#[derive(Clone, Debug)]
pub struct Comparison {
    pub body: String,
    pub etag: String,
    verified: bool,
}

pub struct EditSession {
    key: NoteKey,
    launch_generation: u64,
    storage_generation: u64,
    baseline: String,
    etag: String,
    draft: String,
    state: DraftState,
    composing: bool,
    retired: bool,
    write_blocked: bool,
    in_flight: Option<PreparedSave>,
    pending_leave: bool,
    comparison: Option<Comparison>,
}
impl EditSession {
    pub fn from_note(
        launch_generation: u64,
        storage: &Storage,
        source: &NoteResponse,
    ) -> Result<Self, String> {
        verify_source(storage, source)?;
        let body = source.note.body.clone();
        Ok(Self {
            key: source.note.note.key(),
            launch_generation,
            storage_generation: storage.generation,
            baseline: body.clone(),
            draft: body,
            etag: source.note.etag.clone(),
            state: DraftState::Clean,
            composing: false,
            retired: false,
            write_blocked: false,
            in_flight: None,
            pending_leave: false,
            comparison: None,
        })
    }
    pub fn key(&self) -> &NoteKey {
        &self.key
    }
    pub fn launch_generation(&self) -> u64 {
        self.launch_generation
    }
    pub fn storage_generation(&self) -> u64 {
        self.storage_generation
    }
    pub fn comparison(&self) -> Option<&Comparison> {
        self.comparison.as_ref()
    }
    pub fn is_composing(&self) -> bool {
        self.composing
    }
    pub fn is_retired(&self) -> bool {
        self.retired
    }
    pub fn is_saving(&self) -> bool {
        self.in_flight.is_some()
    }
    pub fn draft(&self) -> &str {
        &self.draft
    }
    pub fn baseline(&self) -> &str {
        &self.baseline
    }
    pub fn etag(&self) -> &str {
        &self.etag
    }
    pub fn state(&self) -> &DraftState {
        &self.state
    }
    pub fn dirty(&self) -> bool {
        self.draft != self.baseline
    }
    /// Plain text callers have no active IME; UI integration should use
    /// replace_text_with_composition and signal unmark with set_composing(false).
    pub fn replace_text(&mut self, text: String) -> Result<(), String> {
        self.replace_text_with_composition(text, false)
    }
    pub fn set_composing(&mut self, composing: bool) -> Result<(), String> {
        if self.retired || self.in_flight.is_some() {
            return Err("This draft is not accepting edits.".into());
        }
        self.composing = composing;
        Ok(())
    }
    pub fn replace_text_with_composition(
        &mut self,
        text: String,
        composing: bool,
    ) -> Result<(), String> {
        if self.retired {
            return Err("This draft was explicitly discarded; load a fresh session.".into());
        }
        if self.in_flight.is_some() {
            return Err("Wait for the current save before editing.".into());
        }
        self.draft = text;
        self.composing = composing;
        if !matches!(
            self.state,
            DraftState::Uncertain(_) | DraftState::Conflict(_)
        ) {
            self.state = if self.dirty() {
                DraftState::Dirty
            } else {
                DraftState::Clean
            };
        }
        Ok(())
    }
    pub fn prepare_save(
        &mut self,
        launch: u64,
        storage: &Storage,
    ) -> Result<Option<PreparedSave>, String> {
        if self.retired {
            return Err("This draft was discarded; load a fresh session.".into());
        }
        if self.composing {
            return Err("Finish or cancel IME composition before saving.".into());
        }
        if self.in_flight.is_some() {
            return Err("A save is already pending.".into());
        }
        if matches!(
            self.state,
            DraftState::Uncertain(_) | DraftState::Conflict(_)
        ) {
            return Err("Inspect the current server content before deciding how to continue; no automatic retry is safe.".into());
        }
        if !self.dirty() {
            return Ok(None);
        }
        self.verify_epoch(launch, storage)?;
        verify_storage(storage)?;
        if self.write_blocked {
            return Err(
                "The latest source is not verified writable; reload before editing.".into(),
            );
        }
        let attempt = NEXT_ATTEMPT
            .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |n| n.checked_add(1))
            .map_err(|_| "Save token space exhausted.".to_string())?;
        let save = PreparedSave {
            attempt,
            request: SaveRequest {
                key: self.key.clone(),
                body: self.draft.clone(),
                etag: self.etag.clone(),
                launch_generation: self.launch_generation,
                storage_generation: self.storage_generation,
            },
        };
        self.in_flight = Some(save.clone());
        self.state = DraftState::Saving;
        Ok(Some(save))
    }
    /// An acknowledgment only applies to its exact attempt. Editing is locked while saving.
    pub fn finish_save(
        &mut self,
        attempt: u64,
        current_launch: u64,
        current_storage_generation: u64,
        result: Result<SaveAck, Error>,
    ) -> SaveOutcome {
        if self.in_flight.as_ref().is_none_or(|s| s.attempt != attempt) {
            return SaveOutcome::StaleResponse;
        }
        let save = self.in_flight.take().unwrap();
        if current_launch != self.launch_generation
            || current_storage_generation != self.storage_generation
        {
            self.pending_leave = false;
            self.state = match &result {
                Err(error) if error.save_disposition() == SaveDisposition::NotSent => DraftState::NotSent(error.to_string()),
                _ => DraftState::Uncertain("The server/storage generation changed while saving. The old request may have completed; keep this draft and inspect the original identity.".into()),
            };
            return SaveOutcome::Retained;
        }
        match result {
            Ok(ack)
                if ack.saved
                    && !ack.etag.is_empty()
                    && ack.note_id == self.key.note_id
                    && ack.vault == self.key.vault =>
            {
                // Acknowledgment binds the exact submitted buffer to the new etag.
                // GET has a different, trailing-LF-free representation.
                self.baseline = save.request.body;
                self.draft = self.baseline.clone();
                self.etag = ack.etag;
                self.state = DraftState::Clean;
                self.comparison = None;
                let leave = std::mem::take(&mut self.pending_leave);
                SaveOutcome::Saved { leave }
            }
            result => {
                self.pending_leave = false;
                self.state = match result {
                    Err(error) => match error.save_disposition() {
                        SaveDisposition::NotSent => DraftState::NotSent(format!("No save request was sent: {error}")),
                        SaveDisposition::Rejected => DraftState::Rejected(error.to_string()),
                        SaveDisposition::Conflict if is_etag_conflict(&error) => DraftState::Conflict(error.to_string()),
                        SaveDisposition::Conflict => DraftState::Rejected(format!("Save rejected (HTTP 409, not a confirmed body conflict): {error}")),
                        SaveDisposition::Uncertain => DraftState::Uncertain(format!("The save outcome is unknown. Keep this draft and inspect the server content: {error}")),
                    },
                    Ok(_) => DraftState::Uncertain("The save acknowledgment was incomplete or mismatched. Inspect the server content before another save.".into()),
                };
                SaveOutcome::Retained
            }
        }
    }
    pub fn leave_prompt(&self) -> LeavePrompt {
        if self.in_flight.is_some() {
            LeavePrompt::WaitForSave
        } else if self.composing {
            LeavePrompt::WaitForComposition
        } else if self.dirty() || matches!(self.state, DraftState::Uncertain(_)) {
            LeavePrompt::AskSaveDiscardCancel
        } else {
            LeavePrompt::Leave
        }
    }
    pub fn choose_leave(
        &mut self,
        choice: LeaveChoice,
        launch: u64,
        storage: &Storage,
    ) -> Result<LeaveAction, String> {
        if choice == LeaveChoice::Cancel {
            self.pending_leave = false;
            return Ok(LeaveAction::Stay);
        }
        if self.in_flight.is_some() {
            return Err("Wait for the current save before leaving.".into());
        }
        if self.composing {
            return Err("Finish or cancel IME composition before leaving.".into());
        }
        match choice {
            LeaveChoice::Cancel => {
                self.pending_leave = false;
                Ok(LeaveAction::Stay)
            }
            LeaveChoice::Discard => {
                self.retired = true;
                self.draft = self.baseline.clone();
                self.state = DraftState::Clean;
                self.pending_leave = false;
                self.comparison = None;
                Ok(LeaveAction::Leave)
            }
            LeaveChoice::Save => match self.prepare_save(launch, storage)? {
                Some(save) => {
                    self.pending_leave = true;
                    Ok(LeaveAction::Save(save))
                }
                None => Ok(LeaveAction::Leave),
            },
        }
    }
    /// Comparison does not replace the buffer, update its etag, clear dirty state,
    /// or authorize another save. A separate explicit decision is still required.
    pub fn inspect_latest(
        &mut self,
        launch: u64,
        storage: &Storage,
        source: &NoteResponse,
    ) -> Result<(), String> {
        if self.in_flight.is_some() {
            return Err("Wait for the save response before comparing.".into());
        }
        if self.retired {
            return Err("This draft was discarded.".into());
        }
        self.verify_epoch(launch, storage)?;
        if source.note.etag.is_empty() {
            return Err("Comparison has no source revision.".into());
        }
        if source.note.note.key() != self.key {
            return Err("The comparison belongs to a different note.".into());
        }
        self.write_blocked = verify_source(storage, source).is_err();
        self.comparison = Some(Comparison {
            body: source.note.body.clone(),
            etag: source.note.etag.clone(),
            verified: storage.phase == "ready"
                && !degraded(
                    &serde_json::to_value(&storage.additional).expect("JSON fields serialize"),
                )
                && !degraded(
                    &serde_json::to_value(&source.related_and_diagnostics)
                        .expect("JSON fields serialize"),
                )
                && !degraded(
                    &serde_json::to_value(&source.note.note.additional)
                        .expect("JSON fields serialize"),
                ),
        });
        if source.note.etag != self.etag && !matches!(self.state, DraftState::Uncertain(_)) {
            self.state = DraftState::Conflict(
                "The server revision has changed; local edits are preserved.".into(),
            );
        }
        Ok(())
    }
    /// Classify the comparison without rewriting either buffer. Only the draft is
    /// projected to the documented GET representation; no blanket whitespace trim.
    pub fn comparison_match(&self) -> Option<ComparisonMatch> {
        self.comparison
            .as_ref()
            .map(|comparison| compare_read_model(&self.draft, &comparison.body))
    }
    /// Exact GET text can reconcile normally. An already acknowledged unchanged
    /// draft also retains its authored LF bytes when the same etag is reread.
    /// For uncertain terminal LF bytes, use the separately explicit method below.
    pub fn accept_matching_comparison(&mut self) -> Result<ComparisonMatch, String> {
        self.accept_comparison(false)
    }
    /// Caller must explicitly explain that GET cannot verify terminal LF count.
    /// Accepts the observed GET revision as the baseline while retaining ALL authored
    /// bytes. This acknowledges read-model equivalence, NOT a byte-exact disk save.
    /// It never sends a write or retries the uncertain request.
    pub fn accept_read_model_comparison(&mut self) -> Result<ComparisonMatch, String> {
        self.accept_comparison(true)
    }
    fn accept_comparison(
        &mut self,
        accept_unobservable_lf: bool,
    ) -> Result<ComparisonMatch, String> {
        if self.in_flight.is_some() || self.composing || self.retired {
            return Err("Finish the pending save/composition, or load a fresh discarded session, before reconciliation.".into());
        }
        let comparison = self
            .comparison
            .as_ref()
            .ok_or("Inspect the current note first.")?;
        if !comparison.verified {
            return Err(
                "Comparison is partial, cached, or unverified; it cannot acknowledge a save."
                    .into(),
            );
        }
        let matched = compare_read_model(&self.draft, &comparison.body);
        if matched == ComparisonMatch::Different {
            return Err("The server body differs beyond terminal LF representation. Keep the draft for comparison or explicitly discard it; it will not be overwritten.".into());
        }
        let already_acknowledged = comparison.etag == self.etag
            && self.draft == self.baseline
            && !matches!(self.state, DraftState::Uncertain(_));
        if matched == ComparisonMatch::TerminalLfUnobservable
            && !already_acknowledged
            && !accept_unobservable_lf
        {
            return Err("The visible source matches, but GET removes terminal LF characters and cannot verify the draft's final LF count. The authored draft is preserved. Explicitly accept read-model equivalence only if that limitation is acceptable; this is not confirmation of exact disk bytes.".into());
        }
        // Never replace authored text with GET's lossy representation, even after
        // explicit acceptance. Future edits compare against this exact local baseline.
        self.baseline = self.draft.clone();
        self.etag = comparison.etag.clone();
        self.state = DraftState::Clean;
        self.pending_leave = false;
        self.comparison = None;
        Ok(matched)
    }
    fn verify_epoch(&self, launch: u64, storage: &Storage) -> Result<(), String> {
        if launch != self.launch_generation || storage.generation != self.storage_generation {
            return Err("The server or storage generation changed. Keep the draft and reload for comparison.".into());
        }
        Ok(())
    }
}
fn is_etag_conflict(error: &Error) -> bool {
    let Error::Http { status: 409, body } = error else {
        return false;
    };
    let json = serde_json::from_str::<Value>(body).ok();
    let message = json
        .as_ref()
        .and_then(|v| v.get("error"))
        .and_then(Value::as_str)
        .unwrap_or(body);
    message == "note changed on disk since it was loaded; reload before saving"
}
fn compare_read_model(draft: &str, server_body: &str) -> ComparisonMatch {
    if draft == server_body {
        ComparisonMatch::Exact
    } else if draft.trim_end_matches('\n') == server_body {
        ComparisonMatch::TerminalLfUnobservable
    } else {
        ComparisonMatch::Different
    }
}
fn verify_storage(storage: &Storage) -> Result<(), String> {
    if storage.phase != "ready"
        || storage.read_only
        || degraded(&serde_json::to_value(&storage.additional).expect("JSON fields serialize"))
    {
        return Err("Storage is not verified writable; editing is unavailable.".into());
    }
    Ok(())
}
fn degraded(value: &Value) -> bool {
    match value {
        Value::Object(fields) => fields.iter().any(|(key, value)| match key.as_str() {
            "partial" | "cache_used" | "read_only" => {
                value == &Value::Bool(true) || value.as_array().is_some_and(|a| !a.is_empty())
            }
            "unavailable" | "issues" => value.as_array().is_some_and(|a| !a.is_empty()),
            "omitted" => value.as_u64().is_some_and(|n| n > 0),
            "storage_state" => value.as_str().is_some_and(|s| s != "ready"),
            _ => degraded(value),
        }),
        Value::Array(values) => values.iter().any(degraded),
        _ => false,
    }
}
fn verify_source(storage: &Storage, source: &NoteResponse) -> Result<(), String> {
    verify_storage(storage)?;
    if source.note.read_only
        || source.note.etag.is_empty()
        || source.note.note.note_id == 0
        || degraded(&serde_json::to_value(&source.related_and_diagnostics).unwrap())
        || degraded(&serde_json::to_value(&source.note.note.additional).unwrap())
    {
        return Err(
            "This source has no safe writable revision or carries partial/cached diagnostics."
                .into(),
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    fn storage() -> Storage {
        serde_json::from_str(r#"{"phase":"ready","read_only":false,"verified":1,"generation":2}"#)
            .unwrap()
    }
    fn source(body: &str, etag: &str) -> NoteResponse {
        serde_json::from_value(serde_json::json!({"note":{"note_id":42,"vault":"日本語 & work","title":"Title","path":"display only","file_kind":"note","body":body,"etag":etag,"read_only":false}})).unwrap()
    }
    fn draft() -> EditSession {
        EditSession::from_note(1, &storage(), &source("before\n", "old")).unwrap()
    }
    fn ack() -> SaveAck {
        SaveAck {
            vault: "日本語 & work".into(),
            note_id: 42,
            etag: "new".into(),
            saved: true,
        }
    }
    #[test]
    fn clean_and_reverted_drafts_never_request_save() {
        let mut edit = draft();
        assert!(edit.prepare_save(1, &storage()).unwrap().is_none());
        edit.replace_text("different".into()).unwrap();
        edit.replace_text("before\n".into()).unwrap();
        assert!(!edit.dirty());
        assert!(edit.prepare_save(1, &storage()).unwrap().is_none());
    }
    #[test]
    fn normalized_success_and_duplicate_save_lock() {
        for (body, expected) in [
            ("", ""),
            ("日本語 [[Link]]", "日本語 [[Link]]"),
            ("a\n", "a\n"),
            ("a\n\n", "a\n\n"),
        ] {
            let mut edit = draft();
            edit.replace_text(body.into()).unwrap();
            let save = edit.prepare_save(1, &storage()).unwrap().unwrap();
            assert_eq!(save.request.body, body);
            assert_eq!(save.request.etag, "old");
            assert!(edit.replace_text("newer".into()).is_err());
            assert!(edit.prepare_save(1, &storage()).is_err());
            assert_eq!(
                edit.finish_save(save.attempt, 1, 2, Ok(ack())),
                SaveOutcome::Saved { leave: false }
            );
            assert_eq!(edit.draft(), expected);
            assert!(!edit.dirty());
            assert_eq!(edit.etag(), "new");
        }
    }
    #[test]
    fn cancel_discard_and_failed_navigation_never_lose_edits() {
        let mut edit = draft();
        edit.replace_text("local".into()).unwrap();
        assert_eq!(edit.leave_prompt(), LeavePrompt::AskSaveDiscardCancel);
        assert!(matches!(
            edit.choose_leave(LeaveChoice::Cancel, 1, &storage())
                .unwrap(),
            LeaveAction::Stay
        ));
        assert_eq!(edit.draft(), "local");
        let LeaveAction::Save(save) = edit.choose_leave(LeaveChoice::Save, 1, &storage()).unwrap()
        else {
            panic!()
        };
        assert_eq!(
            edit.finish_save(
                save.attempt,
                1,
                2,
                Err(Error::Http {
                    status: 409,
                    body: "changed on disk".into()
                })
            ),
            SaveOutcome::Retained
        );
        assert_eq!(edit.draft(), "local");
        assert_eq!(edit.etag(), "old");
        assert!(matches!(
            edit.choose_leave(LeaveChoice::Discard, 1, &storage())
                .unwrap(),
            LeaveAction::Leave
        ));
    }
    #[test]
    fn ambiguous_outcomes_require_inspection_and_explicit_reconciliation() {
        let errors = vec![
            Error::Transport("lost response".into()),
            Error::Http {
                status: 500,
                body: "index failed".into(),
            },
            Error::Json("invalid".into()),
        ];
        for error in errors {
            let mut edit = draft();
            edit.replace_text("local".into()).unwrap();
            let save = edit.prepare_save(1, &storage()).unwrap().unwrap();
            edit.finish_save(save.attempt, 1, 2, Err(error));
            assert!(matches!(edit.state(), DraftState::Uncertain(_)));
            assert!(edit.prepare_save(1, &storage()).is_err());
            assert!(edit.dirty());
            edit.inspect_latest(1, &storage(), &source("local", "new"))
                .unwrap();
            assert!(edit.dirty());
            assert_eq!(edit.etag(), "old");
            edit.accept_matching_comparison().unwrap();
            assert!(!edit.dirty());
            assert_eq!(edit.etag(), "new");
        }
    }
    #[test]
    fn stale_response_and_epoch_cannot_clear_draft() {
        let mut edit = draft();
        edit.replace_text("local".into()).unwrap();
        assert!(edit.prepare_save(99, &storage()).is_err());
        let save = edit.prepare_save(1, &storage()).unwrap().unwrap();
        assert_eq!(
            edit.finish_save(save.attempt + 1, 1, 2, Ok(ack())),
            SaveOutcome::StaleResponse
        );
        let mut wrong = ack();
        wrong.vault = "another vault".into();
        assert_eq!(
            edit.finish_save(save.attempt, 1, 2, Ok(wrong)),
            SaveOutcome::Retained
        );
        assert!(edit.dirty());
    }
    #[test]
    fn source_safety_is_fail_closed() {
        let mut note = source("body", "etag");
        note.note.read_only = true;
        assert!(EditSession::from_note(1, &storage(), &note).is_err());
        let mut note = source("body", "etag");
        note.related_and_diagnostics
            .insert("read_state".into(), serde_json::json!({"partial":true}));
        assert!(EditSession::from_note(1, &storage(), &note).is_err());
        let mut st = storage();
        st.phase = "initializing".into();
        assert!(EditSession::from_note(1, &st, &source("body", "etag")).is_err());
        assert!(EditSession::from_note(1, &storage(), &source("body", "")).is_err());
    }
    fn prepare_local(edit: &mut EditSession) -> PreparedSave {
        edit.replace_text("local".into()).unwrap();
        edit.prepare_save(1, &storage()).unwrap().unwrap()
    }
    #[test]
    fn typed_disposition_distinguishes_not_sent_rejection_and_etag_conflict() {
        let cases = [
            (Error::SaveNotSent(Box::new(Error::Transport("preflight GET timed out".into()))), "not_sent"),
            (Error::Http { status: 409, body: r#"{"error":"required metadata unavailable"}"#.into() }, "rejected"),
            (Error::Http { status: 409, body: r#"{"error":"note changed on disk since it was loaded; reload before saving"}"#.into() }, "conflict"),
            (Error::Http { status: 403, body: "origin rejected".into() }, "rejected"),
            (Error::Config("not proven preflight".into()), "uncertain"),
            (Error::Http { status: 500, body: "write may have happened".into() }, "uncertain"),
        ];
        for (error, expected) in cases {
            let mut edit = draft();
            let save = prepare_local(&mut edit);
            assert_eq!(
                edit.finish_save(save.attempt, 1, 2, Err(error)),
                SaveOutcome::Retained
            );
            let actual = match edit.state() {
                DraftState::NotSent(_) => "not_sent",
                DraftState::Rejected(_) => "rejected",
                DraftState::Conflict(_) => "conflict",
                DraftState::Uncertain(_) => "uncertain",
                _ => panic!(),
            };
            assert_eq!(actual, expected);
            assert_eq!(edit.draft(), "local");
            assert_eq!(edit.etag(), "old");
            assert_eq!(edit.baseline(), "before\n");
            if expected == "not_sent" || expected == "rejected" {
                assert!(edit.prepare_save(1, &storage()).unwrap().is_some());
            } else {
                assert!(edit.prepare_save(1, &storage()).is_err());
            }
        }
    }
    #[test]
    fn replacement_session_and_other_vault_cannot_consume_old_save_token() {
        let mut first = draft();
        let first_save = prepare_local(&mut first);
        let mut replacement = draft();
        let new_save = prepare_local(&mut replacement);
        assert_ne!(first_save.attempt, new_save.attempt);
        assert_eq!(
            replacement.finish_save(first_save.attempt, 1, 2, Ok(ack())),
            SaveOutcome::StaleResponse
        );
        assert!(replacement.is_saving());
        assert_eq!(replacement.draft(), "local");
        let mut other_source = source("before\n", "other-revision");
        other_source.note.note.vault = "other".into();
        let mut other = EditSession::from_note(1, &storage(), &other_source).unwrap();
        let other_save = prepare_local(&mut other);
        assert_eq!(other_save.request.key.vault, "other");
        assert_eq!(first_save.request.key.vault, "日本語 & work");
        assert_eq!(
            other.finish_save(first_save.attempt, 1, 2, Ok(ack())),
            SaveOutcome::StaleResponse
        );
        assert_eq!(
            other.finish_save(other_save.attempt, 1, 2, Ok(ack())),
            SaveOutcome::Retained
        );
        assert_eq!(other.etag(), "other-revision");
    }
    #[test]
    fn epoch_change_during_save_cannot_clean_or_complete_leave() {
        for (launch, generation) in [(9, 2), (1, 9)] {
            let mut edit = draft();
            edit.replace_text("local".into()).unwrap();
            let LeaveAction::Save(save) =
                edit.choose_leave(LeaveChoice::Save, 1, &storage()).unwrap()
            else {
                panic!()
            };
            assert_eq!(
                edit.finish_save(save.attempt, launch, generation, Ok(ack())),
                SaveOutcome::Retained
            );
            assert!(matches!(edit.state(), DraftState::Uncertain(_)));
            assert_eq!(edit.draft(), "local");
            assert!(!edit.pending_leave);
        }
    }
    #[test]
    fn cancel_pending_navigation_does_not_cancel_write_or_leave_after_ack() {
        let mut edit = draft();
        edit.replace_text("local".into()).unwrap();
        let LeaveAction::Save(save) = edit.choose_leave(LeaveChoice::Save, 1, &storage()).unwrap()
        else {
            panic!()
        };
        assert!(matches!(
            edit.choose_leave(LeaveChoice::Cancel, 1, &storage())
                .unwrap(),
            LeaveAction::Stay
        ));
        assert!(edit.is_saving());
        assert!(
            edit.choose_leave(LeaveChoice::Discard, 1, &storage())
                .is_err()
        );
        assert_eq!(
            edit.finish_save(save.attempt, 1, 2, Ok(ack())),
            SaveOutcome::Saved { leave: false }
        );
    }
    #[test]
    fn composition_cannot_save_leave_or_be_discarded_until_finished() {
        let mut edit = draft();
        edit.replace_text_with_composition("にほ".into(), true)
            .unwrap();
        assert_eq!(edit.leave_prompt(), LeavePrompt::WaitForComposition);
        assert!(edit.prepare_save(1, &storage()).is_err());
        assert!(
            edit.choose_leave(LeaveChoice::Discard, 1, &storage())
                .is_err()
        );
        assert!(matches!(
            edit.choose_leave(LeaveChoice::Cancel, 1, &storage())
                .unwrap(),
            LeaveAction::Stay
        ));
        assert_eq!(edit.draft(), "にほ");
        edit.set_composing(false).unwrap();
        let save = edit.prepare_save(1, &storage()).unwrap().unwrap();
        assert!(
            edit.replace_text_with_composition("new preedit".into(), true)
                .is_err()
        );
        assert_eq!(edit.draft(), save.request.body);
    }
    #[test]
    fn discarded_uncertain_session_cannot_retry_with_old_revision() {
        let mut edit = draft();
        let save = prepare_local(&mut edit);
        edit.finish_save(
            save.attempt,
            1,
            2,
            Err(Error::Transport("lost response".into())),
        );
        assert!(matches!(
            edit.choose_leave(LeaveChoice::Discard, 1, &storage())
                .unwrap(),
            LeaveAction::Leave
        ));
        assert!(edit.is_retired());
        assert!(edit.replace_text("later".into()).is_err());
        assert!(edit.prepare_save(1, &storage()).is_err());
    }
    #[test]
    fn readonly_comparison_preserves_draft_and_cannot_reenable_writes() {
        let mut edit = draft();
        let save = prepare_local(&mut edit);
        edit.finish_save(
            save.attempt,
            1,
            2,
            Err(Error::Transport("lost response".into())),
        );
        let mut st = storage();
        st.read_only = true;
        let mut latest = source("local", "new");
        latest.note.read_only = true;
        edit.inspect_latest(1, &st, &latest).unwrap();
        assert_eq!(edit.draft(), "local");
        assert_eq!(edit.etag(), "old");
        edit.accept_matching_comparison().unwrap();
        edit.replace_text("another".into()).unwrap();
        assert!(edit.prepare_save(1, &storage()).is_err());
    }
    #[test]
    fn partial_comparison_cannot_reconcile_and_partial_storage_cannot_edit() {
        let mut edit = draft();
        let save = prepare_local(&mut edit);
        edit.finish_save(
            save.attempt,
            1,
            2,
            Err(Error::Transport("lost response".into())),
        );
        let mut latest = source("local", "new");
        latest
            .related_and_diagnostics
            .insert("read_state".into(), serde_json::json!({"cache_used":true}));
        edit.inspect_latest(1, &storage(), &latest).unwrap();
        assert!(edit.accept_matching_comparison().is_err());
        assert!(edit.dirty());
        let mut st = storage();
        st.additional.insert("partial".into(), Value::Bool(true));
        assert!(EditSession::from_note(1, &st, &source("body", "revision")).is_err());
        let mut edit = draft();
        edit.replace_text("local".into()).unwrap();
        assert!(edit.prepare_save(1, &st).is_err());
    }
    #[test]
    fn acknowledged_buffers_preserve_all_four_newline_cases_across_reread() {
        for authored in ["", "final", "final\n", "final\n\n"] {
            let mut edit = draft();
            edit.replace_text(authored.into()).unwrap();
            let save = edit.prepare_save(1, &storage()).unwrap().unwrap();
            assert_eq!(
                edit.finish_save(save.attempt, 1, 2, Ok(ack())),
                SaveOutcome::Saved { leave: false }
            );
            assert_eq!(edit.baseline(), authored);
            assert_eq!(edit.draft(), authored);
            assert!(edit.prepare_save(1, &storage()).unwrap().is_none());
            let reread = source(authored.trim_end_matches('\n'), "new");
            edit.inspect_latest(1, &storage(), &reread).unwrap();
            assert_eq!(edit.state(), &DraftState::Clean);
            assert!(!edit.dirty());
            assert_eq!(edit.draft(), authored);
            edit.accept_matching_comparison().unwrap();
            assert_eq!(edit.baseline(), authored);
            assert_eq!(edit.draft(), authored);
            assert!(edit.prepare_save(1, &storage()).unwrap().is_none());
        }
    }
    #[test]
    fn uncertain_reconciliation_preserves_authored_lfs_and_exposes_unobservable_bytes() {
        for authored in ["", "final", "final\n", "final\n\n"] {
            let mut edit = draft();
            edit.replace_text(authored.into()).unwrap();
            let save = edit.prepare_save(1, &storage()).unwrap().unwrap();
            edit.finish_save(
                save.attempt,
                1,
                2,
                Err(Error::Transport("response dropped".into())),
            );
            edit.inspect_latest(
                1,
                &storage(),
                &source(authored.trim_end_matches('\n'), "new"),
            )
            .unwrap();
            assert_eq!(edit.draft(), authored);
            assert_eq!(edit.etag(), "old");
            if authored.ends_with('\n') {
                assert_eq!(
                    edit.comparison_match(),
                    Some(ComparisonMatch::TerminalLfUnobservable)
                );
                assert!(edit.accept_matching_comparison().is_err());
                assert!(matches!(edit.state(), DraftState::Uncertain(_)));
                assert_eq!(
                    edit.accept_read_model_comparison().unwrap(),
                    ComparisonMatch::TerminalLfUnobservable
                );
            } else {
                assert_eq!(
                    edit.accept_matching_comparison().unwrap(),
                    ComparisonMatch::Exact
                );
            }
            assert_eq!(edit.draft(), authored);
            assert_eq!(edit.baseline(), authored);
            assert_eq!(edit.etag(), "new");
            assert!(!edit.dirty());
        }
    }
    #[test]
    fn read_model_projection_does_not_ignore_other_content_or_whitespace() {
        for (draft, server) in [
            ("a \n", "a"),
            ("a\r\n", "a"),
            ("a\tb\n", "ab"),
            ("a\n\nb\n", "a\nb"),
            ("e\u{301}\n", "é"),
            ("[[A]]\n", "[[B]]"),
        ] {
            assert_eq!(
                compare_read_model(draft, server),
                ComparisonMatch::Different
            );
        }
        assert_eq!(
            compare_read_model("a\r\n", "a\r"),
            ComparisonMatch::TerminalLfUnobservable
        );
        assert_eq!(
            compare_read_model("\n\n", ""),
            ComparisonMatch::TerminalLfUnobservable
        );
        // Don't normalize a malformed/unknown server representation on both sides.
        assert_eq!(compare_read_model("a", "a\n"), ComparisonMatch::Different);
    }
    #[test]
    fn unchanged_get_and_old_etag_cannot_silently_confirm_lf_only_edit() {
        let mut edit = EditSession::from_note(1, &storage(), &source("a", "old")).unwrap();
        edit.replace_text("a\n\n".into()).unwrap();
        let save = edit.prepare_save(1, &storage()).unwrap().unwrap();
        edit.finish_save(save.attempt, 1, 2, Err(Error::Transport("unknown".into())));
        edit.inspect_latest(1, &storage(), &source("a", "old"))
            .unwrap();
        assert!(edit.accept_matching_comparison().is_err());
        assert_eq!(edit.draft(), "a\n\n");
        assert!(edit.dirty());
        assert!(edit.prepare_save(1, &storage()).is_err());
    }
}
