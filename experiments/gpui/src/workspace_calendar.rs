//! Dedicated read-only Calendar route. Month/day/result state is process-only.
use super::*;
use gpui::Div;
#[derive(Default)]
pub(crate) struct CalendarState {
    pub month: Option<calendar_month::Month>,
    pub revision: u64,
}
pub(crate) fn involves_calendar(
    intent: &workspace_edit::Transition,
    active: bool,
    retained: bool,
) -> bool {
    use workspace_edit::Transition;
    active
        || matches!(
            intent,
            Transition::Calendar | Transition::ActivateTab(tabs::TabKey::Calendar)
        )
        || matches!(intent, Transition::CloseTab(_) if retained)
        || matches!(intent, Transition::History(ticket) if ticket.route == Some(tabs::TabKey::Calendar))
}
impl Workspace {
    pub(crate) fn calendar_active(&self) -> bool {
        self.tag_page.is_none()
            && !self.shell.home
            && self
                .tabs
                .active()
                .is_some_and(|tab| tab.key == tabs::TabKey::Calendar)
    }
    pub(crate) fn open_calendar(&mut self, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::Calendar, cx) {
            return;
        }
        self.previews.model.invalidate_session();
        self.previews.cancel_wake();
        self.tag_page = None;
        self.tabs.open_calendar();
        self.history.record(Some(tabs::TabKey::Calendar));
        self.shell.home = false;
        self.shell.search_open = false;
        self.shell.tab_menu = false;
        self.shell.find.open = false;
        self.selected = None;
        self.read_error = None;
        self.partial_read = false;
        self.current_note = None;
        self.current_note_request = None;
        self.content.clear();
        self.current_related.clear();
        self.show_graph = false;
        self.request += 1;
        self.graph_request += 1;
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.shell.calendar.revision = self.shell.calendar.revision.wrapping_add(1);
        self.shell.activity = None;
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        cx.notify();
    }
    pub(crate) fn render_calendar(
        &mut self,
        width: f32,
        dark: bool,
        cx: &mut Context<Self>,
    ) -> Div {
        let body = self.calendar_activity((width - 120.).clamp(280., 960.), dark, cx);
        div().flex_1().min_h_0().p_4().pl(px(68.)).child(
            div()
                .id("calendar-page-scroll")
                .h_full()
                .overflow_y_scroll()
                .child(body),
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn entry_and_neighbor_routes_require_owner_gate_before_mutation() {
        use workspace_edit::Transition;
        assert!(involves_calendar(&Transition::Calendar, false, false));
        assert!(involves_calendar(
            &Transition::ActivateTab(tabs::TabKey::Calendar),
            false,
            true
        ));
        assert!(involves_calendar(
            &Transition::CloseTab(tabs::TabKey::Note {
                vault: "a".into(),
                note_id: 1
            }),
            false,
            true
        ));
        assert!(involves_calendar(&Transition::Home, true, true));
        assert!(!involves_calendar(&Transition::Home, false, false));
        let mut history = navigation::History::default();
        history.record(Some(tabs::TabKey::Calendar));
        history.record(Some(tabs::TabKey::Home));
        assert!(involves_calendar(
            &Transition::History(history.peek(false).unwrap()),
            false,
            true
        ));
    }
}
