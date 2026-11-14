//! Native, read-only CommonMark subset. Unsupported content stays inert and visible.
use crate::reader_find::{Matches, Target};
use gpui::{
    AnyElement, App, Div, FontStyle, FontWeight, InteractiveText, ScrollAnchor, ScrollHandle,
    SharedString, StyledText, TextRun, UnderlineStyle, Window, div, font, prelude::*, px, rgb,
};
use pulldown_cmark::{Alignment, Event, Options, Parser, Tag};
use std::{ops::Range, rc::Rc};

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum BlockKind {
    Heading(u8),
    Paragraph,
    Code,
    Literal,
    Table,
}

#[derive(Clone, Debug, PartialEq)]
pub struct Block {
    pub kind: BlockKind,
    pub text: String,
    pub indent: usize,
    pub quoted: bool,
    pub marker: Option<String>,
    pub spans: Vec<Span>,
    pub links: Vec<Link>,
    pub table: Option<Table>,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct InlineStyle {
    pub emphasis: bool,
    pub strong: bool,
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

/// Shell supplies only resolved internal links. Pending, missing and errors stay inert.
#[derive(Clone)]
pub struct LinkCallbacks {
    pub resolved: Rc<dyn Fn(&LinkTarget) -> bool>,
    pub activate: Rc<dyn Fn(&LinkTarget, &mut Window, &mut App)>,
}

/// Resolver input, with source vault passed separately by the shell. Never infer
/// numeric IDs, registered vault prefixes or id-v1 configuration here.
pub fn resolve_term(target: &LinkTarget) -> Option<String> {
    match target {
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
    UnsupportedBlock,
    InvalidFragment,
    UnsupportedDestination,
}

/// Match a verified reader heading fragment to this renderer's native anchor.
/// No I/O: the caller supplies the accepted destination document, retains its
/// navigation generation, and scrolls only after that document has laid out.
/// Missing headings are explicitly reported so the shell can use note-top fallback.
pub fn fragment_anchor(
    blocks: &[Block],
    target: &LinkTarget,
) -> Result<Option<String>, FragmentError> {
    let fragment = match target {
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
    if text.starts_with('^') {
        return Err(FragmentError::UnsupportedBlock);
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

/// Retain this alongside the displayed document. Rebuild when its headings or
/// reader ScrollHandle change; do not recreate on each render (anchors hold layout).
pub struct HeadingAnchors {
    targets: std::collections::HashMap<String, ScrollAnchor>,
}

impl HeadingAnchors {
    pub fn new(blocks: &[Block], handle: &ScrollHandle) -> Self {
        Self {
            targets: outline(blocks)
                .into_iter()
                .map(|entry| (entry.anchor, ScrollAnchor::for_handle(handle.clone())))
                .collect(),
        }
    }

    /// Call after the document has laid out. Supports nested article/aside layout.
    /// Request a frame with cx.notify() in the calling view after this returns true.
    pub fn scroll_to(&self, anchor: &str, window: &mut Window, cx: &mut App) -> bool {
        let Some(target) = self.targets.get(anchor) else {
            return false;
        };
        target.scroll_to(window, cx);
        true
    }
}

/// Retained block/cell anchors; this does not provide exact text-range scrolling.
pub struct FindAnchors {
    targets: std::collections::HashMap<Target, ScrollAnchor>,
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
        Self { targets }
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
                _ => {}
            }
            for child in &node.children {
                inline(child, source, out, style);
            }
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
        Event::SoftBreak => out.append(" ", style),
        Event::HardBreak => out.append("\n", style),
        _ => out.append(&source[node.range.clone()], style),
    }
}

fn blocks(node: &Node<'_>, source: &str, indent: usize, quoted: bool, out: &mut Vec<Block>) {
    match &node.event {
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
                    kind: BlockKind::Literal,
                    text: source[node.range.clone()].into(),
                    indent,
                    quoted,
                    marker: None,
                    spans: vec![],
                    links: vec![],
                    table: None,
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
                kind: BlockKind::Table,
                text: source[node.range.clone()].into(),
                indent,
                quoted,
                marker: None,
                spans: vec![],
                links: vec![],
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
                blocks(child, source, indent + 1, quoted, out);
                if let Some(first) = out.get_mut(before) {
                    first.marker = Some(
                        start.map_or_else(|| "•".into(), |n| format!("{}.", n + index as u64)),
                    );
                }
            }
            return;
        }
        Event::Start(Tag::Item) => {
            // Tight lists omit paragraph tags. Group inline runs into one paragraph.
            let mut run = Cell::default();
            for child in &node.children {
                if matches!(
                    child.event,
                    Event::Start(
                        Tag::Paragraph
                            | Tag::Heading { .. }
                            | Tag::CodeBlock(_)
                            | Tag::List(_)
                            | Tag::BlockQuote(_)
                            | Tag::Table(_)
                    )
                ) {
                    flush_run(&mut run, indent, quoted, out);
                    blocks(child, source, indent, quoted, out);
                } else {
                    inline(child, source, &mut run, InlineStyle::default());
                }
            }
            flush_run(&mut run, indent, quoted, out);
            return;
        }
        Event::Start(Tag::BlockQuote(_)) => {
            for child in &node.children {
                blocks(child, source, indent, true, out);
            }
            return;
        }
        _ => {}
    }
    let kind = match &node.event {
        Event::Start(Tag::Heading { level, .. }) => BlockKind::Heading(*level as u8),
        Event::Start(Tag::Paragraph) => BlockKind::Paragraph,
        Event::Start(Tag::CodeBlock(_)) => BlockKind::Code,
        _ => BlockKind::Literal,
    };
    let mut content = Cell::default();
    if kind == BlockKind::Literal {
        content.text.push_str(&source[node.range.clone()]);
    } else {
        inline(node, source, &mut content, InlineStyle::default());
    }
    out.push(Block {
        kind,
        text: content.text,
        spans: content.spans,
        links: content.links,
        table: None,
        indent,
        quoted,
        marker: None,
    });
}

fn flush_run(run: &mut Cell, indent: usize, quoted: bool, out: &mut Vec<Block>) {
    if !run.text.is_empty() {
        out.push(Block {
            kind: BlockKind::Paragraph,
            text: std::mem::take(&mut run.text),
            spans: std::mem::take(&mut run.spans),
            links: std::mem::take(&mut run.links),
            table: None,
            indent,
            quoted,
            marker: None,
        });
    }
}

/// Parse CommonMark plus tables without I/O. Other extensions remain literal.
/// Reference definitions omitted by the parser are retained as source fallback.
pub fn parse(source: &str) -> Vec<Block> {
    let tree = nodes(&mut Parser::new_ext(source, Options::ENABLE_TABLES).into_offset_iter());
    let mut result = Vec::new();
    let mut end = 0;
    for node in &tree {
        fallback(&source[end..node.range.start], &mut result);
        blocks(node, source, 0, false, &mut result);
        end = node.range.end;
    }
    fallback(&source[end..], &mut result);
    result
}

fn fallback(text: &str, out: &mut Vec<Block>) {
    if !text.trim().is_empty() {
        out.push(Block {
            kind: BlockKind::Literal,
            text: text.to_owned(),
            spans: vec![],
            links: vec![],
            table: None,
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
pub fn render_themed(blocks: &[Block], dark: bool) -> Div {
    render_reader(blocks, dark, None, None, None)
}

/// Anchor-aware variant that preserves nested article layout. The outer reader
/// must track the same handle used to construct `anchors`.
pub fn render_themed_with_anchors(blocks: &[Block], dark: bool, anchors: &HeadingAnchors) -> Div {
    render_reader(blocks, dark, Some(anchors), None, None)
}

pub fn render_with_links(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: &LinkCallbacks,
) -> Div {
    render_reader(blocks, dark, anchors, Some(callbacks), None)
}

pub fn render_with_links_and_find(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: &LinkCallbacks,
    find: Option<FindPresentation<'_>>,
) -> Div {
    render_reader(blocks, dark, anchors, Some(callbacks), find)
}

fn render_reader(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: Option<&LinkCallbacks>,
    find: Option<FindPresentation<'_>>,
) -> Div {
    let theme = ReaderTheme::new(dark);
    div()
        .w_full()
        .min_w_0()
        .flex()
        .flex_col()
        .text_size(px(16.))
        .line_height(px(29.6))
        .font_family(".SystemUIFont")
        .text_color(rgb(theme.ink))
        .children(render_elements(blocks, dark, anchors, callbacks, find))
}

/// Direct block children for a shell-owned tracked scroller. Keep each element
/// as one direct child; wrapping the returned vector defeats child-index anchors.
/// Each row carries the same body metrics as render_themed.
pub fn render_block_elements_themed(blocks: &[Block], dark: bool) -> Vec<AnyElement> {
    render_elements(blocks, dark, None, None, None)
}

fn render_elements(
    blocks: &[Block],
    dark: bool,
    anchors: Option<&HeadingAnchors>,
    callbacks: Option<&LinkCallbacks>,
    find: Option<FindPresentation<'_>>,
) -> Vec<AnyElement> {
    let theme = ReaderTheme::new(dark);
    let entries = outline(blocks);
    let highlights = find_highlights(find);
    blocks
        .iter()
        .enumerate()
        .map(|(index, block)| {
            let mut row = div()
                .w_full()
                .min_w_0()
                .text_size(px(16.))
                .line_height(px(29.6))
                .font_family(".SystemUIFont")
                .text_color(rgb(theme.ink))
                .pl(px((block.indent * 16) as f32));
            if block.quoted {
                row = row.border_l_2().border_color(rgb(theme.strong_line)).pl_3();
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
            let offset = text.len() - block.text.len();
            let visible_spans = resolved_spans(&block.spans, &block.links, callbacks);
            let row = row.child(link_text(
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
                ),
                &block.links,
                offset,
                callbacks,
                format!("md-block-{index}"),
            ));
            let element =
                if let Some(entry) = entries.iter().find(|entry| entry.block_index == index) {
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
                } else {
                    row.into_any_element()
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
                        && (resolve_term(&link.target).is_some() || is_local_fragment(&link.target))
                        && callbacks.is_some_and(|callbacks| (callbacks.resolved)(&link.target))
                });
                span.style.link = resolved;
                span.style.unresolved = !resolved;
            }
            span
        })
        .collect()
}

fn link_text(
    text: StyledText,
    links: &[Link],
    offset: usize,
    callbacks: Option<&LinkCallbacks>,
    id: String,
) -> AnyElement {
    let Some(callbacks) = callbacks else {
        return text.into_any_element();
    };
    let active: Vec<_> = links
        .iter()
        .filter(|link| {
            (resolve_term(&link.target).is_some() || is_local_fragment(&link.target))
                && (callbacks.resolved)(&link.target)
        })
        .cloned()
        .collect();
    let ranges = active
        .iter()
        .map(|link| link.range.start + offset..link.range.end + offset)
        .collect();
    let activate = callbacks.activate.clone();
    InteractiveText::new(SharedString::from(id), text)
        .on_click(ranges, move |index, window, cx| {
            if let Some(link) = active.get(index) {
                activate(&link.target, window, cx);
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
        strikethrough: None,
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
                let view = view.child(link_text(
                    styled_text_find(
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
                    ),
                    &cell.links,
                    0,
                    callbacks,
                    format!("md-table-{block_index}-{index}-{column}"),
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
            Err(FragmentError::UnsupportedBlock)
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
        let block = parse("[[Title]] [external](https://example.com)").remove(0);
        let pending = resolved_spans(&block.spans, &block.links, None);
        assert!(pending.iter().filter(|span| span.style.unresolved).count() >= 2);
        assert!(pending.iter().all(|span| !span.style.link));
        let callbacks = LinkCallbacks {
            resolved: Rc::new(|_| true),
            activate: Rc::new(|_, _, _| {}),
        };
        let resolved = resolved_spans(&block.spans, &block.links, Some(&callbacks));
        assert!(resolved[0].style.link);
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
