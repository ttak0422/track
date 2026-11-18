//! Explicit, non-persistent follow invalidations. No paths, writes, or scroll commands.
use crate::{live, live_events};
use serde::Deserialize;
use std::time::{Duration, Instant};

pub const WIRE_LIMIT: usize = 16 * 1024;
const INTERVAL: Duration = Duration::from_millis(200);
const EXPIRY: Duration = Duration::from_secs(10);

#[derive(Clone, Debug, Deserialize, PartialEq, Eq)]
pub struct FollowState {
    #[serde(default)]
    pub vault: String,
    pub note_id: i64,
    pub file_kind: String,
    pub line: u32,
    pub top_line: u32,
    pub line_count: u32,
    pub updated_at: String,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum FollowResponse {
    Inactive,
    Active(FollowState),
}
impl FollowResponse {
    pub fn decode(bytes: &[u8]) -> Result<Self, &'static str> {
        if bytes.len() > WIRE_LIMIT {
            return Err("Follow response exceeds limit");
        }
        // Untyped wire data lives only during decoding; unknown fields never enter state.
        let value: serde_json::Value =
            serde_json::from_slice(bytes).map_err(|_| "Invalid follow response")?;
        match value.get("active").and_then(|v| v.as_bool()) {
            Some(false) => Ok(Self::Inactive),
            Some(true) => {
                let state: FollowState = serde_json::from_value(
                    value.get("state").cloned().ok_or("Missing follow state")?,
                )
                .map_err(|_| "Invalid follow state")?;
                if state.note_id <= 0
                    || !matches!(state.file_kind.as_str(), "note" | "journal")
                    || state.vault.len() > 256
                    || state.vault.chars().any(char::is_control)
                    || [state.line, state.top_line, state.line_count]
                        .iter()
                        .any(|n| !(1..=1_000_000_000).contains(n))
                    || state.updated_at.is_empty()
                    || state.updated_at.len() > 128
                    || !timestamp_valid(&state.updated_at)
                {
                    return Err("Invalid follow state");
                }
                Ok(Self::Active(state))
            }
            _ => Err("Missing follow active flag"),
        }
    }
}

