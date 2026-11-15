//! Portable Markdown from exact accepted source bytes. Not a rendered reconstruction.
use crate::code_copy::Document;
use pulldown_cmark::{Event, Options, Parser, Tag};
use std::{ops::Range, sync::Arc};
pub const MAX_BYTES: usize = 256 * 1024;

fn protected_ranges(source: &str) -> Vec<Range<usize>> {
    let parser = Parser::new_ext(
        source,
        Options::ENABLE_TABLES
            | Options::ENABLE_TASKLISTS
            | Options::ENABLE_STRIKETHROUGH
            | Options::ENABLE_FOOTNOTES,
    );
    let mut ranges: Vec<Range<usize>> = parser
        .reference_definitions()
        .iter()
        .map(|(_, definition)| definition.span.clone())
        .collect();
    // The reference map is first-definition-wins. Preserve duplicate/uncertain
    // definition-looking lines and their nonblank continuations conservatively.
    // This is source protection only, not reference resolution or reconstruction.
    let mut offset = 0;
    let mut definition_run = false;
    for line in source.split_inclusive('\n') {
        if line.trim().is_empty() {
            definition_run = false;
        }
        let definition_like = line.find('[').is_some_and(|start| {
            let suffix = &line[start..];
            !suffix.starts_with("[[") && !suffix.starts_with("[^") && suffix.contains("]:")
        });
        definition_run |= definition_like;
        if definition_run {
            ranges.push(offset..offset + line.len());
        }
        offset += line.len();
    }
    for (event, range) in parser.into_offset_iter() {
        if matches!(
            event,
            Event::Code(_)
                | Event::Html(_)
                | Event::InlineHtml(_)
                | Event::Start(Tag::CodeBlock(_) | Tag::Link { .. } | Tag::Image { .. })
        ) {
            ranges.push(range);
        }
    }
    ranges.sort_by_key(|range| range.start);
    let mut merged: Vec<Range<usize>> = Vec::new();
    for range in ranges {
        if let Some(last) = merged.last_mut().filter(|last| last.end >= range.start) {
            last.end = last.end.max(range.end);
        } else {
            merged.push(range);
        }
    }
    merged
}
fn protected(ranges: &[Range<usize>], range: Range<usize>) -> bool {
    ranges
        .get(ranges.partition_point(|old| old.end <= range.start))
        .is_some_and(|old| old.start < range.end)
}
// Same balanced-bracket shape as reader wiki labels, but no resolution or rendering.
fn wiki_at(text: &str, start: usize) -> Option<(usize, String)> {
    if !text.get(start..)?.starts_with("[[") {
        return None;
    }
    let mut depth = 0usize;
    for (offset, ch) in text[start + 2..].char_indices() {
        let at = start + 2 + offset;
        match ch {
            '\r' | '\n' => return None,
            '[' => depth += 1,
            ']' if depth > 0 => depth -= 1,
            ']' => {
                if !text[at..].starts_with("]]") {
                    return None;
                }
                let body = &text[start + 2..at];
                let (target, alias) = body
                    .split_once('|')
                    .map_or((body, None), |(target, alias)| (target, Some(alias.trim())));
                let title = target.split('#').next()?.trim();
                if title.is_empty() {
                    return None;
                }
                return Some((
                    at + 2,
                    alias
                        .filter(|alias| !alias.is_empty())
                        .unwrap_or(title)
                        .to_owned(),
                ));
            }
            _ => {}
        }
    }
    None
}
fn positive(text: &str) -> Option<u64> {
    if text.is_empty() || !text.bytes().all(|byte| byte.is_ascii_digit()) {
        return None;
    }
    text.parse::<u64>().ok().filter(|value| *value > 0)
}
fn line_range(text: &str) -> bool {
    text.split(',').all(|range| {
        if let Some((first, last)) = range.split_once('-') {
            matches!((positive(first), positive(last)), (Some(first), Some(last)) if last >= first)
        } else {
            positive(range).is_some()
        }
    })
}
fn include_tail(tail: &str) -> bool {
    if !tail.is_empty() && !tail.starts_with(char::is_whitespace) {
        return false;
    }
    let mut words = tail.split_whitespace();
    while let Some(word) = words.next() {
        match word {
            ":only-contents" => {}
            ":lines" if words.next().is_some_and(line_range) => {}
            _ => return false,
        }
    }
    true
}
fn convert_line(line: &str, offset: usize, ranges: &[Range<usize>]) -> String {
    let content = line.strip_suffix('\n').unwrap_or(line);
    let content = content.strip_suffix('\r').unwrap_or(content);
    let newline = &line[content.len()..];
    let indent = content.len() - content.trim_start_matches([' ', '\t']).len();
    if content[indent..].strip_prefix('!').is_some_and(|rest| {
        !rest.starts_with("[[") && rest.trim_start_matches([' ', '\t']).starts_with("[[")
    }) {
        return line.to_owned(); // malformed spaced include, not an ordinary wiki link
    }
    if content[indent..].starts_with("![[") {
        // Unknown tails must retain the entire original line, not lose prose.
        if !protected(ranges, offset..offset + content.len()) {
            if let Some((end, label)) = wiki_at(content, indent + 1) {
                let tail = &content[end..];
                if include_tail(tail) {
                    let whitespace = if tail.trim().is_empty() { tail } else { "" };
                    return format!("{}{label}{whitespace}{newline}", &content[..indent]);
                }
            }
        }
        return line.to_owned();
    }
    let mut output = String::new();
    let mut cursor = 0;
    while let Some(relative) = content[cursor..].find("[[") {
        let start = cursor + relative;
        output.push_str(&content[cursor..start]);
        let Some((end, label)) = wiki_at(content, start) else {
            output.push_str(&content[start..]);
            output.push_str(newline);
            return output;
        };
        let escaped = content[..start]
            .bytes()
            .rev()
            .take_while(|byte| *byte == b'\\')
            .count()
            % 2
            == 1;
        if escaped
            || content[..start].ends_with('!')
            || protected(ranges, offset + start..offset + end)
        {
            output.push_str(&content[start..end]);
        } else {
            output.push_str(&label);
        }
        cursor = end;
    }
    output.push_str(&content[cursor..]);
    output.push_str(newline);
    output
}
pub fn portable(source: &str) -> Result<String, &'static str> {
    if source.len() > MAX_BYTES {
        return Err("Body exceeds the 256 KiB copy limit; nothing was copied.");
    }
    let ranges = protected_ranges(source);
    let mut output = String::with_capacity(source.len());
    let mut offset = 0;
    for line in source.split_inclusive('\n') {
        output.push_str(&convert_line(line, offset, &ranges));
        offset += line.len();
        if output.len() > MAX_BYTES {
            return Err("Converted Markdown exceeds the copy limit; nothing was copied.");
        }
    }
    Ok(output)
}
#[derive(Clone, Debug, PartialEq)]
struct Source {
    document: Document,
    body: Arc<str>,
}
#[derive(Default)]
pub struct State {
    source: Option<Source>,
    pub epoch: u64,
    serial: u64,
    pub feedback: Option<u64>,
    pub oversized: bool,
}
impl State {
    pub fn ready(&self) -> bool {
        self.source.is_some()
    }
    pub fn sync(&mut self, document: Option<Document>, body: Option<&str>) {
        let oversized = document.is_some() && body.is_some_and(|body| body.len() > MAX_BYTES);
        let valid = document
            .as_ref()
            .zip(body)
            .filter(|(_, body)| body.len() <= MAX_BYTES);
        let same = match (&self.source, valid) {
            (Some(old), Some((document, body))) => {
                old.document == *document && old.body.as_ref() == body
            }
            (None, None) => true,
            _ => false,
        };
        if !same || self.oversized != oversized {
            self.epoch = self.epoch.wrapping_add(1);
            self.feedback = None;
            self.source = valid.map(|(document, body)| Source {
                document: document.clone(),
                body: Arc::from(body),
            });
        }
        self.oversized = oversized;
    }
    pub fn payload(
        &self,
        epoch: u64,
        document: Option<&Document>,
        body: Option<&str>,
    ) -> Option<Result<String, &'static str>> {
        let source = self.source.as_ref()?;
        if epoch != self.epoch
            || document != Some(&source.document)
            || body != Some(source.body.as_ref())
        {
            return None;
        }
        Some(portable(&source.body))
    }
    pub fn submitted(&mut self) -> u64 {
        self.serial = self.serial.wrapping_add(1);
        self.feedback = Some(self.serial);
        self.serial
    }
    pub fn expire(&mut self, epoch: u64, serial: u64) -> bool {
        if epoch != self.epoch || self.feedback != Some(serial) {
            return false;
        }
        self.feedback = None;
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn ordinary_markdown_unicode_whitespace_and_line_endings_are_exact() {
        let body = "# 日本語  \r\n\r\n- [x] task\t\r\n\n[link](https://example.com) ![image](assets/x.png)\n| A | B |\n|---|---|\n| x | y |\n\nlast  ";
        assert_eq!(portable(body).unwrap(), body);
        assert_eq!(portable("").unwrap(), "");
    }
    #[test]
    fn labels_drop_fragments_keep_numeric_ids_nested_brackets_and_empty_alias_fallback() {
        assert_eq!(portable("[[ Title #Heading ]] [[日本語|別名]] [[id:123#^block]] [[1794728000#^x|]] [[Note [nested]|alias [nested]]] [[Note|   ]]\n").unwrap(),
            "Title 別名 id:123 1794728000 alias [nested] Note\n");
    }
    #[test]
    fn only_wholly_valid_include_lines_remove_marker_and_known_options() {
        assert_eq!(portable("![[Note]]\n  ![[Note##設計|抜粋]] :only-contents :lines 4-5,8\r\n![[Note|]] :lines 1 :only-contents :lines 2-2\n").unwrap(),"Note\n  抜粋\r\nNote\n");
        for line in [
            "![[Note]] suffix",
            "![[Note]] :unknown",
            "![[Note]] :lines",
            "![[Note]] :lines 0",
            "![[Note]] :lines 4-2",
            "![[Note]] :lines 1,",
            "![[Note]] :lines x",
            "![[Note]] :lines 1-2-3",
            "![[Note]] [[Another]]",
            "! [[Note]]",
            "before ![[Note]] after",
        ] {
            assert_eq!(portable(line).unwrap(), line, "{line}");
        }
    }
    #[test]
    fn code_and_ordinary_markdown_links_images_html_are_preserved() {
        let body = "`[[Code|alias]]` and [[Live#x]]\r\n```md\r\n[[Code|alias]]\r\n![[Code]] :lines 1\r\n```\r\n\n    ![[Indented]]\n\n[ordinary [[wiki]]](https://example.com)\n![image [[wiki]]](assets/a.png)\n<div>[[raw]]</div>\n";
        assert_eq!(portable(body).unwrap(), body.replace("[[Live#x]]", "Live"));
    }
    #[test]
    fn malformed_and_escaped_wikis_remain_literal_without_destructive_tail_changes() {
        for source in [
            "[[unclosed",
            "[[#only-fragment]]",
            "[[|alias]]",
            "[[bad] tail",
            "\\[[Escaped|alias]]",
            "[[nested [[unclosed]]",
            "![[Note]] arbitrary suffix [[Valid]]",
        ] {
            assert_eq!(portable(source).unwrap(), source, "{source}");
        }
        assert_eq!(
            portable("| [[Note#x|label]] | plain |\r\n|---|---|\r\n").unwrap(),
            "| label | plain |\r\n|---|---|\r\n"
        );
    }
    #[test]
    fn byte_limit_and_adversarial_nesting_are_bounded_without_truncation() {
        let body = "é".repeat(MAX_BYTES / 2);
        assert_eq!(portable(&body).unwrap(), body);
        assert!(portable(&(body + "x")).is_err());
        let malformed = "[".repeat(MAX_BYTES);
        assert_eq!(portable(&malformed).unwrap(), malformed);
        let nested = format!("[[{}x{}]]", "[".repeat(10000), "]".repeat(10000));
        assert_eq!(
            portable(&nested).unwrap(),
            format!("{}x{}", "[".repeat(10000), "]".repeat(10000))
        );
    }
    fn document() -> Document {
        Document {
            tab: Some(crate::tabs::TabKey::Note {
                vault: "work".into(),
                note_id: 42,
            }),
            request: 1,
            etag: Some("rev".into()),
            storage: Some(2),
            launch: Some(3),
        }
    }
    #[test]
    fn copy_revalidates_exact_body_source_and_unavailable_states() {
        let doc = document();
        let mut state = State::default();
        state.sync(Some(doc.clone()), Some("[[A]]"));
        let epoch = state.epoch;
        assert_eq!(
            state
                .payload(epoch, Some(&doc), Some("[[A]]"))
                .unwrap()
                .unwrap(),
            "A"
        );
        assert!(state.payload(epoch, Some(&doc), Some("[[B]]")).is_none());
        let mut changed = vec![doc.clone(); 6];
        changed[0].request += 1;
        changed[1].etag = Some("changed".into());
        changed[2].storage = Some(4);
        changed[3].launch = Some(4);
        changed[4].tab = Some(crate::tabs::TabKey::Note {
            vault: "other".into(),
            note_id: 42,
        });
        changed[5].tab = Some(crate::tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 43,
        });
        for other in changed {
            assert!(state.payload(epoch, Some(&other), Some("[[A]]")).is_none());
        }
        assert!(state.payload(epoch, None, Some("[[A]]")).is_none());
        state.sync(None, None);
        state.sync(Some(doc.clone()), Some("[[A]]"));
        assert!(state.payload(epoch, Some(&doc), Some("[[A]]")).is_none());
        state.sync(Some(doc), Some(&"x".repeat(MAX_BYTES + 1)));
        assert!(!state.ready());
        assert!(state.oversized);
    }
    #[test]
    fn repeated_submission_and_navigation_tokens_do_not_clear_new_feedback() {
        let doc = document();
        let mut state = State::default();
        state.sync(Some(doc.clone()), Some("body"));
        let epoch = state.epoch;
        let first = state.submitted();
        let second = state.submitted();
        assert!(!state.expire(epoch, first));
        assert!(state.expire(epoch, second));
        let old = state.submitted();
        state.sync(None, None);
        state.sync(Some(doc), Some("body"));
        let new = state.submitted();
        assert!(!state.expire(epoch, old));
        assert_eq!(state.feedback, Some(new));
    }
}

#[cfg(test)]
mod reference_definition_tests {
    use super::portable;
    #[test]
    fn reference_destinations_titles_duplicates_and_continuations_are_literal() {
        for body in [
            "[r]: https://example.com/[[Note]] \"[[title]]\"\n\n[use][r]\n",
            "[r]: https://example.com/first\n[r]: https://example.com/[[Duplicate]]\n  \"title [[Label]]\"\n",
            "[r]:\n  https://example.com/[[Note]]\n  \"title [[Label]]\"\n",
            "> [r]: https://example.com/[[Note]]\n",
            "- [r]: https://example.com/[[Note]]\n  [r]: https://example.com/[[Duplicate]]\n",
        ] {
            assert_eq!(portable(body).unwrap(), body);
        }
        assert_eq!(
            portable("[r]: https://example.com/[[Note]]\n\n[[Live#fragment]]\n").unwrap(),
            "[r]: https://example.com/[[Note]]\n\nLive\n"
        );
    }
}
