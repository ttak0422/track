//! Plain-source editor: no persistence, parsing, automatic loading, or save policy.
//! State uses UTF-8; platform IME protocol uses UTF-16. User navigation/deletion
//! uses extended grapheme clusters; platform ranges retain scalar precision. No wrapping or blink yet.
//! Undo keeps 256 complete edit transactions; each IME composition is one transaction.
//! Parent must protect dirty text before calling set_text or replacing this entity.
use gpui::{
    App, Bounds, ClipboardItem, Context, CursorStyle, ElementInputHandler, EntityInputHandler,
    EventEmitter, FocusHandle, Focusable, KeyBinding, MouseButton, MouseDownEvent, MouseMoveEvent,
    Pixels, Point, ScrollWheelEvent, ShapedLine, SharedString, TextRun, UTF16Selection, Window,
    actions, canvas, div, fill, point, prelude::*, px, rgb, size,
};
use std::ops::Range;
use unicode_segmentation::UnicodeSegmentation;

actions!(
    track_source_editor,
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
        Newline,
        Indent,
        Up,
        Down,
        ExtendUp,
        ExtendDown,
        DocumentHome,
        DocumentEnd,
        Undo,
        Redo
    ]
);

#[derive(Clone, Debug)]
pub struct EditorChanged(pub String);

pub struct TextEditor {
    focus: FocusHandle,
    mutation_guard: MutationGuard,
    dark: bool,
    text: String,
    anchor: usize,
    caret: usize,
    marked: Option<Range<usize>>,
    layout: Vec<EditorLine>,
    baseline: String,
    history: EditHistory,
    preferred_column: Option<usize>,
    reveal_caret: bool,
    bounds: Option<Bounds<Pixels>>,
    scroll: Point<Pixels>,
    dragging: bool,
}
/// Matches the shell's paper/ink/vermilion and search selection colors.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct EditorPalette {
    paper: u32,
    ink: u32,
    line: u32,
    mark: u32,
    selection: u32,
}
impl EditorPalette {
    fn new(dark: bool) -> Self {
        if dark {
            Self {
                paper: 0x191c1e,
                ink: 0xe9e9e4,
                line: 0x282c2f,
                mark: 0xf4785e,
                selection: 0x573a32,
            }
        } else {
            Self {
                paper: 0xffffff,
                ink: 0x1a1a18,
                line: 0xe6e4de,
                mark: 0xc13a1e,
                selection: 0xf2d9cf,
            }
        }
    }
}
/// One mutation gate shared by keyboard, platform IME, history and host resets.
#[derive(Default)]
struct MutationGuard {
    read_only: bool,
}
impl MutationGuard {
    fn set_read_only(&mut self, read_only: bool, composing: bool) -> Result<(), &'static str> {
        if read_only && composing {
            return Err("Finish or cancel IME composition before locking the editor.");
        }
        self.read_only = read_only;
        Ok(())
    }
    fn require_edit(&self) -> Result<(), &'static str> {
        if self.read_only {
            Err("The editor is locked.")
        } else {
            Ok(())
        }
    }
}
/// Acknowledgment changes only the clean baseline, never the authored buffer.
fn acknowledge_baseline(
    baseline: &mut String,
    current: &str,
    acknowledged: &str,
    composing: bool,
) -> Result<(), &'static str> {
    if composing {
        return Err("Finish or cancel IME composition before acknowledging the editor buffer.");
    }
    if current != acknowledged {
        return Err("The acknowledged text differs from the current editor buffer.");
    }
    *baseline = current.to_owned();
    Ok(())
}
impl EventEmitter<EditorChanged> for TextEditor {}
impl Focusable for TextEditor {
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
    text.grapheme_indices(true)
        .map(|(i, _)| i)
        .take_while(|i| *i < at)
        .last()
        .unwrap_or(0)
}
fn next(text: &str, at: usize) -> usize {
    text.grapheme_indices(true)
        .map(|(i, _)| i)
        .find(|i| *i > at)
        .unwrap_or(text.len())
}
fn grapheme_floor(text: &str, at: usize) -> usize {
    if at >= text.len() {
        return text.len();
    }
    text.grapheme_indices(true)
        .map(|(i, _)| i)
        .take_while(|i| *i <= at)
        .last()
        .unwrap_or(0)
}
fn grapheme_ceil(text: &str, at: usize) -> usize {
    text.grapheme_indices(true)
        .map(|(i, _)| i)
        .find(|i| *i >= at)
        .unwrap_or(text.len())
}
fn deletion_range(text: &str, selected: Range<usize>, forward: bool) -> Range<usize> {
    let start = grapheme_floor(text, selected.start);
    let end = grapheme_ceil(text, selected.end);
    if start != end {
        return start..end;
    }
    if forward {
        start..next(text, end)
    } else {
        previous(text, start)..end
    }
}

