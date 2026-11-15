//! Native, read-only CommonMark subset. Unsupported content stays inert and visible.
use crate::reader_find::{Hit, Matches, Target};
use gpui::{
    AnyElement, App, Div, FontStyle, FontWeight, InteractiveText, ScrollAnchor, ScrollHandle,
    SharedString, StrikethroughStyle, StyledText, TextRun, UnderlineStyle, Window, div, font,
    prelude::*, px, rgb,
};
use pulldown_cmark::{Alignment, Event, Options, Parser, Tag};
use std::{cell::RefCell, ops::Range, rc::Rc, sync::Arc};

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum BlockKind {
    Heading(u8),
    Paragraph,
    Code,
    Literal,
    Rule,
    FootnoteDefinition(String),
    Table,
}

#[derive(Clone, Debug, PartialEq)]
pub struct Block {
    /// Authored current-reader block ID, never inferred from a heading/footnote.
    pub block_id: Option<String>,
    pub kind: BlockKind,
    pub text: String,
    pub indent: usize,
    pub quoted: bool,
    pub marker: Option<String>,
    pub spans: Vec<Span>,
    pub links: Vec<Link>,
    pub table: Option<Table>,
    /// Standalone parser image node; never inferred from rendered text.
    pub image: Option<String>,
    /// First fenced info token; never mixed into body/Find byte ranges.
    pub language: Option<String>,
    /// Exact parser fence info, retained separately from syntax highlighting.
    pub fence_info: Option<String>,
    /// Ingestion-time lexical ranges, shared across display projections.
    pub syntax: Arc<[crate::code_highlight::Token]>,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct InlineStyle {
    pub emphasis: bool,
    pub strong: bool,
    pub strike: bool,
    pub code: bool,
    pub link: bool,
    pub unresolved: bool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Span {
    /// UTF-8 byte range into the block/cell text, not character indices.
    pub range: Range<usize>,
    pub style: InlineStyle,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Cell {
    pub text: String,
    pub spans: Vec<Span>,
    pub links: Vec<Link>,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum LinkTarget {
    Footnote {
        label: String,
    },
    Wiki {
        key: String,
        fragment: Option<String>,
        authored_label: bool,
    },
    Markdown {
        destination: String,
    },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Link {
    pub range: Range<usize>,
    pub target: LinkTarget,
}

/// A display-only projection. Targets and the authored parser snapshot stay unchanged.
/// Resolver output is inserted as plain text, never parsed or looked up again.
pub fn with_resolved_titles(
    blocks: &[Block],
    titles: &std::collections::HashMap<String, String>,
) -> Vec<Block> {
    fn eligible(target: &LinkTarget) -> Option<&str> {
        let LinkTarget::Wiki {
            key,
            authored_label: false,
            ..
        } = target
        else {
            return None;
        };
        let explicit = key.starts_with("id:")
            || key.split_once(':').is_some_and(|(vault, rest)| {
                !vault.is_empty()
                    && vault
                        .bytes()
                        .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
                    && rest.starts_with("id:")
            });
        explicit.then_some(key)
    }
    fn replace(
        text: &mut String,
        spans: &mut Vec<Span>,
        links: &mut [Link],
        titles: &std::collections::HashMap<String, String>,
    ) {
        // Reverse order keeps all still-to-be-replaced byte ranges valid.
        for index in (0..links.len()).rev() {
            let Some(title) = eligible(&links[index].target).and_then(|key| titles.get(key)) else {
                continue;
            };
            let old = links[index].range.clone();
            text.replace_range(old.clone(), title);
            let shift = |offset: usize, end: bool| {
                if offset <= old.start {
                    offset
                } else if offset >= old.end {
                    old.start + title.len() + offset - old.end
                } else if end {
                    old.start + title.len()
                } else {
                    old.start
                }
            };
            for span in spans.iter_mut() {
                span.range = shift(span.range.start, false)..shift(span.range.end, true);
            }
            for link in links.iter_mut() {
                link.range = shift(link.range.start, false)..shift(link.range.end, true);
            }
            spans.retain(|span| !span.range.is_empty());
        }
    }
    let mut displayed = blocks.to_vec();
    for block in &mut displayed {
        replace(&mut block.text, &mut block.spans, &mut block.links, titles);
        if let Some(table) = &mut block.table {
            for cell in table.rows.iter_mut().flatten() {
                replace(&mut cell.text, &mut cell.spans, &mut cell.links, titles);
            }
        }
    }
    displayed
}

/// Shell supplies verified internal or explicit HTTP(S) links. Others stay inert.
#[derive(Clone)]
pub struct LinkCallbacks {
    pub enlarge: Option<Rc<dyn Fn(usize, &mut Window, &mut App)>>,
    pub plain_click: Option<Rc<dyn Fn(usize, &gpui::ClickEvent, &mut Window, &mut App)>>,
    pub resolved: Rc<dyn Fn(&LinkTarget) -> bool>,
    pub activate: Rc<dyn Fn(&Link, &mut Window, &mut App)>,
}

/// Controls are supplied by the shell only for the current live reader snapshot.
#[derive(Clone)]
pub struct CodeCallbacks {
    pub labels: Option<std::collections::BTreeMap<usize, String>>,
    pub anchors: Option<Rc<FindAnchors>>,
    pub controls: std::collections::BTreeMap<usize, (gpui::FocusHandle, bool)>,
    pub activate: Rc<dyn Fn(usize, &mut Window, &mut App)>,
}

/// Resolver input, with source vault passed separately by the shell. Never infer
/// numeric IDs, registered vault prefixes or id-v1 configuration here.
pub fn resolve_term(target: &LinkTarget) -> Option<String> {
    match target {
        LinkTarget::Footnote { .. } => None,
        LinkTarget::Wiki { key, .. } => Some(key.clone()),
        LinkTarget::Markdown { destination } => {
            let raw = destination.split(['?', '#']).next()?.trim();
            if raw.is_empty()
                || raw.contains(':')
                || raw.starts_with('/')
                || raw.starts_with("assets/")
            {
                return None;
            }
            let mut bytes = Vec::new();
            let mut iter = raw.as_bytes().iter().copied();
            while let Some(ch) = iter.next() {
                if ch == b'%' {
                    let hi = (iter.next()? as char).to_digit(16)?;
                    let lo = (iter.next()? as char).to_digit(16)?;
                    bytes.push((hi * 16 + lo) as u8);
                } else {
                    bytes.push(ch);
                }
            }
            let decoded = String::from_utf8(bytes).ok()?;
            if decoded.contains(':')
                || decoded.starts_with('/')
                || decoded.starts_with("assets/")
                || decoded.split('/').any(|part| part == "..")
            {
                return None;
            }
            let key = decoded.strip_suffix(".md").unwrap_or(&decoded).trim();
            (!key.is_empty()).then(|| key.to_owned())
        }
    }
}

/// Hash-only Markdown targets stay in the current reader and never use the resolver.
pub fn is_local_fragment(target: &LinkTarget) -> bool {
    matches!(target, LinkTarget::Markdown { destination } if destination.starts_with('#'))
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum FragmentError {
    MissingHeading(String),
    MissingBlock(String),
    InvalidFragment,
    UnsupportedDestination,
}

/// Match a verified reader heading or authored block fragment to a native anchor.
/// No I/O: the caller supplies the accepted destination document, retains its
/// navigation generation, and scrolls only after that document has laid out.
/// Missing headings are explicitly reported so the shell can use note-top fallback.
pub fn fragment_anchor(
    blocks: &[Block],
    target: &LinkTarget,
) -> Result<Option<String>, FragmentError> {
    let fragment = match target {
        LinkTarget::Footnote { .. } => return Err(FragmentError::UnsupportedDestination),
        LinkTarget::Wiki { fragment, .. } => fragment.as_deref(),
        LinkTarget::Markdown { destination } => {
            if resolve_term(target).is_none() && !is_local_fragment(target) {
                return Err(FragmentError::UnsupportedDestination);
            }
            destination.split_once('#').map(|(_, fragment)| fragment)
        }
    };
    let Some(fragment) = fragment else {
        return Ok(None);
    };
    let text = fragment.trim_start_matches('#');
    // Encoded fragments and reference-renderer h-* IDs require separately
    // verified decoding/duplicate-ID semantics. Never strip their punctuation
    // into a different heading or misreport them as an ordinary missing heading.
    if text.contains('%') || text.starts_with("h-") {
        return Err(FragmentError::InvalidFragment);
    }
    if text.is_empty() {
        return Ok(None);
    }
    if let Some(id) = text.strip_prefix('^') {
        if !valid_block_id(id) {
            return Err(FragmentError::InvalidFragment);
        }
        return authored_block_targets(blocks)
            .contains_key(text)
            .then(|| Some(text.to_owned()))
            .ok_or_else(|| FragmentError::MissingBlock(id.into()));
    }
    let wanted = heading_fragment_slug(text);
    outline(blocks)
        .into_iter()
        .find(|heading| heading_fragment_slug(&heading.title) == wanted)
        .map(|heading| Some(heading.anchor))
        .ok_or_else(|| FragmentError::MissingHeading(text.into()))
}

// Current web reader semantics are level-agnostic and first-match, unlike
// the editor's exact-level selection. This slug is only a matching key: native
// element IDs remain independently collision-safe. Tested matching covers ASCII,
// Japanese and ordinary Unicode letters/numbers. Rust is_alphanumeric is not
// claimed identical to the reference Unicode category regex for every scalar.
fn heading_fragment_slug(text: &str) -> String {
    text.chars()
        .flat_map(char::to_lowercase)
        .filter(|ch| ch.is_alphanumeric() || *ch == ' ' || *ch == '-')
        .map(|ch| if ch == ' ' { '-' } else { ch })
        .collect()
}

// Wiki syntax is recognized only in parser text events, never code/HTML/images.
fn wiki_text(text: &str, out: &mut Cell, style: InlineStyle) {
    let mut cursor = 0;
    while let Some(relative) = text[cursor..].find("[[") {
        let start = cursor + relative;
        out.append(&text[cursor..start], style);
        let mut depth = 0usize;
        let mut end = None;
        for (offset, ch) in text[start + 2..].char_indices() {
            let at = start + 2 + offset;
            if ch == '\n' || ch == '\r' {
                break;
            }
            if ch == '[' {
                depth += 1;
            }
            if ch == ']' {
                if depth > 0 {
                    depth -= 1;
                } else if text[at..].starts_with("]]") {
                    end = Some(at);
                    break;
                } else {
                    break;
                }
            }
        }
        let Some(end) = end else {
            out.append(&text[start..], style);
            return;
        };
        let body = &text[start + 2..end];
        let (target, label) = body
            .split_once('|')
            .map_or((body, None), |(a, b)| (a, Some(b.trim())));
        let target = target.trim();
        let (key, fragment) = target
            .split_once('#')
            .map_or((target, None), |(key, rest)| {
                (key.trim(), Some(format!("#{rest}")))
            });
        // Includes are not ordinary links; keep the entire directive literal.
        if key.is_empty() || text[..start].ends_with('!') {
            out.append(&text[start..end + 2], style);
        } else {
            let display = label.filter(|label| !label.is_empty()).unwrap_or(target);
            let begin = out.text.len();
            out.append(
                display,
                InlineStyle {
                    link: true,
                    ..style
                },
            );
            out.links.push(Link {
                range: begin..out.text.len(),
                target: LinkTarget::Wiki {
                    key: key.to_owned(),
                    fragment,
                    authored_label: label.is_some_and(|label| !label.is_empty()),
                },
            });
        }
        cursor = end + 2;
    }
    out.append(&text[cursor..], style);
}

impl Cell {
    fn append(&mut self, text: &str, style: InlineStyle) {
        let start = self.text.len();
        self.text.push_str(text);
        if !text.is_empty() && style != InlineStyle::default() {
            self.spans.push(Span {
                range: start..self.text.len(),
                style,
            });
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Table {
    pub alignments: Vec<Alignment>,
    /// First row is the header.
    pub rows: Vec<Vec<Cell>>,
}

/// A native-reader anchor, not a promise of reference-renderer URL slug parity.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutlineEntry {
    pub anchor: String,
    pub title: String,
    pub level: u8,
    pub block_index: usize,
}

/// Stable for unchanged heading text/order, including when non-heading blocks change.
/// Duplicate headings receive deterministic suffixes; editing/reordering duplicates
/// may change their suffixes. Non-Latin letters are retained, punctuation is omitted.
pub fn outline(blocks: &[Block]) -> Vec<OutlineEntry> {
    let mut used = std::collections::HashSet::new();
    blocks
        .iter()
        .enumerate()
        .filter_map(|(block_index, block)| {
            let BlockKind::Heading(level) = block.kind else {
                return None;
            };
            let mut base = String::new();
            for ch in block.text.chars().flat_map(char::to_lowercase) {
                if ch.is_alphanumeric() || ch == '_' {
                    base.push(ch);
                } else if (ch.is_whitespace() || ch == '-')
                    && !base.is_empty()
                    && !base.ends_with('-')
                {
                    base.push('-');
                }
            }
            let base = base.trim_end_matches('-');
            let base = if base.is_empty() { "section" } else { base };
            let mut anchor = base.to_owned();
            let mut suffix = 2;
            while !used.insert(anchor.clone()) {
                anchor = format!("{base}-{suffix}");
                suffix += 1;
            }
            Some(OutlineEntry {
                anchor,
                title: block.text.clone(),
                level,
                block_index,
            })
        })
        .collect()
}

/// Request scrolling after the next layout. Caller must notify its GPUI context.
/// The tracked container MUST contain render_block_elements_themed's elements
/// directly, following exactly `prefix_children` title/meta/etc. children.
/// Call against the currently displayed document, not a stale outline selection.
pub fn scroll_to_heading(
    blocks: &[Block],
    anchor: &str,
    handle: &ScrollHandle,
    prefix_children: usize,
) -> bool {
    let Some(entry) = outline(blocks)
        .into_iter()
        .find(|entry| entry.anchor == anchor)
    else {
        return false;
    };
    let Some(index) = prefix_children.checked_add(entry.block_index) else {
        return false;
    };
    handle.scroll_to_top_of_item(index);
    true
}

/// Only unambiguous parser definitions are navigation targets. Labels are not
/// heading fragments or URLs, and rendered text is never reparsed for targets.
pub fn footnote_targets(blocks: &[Block]) -> std::collections::BTreeMap<String, usize> {
    let mut seen = std::collections::BTreeMap::new();
    for (index, block) in blocks.iter().enumerate() {
        if let BlockKind::FootnoteDefinition(label) = &block.kind {
            seen.entry(label.clone())
                .and_modify(|entry| *entry = None)
                .or_insert(Some(index));
        }
    }
    seen.into_iter()
        .filter_map(|(label, index)| index.map(|index| (label, index)))
        .collect()
}

pub fn footnote_link_valid(blocks: &[Block], link: &Link) -> bool {
    let LinkTarget::Footnote { label } = &link.target else {
        return false;
    };
    footnote_targets(blocks).contains_key(label)
        && blocks.iter().any(|block| {
            block.links.contains(link)
                || block.table.as_ref().is_some_and(|table| {
                    table
                        .rows
                        .iter()
                        .flatten()
                        .any(|cell| cell.links.contains(link))
                })
        })
}

/// Retain this alongside the displayed document. Rebuild when its headings or
/// reader ScrollHandle change; do not recreate on each render (anchors hold layout).
pub struct HeadingAnchors {
    entries: Vec<OutlineEntry>,
    targets: std::collections::HashMap<String, ScrollAnchor>,
    footnotes: std::collections::BTreeMap<String, (usize, ScrollAnchor)>,
    authored: std::collections::BTreeMap<String, (usize, ScrollAnchor)>,
}

impl HeadingAnchors {
    pub fn new(blocks: &[Block], handle: &ScrollHandle) -> Self {
        Self {
            entries: outline(blocks),
            authored: authored_block_targets(blocks)
                .into_iter()
                .map(|(id, index)| (id, (index, ScrollAnchor::for_handle(handle.clone()))))
                .collect(),
            footnotes: footnote_targets(blocks)
                .into_iter()
                .map(|(label, index)| (label, (index, ScrollAnchor::for_handle(handle.clone()))))
                .collect(),
            targets: outline(blocks)
                .into_iter()
                .map(|entry| (entry.anchor, ScrollAnchor::for_handle(handle.clone())))
                .collect(),
        }
    }

    pub fn scroll_to_footnote(&self, label: &str, window: &mut Window, cx: &mut App) -> bool {
        let Some((_, target)) = self.footnotes.get(label) else {
            return false;
        };
        target.scroll_to(window, cx);
        true
    }

    /// Call after the document has laid out. Supports nested article/aside layout.
    /// Request a frame with cx.notify() in the calling view after this returns true.
    pub fn scroll_to(&self, anchor: &str, window: &mut Window, cx: &mut App) -> bool {
        let Some(target) = self
            .targets
            .get(anchor)
            .or_else(|| self.authored.get(anchor).map(|(_, anchor)| anchor))
        else {
            return false;
        };
        target.scroll_to(window, cx);
        true
    }
}

/// Transient semantic host-block measurements for guarded reader scroll actions.
/// Bounds are transient native layout evidence, never serialized coordinates.
pub struct PositionAnchors {
    handle: ScrollHandle,
    count: usize,
    layout: Rc<RefCell<PositionLayout>>,
}
#[derive(Default)]
struct PositionLayout {
    generation: u64,
    measured: Option<PositionMeasurement>,
    refreshed: Option<PositionMeasurement>,
    pending: Option<PositionAfterLayout>,
}
struct PositionAfterLayout {
    expected: PositionMeasurement,
    callback: Box<dyn FnOnce(bool, &mut Window, &mut App)>,
}
#[derive(Clone, PartialEq)]
struct PositionMeasurement {
    blocks: Vec<gpui::Bounds<gpui::Pixels>>,
    reader: gpui::Bounds<gpui::Pixels>,
    viewport: gpui::Size<gpui::Pixels>,
    scale: f32,
    offset: gpui::Point<gpui::Pixels>,
    // Includes all reader content, including padding and non-host-block content.
    max_offset: gpui::Size<gpui::Pixels>,
}
#[derive(Clone, Debug, PartialEq)]
pub struct PositionCapture {
    pub generation: u64,
    pub block_index: usize,
    pub geometry: crate::session_store::PositionGeometry,
}
impl PositionLayout {
    fn consume_pending(&mut self) -> Option<(bool, Box<dyn FnOnce(bool, &mut Window, &mut App)>)> {
        let pending = self.pending.take()?;
        let fresh = self.measured.as_ref() == Some(&pending.expected);
        Some((fresh, pending.callback))
    }
    fn capture(
        &self,
        reader: gpui::Bounds<gpui::Pixels>,
        offset: gpui::Point<gpui::Pixels>,
        viewport: gpui::Size<gpui::Pixels>,
        scale: f32,
    ) -> Option<PositionCapture> {
        let measured = self.measured.as_ref()?;
        if measured.reader != reader
            || measured.viewport != viewport
            || measured.scale != scale
            || measured.offset != offset
        {
            return None;
        }
        let geometry = crate::session_store::PositionGeometry {
            viewport_width: f32::from(viewport.width) as f64,
            viewport_height: f32::from(viewport.height) as f64,
            reader_width: f32::from(measured.reader.size.width) as f64,
            reader_height: f32::from(measured.reader.size.height) as f64,
            scale: scale as f64,
        };
        if ![
            geometry.viewport_width,
            geometry.viewport_height,
            geometry.reader_width,
            geometry.reader_height,
        ]
        .iter()
        .all(|v| v.is_finite() && *v > 0. && *v <= 65536.)
        {
            return None;
        }
        if !scale.is_finite() || scale <= 0. || scale > 16. {
            return None;
        }
        if measured.blocks.iter().any(|b| {
            !f32::from(b.origin.x).is_finite()
                || !f32::from(b.size.width).is_finite()
                || f32::from(b.size.width) <= 0.
        }) || !f32::from(measured.reader.origin.x).is_finite()
        {
            return None;
        }
        let rows: Vec<_> = measured
            .blocks
            .iter()
            .map(|b| (f32::from(b.origin.y), f32::from(b.size.height)))
            .collect();
        let block_index = first_visible_position(
            &rows,
            f32::from(measured.reader.origin.y),
            f32::from(measured.reader.size.height),
        )?;
        Some(PositionCapture {
            generation: self.generation,
            block_index,
            geometry,
        })
    }
}

/// Editor line positions describe proportional progress, never byte offsets.
pub fn follow_scroll_ratio(top_line: u64, line_count: u64) -> f64 {
    let top = top_line.max(1);
    let count = line_count.max(top);
    if count <= 1 {
        0.
    } else {
        (top - 1) as f64 / (count - 1) as f64
    }
}

fn proportional_scroll_offset(ratio: f64, max_offset: Option<f32>) -> Option<f32> {
    let max_offset = max_offset?;
    if !ratio.is_finite() || !(0. ..=1.).contains(&ratio) || !max_offset.is_finite() {
        return None;
    }
    // Native scrolling translates content upward, hence a negative y offset.
    Some(-(f64::from(max_offset.max(0.)) * ratio) as f32)
}

fn first_visible_position(blocks: &[(f32, f32)], top: f32, height: f32) -> Option<usize> {
    if blocks.is_empty()
        || blocks.len() > 4096
        || !top.is_finite()
        || !height.is_finite()
        || height <= 0.
        || blocks
            .iter()
            .any(|(y, h)| !y.is_finite() || !h.is_finite() || !(*y + *h).is_finite() || *h <= 0.)
    {
        return None;
    }
    if blocks
        .windows(2)
        .any(|pair| pair[0].0 + pair[0].1 > pair[1].0)
    {
        return None;
    }
    blocks
        .iter()
        .position(|(y, h)| *y < top + height && *y + *h > top)
}
impl PositionAnchors {
    pub fn new(count: usize, handle: &ScrollHandle) -> Option<Self> {
        (count > 0 && count <= 4096).then(|| Self {
            handle: handle.clone(),
            count,
            layout: Rc::default(),
        })
    }
    pub fn current_generation(&self) -> u64 {
        self.layout.borrow().generation
    }
    /// A single explicit action, consumed by the next completed host prepaint.
    /// The callback must only retain a Weak reference to these anchors.
    pub fn request_after_layout(
        &self,
        callback: impl FnOnce(bool, &mut Window, &mut App) + 'static,
    ) -> bool {
        let mut layout = self.layout.borrow_mut();
        let Some(expected) = layout.measured.clone() else {
            return false;
        };
        layout.pending = Some(PositionAfterLayout {
            expected,
            callback: Box::new(callback),
        });
        true
    }
    pub fn cancel_after_layout(&self) {
        self.layout.borrow_mut().pending = None;
    }
    fn track(&self, reader: Div, count: usize) -> Div {
        let generation = {
            let mut state = self.layout.borrow_mut();
            state.generation = state.generation.wrapping_add(1);
            state.measured = None;
            state.generation
        };
        let expected_count = self.count;
        let state = self.layout.clone();
        let handle = self.handle.clone();
        reader.on_children_prepainted(move |blocks, window, cx| {
            let mut state = state.borrow_mut();
            if state.generation != generation {
                return;
            }
            let count_matches = blocks.len() == count && count == expected_count;
            let measured = PositionMeasurement {
                blocks,
                reader: handle.bounds(),
                viewport: window.viewport_size(),
                scale: window.scale_factor(),
                offset: handle.offset(),
                max_offset: handle.max_offset(),
            };
            if state.refreshed.as_ref() != Some(&measured) {
                state.refreshed = Some(measured.clone());
                window.refresh();
            }
            state.measured = count_matches.then_some(measured);
            let pending = state.consume_pending();
            drop(state);
            if let Some((fresh, callback)) = pending {
                callback(fresh, window, cx);
            }
        })
    }
    pub fn capture(
        &self,
        viewport: gpui::Size<gpui::Pixels>,
        scale: f32,
    ) -> Option<PositionCapture> {
        let layout = self.layout.borrow();
        let measured = layout.measured.as_ref()?;
        let max_offset = self.handle.max_offset();
        if measured.max_offset != max_offset
            || ![
                f32::from(max_offset.width),
                f32::from(max_offset.height),
                f32::from(measured.offset.x),
                f32::from(measured.offset.y),
            ]
            .iter()
            .all(|value| value.is_finite())
        {
            return None;
        }
        layout.capture(self.handle.bounds(), self.handle.offset(), viewport, scale)
    }

    pub fn matches_capture(
        &self,
        capture: &PositionCapture,
        viewport: gpui::Size<gpui::Pixels>,
        scale: f32,
    ) -> bool {
        self.capture(viewport, scale).as_ref() == Some(capture)
    }
    /// Apply approximate editor viewport progress using the full measured reader
    /// scroll extent. The caller must additionally guard source/request identity
    /// and invoke this only from its completed-layout callback.
    pub fn apply_proportion(
        &self,
        ratio: f64,
        capture: &PositionCapture,
        viewport: gpui::Size<gpui::Pixels>,
        scale: f32,
    ) -> bool {
        if !self.matches_capture(capture, viewport, scale) {
            return false;
        }
        let layout = self.layout.borrow();
        let Some(measured) = layout.measured.as_ref() else {
            return false;
        };
        let Some(y) =
            proportional_scroll_offset(ratio, Some(f32::from(measured.max_offset.height)))
        else {
            return false;
        };
        let mut offset = self.handle.offset();
        offset.y = px(y);
        self.handle.set_offset(offset);
        true
    }
    /// Apply once inside the caller's guarded after-layout callback. ScrollAnchor's
    /// public scroll_to queues another unguarded frame, so use the same measured
    /// native origin directly here. No persisted or estimated pixel offset is used.
    pub fn apply(
        &self,
        index: usize,
        capture: &PositionCapture,
        viewport: gpui::Size<gpui::Pixels>,
        scale: f32,
    ) -> bool {
        if !self.matches_capture(capture, viewport, scale) {
            return false;
        }
        let state = self.layout.borrow();
        let Some(measured) = state.measured.as_ref() else {
            return false;
        };
        let Some(block) = measured.blocks.get(index) else {
            return false;
        };
        let mut offset = self.handle.offset();
        offset.y += measured.reader.origin.y - block.origin.y;
        offset.y =
            px(f32::from(offset.y).clamp(-f32::from(self.handle.max_offset().height).max(0.), 0.));
        self.handle.set_offset(offset);
        true
    }
}

/// Geometry from a completed native prepaint, in unscrolled window coordinates.
#[derive(Clone, Copy)]
struct FindLine {
    top: f32,
    height: f32,
    reader_bounds: gpui::Bounds<gpui::Pixels>,
    viewport: gpui::Size<gpui::Pixels>,
}
#[derive(Default)]
struct FindLayout {
    generation: u64,
    hit: Option<Hit>,
    line: Option<FindLine>,
}
impl FindLayout {
    fn begin(&mut self, hit: Option<Hit>) -> u64 {
        self.generation = self.generation.wrapping_add(1);
        self.hit = hit;
        self.line = None;
        self.generation
    }
    fn publish(&mut self, generation: u64, line: FindLine) {
        if self.generation == generation {
            self.line = Some(line);
        }
    }
}

// Search offsets exclude generated list markers; both remain UTF-8 byte offsets.
fn match_start(text: &str, range: &Range<usize>, prefix: usize) -> Option<usize> {
    if range.is_empty() || !text.is_char_boundary(range.start) || !text.is_char_boundary(range.end)
    {
        return None;
    }
    range.start.checked_add(prefix)
}

// Coordinates are from actual text layout, not character/line-count estimates.
fn reveal_line_offset(
    current: f32,
    view_top: f32,
    view_height: f32,
    line_top: f32,
    line_height: f32,
    max_scroll: f32,
) -> Option<f32> {
    if ![
        current,
        view_top,
        view_height,
        line_top,
        line_height,
        max_scroll,
    ]
    .iter()
    .all(|v| v.is_finite())
        || view_height <= 0.
        || line_height <= 0.
        || max_scroll < 0.
    {
        return None;
    }
    let top = line_top + current;
    let bottom = top + line_height;
    let next = if top < view_top || line_height > view_height {
        view_top - line_top
    } else if bottom > view_top + view_height {
        view_top + view_height - line_top - line_height
    } else {
        current
    };
    Some(next.clamp(-max_scroll, 0.))
}

pub struct FindAnchors {
    targets: std::collections::HashMap<Target, ScrollAnchor>,
    handle: ScrollHandle,
    layout: Rc<RefCell<FindLayout>>,
}
impl FindAnchors {
    pub fn new(blocks: &[Block], handle: &ScrollHandle) -> Self {
        let mut targets = std::collections::HashMap::new();
        for (block, value) in blocks.iter().enumerate() {
            if let Some(table) = &value.table {
                for (row, cells) in table.rows.iter().enumerate() {
                    for column in 0..cells.len() {
                        targets.insert(
                            Target::Cell { block, row, column },
                            ScrollAnchor::for_handle(handle.clone()),
                        );
                    }
                }
            } else {
                targets.insert(
                    Target::Block(block),
                    ScrollAnchor::for_handle(handle.clone()),
                );
            }
        }
        Self {
            targets,
            handle: handle.clone(),
            layout: Rc::default(),
        }
    }
    fn begin_layout(&self, hit: Option<Hit>) -> u64 {
        self.layout.borrow_mut().begin(hit)
    }
    fn track_text(
        &self,
        row: Div,
        text: &StyledText,
        source: &str,
        prefix: usize,
        target: &Target,
        generation: u64,
    ) -> Div {
        let state = self.layout.borrow();
        let Some(hit) = state.hit.as_ref().filter(|hit| &hit.target == target) else {
            return row;
        };
        let Some(index) = match_start(source, &hit.range, prefix) else {
            return row;
        };
        drop(state);
        let text = text.layout().clone();
        let state = self.layout.clone();
        let handle = self.handle.clone();
        // Runs only after this row's StyledText/InteractiveText has prepainted.
        row.on_children_prepainted(move |_, window, _| {
            if state.borrow().generation != generation {
                return;
            }
            if let Some(position) = text.position_for_index(index) {
                let top = f32::from(position.y - handle.offset().y);
                state.borrow_mut().publish(
                    generation,
                    FindLine {
                        top,
                        height: f32::from(text.line_height()),
                        reader_bounds: handle.bounds(),
                        viewport: window.viewport_size(),
                    },
                );
            }
        })
    }
    pub fn scroll_to_hit(&self, hit: &Hit, viewport: gpui::Size<gpui::Pixels>) -> bool {
        let state = self.layout.borrow();
        if state.hit.as_ref() != Some(hit) {
            return false;
        }
        let Some(FindLine {
            top,
            height,
            reader_bounds: bounds,
            viewport: measured_viewport,
        }) = state.line
        else {
            return false;
        };
        // Refuse a prior viewport's geometry rather than guessing after a resize.
        if bounds != self.handle.bounds() || measured_viewport != viewport {
            return false;
        }
        let mut offset = self.handle.offset();
        let Some(y) = reveal_line_offset(
            f32::from(offset.y),
            f32::from(bounds.origin.y),
            f32::from(bounds.size.height),
            top,
            height,
            f32::from(self.handle.max_offset().height),
        ) else {
            return false;
        };
        offset.y = px(y);
        self.handle.set_offset(offset);
        true
    }
    pub fn scroll_to(&self, target: &Target, window: &mut Window, cx: &mut App) -> bool {
        let Some(anchor) = self.targets.get(target) else {
            return false;
        };
        anchor.scroll_to(window, cx);
        true
    }
}
#[derive(Clone, Copy)]
pub struct FindPresentation<'a> {
    pub matches: &'a Matches,
    pub active: Option<usize>,
    pub anchors: &'a FindAnchors,
}

struct Node<'a> {
    event: Event<'a>,
    range: Range<usize>,
    children: Vec<Node<'a>>,
}

fn nodes<'a>(events: &mut impl Iterator<Item = (Event<'a>, Range<usize>)>) -> Vec<Node<'a>> {
    let mut result: Vec<Node<'_>> = Vec::new();
    while let Some((event, range)) = events.next() {
        if matches!(event, Event::End(_)) {
            break;
        }
        let children = if matches!(event, Event::Start(_)) {
            nodes(events)
        } else {
            vec![]
        };
        if let Event::Text(next) = &event {
            if let Some(Node {
                event: Event::Text(previous),
                range: previous_range,
                ..
            }) = result.last_mut()
            {
                *previous = format!("{previous}{next}").into();
                previous_range.end = range.end;
                continue;
            }
        }
        result.push(Node {
            event,
            range,
            children,
        });
    }
    result
}

fn inline(node: &Node<'_>, source: &str, out: &mut Cell, mut style: InlineStyle) {
    match &node.event {
        Event::Start(Tag::Image { .. }) => out.append(&source[node.range.clone()], style),
        Event::Start(Tag::Link { dest_url, .. }) => {
            style.link = true;
            let start = out.text.len();
            for child in &node.children {
                inline(child, source, out, style);
            }
            out.links.push(Link {
                range: start..out.text.len(),
                target: LinkTarget::Markdown {
                    destination: dest_url.to_string(),
                },
            });
        }
        Event::Start(tag) => {
            if matches!(tag, Tag::CodeBlock(_)) {
                for child in &node.children {
                    if let Event::Text(text) = &child.event {
                        out.append(text, style);
                    }
                }
                return;
            }
            match tag {
                Tag::Emphasis => style.emphasis = true,
                Tag::Strong => style.strong = true,
                Tag::Strikethrough => style.strike = true,
                _ => {}
            }
            for child in &node.children {
                inline(child, source, out, style);
            }
        }
        Event::FootnoteReference(label) => {
            let start = out.text.len();
            style.link = true;
            out.append(&format!("[^{label}]"), style);
            out.links.push(Link {
                range: start..out.text.len(),
                target: LinkTarget::Footnote {
                    label: unicase::UniCase::new(label.as_ref()).to_folded_case(),
                },
            });
        }
        Event::Code(text) => {
            style.code = true;
            out.append(text, style);
        }
        Event::Text(text) => {
            if style.link {
                out.append(text, style);
            } else {
                wiki_text(text, out, style);
            }
        }
        Event::Html(text) | Event::InlineHtml(text) => out.append(text, style),
        Event::TaskListMarker(_) => {} // Displayed once as an inert list marker, not searchable text.
        Event::SoftBreak => out.append(" ", style),
        Event::HardBreak => out.append("\n", style),
        _ => out.append(&source[node.range.clone()], style),
    }
}

// Only the item's first parsed inline position can provide its own task state.
// Never inherit a nested child's state or reinterpret custom Track state tokens.
fn task_state(item: &Node<'_>) -> Option<bool> {
    let first = item.children.first()?;
    match first.event {
        Event::TaskListMarker(checked) => Some(checked),
        Event::Start(Tag::Paragraph) => match first.children.first()?.event {
            Event::TaskListMarker(checked) => Some(checked),
            _ => None,
        },
        _ => None,
    }
}

fn valid_block_id(id: &str) -> bool {
    let mut bytes = id.bytes();
    bytes.next().is_some_and(|b| b.is_ascii_alphanumeric())
        && bytes.all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
}
fn terminal_marker(text: &str) -> Option<(usize, &str)> {
    let text = text.trim_end();
    let boundary = text.rfind(char::is_whitespace)?;
    let start = boundary + text[boundary..].chars().next()?.len_utf8();
    let id = text[start..].strip_prefix('^')?;
    valid_block_id(id).then_some((start, id))
}
fn apply_authored_marker(block: &mut Block, last: Option<&Node<'_>>, source: &str) {
    if block.kind != BlockKind::Paragraph || block.quoted || block.image.is_some() {
        return;
    }
    let Some(last) = last.filter(|node| matches!(node.event, Event::Text(_))) else {
        return;
    };
    let Some((_, authored)) = terminal_marker(&source[last.range.clone()]) else {
        return;
    };
    let Some((start, id)) = terminal_marker(&block.text) else {
        return;
    };
    if id != authored
        || block.text[..start].trim().is_empty()
        || block
            .spans
            .iter()
            .any(|span| span.range.end > start && (span.style.code || span.style.link))
        || block.links.iter().any(|link| link.range.end > start)
    {
        return;
    }
    let id = id.to_owned();
    let cutoff = block.text[..start].trim_end().len();
    // Whitespace may belong to a parsed link/code label, not to the marker.
    // Keep the original prose rather than leave a range outside visible text.
    if block.links.iter().any(|link| link.range.end > cutoff)
        || block
            .spans
            .iter()
            .any(|span| span.style.code && span.range.end > cutoff)
    {
        return;
    }
    block.text.truncate(cutoff);
    for span in &mut block.spans {
        span.range.end = span.range.end.min(cutoff);
    }
    block.spans.retain(|span| span.range.start < span.range.end);
    block.block_id = Some(id);
}
fn flush_authored_run(
    run: &mut Cell,
    indent: usize,
    quoted: bool,
    out: &mut Vec<Block>,
    last: Option<&Node<'_>>,
    source: &str,
    authored: bool,
) {
    let before = out.len();
    flush_run(run, indent, quoted, out);
    if authored && out.len() > before {
        apply_authored_marker(out.last_mut().unwrap(), last, source);
    }
}
/// IDs are case-sensitive and duplicate IDs select the first document occurrence.
pub fn authored_block_targets(blocks: &[Block]) -> std::collections::BTreeMap<String, usize> {
    let mut targets = std::collections::BTreeMap::new();
    for (index, block) in blocks.iter().enumerate() {
        if let Some(id) = &block.block_id {
            targets.entry(format!("^{id}")).or_insert(index);
        }
    }
    targets
}

fn blocks(
    node: &Node<'_>,
    source: &str,
    indent: usize,
    quoted: bool,
    out: &mut Vec<Block>,
    authored: bool,
) {
    match &node.event {
        Event::Start(Tag::FootnoteDefinition(label)) => {
            // Labels remain literal and searchable, not fake navigation links.
            // Keep every definition at its source position, including unused or
            // repeated labels, rather than dropping content during collection.
            let mut label_text = Cell::default();
            label_text.append(&format!("[^{label}]:"), InlineStyle::default());
            flush_run(&mut label_text, indent, quoted, out);
            out.last_mut().unwrap().kind = BlockKind::FootnoteDefinition(
                unicase::UniCase::new(label.as_ref()).to_folded_case(),
            );
            for child in &node.children {
                blocks(child, source, indent + 1, quoted, out, authored);
            }
            return;
        }
        Event::Start(Tag::Table(alignments)) => {
            // GFM ignores excess row cells. Keep those tables as source instead
            // of silently hiding content that the parser did not represent.
            let unrepresented = node.children.iter().any(|row| {
                row.children.last().is_some_and(|cell| {
                    !source[cell.range.end..row.range.end]
                        .trim_matches(|ch: char| ch == '|' || ch.is_whitespace())
                        .is_empty()
                })
            });
            if unrepresented {
                out.push(Block {
                    block_id: None,
                    kind: BlockKind::Literal,
                    text: source[node.range.clone()].into(),
                    indent,
                    quoted,
                    marker: None,
                    spans: vec![],
                    links: vec![],
                    language: None,
                    fence_info: None,
                    syntax: Arc::from([]),
                    table: None,
                    image: None,
                });
                return;
            }
            let rows = node
                .children
                .iter()
                .map(|row| {
                    row.children
                        .iter()
                        .map(|cell| {
                            let mut content = Cell::default();
                            inline(cell, source, &mut content, InlineStyle::default());
                            content
                        })
                        .collect()
                })
                .collect();
            out.push(Block {
                block_id: None,
                kind: BlockKind::Table,
                text: source[node.range.clone()].into(),
                indent,
                quoted,
                marker: None,
                spans: vec![],
                links: vec![],
                language: None,
                fence_info: None,
                syntax: Arc::from([]),
                image: None,
                table: Some(Table {
                    alignments: alignments.clone(),
                    rows,
                }),
            });
            return;
        }
        Event::Start(Tag::List(start)) => {
            for (index, child) in node.children.iter().enumerate() {
                let before = out.len();
                blocks(child, source, indent + 1, quoted, out, authored);
                if let Some(first) = out
                    .get_mut(before)
                    .filter(|block| block.indent == indent + 1)
                {
                    // Static neutral glyphs, no checkbox role, focus, or mutation callback.
                    first.marker = Some(match task_state(child) {
                        Some(false) => "☐".into(),
                        Some(true) => "☑".into(),
                        None => {
                            start.map_or_else(|| "•".into(), |n| format!("{}.", n + index as u64))
                        }
                    });
                }
            }
            return;
        }
        Event::Start(Tag::Item) => {
            // Tight lists omit paragraph tags. Group inline runs into one paragraph.
            let before = out.len();
            let mut run = Cell::default();
            let mut last_inline = None;
            for child in &node.children {
                if matches!(
                    child.event,
                    Event::Rule
                        | Event::Start(
                            Tag::Paragraph
                                | Tag::Heading { .. }
                                | Tag::CodeBlock(_)
                                | Tag::List(_)
                                | Tag::BlockQuote(_)
                                | Tag::Table(_)
                                | Tag::FootnoteDefinition(_)
                        )
                ) {
                    flush_authored_run(
                        &mut run,
                        indent,
                        quoted,
                        out,
                        last_inline.take(),
                        source,
                        authored,
                    );
                    blocks(child, source, indent, quoted, out, authored);
                } else {
                    inline(child, source, &mut run, InlineStyle::default());
                    last_inline = Some(child);
                }
            }
            flush_authored_run(&mut run, indent, quoted, out, last_inline, source, authored);
            // An empty task still needs its own indicator, including when its only
            // content is a nested list. Do not put the parent's state on its child.
            if task_state(node).is_some()
                && out.get(before).is_none_or(|block| block.indent > indent)
            {
                out.insert(
                    before,
                    Block {
                        block_id: None,
                        kind: BlockKind::Paragraph,
                        text: String::new(),
                        indent,
                        quoted,
                        marker: None,
                        spans: vec![],
                        links: vec![],
                        table: None,
                        image: None,
                        language: None,
                        fence_info: None,
                        syntax: Arc::from([]),
                    },
                );
            }
            return;
        }
        Event::Start(Tag::BlockQuote(_)) => {
            for child in &node.children {
                blocks(child, source, indent, true, out, authored);
            }
            return;
        }
        _ => {}
    }
    let kind = match &node.event {
        Event::Start(Tag::Heading { level, .. }) => BlockKind::Heading(*level as u8),
        Event::Start(Tag::Paragraph) => BlockKind::Paragraph,
        Event::Start(Tag::CodeBlock(_)) => BlockKind::Code,
        Event::Rule => BlockKind::Rule,
        _ => BlockKind::Literal,
    };
    let mut content = Cell::default();
    if kind == BlockKind::Literal {
        content.text.push_str(&source[node.range.clone()]);
    } else if kind != BlockKind::Rule {
        inline(node, source, &mut content, InlineStyle::default());
    }
    let language = match &node.event {
        Event::Start(Tag::CodeBlock(pulldown_cmark::CodeBlockKind::Fenced(info))) => {
            info.split_whitespace().next().map(str::to_owned)
        }
        _ => None,
    };
    let fence_info = match &node.event {
        Event::Start(Tag::CodeBlock(pulldown_cmark::CodeBlockKind::Fenced(info))) => {
            Some(info.to_string())
        }
        _ => None,
    };
    let syntax =
        Arc::from(crate::code_highlight::classify(&content.text, language.as_deref()).as_ref());
    let image = if kind == BlockKind::Paragraph && node.children.len() == 1 {
        match &node.children[0].event {
            Event::Start(Tag::Image { dest_url, .. }) => Some(dest_url.to_string()),
            _ => None,
        }
    } else {
        None
    };
    out.push(Block {
        block_id: None,
        syntax,
        language,
        fence_info,
        kind,
        text: content.text,
        spans: content.spans,
        links: content.links,
        table: None,
        image,
        indent,
        quoted,
        marker: None,
    });
    if authored {
        apply_authored_marker(out.last_mut().unwrap(), node.children.last(), source);
    }
}

fn flush_run(run: &mut Cell, indent: usize, quoted: bool, out: &mut Vec<Block>) {
    if !run.text.is_empty() {
        out.push(Block {
            block_id: None,
            kind: BlockKind::Paragraph,
            text: std::mem::take(&mut run.text),
            spans: std::mem::take(&mut run.spans),
            links: std::mem::take(&mut run.links),
            language: None,
            fence_info: None,
            syntax: Arc::from([]),
            table: None,
            image: None,
            indent,
            quoted,
            marker: None,
        });
    }
}

/// Parse CommonMark plus tables, ordinary tasks, strike and display-only footnotes.
/// Footnotes stay labeled in source order; unique definitions have local targets.
/// Rich Track task notation remains literal; no task mutation is supported.
/// Reference definitions omitted by the parser are retained as source fallback.
pub fn parse(source: &str) -> Vec<Block> {
    parse_mode(source, false)
}
/// Accepted live reader only. Snapshot and inert preview parsing remain unchanged.
pub fn parse_reader(source: &str) -> Vec<Block> {
    parse_mode(source, true)
}
fn parse_mode(source: &str, authored: bool) -> Vec<Block> {
    let options = Options::ENABLE_TABLES
        | Options::ENABLE_TASKLISTS
        | Options::ENABLE_STRIKETHROUGH
        | Options::ENABLE_FOOTNOTES;
    let tree = nodes(&mut Parser::new_ext(source, options).into_offset_iter());
    let mut result = Vec::new();
    let mut end = 0;
    for node in &tree {
        fallback(&source[end..node.range.start], &mut result);
        blocks(node, source, 0, false, &mut result, authored);
        end = node.range.end;
    }
    fallback(&source[end..], &mut result);
    result
}

/// A separate inert document, with no host anchors, link cache or Find identity.
#[derive(Clone, Debug)]
pub(crate) struct IncludePreview {
    pub items: Vec<PreviewItem>,
    pub unsupported: Vec<String>,
}
#[derive(Clone, Debug)]
pub(crate) enum PreviewItem {
    Host(Vec<Block>),
    Include {
        index: usize,
        directive: String,
        blocks: Vec<Block>,
    },
}
const PREVIEW_BLOCK_LIMIT: usize = 4096;

fn preview_tree(source: &str) -> Result<Vec<Node<'_>>, String> {
    let options = Options::ENABLE_TABLES
        | Options::ENABLE_TASKLISTS
        | Options::ENABLE_STRIKETHROUGH
        | Options::ENABLE_FOOTNOTES;
    // Cap parser events and nesting before allocating the recursive native tree.
    let mut events = Vec::new();
    let mut depth = 0usize;
    for (event, range) in Parser::new_ext(source, options).into_offset_iter() {
        match event {
            Event::Start(_) => depth += 1,
            Event::End(_) => depth = depth.saturating_sub(1),
            _ => {}
        }
        if depth > 128 || events.len() >= 65536 {
            return Err(
                "Rendered preview exceeds parser limits (128 nesting levels, 65536 events).".into(),
            );
        }
        events.push((event, range));
    }
    Ok(nodes(&mut events.into_iter()))
}
pub(crate) fn preview_blocks(source: &str) -> Result<Vec<Block>, String> {
    let tree = preview_tree(source)?;
    let mut result = Vec::new();
    let mut end = 0;
    for node in &tree {
        fallback(&source[end..node.range.start], &mut result);
        blocks(node, source, 0, false, &mut result, false);
        if result.len() > PREVIEW_BLOCK_LIMIT {
            return Err("Rendered preview exceeds 4096 blocks.".into());
        }
        end = node.range.end;
    }
    fallback(&source[end..], &mut result);
    if result.len() > PREVIEW_BLOCK_LIMIT {
        return Err("Rendered preview exceeds 4096 blocks.".into());
    }
    Ok(result)
}

/// Parse the *whole returned Markdown* once, preserving reference resolution and
/// surrounding list/table/fence structure. Positions never refer to raw source.
pub(crate) fn included_preview(
    response: &crate::included_notes::Response,
) -> Result<IncludePreview, String> {
    let source = &response.markdown;
    if source.len() > 16 * 1024 * 1024 || response.entries.len() > 32 {
        return Err("Rendered preview exceeds response bounds.".into());
    }
    let mut total_text = 0usize;
    let mut entries = std::collections::BTreeMap::new();
    let mut starts = vec![0];
    starts.extend(source.match_indices('\n').map(|(i, _)| i + 1));
    for (index, entry) in response.entries.iter().enumerate() {
        total_text = total_text
            .checked_add(entry.excerpt.len())
            .ok_or("Rendered preview text overflow.")?;
        let line = usize::try_from(entry.line).map_err(|_| "Invalid rendered include line.")?;
        if total_text > 256 * 1024 || line >= starts.len() || entries.insert(line, index).is_some()
        {
            return Err("Rendered preview has invalid lines or exceeds excerpt bounds.".into());
        }
    }
    let tree = preview_tree(source)?;
    let mut items = Vec::new();
    let mut host = Vec::new();
    let mut matched = std::collections::HashSet::new();
    let mut count = 0usize;
    let mut end = 0;
    for node in &tree {
        fallback(&source[end..node.range.start], &mut host);
        let line = starts
            .partition_point(|start| *start <= node.range.start)
            .saturating_sub(1);
        let candidate = entries.get(&line).copied().filter(|_| {
            let raw_end = starts.get(line + 1).copied().unwrap_or(source.len());
            let raw = source[starts[line]..raw_end].trim_end_matches(['\r', '\n']);
            matches!(node.event, Event::Start(Tag::Paragraph))
                && node.range.start == starts[line]
                && source[node.range.clone()].trim_end_matches(['\r', '\n']) == raw
                && raw.starts_with("![[")
                && raw.ends_with("]]")
                && raw.matches("![[").count() == 1
                && raw.matches("]]").count() == 1
        });
        if let Some(index) = candidate {
            count += host.len();
            if !host.is_empty() {
                items.push(PreviewItem::Host(std::mem::take(&mut host)));
            }
            let entry = &response.entries[index];
            let excerpt = if entry.error.as_ref().is_some_and(|e| !e.is_empty()) {
                Vec::new()
            } else {
                preview_blocks(&entry.excerpt)?
            };
            count += 1 + excerpt.len();
            items.push(PreviewItem::Include {
                index,
                directive: source[node.range.clone()].to_owned(),
                blocks: excerpt,
            });
            matched.insert(line);
        } else {
            blocks(node, source, 0, false, &mut host, false);
        }
        if count + host.len() > PREVIEW_BLOCK_LIMIT {
            return Err("Rendered preview exceeds 4096 blocks.".into());
        }
        end = node.range.end;
    }
    fallback(&source[end..], &mut host);
    let unsupported: Vec<String> = entries.iter().filter(|(line, _)| !matched.contains(line)).map(|(line, index)| {
        let entry = &response.entries[*index];
        let mut notice = format!("Include at returned host line {} has an unsupported location; original host content is retained.", line + 1);
        if let Some(error) = entry.error.as_ref().filter(|e| !e.is_empty()) { notice.push_str(&format!("\nEngine error: {error}")); }
        for option in &entry.bad_options { notice.push_str(&format!("\nBad option: {option}")); }
        notice
    }).collect();
    if count + host.len() + unsupported.len() > PREVIEW_BLOCK_LIMIT {
        return Err("Rendered preview exceeds 4096 blocks.".into());
    }
    if !host.is_empty() {
        items.push(PreviewItem::Host(host));
    }
    Ok(IncludePreview { items, unsupported })
}

fn fallback(text: &str, out: &mut Vec<Block>) {
    if !text.trim().is_empty() {
        out.push(Block {
            block_id: None,
            kind: BlockKind::Literal,
            text: text.to_owned(),
            spans: vec![],
            links: vec![],
            language: None,
            fence_info: None,
            syntax: Arc::from([]),
            table: None,
            image: None,
            indent: 0,
            quoted: false,
            marker: None,
        });
    }
}

#[derive(Clone, Copy)]
struct ReaderTheme {
    ink: u32,
    muted: u32,
    soft: u32,
    line: u32,
    strong_line: u32,
}

impl ReaderTheme {
    fn new(dark: bool) -> Self {
        if dark {
            Self {
                ink: 0xe9e9e4,
                muted: 0xa2a29b,
                soft: 0x212528,
                line: 0x282c2f,
                strong_line: 0x3e4347,
            }
        } else {
            Self {
                ink: 0x1a1a18,
                muted: 0x5e5d58,
                soft: 0xf3f2ee,
                line: 0xe6e4de,
                strong_line: 0xc7c5bd,
            }
        }
    }
}

/// Compatibility wrapper for the original dark experiment shell.
#[allow(dead_code)]
pub fn render(blocks: &[Block]) -> Div {
    render_themed(blocks, true)
}

/// Native read-only prose. The shell owns the sheet ground and outer 880px cap.
/// Uses the system sans fallback; exact IBM Plex Sans JP typography is not claimed.
/// No HTML execution, network fetching, or link navigation is performed.
pub fn render_preview_code(blocks: &[Block], dark: bool, copy: &CodeCallbacks) -> Div {
    render_reader(blocks, dark, None, None, None, Some(copy), None)
}

pub fn render_themed(blocks: &[Block], dark: bool) -> Div {
    render_reader(blocks, dark, None, None, None, None, None)
}

/// Anchor-aware variant that preserves nested article layout. The outer reader
/// must track the same handle used to construct `anchors`.
pub fn render_themed_with_anchors(blocks: &[Block], dark: bool, anchors: &HeadingAnchors) -> Div {
    render_reader(blocks, dark, Some(anchors), None, None, None, None)
}

pub fn render_with_links(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: &LinkCallbacks,
) -> Div {
    render_reader(blocks, dark, anchors, Some(callbacks), None, None, None)
}

pub fn render_with_links_and_find(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: &LinkCallbacks,
    find: Option<FindPresentation<'_>>,
    copy: Option<&CodeCallbacks>,
    images: Option<&std::collections::BTreeMap<usize, crate::raster::Decoded>>,
) -> Div {
    render_reader(blocks, dark, anchors, Some(callbacks), find, copy, images)
}

/// Explicit fixture-position variant; ordinary callers collect no position geometry.
pub fn render_with_positions(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: &LinkCallbacks,
    find: Option<FindPresentation<'_>>,
    copy: Option<&CodeCallbacks>,
    images: Option<&std::collections::BTreeMap<usize, crate::raster::Decoded>>,
    positions: Option<&PositionAnchors>,
) -> Div {
    let reader = render_with_links_and_find(blocks, dark, anchors, callbacks, find, copy, images);
    if let Some(positions) = positions {
        positions.track(reader, blocks.len())
    } else {
        reader
    }
}

pub const BODY_LINE_HEIGHT: f32 = 29.6;
pub fn plain_reader_block(block: &Block) -> bool {
    block.kind == BlockKind::Paragraph
        && block.links.is_empty()
        && block.table.is_none()
        && block.image.is_none()
        && block.language.is_none()
        && block.marker.is_none()
        && !block.quoted
        && block.indent == 0
        && !block
            .spans
            .iter()
            .any(|span| span.style.code || span.style.link)
}

fn render_reader(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: Option<&LinkCallbacks>,
    find: Option<FindPresentation<'_>>,
    copy: Option<&CodeCallbacks>,
    images: Option<&std::collections::BTreeMap<usize, crate::raster::Decoded>>,
) -> Div {
    let theme = ReaderTheme::new(dark);
    div()
        .w_full()
        .min_w_0()
        .flex()
        .flex_col()
        .text_size(px(16.))
        .line_height(px(BODY_LINE_HEIGHT))
        .font_family(".SystemUIFont")
        .text_color(rgb(theme.ink))
        .children(render_elements(
            blocks, dark, anchors, callbacks, find, copy, images,
        ))
}

/// Direct block children for a shell-owned tracked scroller. Keep each element
/// as one direct child; wrapping the returned vector defeats child-index anchors.
/// Each row carries the same body metrics as render_themed.
pub fn render_block_elements_themed(blocks: &[Block], dark: bool) -> Vec<AnyElement> {
    render_elements(blocks, dark, None, None, None, None, None)
}

fn render_elements(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: Option<&LinkCallbacks>,
    find: Option<FindPresentation<'_>>,
    copy: Option<&CodeCallbacks>,
    images: Option<&std::collections::BTreeMap<usize, crate::raster::Decoded>>,
) -> Vec<AnyElement> {
    let theme = ReaderTheme::new(dark);
    let entries = anchors
        .map(|a| a.entries.clone())
        .unwrap_or_else(|| outline(blocks));
    let highlights = find_highlights(find);
    let find_generation = find.map(|f| {
        f.anchors
            .begin_layout(f.active.and_then(|i| f.matches.hits.get(i)).cloned())
    });
    blocks
        .iter()
        .enumerate()
        .map(|(index, block)| {
            let mut row = div()
                .w_full()
                .min_w_0()
                .text_size(px(16.))
                .line_height(px(BODY_LINE_HEIGHT))
                .font_family(".SystemUIFont")
                .text_color(rgb(theme.ink))
                .pl(px((block.indent * 16) as f32));
            if block.quoted {
                row = row
                    .border_l_2()
                    .border_color(rgb(theme.strong_line))
                    .pl(px(12. + (block.indent * 16) as f32));
            }
            if block.kind == BlockKind::Rule {
                // Keep one layout element per parsed block, including empty-text
                // separators, so later heading/Find geometry indices stay stable.
                return row
                    .max_w(px(640.))
                    .mx_auto()
                    .py(px(16.))
                    .flex()
                    .items_center()
                    .gap_2()
                    .when_some(block.marker.clone(), |row, marker| row.child(marker))
                    .child(div().flex_1().min_w_0().h(px(1.)).bg(rgb(theme.line)))
                    .into_any_element();
            }
            if let Some(table) = &block.table {
                return row
                    .mt(px(13.))
                    .child(render_table(
                        table,
                        theme,
                        callbacks,
                        index,
                        find,
                        &highlights,
                        dark,
                        find_generation,
                    ))
                    .into_any_element();
            }
            let text = block.marker.as_ref().map_or_else(
                || block.text.clone(),
                |marker| format!("{marker} {}", block.text),
            );
            let (mono, bold) = match block.kind {
                BlockKind::Heading(level) => {
                    let (size, margin) = match level {
                        1 => (26., 44.),
                        2 => (23., 44.),
                        3 => (21., 32.),
                        4 => (19., 26.),
                        _ => (18., 22.),
                    };
                    row = row
                        .max_w(px(640.))
                        .mx_auto()
                        .text_size(px(size))
                        .line_height(px(size * 1.5))
                        .mt(px(if index == 0 { 0. } else { margin }));
                    (false, true)
                }
                BlockKind::Code | BlockKind::Literal => {
                    row = row
                        .mt(px(13.))
                        .p_3()
                        .rounded(px(4.))
                        .bg(rgb(theme.soft))
                        .text_size(px(14.))
                        .line_height(px(23.));
                    (true, false)
                }
                _ => {
                    row = row.max_w(px(640.)).mx_auto().mt(px(if index == 0 {
                        0.
                    } else if block.indent > 0 {
                        7.
                    } else {
                        13.
                    }));
                    (false, false)
                }
            };
            if let Some(language) = &block.language {
                let group = SharedString::from(format!("code-bed-{index}"));
                row = row.group(group.clone());
                let mut header = div()
                    .flex()
                    .items_center()
                    .justify_between()
                    .mb_2()
                    .font_family("monospace")
                    .text_size(px(11.))
                    .text_color(rgb(theme.muted))
                    .child(language.to_uppercase());
                if let Some((focus, copied)) = copy.and_then(|copy| copy.controls.get(&index)) {
                    let activate = copy.unwrap().activate.clone();
                    let keyboard = activate.clone();
                    let focus = focus.clone();
                    let mouse_focus = focus.clone();
                    header = header.child(
                        div()
                            .id(SharedString::from(format!("copy-code-{index}")))
                            .track_focus(&focus)
                            .tab_index(0)
                            .opacity(if *copied { 1. } else { 0. })
                            .group_hover(group, |style| style.opacity(1.))
                            .focus(|style| style.opacity(1.).border_color(rgb(theme.strong_line)))
                            .border_1()
                            .border_color(rgb(theme.line))
                            .rounded(px(4.))
                            .px_2()
                            .cursor_pointer()
                            .child(
                                copy.and_then(|c| c.labels.as_ref())
                                    .and_then(|labels| labels.get(&index))
                                    .cloned()
                                    .unwrap_or_else(|| {
                                        if *copied {
                                            "Copied".into()
                                        } else {
                                            "Copy".into()
                                        }
                                    }),
                            )
                            .on_mouse_down(gpui::MouseButton::Left, move |_, window, cx| {
                                window.focus(&mouse_focus);
                                cx.stop_propagation();
                            })
                            .on_click(move |_, window, cx| {
                                activate(index, window, cx);
                                cx.stop_propagation();
                            })
                            .on_key_down(move |event, window, cx| {
                                if matches!(event.keystroke.key.as_str(), "enter" | "space")
                                    && !event.keystroke.modifiers.platform
                                    && !event.keystroke.modifiers.control
                                    && !event.keystroke.modifiers.alt
                                {
                                    keyboard(index, window, cx);
                                    cx.stop_propagation();
                                }
                            }),
                    );
                }
                if copy.is_some_and(|c| !c.controls.contains_key(&index)) {
                    if let Some(label) = copy
                        .and_then(|c| c.labels.as_ref())
                        .and_then(|labels| labels.get(&index))
                    {
                        header = header.child(label.clone());
                    }
                }
                row = row.child(header);
            }
            let offset = text.len() - block.text.len();
            let visible_spans = resolved_spans(&block.spans, &block.links, callbacks);
            let presentation = if block.kind == BlockKind::Code {
                let runs = code_runs(block, offset, dark);
                let hits: Vec<_> = highlights
                    .get(&Target::Block(index))
                    .into_iter()
                    .flatten()
                    .map(|(r, active)| (r.start + offset..r.end + offset, *active))
                    .collect();
                StyledText::new(text.clone()).with_runs(highlight_runs(runs, &hits, dark))
            } else {
                styled_text_find(
                    text,
                    &visible_spans,
                    offset,
                    theme,
                    mono,
                    bold,
                    highlights
                        .get(&Target::Block(index))
                        .map(Vec::as_slice)
                        .unwrap_or_default(),
                    dark,
                )
            };
            let row = if let (Some(find), Some(generation)) = (find, find_generation) {
                find.anchors.track_text(
                    row,
                    &presentation,
                    &block.text,
                    offset,
                    &Target::Block(index),
                    generation,
                )
            } else {
                row
            };
            let row = row.child(link_text(
                presentation,
                &block.links,
                offset,
                callbacks,
                format!("md-block-{index}"),
                theme,
            ));
            let row = if let Some(image) = images.and_then(|images| images.get(&index)) {
                let (width, ratio) = crate::raster::inline_geometry(image.width, image.height);
                row.child(
                    // Img resolves auto height from its requested width before
                    // max-width clamping. Let the parent own coupled geometry;
                    // the absolute image cannot contribute intrinsic sizing.
                    div()
                        .relative()
                        .w(px(width))
                        .max_w_full()
                        .map(|mut frame| {
                            frame.style().aspect_ratio = Some(ratio);
                            frame
                        })
                        .child(
                            gpui::img(image.image.clone())
                                .absolute()
                                .inset_0()
                                .size_full()
                                .object_fit(gpui::ObjectFit::Contain),
                        ),
                )
                .child(
                    div()
                        .id(SharedString::from(format!("enlarge-image-{index}")))
                        .text_size(px(12.))
                        .child("Enlarge")
                        .when_some(
                            callbacks.and_then(|callbacks| callbacks.enlarge.clone()),
                            |control, enlarge| {
                                control.cursor_pointer().on_click(move |_, window, cx| {
                                    enlarge(index, window, cx);
                                    cx.stop_propagation();
                                })
                            },
                        ),
                )
            } else {
                row
            };
            let element = if let Some((id, (_, anchor))) = anchors.and_then(|anchors| {
                anchors
                    .authored
                    .iter()
                    .find(|(_, (target, _))| *target == index)
            }) {
                row.id(SharedString::from(format!("markdown-block:{id}")))
                    .anchor_scroll(Some(anchor.clone()))
                    .when_some(
                        callbacks
                            .filter(|_| plain_reader_block(block))
                            .and_then(|callbacks| callbacks.plain_click.clone()),
                        |row, activate| {
                            row.on_click(move |event, window, cx| {
                                activate(index, event, window, cx)
                            })
                        },
                    )
                    .into_any_element()
            } else if let Some(entry) = entries.iter().find(|entry| entry.block_index == index) {
                row.id(SharedString::from(format!(
                    "markdown-heading:{}",
                    entry.anchor
                )))
                .anchor_scroll(
                    anchors
                        .and_then(|anchors| anchors.targets.get(&entry.anchor))
                        .cloned(),
                )
                .into_any_element()
            } else if let Some((index, anchor)) = anchors.and_then(|anchors| {
                anchors
                    .footnotes
                    .values()
                    .find(|(target, _)| *target == index)
            }) {
                row.id(SharedString::from(format!("markdown-footnote-{index}")))
                    .anchor_scroll(Some(anchor.clone()))
                    .into_any_element()
            } else if let Some(activate) = callbacks
                .filter(|_| plain_reader_block(block))
                .and_then(|callbacks| callbacks.plain_click.as_ref())
            {
                let activate = activate.clone();
                row.id(SharedString::from(format!("plain-reader-{index}")))
                    .on_click(move |event, window, cx| activate(index, event, window, cx))
                    .into_any_element()
            } else {
                row.into_any_element()
            };
            // Copy and Find may coexist after Find closes. Lay out both retained
            // anchors; choosing just one leaves the other with stale/no bounds.
            let element = if let Some(anchor) = copy
                .filter(|_| block.language.is_some())
                .and_then(|copy| copy.anchors.as_ref())
                .and_then(|anchors| anchors.targets.get(&Target::Block(index)))
            {
                div()
                    .id(SharedString::from(format!("code-block-anchor-{index}")))
                    .w_full()
                    .anchor_scroll(Some(anchor.clone()))
                    .child(element)
                    .into_any_element()
            } else {
                element
            };
            if let Some(anchor) = find.and_then(|f| f.anchors.targets.get(&Target::Block(index))) {
                div()
                    .id(SharedString::from(format!("find-block-{index}")))
                    .w_full()
                    .anchor_scroll(Some(anchor.clone()))
                    .child(element)
                    .into_any_element()
            } else {
                element
            }
        })
        .collect()
}

fn resolved_spans(spans: &[Span], links: &[Link], callbacks: Option<&LinkCallbacks>) -> Vec<Span> {
    spans
        .iter()
        .map(|span| {
            let mut span = span.clone();
            if span.style.link {
                let resolved = links.iter().any(|link| {
                    link.range.start <= span.range.start
                        && link.range.end >= span.range.end
                        && (matches!(link.target, LinkTarget::Footnote { .. })
                            || resolve_term(&link.target).is_some()
                            || is_local_fragment(&link.target)
                            || crate::external_links::destination(&link.target).is_some())
                        && callbacks.is_some_and(|callbacks| (callbacks.resolved)(&link.target))
                });
                span.style.link = resolved;
                span.style.unresolved = !resolved;
            }
            span
        })
        .collect()
}

struct DestinationTooltip(String, ReaderTheme);
impl gpui::Render for DestinationTooltip {
    fn render(&mut self, _: &mut Window, _: &mut gpui::Context<Self>) -> impl IntoElement {
        div()
            .px_2()
            .py_1()
            .max_w(px(600.))
            .bg(rgb(self.1.soft))
            .border_1()
            .border_color(rgb(self.1.line))
            .whitespace_normal()
            .text_color(rgb(self.1.ink))
            .text_size(px(12.))
            .child(self.0.clone())
    }
}

fn link_text(
    text: StyledText,
    links: &[Link],
    offset: usize,
    callbacks: Option<&LinkCallbacks>,
    id: String,
    theme: ReaderTheme,
) -> AnyElement {
    let Some(callbacks) = callbacks else {
        return text.into_any_element();
    };
    let active: Vec<_> = links
        .iter()
        .filter(|link| {
            (matches!(link.target, LinkTarget::Footnote { .. })
                || resolve_term(&link.target).is_some()
                || is_local_fragment(&link.target)
                || crate::external_links::destination(&link.target).is_some())
                && (callbacks.resolved)(&link.target)
        })
        .cloned()
        .collect();
    let ranges = active
        .iter()
        .map(|link| link.range.start + offset..link.range.end + offset)
        .collect();
    // Destination text only: no title, icon, network request, or opener on hover.
    let destinations: Vec<_> = active
        .iter()
        .filter_map(|link| {
            crate::external_links::destination(&link.target)
                .map(|url| (link.range.start + offset..link.range.end + offset, url))
        })
        .collect();
    let activate = callbacks.activate.clone();
    InteractiveText::new(SharedString::from(id), text)
        .tooltip(move |index, _, cx| {
            destinations
                .iter()
                .find(|(range, _)| range.contains(&index))
                .map(|(_, url)| cx.new(|_| DestinationTooltip(url.clone(), theme)).into())
        })
        .on_click(ranges, move |index, window, cx| {
            if let Some(link) = active.get(index) {
                activate(link, window, cx);
            }
        })
        .into_any_element()
}

fn styled_runs(
    text: String,
    spans: &[Span],
    offset: usize,
    theme: ReaderTheme,
    mono: bool,
    bold: bool,
) -> Vec<TextRun> {
    let mut runs = Vec::new();
    let mut cursor = 0;
    for span in spans {
        let start = span.range.start + offset;
        let end = span.range.end + offset;
        if start > cursor {
            runs.push(text_run(
                start - cursor,
                InlineStyle::default(),
                theme,
                mono,
                bold,
            ));
        }
        runs.push(text_run(end - start, span.style, theme, mono, bold));
        cursor = end;
    }
    if cursor < text.len() {
        runs.push(text_run(
            text.len() - cursor,
            InlineStyle::default(),
            theme,
            mono,
            bold,
        ));
    }
    runs
}

/// Theme mapping consumes cached ranges; classification never occurs in rendering.
fn code_runs(block: &Block, offset: usize, dark: bool) -> Vec<TextRun> {
    use crate::code_highlight::Role;
    let theme = ReaderTheme::new(dark);
    if block.syntax.is_empty() {
        return vec![text_run(
            offset + block.text.len(),
            InlineStyle::default(),
            theme,
            true,
            false,
        )];
    }
    let mut runs = Vec::new();
    if offset > 0 {
        runs.push(text_run(offset, InlineStyle::default(), theme, true, false));
    }
    for token in block.syntax.iter() {
        let mut run = text_run(
            token.range.len(),
            InlineStyle {
                emphasis: token.role == Role::Comment,
                strong: token.role == Role::Keyword,
                ..InlineStyle::default()
            },
            theme,
            true,
            false,
        );
        let color = match token.role {
            Role::Plain | Role::Keyword => theme.ink,
            Role::Comment => theme.muted,
            Role::String => {
                if dark {
                    0xa9d89e
                } else {
                    0x28652e
                }
            }
            Role::Number => {
                if dark {
                    0xe8b587
                } else {
                    0x985116
                }
            }
            Role::Function => {
                if dark {
                    0xbcb8f0
                } else {
                    0x6543a0
                }
            }
            Role::Property => {
                if dark {
                    0x8dcbdc
                } else {
                    0x17667d
                }
            }
        };
        run.color = rgb(color).into();
        runs.push(run);
    }
    runs
}

type FindHighlights = std::collections::HashMap<Target, Vec<(Range<usize>, bool)>>;
fn find_highlights(find: Option<FindPresentation<'_>>) -> FindHighlights {
    let mut grouped = FindHighlights::new();
    if let Some(find) = find {
        for (index, hit) in find.matches.hits.iter().enumerate() {
            grouped
                .entry(hit.target.clone())
                .or_default()
                .push((hit.range.clone(), find.active == Some(index)));
        }
    }
    grouped
}

fn styled_text_find(
    text: String,
    spans: &[Span],
    offset: usize,
    theme: ReaderTheme,
    mono: bool,
    bold: bool,
    highlights: &[(Range<usize>, bool)],
    dark: bool,
) -> StyledText {
    let runs = styled_runs(text.clone(), spans, offset, theme, mono, bold);
    let highlights: Vec<_> = highlights
        .iter()
        .map(|(r, active)| ((r.start + offset)..(r.end + offset), *active))
        .filter(|(range, _)| range.start < range.end && text.get(range.clone()).is_some())
        .collect();
    StyledText::new(text).with_runs(highlight_runs(runs, &highlights, dark))
}

/// Split only at validated original-text boundaries; all original styles survive.
fn highlight_runs(
    runs: Vec<TextRun>,
    highlights: &[(Range<usize>, bool)],
    dark: bool,
) -> Vec<TextRun> {
    let mut output = Vec::new();
    let mut position = 0;
    let mut hit = 0;
    for run in runs {
        let end = position + run.len;
        while position < end {
            while hit < highlights.len() && highlights[hit].0.end <= position {
                hit += 1;
            }
            let current = highlights.get(hit);
            let colored = current.filter(|(range, _)| range.start <= position);
            let next = match current {
                Some((range, _)) if range.start > position => end.min(range.start),
                Some((range, _)) => end.min(range.end),
                None => end,
            };
            let mut piece = run.clone();
            piece.len = next - position;
            if let Some((_, active)) = colored {
                piece.background_color = Some(
                    rgb(match (dark, *active) {
                        (true, true) => 0x93432f,
                        (true, false) => 0x51432c,
                        (false, true) => 0xf0b09a,
                        (false, false) => 0xf7e4a2,
                    })
                    .into(),
                );
            }
            output.push(piece);
            position = next;
        }
    }
    output
}

fn text_run(len: usize, style: InlineStyle, theme: ReaderTheme, mono: bool, bold: bool) -> TextRun {
    let mut face = font(if mono || style.code {
        "Menlo"
    } else {
        ".SystemUIFont"
    });
    if bold || style.strong {
        face.weight = FontWeight::BOLD;
    }
    if style.emphasis {
        face.style = FontStyle::Italic;
    }
    TextRun {
        len,
        font: face,
        color: rgb(if style.code || style.unresolved {
            theme.muted
        } else {
            theme.ink
        })
        .into(),
        background_color: None,
        underline: style.link.then(|| UnderlineStyle {
            thickness: px(1.),
            color: Some(rgb(theme.strong_line).into()),
            wavy: false,
        }),
        strikethrough: style.strike.then(|| StrikethroughStyle {
            thickness: px(1.),
            color: None, // Inherit foreground, including unresolved links and code.
        }),
    }
}

fn render_table(
    table: &Table,
    theme: ReaderTheme,
    callbacks: Option<&LinkCallbacks>,
    block_index: usize,
    find: Option<FindPresentation<'_>>,
    highlights: &FindHighlights,
    dark: bool,
    find_generation: Option<u64>,
) -> Div {
    div()
        .w_full()
        .min_w_0()
        .flex()
        .flex_col()
        .text_size(px(14.))
        .line_height(px(23.))
        .border_1()
        .border_color(rgb(theme.line))
        .children(table.rows.iter().enumerate().map(|(index, cells)| {
            let mut row = div().w_full().min_w_0().flex();
            if index == 0 {
                row = row.bg(rgb(theme.soft));
            }
            row.children(cells.iter().enumerate().map(|(column, cell)| {
                let mut view = div()
                    .flex_1()
                    .min_w_0()
                    .p_2()
                    .border_b_1()
                    .border_r_1()
                    .border_color(rgb(theme.line));
                view = match table.alignments.get(column) {
                    Some(Alignment::Center) => view.text_center(),
                    Some(Alignment::Right) => view.text_right(),
                    _ => view,
                };
                let visible_spans = resolved_spans(&cell.spans, &cell.links, callbacks);
                let target = Target::Cell {
                    block: block_index,
                    row: index,
                    column,
                };
                let presentation = styled_text_find(
                    cell.text.clone(),
                    &visible_spans,
                    0,
                    theme,
                    false,
                    index == 0,
                    highlights
                        .get(&target)
                        .map(Vec::as_slice)
                        .unwrap_or_default(),
                    dark,
                );
                let view = if let (Some(find), Some(generation)) = (find, find_generation) {
                    find.anchors
                        .track_text(view, &presentation, &cell.text, 0, &target, generation)
                } else {
                    view
                };
                let view = view.child(link_text(
                    presentation,
                    &cell.links,
                    0,
                    callbacks,
                    format!("md-table-{block_index}-{index}-{column}"),
                    theme,
                ));
                view.id(SharedString::from(format!(
                    "find-cell-{block_index}-{index}-{column}"
                )))
                .anchor_scroll(find.and_then(|f| f.anchors.targets.get(&target)).cloned())
            }))
        }))
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn python_fences_keep_copy_find_and_cached_source_lossless() {
        let body = "def greeting():\n    return '''日本語🦀\n# False [[id:123]]'''\n";
        for language in ["python", "py", "python3"] {
            let blocks = parse(&format!("```{language}\n{body}```"));
            let block = &blocks[0];
            assert_eq!(block.text, body);
            assert!(block.links.is_empty());
            assert!(
                block
                    .syntax
                    .iter()
                    .any(|token| token.role == crate::code_highlight::Role::Keyword)
            );
            let projected = with_resolved_titles(&blocks, &std::collections::HashMap::new());
            assert!(Arc::ptr_eq(&block.syntax, &projected[0].syntax));
            let found = crate::reader_find::search(&blocks, "日本語🦀");
            assert_eq!(found.hits.len(), 1);
            assert_eq!(&body[found.hits[0].range.clone()], "日本語🦀");
            for dark in [false, true] {
                assert_eq!(
                    code_runs(block, 0, dark)
                        .iter()
                        .map(|run| run.len)
                        .sum::<usize>(),
                    body.len()
                );
            }
            let document = Some(crate::code_copy::Document {
                tab: None,
                request: 1,
                etag: None,
                storage: None,
                launch: None,
            });
            let mut copy = crate::code_copy::State::default();
            copy.sync(document.clone(), &blocks);
            assert_eq!(
                copy.body(copy.epoch, 0, &document, &blocks).as_deref(),
                Some(body)
            );
        }
    }

    #[test]
    fn shell_fences_keep_copy_find_and_cached_source_lossless() {
        let body = "if true; then\n  echo '日本語🦀 [[id:123]]' # comment\nfi\n";
        for language in ["bash", "sh", "shell"] {
            let blocks = parse(&format!("```{language}\n{body}```"));
            let block = &blocks[0];
            assert_eq!(block.text, body);
            assert!(block.links.is_empty());
            assert!(
                block
                    .syntax
                    .iter()
                    .any(|token| token.role == crate::code_highlight::Role::Keyword)
            );
            let projected = with_resolved_titles(&blocks, &std::collections::HashMap::new());
            assert!(Arc::ptr_eq(&block.syntax, &projected[0].syntax));
            let found = crate::reader_find::search(&blocks, "日本語🦀");
            assert_eq!(found.hits.len(), 1);
            assert_eq!(&body[found.hits[0].range.clone()], "日本語🦀");
            for dark in [false, true] {
                assert_eq!(
                    code_runs(block, 0, dark)
                        .iter()
                        .map(|run| run.len)
                        .sum::<usize>(),
                    body.len()
                );
            }
            let document = Some(crate::code_copy::Document {
                tab: None,
                request: 1,
                etag: None,
                storage: None,
                launch: None,
            });
            let mut copy = crate::code_copy::State::default();
            copy.sync(document.clone(), &blocks);
            assert_eq!(
                copy.body(copy.epoch, 0, &document, &blocks).as_deref(),
                Some(body)
            );
        }
    }

    #[test]
    fn cached_code_styles_preserve_find_copy_caption_and_literal_boundary() {
        let source = "```language-TS caption\nconst value = compute(42); \"日本語🦀\"; // 42 [[id:123]] https://example.com <img>\n```";
        let blocks = parse(source);
        let block = &blocks[0];
        assert_eq!(block.language.as_deref(), Some("language-TS"));
        assert!(block.links.is_empty());
        assert!(block.spans.is_empty());
        let projected = with_resolved_titles(
            &blocks,
            &std::collections::HashMap::from([("id:123".into(), "changed".into())]),
        );
        assert_eq!(blocks, projected);
        assert!(Arc::ptr_eq(&block.syntax, &projected[0].syntax));
        let found = crate::reader_find::search(&blocks, "42");
        assert_eq!(found.hits.len(), 2);
        let hits: Vec<_> = found
            .hits
            .iter()
            .enumerate()
            .map(|(i, hit)| {
                assert_eq!(hit.target, Target::Block(0));
                assert_eq!(&block.text[hit.range.clone()], "42");
                (hit.range.clone(), i == 0)
            })
            .collect();
        for dark in [false, true] {
            let runs = code_runs(block, 0, dark);
            let decorated = highlight_runs(runs.clone(), &hits, dark);
            assert_eq!(
                decorated.iter().map(|r| r.len).sum::<usize>(),
                block.text.len()
            );
            let mut position = 0;
            for run in &decorated {
                let original = runs
                    .iter()
                    .scan(0, |end, r| {
                        let start = *end;
                        *end += r.len;
                        Some((start..*end, r))
                    })
                    .find(|(range, _)| range.contains(&position))
                    .unwrap()
                    .1;
                assert_eq!(run.font, original.font);
                assert_eq!(run.color, original.color);
                assert_eq!(
                    run.background_color.is_some(),
                    hits.iter().any(|(r, _)| r.contains(&position))
                );
                position += run.len;
                assert!(block.text.is_char_boundary(position));
            }
            assert!(Arc::ptr_eq(&block.syntax, &projected[0].syntax));
        }
        assert_ne!(
            code_runs(block, 0, false)[0].color,
            code_runs(block, 0, true)[0].color
        );
        let document = Some(crate::code_copy::Document {
            tab: None,
            request: 1,
            etag: None,
            storage: None,
            launch: None,
        });
        let mut copy = crate::code_copy::State::default();
        copy.sync(document.clone(), &blocks);
        assert_eq!(
            copy.body(copy.epoch, 0, &document, &blocks).as_deref(),
            Some(block.text.as_str())
        );
        let changed_body = parse(&source.replace("const", "hello"));
        assert_ne!(block.syntax, changed_body[0].syntax);
        let changed_language = parse(&source.replace("language-TS", "unknown"));
        assert!(changed_language[0].syntax.is_empty());
        assert_eq!(block.text, changed_language[0].text);
        let normalized_language = parse(&source.replace("language-TS", "typescript"));
        assert_eq!(block.syntax, normalized_language[0].syntax);
    }

    #[test]
    fn huge_code_falls_back_without_changing_body_find_or_copy() {
        let body = format!(
            "{}日本語🦀\n",
            "x".repeat(crate::code_highlight::MAX_BODY_BYTES)
        );
        let blocks = parse(&format!("```js\n{body}```"));
        assert_eq!(blocks[0].text, body);
        assert!(blocks[0].syntax.is_empty());
        assert_eq!(code_runs(&blocks[0], 0, true).len(), 1);
        let hits = crate::reader_find::search(&blocks, "日本語🦀");
        assert_eq!(&body[hits.hits[0].range.clone()], "日本語🦀");
    }

    #[test]
    fn empty_tasks_and_nested_children_do_not_lose_or_share_state() {
        // The parser requires whitespace after a marker: EOF alone stays literal.
        assert_eq!(parse("- [x]")[0].text, "[x]");
        assert_eq!(parse("- [x] ")[0].marker.as_deref(), Some("☑"));
        let empty = parse("- [ ]\n- [x]\n- [X]\n");
        assert_eq!(empty.len(), 3);
        assert!(empty.iter().all(|b| b.text.is_empty()), "{empty:?}");
        assert_eq!(
            empty
                .iter()
                .map(|b| b.marker.as_deref())
                .collect::<Vec<_>>(),
            vec![Some("☐"), Some("☑"), Some("☑")]
        );
        let nested = parse("- [ ]\n  - [x] child\n- parent\n  - [x] nested\n\n1. [x] one\n2. two");
        assert_eq!(
            nested
                .iter()
                .map(|b| (b.marker.as_deref(), b.text.as_str()))
                .collect::<Vec<_>>(),
            vec![
                (Some("☐"), ""),
                (Some("☑"), "child"),
                (Some("•"), "parent"),
                (Some("☑"), "nested"),
                (Some("☑"), "one"),
                (Some("2."), "two")
            ]
        );
        let only_child = parse("-\n  - [x] child");
        assert_eq!(only_child[0].marker.as_deref(), Some("☑"));
    }
    #[test]
    fn task_and_strike_keep_block_anchor_indexes_and_plain_text_boundaries() {
        let styled = parse("- [x] ~~done~~\n\n# ~~Heading~~\n\n```txt\n[x] ~~literal~~\n```");
        let plain = parse("- done\n\n# Heading\n\n```txt\n[x] ~~literal~~\n```");
        assert_eq!(outline(&styled), outline(&plain));
        assert_eq!(styled.len(), plain.len());
        assert_eq!(styled[2].text, plain[2].text);
        for source in ["[x] prose", "x [x] y", "- \\[x] escaped"] {
            let blocks = parse(source);
            assert!(blocks[0].text.contains("[x]"));
            assert_ne!(blocks[0].marker.as_deref(), Some("☑"));
        }
        // pulldown-cmark's enabled strike extension also accepts single tilde.
        // This observation is not a claim of complete reference dialect parity.
        assert_eq!(parse("~single~")[0].text, "single");
        assert!(parse("~single~")[0].spans[0].style.strike);
    }
    #[test]
    fn task_markers_are_read_only_presentation_not_label_styles() {
        let blocks = parse(
            "- [ ] pending\n- [x] done\n- [X] upper\n- ordinary\n\n1. [ ] ordered\n2. normal",
        );
        assert_eq!(
            blocks
                .iter()
                .map(|b| (b.marker.as_deref(), b.text.as_str()))
                .collect::<Vec<_>>(),
            vec![
                (Some("☐"), "pending"),
                (Some("☑"), "done"),
                (Some("☑"), "upper"),
                (Some("•"), "ordinary"),
                (Some("☐"), "ordered"),
                (Some("2."), "normal")
            ]
        );
        assert!(blocks.iter().all(|b| b.spans.is_empty()));
        assert!(crate::reader_find::search(&blocks, "[x]").hits.is_empty());
        assert!(crate::reader_find::search(&blocks, "☑").hits.is_empty());
    }
    #[test]
    fn nested_quoted_and_loose_tasks_keep_independent_markers() {
        let blocks = parse(
            "- [x] parent\n  - [ ] child\n\n- [ ] loose\n\n  continuation\n\n> - [X] quoted\n\nafter",
        );
        assert_eq!(
            blocks.iter().map(|b| b.text.as_str()).collect::<Vec<_>>(),
            vec![
                "parent",
                "child",
                "loose",
                "continuation",
                "quoted",
                "after"
            ]
        );
        assert_eq!(blocks[0].marker.as_deref(), Some("☑"));
        assert_eq!(blocks[1].marker.as_deref(), Some("☐"));
        assert_eq!(blocks[1].indent, 2);
        assert_eq!(blocks[2].marker.as_deref(), Some("☐"));
        assert!(blocks[3].marker.is_none());
        assert!(blocks[4].quoted);
        assert_eq!(blocks[4].marker.as_deref(), Some("☑"));
    }
    #[test]
    fn strike_composes_with_links_titles_and_find_without_mutating_source() {
        let source = "- [x] before ~~**日本語** *italic* [[id:123]] [[id:456]] [[id:789|alias]]~~ after [link](Note.md)";
        let original = parse(source);
        let titles = std::collections::HashMap::from([
            ("id:123".into(), "長いタイトル 🦀".into()),
            ("id:456".into(), "短".into()),
            ("id:789".into(), "ignored".into()),
        ]);
        let displayed = with_resolved_titles(&original, &titles);
        assert_eq!(original, parse(source));
        let b = &displayed[0];
        assert_eq!(
            b.text,
            "before 日本語 italic 長いタイトル 🦀 短 alias after link"
        );
        for (link, label) in b
            .links
            .iter()
            .zip(["長いタイトル 🦀", "短", "alias", "link"])
        {
            assert_eq!(&b.text[link.range.clone()], label);
            let style = b
                .spans
                .iter()
                .find(|s| s.range == link.range)
                .unwrap()
                .style;
            assert!(style.link);
            assert_eq!(style.strike, label != "link");
        }
        assert!(b.spans.iter().any(|s| s.style.strong && s.style.strike));
        assert!(b.spans.iter().any(|s| s.style.emphasis && s.style.strike));
        let found = crate::reader_find::search(&displayed, "タイトル");
        assert_eq!(found.hits.len(), 1);
        assert_eq!(&b.text[found.hits[0].range.clone()], "タイトル");
        let text = format!("{} {}", b.marker.as_ref().unwrap(), b.text);
        let offset = text.len() - b.text.len();
        let runs = styled_runs(
            text.clone(),
            &b.spans,
            offset,
            ReaderTheme::new(false),
            false,
            false,
        );
        let highlighted = highlight_runs(
            runs,
            &[(
                offset + found.hits[0].range.start..offset + found.hits[0].range.end,
                true,
            )],
            false,
        );
        assert_eq!(highlighted.iter().map(|r| r.len).sum::<usize>(), text.len());
        assert!(highlighted.iter().any(|r| r.strikethrough.is_some()
            && r.underline.is_some()
            && r.background_color.is_some()));
    }
    #[test]
    fn strike_in_headings_tables_and_literal_boundaries() {
        let blocks = parse(
            "# ~~heading~~\n\n| A |\n|---|\n| ~~日本語~~ |\n\n[ ] prose x [x] y `~~code~~ [x]` \\~\\~escaped\\~\\~ ~~unmatched\n\n```txt\n- [x] ~~literal~~\n```\n\n- [>] custom #tag 2026-11-14\n- [-] custom",
        );
        assert_eq!(blocks[0].text, "heading");
        assert!(blocks[0].spans[0].style.strike);
        let cell = &blocks[1].table.as_ref().unwrap().rows[1][0];
        assert_eq!(cell.text, "日本語");
        assert!(cell.spans[0].style.strike);
        assert_eq!(
            blocks[2].text,
            "[ ] prose x [x] y ~~code~~ [x] ~~escaped~~ ~~unmatched"
        );
        assert!(blocks[2].spans.iter().all(|s| !s.style.strike));
        assert_eq!(blocks[3].text, "- [x] ~~literal~~\n");
        assert!(blocks[3].spans.is_empty());
        assert_eq!(blocks[4].text, "[>] custom #tag 2026-11-14");
        assert_eq!(blocks[5].text, "[-] custom");
        assert_eq!(blocks[4].marker.as_deref(), Some("•"));
    }
    /// Synthetic model-only cost, not GPUI layout/render/frame timing.
    /// Run explicitly with --ignored --nocapture projection_cost_synthetic.
    #[test]
    #[ignore = "manual synthetic projection microbenchmark; not frame timing"]
    fn projection_cost_synthetic() {
        use std::{hint::black_box, time::Instant};
        const SAMPLES: usize = 30;
        for count in [100, 1_000, 10_000] {
            let source = (0..count).map(|i| format!(
                "Paragraph {i}: **styled words** before [[id:123]] and Unicode 日本語 after the link.\n\n"
            )).collect::<String>();
            let blocks = parse(&source);
            assert_eq!(blocks.len(), count);
            for (case, titles) in [
                ("zero", std::collections::HashMap::new()),
                (
                    "every-block",
                    [(
                        "id:123".into(),
                        "Resolved title 日本語 longer than ID".into(),
                    )]
                    .into_iter()
                    .collect(),
                ),
            ] {
                black_box(with_resolved_titles(&blocks, &titles));
                let mut samples_ms = Vec::with_capacity(SAMPLES);
                for _ in 0..SAMPLES {
                    let start = Instant::now();
                    // Include allocating/projecting and freeing the projection, but
                    // exclude source creation/parsing and output/logging.
                    drop(black_box(with_resolved_titles(
                        black_box(&blocks),
                        black_box(&titles),
                    )));
                    samples_ms.push(start.elapsed().as_secs_f64() * 1_000.0);
                }
                // Separately measure cached Rc access, not projection or full
                // provenance checks. Batch operations to reduce timer overhead.
                let cached = Rc::new(with_resolved_titles(&blocks, &titles));
                let start = Instant::now();
                for _ in 0..100_000 {
                    drop(black_box(black_box(&cached).clone()));
                }
                let cached_ns = start.elapsed().as_secs_f64() * 1e9 / 100_000.0;
                println!(
                    "cached-Rc-clone-drop-only blocks={count} substitutions={case} mean_ns={cached_ns:.2} iterations=100000"
                );
                samples_ms.sort_by(f64::total_cmp);
                println!(
                    "projection-only blocks={count} substitutions={case} samples={SAMPLES} median_ms={:.3} p95_ms={:.3} min_ms={:.3}",
                    samples_ms[SAMPLES / 2],
                    samples_ms[(SAMPLES * 95 / 100).min(SAMPLES - 1)],
                    samples_ms[0]
                );
            }
        }
    }

    #[test]
    fn resolved_titles_only_replace_unaliased_explicit_id_wikilinks() {
        let cases = [
            ("[[id:123]]", "Current title"),
            ("[[work:id:123]]", "Current title"),
            ("[[work:id:123#Heading]]", "Current title"),
            ("[[id:123#^block]]", "Current title"),
            ("[[id:123|Custom label]]", "Custom label"),
            ("[[id:123|id:123]]", "id:123"),
            ("[[id:123| ]]", "Current title"),
            ("[[Some old title]]", "Some old title"),
            ("[[123]]", "123"),
            ("[id:123](id:123)", "id:123"),
            ("[Custom](SomeNote.md)", "Custom"),
            ("[[Work:id:123]]", "Work:id:123"),
            ("[[ID:123]]", "ID:123"),
        ];
        for (source, expected) in cases {
            let raw = parse(source);
            let titles = raw[0]
                .links
                .iter()
                .filter_map(|link| resolve_term(&link.target))
                .map(|key| (key, "Current title".into()))
                .collect();
            let visible = with_resolved_titles(&raw, &titles);
            assert_eq!(visible[0].text, expected, "{source}");
            assert_eq!(visible[0].links[0].target, raw[0].links[0].target);
            assert_eq!(with_resolved_titles(&raw, &Default::default()), raw);
        }
    }

    #[test]
    fn title_projection_remaps_unicode_styles_links_and_contexts_without_parsing_titles() {
        let source = "# [[id:123]] heading\n\n- **[[work:id:123]]** tail [[id:123#Part]] *after*\n\n| Col | Other |\n| --- | --- |\n| [[id:123]] | [[work:id:123]] |\n\n`[[id:123]]`\n\n```text\n[[id:123]]\n```";
        let raw = parse(source);
        let snapshot = raw.clone();
        let titles = [
            ("id:123".into(), "短".into()),
            ("work:id:123".into(), "長い **Title** [[Other]]".into()),
        ]
        .into_iter()
        .collect();
        let visible = with_resolved_titles(&raw, &titles);
        assert_eq!(raw, snapshot);
        assert_eq!(visible[0].text, "短 heading");
        let list = visible
            .iter()
            .find(|block| block.text.contains("tail"))
            .unwrap();
        assert_eq!(list.text, "長い **Title** [[Other]] tail 短 after");
        assert_eq!(list.links.len(), 2);
        assert_eq!(
            &list.text[list.links[0].range.clone()],
            "長い **Title** [[Other]]"
        );
        assert_eq!(&list.text[list.links[1].range.clone()], "短");
        assert!(list.spans.iter().any(|span| span.style.strong
            && &list.text[span.range.clone()] == "長い **Title** [[Other]]"));
        assert!(
            list.spans
                .iter()
                .any(|span| span.style.emphasis && &list.text[span.range.clone()] == "after")
        );
        let table = visible
            .iter()
            .find_map(|block| block.table.as_ref())
            .unwrap();
        assert_eq!(table.rows[1][0].text, "短");
        assert_eq!(table.rows[1][1].text, "長い **Title** [[Other]]");
        assert_eq!(
            visible.iter().find(|b| b.kind == BlockKind::Code),
            raw.iter().find(|b| b.kind == BlockKind::Code)
        );
        assert!(visible.iter().any(|b| b.text == "[[id:123]]"));
        assert_eq!(crate::reader_find::search(&visible, "短").hits.len(), 3);
        assert!(crate::reader_find::search(&raw, "短").hits.is_empty());
    }

    #[test]
    fn empty_resolved_title_is_not_replaced_by_an_invented_label() {
        let raw = parse("before [[id:123]] after [[id:456]] end");
        let titles = [
            ("id:123".into(), String::new()),
            ("id:456".into(), "終".into()),
        ]
        .into_iter()
        .collect();
        let visible = with_resolved_titles(&raw, &titles);
        assert_eq!(visible[0].text, "before  after 終 end");
        assert!(visible[0].links[0].range.is_empty());
        assert_eq!(&visible[0].text[visible[0].links[1].range.clone()], "終");
    }

    #[test]
    fn find_splits_runs_without_changing_style_or_link_offsets() {
        let theme = ReaderTheme::new(false);
        let style = InlineStyle {
            strong: true,
            emphasis: true,
            code: true,
            link: true,
            ..Default::default()
        };
        let original = text_run(9, style, theme, true, false);
        let runs = highlight_runs(
            vec![original.clone()],
            &[(2..5, false), (6..9, true)],
            false,
        );
        assert_eq!(
            runs.iter().map(|r| r.len).collect::<Vec<_>>(),
            vec![2, 3, 1, 3]
        );
        for run in &runs {
            let mut plain = run.clone();
            plain.len = original.len;
            plain.background_color = None;
            assert_eq!(plain, original);
        }
        assert_ne!(runs[1].background_color, runs[3].background_color);
        let blocks = parse("- **日本** [link](note) tail");
        let before = blocks[0].links.clone();
        let hits = crate::reader_find::search(&blocks, "日本 link");
        assert_eq!(hits.hits.len(), 1);
        assert_eq!(blocks[0].links, before);
        // List marker lies outside hit byte ranges and receives no highlight.
        let offset = blocks[0].marker.as_ref().unwrap().len() + 1;
        let text = format!("{} {}", blocks[0].marker.as_ref().unwrap(), blocks[0].text);
        let base = styled_runs(text, &blocks[0].spans, offset, theme, false, false);
        let range = hits.hits[0].range.clone();
        let runs = highlight_runs(
            base,
            &[((range.start + offset)..(range.end + offset), true)],
            false,
        );
        assert!(runs[0].background_color.is_none());
        assert_eq!(runs[0].len, offset);
    }
    #[test]
    fn dense_find_sweep_is_bounded_by_runs_and_hits() {
        let count = crate::reader_find::MAX_MATCHES;
        let highlights = (0..count)
            .map(|i| ((i * 2)..(i * 2 + 1), i == count - 1))
            .collect::<Vec<_>>();
        let original = text_run(
            count * 2,
            InlineStyle::default(),
            ReaderTheme::new(true),
            false,
            false,
        );
        let output = highlight_runs(vec![original], &highlights, true);
        assert_eq!(output.len(), count * 2);
        assert_eq!(output.iter().map(|r| r.len).sum::<usize>(), count * 2);
        assert_eq!(
            output
                .iter()
                .filter(|r| r.background_color.is_some())
                .count(),
            count
        );
    }
    #[test]
    fn unverified_encoded_and_explicit_web_ids_are_rejected_not_guessed() {
        let blocks = parse("# Hello World\n\n# h-title\n\n# 20target\n\n# Title\n\n# Title");
        for fragment in [
            "#Hello%20World",
            "#%20target",
            "#h-title",
            "#h-title-2",
            "#%E6%97%A5%E6%9C%AC%E8%AA%9E",
            "#bad%ZZ",
        ] {
            let local = LinkTarget::Markdown {
                destination: fragment.into(),
            };
            let wiki = LinkTarget::Wiki {
                key: "Note".into(),
                fragment: Some(fragment.into()),
                authored_label: false,
            };
            assert_eq!(
                fragment_anchor(&blocks, &local),
                Err(FragmentError::InvalidFragment),
                "{fragment}"
            );
            assert_eq!(
                fragment_anchor(&blocks, &wiki),
                Err(FragmentError::InvalidFragment),
                "{fragment}"
            );
        }
    }
    #[test]
    fn wiki_heading_fragments_use_first_level_agnostic_unicode_match() {
        let blocks = parse("## 日本語 Hello!\n\n# 日本語 Hello!\n\n### Other");
        for fragment in ["#日本語 Hello!", "##日本語 hello", "###日本語-hello"] {
            let target = LinkTarget::Wiki {
                key: "work:id:123".into(),
                fragment: Some(fragment.into()),
                authored_label: false,
            };
            assert_eq!(
                fragment_anchor(&blocks, &target),
                Ok(Some(outline(&blocks)[0].anchor.clone()))
            );
            assert_eq!(resolve_term(&target).as_deref(), Some("work:id:123"));
        }
    }
    #[test]
    fn missing_block_and_external_fragments_never_guess_an_anchor() {
        let blocks = parse("# Existing");
        let wiki = |fragment: &str| LinkTarget::Wiki {
            key: "Note".into(),
            fragment: Some(fragment.into()),
            authored_label: false,
        };
        assert_eq!(
            fragment_anchor(&blocks, &wiki("#absent")),
            Err(FragmentError::MissingHeading("absent".into()))
        );
        assert_eq!(
            fragment_anchor(&blocks, &wiki("#^block")),
            Err(FragmentError::MissingBlock("block".into()))
        );
        assert_eq!(fragment_anchor(&blocks, &wiki("#")), Ok(None));
        for destination in [
            "https://example.com/#Existing",
            "file:///tmp/note#Existing",
            "javascript:evil#Existing",
            "assets/a#Existing",
        ] {
            assert_eq!(
                fragment_anchor(
                    &blocks,
                    &LinkTarget::Markdown {
                        destination: destination.into()
                    }
                ),
                Err(FragmentError::UnsupportedDestination)
            );
        }
    }
    #[test]
    fn local_markdown_heading_target_never_becomes_a_resolve_term() {
        let blocks = parse("# Section");
        let target = LinkTarget::Markdown {
            destination: "#section".into(),
        };
        assert!(is_local_fragment(&target));
        assert_eq!(resolve_term(&target), None);
        assert_eq!(
            fragment_anchor(&blocks, &target),
            Ok(Some("section".into()))
        );
        let cross = LinkTarget::Markdown {
            destination: "Other.md#section".into(),
        };
        assert!(!is_local_fragment(&cross));
        assert_eq!(resolve_term(&cross).as_deref(), Some("Other"));
        assert_eq!(fragment_anchor(&blocks, &cross), Ok(Some("section".into())));
    }
    #[test]
    fn unresolved_and_unsafe_links_cannot_look_actionable() {
        let block =
            parse("[[Title]] [external](https://example.com) [unsafe](javascript:alert)").remove(0);
        let pending = resolved_spans(&block.spans, &block.links, None);
        assert!(pending.iter().filter(|span| span.style.unresolved).count() >= 2);
        assert!(pending.iter().all(|span| !span.style.link));
        let callbacks = LinkCallbacks {
            enlarge: None,
            plain_click: None,
            resolved: Rc::new(|_| true),
            activate: Rc::new(|_, _, _| {}),
        };
        let resolved = resolved_spans(&block.spans, &block.links, Some(&callbacks));
        assert!(resolved[0].style.link);
        assert!(resolved.iter().filter(|span| span.style.link).count() >= 2);
        assert!(resolved.last().unwrap().style.unresolved);
    }
    #[test]
    fn wiki_targets_labels_and_fragments_keep_resolver_authority() {
        for (source, display, key, fragment, authored) in [
            ("[[123]]", "123", "123", None, false),
            (
                "[[ id:123##Design | 設計 ]]",
                "設計",
                "id:123",
                Some("##Design"),
                true,
            ),
            (
                "[[work:id:123#^block]]",
                "work:id:123#^block",
                "work:id:123",
                Some("#^block"),
                false,
            ),
            ("[[日本語|別名|追加]]", "別名|追加", "日本語", None, true),
            ("[[Title| ]]", "Title", "Title", None, false),
            ("[[[test]]]", "[test]", "[test]", None, false),
            (
                "[[Plan [draft [v2]]]]",
                "Plan [draft [v2]]",
                "Plan [draft [v2]]",
                None,
                false,
            ),
            (
                "[[[test]|Example [draft]]]",
                "Example [draft]",
                "[test]",
                None,
                true,
            ),
        ] {
            let parsed = parse(source);
            assert_eq!(parsed[0].text, display, "{source}");
            assert_eq!(parsed[0].links.len(), 1, "{source}");
            assert_eq!(
                parsed[0].links[0].target,
                LinkTarget::Wiki {
                    key: key.into(),
                    fragment: fragment.map(str::to_owned),
                    authored_label: authored
                }
            );
            assert_eq!(
                resolve_term(&parsed[0].links[0].target).as_deref(),
                Some(key)
            );
        }
    }
    #[test]
    fn wiki_code_includes_and_invalid_syntax_are_inert() {
        for source in [
            "`[[Title]]`",
            "```\n[[Title]]\n```",
            "![[Title]]",
            "![[Title#Heading]] :only-contents",
            "[[ ]]",
            "[[Title",
            "[[Title\nnext]]",
            "[[unbalanced [title]]",
        ] {
            let parsed = parse(source);
            assert!(
                parsed.iter().all(|block| block.links.is_empty()),
                "{source}"
            );
        }
        assert_eq!(parse("![[Title]]")[0].text, "![[Title]]");
    }
    #[test]
    fn standard_links_preserve_label_and_restrict_resolution() {
        let parsed = parse("[日本語](Hello%20World.md?mode=x#heading)");
        assert_eq!(parsed[0].text, "日本語");
        assert_eq!(
            resolve_term(&parsed[0].links[0].target).as_deref(),
            Some("Hello World")
        );
        for destination in [
            "https://example.com",
            "file:///tmp/secret",
            "javascript:evil()",
            "data:text/plain,test",
            "command:run",
            "#heading",
            "assets/image.png",
            "/apps/demo/",
            "../secret",
            "javascript%3Aevil",
        ] {
            assert!(
                resolve_term(&LinkTarget::Markdown {
                    destination: destination.into()
                })
                .is_none(),
                "{destination}"
            );
        }
    }
    #[test]
    fn outline_anchors_are_unique_unicode_and_non_heading_stable() {
        let blocks =
            parse("# 日本語 Hello!\n\nparagraph\n\n## Hello\n\n## Hello\n\n## Hello-2\n\n# 🦀");
        let entries = outline(&blocks);
        assert_eq!(
            entries
                .iter()
                .map(|e| e.anchor.as_str())
                .collect::<Vec<_>>(),
            vec!["日本語-hello", "hello", "hello-2", "hello-2-2", "section"]
        );
        assert_eq!(entries[1].block_index, 2);
        assert_eq!(entries[1].level, 2);
        let mut changed = blocks.clone();
        changed.insert(0, parse("extra paragraph").remove(0));
        assert_eq!(
            outline(&changed)
                .iter()
                .map(|e| &e.anchor)
                .collect::<Vec<_>>(),
            entries.iter().map(|e| &e.anchor).collect::<Vec<_>>()
        );
        assert_eq!(outline(&changed)[1].block_index, 3);
    }
    #[test]
    fn outline_navigation_validates_current_document_anchor() {
        let blocks = parse("intro\n\n# Target");
        let handle = ScrollHandle::new();
        assert!(scroll_to_heading(&blocks, "target", &handle, 2));
        assert!(!scroll_to_heading(&blocks, "missing", &handle, 2));
        assert!(!scroll_to_heading(&blocks, "target", &handle, usize::MAX));
        assert!(outline(&parse("```\n# not a heading\n```")).is_empty());
    }
    #[test]
    fn themed_runs_use_neutral_code_and_ink_links() {
        for dark in [false, true] {
            let theme = ReaderTheme::new(dark);
            let code = text_run(
                4,
                InlineStyle {
                    code: true,
                    ..Default::default()
                },
                theme,
                false,
                false,
            );
            assert_eq!(code.font.family.as_ref(), "Menlo");
            assert_eq!(code.color, rgb(theme.muted).into());
            assert!(code.background_color.is_none());
            let link = text_run(
                4,
                InlineStyle {
                    link: true,
                    ..Default::default()
                },
                theme,
                false,
                false,
            );
            assert_eq!(link.color, rgb(theme.ink).into());
            assert_eq!(
                link.underline.unwrap().color,
                Some(rgb(theme.strong_line).into())
            );
            assert_eq!(
                text_run(4, InlineStyle::default(), theme, false, true)
                    .font
                    .weight,
                FontWeight::BOLD
            );
        }
    }
    #[test]
    fn inline_styles_are_composed_on_utf8_byte_ranges() {
        let parsed = parse("- 日本語 **太い *斜体🦀*** と `code` [**link**](https://example.com)");
        let block = &parsed[0];
        assert_eq!(block.text, "日本語 太い 斜体🦀 と code link");
        let styled = |text: &str| {
            block
                .spans
                .iter()
                .find(|span| &block.text[span.range.clone()] == text)
                .unwrap()
                .style
        };
        assert!(styled("太い ").strong);
        assert!(styled("斜体🦀").strong && styled("斜体🦀").emphasis);
        assert!(styled("code").code);
        assert!(styled("link").link && styled("link").strong);
        assert!(
            block
                .spans
                .windows(2)
                .all(|pair| pair[0].range.end <= pair[1].range.start)
        );
    }
    #[test]
    fn tables_preserve_cells_styles_alignment_and_source() {
        let source = "| Left | Center | Right |\n|:---|:---:|---:|\n| 日本語 | **bold** | `a` |\n| x | | [link](https://example.com) |";
        let parsed = parse(source);
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0].kind, BlockKind::Table);
        assert_eq!(parsed[0].text, source);
        let table = parsed[0].table.as_ref().unwrap();
        assert_eq!(
            table.alignments,
            vec![Alignment::Left, Alignment::Center, Alignment::Right]
        );
        assert_eq!(table.rows.len(), 3);
        assert_eq!(table.rows[1][0].text, "日本語");
        assert!(table.rows[1][1].spans[0].style.strong);
        assert!(table.rows[1][2].spans[0].style.code);
        assert!(table.rows[2][1].text.is_empty());
        assert_eq!(table.rows[2][2].text, "link");
    }
    #[test]
    fn excess_table_cells_fall_back_without_data_loss() {
        let source = "| A | B |\n|---|---|\n| one | two | must not disappear |";
        let parsed = parse(source);
        assert_eq!(parsed[0].kind, BlockKind::Literal);
        assert_eq!(parsed[0].text, source);
    }
    #[test]
    fn code_info_does_not_change_exact_parser_body_or_find_ranges() {
        let blocks = parse(
            "```lua extra-info\n\tprint(\"日本語 🦀\")  \n```\n\n```unknownlang\nsecond\n```\n\n```\nplain\n```\n\n    indented\n\ninline `code`",
        );
        assert_eq!(blocks[0].language.as_deref(), Some("lua"));
        assert_eq!(blocks[0].text, "\tprint(\"日本語 🦀\")  \n");
        assert_eq!(blocks[1].language.as_deref(), Some("unknownlang"));
        assert!(blocks[2..].iter().all(|block| block.language.is_none()));
        assert!(crate::reader_find::search(&blocks, "lua").hits.is_empty());
        let found = crate::reader_find::search(&blocks, "日本語");
        assert_eq!(found.hits.len(), 1);
        assert_eq!(found.hits[0].target, Target::Block(0));
        assert_eq!(&blocks[0].text[found.hits[0].range.clone()], "日本語");
    }
    #[test]
    fn code_copy_uses_parser_newlines_and_excludes_container_markers() {
        let quoted = parse("> ```lua\n> a  \n> ```");
        assert_eq!(quoted[0].text, "a  \n");
        assert!(quoted[0].quoted);
        let listed = parse("- ```lua\n  a\n    b\n  ```");
        assert_eq!(listed[0].text, "a\n  b\n");
        assert!(listed[0].marker.is_some());
        assert_eq!(parse("```lua\nlast")[0].text, "last");
        assert_eq!(parse("```lua\nlast\n```")[0].text, "last\n");
        assert_eq!(parse("```lua\r\nline\r\n```")[0].text, "line\n");
        assert_eq!(parse("```lua\n```")[0].text, "");
    }
    #[test]
    fn rich_fences_remain_plain_inert_body_with_no_links_or_styles() {
        let blocks = parse(
            "```mermaid\ngraph TD; A-->B\n```\n\n```html\n<script>bad()</script> [[note]]\n```",
        );
        assert!(blocks.iter().all(|block| block.kind == BlockKind::Code
            && block.links.is_empty()
            && block.spans.is_empty()));
        assert_eq!(blocks[1].text, "<script>bad()</script> [[note]]\n");
    }
    #[test]
    fn malformed_table_is_visible_and_code_is_not_styled() {
        assert_eq!(
            parse("| a | b |\nnot a delimiter")[0].text,
            "| a | b | not a delimiter"
        );
        let parsed = parse("```\n**plain** `plain`\n```");
        assert!(parsed[0].spans.is_empty());
        assert_eq!(parsed[0].text, "**plain** `plain`\n");
    }
    #[test]
    fn headings_and_unicode() {
        let parsed = parse("# 日本語 🦀\n\n###### Six\n\n第一行\n第二行");
        assert_eq!(parsed[0].kind, BlockKind::Heading(1));
        assert_eq!(parsed[0].text, "日本語 🦀");
        assert_eq!(parsed[1].kind, BlockKind::Heading(6));
        assert_eq!(parsed[2].text, "第一行 第二行");
    }
    #[test]
    fn source_softbreaks_flow_as_spaces_but_explicit_breaks_remain() {
        let blocks =
            parse("Connect them with\nexplicit links.\n\n**first\nsecond**  \nthird\\\nfourth");
        assert_eq!(blocks[0].text, "Connect them with explicit links.");
        assert_eq!(blocks[1].text, "first second\nthird\nfourth");
        assert!(blocks[1].spans.iter().all(|span| span.style.strong));
        assert_eq!(parse("```\nfirst\nsecond\n```")[0].text, "first\nsecond\n");
    }
    #[test]
    fn all_heading_levels_and_hard_breaks() {
        for level in 1..=6 {
            assert_eq!(
                parse(&format!("{} title", "#".repeat(level)))[0].kind,
                BlockKind::Heading(level as u8)
            );
        }
        assert_eq!(parse("one  \ntwo\\\nthree")[0].text, "one\ntwo\nthree");
    }
    #[test]
    fn nested_lists_keep_text_and_order() {
        let parsed = parse("- first\n  - child\n\n- second\n\n  continuation\n");
        assert_eq!(
            parsed.iter().map(|b| b.text.as_str()).collect::<Vec<_>>(),
            vec!["first", "child", "second", "continuation"]
        );
        assert_eq!(parsed[1].indent, 2);
    }
    #[test]
    fn code_is_inert_and_retains_lines() {
        let parsed =
            parse("```html\n<script>alert(1)</script>\n# not heading\n```\n\n    indented\n");
        assert_eq!(parsed[0].kind, BlockKind::Code);
        assert_eq!(parsed[0].text, "<script>alert(1)</script>\n# not heading\n");
        assert_eq!(parsed[1].kind, BlockKind::Code);
        assert_eq!(parsed[1].text, "indented\n");
    }
    #[test]
    fn links_have_labels_and_destinations() {
        assert_eq!(parse("[日本語](https://example.com/a)")[0].text, "日本語");
    }
    #[test]
    fn unsupported_content_remains_visible() {
        for source in [
            "![alt](https://example.com/image.png)",
            "<script>evil()</script>",
            "$$x^2$$",
            "```mermaid\ngraph TD; A-->B\n```",
            "[unused]: https://example.com",
        ] {
            let parsed = parse(source);
            assert!(!parsed.is_empty(), "{source}");
            let visible = parsed
                .iter()
                .map(|b| b.text.as_str())
                .collect::<Vec<_>>()
                .join("\n");
            assert!(
                visible.contains(if source.starts_with("```mermaid") {
                    "graph TD; A-->B"
                } else {
                    source
                }),
                "{visible}"
            );
        }
    }
    #[test]
    fn lists_quotes_and_empty_input() {
        let parsed = parse("3. first\n4. second\n\n> quoted\n> next\n");
        assert_eq!(parsed[0].marker.as_deref(), Some("3."));
        assert_eq!(parsed[1].marker.as_deref(), Some("4."));
        assert!(parsed[2].quoted);
        assert_eq!(parsed[2].text, "quoted next");
        assert!(parse("").is_empty());
    }
}

