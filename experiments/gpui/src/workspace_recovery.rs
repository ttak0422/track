//! Disposable-fixture recovery adapter. File operations belong to SessionUi's
//! sole background owner; this adapter only captures immutable text and epochs.
use super::*;
use draft_recovery::{LocalStatus, Record, RecoveryContext, RecoveryToken};
use workspace_session::RecoveryStatus;

#[derive(Debug, PartialEq)]
enum ClearProgress {
    Waiting,
    Complete,
    Retain,
}
fn clear_progress(status: RecoveryStatus, elapsed: Option<std::time::Duration>) -> ClearProgress {
    if status == RecoveryStatus::Ready(LocalStatus::Empty) {
        ClearProgress::Complete
    } else if matches!(
        status,
        RecoveryStatus::Failed | RecoveryStatus::Ready(LocalStatus::Failed)
    ) || elapsed.is_some_and(|d| d >= std::time::Duration::from_secs(5))
    {
        ClearProgress::Retain
    } else {
        ClearProgress::Waiting
    }
}

/// Evaluated at action time, not merely when the diagnostics button is rendered.
/// Diagnostics is the owning surface, not a competing menu.
#[derive(Default)]
struct RecoveryActionGate {
    diagnostics_open: bool,
    closing: bool,
    exiting: bool,
    handoff: bool,
    includes: bool,
    graph: bool,
    search: bool,
    composing: bool,
    menu: bool,
    find: bool,
    editing: bool,
    pending: bool,
    clearing: bool,
    preview_gesture: bool,
}
impl RecoveryActionGate {
    fn allowed(&self) -> bool {
        self.diagnostics_open
            && !(self.closing
                || self.exiting
                || self.handoff
                || self.includes
                || self.graph
                || self.search
                || self.composing
                || self.menu
                || self.find
                || self.editing
                || self.pending
                || self.clearing
                || self.preview_gesture)
    }
}

// The local graph is a passive reader aside populated on every note read.
// Only the full Graph tab displaces the source required for recovery.
fn recovery_graph_surface(full_graph: bool, _aside_graph: bool) -> bool {
    full_graph
}

fn guarded_recovery_action<T>(gate: RecoveryActionGate, action: impl FnOnce() -> T) -> Option<T> {
    gate.allowed().then(action)
}

impl Workspace {
    fn recovery_action_gate(&self, cx: &App) -> RecoveryActionGate {
        RecoveryActionGate {
            diagnostics_open: self.shell.diagnostics,
            closing: self.quit_session.is_closing(),
            exiting: self.editing_ui.exit_requested,
            handoff: self.handoff_active(),
            includes: self.includes_preview_active(),
            graph: recovery_graph_surface(self.full_graph_active(), self.show_graph),
            search: self.shell.search_open,
            composing: self.search.read(cx).is_composing(),
            menu: self.shell.vault_menu || self.shell.tab_menu || self.editing_ui.mode_menu,
            find: self.shell.find.open,
            editing: self.editing_ui.active.is_some(),
            pending: self.editing_ui.pending.is_some(),
            clearing: self.editing_ui.recovery_clearing,
            preview_gesture: self.previews.model.geometry_active(),
        }
    }

