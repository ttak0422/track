//! Workspace-local side-button tab traversal, independent of screen history.
use super::*;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Phase {
    Down,
    Up,
}

fn direction(button: gpui::MouseButton, phase: Phase) -> Option<gpui::NavigationDirection> {
    match (button, phase) {
        (gpui::MouseButton::Navigate(direction), Phase::Down) => Some(direction),
        _ => None,
    }
}

fn keys(tabs: &tabs::Tabs) -> Vec<tabs::TabKey> {
    tabs.entries().iter().map(|tab| tab.key.clone()).collect()
}

fn supported(key: &tabs::TabKey) -> bool {
    matches!(
        key,
        tabs::TabKey::Note { .. } | tabs::TabKey::Graph | tabs::TabKey::Calendar
    )
}

#[derive(Clone, Copy, Debug)]
struct Admission {
    active_workspace: bool,
    open_route: bool,
    owner_blocked: bool,
    input_busy: bool,
    recovery_busy: bool,
}

impl Admission {
    fn allowed(self) -> bool {
        self.active_workspace
            && self.open_route
            && !self.owner_blocked
            && !self.input_busy
            && !self.recovery_busy
    }
}

/// Retain sequence-start order while successful selection promotes the active tab.
/// An unaccepted destination never advances the walk.
#[derive(Debug, Default)]
pub(crate) struct Walk {
    order: Vec<tabs::TabKey>,
    observed: Vec<tabs::TabKey>,
    requested: Option<tabs::TabKey>,
}

impl Walk {
    pub(crate) fn reset(&mut self) {
        *self = Self::default();
    }

    pub(crate) fn pointer_down(&mut self, pending_decision: bool) {
        // Save/Discard/Cancel owns pointer interaction until its decision ends.
        // In particular, clicking Cancel must not change the retry neighbor.
        if !pending_decision {
            self.reset();
        }
    }

    pub(crate) fn navigation(
        &mut self,
        intent: &workspace_edit::Transition,
        pending_decision: bool,
    ) {
        if !matches!(intent, workspace_edit::Transition::MouseTab(_)) && !pending_decision {
            self.reset();
        }
    }

    pub(crate) fn destination(
        &mut self,
        tabs: &tabs::Tabs,
        direction: gpui::NavigationDirection,
    ) -> Option<tabs::TabKey> {
        let active = &tabs.active()?.key;
        if !supported(active) {
            return None;
        }
        let current = keys(tabs);
        if self.observed != current {
            self.order = current
                .iter()
                .filter(|key| supported(key))
                .cloned()
                .collect();
            self.observed = current;
        }
        if self.order.len() < 2 {
            return None;
        }
        let index = self.order.iter().position(|key| key == active)?;
        let index = match direction {
            gpui::NavigationDirection::Back => (index + self.order.len() - 1) % self.order.len(),
            gpui::NavigationDirection::Forward => (index + 1) % self.order.len(),
        };
        self.requested = Some(self.order[index].clone());
        self.requested.clone()
    }

    fn event(
        &mut self,
        tabs: &tabs::Tabs,
        button: gpui::MouseButton,
        phase: Phase,
        admission: Admission,
    ) -> Option<tabs::TabKey> {
        let direction = direction(button, phase)?;
        admission
            .allowed()
            .then(|| self.destination(tabs, direction))
            .flatten()
    }

    pub(crate) fn accepts(&self, key: &tabs::TabKey, tabs: &tabs::Tabs) -> bool {
        self.requested.as_ref() == Some(key)
            && self.observed == keys(tabs)
            && tabs.get(key).is_some()
            && tabs
                .active()
                .is_some_and(|active| supported(&active.key) && &active.key != key)
    }

    pub(crate) fn complete(&mut self, tabs: &tabs::Tabs) {
        if tabs
            .active()
            .is_some_and(|active| self.requested.as_ref() == Some(&active.key))
        {
            self.observed = keys(tabs);
            self.requested = None;
        }
    }
}