#[cfg(test)]
mod exact_find_geometry_tests {
    use super::*;
    #[test]
    fn unicode_list_code_and_table_offsets_remain_original_utf8_bytes() {
        let blocks = parse(
            "- 日本語 🦀 target\n\n```rust\nlet 日本語 = \"🦀 target\";\n```\n\n| Header |\n| --- |\n| 日本語 🦀 target |\n",
        );
        let hits = crate::reader_find::search(&blocks, "🦀 target");
        assert_eq!(hits.hits.len(), 3);
        for hit in hits.hits {
            let (text, prefix) = match hit.target {
                Target::Block(i) => (
                    &blocks[i].text,
                    blocks[i].marker.as_ref().map_or(0, |m| m.len() + 1),
                ),
                Target::Cell { block, row, column } => (
                    &blocks[block].table.as_ref().unwrap().rows[row][column].text,
                    0,
                ),
            };
            assert_eq!(&text[hit.range.clone()], "🦀 target");
            assert_eq!(
                match_start(text, &hit.range, prefix),
                Some(text.find('🦀').unwrap() + prefix)
            );
        }
        assert_eq!(match_start("🦀", &(1..4), 0), None);
        assert_eq!(match_start("🦀", &(0..5), 0), None);
        assert_eq!(match_start("🦀", &(0..0), 0), None);
        assert_eq!(match_start("x🦀", &(1..5), usize::MAX), None);
    }
    #[test]
    fn measured_wrapped_line_reveal_preserves_visible_position_and_clamps() {
        // Sample measured pixel coordinates; no assumed character width or wrap count.
        assert_eq!(
            reveal_line_offset(-200., 100., 300., 1000., 23., 2000.),
            Some(-623.)
        );
        assert_eq!(
            reveal_line_offset(-700., 100., 300., 1000., 23., 2000.),
            Some(-700.)
        );
        assert_eq!(
            reveal_line_offset(-1000., 100., 300., 1000., 23., 2000.),
            Some(-900.)
        );
        assert_eq!(
            reveal_line_offset(0., 100., 300., 1000., 23., 500.),
            Some(-500.)
        );
        assert_eq!(
            reveal_line_offset(-700., 100., 10., 1000., 23., 2000.),
            Some(-900.)
        );
        assert_eq!(reveal_line_offset(0., 0., 0., 100., 23., 100.), None);
        assert_eq!(reveal_line_offset(0., 0., 100., f32::NAN, 23., 100.), None);
    }
    #[test]
    fn new_render_invalidates_geometry_and_rejects_old_prepaint_and_hit() {
        let anchors = FindAnchors::new(&parse("first second"), &ScrollHandle::new());
        let first = Hit {
            target: Target::Block(0),
            range: 0..5,
        };
        let second = Hit {
            target: Target::Block(0),
            range: 6..12,
        };
        let first_generation = anchors.begin_layout(Some(first.clone()));
        // No prepaint: no geometry method is called and no scroll can happen.
        assert!(!anchors.scroll_to_hit(&first, gpui::Size::default()));
        let second_generation = anchors.begin_layout(Some(second.clone()));
        let line = FindLine {
            top: 500.,
            height: 23.,
            reader_bounds: gpui::Bounds::default(),
            viewport: gpui::Size::default(),
        };
        anchors.layout.borrow_mut().publish(first_generation, line);
        assert!(anchors.layout.borrow().line.is_none());
        anchors.layout.borrow_mut().publish(second_generation, line);
        assert!(!anchors.scroll_to_hit(&first, gpui::Size::default()));
        anchors.begin_layout(Some(second));
        assert!(anchors.layout.borrow().line.is_none());
    }
}

