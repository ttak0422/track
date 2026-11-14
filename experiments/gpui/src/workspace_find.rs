//! Window-local read-only Find. This subset searches rendered body/table text,
//! and reveals a block/cell rather than an exact laid-out character rectangle.
use super::*;
use gpui::{Div, Focusable};
use std::rc::Rc;
#[derive(Default)]
pub(crate) struct FindState {
    pub open: bool,
    pub highlighted: bool,
    input: Option<gpui::Entity<search_input::SearchInput>>,
    query: String,
    identity: Option<(tabs::TabKey, u64)>,
    blocks: Rc<Vec<markdown::Block>>,
    pub matches: reader_find::Matches,
    pub active: Option<usize>,
    pub anchors: Option<Rc<markdown::FindAnchors>>,
    revision: u64,
    pending_scroll: bool,
}
impl FindState {
    fn deactivate(&mut self) {
        self.open = false;
        self.highlighted = false;
        self.pending_scroll = false;
        self.revision = self.revision.wrapping_add(1);
    }
}
fn same_display(left: &Rc<Vec<markdown::Block>>, right: &Rc<Vec<markdown::Block>>) -> bool {
    Rc::ptr_eq(left, right) || left == right
}
fn document_matches(
    expected: &Option<(tabs::TabKey, u64)>,
    current: &Option<(tabs::TabKey, u64)>,
    same_content: bool,
) -> bool {
    expected.is_some() && expected == current && same_content
}
fn scroll_is_current(
    expected_revision: u64,
    revision: u64,
    expected_request: u64,
    request: u64,
    same_document: bool,
    available: bool,
    highlighted: bool,
) -> bool {
    expected_revision == revision
        && expected_request == request
        && same_document
        && available
        && highlighted
}
impl Workspace {
    pub(crate) fn find_available(&self) -> bool {
        !self.shell.home
            && !self.editing_ui.active()
            && !self.editing_ui.has_pending()
            && !self.shell.search_open
            && !self.shell.vault_menu
            && !self.shell.tab_menu
            && !self.shell.diagnostics
            && (self.current_note.is_some() || !self.content.is_empty())
    }
    fn evaluate_find(&mut self, reset: bool, scroll: bool) {
        let displayed = self.displayed_content();
        let state = &mut self.shell.find;
        state.matches = reader_find::search(&displayed, &state.query);
        state.active = if reset {
            (!state.matches.hits.is_empty()).then_some(0)
        } else {
            state.active.and_then(|i| {
                (!state.matches.hits.is_empty())
                    .then_some(i.min(state.matches.hits.len().saturating_sub(1)))
            })
        };
        state.revision = state.revision.wrapping_add(1);
        state.pending_scroll = scroll && state.active.is_some();
    }
    pub(crate) fn sync_find(
        &mut self,
        key: tabs::TabKey,
        scroll: &gpui::ScrollHandle,
        dark: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.shell.find.input.is_none() {
            let input = cx.new(|cx| {
                search_input::SearchInput::with_purpose(search_input::InputPurpose::ReaderFind, cx)
            });
            self.subscriptions.push(cx.subscribe(
                &input,
                |this, _, event: &search_input::SearchChanged, cx| {
                    let changed = this.shell.find.query != event.0;
                    this.shell.find.query = event.0.clone();
                    if this.find_available() && this.shell.find.open {
                        this.evaluate_find(changed || this.shell.find.active.is_none(), true);
                    }
                    cx.notify();
                },
            ));
            self.shell.find.input = Some(input);
        }
        self.shell
            .find
            .input
            .as_ref()
            .unwrap()
            .update(cx, |input, _| input.set_dark(dark));
        if !self.find_available() {
            let state = &mut self.shell.find;
            if state.open || state.highlighted {
                state.revision = state.revision.wrapping_add(1);
            }
            state.open = false;
            state.highlighted = false;
            state.pending_scroll = false;
            return;
        }
        let composing = self
            .shell
            .find
            .input
            .as_ref()
            .unwrap()
            .read(cx)
            .is_composing();
        let identity = (key, self.request);
        let route_changed = self.shell.find.identity.as_ref() != Some(&identity);
        let displayed = self.displayed_content();
        let changed = route_changed || !same_display(&self.shell.find.blocks, &displayed);
        if changed {
            let state = &mut self.shell.find;
            state.identity = Some(identity);
            state.blocks = displayed.clone();
            state.anchors = Some(Rc::new(markdown::FindAnchors::new(&displayed, scroll)));
            if state.highlighted && !composing {
                self.evaluate_find(route_changed, false);
            } else {
                state.matches = Default::default();
                state.active = None;
                state.revision = state.revision.wrapping_add(1);
            }
        }
        if self.shell.find.pending_scroll {
            self.shell.find.pending_scroll = false;
            let state = &self.shell.find;
            let target = state
                .active
                .and_then(|i| state.matches.hits.get(i))
                .map(|hit| hit.target.clone());
            if let (Some(target), Some(anchors)) = (target, state.anchors.clone()) {
                let revision = state.revision;
                let request = self.request;
                let identity = self.shell.find.identity.clone();
                let weak = cx.entity().downgrade();
                window.on_next_frame(move |window, cx| {
                    let _ = weak.update(cx, |this, cx| {
                        if scroll_is_current(
                            revision,
                            this.shell.find.revision,
                            request,
                            this.request,
                            document_matches(
                                &identity,
                                &this.shell.find.identity,
                                same_display(&this.displayed_content(), &this.shell.find.blocks),
                            ),
                            this.find_available(),
                            this.shell.find.highlighted,
                        ) {
                            anchors.scroll_to(&target, window, cx);
                            cx.notify();
                        }
                    });
                });
            }
        }
    }
    pub(crate) fn open_find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.find_available() {
            return;
        }
        let Some(input) = self.shell.find.input.clone() else {
            return;
        };
        if input.read(cx).is_composing() {
            return;
        }
        if !self.shell.find.open {
            self.shell.find.highlighted = true;
            self.evaluate_find(true, true);
        }
        self.shell.find.open = true;
        input.update(cx, |input, cx| input.select_all(cx));
        window.focus(&input.focus_handle(cx));
        cx.notify();
    }
    pub(crate) fn move_find(&mut self, forward: bool, cx: &mut Context<Self>) {
        if !self.find_available()
            || self
                .shell
                .find
                .input
                .as_ref()
                .is_some_and(|i| i.read(cx).is_composing())
        {
            return;
        }
        let was_highlighted = self.shell.find.highlighted;
        self.evaluate_find(false, false);
        let state = &mut self.shell.find;
        state.highlighted = true;
        state.active = reader_find::step(
            if was_highlighted { state.active } else { None },
            state.matches.hits.len(),
            forward,
        );
        state.pending_scroll = state.active.is_some();
        cx.notify();
    }
    pub(crate) fn close_find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .shell
            .find
            .input
            .as_ref()
            .is_some_and(|i| i.read(cx).is_composing())
        {
            return;
        }
        self.shell.find.deactivate();
        if let Some(focus) = &self.shell.reader_focus {
            window.focus(focus);
        }
        cx.notify();
    }
    pub(crate) fn find_keyboard(
        &mut self,
        event: &gpui::KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        let focused = self
            .shell
            .reader_focus
            .as_ref()
            .is_some_and(|f| f.is_focused(window))
            || self
                .shell
                .find
                .input
                .as_ref()
                .is_some_and(|i| i.focus_handle(cx).is_focused(window));
        if !focused || !self.find_available() {
            return false;
        }
        let m = event.keystroke.modifiers;
        if m.platform && !m.control && !m.alt && event.keystroke.key == "f" && !m.shift {
            self.open_find(window, cx);
            return true;
        }
        if m.platform && !m.control && !m.alt && event.keystroke.key == "g" {
            self.move_find(!m.shift, cx);
            return true;
        }
        false
    }
    pub(crate) fn find_overlay(
        &self,
        dark: bool,
        width: gpui::Pixels,
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<Div> {
        let state = &self.shell.find;
        let status = if state.matches.error.is_some() {
            "Find unavailable".into()
        } else if state.query.is_empty() {
            String::new()
        } else if state.matches.hits.is_empty() {
            "No matches".into()
        } else {
            format!(
                "{} of {}{}",
                state.active.map_or(0, |i| i + 1),
                state.matches.hits.len(),
                if state.matches.truncated {
                    "+ (limited)"
                } else {
                    ""
                }
            )
        };
        let enabled = !state.matches.hits.is_empty();
        let mut controls = div()
            .flex()
            .gap_3()
            .items_center()
            .child(div().flex_1().child(status));
        for (id, label, forward) in [
            ("find-previous", "Previous", false),
            ("find-next", "Next", true),
        ] {
            controls = controls.child(
                div()
                    .id(id)
                    .when(enabled, |button| {
                        button.cursor_pointer().on_click(
                            cx.listener(move |this, _, _, cx| this.move_find(forward, cx)),
                        )
                    })
                    .opacity(if enabled { 1. } else { 0.4 })
                    .child(label),
            );
        }
        controls = controls.child(
            div()
                .id("find-done")
                .cursor_pointer()
                .child("Done")
                .on_click(cx.listener(|this, _, window, cx| this.close_find(window, cx))),
        );
        div()
            .id("reader-find-bar")
            .absolute()
            .top(px(46.))
            .right(px(16.))
            .w(px(360.).min((width - px(32.)).max(px(0.))))
            .p_2()
            .rounded(px(8.))
            .border_1()
            .border_color(rgb(if dark { 0x3e4347 } else { 0xc7c5bd }))
            .bg(rgb(if dark { 0x191c1e } else { 0xffffff }))
            .text_color(rgb(if dark { 0xe9e9e4 } else { 0x1a1a18 }))
            .text_size(px(11.))
            .children(state.input.clone())
            .child(controls)
            .child(
                div()
                    .mt_1()
                    .text_size(px(10.))
                    .child("Body/table only · block/cell scroll, not exact range"),
            )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn cached_display_keeps_find_current_until_title_snapshot_changes() {
        let raw = Rc::new(markdown::parse("[[id:123]] tail"));
        assert!(same_display(&raw, &raw.clone()));
        assert!(same_display(&raw, &Rc::new(raw.as_ref().clone())));
        let title = Rc::new(markdown::with_resolved_titles(
            &raw,
            &[("id:123".into(), "Visible title".into())]
                .into_iter()
                .collect(),
        ));
        assert!(!same_display(&raw, &title));
        let next = Rc::new(markdown::with_resolved_titles(
            &raw,
            &[("id:123".into(), "New title".into())]
                .into_iter()
                .collect(),
        ));
        assert!(!same_display(&title, &next));
        assert!(reader_find::search(&next, "Visible").hits.is_empty());
        assert_eq!(reader_find::search(&next, "New").hits.len(), 1);
    }
    #[test]
    fn resolved_title_change_invalidates_old_find_document_and_byte_ranges() {
        let raw = markdown::parse("[[id:123]] tail");
        let titles = [("id:123".into(), "日本語の長いタイトル".into())]
            .into_iter()
            .collect();
        let displayed = markdown::with_resolved_titles(&raw, &titles);
        let identity = Some((
            tabs::TabKey::Note {
                vault: "main".into(),
                note_id: 1,
            },
            7,
        ));
        assert!(!document_matches(&identity, &identity, raw == displayed));
        assert!(!scroll_is_current(
            1,
            1,
            7,
            7,
            document_matches(&identity, &identity, raw == displayed),
            true,
            true
        ));
        assert!(reader_find::search(&displayed, "id:123").hits.is_empty());
        assert_eq!(reader_find::search(&displayed, "日本語").hits.len(), 1);
        let old = reader_find::search(&raw, "tail");
        let new = reader_find::search(&displayed, "tail");
        assert_ne!(old, new);
    }
    #[test]
    fn identical_body_does_not_cross_vault_tab_or_request() {
        let a = Some((
            tabs::TabKey::Note {
                vault: "a".into(),
                note_id: 42,
            },
            7,
        ));
        let b = Some((
            tabs::TabKey::Note {
                vault: "b".into(),
                note_id: 42,
            },
            7,
        ));
        let reload = Some((
            tabs::TabKey::Note {
                vault: "a".into(),
                note_id: 42,
            },
            8,
        ));
        assert!(document_matches(&a, &a, true));
        assert!(!document_matches(&a, &b, true));
        assert!(!document_matches(&a, &reload, true));
        assert!(!document_matches(&a, &a, false));
        assert!(!document_matches(&None, &None, true));
    }
    #[test]
    fn close_retains_query_but_cancels_highlight_and_scroll() {
        let mut state = FindState::default();
        state.query = "日本".into();
        state.open = true;
        state.highlighted = true;
        state.pending_scroll = true;
        state.revision = 9;
        state.deactivate();
        assert_eq!(state.query, "日本");
        assert!(!state.open);
        assert!(!state.highlighted);
        assert!(!state.pending_scroll);
        assert_eq!(state.revision, 10);
        state.open = true;
        state.highlighted = true;
        assert!(!scroll_is_current(
            9,
            state.revision,
            4,
            4,
            true,
            true,
            true
        ));
    }
    #[test]
    fn delayed_scroll_rejects_query_close_source_and_request_changes() {
        assert!(scroll_is_current(2, 2, 4, 4, true, true, true));
        assert!(!scroll_is_current(2, 3, 4, 4, true, true, true));
        assert!(!scroll_is_current(2, 2, 4, 5, true, true, true));
        assert!(!scroll_is_current(2, 2, 4, 4, false, true, true));
        assert!(!scroll_is_current(2, 2, 4, 4, true, false, true));
        assert!(!scroll_is_current(2, 2, 4, 4, true, true, false));
    }
}