// Validate the bounded RFC3339Nano wire shape, not timestamp order or timezone
// conversion. Reuse the existing Gregorian date validator.
fn timestamp_valid(value: &str) -> bool {
    let b = value.as_bytes();
    if !value.is_ascii()
        || b.len() < 20
        || !live::calendar_date_valid(&value[..10])
        || !matches!(b[10], b'T' | b't')
        || b[13] != b':'
        || b[16] != b':'
    {
        return false;
    }
    let number = |start: usize| -> Option<u32> {
        let digits = b.get(start..start + 2)?;
        if !digits.iter().all(u8::is_ascii_digit) {
            return None;
        }
        Some(((digits[0] - b'0') as u32) * 10 + (digits[1] - b'0') as u32)
    };
    if !number(11).is_some_and(|n| n <= 23)
        || !number(14).is_some_and(|n| n <= 59)
        || !number(17).is_some_and(|n| n <= 60)
    {
        return false;
    }
    let mut end = 19;
    if b[end] == b'.' {
        end += 1;
        let first = end;
        while b.get(end).is_some_and(u8::is_ascii_digit) {
            end += 1;
        }
        if !(1..=9).contains(&(end - first)) {
            return false;
        }
    }
    match b.get(end) {
        Some(b'Z' | b'z') => end + 1 == b.len(),
        Some(b'+' | b'-') => {
            end + 6 == b.len()
                && b[end + 3] == b':'
                && number(end + 1).is_some_and(|n| n <= 23)
                && number(end + 4).is_some_and(|n| n <= 59)
        }
        _ => false,
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Identity {
    pub launch: u64,
    pub storage: Option<u64>,
    pub registry: String,
    pub observer: u64,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Request {
    pub identity: Identity,
    epoch: u64,
    sequence: u64,
    pub(crate) signal: u64,
    started: Instant,
}
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Status {
    #[default]
    Off,
    Waiting,
    Following,
    Paused(&'static str),
}
#[derive(Default)]
pub struct Controller {
    status: Status,
    identity: Option<Identity>,
    epoch: u64,
    sequence: u64,
    signal: u64,
    gaps: u64,
    pending: bool,
    // Retain occupied slot across stop/re-enable until its worker actually returns.
    inflight: Option<Request>,
    last_start: Option<Instant>,
    accepted: Option<Request>,
}
impl Controller {
    pub fn status(&self) -> Status {
        self.status
    }
    pub fn enabled(&self) -> bool {
        self.identity.is_some()
    }
    pub fn enable(&mut self, identity: Identity, snapshot: live_events::Snapshot) {
        if snapshot.unavailable {
            self.pause("Live observation is unavailable in this session; use explicit navigation");
            return;
        }
        self.epoch = self.epoch.checked_add(1).expect("follow epoch exhausted");
        self.identity = Some(identity);
        self.signal = snapshot.follow;
        self.gaps = snapshot.gaps;
        self.pending = true;
        self.accepted = None;
        self.status = Status::Waiting;
    }
    pub fn stop(&mut self) {
        self.identity = None;
        self.pending = false;
        self.accepted = None;
        self.status = Status::Off;
    }
    pub fn pause(&mut self, reason: &'static str) {
        self.stop();
        self.status = Status::Paused(reason);
    }
    pub fn observe(&mut self, identity: &Identity, snapshot: live_events::Snapshot) {
        if snapshot.unavailable {
            if self.enabled() {
                self.pause(
                    "Live observation is unavailable in this session; use explicit navigation",
                );
            }
            return;
        }
        let Some(current) = self.identity.as_ref() else {
            return;
        };
        if current != identity || self.gaps != snapshot.gaps {
            self.pause("Connection or served scope changed; enable Follow again");
            return;
        }
        if self.signal != snapshot.follow {
            self.signal = snapshot.follow;
            self.pending = true;
            self.accepted = None;
        }
    }
    pub fn take_request(&mut self, now: Instant) -> Option<Request> {
        if !self.pending
            || self.inflight.is_some()
            || self
                .last_start
                .is_some_and(|last| now.saturating_duration_since(last) < INTERVAL)
        {
            return None;
        }
        let identity = self.identity.clone()?;
        self.sequence = self
            .sequence
            .checked_add(1)
            .expect("follow sequence exhausted");
        let request = Request {
            identity,
            epoch: self.epoch,
            sequence: self.sequence,
            signal: self.signal,
            started: now,
        };
        self.pending = false;
        self.last_start = Some(now);
        self.inflight = Some(request.clone());
        Some(request)
    }
    /// Also guards an asynchronous note-open completion. Observe the latest SSE snapshot first.
    pub fn is_current(&self, request: &Request, identity: &Identity) -> bool {
        self.identity.as_ref() == Some(identity)
            && &request.identity == identity
            && request.epoch == self.epoch
            && request.sequence == self.sequence
            && request.signal == self.signal
            && !self.pending
    }
    pub fn finish(
        &mut self,
        request: &Request,
        identity: &Identity,
        response: Result<FollowResponse, live::Error>,
        now: Instant,
    ) -> Option<FollowState> {
        if self.inflight.as_ref() != Some(request) {
            return None;
        }
        self.inflight = None;
        if !self.is_current(request, identity) {
            return None;
        }
        match response {
            Ok(FollowResponse::Inactive) => {
                self.accepted = None;
                self.status = Status::Waiting;
                None
            }
            Ok(FollowResponse::Active(state)) => {
                if now.saturating_duration_since(request.started) >= EXPIRY {
                    self.status = Status::Waiting;
                    return None;
                }
                self.accepted = Some(request.clone());
                self.status = Status::Following;
                Some(state)
            }
            Err(_) => {
                self.pause("Editor follow unavailable; enable Follow to retry");
                None
            }
        }
    }
    /// Display-only TTL; expiry never starts a GET or changes the current note.
    pub fn expire(&mut self, now: Instant) {
        if self
            .accepted
            .as_ref()
            .is_some_and(|r| now.saturating_duration_since(r.started) >= EXPIRY)
        {
            self.accepted = None;
            self.status = Status::Waiting;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn state() -> serde_json::Value {
        serde_json::json!({"active":true,"state":{"note_id":i64::MAX,"file_kind":"note",
            "line":4,"top_line":2,"line_count":1,"updated_at":"2026-11-15T04:48:51.78618+09:00",
            "path":"/private/never-open","vault_path":"/private/never-retain"}})
    }
    fn identity() -> Identity {
        Identity {
            launch: 1,
            storage: Some(2),
            registry: "verified".into(),
            observer: 3,
        }
    }
    #[test]
    fn decode_strict_and_discards_paths() {
        let parsed = FollowResponse::decode(&serde_json::to_vec(&state()).unwrap()).unwrap();
        let FollowResponse::Active(s) = parsed else {
            panic!()
        };
        assert_eq!(s.note_id, i64::MAX);
        assert!(s.vault.is_empty());
        assert!(!format!("{s:?}").contains("private"));
        assert_eq!(
            FollowResponse::decode(br#"{"active":false,"state":"anything"}"#),
            Ok(FollowResponse::Inactive)
        );
        for (field, value) in [
            ("note_id", serde_json::json!(0)),
            ("file_kind", serde_json::json!("other")),
            ("line", serde_json::json!(-1)),
            ("top_line", serde_json::json!(1.5)),
            ("line_count", serde_json::json!(1_000_000_001)),
            ("vault", serde_json::json!(null)),
            ("updated_at", serde_json::json!("2026-02-30T00:00:00Z")),
            ("updated_at", serde_json::json!("2026-11-15T04:48:51")),
            ("vault", serde_json::json!("v".repeat(257))),
        ] {
            let mut wire = state();
            wire["state"][field] = value;
            assert!(
                FollowResponse::decode(&serde_json::to_vec(&wire).unwrap()).is_err(),
                "{field}"
            );
        }
        assert!(FollowResponse::decode(&vec![b' '; WIRE_LIMIT + 1]).is_err());
        assert!(FollowResponse::decode(br#"{"active":1}"#).is_err());
    }
    #[test]
    fn timestamp_offsets_fraction_and_date_bounds() {
        for timestamp in [
            "2024-02-29T23:59:59Z",
            "2026-11-15T04:48:51.123456789+09:00",
            "2026-11-15t04:48:51-07:30",
        ] {
            assert!(timestamp_valid(timestamp));
        }
        for timestamp in [
            "2025-02-29T00:00:00Z",
            "2026-11-15T24:00:00Z",
            "2026-11-15T04:60:00Z",
            "2026-11-15T04:00:61Z",
            "2026-11-15T04:00:00+24:00",
            "2026-11-15T04:00:00+01:60",
            "2026-11-15T04:00:00.Z",
            "2026-11-15T04:00:00.1234567890Z",
            "2026-11-15T04:00:00Zextra",
        ] {
            assert!(!timestamp_valid(timestamp), "{timestamp}");
        }
    }
    #[test]
    fn bursts_latest_only_rate_limit_and_enable_aba() {
        let mut c = Controller::default();
        let now = Instant::now();
        let id = identity();
        assert!(c.take_request(now).is_none());
        c.enable(id.clone(), live_events::Snapshot::default());
        let first = c.take_request(now).unwrap();
        for follow in 1..100 {
            c.observe(
                &id,
                live_events::Snapshot {
                    follow,
                    ..Default::default()
                },
            );
            assert!(c.take_request(now + Duration::from_secs(1)).is_none());
        }
        assert!(
            c.finish(&first, &id, Ok(FollowResponse::Inactive), now)
                .is_none()
        );
        assert!(c.take_request(now + Duration::from_millis(199)).is_none());
        let latest = c.take_request(now + INTERVAL).unwrap();
        c.stop();
        c.enable(id.clone(), live_events::Snapshot::default());
        assert!(c.take_request(now + Duration::from_secs(1)).is_none());
        c.finish(&latest, &id, Ok(FollowResponse::Inactive), now);
        assert_eq!(c.status(), Status::Waiting);
        assert!(c.take_request(now + Duration::from_secs(1)).is_some());
    }
    #[test]
    fn unavailable_observation_refuses_enable_and_invalidates_inflight_follow() {
        let mut c = Controller::default();
        let now = Instant::now();
        let id = identity();
        let unavailable = live_events::Snapshot {
            unavailable: true,
            ..Default::default()
        };
        c.enable(id.clone(), unavailable);
        assert!(!c.enabled());
        assert!(matches!(c.status(), Status::Paused(_)));
        assert!(c.take_request(now).is_none());

        c.enable(id.clone(), Default::default());
        let request = c.take_request(now).unwrap();
        c.observe(&id, unavailable);
        assert!(!c.enabled());
        assert!(!c.is_current(&request, &id));
        let response = FollowResponse::decode(&serde_json::to_vec(&state()).unwrap()).unwrap();
        assert!(c.finish(&request, &id, Ok(response), now).is_none());
        assert!(matches!(c.status(), Status::Paused(_)));
        c.observe(&id, Default::default());
        assert!(c.take_request(now + INTERVAL).is_none());
        c.enable(id.clone(), Default::default());
        assert!(c.take_request(now + INTERVAL).is_some());
    }
    #[test]
    fn gap_identity_errors_expiry_require_explicit_enable() {
        let mut c = Controller::default();
        let now = Instant::now();
        let id = identity();
        c.enable(id.clone(), Default::default());
        let r = c.take_request(now).unwrap();
        let response = FollowResponse::decode(&serde_json::to_vec(&state()).unwrap()).unwrap();
        assert!(c.finish(&r, &id, Ok(response), now).is_some());
        c.expire(now + EXPIRY);
        assert_eq!(c.status(), Status::Waiting);
        assert!(c.take_request(now + EXPIRY).is_none());
        c.observe(
            &id,
            live_events::Snapshot {
                gaps: 1,
                ..Default::default()
            },
        );
        assert!(!c.enabled());
        c.observe(
            &id,
            live_events::Snapshot {
                connected: true,
                follow: 2,
                ..Default::default()
            },
        );
        assert!(c.take_request(now + EXPIRY).is_none());
        c.enable(id.clone(), Default::default());
        let mut changed = id.clone();
        changed.storage = Some(3);
        c.observe(&changed, Default::default());
        assert!(!c.enabled());
        c.enable(id.clone(), Default::default());
        let r = c.take_request(now + EXPIRY).unwrap();
        c.finish(&r, &id, Err(live::Error::Stale), now + EXPIRY);
        assert!(matches!(c.status(), Status::Paused(_)));
    }
}
