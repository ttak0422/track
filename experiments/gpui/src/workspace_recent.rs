//! Explicit current-run recents popup; no persistence, prefetch, or preview reads.
use super::*;
#[derive(Clone, Debug, PartialEq, Eq)]
struct Owner {
    launch: u64,
    storage: u64,
    request: u64,
    scope_revision: u64,
    scope: String,
    launch_name: String,
    registry: Vec<String>,
    revision: u64,
}
#[derive(Default)]
pub(crate) struct State {
    model: recent_notes::RecentNotes,
    pending: Option<(u64, live::NoteKey)>,
    panel: Option<Owner>,
    focus: Option<gpui::FocusHandle>,
    return_reader_focus: bool,
}
fn accepted_key(
    route: &live::NoteKey,
    note: &live::NoteKey,
    launch: &str,
    registry: &[String],
) -> Option<live::NoteKey> {
    let canonical = |scope: &str| {
        if scope.is_empty() {
            launch.to_owned()
        } else {
            scope.to_owned()
        }
    };
    let vault = canonical(&route.vault);
    (registry.contains(&vault) && canonical(&note.vault) == vault && note.note_id == route.note_id)
        .then_some(live::NoteKey {
            vault,
            note_id: note.note_id,
        })
}
fn requested(pending: Option<&(u64, live::NoteKey)>, request: u64, key: &live::NoteKey) -> bool {
    pending.is_some_and(|(r, k)| *r == request && k == key)
}
fn forget_pending(pending: &mut Option<(u64, live::NoteKey)>, request: u64) {
    if pending.as_ref().is_some_and(|(r, _)| *r == request) {
        *pending = None;
    }
}
fn recent_control_allowed(current: bool, active: bool, focused: bool) -> bool {
    current && active && focused
}
impl Workspace {
    pub(crate) fn recent_open(&self) -> bool {
        self.shell.recent.panel.is_some()
    }
    pub(crate) fn recent_note_requested(&mut self, previous: u64, key: live::NoteKey) {
        if self.request != previous
            && !self.follow.routing
            && self.tabs.active().is_some_and(|tab| {
                tab.key
                    == tabs::TabKey::Note {
                        vault: key.vault.clone(),
                        note_id: key.note_id,
                    }
            })
        {
            self.shell.recent.pending = Some((self.request, key));
        }
    }
    pub(crate) fn forget_recent_request(&mut self, request: u64) {
        forget_pending(&mut self.shell.recent.pending, request);
    }
    pub(crate) fn recent_request_pending(&self, request: u64, key: &live::NoteKey) -> bool {
        requested(self.shell.recent.pending.as_ref(), request, key)
    }
    pub(crate) fn recent_accept_note(
        &mut self,
        request: u64,
        route: &live::NoteKey,
        note: &live::Note,
    ) {
        if self.request != request
            || !self.session_ui.permits(&route.vault)
            || !self.tabs.active().is_some_and(|tab| {
                tab.key
                    == tabs::TabKey::Note {
                        vault: route.vault.clone(),
                        note_id: route.note_id,
                    }
            })
        {
            return;
        }
        let visit = self.recent_request_pending(request, route);
        if visit {
            self.shell.recent.pending = None;
        }
        let Some((launch_name, registry)) = self.follow_registry() else {
            return;
        };
        let Some(key) = accepted_key(route, &note.key(), &launch_name, &registry) else {
            return;
        };
        let Some(session) = self.live.as_ref() else {
            return;
        };
        let launch = session.client().launch_generation;
        self.shell.recent.model.reset_launch(launch);
        if visit {
            self.shell.recent.model.record(launch, key, &note.title);
        } else {
            self.shell.recent.model.update_title(&key, &note.title);
        }
    }