/// Only standalone image paragraphs, never inline/table/code/HTML references.
/// The original source text remains the searchable caption and fallback.
pub fn standalone_images(blocks: &[Block]) -> Vec<(usize, String)> {
    blocks
        .iter()
        .enumerate()
        .filter_map(|(index, block)| {
            let reference = block.image.as_ref()?;
            crate::raster::asset_name(reference).ok()?;
            Some((index, reference.clone()))
        })
        .take(4)
        .collect()
}

#[cfg(test)]
mod raster_reference_tests {
    use super::*;
    #[test]
    fn standalone_rasters_keep_original_text_and_ignore_inline_tables_and_code() {
        let source = "![日本語](assets/a.png)\n\nprefix ![inline](assets/b.jpg)\n\n```md\n![code](assets/c.png)\n```\n\n![remote](https://example.com/x.png)";
        let blocks = parse(source);
        let refs = standalone_images(&blocks);
        assert_eq!(refs.len(), 1);
        assert_eq!(refs[0].1, "assets/a.png");
        assert_eq!(blocks[refs[0].0].text, "![日本語](assets/a.png)");
        assert_eq!(crate::reader_find::search(&blocks, "日本語").hits.len(), 1);
        assert!(standalone_images(&parse("| Image |\n|---|\n| ![x](assets/a.png) |")).is_empty());
        assert!(standalone_images(&parse("![x](assets/a.png) ![y](assets/b.png)")).is_empty());
        assert!(standalone_images(&parse(r"\![escaped](assets/a.png)")).is_empty());
        assert!(standalone_images(&parse("`![code](assets/a.png)`")).is_empty());
    }
    #[test]
    fn current_reader_raster_requests_are_capped() {
        let source = (0..10)
            .map(|n| format!("![x](assets/{n}.jpg)\n\n"))
            .collect::<String>();
        assert_eq!(standalone_images(&parse(&source)).len(), 4);
    }
}

