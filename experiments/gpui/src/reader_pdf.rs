//! Literal local attachment admission. Unsupported syntax stays in the reader.
use crate::markdown::{Block, BlockKind};
pub fn asset_name(reference: &str) -> Result<&str, String> {
    let invalid = || "Only literal assets/ PDF references are supported.".to_owned();
    let name = reference.strip_prefix("assets/").ok_or_else(invalid)?;
    if reference.len() > 4096
        || name.is_empty()
        || name
            .chars()
            .any(|c| c.is_control() || matches!(c, '\\' | '%' | '?' | '#' | ':'))
        || name
            .split('/')
            .any(|p| p.is_empty() || matches!(p, "." | "..") || p.trim() != p)
        || !name
            .rsplit_once('.')
            .is_some_and(|(_, ext)| ext.eq_ignore_ascii_case("pdf"))
    {
        return Err(invalid());
    }
    Ok(name)
}
pub fn references(blocks: &[Block]) -> Vec<(usize, String)> {
    blocks
        .iter()
        .enumerate()
        .filter_map(|(i, b)| {
            if b.kind != BlockKind::Paragraph || b.quoted || b.indent != 0 || b.marker.is_some() {
                return None;
            }
            let r = b.image.as_ref()?;
            asset_name(r).ok()?;
            Some((i, r.clone()))
        })
        .take(8)
        .collect()
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn literal_source_only() {
        assert_eq!(asset_name("assets/日本語/a.PDF").unwrap(), "日本語/a.PDF");
        for r in [
            "https://a/a.pdf",
            "file:///a.pdf",
            "assets/../a.pdf",
            "assets/a.pdf#page=2",
            "assets/%2e/a.pdf",
            "assets/a.png",
            "assets//a.pdf",
            "assets/ a.pdf",
        ] {
            assert!(asset_name(r).is_err(), "{r}");
        }
    }
    #[test]
    fn standalone_only() {
        let blocks = crate::markdown::parse_reader(
            "![doc](assets/a.pdf)\n\nprefix ![inline](assets/b.pdf)\n\n```md\n![literal](assets/c.pdf)\n```\n\n![remote](https://example.com/a.pdf)",
        );
        assert_eq!(references(&blocks), vec![(0, "assets/a.pdf".into())]);
    }
}
