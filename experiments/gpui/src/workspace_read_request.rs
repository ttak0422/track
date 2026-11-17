//! Inactive presentation state only. A recorded preview choice is not a grant,
//! filesystem check, launch capability, or permission to read a real workspace.
//! Supplied active and registered display paths remain independent. No alias
//! equivalence, complete registry, or production authorization is inferred.
use std::collections::BTreeMap;

fn unsafe_display(c: char) -> bool {
    c.is_control()
        || matches!(c, '\u{061c}' | '\u{200e}' | '\u{200f}' | '\u{202a}'..='\u{202e}' | '\u{2066}'..='\u{2069}')
}

pub const NOTICE: &str = "Fixture preview only · ordinary workspace access is blocked";
pub const CONSEQUENCES: &str = "Read the active vault and every registered peer listed below. Canonical notes remain read-only: no edits, saves, or creation. Derived indexes use a separate private cache. Unknown cache contents are retained, not recursively deleted; cleanup may require separate review.";
#[derive(Clone, PartialEq, Eq)]
pub struct WorkspaceRequest {
    active: String,
    active_display_path: String,
    peers: BTreeMap<String, String>,
}
impl std::fmt::Debug for WorkspaceRequest {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("WorkspaceRequest { values: [redacted] }")
    }
}
impl WorkspaceRequest {
    /// Inputs must be supplied explicitly. This does not discover registry entries
    /// or prove that the supplied set is complete, authorized, or filesystem-safe.
    pub fn new(
        active: String,
        active_display_path: String,
        peers: Vec<(String, String)>,
    ) -> Result<Self, &'static str> {
        let valid_name = |s: &str| {
            !s.is_empty()
                && s.len() <= 256
                && s.bytes()
                    .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
        };
        if (!active.is_empty() && !valid_name(&active))
            || peers.len() > 32
            || !active_display_path.starts_with('/')
            || active_display_path.len() > 4096
            || active_display_path.chars().any(unsafe_display)
        {
            return Err("Invalid workspace request");
        }
        let mut entries = BTreeMap::new();
        for (name, path) in peers {
            if !valid_name(&name)
                || !path.starts_with('/')
                || path.len() > 4096
                || path.chars().any(unsafe_display)
                || entries.insert(name, path).is_some()
            {
                return Err("Invalid workspace request");
            }
        }
        if !active.is_empty() && !entries.contains_key(&active) {
            return Err("Missing active vault");
        }
        Ok(Self {
            active,
            active_display_path,
            peers: entries,
        })
    }
    pub fn active(&self) -> (&str, &str) {
        (&self.active, &self.active_display_path)
    }
    pub fn rows(&self) -> impl Iterator<Item = (&str, &str, bool)> {
        self.peers
            .iter()
            .map(|(name, path)| (name.as_str(), path.as_str(), name == &self.active))
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Decision {
    Pending,
    PreviewConfirmed,
    Denied,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Event {
    ExplicitConfirm,
    Cancel,
    Close,
    Enter,
}
#[derive(Debug)]
pub struct RequestDialog {
    request: WorkspaceRequest,
    generation: u64,
    decision: Decision,
}
impl RequestDialog {
    pub fn new(request: WorkspaceRequest) -> Self {
        Self {
            request,
            generation: 1,
            decision: Decision::Pending,
        }
    }
    pub fn request(&self) -> &WorkspaceRequest {
        &self.request
    }
    pub fn generation(&self) -> u64 {
        self.generation
    }
    pub fn decision(&self) -> Decision {
        self.decision
    }
    pub fn replace(&mut self, request: WorkspaceRequest) -> Result<(), &'static str> {
        // Even identical content invalidates old events and the old choice.
        self.decision = Decision::Denied;
        self.generation = self
            .generation
            .checked_add(1)
            .ok_or("Request generation exhausted")?;
        self.request = request;
        self.decision = Decision::Pending;
        Ok(())
    }
    /// Denial is terminal; a recorded preview choice can still be dismissed.
    /// Stale events never change a decision. Enter is never consent.
    pub fn apply(&mut self, generation: u64, event: Event) -> bool {
        if generation != self.generation || self.decision == Decision::Denied {
            return false;
        }
        self.decision = match event {
            Event::ExplicitConfirm if self.decision == Decision::Pending => {
                Decision::PreviewConfirmed
            }
            Event::ExplicitConfirm => return false,
            Event::Cancel | Event::Close => Decision::Denied,
            Event::Enter => return false,
        };
        true
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn request() -> WorkspaceRequest {
        WorkspaceRequest::new(
            "work".into(),
            "/fixture/work".into(),
            vec![
                ("work".into(), "/fixture/work".into()),
                ("main".into(), "/fixture/main".into()),
            ],
        )
        .unwrap()
    }
    #[test]
    fn pending_cancel_close_and_enter_never_confirm() {
        for event in [Event::Cancel, Event::Close] {
            let mut dialog = RequestDialog::new(request());
            assert_eq!(dialog.decision(), Decision::Pending);
            assert!(!dialog.apply(1, Event::Enter));
            assert!(dialog.apply(1, event));
            assert_eq!(dialog.decision(), Decision::Denied);
            assert!(!dialog.apply(1, Event::ExplicitConfirm));
            assert!(!dialog.apply(1, event));
        }
    }
    #[test]
    fn replacement_invalidates_confirmation_and_stale_events() {
        let mut dialog = RequestDialog::new(request());
        assert!(dialog.apply(1, Event::ExplicitConfirm));
        assert!(!dialog.apply(1, Event::ExplicitConfirm));
        assert!(dialog.apply(1, Event::Cancel));
        assert_eq!(dialog.decision(), Decision::Denied);
        dialog.replace(request()).unwrap();
        assert_eq!(dialog.decision(), Decision::Pending);
        for event in [Event::ExplicitConfirm, Event::Cancel, Event::Close] {
            assert!(!dialog.apply(1, event));
        }
        assert!(!dialog.apply(2, Event::Enter));
        assert!(dialog.apply(2, Event::ExplicitConfirm));
    }
    #[test]
    fn confirmed_preview_can_be_dismissed_without_authorizing_access() {
        for event in [Event::Cancel, Event::Close] {
            let mut dialog = RequestDialog::new(request());
            assert!(dialog.apply(1, Event::ExplicitConfirm));
            assert!(dialog.apply(1, event));
            assert_eq!(dialog.decision(), Decision::Denied);
            assert!(!dialog.apply(1, Event::ExplicitConfirm));
            assert!(!dialog.apply(1, event));
        }
    }
    #[test]
    fn active_alias_and_unregistered_paths_are_displayed_independently() {
        let alias = WorkspaceRequest::new(
            "work".into(),
            "/display/alias".into(),
            vec![("work".into(), "/registered/work".into())],
        )
        .unwrap();
        assert_eq!(alias.active(), ("work", "/display/alias"));
        assert_eq!(
            alias.rows().next(),
            Some(("work", "/registered/work", true))
        );
        let unregistered =
            WorkspaceRequest::new(String::new(), "/explicit/vault".into(), vec![]).unwrap();
        assert_eq!(unregistered.active(), ("", "/explicit/vault"));
        assert_eq!(unregistered.rows().count(), 0);
        let mut dialog = RequestDialog::new(alias);
        assert!(dialog.apply(1, Event::ExplicitConfirm));
        dialog.replace(unregistered).unwrap();
        assert_eq!(dialog.decision(), Decision::Pending);
        assert!(!dialog.apply(1, Event::ExplicitConfirm));
    }
    #[test]
    fn misleading_bidi_paths_are_rejected_and_debug_is_redacted() {
        for path in ["/fixture/\u{202e}work", "/fixture/\u{2066}work"] {
            assert!(WorkspaceRequest::new("".into(), path.into(), vec![]).is_err());
            assert!(
                WorkspaceRequest::new(
                    "work".into(),
                    "/fixture/work".into(),
                    vec![("work".into(), path.into())]
                )
                .is_err()
            );
        }
        assert!(!format!("{:?}", RequestDialog::new(request())).contains("/fixture"));
    }
    #[test]
    fn scope_is_explicit_and_active_is_distinguished() {
        let request = request();
        assert_eq!(
            request.rows().collect::<Vec<_>>(),
            vec![
                ("main", "/fixture/main", false),
                ("work", "/fixture/work", true)
            ]
        );
        assert!(
            WorkspaceRequest::new(
                "other".into(),
                "/fixture/other".into(),
                vec![("work".into(), "/fixture/work".into())]
            )
            .is_err()
        );
        assert!(
            WorkspaceRequest::new(
                "work".into(),
                "/fixture/work".into(),
                vec![
                    ("work".into(), "/one".into()),
                    ("work".into(), "/two".into())
                ]
            )
            .is_err()
        );
    }
}