#[cfg(test)]
mod thematic_rule_tests {
    use super::*;

    #[test]
    fn thematic_rules_are_parser_blocks_with_no_invisible_find_text() {
        for source in ["---", "***", "___", "* * *", "- - -"] {
            let parsed = parse(source);
            assert_eq!(parsed.len(), 1, "{source}");
            assert_eq!(parsed[0].kind, BlockKind::Rule);
            assert!(parsed[0].text.is_empty());
            assert!(parsed[0].language.is_none());
            assert!(parsed[0].links.is_empty());
            assert!(crate::reader_find::search(&parsed, source).hits.is_empty());
        }
        let parsed = parse("Before\n\n---\n\n# After");
        assert_eq!(parsed.len(), 3);
        assert_eq!(outline(&parsed)[0].block_index, 2);
        assert_eq!(
            crate::reader_find::search(&parsed, "After").hits[0].target,
            Target::Block(2)
        );
    }

    #[test]
    fn thematic_rules_preserve_quote_and_list_structure() {
        let parsed = parse("> before\n>\n> ---\n>\n> after");
        assert_eq!(parsed[1].kind, BlockKind::Rule);
        assert!(parsed[1].quoted);
        let parsed = parse("- before\n\n  ***\n\n  after");
        assert_eq!(parsed.len(), 3);
        assert_eq!(parsed[1].kind, BlockKind::Rule);
        assert_eq!(parsed[1].indent, 1);
        assert!(parsed[1].marker.is_none());
        assert_eq!(parsed[2].text, "after");
        assert_eq!(parsed[2].indent, 1);
        let first = parse("- ***");
        assert_eq!(first[0].kind, BlockKind::Rule);
        assert_eq!(first[0].marker.as_deref(), Some("•"));
        assert_eq!(first[0].indent, 1);
    }