/// Complete state snapshots make undo lossless for raw source and reversed selections.
#[derive(Clone, Debug, PartialEq, Eq)]
struct EditSnapshot {
    text: String,
    anchor: usize,
    caret: usize,
}
/// Platform-provided ranges are UTF-16 and need scalar, not grapheme, precision.
fn apply_replacement(
    before: &EditSnapshot,
    marked_range: Option<Range<usize>>,
    range_utf16: Option<Range<usize>>,
    inserted: &str,
    composing: bool,
    selected_utf16: Option<Range<usize>>,
) -> (EditSnapshot, Option<Range<usize>>) {
    let range = range_utf16
        .map(|r| byte_range(&before.text, r))
        .or(marked_range)
        .unwrap_or_else(|| selection_range(before.anchor, before.caret));
    let mut text = before.text.clone();
    text.replace_range(range.clone(), inserted);
    let selected = selected_utf16
        .map(|r| byte_range(inserted, r))
        .unwrap_or(inserted.len()..inserted.len());
    let marked =
        (composing && !inserted.is_empty()).then_some(range.start..range.start + inserted.len());
    (
        EditSnapshot {
            text,
            anchor: range.start + selected.start,
            caret: range.start + selected.end,
        },
        marked,
    )
}
const HISTORY_LIMIT: usize = 256;
#[derive(Default)]
struct EditHistory {
    undo: Vec<EditSnapshot>,
    redo: Vec<EditSnapshot>,
    composing: Option<EditSnapshot>,
}
impl EditHistory {
    fn record(&mut self, before: EditSnapshot, after: &EditSnapshot, composing: bool) {
        if composing {
            // Keep the state before the FIRST preedit, not each replacement of it.
            if self.composing.is_none() {
                self.composing = Some(before);
            }
            return;
        }
        let before = self.composing.take().unwrap_or(before);
        self.commit(before, after);
    }
    fn commit(&mut self, before: EditSnapshot, after: &EditSnapshot) {
        if before.text == after.text {
            return;
        }
        if self.undo.len() == HISTORY_LIMIT {
            self.undo.remove(0);
        }
        self.undo.push(before);
        self.redo.clear();
    }
    fn finish_composition(&mut self, after: &EditSnapshot) {
        if let Some(before) = self.composing.take() {
            self.commit(before, after);
        }
    }
    fn undo(&mut self, current: EditSnapshot) -> Option<EditSnapshot> {
        // The platform owns active preedit. Never erase it behind the IME's back.
        if self.composing.is_some() {
            return None;
        }
        let before = self.undo.pop()?;
        self.redo.push(current);
        Some(before)
    }
    fn redo(&mut self, current: EditSnapshot) -> Option<EditSnapshot> {
        if self.composing.is_some() {
            return None;
        }
        let after = self.redo.pop()?;
        self.undo.push(current);
        Some(after)
    }
}

const LINE_HEIGHT: f32 = 22.;
const TAB_WIDTH: usize = 4;

struct EditorLine {
    source: Range<usize>,
    shaped: ShapedLine,
    // Exact source/display byte boundaries, including the end boundary.
    map: Vec<(usize, usize)>,
}
impl EditorLine {
    fn x(&self, byte: usize) -> Pixels {
        let local = byte.saturating_sub(self.source.start);
        let display = self
            .map
            .iter()
            .rev()
            .find(|(s, _)| *s <= local)
            .map(|(_, d)| *d)
            .unwrap_or(0);
        self.shaped.x_for_index(display)
    }
    fn byte_for_x(&self, x: Pixels) -> usize {
        let display = self.shaped.closest_index_for_x(x);
        let local = self
            .map
            .iter()
            .min_by_key(|(_, d)| d.abs_diff(display))
            .map(|(s, _)| *s)
            .unwrap_or(0);
        self.source.start + local
    }
}
fn line_ranges(text: &str) -> Vec<Range<usize>> {
    let mut start = 0;
    let mut result = Vec::new();
    for (at, ch) in text.char_indices() {
        if ch == '\n' {
            result.push(start..at);
            start = at + 1;
        }
    }
    result.push(start..text.len());
    result
}
fn line_index(ranges: &[Range<usize>], byte: usize) -> usize {
    ranges
        .partition_point(|r| r.start <= byte)
        .saturating_sub(1)
}
fn display_line(source: &str) -> (String, Vec<(usize, usize)>) {
    let mut display = String::new();
    let mut map = Vec::new();
    let mut column = 0;
    for (at, cluster) in source.grapheme_indices(true) {
        map.push((at, display.len()));
        if cluster == "\t" {
            let spaces = TAB_WIDTH - column % TAB_WIDTH;
            display.extend(std::iter::repeat_n(' ', spaces));
            column += spaces;
        } else {
            // Preserve CR in source (including CRLF), but make it visible rather
            // than giving the shaper an inline control character.
            display.push_str(if cluster == "\r" { "␍" } else { cluster });
            column += 1;
        }
    }
    map.push((source.len(), display.len()));
    (display, map)
}
fn visual_column(text: &str) -> usize {
    text.graphemes(true).fold(0, |col, cluster| {
        col + if cluster == "\t" {
            TAB_WIDTH - col % TAB_WIDTH
        } else {
            1
        }
    })
}
fn byte_at_column(text: &str, target: usize) -> usize {
    let mut col = 0;
    for (i, cluster) in text.grapheme_indices(true) {
        let width = if cluster == "\t" {
            TAB_WIDTH - col % TAB_WIDTH
        } else {
            1
        };
        if col + width > target {
            return i;
        }
        col += width;
    }
    text.len()
}
fn vertical_target(
    text: &str,
    caret: usize,
    direction: isize,
    preferred: Option<usize>,
) -> (usize, usize) {
    let ranges = line_ranges(text);
    let row = line_index(&ranges, caret);
    let col = preferred.unwrap_or_else(|| visual_column(&text[ranges[row].start..caret]));
    let target = row.saturating_add_signed(direction).min(ranges.len() - 1);
    (
        ranges[target].start + byte_at_column(&text[ranges[target].clone()], col),
        col,
    )
}
fn selection_range(anchor: usize, caret: usize) -> Range<usize> {
    anchor.min(caret)..anchor.max(caret)
}

