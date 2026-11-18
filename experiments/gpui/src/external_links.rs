//! Explicit browser dispatch only. No fetching, DNS, shell commands, or navigation state.
use crate::markdown::{Block, Link, LinkTarget};

/// Require an authored authority; do not accept URL-parser repairs or credentials.
pub fn destination(target: &LinkTarget) -> Option<String> {
    let LinkTarget::Markdown { destination } = target else {
        return None;
    };
    validate(destination)
}

fn validate(raw: &str) -> Option<String> {
    if raw
        .chars()
        .any(|ch| ch.is_control() || ch.is_ascii_whitespace() || ch == '\\')
    {
        return None;
    }
    let (scheme, rest) = raw.split_once("://")?;
    if !scheme.eq_ignore_ascii_case("http") && !scheme.eq_ignore_ascii_case("https") {
        return None;
    }
    let authority = rest.split(['/', '?', '#']).next()?;
    if authority.is_empty() || authority.contains('@') {
        return None;
    }
    let url = reqwest::Url::parse(raw).ok()?;
    if !matches!(url.scheme(), "http" | "https")
        || url.host_str().is_none()
        || !url.username().is_empty()
        || url.password().is_some()
    {
        return None;
    }
    Some(url.into())
}

/// Revalidate the source and exact displayed snapshot at the gesture boundary.
/// Only this function calls the injected opener; inspection never dispatches.
/// Taking immutable inputs prevents this path from saving/discarding a draft.
pub fn dispatch<S: PartialEq>(
    source: &S,
    current_source: Option<&S>,
    displayed: &[Block],
    current_displayed: &[Block],
    link: &Link,
    opener: impl FnOnce(&str),
) -> bool {
    if current_source != Some(source) || displayed != current_displayed {
        return false;
    }
    let exists = displayed.iter().any(|block| {
        block.links.contains(link)
            || block.table.as_ref().is_some_and(|table| {
                table
                    .rows
                    .iter()
                    .flatten()
                    .any(|cell| cell.links.contains(link))
            })
    });
    if !exists {
        return false;
    }
    let Some(url) = destination(&link.target) else {
        return false;
    };
    opener(&url);
    true
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn normalized_explicit_http_including_deliberate_local_destinations() {
        for raw in [
            "https://example.com/a?q=1#part",
            "HTTP://example.com/",
            "https://example.com:8443/path",
            "https://例え.jp/",
            "http://localhost/",
            "http://LOCALHOST./",
            "http://a.localhost/",
            "http://127.0.0.1/",
            "http://127.1/",
            "http://2130706433/",
            "http://[::1]/",
            "http://[::ffff:127.0.0.1]/",
            "http://127.0.0.1:34567/",
        ] {
            assert!(validate(raw).is_some(), "{raw}");
        }
        assert_eq!(
            validate("HTTP://EXAMPLE.COM:80/a%2Fb?q=%2520#x").as_deref(),
            Some("http://example.com/a%2Fb?q=%2520#x")
        );
        assert_eq!(
            validate("http://127.1/").as_deref(),
            Some("http://127.0.0.1/")
        );
    }
    #[test]
    fn unsupported_and_repaired_spellings_are_inert() {
        for raw in [
            "javascript:alert(1)",
            "file:///tmp/a",
            "mailto:a@example.com",
            "custom://x",
            "data:text/html,x",
            "https://u:p@example.com",
            "https://u@example.com",
            "https://@example.com",
            "https:example.com",
            "https:///example.com",
            "https:////example.com",
            "//example.com",
            "example.com/path",
            "/etc/passwd",
            "../note.md",
            "#Heading",
            "https://example.com:bad",
            "https://example.com:65536",
            "https://",
            "https://?a",
            " https://example.com",
            "https://example.com/\n",
            "https://exa\tmple.com",
            "https://example.com/\0",
            "https://example.com\\@other.test",
            "https://example.com/a b",
        ] {
            assert_eq!(validate(raw), None, "{raw:?}");
        }
        assert!(destination(&wiki_target()).is_none());
    }
    fn wiki_target() -> LinkTarget {
        crate::markdown::parse("[[https://example.com]]")[0].links[0]
            .target
            .clone()
    }
    #[test]
    fn only_current_exact_link_gestures_dispatch_once_and_keep_inputs_intact() {
        let displayed = crate::markdown::parse("[日本語 🦀](http://localhost:34567/a%2Fb?q=1#p)");
        let before = displayed.clone();
        let link = displayed[0].links[0].clone();
        let source = ("vault", 1, "etag", 2, 3, 4);
        let mut opened = Vec::new();
        assert!(destination(&link.target).is_some());
        assert!(opened.is_empty()); // render/focus/hover validation has no opener.
        assert!(dispatch(
            &source,
            Some(&source),
            &displayed,
            &displayed,
            &link,
            |url| opened.push(url.to_owned())
        ));
        assert_eq!(opened, ["http://localhost:34567/a%2Fb?q=1#p"]);
        let stale = ("vault", 1, "etag", 2, 3, 5);
        assert!(!dispatch(
            &source,
            Some(&stale),
            &displayed,
            &displayed,
            &link,
            |_| panic!("stale")
        ));
        assert!(!dispatch(
            &source,
            None,
            &displayed,
            &displayed,
            &link,
            |_| panic!("missing")
        ));
        let changed =
            crate::markdown::parse("prefix [日本語 🦀](http://localhost:34567/a%2Fb?q=1#p)");
        assert!(!dispatch(
            &source,
            Some(&source),
            &displayed,
            &changed,
            &link,
            |_| panic!("range changed")
        ));
        let mut forged = link.clone();
        forged.range = 0..1;
        assert!(!dispatch(
            &source,
            Some(&source),
            &displayed,
            &displayed,
            &forged,
            |_| panic!("forged")
        ));
        assert_eq!(displayed, before);
        assert!(dispatch(
            &source,
            Some(&source),
            &displayed,
            &displayed,
            &link,
            |url| opened.push(url.to_owned())
        ));
        assert_eq!(opened.len(), 2); // Two separate gestures, not duplicate handlers.
    }
    #[test]
    fn table_links_use_exact_displayed_ranges_and_changed_destination_is_rejected() {
        let displayed =
            crate::markdown::parse("| Link |\n| --- |\n| [日本語](https://example.com) |");
        let link = displayed[0]
            .table
            .as_ref()
            .unwrap()
            .rows
            .iter()
            .flatten()
            .flat_map(|cell| &cell.links)
            .next()
            .unwrap();
        let mut calls = 0;
        assert!(dispatch(&1, Some(&1), &displayed, &displayed, link, |_| {
            calls += 1
        }));
        assert_eq!(calls, 1);
        let mut forged = link.clone();
        forged.target = LinkTarget::Markdown {
            destination: "https://other.example".into(),
        };
        assert!(!dispatch(
            &1,
            Some(&1),
            &displayed,
            &displayed,
            &forged,
            |_| panic!("changed destination")
        ));
    }
    #[test]
    fn literal_code_and_internal_targets_never_dispatch() {
        let displayed = crate::markdown::parse(
            "`[x](https://example.com)`\n\n```\n[x](https://example.com)\n```\n\n[note](other.md)\n\n[heading](#Heading)",
        );
        assert!(displayed[0].links.is_empty());
        assert!(displayed[1].links.is_empty());
        for link in displayed.iter().flat_map(|block| &block.links) {
            assert!(!dispatch(
                &1,
                Some(&1),
                &displayed,
                &displayed,
                link,
                |_| panic!("not external")
            ));
        }
    }
}