    #[test]
    fn thematic_rules_do_not_reinterpret_other_markdown_constructs() {
        let heading = parse("Title\n---");
        assert_eq!(heading[0].kind, BlockKind::Heading(2));
        for source in [
            "\\*\\*\\*",
            "`---`",
            "```text\n---\n```",
            "    ---",
            "<div>\n---\n</div>",
            "| a |\n| --- |\n| b |",
        ] {
            let parsed = parse(source);
            assert!(parsed.iter().all(|b| b.kind != BlockKind::Rule), "{source}");
        }
        let code = parse("```text\n---\n```");
        assert_eq!(code[0].kind, BlockKind::Code);
        assert_eq!(code[0].text, "---\n");
        assert_eq!(crate::reader_find::search(&code, "---").hits.len(), 1);
    }
}

#[cfg(test)]
mod footnote_display_tests {
    use super::*;
    #[test]
    fn footnote_definitions_keep_formatted_children_and_source_position() {
        let parsed = parse("Before[^日本語].\n\n[^日本語]: **Detail** and [note](42.md).\n\nAfter");
        assert_eq!(
            parsed.iter().map(|b| b.text.as_str()).collect::<Vec<_>>(),
            vec![
                "Before[^日本語].",
                "[^日本語]:",
                "Detail and note.",
                "After"
            ]
        );
        assert_eq!(parsed[2].indent, 1);
        assert!(parsed[2].spans.iter().any(|s| s.style.strong));
        assert_eq!(parsed[2].links.len(), 1);
        assert!(matches!(
            &parsed[0].links[0].target,
            LinkTarget::Footnote { .. }
        ));
        assert_eq!(
            crate::reader_find::search(&parsed, "Detail").hits[0].target,
            Target::Block(2)
        );
        assert_eq!(crate::reader_find::search(&parsed, "日本語").hits.len(), 2);
    }
    #[test]
    fn footnote_missing_duplicate_unused_and_multiblock_content_is_visible() {
        let source = "Missing[^none].\n\n[^same]: first\n\n[^same]: second\n\n[^unused]: unused\n\n    next paragraph\n\n    - nested item";
        let parsed = parse(source);
        let text = parsed
            .iter()
            .map(|b| b.text.as_str())
            .collect::<Vec<_>>()
            .join("\n");
        for expected in [
            "Missing[^none].",
            "first",
            "second",
            "unused",
            "next paragraph",
            "nested item",
        ] {
            assert!(text.contains(expected), "{expected}: {text}");
        }
        assert_eq!(text.matches("[^same]:").count(), 2);
    }
    #[test]
    fn footnote_nested_blocks_and_repeated_table_references_keep_indices() {
        let parsed = parse(
            "Ref[^x] again[^x].\n\n| Ref |\n| --- |\n| [^x] |\n\n[^x]: Intro\n\n    > quote\n\n    ```lua\n    print(1)\n    ```\n\n    | Col |\n    | --- |\n    | value |\n\n# Tail",
        );
        assert_eq!(parsed[0].text.matches("[^x]").count(), 2);
        assert_eq!(parsed[1].table.as_ref().unwrap().rows[1][0].text, "[^x]");
        assert!(
            parsed
                .iter()
                .any(|b| b.quoted && b.text == "quote" && b.indent == 1)
        );
        let code = parsed.iter().find(|b| b.kind == BlockKind::Code).unwrap();
        assert_eq!(code.text, "print(1)\n");
        assert_eq!(code.language.as_deref(), Some("lua"));
        assert!(
            parsed
                .iter()
                .any(|b| b.kind == BlockKind::Table && b.indent == 1)
        );
        let tail = parsed.len() - 1;
        assert_eq!(outline(&parsed)[0].block_index, tail);
        assert_eq!(
            crate::reader_find::search(&parsed, "Tail").hits[0].target,
            Target::Block(tail)
        );
        assert_eq!(crate::reader_find::search(&parsed, "[^x]").hits.len(), 4);
    }
    #[test]
    fn footnote_parser_does_not_promote_code_or_escaped_markers() {
        let parsed = parse("```md\n[^a]: code\n```\n\n\\[^b]: escaped\n\n> [^c]: quoted");
        assert_eq!(parsed[0].kind, BlockKind::Code);
        assert_eq!(parsed[0].text, "[^a]: code\n");
        assert!(
            parsed
                .iter()
                .any(|b| b.text == "[^b]: escaped" && b.kind == BlockKind::Paragraph)
        );
        assert!(parsed.iter().any(|b| b.text == "[^c]:" && b.quoted));
        assert!(
            parsed
                .iter()
                .any(|b| b.text == "quoted" && b.quoted && b.indent == 1)
        );
    }
}

