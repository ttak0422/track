//! Native single-line search, not a Markdown editor. Offsets in state are UTF-8;
//! only the platform input protocol uses UTF-16. Composition edits also emit queries.
use gpui::{
    App, Bounds, ClipboardItem, Context, CursorStyle, ElementInputHandler, EntityInputHandler,
    EventEmitter, FocusHandle, Focusable, KeyBinding, MouseButton, MouseDownEvent, MouseMoveEvent,
    Pixels, Point, ShapedLine, SharedString, TextRun, UTF16Selection, Window, actions, canvas, div,
    fill, point, prelude::*, px, rgb, size,
};
use std::ops::Range;

actions!(
    track_search,
    [
        Backspace,
        Delete,
        Left,
        Right,
        ExtendLeft,
        ExtendRight,
        Home,
        End,
        ExtendHome,
        ExtendEnd,
        SelectAll,
        Copy,
        Cut,
        Paste,
        PreviousResult,
        NextResult,
        OpenResult,
        DismissSearch,
        FindNext,
        FindPrevious,
        CloseFind
    ]
);

#[derive(Clone, Debug)]
pub struct SearchChanged(pub String);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum InputPurpose {
    WorkspaceSearch,
    ReaderFind,
}
impl InputPurpose {
    fn context(self, composing: bool) -> &'static str {
        match (self, composing) {
            (Self::WorkspaceSearch, false) => "TrackSearch",
            (Self::WorkspaceSearch, true) => "TrackSearch composing",
            (Self::ReaderFind, false) => "TrackReaderFind",
            (Self::ReaderFind, true) => "TrackReaderFind composing",
        }
    }
    fn placeholder(self) -> &'static str {
        match self {
            Self::WorkspaceSearch => "Search notes…",
            Self::ReaderFind => "Find in page",
        }
    }
}

fn emits_unmark(purpose: InputPurpose, was_marked: bool) -> bool {
    purpose == InputPurpose::ReaderFind && was_marked
}

fn emits_change(purpose: InputPurpose, marked: bool) -> bool {
    purpose == InputPurpose::WorkspaceSearch || !marked
}

