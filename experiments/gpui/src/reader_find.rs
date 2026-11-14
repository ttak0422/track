//! Read-only search over rendered reader text, never source or workspace chrome.
use crate::markdown::Block;
use regex::RegexBuilder;
use std::ops::Range;

pub const MAX_QUERY_BYTES: usize = 4096;
pub const MAX_MATCHES: usize = 10_000;

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum Target {
    Block(usize),
    Cell {
        block: usize,
        row: usize,
        column: usize,
    },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Hit {
    pub target: Target,
    /// Original rendered text UTF-8 byte offsets, before any list marker.
    pub range: Range<usize>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Matches {
    pub hits: Vec<Hit>,
    pub truncated: bool,
    pub error: Option<String>,
}

// ECMAScript whitespace set; Rust's Unicode White_Space differs for FEFF/0085.
fn query_whitespace(ch: char) -> bool {
    matches!(ch, '\u{0009}'..='\u{000D}' | '\u{0020}' | '\u{00A0}' | '\u{1680}' |
        '\u{2000}'..='\u{200A}' | '\u{2028}' | '\u{2029}' | '\u{202F}' | '\u{205F}' |
        '\u{3000}' | '\u{FEFF}')
}

pub fn search(blocks: &[Block], query: &str) -> Matches {
    let mut result = Matches::default();
    if query.len() > MAX_QUERY_BYTES {
        result.error = Some(format!("Find query exceeds {MAX_QUERY_BYTES} bytes."));
        return result;
    }
    if query.chars().all(query_whitespace) {
        return result;
    }
    let mut pattern = String::new();
    let mut literal = String::new();
    let mut whitespace = false;
    for ch in query.chars() {
        if query_whitespace(ch) {
            pattern.push_str(&regex::escape(&literal));
            literal.clear();
            if !whitespace {
                pattern.push_str(r"[\x09-\x0D\x20\u{00A0}\u{1680}\u{2000}-\u{200A}\u{2028}\u{2029}\u{202F}\u{205F}\u{3000}\u{FEFF}]+");
            }
            whitespace = true;
        } else {
            literal.push(ch);
            whitespace = false;
        }
    }
    pattern.push_str(&regex::escape(&literal));
    let regex = match RegexBuilder::new(&pattern).case_insensitive(true).build() {
        Ok(regex) => regex,
        Err(_) => {
            result.error = Some("Find query could not be evaluated.".into());
            return result;
        }
    };
    let mut collect = |target: Target, text: &str| -> bool {
        for found in regex.find_iter(text) {
            if result.hits.len() == MAX_MATCHES {
                result.truncated = true;
                return false;
            }
            result.hits.push(Hit {
                target: target.clone(),
                range: found.range(),
            });
        }
        true
    };
    for (block, value) in blocks.iter().enumerate() {
        if let Some(table) = &value.table {
            for (row, cells) in table.rows.iter().enumerate() {
                for (column, cell) in cells.iter().enumerate() {
                    if !collect(Target::Cell { block, row, column }, &cell.text) {
                        return result;
                    }
                }
            }
        } else if !collect(Target::Block(block), &value.text) {
            return result;
        }
    }
    result
}

pub fn step(active: Option<usize>, len: usize, forward: bool) -> Option<usize> {
    if len == 0 {
        return None;
    }
    Some(match active.filter(|index| *index < len) {
        None => {
            if forward {
                0
            } else {
                len - 1
            }
        }
        Some(index) if forward => (index + 1) % len,
        Some(0) => len - 1,
        Some(index) => index - 1,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::markdown::parse;
    #[test]
    fn literal_unicode_offsets_and_simple_case_folding() {
        let blocks = parse("日本😀 K K k ſ S σ ς Σ .* [go]");
        for (q, expected) in [
            ("日本😀", 1),
            ("k", 3),
            ("s", 2),
            ("σ", 3),
            (".*", 1),
            ("[go]", 1),
        ] {
            let m = search(&blocks, q);
            assert_eq!(m.hits.len(), expected, "{q}");
            for hit in m.hits {
                assert!(blocks[0].text.get(hit.range).is_some());
            }
        }
        assert!(search(&parse("ß"), "ss").hits.is_empty());
        assert_eq!(search(&parse("café"), "cafe\u{301}").hits.len(), 0);
    }
    #[test]
    fn whitespace_is_flexible_but_not_trimmed_or_cross_block() {
        let blocks = parse("alpha **beta**\n gamma\n\nalpha\n\nbeta");
        assert_eq!(search(&blocks, "ALPHA beta").hits.len(), 1);
        assert_eq!(search(&blocks, "beta \n gamma").hits.len(), 1);
        assert_eq!(search(&blocks, " alpha").hits.len(), 0);
        assert_eq!(search(&blocks, "alpha beta gamma alpha").hits.len(), 0);
        assert!(search(&blocks, " \n\t").hits.is_empty());
        assert!(search(&blocks, "").hits.is_empty());
        assert_eq!(search(&parse("a\u{FEFF}b"), "a b").hits.len(), 1);
        assert!(search(&parse("a\u{0085}b"), "a b").hits.is_empty());
    }
    #[test]
    fn cells_are_ordered_and_never_joined_or_counted_twice() {
        let blocks = parse("| needle | needle |\n| --- | --- |\n| other | needle |");
        let m = search(&blocks, "needle");
        assert_eq!(m.hits.len(), 3);
        assert_eq!(
            m.hits[2].target,
            Target::Cell {
                block: 0,
                row: 1,
                column: 1
            }
        );
        assert!(search(&blocks, "needle needle").hits.is_empty());
    }
    #[test]
    fn bounds_and_nonoverlap_are_explicit() {
        assert_eq!(search(&parse("aaaa"), "aa").hits.len(), 2);
        let m = search(&parse(&"a ".repeat(MAX_MATCHES + 1)), "a");
        assert_eq!(m.hits.len(), MAX_MATCHES);
        assert!(m.truncated);
        assert!(!search(&parse(&"a ".repeat(MAX_MATCHES)), "a").truncated);
        assert!(
            search(&[], &"x".repeat(MAX_QUERY_BYTES + 1))
                .error
                .is_some()
        );
    }
    #[test]
    fn traversal_wraps_and_handles_reset_and_empty() {
        assert_eq!(step(None, 3, true), Some(0));
        assert_eq!(step(None, 3, false), Some(2));
        assert_eq!(step(Some(2), 3, true), Some(0));
        assert_eq!(step(Some(0), 3, false), Some(2));
        assert_eq!(step(Some(99), 3, true), Some(0));
        assert_eq!(step(Some(0), 0, true), None);
    }
}