impl Workspace {
    fn mouse_tabs_admission(&self, window: &Window, cx: &App) -> Admission {
        Admission {
            active_workspace: window.is_window_active()
                && cx.active_window() == Some(window.window_handle())
                && cx.active_window().and_then(shortcut_workspace).is_some(),
            open_route: !self.shell.home && self.tag_page.is_none(),
            owner_blocked: self.reader_selection_dragging() || self.tag_routing_blocked(cx),
            input_busy: self.editor_shortcut_blocked(cx) || self.shell.find.is_composing(cx),
            recovery_busy: self.session_ui.recovery_enabled()
                && (!self.session_ui.recovery_ready()
                    || self.session_ui.recovery_status()
                        == workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Failed,
                        )),
        }
    }

    pub(crate) fn mouse_tab_event(
        &mut self,
        button: gpui::MouseButton,
        phase: Phase,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        if let gpui::MouseButton::Navigate(_) = button {
            // Consume even a suppressed press/release: no page-history fallback.
            cx.stop_propagation();
            let admission = self.mouse_tabs_admission(window, cx);
            if let Some(key) = self
                .shell
                .mouse_tabs
                .event(&self.tabs, button, phase, admission)
            {
                self.activate_mouse_tab(key, cx);
            }
        } else if phase == Phase::Down {
            self.shell
                .mouse_tabs
                .pointer_down(self.editing_ui.has_pending());
        }
    }

    pub(crate) fn activate_mouse_tab(&mut self, key: tabs::TabKey, cx: &mut Context<Self>) {
        if self.shell.home
            || self.tag_page.is_some()
            || !self.shell.mouse_tabs.accepts(&key, &self.tabs)
        {
            return;
        }
        // Nested Note/Graph/Calendar guards reset ordinary navigation walks. Keep
        // this walk outside those calls, including a deferred exact dirty intent.
        let mut walk = std::mem::take(&mut self.shell.mouse_tabs);
        self.activate_mouse_tab_inner(key, cx);
        walk.complete(&self.tabs);
        self.shell.mouse_tabs = walk;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use gpui::NavigationDirection::{Back, Forward};

    fn note(id: i64) -> tabs::TabKey {
        tabs::TabKey::Note {
            vault: "fixture".into(),
            note_id: id,
        }
    }
    fn fixture() -> tabs::Tabs {
        tabs::Tabs::restore(
            [
                note(1),
                tabs::TabKey::Graph,
                note(2),
                tabs::TabKey::Calendar,
                note(3),
            ]
            .into_iter()
            .map(|key| tabs::Tab {
                key,
                title: "Synthetic".into(),
            })
            .collect(),
        )
    }
    fn admission() -> Admission {
        Admission {
            active_workspace: true,
            open_route: true,
            owner_blocked: false,
            input_busy: false,
            recovery_busy: false,
        }
    }
    fn press(
        walk: &mut Walk,
        tabs: &mut tabs::Tabs,
        direction: gpui::NavigationDirection,
    ) -> tabs::TabKey {
        let key = walk
            .event(
                tabs,
                gpui::MouseButton::Navigate(direction),
                Phase::Down,
                admission(),
            )
            .unwrap();
        assert!(walk.accepts(&key, tabs));
        assert!(tabs.activate(&key));
        walk.complete(tabs);
        key
    }

    #[test]
    fn multiple_presses_wrap_and_reverse_sequence_start_order() {
        let mut tabs = fixture();
        let mut walk = Walk::default();
        let forward: Vec<_> = (0..5)
            .map(|_| press(&mut walk, &mut tabs, Forward))
            .collect();
        assert_eq!(
            forward,
            vec![
                tabs::TabKey::Graph,
                note(2),
                tabs::TabKey::Calendar,
                note(3),
                note(1)
            ]
        );
        let backward: Vec<_> = (0..5).map(|_| press(&mut walk, &mut tabs, Back)).collect();
        assert_eq!(
            backward,
            vec![
                note(3),
                tabs::TabKey::Calendar,
                note(2),
                tabs::TabKey::Graph,
                note(1)
            ]
        );
    }

    #[test]
    fn overflow_and_title_renames_keep_qualified_identity() {
        let mut tabs = fixture();
        let mut walk = Walk::default();
        assert!(tabs.layout(0).overflow.contains(&note(3)));
        assert_eq!(press(&mut walk, &mut tabs, Forward), tabs::TabKey::Graph);
        tabs.update_title(&note(2), "Renamed 日本語");
        assert_eq!(press(&mut walk, &mut tabs, Forward), note(2));
        assert_eq!(tabs.active().unwrap().title, "Renamed 日本語");
        assert_eq!(press(&mut walk, &mut tabs, Forward), tabs::TabKey::Calendar);
        assert_eq!(press(&mut walk, &mut tabs, Forward), note(3));
    }

    #[test]
    fn pointer_and_explicit_navigation_start_from_current_mru() {
        let mut tabs = fixture();
        let mut walk = Walk::default();
        press(&mut walk, &mut tabs, Forward);
        press(&mut walk, &mut tabs, Forward);
        walk.pointer_down(false);
        assert_eq!(press(&mut walk, &mut tabs, Forward), tabs::TabKey::Graph);
        walk.navigation(
            &workspace_edit::Transition::ActivateTab(tabs::TabKey::Graph),
            false,
        );
        assert_eq!(press(&mut walk, &mut tabs, Forward), note(2));
        tabs.activate(&note(3)); // changed route is also detected without a pointer event
        assert_eq!(press(&mut walk, &mut tabs, Forward), note(2));
    }

    #[test]
    fn open_close_and_external_reorder_reset_the_walk() {
        let mut tabs = fixture();
        let mut walk = Walk::default();
        press(&mut walk, &mut tabs, Forward);
        tabs.open_note("other", 2, "Same ID in another vault");
        assert_eq!(press(&mut walk, &mut tabs, Forward), tabs::TabKey::Graph);
        tabs.close(&note(1));
        assert_eq!(
            press(&mut walk, &mut tabs, Forward),
            tabs::TabKey::Note {
                vault: "other".into(),
                note_id: 2
            }
        );
    }

    #[test]
    fn home_empty_and_single_tab_do_not_select_a_remembered_tab() {
        let mut walk = Walk::default();
        for mut tabs in [
            tabs::Tabs::default(),
            tabs::Tabs::restore(vec![tabs::Tab {
                key: note(1),
                title: "Only".into(),
            }]),
            fixture(),
        ] {
            if tabs.entries().len() > 1 {
                tabs.open_home();
            }
            let before = tabs.clone();
            for direction in [Back, Forward] {
                assert!(walk.destination(&tabs, direction).is_none());
            }
            assert_eq!(tabs, before);
        }
        let mut tabs = fixture();
        tabs.open_home();
        tabs.activate(&note(1));
        assert_eq!(press(&mut walk, &mut tabs, Back), note(3)); // background Home is not a supported tab destination
    }

    #[test]
    fn screen_history_never_supplies_a_tab_destination_or_fallback() {
        let mut history = navigation::History::default();
        history.record(Some(note(99)));
        history.record(Some(note(1)));
        assert_eq!(history.peek(false).unwrap().route, Some(note(99)));
        let before = history.clone();
        let mut tabs = fixture();
        let mut walk = Walk::default();
        assert_eq!(press(&mut walk, &mut tabs, Back), note(3));
        assert_eq!(history, before);
        tabs.open_home();
        assert!(walk.destination(&tabs, Back).is_none());
        assert_eq!(history, before);
    }

    #[test]
    fn suppressed_events_do_not_mutate_or_replay_after_owner_dismissal() {
        let mut tabs = fixture();
        let mut walk = Walk::default();
        for blocked in [
            Admission {
                active_workspace: false,
                ..admission()
            },
            Admission {
                open_route: false,
                ..admission()
            },
            Admission {
                owner_blocked: true,
                ..admission()
            },
            Admission {
                input_busy: true,
                ..admission()
            },
            Admission {
                recovery_busy: true,
                ..admission()
            },
        ] {
            for direction in [Back, Forward] {
                assert!(
                    walk.event(
                        &tabs,
                        gpui::MouseButton::Navigate(direction),
                        Phase::Down,
                        blocked
                    )
                    .is_none()
                );
            }
            assert!(walk.requested.is_none());
            assert_eq!(tabs.active().unwrap().key, note(1));
        }
        // Admission has no focus restriction: an ordinary focused editor is not busy.
        assert_eq!(press(&mut walk, &mut tabs, Forward), tabs::TabKey::Graph);
    }

    #[test]
    fn release_and_other_buttons_never_select_twice() {
        let mut tabs = fixture();
        let mut walk = Walk::default();
        assert_eq!(press(&mut walk, &mut tabs, Forward), tabs::TabKey::Graph);
        let before = tabs.clone();
        for button in gpui::MouseButton::all() {
            assert!(walk.event(&tabs, button, Phase::Up, admission()).is_none());
            if !matches!(button, gpui::MouseButton::Navigate(_)) {
                assert!(
                    walk.event(&tabs, button, Phase::Down, admission())
                        .is_none()
                );
            }
        }
        assert_eq!(tabs, before);
        assert_eq!(press(&mut walk, &mut tabs, Forward), note(2));
    }

    #[test]
    fn rejected_destination_stays_available_but_stale_deferred_intent_is_refused() {
        let mut tabs = fixture();
        let mut walk = Walk::default();
        press(&mut walk, &mut tabs, Forward);
        press(&mut walk, &mut tabs, Forward);
        let intended = walk.destination(&tabs, Forward).unwrap();
        assert_eq!(intended, tabs::TabKey::Calendar);
        walk.complete(&tabs); // access/dirty refusal did not activate anything
        walk.pointer_down(true); // Cancel button belongs to the pending leave prompt
        walk.navigation(&workspace_edit::Transition::Home, true); // unrelated blocked intent cannot erase the frozen request
        assert!(walk.accepts(&intended, &tabs));
        assert_eq!(walk.destination(&tabs, Forward), Some(intended.clone()));
        assert_eq!(tabs.active().unwrap().key, note(2));
        tabs.close(&intended);
        assert!(!walk.accepts(&intended, &tabs));
        assert_eq!(walk.destination(&tabs, Forward), Some(tabs::TabKey::Graph));
        walk.reset();
        assert!(!walk.accepts(&tabs::TabKey::Graph, &tabs));
    }
}