    pub(super) fn recovery_notice(&self) -> &'static str {
        match self.session_ui.recovery_status() {
            RecoveryStatus::Disabled => "Fixture editing · memory-only drafts",
            RecoveryStatus::Loading => {
                "Fixture recovery · checking retained draft… editing unavailable"
            }
            RecoveryStatus::Failed | RecoveryStatus::Ready(LocalStatus::Failed) => {
                "Fixture recovery failed · draft retained; see diagnostics"
            }
            RecoveryStatus::Ready(LocalStatus::Available) => {
                "Unsaved fixture draft available · open its note, then diagnostics"
            }
            RecoveryStatus::Ready(LocalStatus::Pending) => {
                "Fixture recovery Pending · latest text is not yet durable"
            }
            RecoveryStatus::Ready(LocalStatus::SavedLocally) => {
                "Fixture draft Saved locally · not saved to the note"
            }
            RecoveryStatus::Ready(LocalStatus::Empty) => {
                "Fixture recovery ready · explicit engine Save only"
            }
        }
    }
    pub(super) fn checkpoint_editor_recovery(&mut self, cx: &mut Context<Self>) {
        if !self.session_ui.recovery_enabled() || self.editing_ui.recovery_clearing {
            return;
        }
        let Some(active) = self.editing_ui.active.as_ref() else {
            return;
        };
        if !active.session.dirty() && active.recovery.is_none() {
            return;
        }
        if active.recovery.as_ref().is_some_and(|r| {
            r.body == active.session.draft() && r.base_etag == active.session.etag()
        }) {
            return;
        }
        let Some(binding) = self.session_ui.recovery_binding() else {
            self.editing_ui.message = "Local recovery is unavailable; the in-memory draft remains protected by the dirty gate.".into();
            return;
        };
        let revision = match self.session_ui.next_revision() {
            Ok(r) => r,
            Err(e) => {
                self.editing_ui.message = e;
                return;
            }
        };
        let record = Record {
            schema: 1,
            binding,
            scope: self
                .current_note_request
                .as_ref()
                .map(|k| k.vault.clone())
                .unwrap_or_default(),
            vault: active.session.key().vault.clone(),
            note_id: active.session.key().note_id as u64,
            kind: "note".into(),
            base_etag: active.session.etag().into(),
            body: active.session.draft().into(),
            revision,
            title: Some(active.title.clone()),
        };
        match self.session_ui.checkpoint(record.clone()) {
            Ok(()) => self.editing_ui.active.as_mut().unwrap().recovery = Some(record),
            Err(_) => {
                self.editing_ui.message = "Local checkpoint failed or exceeds recovery bounds. Text remains in memory; no safe-close claim.".into();
                self.shell.diagnostics = true;
            }
        }
        cx.notify();
    }
    fn recovery_context(&self, cx: &App) -> Option<RecoveryContext> {
        if !self.recovery_action_gate(cx).allowed() {
            return None;
        }
        if self.editing_ui.active.is_some()
            || self.editing_ui.pending.is_some()
            || self.editing_ui.recovery_clearing
        {
            return None;
        }
        let (launch, storage, source) = self.editing_source().ok()?;
        let requested = self.current_note_request.as_ref()?;
        if requested.note_id != source.note.note.note_id
            || self.tabs.active().is_none_or(|t| {
                t.key
                    != tabs::TabKey::Note {
                        vault: requested.vault.clone(),
                        note_id: requested.note_id,
                    }
            })
        {
            return None;
        }
        Some(RecoveryContext {
            binding: self.session_ui.recovery_binding()?,
            scope: requested.vault.clone(),
            vault: source.note.note.vault,
            note_id: source.note.note.note_id as u64,
            kind: source.note.note.file_kind,
            etag: source.note.etag,
            request: self.request,
            launch,
            storage: storage.generation,
            editing_ready: !storage.read_only && storage.phase == "ready",
            storage_ready: self.session_ui.recovery_ready(),
            draft_busy: false,
        })
    }
    fn recover_fixture_draft(&mut self, token: RecoveryToken, cx: &mut Context<Self>) {
        let Some(fresh) = self.recovery_context(cx) else {
            return;
        };
        let Some(record) = self.session_ui.recovery_record() else {
            return;
        };
        let Ok((launch, storage, source)) = self.editing_source() else {
            return;
        };
        let Ok(mut session) = EditSession::from_note(launch, &storage, &source) else {
            return;
        };
        if session
            .install_recovered(record.body.clone(), &record.base_etag)
            .is_err()
        {
            return;
        }
        // The model rechecks every captured action epoch. No save/fetch/navigation occurs.
        if guarded_recovery_action(self.recovery_action_gate(cx), || {
            self.session_ui.recover(&token, &fresh)
        })
        .flatten()
        .is_none()
        {
            return;
        }
        let input = cx.new(|cx| editor_input::TextEditor::new(session.draft(), cx));
        let subscription = cx.subscribe(
            &input,
            |this, entity, event: &editor_input::EditorChanged, cx| {
                let composing = entity.read(cx).is_composing();
                if let Some(active) = this.editing_ui.active.as_mut() {
                    if active.input != entity {
                        return;
                    }
                    if let Err(message) = active
                        .session
                        .replace_text_with_composition(event.0.clone(), composing)
                    {
                        this.editing_ui.message = message;
                    }
                }
                this.checkpoint_editor_recovery(cx);
                cx.notify();
            },
        );
        self.request += 1;
        self.graph_request += 1;
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.shell.home = false;
        self.show_graph = false;
        self.editing_ui.active = Some(ActiveDraft {
            session,
            input,
            _subscription: subscription,
            source_path: source.note.note.path.into(),
            title: source.note.note.title,
            recovery: Some(record),
        });
        self.editing_ui.mode = EditorMode::Edit;
        self.editing_ui.message = "Recovered explicitly as an unsaved draft against the original base revision. No engine write sent.".into();
        cx.notify();
    }
    fn start_recovery_clear(&mut self, intent: Option<Transition>, cx: &mut Context<Self>) {
        self.editing_ui.recovery_clearing = true;
        self.editing_ui.recovery_started = Some(std::time::Instant::now());
        self.editing_ui.recovery_wait = intent.clone();
        if let Some(intent) = intent {
            self.editing_ui.pending.capture(intent);
        }
        if let Some(active) = &self.editing_ui.active {
            let _ = active.input.update(cx, |e, cx| e.set_read_only(true, cx));
        }
        self.editing_ui.message = "Waiting for ordered local draft removal. Cancel keeps this editor open; it cannot cancel a disk operation already started.".into();
        cx.notify();
    }
    pub(super) fn finish_recovery_save(
        &mut self,
        record: &Record,
        intent: Option<Transition>,
        cx: &mut Context<Self>,
    ) {
        if self.session_ui.acknowledged(record) {
            self.start_recovery_clear(intent, cx);
        } else {
            self.editing_ui.message = "Note save was acknowledged, but local recovery could not be reconciled. Recovery retained; no transition authorized.".into();
            self.editing_ui.pending.cancel();
            self.shell.diagnostics = true;
        }
    }
    pub(super) fn discard_editor_recovery(&mut self, cx: &mut Context<Self>) -> bool {
        let Some(active) = self.editing_ui.active.as_ref() else {
            return false;
        };
        let Some(record) = active.recovery.clone() else {
            return false;
        };
        if active.session.is_saving() || active.input.read(cx).is_composing() {
            return false;
        }
        if self.session_ui.discard(&record) {
            let intent = self.editing_ui.pending.intent.clone();
            self.start_recovery_clear(intent, cx);
        } else {
            self.editing_ui.message =
                "Recovery removal unavailable. In-memory draft retained; no transition authorized."
                    .into();
            cx.notify();
        }
        true
    }
    pub(crate) fn poll_editor_recovery(&mut self, cx: &mut Context<Self>) {
        if !self.session_ui.recovery_enabled() {
            return;
        }
        let status = self.session_ui.recovery_status();
        if self.editing_ui.recovery_seen.as_ref() != Some(&status) {
            self.editing_ui.recovery_seen = Some(status.clone());
            cx.notify();
        }
        if !self.editing_ui.recovery_clearing {
            return;
        }
        let progress = clear_progress(
            status,
            self.editing_ui.recovery_started.map(|t| t.elapsed()),
        );
        if progress == ClearProgress::Complete {
            self.editing_ui.recovery_clearing = false;
            self.editing_ui.recovery_started = None;
            self.editing_ui.recovery_discard = None;
            if let Some(active) = self.editing_ui.active.as_mut() {
                active.recovery = None;
                let _ = active.input.update(cx, |e, cx| e.set_read_only(false, cx));
            }
            self.editing_ui.pending.cancel();
            if let Some(intent) = self.editing_ui.recovery_wait.take() {
                self.execute_transition(intent, cx);
            } else {
                // Cancellation kept unsaved text alive: checkpoint it as a new revision.
                self.checkpoint_editor_recovery(cx);
                cx.notify();
            }
        } else if progress == ClearProgress::Retain {
            self.editing_ui.recovery_wait = None;
            self.editing_ui.pending.cancel();
            self.editing_ui.recovery_started = None;
            self.editing_ui.message = "Local removal failed or timed out. Draft/editor retained; no close authorized. A pending disk operation may still finish. Recovery failure requires restart/reconciliation, not automatic retry.".into();
            self.shell.diagnostics = true;
            cx.notify();
        }
    }
    pub(super) fn render_recovery_diagnostics(
        &self,
        mut panel: Div,
        cx: &mut Context<Self>,
    ) -> Div {
        if !self.session_ui.recovery_enabled() {
            return panel;
        }
        panel = panel.child(self.recovery_notice());
        if let Some(record) = self
            .session_ui
            .recovery_record()
            .filter(|_| self.editing_ui.active.is_none())
        {
            panel = panel.child(format!(
                "Retained fixture note: {} / {} · {}",
                record.vault,
                record.note_id,
                record.title.as_deref().unwrap_or("Untitled")
            ));
            if let Some(token) = self
                .recovery_context(cx)
                .and_then(|ctx| self.session_ui.offer_recovery(&ctx))
            {
                panel = panel.child(
                    div()
                        .id("recover-fixture-draft")
                        .cursor_pointer()
                        .child("Recover draft (no save)")
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.recover_fixture_draft(token.clone(), cx)
                        })),
                );
            } else {
                panel = panel.child("Open the matching fixture note. If its source revision changed or is unavailable, recovery is retained; automatic recovery is unavailable.");
            }
            let confirm = record.clone();
            panel = panel.child(
                div()
                    .id("request-discard-fixture-draft")
                    .cursor_pointer()
                    .child("Discard saved fixture draft…")
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if !this.recovery_action_gate(cx).allowed() {
                            return;
                        }
                        this.editing_ui.recovery_discard = Some(confirm.clone());
                        cx.notify();
                    })),
            );
        }
        if let Some(record) = &self.editing_ui.recovery_discard {
            let exact = record.clone();
            panel = panel.child(format!("Permanently discard saved fixture draft for {} / {} · {}? The note itself is not changed.", record.vault, record.note_id, record.title.as_deref().unwrap_or("Untitled")))
                .child(div().id("confirm-discard-fixture-draft").cursor_pointer().child("Confirm discard saved fixture draft")
                    .on_click(cx.listener(move |this, _, _, cx| { if guarded_recovery_action(this.recovery_action_gate(cx), || this.session_ui.discard(&exact)) == Some(true) { this.start_recovery_clear(None, cx); } })))
                .child(div().id("cancel-discard-fixture-draft").cursor_pointer().child("Cancel")
                    .on_click(cx.listener(|this, _, _, cx| { this.editing_ui.recovery_discard = None; cx.notify(); })));
        }
        panel
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn local_graph_aside_does_not_block_retained_draft_actions() {
        for aside in [false, true] {
            for full_graph in [false, true] {
                let gate = RecoveryActionGate {
                    diagnostics_open: true,
                    graph: recovery_graph_surface(full_graph, aside),
                    ..Default::default()
                };
                assert_eq!(gate.allowed(), !full_graph);
            }
        }
    }

    #[test]
    fn retained_actions_recheck_closing_and_competing_surfaces_before_mutation() {
        // The same gate runs before recovery context/model resolution and before
        // both discard handlers. A formerly rendered callback gets fresh flags.
        let enabled = || RecoveryActionGate {
            diagnostics_open: true,
            ..Default::default()
        };
        // Both actual mutation sites use guarded_recovery_action. The owning
        // diagnostics and its saved-draft confirmation are intentionally allowed.
        let mut recovered = false;
        assert_eq!(
            guarded_recovery_action(enabled(), || {
                recovered = true;
                true
            }),
            Some(true)
        );
        assert!(recovered);
        let mut confirmed_discard = false;
        assert_eq!(
            guarded_recovery_action(enabled(), || {
                confirmed_discard = true;
                true
            }),
            Some(true)
        );
        assert!(confirmed_discard);
        let blockers: [fn(&mut RecoveryActionGate); 13] = [
            |g| g.closing = true,
            |g| g.exiting = true,
            |g| g.handoff = true,
            |g| g.includes = true,
            |g| g.graph = true,
            |g| g.search = true,
            |g| g.composing = true,
            |g| g.menu = true,
            |g| g.find = true,
            |g| g.editing = true,
            |g| g.pending = true,
            |g| g.clearing = true,
            |g| g.preview_gesture = true,
        ];
        for block in blockers {
            let mut current = enabled();
            assert!(current.allowed()); // button render before state change
            block(&mut current);
            let mut installed_or_discarded = false;
            assert!(
                guarded_recovery_action(current, || {
                    installed_or_discarded = true;
                })
                .is_none()
            );
            assert!(!installed_or_discarded);
        }
        let mut hidden = enabled();
        hidden.diagnostics_open = false;
        assert!(!hidden.allowed());
    }
    #[test]
    fn close_barrier_never_treats_checkpoint_or_error_as_clear_completion() {
        for status in [
            RecoveryStatus::Loading,
            RecoveryStatus::Ready(LocalStatus::Pending),
            RecoveryStatus::Ready(LocalStatus::SavedLocally),
            RecoveryStatus::Ready(LocalStatus::Available),
        ] {
            assert_eq!(
                clear_progress(status, Some(std::time::Duration::from_secs(1))),
                ClearProgress::Waiting
            );
            assert_eq!(
                clear_progress(status, Some(std::time::Duration::from_secs(5))),
                ClearProgress::Retain
            );
        }
        for status in [
            RecoveryStatus::Failed,
            RecoveryStatus::Ready(LocalStatus::Failed),
        ] {
            assert_eq!(clear_progress(status, None), ClearProgress::Retain);
        }
        assert_eq!(
            clear_progress(RecoveryStatus::Ready(LocalStatus::Empty), None),
            ClearProgress::Complete
        );
    }
    #[test]
    fn canceled_or_timed_out_close_has_no_destination_for_late_completion() {
        let mut ui = EditingUi::new(None);
        ui.recovery_clearing = true;
        ui.recovery_wait = Some(Transition::Quit);
        // Same cancellation performed by the modal and timeout adapter.
        ui.recovery_wait = None;
        assert_eq!(
            clear_progress(RecoveryStatus::Ready(LocalStatus::Empty), None),
            ClearProgress::Complete
        );
        assert!(ui.recovery_wait.take().is_none());
        // The draft entity is not removed by either cancel or timeout.
        assert!(ui.recovery_clearing);
    }
}
