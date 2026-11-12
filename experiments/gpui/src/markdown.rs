//! Native, read-only CommonMark subset. Unsupported content stays inert and visible.
use gpui::{Div, div, prelude::*, px, rgb};
use pulldown_cmark::{Event, Parser, Tag};
use std::ops::Range;

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum BlockKind {
    Heading(u8),
    Paragraph,
    Code,
    Literal,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Block {
    pub kind: BlockKind,
    pub text: String,
    pub indent: usize,
    pub quoted: bool,
    pub marker: Option<String>,
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

fn inline(node: &Node<'_>, source: &str, out: &mut String) {
    match &node.event {
        Event::Start(Tag::Image { .. }) => out.push_str(&source[node.range.clone()]),
        Event::Start(Tag::Link { dest_url, .. }) => {
            for child in &node.children {
                inline(child, source, out);
            }
            out.push_str(" (");
            out.push_str(dest_url);
            out.push(')');
        }
        Event::Start(_) => {
            for child in &node.children {
                inline(child, source, out);
            }
        }
        Event::Text(text) | Event::Code(text) | Event::Html(text) | Event::InlineHtml(text) => {
            out.push_str(text)
        }
        Event::SoftBreak | Event::HardBreak => out.push('\n'),
        _ => out.push_str(&source[node.range.clone()]),
    }
}

fn blocks(node: &Node<'_>, source: &str, indent: usize, quoted: bool, out: &mut Vec<Block>) {
    match &node.event {
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
            let mut run = String::new();
            for child in &node.children {
                if matches!(
                    child.event,
                    Event::Start(
                        Tag::Paragraph
                            | Tag::Heading { .. }
                            | Tag::CodeBlock(_)
                            | Tag::List(_)
                            | Tag::BlockQuote(_)
                    )
                ) {
                    flush_run(&mut run, indent, quoted, out);
                    blocks(child, source, indent, quoted, out);
                } else {
                    inline(child, source, &mut run);
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
    let mut text = String::new();
    if kind == BlockKind::Literal {
        text.push_str(&source[node.range.clone()]);
    } else {
        inline(node, source, &mut text);
    }
    out.push(Block {
        kind,
        text,
        indent,
        quoted,
        marker: None,
    });
}

fn flush_run(run: &mut String, indent: usize, quoted: bool, out: &mut Vec<Block>) {
    if !run.is_empty() {
        out.push(Block {
            kind: BlockKind::Paragraph,
            text: std::mem::take(run),
            indent,
            quoted,
            marker: None,
        });
    }
}

/// Parse without I/O. CommonMark extensions intentionally remain literal text.
/// Reference definitions omitted by the parser are retained as source fallback.
pub fn parse(source: &str) -> Vec<Block> {
    let tree = nodes(&mut Parser::new(source).into_offset_iter());
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
            indent: 0,
            quoted: false,
            marker: None,
        });
    }
}

/// Render pre-parsed blocks. Put this inside a width-constrained scrolling parent.
/// No HTML execution, network fetching, or link navigation is performed.
pub fn render(blocks: &[Block]) -> Div {
    div()
        .w_full()
        .min_w_0()
        .flex()
        .flex_col()
        .gap_3()
        .children(blocks.iter().map(|block| {
            let mut row = div().w_full().min_w_0().pl(px((block.indent * 16) as f32));
            if block.quoted {
                row = row.border_l_2().border_color(rgb(0x7ec9b5)).pl_3();
            }
            let text = block.marker.as_ref().map_or_else(
                || block.text.clone(),
                |marker| format!("{marker} {}", block.text),
            );
            row = match block.kind {
                BlockKind::Heading(level) => row.text_size(px(match level {
                    1 => 28.,
                    2 => 24.,
                    3 => 21.,
                    4 => 18.,
                    _ => 16.,
                })),
                BlockKind::Code | BlockKind::Literal => {
                    row.font_family("Menlo").text_sm().p_3().bg(rgb(0x18222c))
                }
                BlockKind::Paragraph => row.text_base(),
            };
            row.child(text)
        }))
}

#[cfg(test)]
mod tests {
    use super::*;
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
            "| a | b |\n|---|---|\n| 1 | 2 |",
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