#[cfg(test)]
mod footnote_navigation_tests {
    use super::*;
    #[test]
    fn footnote_targets_are_parser_only_unique_and_follow_block_movement() {
        let parsed = parse("Ref[^x] and [^x].\n\n[^x]: body");
        let case = parse("Ref[^X].\n\n[^x]: case");
        assert!(footnote_link_valid(&case, &case[0].links[0]));
        for source in [
            "Ref[^STRASSE].\n\n[^straße]: body",
            "Ref[^x  y].\n\n[^x y]: body",
        ] {
            let normalized = parse(source);
            assert!(footnote_link_valid(&normalized, &normalized[0].links[0]));
        }
        for source in [
            "Ref[^X].\n\n[^x]: one\n\n[^X]: two",
            "Ref[^straße].\n\n[^STRASSE]: one\n\n[^straße]: two",
        ] {
            assert!(footnote_targets(&parse(source)).is_empty());
        }
        let targets = footnote_targets(&parsed);
        assert_eq!(targets.get("x"), Some(&1));
        let link = &parsed[0].links[0];
        assert!(footnote_link_valid(&parsed, link));
        assert!(resolve_term(&link.target).is_none());
        assert!(!is_local_fragment(&link.target));
        assert!(crate::external_links::destination(&link.target).is_none());
        let shifted = parse("intro\n\nRef[^x].\n\n[^x]: body");
        assert_eq!(footnote_targets(&shifted).get("x"), Some(&2));
        let duplicated = parse("Ref[^x].\n\n[^x]: one\n\n[^x]: two");
        assert!(footnote_targets(&duplicated).is_empty());
        assert!(!footnote_link_valid(&duplicated, &duplicated[0].links[0]));
        assert!(footnote_targets(&parse("`[^x]: not definition`")).is_empty());
        assert!(!footnote_link_valid(&parse("missing[^x]"), link));
        let mut forged = link.clone();
        forged.range = 999..1000;
        assert!(!footnote_link_valid(&parsed, &forged));
    }
    #[test]
    fn footnote_table_links_and_visible_styles_require_explicit_resolution() {
        let parsed = parse("| Ref |\n| --- |\n| [^x] |\n\n[^x]: body");
        let cell = &parsed[0].table.as_ref().unwrap().rows[1][0];
        assert!(footnote_link_valid(&parsed, &cell.links[0]));
        let callbacks = LinkCallbacks {
            enlarge: None,
            plain_click: None,
            resolved: Rc::new(|t| matches!(t, LinkTarget::Footnote { .. })),
            activate: Rc::new(|_, _, _| {}),
        };
        assert!(
            resolved_spans(&cell.spans, &cell.links, Some(&callbacks))
                .iter()
                .any(|s| s.style.link)
        );
        assert!(
            resolved_spans(&cell.spans, &cell.links, None)
                .iter()
                .all(|s| !s.style.link)
        );
        let anchors = HeadingAnchors::new(&parsed, &ScrollHandle::new());
        assert!(anchors.targets.is_empty());
        assert_eq!(anchors.footnotes["x"].0, 1);
    }
}