impl TextEditor {
    pub fn new(text: impl Into<String>, cx: &mut Context<Self>) -> Self {
        let text = text.into();
        // Scoped bindings do not interfere with workspace navigation.
        cx.bind_keys([
            KeyBinding::new("cmd-z", Undo, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-shift-z", Redo, Some("TrackSourceEditor")),
            KeyBinding::new("enter", Newline, Some("TrackSourceEditor")),
            KeyBinding::new("tab", Indent, Some("TrackSourceEditor")),
            KeyBinding::new("up", Up, Some("TrackSourceEditor")),
            KeyBinding::new("down", Down, Some("TrackSourceEditor")),
            KeyBinding::new("shift-up", ExtendUp, Some("TrackSourceEditor")),
            KeyBinding::new("shift-down", ExtendDown, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-up", DocumentHome, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-down", DocumentEnd, Some("TrackSourceEditor")),
            KeyBinding::new("backspace", Backspace, Some("TrackSourceEditor")),
            KeyBinding::new("delete", Delete, Some("TrackSourceEditor")),
            KeyBinding::new("left", Left, Some("TrackSourceEditor")),
            KeyBinding::new("right", Right, Some("TrackSourceEditor")),
            KeyBinding::new("shift-left", ExtendLeft, Some("TrackSourceEditor")),
            KeyBinding::new("shift-right", ExtendRight, Some("TrackSourceEditor")),
            KeyBinding::new("home", Home, Some("TrackSourceEditor")),
            KeyBinding::new("end", End, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-left", Home, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-right", End, Some("TrackSourceEditor")),
            KeyBinding::new("shift-home", ExtendHome, Some("TrackSourceEditor")),
            KeyBinding::new("shift-end", ExtendEnd, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-shift-left", ExtendHome, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-shift-right", ExtendEnd, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-a", SelectAll, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-c", Copy, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-x", Cut, Some("TrackSourceEditor")),
            KeyBinding::new("cmd-v", Paste, Some("TrackSourceEditor")),
        ]);
        Self {
            focus: cx.focus_handle(),
            mutation_guard: MutationGuard::default(),
            dark: false,
            baseline: text.clone(),
            history: EditHistory::default(),
            text,
            preferred_column: None,
            reveal_caret: true,
            anchor: 0,
            caret: 0,
            marked: None,
            layout: Vec::new(),
            bounds: None,
            scroll: point(px(0.), px(0.)),
            dragging: false,
        }
    }
    pub fn set_dark(&mut self, dark: bool, cx: &mut Context<Self>) {
        if self.dark != dark {
            self.dark = dark;
            cx.notify();
        }
    }
    /// Lock before preparing a save, after synchronizing the draft and checking IME.
    /// A lock request during composition fails without changing state or text.
    pub fn set_read_only(
        &mut self,
        read_only: bool,
        cx: &mut Context<Self>,
    ) -> Result<(), &'static str> {
        self.mutation_guard
            .set_read_only(read_only, self.is_composing())?;
        cx.notify();
        Ok(())
    }
    pub fn is_read_only(&self) -> bool {
        self.mutation_guard.read_only
    }
    pub fn text(&self) -> &str {
        &self.text
    }
    pub fn is_dirty(&self) -> bool {
        self.text != self.baseline
    }
    /// Mark this exact buffer acknowledged without resetting undo, redo, selection,
    /// scroll, or text. Allowed while locked after a save acknowledgment. Emits no
    /// EditorChanged event. The caller must validate the save identity/revision.
    pub fn mark_saved(
        &mut self,
        acknowledged_text: &str,
        cx: &mut Context<Self>,
    ) -> Result<(), &'static str> {
        let composing = self.is_composing();
        acknowledge_baseline(&mut self.baseline, &self.text, acknowledged_text, composing)?;
        cx.notify();
        Ok(())
    }
    /// Explicitly replace the buffer and reset its baseline; emits no edit event.
    /// The parent must first resolve unsaved edits, unlock, and finish composition.
    /// Returns an error rather than dropping text while locked/composing. Never call during render.
    pub fn set_text(
        &mut self,
        text: impl Into<String>,
        cx: &mut Context<Self>,
    ) -> Result<(), &'static str> {
        self.mutation_guard.require_edit()?;
        if self.is_composing() {
            return Err("Finish or cancel IME composition before replacing the editor buffer.");
        }
        self.text = text.into();
        self.baseline = self.text.clone();
        self.history = EditHistory::default();
        self.anchor = 0;
        self.caret = 0;
        self.marked = None;
        self.layout.clear();
        self.preferred_column = None;
        self.scroll = point(px(0.), px(0.));
        self.reveal_caret = true;
        self.dragging = false;
        cx.notify();
        Ok(())
    }
    pub fn is_composing(&self) -> bool {
        self.history.composing.is_some()
    }
    fn snapshot(&self) -> EditSnapshot {
        EditSnapshot {
            text: self.text.clone(),
            anchor: self.anchor,
            caret: self.caret,
        }
    }
    fn restore(&mut self, snapshot: EditSnapshot, cx: &mut Context<Self>) {
        if self.mutation_guard.require_edit().is_err() {
            return;
        }
        self.text = snapshot.text;
        self.anchor = snapshot.anchor;
        self.caret = snapshot.caret;
        self.marked = None;
        self.layout.clear();
        self.preferred_column = None;
        self.reveal_caret = true;
        cx.emit(EditorChanged(self.text.clone()));
        cx.notify();
    }
    fn undo(&mut self, redo: bool, cx: &mut Context<Self>) {
        if self.mutation_guard.require_edit().is_err() {
            return;
        }
        let current = self.snapshot();
        let target = if redo {
            self.history.redo(current)
        } else {
            self.history.undo(current)
        };
        if let Some(target) = target {
            self.restore(target, cx);
        }
    }
    /// Non-IME editing commands must not consume the entire active marked range.
    fn insert_command(&mut self, text: &str, cx: &mut Context<Self>) {
        if self.mutation_guard.require_edit().is_err() {
            return;
        }
        if !self.is_composing() {
            self.replace(None, text, false, None, cx);
        }
    }
    fn delete_command(&mut self, forward: bool, cx: &mut Context<Self>) {
        if self.mutation_guard.require_edit().is_err() {
            return;
        }
        if self.is_composing() {
            return;
        }
        let range = deletion_range(&self.text, self.selection(), forward);
        self.replace(Some(self.utf16_range(range)), "", false, None, cx);
    }
    fn vertical(&mut self, direction: isize, extend: bool, cx: &mut Context<Self>) {
        let (at, col) = vertical_target(&self.text, self.caret, direction, self.preferred_column);
        self.move_caret(at, extend, cx);
        self.preferred_column = Some(col);
    }
    fn line_edge(&self, end: bool) -> usize {
        let ranges = line_ranges(&self.text);
        let row = line_index(&ranges, self.caret);
        if end {
            ranges[row].end
        } else {
            ranges[row].start
        }
    }
    fn selection(&self) -> Range<usize> {
        selection_range(self.anchor, self.caret)
    }
    fn utf16_range(&self, r: Range<usize>) -> Range<usize> {
        utf16_at_byte(&self.text, r.start)..utf16_at_byte(&self.text, r.end)
    }
    fn move_caret(&mut self, at: usize, extend: bool, cx: &mut Context<Self>) {
        if self.is_composing() {
            return;
        }
        self.preferred_column = None;
        self.reveal_caret = true;
        self.caret = grapheme_floor(&self.text, at);
        if !extend {
            self.anchor = self.caret;
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
        if self.mutation_guard.require_edit().is_err() {
            return;
        }
        let before = self.snapshot();
        let (after, marked_range) =
            apply_replacement(&before, self.marked.clone(), range, text, marked, selected);
        self.text = after.text;
        self.anchor = after.anchor;
        self.caret = after.caret;
        self.marked = marked_range;
        self.history.record(before, &self.snapshot(), marked);
        self.layout.clear();
        self.preferred_column = None;
        self.reveal_caret = true;
        cx.emit(EditorChanged(self.text.clone()));
        cx.notify();
    }
    fn hit(&self, position: Point<Pixels>) -> usize {
        let Some(bounds) = self.bounds else {
            return self.caret;
        };
        if self.layout.is_empty() {
            return self.caret;
        }
        let row = (((position.y - bounds.top() + self.scroll.y) / px(LINE_HEIGHT))
            .floor()
            .max(0.) as usize)
            .min(self.layout.len() - 1);
        grapheme_floor(
            &self.text,
            self.layout[row].byte_for_x(position.x - bounds.left() + self.scroll.x),
        )
    }
    fn copy_selection(&self, cx: &mut Context<Self>) {
        let range = self.selection();
        if !range.is_empty() {
            cx.write_to_clipboard(ClipboardItem::new_string(self.text[range].to_owned()));
        }
    }
}

