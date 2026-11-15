//! Literal query refinement only; the owned search engine interprets tag terms.
const MAX_QUERY_BYTES: usize = 64 * 1024;
pub fn valid_tag(tag: &str) -> bool {
    !tag.is_empty()
        && tag.len() <= crate::tabs::TAG_MAX_BYTES
        && !tag.contains(['%', '_'])
        && !tag.ends_with('/')
        && !tag.chars().any(|c| c.is_whitespace() || c.is_control())
}
pub fn append(query: &str, tag: &str) -> Option<String> {
    if !valid_tag(tag) || query.len() > MAX_QUERY_BYTES {
        return None;
    }
    let term = format!("#{tag}");
    let mut words: Vec<_> = query.split_whitespace().collect();
    if words.contains(&term.as_str()) {
        return None;
    }
    words.push(&term);
    let next = words.join(" ");
    (next.len() <= MAX_QUERY_BYTES).then_some(next)
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Stamp {
    pub query: String,
    pub epoch: u64,
    pub request: u64,
    pub launch: u64,
    pub storage: u64,
    pub scope_revision: u64,
}
pub fn admits(
    captured: &Stamp,
    current: Option<&Stamp>,
    composing: bool,
    owners_allowed: bool,
) -> bool {
    !composing && owners_allowed && current == Some(captured)
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn append_preserves_terms_case_and_unicode_and_normalizes_whitespace() {
        assert_eq!(
            append("  hello  from:work\t#old ", "日本語/a"),
            Some("hello from:work #old #日本語/a".into())
        );
        assert_eq!(append("", "a"), Some("#a".into()));
        assert_eq!(append("#A", "a"), Some("#A #a".into()));
        assert_eq!(append("#a/b #ab", "a"), Some("#a/b #ab #a".into()));
    }
    #[test]
    fn exact_duplicate_is_a_complete_noop_not_whitespace_rewrite() {
        assert_eq!(append("  #a\ttext ", "a"), None);
        assert_eq!(append("#日本語/a", "日本語/a"), None);
    }
    #[test]
    fn wildcard_and_trailing_slash_tags_are_inert_but_literal_punctuation_is_preserved() {
        for tag in ["a_b", "a%b", "a/", "/"] {
            assert!(!valid_tag(tag));
            assert_eq!(append("text", tag), None);
        }
        for tag in ["a|b", "\"a\"", "#a", "a/b"] {
            assert!(valid_tag(tag));
            assert_eq!(append("text", tag), Some(format!("text #{tag}")));
        }
    }
    #[test]
    fn malformed_or_oversized_input_is_never_quoted_or_truncated() {
        for tag in ["", "two words", "a\nb", "a\tb", "a\0b", "a\u{2003}b"] {
            assert!(!valid_tag(tag));
            assert!(append("hello", tag).is_none());
        }
        assert!(!valid_tag(&"x".repeat(crate::tabs::TAG_MAX_BYTES + 1)));
        assert!(append(&"x".repeat(MAX_QUERY_BYTES), "a").is_none());
        assert!(append(&"x".repeat(MAX_QUERY_BYTES + 1), "a").is_none());
    }
    #[test]
    fn newer_query_scope_route_or_lifecycle_and_composition_reject_callback() {
        let stamp = Stamp {
            query: "text".into(),
            epoch: 1,
            request: 2,
            launch: 3,
            storage: 4,
            scope_revision: 5,
        };
        assert!(admits(&stamp, Some(&stamp), false, true));
        assert!(!admits(&stamp, None, false, true));
        assert!(!admits(&stamp, Some(&stamp), true, true));
        assert!(!admits(&stamp, Some(&stamp), false, false));
        for i in 0..6 {
            let mut next = stamp.clone();
            match i {
                0 => next.query.push('x'),
                1 => next.epoch += 1,
                2 => next.request += 1,
                3 => next.launch += 1,
                4 => next.storage += 1,
                _ => next.scope_revision += 1,
            };
            assert!(!admits(&stamp, Some(&next), false, true));
        }
    }
}