#[cfg(test)]
mod position_tests {
    use super::*;
    #[test]
    fn editor_progress_normalizes_lines_without_source_mapping() {
        assert_eq!(follow_scroll_ratio(1, 11), 0.);
        assert_eq!(follow_scroll_ratio(11, 11), 1.);
        assert_eq!(follow_scroll_ratio(6, 11), 0.5);
        assert_eq!(follow_scroll_ratio(1, 1), 0.);
        assert_eq!(follow_scroll_ratio(0, 0), 0.);
        assert_eq!(follow_scroll_ratio(12, 3), 1.);
        assert_eq!(follow_scroll_ratio(1_000_000_000, 1_000_000_000), 1.);
    }
    #[test]
    fn editor_progress_uses_negative_full_scroll_extent_without_guessing() {
        assert_eq!(proportional_scroll_offset(0., Some(600.)), Some(0.));
        assert_eq!(proportional_scroll_offset(1., Some(600.)), Some(-600.));
        assert_eq!(proportional_scroll_offset(0.5, Some(600.)), Some(-300.));
        assert_eq!(proportional_scroll_offset(1., Some(0.)), Some(0.));
        assert_eq!(proportional_scroll_offset(1., Some(-10.)), Some(0.));
        assert_eq!(
            proportional_scroll_offset(0.5, Some(10_000_000.)),
            Some(-5_000_000.)
        );
        assert_eq!(
            proportional_scroll_offset(1., Some(f32::MAX)),
            Some(-f32::MAX)
        );
        assert_eq!(proportional_scroll_offset(0.5, None), None);
        for value in [f32::NAN, f32::INFINITY, f32::NEG_INFINITY] {
            assert_eq!(proportional_scroll_offset(0.5, Some(value)), None);
        }
        for ratio in [f64::NAN, f64::INFINITY, f64::NEG_INFINITY, -0.1, 1.1] {
            assert_eq!(proportional_scroll_offset(ratio, Some(100.)), None);
        }
    }
    #[test]
    fn editor_progress_rejects_missing_layout() {
        let handle = ScrollHandle::new();
        let anchors = PositionAnchors::new(1, &handle).unwrap();
        assert!(
            anchors
                .capture(gpui::size(px(800.), px(600.)), 1.)
                .is_none()
        );
        assert!(!anchors.request_after_layout(|_, _, _| {}));
    }
    #[test]
    fn measured_semantic_blocks_not_lines_or_percentages() {
        assert_eq!(
            first_visible_position(&[(0., 20.), (25., 900.), (930., 20.)], 100., 300.),
            Some(1)
        );
        assert_eq!(first_visible_position(&[(100., 20.)], 0., 50.), None);
        assert_eq!(
            first_visible_position(&[(0., 20.), (20., 20.)], 20., 20.),
            Some(1)
        );
        assert_eq!(
            first_visible_position(&[(0., 30.), (20., 20.)], 20., 20.),
            None
        );
        assert_eq!(
            first_visible_position(&[(0., 0.), (20., 20.)], 20., 20.),
            None
        );
        assert_eq!(first_visible_position(&[(0., f32::NAN)], 0., 20.), None);
        assert_eq!(
            first_visible_position(&vec![(0., 20.); 4097], 0., 20.),
            None
        );
    }
    #[test]
    fn capture_rejects_stale_layout_and_exact_geometry() {
        let reader = gpui::Bounds::new(
            gpui::point(px(10.), px(20.)),
            gpui::size(px(600.), px(400.)),
        );
        let viewport = gpui::size(px(800.), px(600.));
        let offset = gpui::point(px(0.), px(-10.));
        let mut layout = PositionLayout {
            generation: 3,
            measured: Some(PositionMeasurement {
                blocks: vec![gpui::Bounds::new(
                    gpui::point(px(10.), px(0.)),
                    gpui::size(px(600.), px(900.)),
                )],
                reader,
                viewport,
                scale: 2.,
                offset,
                max_offset: gpui::size(px(0.), px(500.)),
            }),
            refreshed: None,
            pending: None,
        };
        let captured = layout.capture(reader, offset, viewport, 2.).unwrap();
        assert_eq!(captured.block_index, 0);
        assert_eq!(captured.generation, 3);
        assert!(layout.capture(reader, offset, viewport, 1.).is_none());
        assert!(
            layout
                .capture(reader, offset, gpui::size(px(801.), px(600.)), 2.)
                .is_none()
        );
        assert!(
            layout
                .capture(reader, gpui::point(px(0.), px(-11.)), viewport, 2.)
                .is_none()
        );
        let mut changed = reader;
        changed.size.width += px(1.);
        assert!(layout.capture(changed, offset, viewport, 2.).is_none());
        layout.generation += 1;
        assert_ne!(
            layout.capture(reader, offset, viewport, 2.).as_ref(),
            Some(&captured)
        );
        layout.measured = None;
        assert!(layout.capture(reader, offset, viewport, 2.).is_none());
    }
    #[test]
    fn queued_action_requires_unchanged_complete_prepaint_and_is_consumed_once() {
        let handle = ScrollHandle::new();
        let anchors = PositionAnchors::new(1, &handle).unwrap();
        assert!(!anchors.request_after_layout(|_, _, _| {}));
        let measurement = PositionMeasurement {
            blocks: vec![gpui::Bounds::new(
                gpui::point(px(0.), px(10.)),
                gpui::size(px(600.), px(900.)),
            )],
            reader: gpui::Bounds::new(gpui::point(px(0.), px(0.)), gpui::size(px(600.), px(400.))),
            viewport: gpui::size(px(800.), px(600.)),
            scale: 1.,
            offset: gpui::point(px(0.), px(0.)),
            max_offset: gpui::size(px(0.), px(500.)),
        };
        anchors.layout.borrow_mut().measured = Some(measurement.clone());
        assert!(anchors.request_after_layout(|_, _, _| {}));
        // A new render generation alone is not a changed measurement.
        anchors.layout.borrow_mut().generation += 1;
        assert!(anchors.layout.borrow_mut().consume_pending().unwrap().0);
        assert!(anchors.layout.borrow_mut().consume_pending().is_none());
        assert!(anchors.request_after_layout(|_, _, _| {}));
        // An asynchronously decoded image/preamble shifts the host block.
        anchors
            .layout
            .borrow_mut()
            .measured
            .as_mut()
            .unwrap()
            .blocks[0]
            .origin
            .y += px(30.);
        assert!(!anchors.layout.borrow_mut().consume_pending().unwrap().0);
        assert!(anchors.layout.borrow_mut().consume_pending().is_none());
        // Even when visible block bounds stay unchanged, a full reader extent
        // change (for example trailing content) invalidates the queued action.
        assert!(anchors.request_after_layout(|_, _, _| {}));
        anchors
            .layout
            .borrow_mut()
            .measured
            .as_mut()
            .unwrap()
            .max_offset
            .height += px(20.);
        assert!(!anchors.layout.borrow_mut().consume_pending().unwrap().0);
        assert!(anchors.request_after_layout(|_, _, _| {}));
        anchors.cancel_after_layout();
        assert!(anchors.layout.borrow_mut().consume_pending().is_none());
        // Source replacement drops a queued callback, rather than retaining a
        // renderer/callback cycle that could resurrect it on later navigation.
        let anchors = Rc::new(anchors);
        let weak = Rc::downgrade(&anchors);
        let callback_weak = weak.clone();
        let invoked = Rc::new(std::cell::Cell::new(false));
        let callback_invoked = invoked.clone();
        assert!(anchors.request_after_layout(move |_, _, _| {
            callback_invoked.set(true);
            let _ = callback_weak.upgrade();
        }));
        assert_eq!(Rc::strong_count(&anchors), 1);
        drop(anchors);
        assert!(weak.upgrade().is_none());
        assert!(!invoked.get());
    }
    #[test]
    fn no_layout_no_capture_and_bounded_document() {
        let handle = ScrollHandle::new();
        assert!(PositionAnchors::new(0, &handle).is_none());
        assert!(PositionAnchors::new(4097, &handle).is_none());
        let positions = PositionAnchors::new(1, &handle).unwrap();
        assert!(
            positions
                .capture(gpui::size(px(800.), px(600.)), 1.)
                .is_none()
        );
        positions.track(div(), 1);
        assert_eq!(positions.current_generation(), 1);
        assert!(
            positions
                .capture(gpui::size(px(800.), px(600.)), 1.)
                .is_none()
        );
    }
}

