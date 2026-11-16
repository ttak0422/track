//! Memory-only opt-in and one-attempt-per-accepted-owner lifecycle.
use super::*;
#[derive(Clone, Debug, PartialEq, Eq)]
pub(super) struct Owner {
    identity: Identity,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
    tasks_invalidated: bool,
    body: std::sync::Arc<str>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(super) struct Ticket {
    generation: u64,
    owner: Owner,
}
impl Ticket {
    pub(super) fn same_owner(&self, other: &Self) -> bool {
        self.owner == other.owner
    }
}
#[derive(Clone)]
struct Control {
    owner: Owner,
    generation: u64,
    serial: u64,
}
impl Control {
    fn current(&self, mode: &Mode, owner: &Owner, serial: u64) -> bool {
        self.generation == mode.generation && &self.owner == owner && self.serial == serial
    }
}
#[derive(Default)]
pub(crate) struct Mode {
    pub enabled: bool,
    generation: u64,
    attempted: Option<Owner>,
    body_cache: std::cell::RefCell<Option<std::sync::Arc<str>>>,
}
impl Mode {
    pub(crate) fn generation(&self) -> u64 {
        self.generation
    }

    fn body_snapshot(&self, body: &str) -> std::sync::Arc<str> {
        let mut cached = self.body_cache.borrow_mut();
        if let Some(value) = cached.as_ref().filter(|value| value.as_ref() == body) {
            return value.clone();
        }
        let value: std::sync::Arc<str> = std::sync::Arc::from(body);
        *cached = Some(value.clone());
        value
    }