    fn recent_owner(&self) -> Option<Owner> {
        let session = self.live.as_ref()?;
        let (launch_name, registry) = self.follow_registry()?;
        Some(Owner {
            launch: session.client().launch_generation,
            storage: self.accepted_storage_generation?,
            request: self.request,
            scope_revision: self.shell.scope_revision,
            scope: self.working_vault_scope().into(),
            launch_name,
            registry,
            revision: self.shell.recent.model.revision(),
        })
    }
    fn recent_owner_available(&self, cx: &App) -> bool {
        !self.reader_selection_dragging()
            && !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && !self.editing_ui.active()
            && !self.editing_ui.has_pending()
            && !self.creation_open()
            && !self.shell.find.is_composing(cx)
            && !self.quit_session.is_closing()
            && self.quit_session.failure().is_none()
    }
    pub(crate) fn toggle_recent(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !window.is_window_active() || cx.active_window().and_then(shortcut_workspace).is_none() {
            return;
        }
        if self.recent_open() {
            self.close_recent(window, cx);
            return;
        }
        if !self.recent_owner_available(cx) {
            return;
        }
        let Some(session) = self.live.as_ref() else {
            return;
        };
        self.shell
            .recent
            .model
            .reset_launch(session.client().launch_generation);
        let Some(owner) = self.recent_owner() else {
            return;
        };
        self.pause_reader_for_overlay();
        self.clear_reader_hover_owner();
        self.clear_reader_selection();
        self.shell.recent.return_reader_focus = self
            .shell
            .reader_focus
            .as_ref()
            .is_some_and(|f| f.is_focused(window));
        let focus = cx.focus_handle();
        window.focus(&focus);
        self.shell.recent.focus = Some(focus);
        self.shell.recent.panel = Some(owner);
        cx.stop_propagation();
        cx.notify();
    }
    pub(crate) fn close_recent(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let eligible = self.shell.recent.panel.as_ref() == self.recent_owner().as_ref();
        self.shell.recent.panel = None;
        let owns_focus = self
            .shell
            .recent
            .focus
            .take()
            .is_some_and(|focus| focus.is_focused(window));
        if owns_focus {
            if eligible
                && self.shell.recent.return_reader_focus
                && window.is_window_active()
                && self.footnote_reader_available(cx)
            {
                if let Some(focus) = &self.shell.reader_focus {
                    window.focus(focus);
                } else {
                    window.blur();
                }
            } else {
                window.blur();
            }
        }
        self.shell.recent.return_reader_focus = false;
        cx.notify();
    }
    pub(crate) fn invalidate_recent_panel(&mut self, window: &mut Window) {
        if self
            .shell
            .recent
            .panel
            .as_ref()
            .is_some_and(|owner| Some(owner) != self.recent_owner().as_ref())
        {
            self.shell.recent.panel = None;
            self.shell.recent.return_reader_focus = false;
            if self
                .shell
                .recent
                .focus
                .take()
                .is_some_and(|focus| focus.is_focused(window))
            {
                window.blur();
            }
        }
    }
    fn activate_recent(
        &mut self,
        owner: &Owner,
        key: &live::NoteKey,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !recent_control_allowed(
            self.shell.recent.panel.as_ref() == Some(owner)
                && self.recent_owner().as_ref() == Some(owner),
            window.is_window_active() && cx.active_window().and_then(shortcut_workspace).is_some(),
            self.shell
                .recent
                .focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window)),
        ) || !owner.registry.contains(&key.vault)
            || !self.shell.recent.model.contains(key)
        {
            return;
        }
        // Clear only this popup before entering the ordinary transition gate.
        self.close_recent(window, cx);
        if !self.recent_owner_available(cx) {
            cx.notify();
            return;
        }
        // Keep the canonical, validated vault name; never reinterpret it as a
        // launch alias or as the current working scope.
        self.open_live_key(key.clone(), cx);
        cx.notify();
    }
    pub(crate) fn render_recent(
        &self,
        colors: (u32, u32, u32, u32),
        cx: &mut Context<Self>,
    ) -> gpui::Div {
        let (paper, ink, muted, line) = colors;
        let Some(owner) = self.shell.recent.panel.clone() else {
            return div();
        };
        let rows = self
            .shell
            .recent
            .model
            .rows(&owner.scope, &owner.launch_name);
        let Some(focus) = self.shell.recent.focus.as_ref() else {
            return div();
        };
        div().absolute().size_full().child(
            div()
                .id("recent-backdrop")
                .absolute()
                .inset_0()
                .track_focus(focus)
                .on_action(|_: &Save, _, cx| cx.stop_propagation())
                .on_action(|_: &CloseTab, _, cx| cx.stop_propagation())
                .on_action(|_: &ShowAbout, _, cx| cx.stop_propagation())
                .on_action(|_: &CopyBuildInfo, _, cx| cx.stop_propagation())
                .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                    if event.keystroke.key == "escape" {
                        this.close_recent(window, cx);
                    }
                    cx.stop_propagation();
                }))
                .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_mouse_up(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_scroll_wheel(|_, _, cx| cx.stop_propagation())
                .on_click(cx.listener(|this, _, window, cx| {
                    this.close_recent(window, cx);
                    cx.stop_propagation();
                }))
                .child(
                    div()
                        .id("recent-panel")
                        .absolute()
                        .left(px(68.))
                        .top(px(48.))
                        .w(px(360.))
                        .max_w_full()
                        .max_h(px(480.))
                        .bg(rgb(paper))
                        .text_color(rgb(ink))
                        .border_1()
                        .border_color(rgb(line))
                        .rounded_lg()
                        .p_3()
                        .flex()
                        .flex_col()
                        .on_click(|_, _, cx| cx.stop_propagation())
                        .child(
                            div()
                                .flex()
                                .justify_between()
                                .child("Recently opened notes")
                                .child(
                                    div()
                                        .id("recent-close")
                                        .cursor_pointer()
                                        .child("Close")
                                        .on_click(cx.listener(|this, _, window, cx| {
                                            this.close_recent(window, cx);
                                            cx.stop_propagation();
                                        })),
                                ),
                        )
                        .child(
                            div()
                                .text_size(px(11.))
                                .text_color(rgb(muted))
                                .mb_2()
                                .child("This run only · successful visits · no restart history"),
                        )
                        .child(
                            div()
                                .id("recent-rows")
                                .overflow_y_scroll()
                                .max_h(px(380.))
                                .when(rows.is_empty(), |list| {
                                    list.child("No recently opened notes in this scope.")
                                })
                                .children(rows.into_iter().enumerate().map(|(index, entry)| {
                                    let ticket = owner.clone();
                                    let key = entry.key;
                                    div()
                                        .id(("recent-note", index))
                                        .px_2()
                                        .py_2()
                                        .cursor_pointer()
                                        .child(div().child(entry.title))
                                        .child(
                                            div().text_size(px(11.)).text_color(rgb(muted)).child(
                                                if key.vault.is_empty() {
                                                    "Launch vault".into()
                                                } else {
                                                    key.vault.clone()
                                                },
                                            ),
                                        )
                                        .on_click(cx.listener(move |this, _, window, cx| {
                                            this.activate_recent(&ticket, &key, window, cx);
                                            cx.stop_propagation();
                                        }))
                                })),
                        ),
                ),
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn key(vault: &str, id: i64) -> live::NoteKey {
        live::NoteKey {
            vault: vault.into(),
            note_id: id,
        }
    }
    #[test]
    fn reload_suppresses_only_its_own_pending_visit() {
        let mut pending = Some((7, key("main", 1)));
        forget_pending(&mut pending, 6);
        assert!(requested(pending.as_ref(), 7, &key("main", 1)));
        forget_pending(&mut pending, 7);
        assert!(!requested(pending.as_ref(), 7, &key("main", 1)));
    }
    #[test]
    fn popup_actions_require_current_active_focused_owner() {
        assert!(recent_control_allowed(true, true, true));
        assert!(!recent_control_allowed(false, true, true));
        assert!(!recent_control_allowed(true, false, true));
        assert!(!recent_control_allowed(true, true, false));
    }
    #[test]
    fn aliases_match_only_a_current_registered_launch() {
        let registry = vec!["main".into(), "work".into()];
        assert_eq!(
            accepted_key(&key("", 1), &key("main", 1), "main", &registry),
            Some(key("main", 1))
        );
        assert!(accepted_key(&key("work", 1), &key("main", 1), "main", &registry).is_none());
        assert!(accepted_key(&key("removed", 1), &key("removed", 1), "main", &registry).is_none());
        assert!(accepted_key(&key("", 1), &key("main", 2), "main", &registry).is_none());
    }
    #[test]
    fn only_the_exact_user_request_can_record_a_visit() {
        let pending = (7, key("work", 1));
        assert!(requested(Some(&pending), 7, &key("work", 1)));
        assert!(!requested(Some(&pending), 8, &key("work", 1)));
        assert!(!requested(Some(&pending), 7, &key("", 1)));
        assert!(!requested(None, 7, &key("work", 1)));
    }
    #[test]
    fn popup_tickets_do_not_survive_registry_scope_or_route_changes() {
        let owner = Owner {
            launch: 1,
            storage: 2,
            request: 3,
            scope_revision: 4,
            scope: "".into(),
            launch_name: "main".into(),
            registry: vec!["main".into()],
            revision: 5,
        };
        let mut other = owner.clone();
        other.request += 1;
        assert_ne!(owner, other);
        other = owner.clone();
        other.scope_revision += 1;
        assert_ne!(owner, other);
        other = owner.clone();
        other.registry.push("work".into());
        assert_ne!(owner, other);
        other = owner.clone();
        other.storage += 1;
        assert_ne!(owner, other);
        other = owner.clone();
        other.revision += 1;
        assert_ne!(owner, other);
    }
}
