//! Native, read-only CommonMark subset. Unsupported content stays inert and visible.
use gpui::{
    Div, FontStyle, FontWeight, StyledText, TextRun, UnderlineStyle, div, font, prelude::*, px, rgb,
};
use pulldown_cmark::{Alignment, Event, Options, Parser, Tag};
use std::ops::Range;

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
    pub table: Option<Table>,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct InlineStyle {
    pub emphasis: bool,
    pub strong: bool,
    pub code: bool,
    pub link: bool,
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

struct Node<'a> {
    event: Event<'a>,
    range: Range<usize>,
    children: Vec<Node<'a>>,
}

fn nodes<'a>(events: &mut impl Iterator<Item = (Event<'a>, Range<usize>)>) -> Vec<Node<'a>> {
    let mut result = Vec::new();
    while let Some((event, range)) = events.next() {
        if matches!(event, Event::End(_)) {
            break;
        }
        let children = if matches!(event, Event::Start(_)) {
            nodes(events)
        } else {
            vec![]
        };
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
            for child in &node.children {
                inline(child, source, out, style);
            }
            out.append(&format!(" ({dest_url})"), style);
        }
        Event::Start(tag) => {
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
        Event::Text(text) | Event::Html(text) | Event::InlineHtml(text) => out.append(text, style),
        Event::SoftBreak | Event::HardBreak => out.append("\n", style),
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
        .children(blocks.iter().enumerate().map(|(index, block)| {
            let mut row = div().w_full().min_w_0().pl(px((block.indent * 16) as f32));
            if block.quoted {
                row = row.border_l_2().border_color(rgb(theme.strong_line)).pl_3();
            }
            if let Some(table) = &block.table {
                return row.mt(px(13.)).child(render_table(table, theme));
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
            row.child(styled_text(text, &block.spans, offset, theme, mono, bold))
        }))
}

fn styled_text(
    text: String,
    spans: &[Span],
    offset: usize,
    theme: ReaderTheme,
    mono: bool,
    bold: bool,
) -> StyledText {
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
    StyledText::new(text).with_runs(runs)
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
        color: rgb(if style.code { theme.muted } else { theme.ink }).into(),
        background_color: None,
        underline: style.link.then(|| UnderlineStyle {
            thickness: px(1.),
            color: Some(rgb(theme.strong_line).into()),
            wavy: false,
        }),
        strikethrough: None,
    }
}

fn render_table(table: &Table, theme: ReaderTheme) -> Div {
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
                view.child(styled_text(
                    cell.text.clone(),
                    &cell.spans,
                    0,
                    theme,
                    false,
                    index == 0,
                ))
            }))
        }))
}

#[cfg(test)]
mod tests {
    use super::*;
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
        assert_eq!(
            block.text,
            "日本語 太い 斜体🦀 と code link (https://example.com)"
        );
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
        assert!(styled(" (https://example.com)").link);
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
        assert_eq!(table.rows[2][2].text, "link (https://example.com)");
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
            "| a | b |\nnot a delimiter"
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
        assert_eq!(parsed[2].text, "第一行\n第二行");
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
        assert_eq!(
            parse("[日本語](https://example.com/a)")[0].text,
            "日本語 (https://example.com/a)"
        );
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
        assert_eq!(parsed[2].text, "quoted\nnext");
        assert!(parse("").is_empty());
    }
}
