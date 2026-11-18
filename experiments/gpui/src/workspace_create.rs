//! Bounded fixture-only title dialog. The Workspace owns all pending mutations;
//! ordinary navigation/quit cannot dismiss them, and no worker retries a POST.
use super::*;
use crate::note_creation::{Flow, Phase};
use gpui::{Focusable, MouseButton};

gpui::actions!(track_create, [SubmitCreate, CancelCreate]);

#[derive(Default)]
pub(super) struct CreationUi {
    pub open: bool,
    pub flow: Flow,
    pub message: String,
    input: Option<Entity<search_input::SearchInput>>,
    receipt: Option<live::CreateAck>,
    reading: bool,
    loading: bool,
    checked_launch: Option<u64>,
    durable: Option<session_store::CreationRecord>,
    journal_error: Option<String>,
    retained_acknowledged: bool,
}

fn retention_allowed(
    phase: Phase,
    busy: bool,
    journal_verified: bool,
    exact_pending: bool,
) -> bool {
    phase == Phase::Uncertain && !busy && journal_verified && exact_pending
}

fn recovery_allows_creation(status: workspace_session::RecoveryStatus) -> bool {
    matches!(
        status,
        workspace_session::RecoveryStatus::Disabled
            | workspace_session::RecoveryStatus::Ready(draft_recovery::LocalStatus::Empty)
    )
}