#[cfg(test)]
mod include_preview_tests {
    use super::*;
    use crate::included_notes;
    use serde_json::json;
    fn response(body: &str, lines: &[usize], excerpt: &str) -> included_notes::Response {
        included_notes::decode(json!({"markdown":body,"includes":lines.iter().map(|line| json!({"line":line,"source_line":0,"note_id":42,"kind":"note","etag":"revision","caption":"Caption","lines":[excerpt]})).collect::<Vec<_>>()}), "host").unwrap()
    }
    fn text(preview: &IncludePreview) -> String {
        preview
            .items
            .iter()
            .flat_map(|item| match item {
                PreviewItem::Host(b) => b,
                PreviewItem::Include { blocks, .. } => blocks,
            })
            .map(|b| b.text.as_str())
            .collect::<Vec<_>>()
            .join("\n")
    }
    #[test]
    fn returned_markdown_positions_preserve_host_and_reference_context() {
        let raw = "raw body with no include";
        let response = response(
            "Before [reference][r]\n\n![[Target]]\n\nAfter\n\n[r]: https://example.com",
            &[2],
            "## Included\n\nBody",
        );
        let preview = included_preview(&response).unwrap();
        assert_eq!(raw, "raw body with no include");
        assert!(text(&preview).contains("Before reference\nIncluded\nBody\nAfter"));
        assert!(
            matches!(&preview.items[1], PreviewItem::Include{index:0,directive,..} if directive == "![[Target]]\n")
        );
        let PreviewItem::Host(host) = &preview.items[0] else {
            panic!()
        };
        assert!(
            matches!(&host[0].links[0].target, LinkTarget::Markdown{destination} if destination == "https://example.com")
        );
        assert!(preview.unsupported.is_empty());
    }
    #[test]
    fn unsupported_locations_keep_native_host_exactly() {
        for (body, lines) in [
            ("- ![[Target]]", vec![0]),
            ("> ![[Target]]", vec![0]),
            ("a | b\n-- | --\n![[Target]] | c", vec![2]),
            ("Before\n![[Target]]\nAfter", vec![1]),
            ("![[One]]\n![[Two]]", vec![0, 1]),
            ("```\n![[Target]]\n```", vec![1]),
            ("\\![[Target]]", vec![0]),
            ("![[One]] ![[Two]]", vec![0]),
            ("ordinary paragraph", vec![0]),
        ] {
            let preview = included_preview(&response(body, &lines, "Should not appear")).unwrap();
            assert_eq!(preview.unsupported.len(), lines.len(), "{body}");
            assert_eq!(preview.items.len(), 1, "{body}");
            let PreviewItem::Host(host) = &preview.items[0] else {
                panic!("{body}")
            };
            assert_eq!(*host, parse(body), "{body}");
        }
    }
    #[test]
    fn crlf_positions_are_exact_and_whitespace_is_conservative() {
        let response = response("Before\r\n\r\n![[Target]]\r\n\r\nAfter", &[2], "excerpt");
        let preview = included_preview(&response).unwrap();
        assert!(preview.unsupported.is_empty());
        assert!(
            matches!(&preview.items[1], PreviewItem::Include{directive,..} if directive == "![[Target]]\r\n")
        );
        for body in [" ![[Target]]", "![[Target]]  ", "![[Target]] suffix"] {
            let mut response = super::include_preview_tests::response(body, &[0], "excerpt");
            response.entries[0].error = Some("opaque failure".into());
            response.entries[0].bad_options = vec!["bad=1".into()];
            let preview = included_preview(&response).unwrap();
            assert_eq!(preview.unsupported.len(), 1, "{body}");
            assert!(preview.unsupported[0].contains("opaque failure"));
            assert!(preview.unsupported[0].contains("bad=1"));
            let PreviewItem::Host(host) = &preview.items[0] else {
                panic!()
            };
            assert_eq!(*host, parse(body));
        }
        // Mapping to a blank line cannot insert into a neighboring paragraph.
        let preview = included_preview(&super::include_preview_tests::response(
            "Before\n\nAfter",
            &[1],
            "excerpt",
        ))
        .unwrap();
        assert_eq!(preview.unsupported.len(), 1);
        assert_eq!(text(&preview), "Before\nAfter");
    }
    #[test]
    fn namespaces_and_origins_remain_separate_and_nested_content_inert() {
        let mut response = response(
            "# Same\n\n![[First]]\n\n![[Second]]",
            &[2, 4],
            "# Same\n\n![[Nested]]\n\n[web](https://example.com)\n\n![asset](asset.png)\n\n- [ ] task\n\n<div>HTML</div>\n\n```rust\nexact();\n```\n",
        );
        response.entries[0].vault = "".into();
        response.entries[1].vault = "foreign".into();
        let preview = included_preview(&response).unwrap();
        assert_eq!(response.entries[0].vault, "");
        assert_eq!(response.entries[1].vault, "foreign");
        let indices: Vec<_> = preview
            .items
            .iter()
            .filter_map(|item| match item {
                PreviewItem::Include { index, blocks, .. } => {
                    assert_eq!(
                        blocks
                            .iter()
                            .find(|b| b.kind == BlockKind::Code)
                            .unwrap()
                            .text,
                        "exact();\n"
                    );
                    assert!(blocks.iter().any(|b| b.text.contains("![[Nested]]")));
                    Some(*index)
                }
                _ => None,
            })
            .collect();
        assert_eq!(indices, [0, 1]);
        // Independent block vectors contain duplicate headings, not a merged host namespace.
        assert!(matches!(&preview.items[0],PreviewItem::Host(b) if b[0].text == "Same"));
        fn send<T: Send>() {}
        send::<IncludePreview>();
    }
    #[test]
    fn errors_empty_excerpts_and_bad_options_are_not_dropped() {
        let mut response = response("![[Missing]]\n\n![[Empty]]", &[0, 2], "");
        response.entries[0].error = Some("missing section".into());
        response.entries[0].bad_options = vec!["bad option".into()];
        response.entries[0].note_id = None;
        let preview = included_preview(&response).unwrap();
        assert_eq!(preview.items.len(), 2);
        for item in &preview.items {
            let PreviewItem::Include {
                blocks, directive, ..
            } = item
            else {
                panic!()
            };
            assert!(blocks.is_empty());
            assert!(!directive.is_empty());
        }
        assert_eq!(
            response.entries[0].error.as_deref(),
            Some("missing section")
        );
        assert_eq!(response.entries[0].bad_options, ["bad option"]);
        assert!(response.entries[1].error.is_none());
    }
    #[test]
    fn projection_limits_are_explicit_and_leave_response_unchanged() {
        let body = (0..32)
            .map(|i| format!("![[Target{i}]]\n\n"))
            .collect::<String>();
        let mut response = response(
            &body,
            &(0..32).map(|i| i * 2).collect::<Vec<_>>(),
            "excerpt",
        );
        assert!(included_preview(&response).is_ok());
        response.entries.push(response.entries[0].clone());
        assert!(included_preview(&response).is_err());
        response.entries.pop();
        response.entries[1].line = response.entries[0].line;
        assert!(included_preview(&response).is_err());
        response.entries[1].line = u64::MAX;
        assert!(included_preview(&response).is_err());
        let mut response = super::include_preview_tests::response("![[Target]]", &[0], "");
        response.entries[0].excerpt = "x".repeat(256 * 1024 + 1);
        assert!(included_preview(&response).is_err());
        let response =
            super::include_preview_tests::response(&"paragraph\n\n".repeat(4097), &[], "");
        assert!(included_preview(&response).is_err());
        assert_eq!(response.markdown, "paragraph\n\n".repeat(4097));
        let response = super::include_preview_tests::response(
            "![[Target]]",
            &[0],
            &"paragraph\n\n".repeat(4096),
        );
        assert!(included_preview(&response).is_err());
    }
}

#[cfg(test)]
mod plain_reader_ownership_tests {
    use super::*;
    #[test]
    fn only_noninteractive_prose_blocks_can_acquire_reader_keys() {
        assert!(plain_reader_block(&parse("A plain 日本語 paragraph.")[0]));
        for text in [
            "# Heading",
            "[link](Other)",
            "[[Missing]]",
            "`code`",
            "```rs\nlet x=1;\n```",
            "![image](assets/x.png)",
            "> quoted",
            "- item",
            "| x |\n|---|\n| y |",
            "Ref[^x]\n\n[^x]: body",
        ] {
            assert!(!plain_reader_block(&parse(text)[0]), "{text}");
        }
    }
}

#[cfg(test)]
mod authored_block_tests {
    use super::*;
    fn wiki(id: &str) -> LinkTarget {
        LinkTarget::Wiki {
            key: "Note".into(),
            fragment: Some(format!("#^{id}")),
            authored_label: false,
        }
    }
    #[test]
    fn accepted_reader_hides_terminal_markers_but_snapshot_and_preview_keep_source() {
        for source in [
            "日本語 paragraph ^A_1-z",
            "- item ^A_1-z",
            "1. ordered ^A_1-z",
            "- [x] task ^A_1-z",
            "- loose ^A_1-z\n\n- second",
        ] {
            let reader = parse_reader(source);
            assert_eq!(reader[0].block_id.as_deref(), Some("A_1-z"), "{source}");
            assert!(!reader[0].text.contains("^A_1-z"));
            assert!(parse(source)[0].text.contains("^A_1-z"));
            assert!(preview_blocks(source).unwrap()[0].text.contains("^A_1-z"));
            assert_eq!(reader[0].marker, parse(source)[0].marker);
            assert_eq!(reader[0].indent, parse(source)[0].indent);
            assert_eq!(
                fragment_anchor(&reader, &wiki("A_1-z")),
                Ok(Some("^A_1-z".into()))
            );
        }
    }
    #[test]
    fn unsupported_or_nonterminal_shapes_remain_visible_without_anchor() {
        for source in [
            "^bare",
            "- ^bare",
            "text ^bad!",
            "text ^_bad",
            "text ^日本語",
            "text ^id more",
            "text `^id`",
            "text \\^id",
            "text **^id**",
            "# heading ^id",
            "> quote ^id",
            "```rust\ntext ^id\n```",
            "| cell |\n|---|\n| text ^id |",
            "text [^id](other)",
        ] {
            let reader = parse_reader(source);
            assert_eq!(reader, parse(source), "{source}");
            assert!(authored_block_targets(&reader).is_empty(), "{source}");
        }
    }
    #[test]
    fn visible_style_link_and_find_byte_offsets_remain_valid() {
        let reader = parse_reader("日本語 **bold** and [link](https://example.com) ^id");
        assert_eq!(reader[0].text, "日本語 bold and link");
        assert_eq!(reader[0].block_id.as_deref(), Some("id"));
        for span in &reader[0].spans {
            assert!(reader[0].text.get(span.range.clone()).is_some());
        }
        assert_eq!(&reader[0].text[reader[0].links[0].range.clone()], "link");
        let found = crate::reader_find::search(&reader, "link");
        assert_eq!(found.hits.len(), 1);
        assert!(crate::reader_find::search(&reader, "^id").hits.is_empty());
    }
    #[test]
    fn duplicate_ids_are_first_case_sensitive_and_separate_from_headings_footnotes() {
        let reader =
            parse_reader("first ^id\n\nsecond ^id\n\nuppercase ^ID\n\n# id\n\n[^id]: definition");
        let targets = authored_block_targets(&reader);
        assert_eq!(targets.get("^id"), Some(&0));
        assert_eq!(targets.get("^ID"), Some(&2));
        assert_eq!(
            fragment_anchor(&reader, &wiki("Id")),
            Err(FragmentError::MissingBlock("Id".into()))
        );
        assert_eq!(
            fragment_anchor(&reader, &wiki("missing")),
            Err(FragmentError::MissingBlock("missing".into()))
        );
        assert_eq!(
            fragment_anchor(&reader, &wiki("bad!")),
            Err(FragmentError::InvalidFragment)
        );
        assert_eq!(
            fragment_anchor(
                &reader,
                &LinkTarget::Markdown {
                    destination: "#^id".into()
                }
            ),
            Ok(Some("^id".into()))
        );
        assert_eq!(outline(&reader)[0].anchor, "id");
        assert!(footnote_targets(&reader).contains_key("id"));
    }
    #[test]
    fn tight_nested_and_loose_runs_do_not_attach_bare_markers_or_change_code() {
        let reader = parse_reader(
            "- first ^one\n  - child ^two\n- [ ] task ^three\n\n- fourth ^four\n\n  later paragraph ^five\n\n```rust\nlet x = 1; ^code\n```",
        );
        for id in ["one", "two", "three", "four", "five"] {
            assert!(
                authored_block_targets(&reader).contains_key(&format!("^{id}")),
                "{id}"
            );
        }
        let code = reader
            .iter()
            .find(|block| block.kind == BlockKind::Code)
            .unwrap();
        assert_eq!(code.text, "let x = 1; ^code\n");
        assert_eq!(code.language.as_deref(), Some("rust"));
        assert!(code.block_id.is_none());
    }
}

#[cfg(test)]
mod authored_marker_protected_ranges {
    #[test]
    fn trailing_label_or_inline_code_space_is_preserved_without_projection() {
        for source in [
            "[foo ](https://example.com) ^id",
            "`foo ` ^id",
            "日本語 [空白 ](target) ^id",
        ] {
            let blocks = super::parse_reader(source);
            assert_eq!(blocks, super::parse(source), "{source}");
            assert!(blocks[0].block_id.is_none());
            for link in &blocks[0].links {
                assert!(blocks[0].text.get(link.range.clone()).is_some());
            }
            for span in &blocks[0].spans {
                assert!(blocks[0].text.get(span.range.clone()).is_some());
            }
        }
    }
}