    pub fn enable(&mut self) {
        self.enabled = true;
        self.generation = self.generation.wrapping_add(1);
        self.attempted = None;
    }
    pub fn disable(&mut self) {
        self.enabled = false;
        self.generation = self.generation.wrapping_add(1);
        self.attempted = None;
    }
    pub(super) fn retire(&mut self) {
        self.generation = self.generation.wrapping_add(1);
        self.attempted = None;
    }
    fn claim(&mut self, owner: Owner) -> Option<Ticket> {
        if !self.enabled || self.attempted.as_ref() == Some(&owner) {
            return None;
        }
        self.generation = self.generation.wrapping_add(1);
        self.attempted = Some(owner.clone());
        Some(Ticket {
            generation: self.generation,
            owner,
        })
    }
    fn retry(&mut self, owner: Owner) -> Option<Ticket> {
        if !self.enabled {
            return None;
        }
        self.generation = self.generation.wrapping_add(1);
        self.attempted = None;
        self.claim(owner)
    }
    pub(super) fn accepts(&self, ticket: &Ticket, current: &Owner) -> bool {
        self.enabled
            && self.generation == ticket.generation
            && self.attempted.as_ref() == Some(current)
            && &ticket.owner == current
    }
}
fn history_route_supported(route: Option<&tabs::TabKey>) -> bool {
    matches!(
        route,
        None | Some(tabs::TabKey::Home | tabs::TabKey::Note { .. })
    )
}
impl Workspace {
    fn reading_owner(&self) -> Option<Owner> {
        self.reading_owner_for(self.includes_identity()?)
    }
    fn reading_owner_for(&self, identity: Identity) -> Option<Owner> {
        if self.shell.accepted_title_request != Some(self.request) {
            return None;
        }
        let note = self.current_note.as_ref()?;
        let body = self.shell.rendered_reading.body_snapshot(&note.body);
        Some(Owner {
            body,
            identity,
            scope: self.working_vault_scope().to_owned(),
            scope_revision: self.shell.scope_revision,
            registry: self.follow_registry()?,
            tasks_invalidated: note
                .note
                .additional
                .get("tasks")
                .is_some_and(serde_json::Value::is_null),
        })
    }
    pub(super) fn reading_ticket_current(&self, ticket: &Ticket) -> bool {
        self.reading_owner()
            .is_some_and(|o| self.shell.rendered_reading.accepts(ticket, &o))
    }
    pub(super) fn reading_ticket_lifetime(&self, ticket: &Ticket) -> bool {
        // Retention is not action authority: editor/recovery owners hide the view
        // and reading_owner still rejects them. Exact source changes discard it.
        self.includes_source_identity()
            .and_then(|id| self.reading_owner_for(id))
            .is_some_and(|owner| self.shell.rendered_reading.accepts(ticket, &owner))
    }
    fn reading_control_current(&self, control: &Control) -> bool {
        let Some(owner) = self.reading_owner() else {
            return false;
        };
        self.shell.includes.as_ref().is_some_and(|s| {
            s.identity == owner.identity
                && control.current(&self.shell.rendered_reading, &owner, s.serial)
        })
    }
    pub(crate) fn retire_reading_history_preview(&mut self) {
        if self.shell.rendered_reading.enabled && self.includes_preview_active() {
            self.shell.rendered_return_focus = self
                .shell
                .includes
                .as_ref()
                .and_then(|s| s.preview_focus.clone());
            self.shell.includes = None;
        }
    }
    fn reading_source_ready(&self, window: &Window, cx: &Context<Self>) -> bool {
        window.is_window_active()
            && self.find_available()
            && self.pin_keyboard_available(cx)
            && !self.shell.find.open
            && !self.reader_selection_dragging()
            && !self.inline_includes_active()
            && !self.query_inline_active()
            && !self.search.read(cx).is_composing()
    }
    pub(crate) fn sync_rendered_reading(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .shell
            .includes
            .as_ref()
            .and_then(|s| s.reading_ticket.as_ref())
            .is_some_and(|ticket| !self.reading_ticket_lifetime(ticket))
        {
            self.shell.includes = None;
            self.shell.rendered_reading.retire();
        }
        if self.editing_ui.active()
            || self.editing_ui.has_pending()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
        {
            if let Some(state) = self
                .shell
                .includes
                .as_mut()
                .filter(|s| s.reading_ticket.is_some())
            {
                state.preview_open = false;
                state.find = Default::default();
                state.selection = Default::default();
            }
            return;
        }
        if let Some(old_focus) = self.shell.rendered_return_focus.take() {
            if window.is_window_active() && old_focus.is_focused(window) {
                if let Some(focus) = &self.shell.reader_focus {
                    window.focus(focus);
                }
            }
        }
        if !self.shell.rendered_reading.enabled || !self.reading_source_ready(window, cx) {
            return;
        }
        let Some(owner) = self.reading_owner() else {
            return;
        };
        // Claim precedes starting the worker: busy/error does not become a render retry loop.
        if let Some(ticket) = self.shell.rendered_reading.claim(owner.clone()) {
            if self
                .shell
                .includes
                .as_ref()
                .is_none_or(|s| s.identity != owner.identity)
            {
                self.shell.includes = Some(IncludesState::new(owner.identity.clone()));
            }
            self.shell.includes.as_mut().unwrap().open = true;
            self.includes_fetch_owned(owner.identity.clone(), Some(ticket), cx);
        }
        let Some(state) = self.shell.includes.as_ref() else {
            return;
        };
        if state.identity != owner.identity
            || !self.includes_body_current(state)
            || !matches!(state.result, Some(Ok(_)))
        {
            return;
        }
        // Only a mode-owned accepted result may auto-open. Source/off-on retires old completions.
        if !state
            .reading_ticket
            .as_ref()
            .is_some_and(|t| self.reading_ticket_current(t))
        {
            return;
        }
        let focus_ok = window.focused(cx).is_none()
            || self
                .shell
                .reader_focus
                .as_ref()
                .is_some_and(|f| f.is_focused(window));
        if !focus_ok {
            return;
        }
        let serial = state.serial;
        self.open_includes_preview(&owner.identity, serial, window, cx);
    }
    fn reading_enable(&mut self, control: &Control, window: &mut Window, cx: &mut Context<Self>) {
        if !self.reading_source_ready(window, cx) || !self.reading_control_current(control) {
            return;
        }
        self.stop_editor_follow();
        self.shell.rendered_reading.enable();
        self.sync_rendered_reading(window, cx);
        cx.notify();
    }
    pub(super) fn close_rendered_user(
        &mut self,
        identity: &Identity,
        serial: u64,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || self.rendered_find_composing(cx)
            || self.includes_identity().as_ref() != Some(identity)
            || self
                .shell
                .includes
                .as_ref()
                .is_none_or(|s| !s.accepts(identity, serial) || !s.preview_open)
        {
            return;
        }
        if self.shell.rendered_reading.enabled {
            self.shell.rendered_reading.disable();
        }
        self.close_includes_preview(identity, serial, window, cx);
    }
    fn reading_retry(&mut self, control: &Control, window: &mut Window, cx: &mut Context<Self>) {
        let expected = &control.owner.identity;
        if !self.reading_control_current(control) || !window.is_window_active() {
            return;
        }
        let gate = if self.includes_preview_active() {
            self.query_control_gate(expected, cx).allowed()
        } else {
            self.reading_source_ready(window, cx)
        };
        if !gate {
            return;
        }
        let Some(owner) = self.reading_owner().filter(|o| &o.identity == expected) else {
            return;
        };
        let Some(ticket) = self.shell.rendered_reading.retry(owner) else {
            return;
        };
        self.includes_fetch_owned(expected.clone(), Some(ticket), cx);
        cx.notify();
    }
    fn reading_history(
        &mut self,
        control: &Control,
        serial: u64,
        forward: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let expected = &control.owner.identity;
        if !self.reading_control_current(control)
            || !window.is_window_active()
            || !self.shell.rendered_reading.enabled
            || !self.query_control_gate(expected, cx).allowed()
            || self
                .shell
                .includes
                .as_ref()
                .is_none_or(|s| !s.accepts(expected, serial))
        {
            return;
        }
        let Some(ticket) = self.history.peek(forward) else {
            return;
        };
        if !history_route_supported(ticket.route.as_ref()) {
            return;
        }
        if let Some(tabs::TabKey::Note { vault, .. }) = &ticket.route {
            if !self.session_ui.permits(vault) {
                return;
            }
        }
        self.replay_history(ticket, cx);
    }
    pub(super) fn reading_controls(
        &self,
        identity: &Identity,
        serial: u64,
        preview: bool,
        cx: &mut Context<Self>,
    ) -> Div {
        let mut row = div().flex().flex_wrap().gap_2();
        let Some(owner) = self.reading_owner().filter(|o| &o.identity == identity) else {
            return row;
        };
        let control = Control {
            owner,
            generation: self.shell.rendered_reading.generation,
            serial,
        };
        if !self.shell.rendered_reading.enabled {
            if preview {
                return row;
            }
            let captured = control.clone();
            return row.child(
                div()
                    .id("reading-mode-enable")
                    .cursor_pointer()
                    .child("Use rendered reading for this session")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.reading_enable(&captured, window, cx)
                    })),
            );
        }
        row = row.child("Rendered reading: session only");
        let captured = control.clone();
        row = row.child(
            div()
                .id("reading-mode-retry")
                .cursor_pointer()
                .child("Reload / Retry rendered content")
                .on_click(cx.listener(move |this, _, window, cx| {
                    this.reading_retry(&captured, window, cx)
                })),
        );
        let captured = control.clone();
        row = row.child(
            div()
                .id("reading-mode-source")
                .cursor_pointer()
                .child("Source (turn mode off)")
                .on_click(cx.listener(move |this, _, window, cx| {
                    if preview {
                        if this.reading_control_current(&captured) {
                            this.close_rendered_user(&captured.owner.identity, serial, window, cx);
                        }
                    } else if this.reading_source_ready(window, cx)
                        && this.reading_control_current(&captured)
                    {
                        this.shell.rendered_reading.disable();
                        if let Some(state) = this.shell.includes.as_mut() {
                            state.serial = SERIAL.fetch_add(1, Ordering::Relaxed);
                            state.fetching = false;
                        }
                        cx.notify();
                    }
                })),
        );
        if preview {
            for (label, forward) in [("Back", false), ("Forward", true)] {
                let Some(ticket) = self.history.peek(forward) else {
                    row = row.child(format!("{label}: no history"));
                    continue;
                };
                if !history_route_supported(ticket.route.as_ref()) {
                    row = row.child(format!("{label}: return to Source for this route"));
                    continue;
                }
                let captured = control.clone();
                row = row.child(
                    div()
                        .id(gpui::SharedString::from(format!("reading-mode-{label}")))
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            this.reading_history(&captured, serial, forward, window, cx)
                        })),
                );
            }
        }
        row
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn owner(request: u64) -> Owner {
        let note = live::NoteKey {
            vault: "host".into(),
            note_id: 1,
        };
        Owner {
            identity: Identity {
                source: LinkSource {
                    note: note.clone(),
                    etag: "same".into(),
                    launch_generation: 3,
                    storage_generation: 4,
                    request,
                },
                route: note,
            },
            scope: "host".into(),
            scope_revision: 5,
            registry: ("host".into(), vec!["host".into()]),
            tasks_invalidated: false,
            body: std::sync::Arc::from("exact body"),
        }
    }
    #[test]
    fn busy_and_error_do_not_reclaim_until_explicit_retry() {
        let mut m = Mode::default();
        assert!(m.claim(owner(1)).is_none());
        m.enable();
        let a = m.claim(owner(1)).unwrap();
        for _ in 0..100 {
            assert!(m.claim(owner(1)).is_none());
        }
        assert!(m.accepts(&a, &owner(1)));
        let retry = m.retry(owner(1)).unwrap();
        assert!(!m.accepts(&a, &owner(1)));
        assert!(m.accepts(&retry, &owner(1)));
    }
    #[test]
    fn same_etag_body_new_request_and_aba_reject_old_completion() {
        let mut m = Mode::default();
        m.enable();
        let a = m.claim(owner(1)).unwrap();
        let b = m.claim(owner(2)).unwrap();
        assert!(!m.accepts(&a, &owner(2)));
        let a2 = m.claim(owner(1)).unwrap();
        assert!(!m.accepts(&a, &owner(1)));
        assert!(!m.accepts(&b, &owner(1)));
        assert!(m.accepts(&a2, &owner(1)));
    }
    #[test]
    fn source_off_then_on_cannot_revive_pending_result_or_controls() {
        let mut m = Mode::default();
        m.enable();
        let ticket = m.claim(owner(1)).unwrap();
        let button = Control {
            owner: owner(1),
            generation: m.generation,
            serial: 9,
        };
        assert!(button.current(&m, &owner(1), 9));
        m.disable();
        assert!(!m.accepts(&ticket, &owner(1)));
        m.enable();
        m.claim(owner(1)).unwrap();
        assert!(!m.accepts(&ticket, &owner(1)));
        assert!(!button.current(&m, &owner(1), 9));
    }
    #[test]
    fn every_owner_dimension_and_exact_body_is_required() {
        for i in 0..9 {
            let mut m = Mode::default();
            m.enable();
            let ticket = m.claim(owner(1)).unwrap();
            let mut changed = owner(1);
            match i {
                0 => changed.identity.source.request += 1,
                1 => changed.identity.source.storage_generation += 1,
                2 => changed.identity.source.launch_generation += 1,
                3 => changed.scope_revision += 1,
                4 => changed.registry.1.push("foreign".into()),
                5 => changed.tasks_invalidated = true,
                6 => changed.body = std::sync::Arc::from("changed body"),
                7 => changed.scope = "foreign".into(),
                _ => changed.identity.source.etag = "changed".into(),
            }
            assert!(!m.accepts(&ticket, &changed));
            assert!(m.claim(changed).is_some());
            m.claim(owner(1));
        }
    }
    #[test]
    fn retained_snapshot_is_only_same_owner_across_retry_not_new_source() {
        let mut m = Mode::default();
        m.enable();
        let first = m.claim(owner(1)).unwrap();
        let retry = m.retry(owner(1)).unwrap();
        assert!(first.same_owner(&retry));
        let next = m.claim(owner(2)).unwrap();
        assert!(!first.same_owner(&next));
        let mut different = owner(1);
        different.body = std::sync::Arc::from("another");
        let changed = m.claim(different).unwrap();
        assert!(!first.same_owner(&changed));
    }
    #[test]
    fn retirement_cancels_once_and_fresh_acceptance_can_try_again() {
        let mut m = Mode::default();
        m.enable();
        let first = m.claim(owner(1)).unwrap();
        m.retire();
        assert!(!m.accepts(&first, &owner(1)));
        assert!(m.enabled);
        let next = m.claim(owner(1)).unwrap();
        assert!(m.accepts(&next, &owner(1)));
        assert!(m.claim(owner(1)).is_none());
        let button = Control {
            owner: owner(1),
            generation: m.generation,
            serial: 7,
        };
        assert!(!button.current(&m, &owner(1), 8));
        assert!(!button.current(&m, &owner(2), 7));
    }
    #[test]
    fn modal_history_support_is_explicit_and_does_not_bypass_special_routes() {
        assert!(history_route_supported(None));
        assert!(history_route_supported(Some(&tabs::TabKey::Home)));
        assert!(history_route_supported(Some(&tabs::TabKey::Note {
            vault: "host".into(),
            note_id: 1
        })));
        for key in [
            tabs::TabKey::Graph,
            tabs::TabKey::Calendar,
            tabs::TabKey::Tag {
                tag: "tag".into(),
                scope: "host".into(),
            },
        ] {
            assert!(!history_route_supported(Some(&key)));
        }
    }
    #[test]
    fn exact_body_cache_reuses_allocation_without_granting_owner_authority() {
        let m = Mode::default();
        let a = m.body_snapshot("日本語 body");
        let b = m.body_snapshot("日本語 body");
        assert!(std::sync::Arc::ptr_eq(&a, &b));
        let c = m.body_snapshot("changed body");
        assert!(!std::sync::Arc::ptr_eq(&a, &c));
        assert_eq!(a.as_ref(), "日本語 body");
        assert!(!m.enabled);
        assert!(m.attempted.is_none());
    }
}
