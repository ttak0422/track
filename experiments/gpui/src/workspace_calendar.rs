//! Dedicated read-only Calendar route. Month/day/result state is process-only.
use super::*;
use gpui::Div;
#[derive(Default)]
pub(crate) struct CalendarState {
    pub month: Option<calendar_month::Month>,
    pub view: calendar_agenda::View,
    pub filter: calendar_agenda::Filter,
    pub agenda: Option<workspace_calendar_agenda::AgendaState>,
    pub revision: u64,
    pub day_memory: Option<CalendarDayMemory>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct CalendarDayMemory {
    pub launch: u64,
    pub epoch: u64,
    pub scope_revision: u64,
    pub scope: String,
    pub registry: (String, Vec<String>),
    pub month: calendar_month::Month,
    pub date: String,
}
impl CalendarDayMemory {
    pub fn matches(
        &self,
        launch: u64,
        epoch: u64,
        scope_revision: u64,
        scope: &str,
        registry: &(String, Vec<String>),
        month: &calendar_month::Month,
    ) -> bool {
        self.launch == launch
            && self.epoch == epoch
            && self.scope_revision == scope_revision
            && self.scope == scope
            && &self.registry == registry
            && &self.month == month
            && calendar_month::Month::containing(&self.date).as_ref() == Some(month)
    }
}
pub(crate) fn day_restore_ready(
    agenda_exists: bool,
    activity_done: bool,
    events_done: bool,
    zone_applied: bool,
) -> bool {
    !agenda_exists && activity_done && events_done && zone_applied
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
        self.shell.calendar.agenda = None;
        if self.tabs.get(&tabs::TabKey::Calendar).is_none() {
            self.shell.calendar.view = calendar_agenda::View::Month;
            self.shell.calendar.filter = calendar_agenda::Filter::All;
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
        let controls = self.calendar_period_controls(dark, cx);
        let body = if self.shell.calendar.view == calendar_agenda::View::Month {
            self.calendar_activity((width - 120.).clamp(280., 960.), dark, cx)
        } else {
            self.calendar_period_body(cx)
        };
        div().flex_1().min_h_0().p_4().pl(px(68.)).child(
            div()
                .id("calendar-page-scroll")
                .h_full()
                .overflow_y_scroll()
                .child(controls)
                .child(body),
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn restore_waits_for_jobs_and_never_replaces_new_day_intent() {
        assert!(!day_restore_ready(false, false, false, true));
        assert!(!day_restore_ready(false, true, false, true));
        assert!(!day_restore_ready(false, false, true, true));
        assert!(!day_restore_ready(false, true, true, false));
        assert!(!day_restore_ready(true, true, true, true));
        assert!(day_restore_ready(false, true, true, true)); // Done includes failed jobs; no success prerequisite.
    }
    #[test]
    fn selected_day_memory_is_scoped_and_never_a_response_ticket() {
        let month = calendar_month::Month::containing("2026-11-17").unwrap();
        let registry = ("host".into(), vec!["foreign".into(), "host".into()]);
        let memory = CalendarDayMemory {
            launch: 1,
            epoch: 2,
            scope_revision: 3,
            scope: "foreign".into(),
            registry: registry.clone(),
            month: month.clone(),
            date: "2026-11-17".into(),
        };
        assert!(memory.matches(1, 2, 3, "foreign", &registry, &month));
        assert!(!memory.matches(2, 2, 3, "foreign", &registry, &month));
        assert!(!memory.matches(1, 4, 3, "foreign", &registry, &month));
        assert!(!memory.matches(1, 2, 5, "foreign", &registry, &month));
        assert!(!memory.matches(1, 2, 3, "host", &registry, &month));
        assert!(!memory.matches(
            1,
            2,
            3,
            "foreign",
            &("host".into(), vec!["host".into()]),
            &month
        ));
        assert!(!memory.matches(1, 2, 3, "foreign", &registry, &month.shifted(true).unwrap()));
        let mut invalid = memory.clone();
        invalid.date = "2026-11-31".into();
        assert!(!invalid.matches(1, 2, 3, "foreign", &registry, &month));
    }
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