pub struct SearchInput {
    purpose: InputPurpose,
    dark: bool,
    selected_result: Option<usize>,
    focus: FocusHandle,
    text: String,
    anchor: usize,
    caret: usize,
    marked: Option<Range<usize>>,
    layout: Option<ShapedLine>,
    bounds: Option<Bounds<Pixels>>,
    scroll: Pixels,
    dragging: bool,
}
impl EventEmitter<SearchChanged> for SearchInput {}
impl Focusable for SearchInput {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

// Clamp malformed platform offsets to a scalar boundary, never slice a code point.
fn byte_at_utf16(text: &str, offset: usize) -> usize {
    let mut units = 0;
    for (byte, ch) in text.char_indices() {
        if units + ch.len_utf16() > offset {
            return byte;
        }
        units += ch.len_utf16();
    }
    text.len()
}
fn utf16_at_byte(text: &str, byte: usize) -> usize {
    text.char_indices()
        .take_while(|(i, _)| *i < byte.min(text.len()))
        .map(|(_, c)| c.len_utf16())
        .sum()
}
fn byte_range(text: &str, range: Range<usize>) -> Range<usize> {
    let a = byte_at_utf16(text, range.start);
    let b = byte_at_utf16(text, range.end);
    a.min(b)..a.max(b)
}
fn previous(text: &str, at: usize) -> usize {
    text.char_indices()
        .map(|(i, _)| i)
        .take_while(|i| *i < at)
        .last()
        .unwrap_or(0)
}
fn next(text: &str, at: usize) -> usize {
    text.char_indices()
        .map(|(i, _)| i)
        .find(|i| *i > at)
        .unwrap_or(text.len())
}
fn single_line(text: &str) -> String {
    text.chars()
        .map(|c| {
            if matches!(c, '\r' | '\n' | '\t' | '\u{2028}' | '\u{2029}') {
                ' '
            } else {
                c
            }
        })
        .collect()
}

fn search_bindings() -> Vec<KeyBinding> {
    vec![
        KeyBinding::new("enter", FindNext, Some("TrackReaderFind && !composing")),
        KeyBinding::new(
            "shift-enter",
            FindPrevious,
            Some("TrackReaderFind && !composing"),
        ),
        KeyBinding::new("escape", CloseFind, Some("TrackReaderFind && !composing")),
        KeyBinding::new("up", PreviousResult, Some("TrackSearch && !composing")),
        KeyBinding::new("down", NextResult, Some("TrackSearch && !composing")),
        KeyBinding::new("enter", OpenResult, Some("TrackSearch && !composing")),
        KeyBinding::new("escape", DismissSearch, Some("TrackSearch && !composing")),
        KeyBinding::new(
            "backspace",
            Backspace,
            Some("TrackSearch || TrackReaderFind"),
        ),
        KeyBinding::new("delete", Delete, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("left", Left, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("right", Right, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new(
            "shift-left",
            ExtendLeft,
            Some("TrackSearch || TrackReaderFind"),
        ),
        KeyBinding::new(
            "shift-right",
            ExtendRight,
            Some("TrackSearch || TrackReaderFind"),
        ),
        KeyBinding::new("home", Home, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("end", End, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("cmd-left", Home, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("cmd-right", End, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new(
            "shift-home",
            ExtendHome,
            Some("TrackSearch || TrackReaderFind"),
        ),
        KeyBinding::new(
            "shift-end",
            ExtendEnd,
            Some("TrackSearch || TrackReaderFind"),
        ),
        KeyBinding::new(
            "cmd-shift-left",
            ExtendHome,
            Some("TrackSearch || TrackReaderFind"),
        ),
        KeyBinding::new(
            "cmd-shift-right",
            ExtendEnd,
            Some("TrackSearch || TrackReaderFind"),
        ),
        KeyBinding::new("cmd-a", SelectAll, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("cmd-c", Copy, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("cmd-x", Cut, Some("TrackSearch || TrackReaderFind")),
        KeyBinding::new("cmd-v", Paste, Some("TrackSearch || TrackReaderFind")),
    ]
}

/// Bounded result navigation, never wrapping or returning an invalid row.
fn result_step(selected: Option<usize>, len: usize, forward: bool) -> Option<usize> {
    if len == 0 {
        return None;
    }
    Some(match selected.filter(|i| *i < len) {
        None => {
            if forward {
                0
            } else {
                len - 1
            }
        }
        Some(i) => {
            if forward {
                (i + 1).min(len - 1)
            } else {
                i.saturating_sub(1)
            }
        }
    })
}

impl SearchInput {
    pub fn is_composing(&self) -> bool {
        self.marked.is_some()
    }
    pub fn selected_result(&self) -> Option<usize> {
        self.selected_result
    }
    pub fn step_result(&mut self, len: usize, forward: bool) -> Option<usize> {
        self.selected_result = result_step(self.selected_result, len, forward);
        self.selected_result
    }
    pub fn reset_result(&mut self) {
        self.selected_result = None;
    }

    pub fn query(&self) -> &str {
        &self.text
    }
    pub fn clear(&mut self, cx: &mut Context<Self>) {
        self.selected_result = None;
        self.text.clear();
        self.anchor = 0;
        self.caret = 0;
        self.marked = None;
        cx.notify();
    }
    pub fn select_all(&mut self, cx: &mut Context<Self>) {
        self.anchor = 0;
        self.caret = self.text.len();
        cx.notify();
    }
    pub fn set_dark(&mut self, dark: bool) {
        self.dark = dark;
    }
    pub fn new(cx: &mut Context<Self>) -> Self {
        Self::with_purpose(InputPurpose::WorkspaceSearch, cx)
    }
    pub fn with_purpose(purpose: InputPurpose, cx: &mut Context<Self>) -> Self {
        // Scoped bindings do not interfere with workspace navigation.
        cx.bind_keys(search_bindings());
        Self {
            purpose,
            dark: false,
            selected_result: None,
            focus: cx.focus_handle(),
            text: String::new(),
            anchor: 0,
            caret: 0,
            marked: None,
            layout: None,
            bounds: None,
            scroll: px(0.),
            dragging: false,
        }
    }
    fn selection(&self) -> Range<usize> {
        self.anchor.min(self.caret)..self.anchor.max(self.caret)
    }
    fn utf16_range(&self, r: Range<usize>) -> Range<usize> {
        utf16_at_byte(&self.text, r.start)..utf16_at_byte(&self.text, r.end)
    }
    fn move_caret(&mut self, at: usize, extend: bool, cx: &mut Context<Self>) {
        self.caret = at;
        if !extend {
            self.anchor = at;
        }
        cx.notify();
    }
    fn replace(
        &mut self,
        range: Option<Range<usize>>,
        text: &str,
        marked: bool,
        selected: Option<Range<usize>>,
        cx: &mut Context<Self>,
    ) {
        let range = range
            .map(|r| byte_range(&self.text, r))
            .or_else(|| self.marked.clone())
            .unwrap_or_else(|| self.selection());
        let inserted = single_line(text);
        self.text.replace_range(range.clone(), &inserted);
        self.marked =
            (marked && !inserted.is_empty()).then_some(range.start..range.start + inserted.len());
        let selection = selected
            .map(|r| byte_range(&inserted, r))
            .unwrap_or(inserted.len()..inserted.len());
        self.anchor = range.start + selection.start;
        self.caret = range.start + selection.end;
        self.selected_result = None;
        if emits_change(self.purpose, marked) {
            cx.emit(SearchChanged(self.text.clone()));
        }
        cx.notify();
    }
    fn hit(&self, point: Point<Pixels>) -> usize {
        match (&self.layout, self.bounds) {
            (Some(line), Some(bounds)) if !self.text.is_empty() => {
                line.closest_index_for_x(point.x - bounds.left() + self.scroll)
            }
            _ => 0,
        }
    }
    fn copy_selection(&self, cx: &mut Context<Self>) {
        let range = self.selection();
        if !range.is_empty() {
            cx.write_to_clipboard(ClipboardItem::new_string(self.text[range].to_owned()));
        }
    }
}

impl EntityInputHandler for SearchInput {
    fn text_for_range(
        &mut self,
        r: Range<usize>,
        actual: &mut Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<String> {
        let r = byte_range(&self.text, r);
        *actual = Some(self.utf16_range(r.clone()));
        Some(self.text[r].to_owned())
    }
    fn selected_text_range(
        &mut self,
        _: bool,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<UTF16Selection> {
        Some(UTF16Selection {
            range: self.utf16_range(self.selection()),
            reversed: self.caret < self.anchor,
        })
    }
    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        self.marked.clone().map(|r| self.utf16_range(r))
    }
    fn unmark_text(&mut self, _: &mut Window, cx: &mut Context<Self>) {
        let was_marked = self.marked.take().is_some();
        if emits_unmark(self.purpose, was_marked) {
            cx.emit(SearchChanged(self.text.clone()));
        }
        cx.notify();
    }
    fn replace_text_in_range(
        &mut self,
        r: Option<Range<usize>>,
        text: &str,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        crate::input_trace::platform_text(text, false);
        self.replace(r, text, false, None, cx);
    }
    fn replace_and_mark_text_in_range(
        &mut self,
        r: Option<Range<usize>>,
        text: &str,
        selected: Option<Range<usize>>,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        crate::input_trace::platform_text(text, true);
        self.replace(r, text, true, selected, cx);
    }
    fn bounds_for_range(
        &mut self,
        r: Range<usize>,
        _: Bounds<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<Bounds<Pixels>> {
        let b = self.bounds?;
        let line = self.layout.as_ref()?;
        let r = byte_range(&self.text, r);
        Some(Bounds::from_corners(
            point(b.left() + line.x_for_index(r.start) - self.scroll, b.top()),
            point(b.left() + line.x_for_index(r.end) - self.scroll, b.bottom()),
        ))
    }
    fn character_index_for_point(
        &mut self,
        p: Point<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<usize> {
        self.bounds?;
        Some(utf16_at_byte(&self.text, self.hit(p)))
    }
}

impl Render for SearchInput {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let input = cx.entity();
        let dark = self.dark;
        let focused = self.focus.is_focused(window);
        div()
            .w_full()
            .min_w_0()
            .p_2()
            .bg(rgb(if dark { 0x191c1e } else { 0xffffff }))
            .border_b_1()
            .border_color(rgb(if focused {
                if dark { 0xf4785e } else { 0xc13a1e }
            } else if dark {
                0x3e4347
            } else {
                0xc7c5bd
            }))
            .text_color(rgb(if dark { 0xe9e9e4 } else { 0x1a1a18 }))
            .text_size(px(16.))
            .key_context(self.purpose.context(self.is_composing()))
            .track_focus(&self.focus)
            .cursor(CursorStyle::IBeam)
            .overflow_hidden()
            .on_action(cx.listener(|s, _: &Left, _, cx| {
                let at = if s.selection().is_empty() {
                    previous(&s.text, s.caret)
                } else {
                    s.selection().start
                };
                s.move_caret(at, false, cx);
            }))
            .on_action(cx.listener(|s, _: &Right, _, cx| {
                let at = if s.selection().is_empty() {
                    next(&s.text, s.caret)
                } else {
                    s.selection().end
                };
                s.move_caret(at, false, cx);
            }))
            .on_action(cx.listener(|s, _: &ExtendLeft, _, cx| {
                s.move_caret(previous(&s.text, s.caret), true, cx)
            }))
            .on_action(cx.listener(|s, _: &ExtendRight, _, cx| {
                s.move_caret(next(&s.text, s.caret), true, cx)
            }))
            .on_action(cx.listener(|s, _: &Home, _, cx| s.move_caret(0, false, cx)))
            .on_action(cx.listener(|s, _: &End, _, cx| s.move_caret(s.text.len(), false, cx)))
            .on_action(cx.listener(|s, _: &ExtendHome, _, cx| s.move_caret(0, true, cx)))
            .on_action(cx.listener(|s, _: &ExtendEnd, _, cx| s.move_caret(s.text.len(), true, cx)))
            .on_action(cx.listener(|s, _: &SelectAll, _, cx| {
                s.anchor = 0;
                s.move_caret(s.text.len(), true, cx);
            }))
            .on_action(cx.listener(|s, _: &Backspace, _, cx| {
                if s.selection().is_empty() {
                    s.anchor = previous(&s.text, s.caret);
                }
                s.replace(None, "", false, None, cx);
            }))
            .on_action(cx.listener(|s, _: &Delete, _, cx| {
                if s.selection().is_empty() {
                    s.anchor = next(&s.text, s.caret);
                }
                s.replace(None, "", false, None, cx);
            }))
            .on_action(cx.listener(|s, _: &Copy, _, cx| s.copy_selection(cx)))
            .on_action(cx.listener(|s, _: &Cut, _, cx| {
                s.copy_selection(cx);
                if !s.selection().is_empty() {
                    s.replace(None, "", false, None, cx);
                }
            }))
            .on_action(cx.listener(|s, _: &Paste, _, cx| {
                if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) {
                    s.replace(None, &text, false, None, cx);
                }
            }))
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|s, e: &MouseDownEvent, w, cx| {
                    w.focus(&s.focus);
                    s.dragging = true;
                    s.move_caret(s.hit(e.position), e.modifiers.shift, cx);
                }),
            )
            .on_mouse_move(cx.listener(|s, e: &MouseMoveEvent, _, cx| {
                if s.dragging {
                    s.move_caret(s.hit(e.position), true, cx);
                }
            }))
            .on_mouse_up(
                MouseButton::Left,
                cx.listener(|s, _, _, _| s.dragging = false),
            )
            .on_mouse_up_out(
                MouseButton::Left,
                cx.listener(|s, _, _, _| s.dragging = false),
            )
            .child(
                canvas(
                    |_, _, _| (),
                    move |bounds, _, window, cx| {
                        input.update(cx, |s, cx| {
                            let style = window.text_style();
                            let display: SharedString = if s.text.is_empty() {
                                s.purpose.placeholder().into()
                            } else {
                                s.text.clone().into()
                            };
                            let run = TextRun {
                                len: display.len(),
                                font: style.font(),
                                color: if s.text.is_empty() {
                                    rgb(if dark { 0x969792 } else { 0x878780 }).into()
                                } else {
                                    style.color
                                },
                                background_color: None,
                                underline: None,
                                strikethrough: None,
                            };
                            let line =
                                window
                                    .text_system()
                                    .shape_line(display, px(16.), &[run], None);
                            let x = line.x_for_index(s.caret);
                            s.scroll = s
                                .scroll
                                .min(x)
                                .max(x - bounds.size.width + px(2.))
                                .max(px(0.));
                            let origin = point(bounds.left() - s.scroll, bounds.top());
                            let selection = s.selection();
                            if !selection.is_empty() {
                                window.paint_quad(fill(
                                    Bounds::from_corners(
                                        point(
                                            origin.x + line.x_for_index(selection.start),
                                            bounds.top(),
                                        ),
                                        point(
                                            origin.x + line.x_for_index(selection.end),
                                            bounds.bottom(),
                                        ),
                                    ),
                                    rgb(if dark { 0x573a32 } else { 0xf2d9cf }),
                                ));
                            }
                            let _ = line.paint(origin, bounds.size.height, window, cx);
                            if let Some(marked) = &s.marked {
                                window.paint_quad(fill(
                                    Bounds::new(
                                        point(
                                            origin.x + line.x_for_index(marked.start),
                                            bounds.bottom() - px(1.),
                                        ),
                                        size(
                                            line.x_for_index(marked.end)
                                                - line.x_for_index(marked.start),
                                            px(1.),
                                        ),
                                    ),
                                    rgb(if dark { 0xf4785e } else { 0xc13a1e }),
                                ));
                            }
                            if s.focus.is_focused(window) {
                                window.paint_quad(fill(
                                    Bounds::new(
                                        point(origin.x + x, bounds.top()),
                                        size(px(1.), bounds.size.height),
                                    ),
                                    rgb(if dark { 0xf4785e } else { 0xc13a1e }),
                                ));
                            }
                            window.handle_input(
                                &s.focus,
                                ElementInputHandler::new(bounds, input.clone()),
                                cx,
                            );
                            s.layout = Some(line);
                            s.bounds = Some(bounds);
                        });
                    },
                )
                .w_full()
                .h(px(22.)),
            )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn find_only_emits_committed_changes() {
        assert!(emits_unmark(InputPurpose::ReaderFind, true));
        assert!(!emits_unmark(InputPurpose::ReaderFind, false));
        assert!(!emits_unmark(InputPurpose::WorkspaceSearch, true));
        assert!(!emits_change(InputPurpose::ReaderFind, true));
        assert!(emits_change(InputPurpose::ReaderFind, false));
        assert!(emits_change(InputPurpose::WorkspaceSearch, true));
        assert!(emits_change(InputPurpose::WorkspaceSearch, false));
    }
    #[test]
    fn reader_find_keys_are_independent_and_composition_safe() {
        let keymap = gpui::Keymap::new(search_bindings());
        let find = [gpui::KeyContext::parse(InputPurpose::ReaderFind.context(false)).unwrap()];
        for (key, action) in [
            ("enter", Box::new(FindNext) as Box<dyn gpui::Action>),
            ("shift-enter", Box::new(FindPrevious)),
            ("escape", Box::new(CloseFind)),
            ("cmd-a", Box::new(SelectAll)),
            ("backspace", Box::new(Backspace)),
        ] {
            let (matches, pending) =
                keymap.bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &find);
            assert!(!pending);
            assert_eq!(matches.len(), 1, "{key}");
            assert!(matches[0].action().partial_eq(action.as_ref()), "{key}");
        }
        for purpose in [InputPurpose::WorkspaceSearch, InputPurpose::ReaderFind] {
            let contexts = [gpui::KeyContext::parse(purpose.context(true)).unwrap()];
            for key in ["enter", "shift-enter", "escape"] {
                assert!(
                    keymap
                        .bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts)
                        .0
                        .is_empty()
                );
            }
        }
        for key in ["up", "down"] {
            assert!(
                keymap
                    .bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &find)
                    .0
                    .is_empty()
            );
        }
        assert_eq!(InputPurpose::WorkspaceSearch.placeholder(), "Search notes…");
        assert_eq!(InputPurpose::ReaderFind.placeholder(), "Find in page");
    }
    #[test]
    fn composition_does_not_bind_result_navigation_or_dismissal() {
        let keymap = gpui::Keymap::new(search_bindings());
        let contexts =
            [gpui::KeyContext::parse(InputPurpose::WorkspaceSearch.context(true)).unwrap()];
        for key in ["up", "down", "enter", "escape"] {
            assert!(
                keymap
                    .bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts)
                    .0
                    .is_empty(),
                "{key}"
            );
        }
    }
    #[test]
    fn result_navigation_is_bounded_and_handles_replaced_lists() {
        assert_eq!(result_step(None, 0, true), None);
        assert_eq!(result_step(Some(9), 0, false), None);
        assert_eq!(result_step(None, 3, true), Some(0));
        assert_eq!(result_step(None, 3, false), Some(2));
        assert_eq!(result_step(Some(0), 3, false), Some(0));
        assert_eq!(result_step(Some(2), 3, true), Some(2));
        assert_eq!(result_step(Some(1), 3, true), Some(2));
        assert_eq!(result_step(Some(1), 3, false), Some(0));
        assert_eq!(result_step(Some(20), 2, true), Some(0));
    }
    #[test]
    fn result_keys_are_scoped_and_modified_keys_not_captured() {
        let keymap = gpui::Keymap::new(search_bindings());
        for context in ["TrackSourceEditor", ""] {
            let contexts = [gpui::KeyContext::parse(context).unwrap()];
            for key in ["up", "down", "enter", "escape"] {
                assert!(
                    keymap
                        .bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts)
                        .0
                        .is_empty()
                );
            }
        }
        let contexts = [gpui::KeyContext::parse("TrackSearch").unwrap()];
        for key in ["cmd-up", "shift-down", "cmd-enter"] {
            assert!(
                keymap
                    .bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts)
                    .0
                    .is_empty()
            );
        }
    }
    #[test]
    fn utf16_round_trips_scalar_boundaries() {
        let text = "a日本😀e\u{301}";
        for i in text.char_indices().map(|(i, _)| i).chain([text.len()]) {
            assert_eq!(byte_at_utf16(text, utf16_at_byte(text, i)), i);
        }
    }
    #[test]
    fn malformed_ranges_are_safe() {
        assert_eq!(byte_range("😀日", 1..99), 0..7);
        assert_eq!(byte_range("😀日", 3..0), 0..7);
        assert_eq!(byte_range("", 99..0), 0..0);
    }
    #[test]
    fn movement_keeps_utf8_boundaries() {
        assert_eq!(next("日😀a", 0), 3);
        assert_eq!(previous("日😀a", 7), 3);
        assert_eq!(next("日😀a", 8), 8);
        assert_eq!(previous("日😀a", 0), 0);
    }
    #[test]
    fn search_is_single_line() {
        assert_eq!(single_line("a\r\nb\tc\u{2028}d"), "a  b c d");
    }
    #[test]
    fn native_command_and_deletion_keys_resolve_in_search_context() {
        let mut bindings = search_bindings();
        bindings.extend(crate::workspace_key_bindings());
        let keymap = gpui::Keymap::new(bindings);
        let contexts = [gpui::KeyContext::parse("TrackSearch").unwrap()];
        for (key, action) in [
            ("cmd-a", Box::new(SelectAll) as Box<dyn gpui::Action>),
            ("backspace", Box::new(Backspace)),
            ("delete", Box::new(Delete)),
            ("cmd-v", Box::new(Paste)),
            ("cmd-q", Box::new(crate::Quit)),
            ("up", Box::new(PreviousResult)),
            ("down", Box::new(NextResult)),
            ("enter", Box::new(OpenResult)),
            ("escape", Box::new(DismissSearch)),
        ] {
            let stroke = gpui::Keystroke::parse(key).unwrap();
            let (matches, pending) = keymap.bindings_for_input(&[stroke], &contexts);
            assert!(!pending, "{key}");
            assert_eq!(matches.len(), 1, "{key}");
            assert!(matches[0].action().partial_eq(action.as_ref()), "{key}");
        }
    }
    #[test]
    fn search_shortcuts_do_not_capture_other_contexts_or_literal_symbols() {
        let keymap = gpui::Keymap::new(search_bindings());
        let other = [gpui::KeyContext::parse("TrackSourceEditor").unwrap()];
        assert!(
            keymap
                .bindings_for_input(&[gpui::Keystroke::parse("cmd-a").unwrap()], &other)
                .0
                .is_empty()
        );
        let search = [gpui::KeyContext::parse("TrackSearch").unwrap()];
        // A text glyph is never reinterpreted as a deletion command.
        assert!(
            keymap
                .bindings_for_input(&[gpui::Keystroke::parse("▽").unwrap()], &search)
                .0
                .is_empty()
        );
    }
}