impl Workspace {
    fn creation_capable(&self) -> bool {
        self.editing_ui.enabled()
            && self
                .live
                .as_ref()
                .is_some_and(|s| s.client().creation_available())
    }
    pub(crate) fn creation_open(&self) -> bool {
        self.editing_ui.creation.open
    }
    pub(crate) fn creation_available(&self) -> bool {
        self.creation_capable()
            && !self.editing_ui.creation.loading
            && self.editing_ui.creation.checked_launch
                == self.live.as_ref().map(|s| s.client().launch_generation)
            && self.editing_ui.creation.journal_error.is_none()
    }
    pub(crate) fn creation_retention_needed(&self) -> bool {
        let ui = &self.editing_ui.creation;
        !ui.retained_acknowledged
            && ui
                .durable
                .as_ref()
                .is_some_and(|r| r.outcome == session_store::CreationOutcome::Pending)
    }
    pub(crate) fn creation_journal_notice(&self) -> Option<String> {
        let ui = &self.editing_ui.creation;
        if let Some(error) = &ui.journal_error {
            Some(format!(
                "Creation disabled: receipt storage could not be verified. Evidence was not erased. {error}"
            ))
        } else if ui.loading {
            Some("Checking durable creation receipt before enabling New…".into())
        } else if ui
            .durable
            .as_ref()
            .is_some_and(|r| r.outcome == session_store::CreationOutcome::Pending)
        {
            Some("Unresolved creation receipt retained on disk. Note may or may not exist; New reopens the receipt, never retries creation.".into())
        } else if let Some(session_store::CreationRecord {
            outcome: session_store::CreationOutcome::Created { note_id, title },
            ..
        }) = &ui.durable
        {
            Some(format!(
                "Durable creation receipt: #{note_id} · {title}. Identity retained; no automatic source read or navigation. A new submission is a separate explicit attempt."
            ))
        } else {
            None
        }
    }
    pub(crate) fn sync_creation_journal(&mut self, cx: &mut Context<Self>) {
        if !self.creation_capable() || self.editing_ui.creation.loading {
            return;
        }
        let Some(session) = self.live.clone() else {
            return;
        };
        let launch = session.client().launch_generation;
        if self.editing_ui.creation.checked_launch == Some(launch) {
            return;
        }
        self.editing_ui.creation.loading = true;
        let job = cx
            .background_executor()
            .spawn(async move { session.client().creation_snapshot() });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                this.editing_ui.creation.loading = false;
                if this.live.as_ref().map(|s| s.client().launch_generation) != Some(launch) {
                    return;
                }
                this.editing_ui.creation.checked_launch = Some(launch);
                match result {
                    Ok(record) => {
                        this.editing_ui.creation.durable = record;
                        this.editing_ui.creation.journal_error = None;
                        if this.creation_retention_needed() {
                            this.show_retained_creation(cx);
                        }
                    }
                    Err(error) => this.editing_ui.creation.journal_error = Some(error),
                }
                cx.notify();
            });
        })
        .detach();
    }
    pub(crate) fn show_retained_creation(&mut self, cx: &mut Context<Self>) {
        if self
            .editing_ui
            .active
            .as_ref()
            .is_some_and(|a| a.input.read(cx).is_composing())
            || self.search.read(cx).is_composing()
            || self.shell.calendar_zone.is_composing(cx)
            || self.handoff_active()
            || self.includes_preview_active()
            || self.shell.search_open
            || self.shell.find.open
            || self.shell.vault_menu
            || self.shell.tab_menu
            || self.shell.diagnostics
            || self.editing_ui.mode_menu
            || self.editing_ui.pending.is_some()
            || self.editing_ui.recovery_clearing
            || self.previews.model.geometry_active()
            || self.previews.model.transient.is_some()
        {
            self.status =
                "Finish composition before reviewing the retained creation receipt.".into();
            cx.notify();
            return;
        }
        let Some(record) = self
            .editing_ui
            .creation
            .durable
            .clone()
            .filter(|r| r.outcome == session_store::CreationOutcome::Pending)
        else {
            return;
        };
        let ui = &mut self.editing_ui.creation;
        ui.flow.phase = Phase::Uncertain;
        ui.flow.title = record.title;
        ui.open = true;
        ui.message = "A durable intent survives from a creation attempt. It may or may not have been sent, and a note may exist. No retry, success inference or rollback. Keeping this exact receipt allows closing the dialog and then normal guarded quit; the evidence stays on disk and creation remains disabled.".into();
        cx.notify();
    }

    // This gate is repeated at opening, submission and read-only result opening.
    // Dirty editor admission belongs to the existing Save/Discard/Cancel owner.
    fn creation_gate(&self, allow_draft: bool, cx: &App) -> Result<(), &'static str> {
        if !self.creation_available()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.editing_ui.exit_requested
        {
            return Err("Creation requires the verified disposable fixture's owned live server.");
        }
        if !self.shell.working_scope.is_empty()
            || self
                .current_note_request
                .as_ref()
                .is_some_and(|k| !k.vault.is_empty())
        {
            return Err(
                "Creation is restricted to the launch fixture. Leave the named-vault context first.",
            );
        }
        if self.handoff_active()
            || self.includes_preview_active()
            || self.shell.search_open
            || self.search.read(cx).is_composing()
            || self.shell.calendar_zone.is_composing(cx)
            || self.shell.find.open
            || self.shell.vault_menu
            || self.shell.tab_menu
            || self.shell.diagnostics
            || self.editing_ui.mode_menu
            || self.editing_ui.inspecting
            || self.editing_ui.pending.is_some()
            || self.editing_ui.recovery_clearing
            || self.previews.model.geometry_active()
            || self.previews.model.transient.is_some()
        {
            return Err(
                "Finish composition and close competing input, menu or preview owners first.",
            );
        }
        if self
            .editing_ui
            .active
            .as_ref()
            .is_some_and(|a| a.input.read(cx).is_composing())
        {
            return Err("Finish or cancel editor composition before creating a note.");
        }
        if !allow_draft && self.editing_ui.active.is_some() {
            return Err("Resolve the current editor before creating or opening a note.");
        }
        if (!allow_draft || self.editing_ui.active.is_none())
            && !recovery_allows_creation(self.session_ui.recovery_status())
        {
            return Err(
                "A recovery draft is retained or recovery is not ready. Resolve it before creation.",
            );
        }
        Ok(())
    }

    pub(crate) fn request_create_note(&mut self, cx: &mut Context<Self>) {
        if self.creation_open() {
            return;
        }
        if self
            .editing_ui
            .creation
            .durable
            .as_ref()
            .is_some_and(|r| r.outcome == session_store::CreationOutcome::Pending)
        {
            self.show_retained_creation(cx);
            return;
        }
        if let Err(message) = self.creation_gate(true, cx) {
            self.editing_ui.message = message.into();
            self.status = message.into();
            cx.notify();
            return;
        }
        if !self.guard_transition(Transition::CreateNote, cx) {
            self.open_creation_form(cx);
        }
    }
    pub(super) fn open_creation_form(&mut self, cx: &mut Context<Self>) {
        if let Err(message) = self.creation_gate(false, cx) {
            self.editing_ui.message = message.into();
            self.status = message.into();
            cx.notify();
            return;
        }
        cx.bind_keys([
            KeyBinding::new(
                "enter",
                SubmitCreate,
                Some("TrackCreateTitle && !composing"),
            ),
            KeyBinding::new(
                "escape",
                CancelCreate,
                Some("TrackCreateTitle && !composing"),
            ),
        ]);
        let input = cx.new(|cx| {
            search_input::SearchInput::with_purpose(search_input::InputPurpose::CreateTitle, cx)
        });
        let ui = &mut self.editing_ui.creation;
        ui.open = true;
        ui.flow = Flow::default();
        ui.input = Some(input);
        ui.receipt = None;
        ui.message = "Title only. A durable intent is saved before sending; the fixture's default template supplies the body.".into();
        cx.notify();
    }
    fn dismiss_creation(&mut self, cx: &mut Context<Self>) {
        let ui = &self.editing_ui.creation;
        if ui.reading
            || !ui.flow.can_dismiss()
            || ui.input.as_ref().is_some_and(|i| i.read(cx).is_composing())
        {
            return;
        }
        self.editing_ui.creation.open = false;
        // Acknowledged receipt stays retained until a fresh explicit New action.
        cx.notify();
    }
    fn submit_creation(&mut self, cx: &mut Context<Self>) {
        if !self.creation_open() || self.editing_ui.creation.flow.phase != Phase::Editing {
            return;
        }
        if let Err(message) = self.creation_gate(false, cx) {
            self.editing_ui.creation.message = message.into();
            cx.notify();
            return;
        }
        let Some(input) = self.editing_ui.creation.input.as_ref() else {
            return;
        };
        if input.read(cx).is_composing() {
            return;
        }
        let title = input.read(cx).query().to_owned();
        let Some(session) = self.live.clone() else {
            return;
        };
        let launch = session.client().launch_generation;
        let Some(ticket) = self.editing_ui.creation.flow.begin(&title, launch) else {
            self.editing_ui.creation.message = "Enter a nonblank note title.".into();
            cx.notify();
            return;
        };
        self.editing_ui.creation.message =
            "Creating one note in the launch fixture… Do not retry; cancellation is not rollback."
                .into();
        let title = self.editing_ui.creation.flow.title.clone();
        // Pending is committed synchronously before spawning. This worker retains
        // the Session through response receipt even if the render task disappears.
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let result = client.create_note(&title, "", launch);
            let durable = client.creation_snapshot();
            (result, durable)
        });
        cx.spawn(async move |this, cx| {
            let (result, durable) = job.await;
            let _ = this.update(cx, |this, cx| {
                let phase = if (result.is_ok() && durable.is_err()) || durable.as_ref().ok().and_then(|r| r.as_ref()).is_some_and(|r| r.outcome == session_store::CreationOutcome::Pending) {
                    Phase::Uncertain
                } else { match &result {
                    Ok(_) => Phase::Created,
                    Err(live::CreateError::Uncertain(_)) => Phase::Uncertain,
                    Err(_) => Phase::Editing,
                }};
                let ui = &mut this.editing_ui.creation;
                if !ui.flow.finish(ticket, phase) { return; }
                ui.retained_acknowledged = false;
                match durable {
                    Ok(record) => { ui.durable = record; ui.journal_error = None; }
                    Err(error) => ui.journal_error = Some(error),
                }
                match result {
                    Ok(ack) => {
                        ui.message = if phase == Phase::Created {
                            format!("Created note #{}: {}. Receipt retained durably. Open verifies the actual default-template content first; no editor activates automatically.", ack.note_id, ack.title)
                        } else {
                            format!("Server acknowledged note #{}: {}, but durable evidence can no longer be verified. No retry, no rollback claim; exit remains blocked.", ack.note_id, ack.title)
                        };
                        ui.receipt = Some(ack);
                    }
                    Err(error) => ui.message = format!("{error}{}", if phase == Phase::Uncertain {
                        " The intent is retained. Once no request is in flight, Keep receipt verifies durable evidence before allowing close/quit. Creation remains disabled; no retry or rollback."
                    } else { " Title retained; no automatic retry." }),
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }
    fn retain_creation_and_close(&mut self, cx: &mut Context<Self>) {
        let ui = &self.editing_ui.creation;
        if !ui.open
            || !retention_allowed(
                ui.flow.phase,
                ui.reading || ui.loading,
                ui.journal_error.is_none(),
                ui.durable
                    .as_ref()
                    .is_some_and(|r| r.outcome == session_store::CreationOutcome::Pending),
            )
        {
            return;
        }
        let Some(exact) = ui
            .durable
            .clone()
            .filter(|r| r.outcome == session_store::CreationOutcome::Pending)
        else {
            return;
        };
        let Some(session) = self.live.clone() else {
            return;
        };
        let launch = session.client().launch_generation;
        self.editing_ui.creation.reading = true;
        self.editing_ui.creation.message =
            "Verifying and syncing the exact unresolved receipt. No note request is sent…".into();
        let job = cx.background_executor().spawn(async move {
            let result = session.client().retain_uncertain_creation(&exact);
            (exact, result)
        });
        cx.spawn(async move |this, cx| {
            let (exact, result) = job.await;
            let _ = this.update(cx, |this, cx| {
                let same_launch = this.live.as_ref().map(|s| s.client().launch_generation) == Some(launch);
                let ui = &mut this.editing_ui.creation;
                ui.reading = false;
                if !same_launch || ui.durable.as_ref() != Some(&exact) || ui.flow.phase != Phase::Uncertain { return; }
                match result {
                    Ok(()) => {
                        ui.retained_acknowledged = true;
                        ui.open = false;
                        ui.message = "Unresolved receipt kept durably. No success or rollback claimed; no retry authorized.".into();
                    }
                    Err(error) => {
                        ui.journal_error = Some(error.clone());
                        ui.message = format!("Cannot verify durable receipt; staying here. Evidence was not erased. {error}");
                    }
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }
    fn open_created_note(&mut self, cx: &mut Context<Self>) {
        if !self.creation_open()
            || self.editing_ui.creation.reading
            || self.editing_ui.creation.flow.phase != Phase::Created
        {
            return;
        }
        if let Err(message) = self.creation_gate(false, cx) {
            self.editing_ui.creation.message = message.into();
            cx.notify();
            return;
        }
        let Some(ack) = self.editing_ui.creation.receipt.clone() else {
            return;
        };
        let Some(session) = self.live.clone() else {
            return;
        };
        if session.client().launch_generation != ack.launch_generation {
            self.editing_ui.creation.message = "Created identity retained, but the original owned launch is unavailable. No new POST will be sent.".into();
            cx.notify();
            return;
        }
        self.editing_ui.creation.reading = true;
        self.editing_ui.creation.message =
            "Created; verifying source before opening (read-only)…".into();
        let request = self.request;
        let job = cx.background_executor().spawn(async move {
            let result = session.client().verify_created(&ack);
            (ack, result)
        });
        cx.spawn(async move |this, cx| {
            let (ack, result) = job.await;
            let _ = this.update(cx, |this, cx| {
                this.editing_ui.creation.reading = false;
                if !this.creation_open() || this.request != request
                    || this.live.as_ref().map(|s| s.client().launch_generation) != Some(ack.launch_generation)
                    || this.creation_gate(false, cx).is_err() {
                    this.editing_ui.creation.message = format!("Created #{}; context changed before opening. Identity retained. Open again only when safe; no POST retry.", ack.note_id);
                    cx.notify();
                    return;
                }
                match result {
                    Ok((_response, storage)) => {
                        this.observe_storage(&storage, cx);
                        this.editing_ui.creation.open = false;
                        this.open_live_key(live::NoteKey { vault: ack.vault, note_id: ack.note_id }, cx);
                    }
                    Err(error) => {
                        this.editing_ui.creation.message = format!("Created #{} but unable to open/verify: {error}. Open retries only the read, never creation.", ack.note_id);
                    }
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }
    pub(crate) fn render_creation(&self, dark: bool, cx: &mut Context<Self>) -> Div {
        let ui = &self.editing_ui.creation;
        let paper = if dark { 0x191c1e } else { 0xffffff };
        let ink = if dark { 0xe9e9e4 } else { 0x1a1a18 };
        let mut dialog = div()
            .w(px(490.))
            .p_5()
            .rounded_lg()
            .bg(rgb(paper))
            .text_color(rgb(ink))
            .flex()
            .flex_col()
            .gap_3()
            .on_action(cx.listener(|this, _: &SubmitCreate, _, cx| this.submit_creation(cx)))
            .on_action(cx.listener(|this, _: &CancelCreate, _, cx| this.dismiss_creation(cx)))
            .child("Create note · disposable launch fixture only")
            .child(format!(
                "Destination: {}",
                self.editing_ui
                    .fixture_root
                    .as_ref()
                    .map(|p| p.display().to_string())
                    .unwrap_or_default()
            ))
            .child("Uses this fixture's default template. No body override or automatic save.");
        if ui.flow.phase == Phase::Editing {
            if let Some(input) = &ui.input {
                input.update(cx, |input, _| input.set_dark(dark));
                let focus = input.focus_handle(cx);
                dialog = dialog.child(
                    div()
                        .id("create-title-input")
                        .p_2()
                        .border_1()
                        .on_click(move |_, window, _| {
                            window.focus(&focus);
                        })
                        .child(input.clone()),
                );
            }
            dialog = dialog.child(
                div()
                    .id("create-submit")
                    .p_2()
                    .cursor_pointer()
                    .child("Create note")
                    .on_click(cx.listener(|this, _, _, cx| this.submit_creation(cx))),
            );
        } else {
            dialog = dialog.child(format!("Title: {}", ui.flow.title));
        }
        dialog = dialog.child(ui.message.clone());
        if ui.flow.phase == Phase::Created && !ui.reading {
            dialog = dialog.child(
                div()
                    .id("create-open-result")
                    .p_2()
                    .cursor_pointer()
                    .child("Open created note (verify source)")
                    .on_click(cx.listener(|this, _, _, cx| this.open_created_note(cx))),
            );
        }
        if retention_allowed(
            ui.flow.phase,
            ui.reading || ui.loading,
            ui.journal_error.is_none(),
            ui.durable
                .as_ref()
                .is_some_and(|r| r.outcome == session_store::CreationOutcome::Pending),
        ) {
            dialog = dialog.child(
                div()
                    .id("create-retain-unresolved")
                    .p_2()
                    .cursor_pointer()
                    .child("Keep unresolved receipt and close (no retry)")
                    .on_click(cx.listener(|this, _, _, cx| this.retain_creation_and_close(cx))),
            );
        }
        if ui.flow.can_dismiss() && !ui.reading {
            dialog = dialog.child(
                div()
                    .id("create-cancel")
                    .p_2()
                    .cursor_pointer()
                    .child(if ui.flow.phase == Phase::Created {
                        "Close receipt"
                    } else {
                        "Cancel"
                    })
                    .on_click(cx.listener(|this, _, _, cx| this.dismiss_creation(cx))),
            );
        }
        div()
            .absolute()
            .inset_0()
            .bg(gpui::rgba(0x000000aa))
            .flex()
            .items_center()
            .justify_center()
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_up(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .child(dialog)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn retention_never_releases_pending_missing_or_unverified_evidence() {
        assert!(retention_allowed(Phase::Uncertain, false, true, true));
        for phase in [Phase::Editing, Phase::Pending, Phase::Created] {
            assert!(!retention_allowed(phase, false, true, true));
        }
        assert!(!retention_allowed(Phase::Uncertain, true, true, true));
        assert!(!retention_allowed(Phase::Uncertain, false, false, true));
        assert!(!retention_allowed(Phase::Uncertain, false, true, false));
    }
    #[test]
    fn retained_loading_or_failed_recovery_never_admits_creation() {
        use draft_recovery::LocalStatus;
        use workspace_session::RecoveryStatus;
        assert!(recovery_allows_creation(RecoveryStatus::Disabled));
        assert!(recovery_allows_creation(RecoveryStatus::Ready(
            LocalStatus::Empty
        )));
        for status in [
            RecoveryStatus::Loading,
            RecoveryStatus::Failed,
            RecoveryStatus::Ready(LocalStatus::Available),
            RecoveryStatus::Ready(LocalStatus::Pending),
            RecoveryStatus::Ready(LocalStatus::SavedLocally),
            RecoveryStatus::Ready(LocalStatus::Failed),
        ] {
            assert!(!recovery_allows_creation(status));
        }
    }
}