impl EntityInputHandler for TextEditor {
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
        if self.mutation_guard.require_edit().is_err() {
            return;
        }
        self.marked = None;
        self.history.finish_composition(&self.snapshot());
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
        let r = byte_range(&self.text, r);
        let row = self
            .layout
            .iter()
            .rposition(|line| line.source.start <= r.start)?;
        let line = &self.layout[row];
        let y = b.top() + px(row as f32 * LINE_HEIGHT) - self.scroll.y;
        // IME candidate rectangle uses first visual line when a range spans rows.
        Some(Bounds::from_corners(
            point(b.left() + line.x(r.start) - self.scroll.x, y),
            point(
                b.left() + line.x(r.end.min(line.source.end)) - self.scroll.x,
                y + px(LINE_HEIGHT),
            ),
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

impl Render for TextEditor {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let input = cx.entity();
        let palette = EditorPalette::new(self.dark);
        div()
            .size_full()
            .min_h_0()
            .min_w_0()
            .p_2()
            .bg(rgb(palette.paper))
            .border_1()
            .border_color(rgb(palette.line))
            .text_color(rgb(palette.ink))
            .text_size(px(14.))
            .key_context("TrackSourceEditor")
            .track_focus(&self.focus)
            .cursor(CursorStyle::IBeam)
            .overflow_hidden()
            .on_action(cx.listener(|s, _: &Undo, _, cx| s.undo(false, cx)))
            .on_action(cx.listener(|s, _: &Redo, _, cx| s.undo(true, cx)))
            .on_action(cx.listener(|s, _: &Newline, _, cx| s.insert_command("\n", cx)))
            .on_action(cx.listener(|s, _: &Indent, _, cx| s.insert_command("\t", cx)))
            .on_action(cx.listener(|s, _: &Up, _, cx| s.vertical(-1, false, cx)))
            .on_action(cx.listener(|s, _: &Down, _, cx| s.vertical(1, false, cx)))
            .on_action(cx.listener(|s, _: &ExtendUp, _, cx| s.vertical(-1, true, cx)))
            .on_action(cx.listener(|s, _: &ExtendDown, _, cx| s.vertical(1, true, cx)))
            .on_action(cx.listener(|s, _: &DocumentHome, _, cx| s.move_caret(0, false, cx)))
            .on_action(
                cx.listener(|s, _: &DocumentEnd, _, cx| s.move_caret(s.text.len(), false, cx)),
            )
            .on_scroll_wheel(cx.listener(|s, e: &ScrollWheelEvent, _, cx| {
                let delta = e.delta.pixel_delta(px(LINE_HEIGHT));
                s.scroll.x = (s.scroll.x - delta.x).max(px(0.));
                s.scroll.y = (s.scroll.y - delta.y).max(px(0.));
                s.reveal_caret = false;
                cx.notify();
            }))
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
            .on_action(
                cx.listener(|s, _: &Home, _, cx| s.move_caret(s.line_edge(false), false, cx)),
            )
            .on_action(cx.listener(|s, _: &End, _, cx| s.move_caret(s.line_edge(true), false, cx)))
            .on_action(
                cx.listener(|s, _: &ExtendHome, _, cx| s.move_caret(s.line_edge(false), true, cx)),
            )
            .on_action(
                cx.listener(|s, _: &ExtendEnd, _, cx| s.move_caret(s.line_edge(true), true, cx)),
            )
            .on_action(cx.listener(|s, _: &SelectAll, _, cx| {
                if s.is_composing() {
                    return;
                }
                s.anchor = 0;
                s.move_caret(s.text.len(), true, cx);
            }))
            .on_action(cx.listener(|s, _: &Backspace, _, cx| s.delete_command(false, cx)))
            .on_action(cx.listener(|s, _: &Delete, _, cx| s.delete_command(true, cx)))
            .on_action(cx.listener(|s, _: &Copy, _, cx| s.copy_selection(cx)))
            .on_action(cx.listener(|s, _: &Cut, _, cx| {
                if s.mutation_guard.require_edit().is_err() {
                    return;
                }
                if s.is_composing() {
                    return;
                }
                s.copy_selection(cx);
                if !s.selection().is_empty() {
                    s.replace(None, "", false, None, cx);
                }
            }))
            .on_action(cx.listener(|s, _: &Paste, _, cx| {
                if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) {
                    s.insert_command(&text, cx);
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
                            let mut lines = Vec::new();
                            let ranges = line_ranges(&s.text);
                            for range in &ranges {
                                let (display, map) = display_line(&s.text[range.clone()]);
                                let display: SharedString = display.into();
                                let run = TextRun {
                                    len: display.len(),
                                    font: style.font(),
                                    color: style.color,
                                    background_color: None,
                                    underline: None,
                                    strikethrough: None,
                                };
                                let shaped =
                                    window
                                        .text_system()
                                        .shape_line(display, px(14.), &[run], None);
                                lines.push(EditorLine {
                                    source: range.clone(),
                                    shaped,
                                    map,
                                });
                            }
                            let row = line_index(&ranges, s.caret);
                            let caret_x = lines[row].x(s.caret);
                            let caret_y = px(row as f32 * LINE_HEIGHT);
                            if s.reveal_caret {
                                s.scroll.x = s
                                    .scroll
                                    .x
                                    .min(caret_x)
                                    .max(caret_x - bounds.size.width + px(2.))
                                    .max(px(0.));
                                s.scroll.y = s
                                    .scroll
                                    .y
                                    .min(caret_y)
                                    .max(caret_y + px(LINE_HEIGHT) - bounds.size.height)
                                    .max(px(0.));
                                s.reveal_caret = false;
                            }
                            let total_height = px(lines.len() as f32 * LINE_HEIGHT);
                            s.scroll.y = s
                                .scroll
                                .y
                                .min((total_height - bounds.size.height).max(px(0.)));
                            let width = lines
                                .iter()
                                .map(|line| line.shaped.width)
                                .fold(px(0.), |a, b| a.max(b));
                            s.scroll.x = s
                                .scroll
                                .x
                                .min((width + px(2.) - bounds.size.width).max(px(0.)));
                            let selection = s.selection();
                            for (index, line) in lines.iter().enumerate() {
                                let origin = point(
                                    bounds.left() - s.scroll.x,
                                    bounds.top() + px(index as f32 * LINE_HEIGHT) - s.scroll.y,
                                );
                                if origin.y + px(LINE_HEIGHT) < bounds.top()
                                    || origin.y > bounds.bottom()
                                {
                                    continue;
                                }
                                let start = selection.start.max(line.source.start);
                                let end = selection.end.min(line.source.end);
                                // Highlight a selected newline even on an empty row.
                                let includes_newline = selection.end > line.source.end
                                    && selection.start <= line.source.end;
                                if start < end || includes_newline {
                                    let left = line.x(start.min(line.source.end));
                                    let right = if includes_newline {
                                        line.x(line.source.end) + px(7.)
                                    } else {
                                        line.x(end)
                                    };
                                    window.paint_quad(fill(
                                        Bounds::new(
                                            point(origin.x + left, origin.y),
                                            size(right - left, px(LINE_HEIGHT)),
                                        ),
                                        rgb(palette.selection),
                                    ));
                                }
                                let _ = line.shaped.paint(origin, px(LINE_HEIGHT), window, cx);
                                if let Some(marked) = &s.marked {
                                    let a = marked.start.max(line.source.start);
                                    let b = marked.end.min(line.source.end);
                                    if a < b {
                                        window.paint_quad(fill(
                                            Bounds::new(
                                                point(
                                                    origin.x + line.x(a),
                                                    origin.y + px(LINE_HEIGHT - 1.),
                                                ),
                                                size(line.x(b) - line.x(a), px(1.)),
                                            ),
                                            rgb(palette.mark),
                                        ));
                                    }
                                }
                            }
                            if !s.is_read_only() && s.focus.is_focused(window) {
                                window.paint_quad(fill(
                                    Bounds::new(
                                        point(
                                            bounds.left() + caret_x - s.scroll.x,
                                            bounds.top() + caret_y - s.scroll.y,
                                        ),
                                        size(px(1.), px(LINE_HEIGHT)),
                                    ),
                                    rgb(palette.mark),
                                ));
                            }
                            if !s.is_read_only() {
                                window.handle_input(
                                    &s.focus,
                                    ElementInputHandler::new(bounds, input.clone()),
                                    cx,
                                );
                            }
                            s.layout = lines;
                            s.bounds = Some(bounds);
                        });
                    },
                )
                .size_full(),
            )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
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
    fn empty_and_trailing_rows_have_positions() {
        assert_eq!(line_ranges(""), vec![0..0]);
        assert_eq!(line_ranges("日\n\n"), vec![0..3, 4..4, 5..5]);
        assert_eq!(line_index(&line_ranges("日\n"), 3), 0);
        assert_eq!(line_index(&line_ranges("日\n"), 4), 1);
    }
    #[test]
    fn vertical_navigation_preserves_column_through_short_rows() {
        let text = "abcd\nx\n日本語a";
        let (at, col) = vertical_target(text, 3, 1, None);
        assert_eq!((at, col), (6, 3));
        assert_eq!(vertical_target(text, at, 1, Some(col)), (16, 3));
        assert_eq!(vertical_target(text, 16, -1, Some(col)), (6, 3));
    }
    #[test]
    fn tabs_have_reversible_source_boundaries() {
        let (display, map) = display_line("日\t😀\r");
        assert_eq!(display, "日   😀␍");
        assert_eq!(map, vec![(0, 0), (3, 3), (4, 6), (8, 10), (9, 13)]);
        assert_eq!(byte_at_column("日\t😀", 4), 4);
        assert_eq!(byte_at_column("日\t😀", 2), 3);
    }
    #[test]
    fn selection_direction_does_not_change_replacement_range() {
        assert_eq!(selection_range(7, 3), 3..7);
        assert_eq!(selection_range(3, 7), 3..7);
        assert_eq!(selection_range(3, 3), 3..3);
    }
    fn snapshot(text: &str, anchor: usize, caret: usize) -> EditSnapshot {
        EditSnapshot {
            text: text.into(),
            anchor,
            caret,
        }
    }
    #[test]
    fn undo_redo_preserves_raw_source_and_reversed_selection() {
        let original = snapshot("a\r\n日\t😀", 11, 3);
        let edited = snapshot("a\r\nreplacement", 14, 14);
        let mut history = EditHistory::default();
        history.record(original.clone(), &edited, false);
        assert_eq!(history.undo(edited.clone()), Some(original.clone()));
        assert_eq!(history.redo(original), Some(edited));
    }
    #[test]
    fn composition_is_one_transaction_and_undo_does_not_erase_preedit() {
        let original = snapshot("prefix 尾", 7, 7);
        let first = snapshot("prefix に尾", 10, 10);
        let second = snapshot("prefix 日本尾", 13, 13);
        let committed = snapshot("prefix 日本語尾", 16, 16);
        let mut history = EditHistory::default();
        history.record(original.clone(), &first, true);
        history.record(first, &second, true);
        assert!(history.undo(second.clone()).is_none());
        assert!(history.redo(second.clone()).is_none());
        history.record(second, &committed, false);
        assert_eq!(history.undo.len(), 1);
        assert_eq!(history.undo(committed.clone()), Some(original.clone()));
        assert_eq!(history.redo(original), Some(committed));
    }
    #[test]
    fn unmark_commits_and_cancelled_composition_is_not_history() {
        let original = snapshot("raw\r\n", 0, 0);
        let preedit = snapshot("語raw\r\n", 3, 3);
        let mut history = EditHistory::default();
        history.record(original.clone(), &preedit, true);
        history.finish_composition(&preedit);
        assert_eq!(history.undo(preedit.clone()), Some(original.clone()));
        history.record(original.clone(), &preedit, true);
        history.record(preedit, &original, false);
        assert!(history.undo.is_empty());
        // A no-op/cancel does not destroy an existing redo branch.
        assert_eq!(history.redo.len(), 1);
    }
    #[test]
    fn new_edit_invalidates_redo_and_history_is_bounded() {
        let mut history = EditHistory::default();
        history.record(snapshot("a", 1, 1), &snapshot("ab", 2, 2), false);
        history.undo(snapshot("ab", 2, 2));
        history.record(snapshot("a", 1, 1), &snapshot("ax", 2, 2), false);
        assert!(history.redo(snapshot("ax", 2, 2)).is_none());
        for i in 0..HISTORY_LIMIT + 10 {
            history.record(
                snapshot(&i.to_string(), 0, 0),
                &snapshot(&(i + 1).to_string(), 0, 0),
                false,
            );
        }
        assert_eq!(history.undo.len(), HISTORY_LIMIT);
    }
    #[test]
    fn navigation_and_deletion_keep_combining_and_zwj_graphemes_whole() {
        let text = "か\u{3099}👨‍👩‍👧‍👦🇯🇵x";
        let clusters: Vec<_> = text
            .grapheme_indices(true)
            .map(|(i, _)| i)
            .chain([text.len()])
            .collect();
        for pair in clusters.windows(2) {
            assert_eq!(next(text, pair[0]), pair[1]);
            assert_eq!(previous(text, pair[1]), pair[0]);
            assert_eq!(
                deletion_range(text, pair[0]..pair[0], true),
                pair[0]..pair[1]
            );
            assert_eq!(
                deletion_range(text, pair[1]..pair[1], false),
                pair[0]..pair[1]
            );
        }
        assert_eq!(deletion_range(text, 3..3, false), 0..6);
        assert_eq!(deletion_range(text, 3..3, true), 0..6);
        assert_eq!(deletion_range(text, 3..6, true), 0..6);
    }
    #[test]
    fn crlf_deletion_and_visual_mapping_preserve_raw_boundaries() {
        assert_eq!(deletion_range("a\r\nb", 3..3, false), 1..3);
        assert_eq!(grapheme_floor("a\r\nb", 2), 1);
        let (display, map) = display_line("e\u{301}\t🇯🇵");
        assert_eq!(display, "e\u{301}   🇯🇵");
        assert_eq!(map, vec![(0, 0), (3, 3), (4, 6), (12, 14)]);
        assert_eq!(visual_column("e\u{301}"), 1);
        assert_eq!(byte_at_column("e\u{301}🇯🇵", 1), 3);
    }
    #[test]
    fn ime_replacement_selection_is_relative_to_inserted_text_not_document() {
        let original = snapshot("😀prefix 尾\r\n", 11, 11);
        let (first, marked) = apply_replacement(&original, None, None, "にほ", true, Some(1..2));
        assert_eq!(first.text, "😀prefix にほ尾\r\n");
        assert_eq!((first.anchor, first.caret), (14, 17));
        assert_eq!(marked, Some(11..17));
        let (second, marked) = apply_replacement(&first, marked, None, "日本語", true, Some(3..3));
        assert_eq!(second.text, "😀prefix 日本語尾\r\n");
        assert_eq!((second.anchor, second.caret), (20, 20));
        let (committed, marked) = apply_replacement(&second, marked, None, "日本語", false, None);
        assert_eq!(committed.text, second.text);
        assert!(marked.is_none());
    }
    #[test]
    fn explicit_platform_range_overrides_marked_range_without_touching_other_source() {
        let before = snapshot("😀ab日本尾", 14, 14);
        let (after, marked) =
            apply_replacement(&before, Some(6..12), Some(2..3), "x\t\r\n", false, None);
        assert_eq!(after.text, "😀x\t\r\nb日本尾");
        assert!(marked.is_none());
        assert_eq!((after.anchor, after.caret), (8, 8));
    }
    #[test]
    fn mutation_lock_rejects_changes_until_explicitly_unlocked() {
        let mut guard = MutationGuard::default();
        let original = snapshot("日本語\r\n[[Link]]", 0, 9);
        let mut current = original.clone();
        let mut history = EditHistory::default();
        history.record(snapshot("before", 6, 6), &original, false);
        guard.set_read_only(true, false).unwrap();
        // Same gate used before every replacement, host reset and history pop.
        if guard.require_edit().is_ok() {
            current = apply_replacement(&current, None, None, "erase", false, None).0;
        }
        if guard.require_edit().is_ok() {
            current = history.undo(current.clone()).unwrap();
        }
        assert_eq!(current, original);
        assert_eq!(history.undo.len(), 1);
        assert!(history.redo.is_empty());
        guard.set_read_only(false, false).unwrap();
        guard.require_edit().unwrap();
        current = history.undo(current).unwrap();
        assert_eq!(current.text, "before");
    }
    #[test]
    fn lock_during_composition_is_rejected_without_committing_or_erasing_preedit() {
        let mut guard = MutationGuard::default();
        let original = snapshot("tail", 0, 0);
        let (preedit, marked) = apply_replacement(&original, None, None, "にほ", true, Some(2..2));
        let mut history = EditHistory::default();
        history.record(original.clone(), &preedit, true);
        assert!(
            guard
                .set_read_only(true, history.composing.is_some())
                .is_err()
        );
        assert!(!guard.read_only);
        guard.require_edit().unwrap();
        let (committed, marked) = apply_replacement(&preedit, marked, None, "日本", false, None);
        history.record(preedit, &committed, false);
        assert!(marked.is_none());
        guard
            .set_read_only(true, history.composing.is_some())
            .unwrap();
        assert_eq!(committed.text, "日本tail");
        assert_eq!(history.undo.len(), 1);
        assert!(guard.require_edit().is_err());
    }
    #[test]
    fn acknowledgment_preserves_authored_state_and_history_while_locked() {
        let mut baseline = "before".to_owned();
        let current = snapshot("日本語\r\n[[Link]]\n\n", 9, 0);
        let mut history = EditHistory::default();
        history.record(snapshot("before", 6, 6), &current, false);
        history.redo.push(snapshot("future", 3, 0));
        let original_undo = history.undo.clone();
        let original_redo = history.redo.clone();
        let mut guard = MutationGuard::default();
        guard.set_read_only(true, false).unwrap();
        acknowledge_baseline(&mut baseline, &current.text, &current.text, false).unwrap();
        assert_eq!(baseline, current.text);
        assert_eq!((current.anchor, current.caret), (9, 0));
        assert_eq!(history.undo, original_undo);
        assert_eq!(history.redo, original_redo);
        assert!(guard.read_only);
        // A save is not an undo boundary that destroys the prior edit.
        assert_eq!(history.undo(current).unwrap().text, "before");
    }
    #[test]
    fn acknowledgment_requires_exact_bytes_and_finalized_composition() {
        let mut baseline = "original".to_owned();
        assert!(acknowledge_baseline(&mut baseline, "authored\n\n", "authored", false).is_err());
        assert_eq!(baseline, "original");
        assert!(acknowledge_baseline(&mut baseline, "にほ", "にほ", true).is_err());
        assert_eq!(baseline, "original");
        acknowledge_baseline(&mut baseline, "", "", false).unwrap();
        assert_eq!(baseline, "");
    }
    #[test]
    fn editor_palette_matches_light_and_dark_shell_and_search() {
        let light = EditorPalette::new(false);
        assert_eq!(
            (light.paper, light.ink, light.line),
            (0xffffff, 0x1a1a18, 0xe6e4de)
        );
        assert_eq!((light.mark, light.selection), (0xc13a1e, 0xf2d9cf));
        let dark = EditorPalette::new(true);
        assert_eq!(
            (dark.paper, dark.ink, dark.line),
            (0x191c1e, 0xe9e9e4, 0x282c2f)
        );
        assert_eq!((dark.mark, dark.selection), (0xf4785e, 0x573a32));
    }
}
