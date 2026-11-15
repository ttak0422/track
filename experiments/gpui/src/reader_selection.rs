//! A bounded, one-paragraph plain-text selection. Source and geometry ownership
//! belong to the reader; this model never reads or writes a clipboard.
use crate::markdown::{Block, BlockKind, InlineStyle};
use std::ops::Range;
use unicode_segmentation::UnicodeSegmentation;

pub const MAX_BYTES: usize = 256 * 1024;

/// Deliberately narrower than prose keyboard focus: styled or interactive
/// paragraphs remain entirely outside the first selection subset.
pub fn eligible(block: &Block) -> bool {
    block.kind == BlockKind::Paragraph
        && block.links.is_empty()
        && block.table.is_none()
        && block.image.is_none()
        && block.language.is_none()
        && block.fence_info.is_none()
        && block.marker.is_none()
        && !block.quoted
        && block.indent == 0
        && block.syntax.is_empty()
        && block
            .spans
            .iter()
            .all(|span| span.style == InlineStyle::default())
}

/// GPUI supplies byte positions. A position inside a scalar or extended
/// grapheme is floored to that grapheme's start, never sliced mid-cluster.
/// Out-of-block positions are rejected rather than silently selecting more text.
pub fn snap_boundary(text: &str, byte: usize) -> Option<usize> {
    if byte > text.len() {
        return None;
    }
    if byte == text.len() {
        return Some(byte);
    }
    text.grapheme_indices(true)
        .take_while(|(start, _)| *start <= byte)
        .last()
        .map(|(start, _)| start)
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CopyError {
    Empty,
    Dragging,
    InvalidRange,
    TooLarge,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct State {
    anchor: usize,
    caret: usize,
    valid: bool,
    dragging: bool,
}
impl State {
    pub fn begin(text: &str, byte: usize) -> Option<Self> {
        let anchor = snap_boundary(text, byte)?;
        Some(Self {
            anchor,
            caret: anchor,
            valid: true,
            dragging: true,
        })
    }

    /// `None` means the hit was outside this exact paragraph. Cancel rather than
    /// capture another block, offscreen content, or a nearest-index guess.
    pub fn update(&mut self, text: &str, byte: Option<usize>) -> bool {
        if !self.valid || !self.dragging {
            return false;
        }
        let Some(caret) = byte.and_then(|byte| snap_boundary(text, byte)) else {
            self.cancel();
            return false;
        };
        self.caret = caret;
        true
    }

    pub fn finish(&mut self, text: &str, byte: Option<usize>) -> bool {
        if !self.update(text, byte) {
            return false;
        }
        self.dragging = false;
        true
    }

    /// Hover state may briefly report false when the same row is repainted.
    /// Only a measured hit outside this paragraph cancels an ongoing drag.
    /// A valid hit preserves BOTH endpoints; it is not a synthetic pointer move.
    pub fn leave(&mut self, text: &str, byte: Option<usize>) -> bool {
        if !self.dragging || byte.and_then(|byte| snap_boundary(text, byte)).is_some() {
            return false;
        }
        self.cancel();
        true
    }

    pub fn cancel(&mut self) {
        self.valid = false;
        self.dragging = false;
    }

    pub fn dragging(&self) -> bool {
        self.dragging
    }

    /// A paintable range may exist during dragging. Copy additionally requires
    /// release; an empty click/drag is never a clipboard payload.
    pub fn range(&self) -> Option<Range<usize>> {
        (self.valid && self.anchor != self.caret)
            .then(|| self.anchor.min(self.caret)..self.anchor.max(self.caret))
    }

    pub fn payload<'a>(&self, text: &'a str) -> Result<&'a str, CopyError> {
        if self.dragging {
            return Err(CopyError::Dragging);
        }
        let range = self.range().ok_or(CopyError::Empty)?;
        if snap_boundary(text, range.start) != Some(range.start)
            || snap_boundary(text, range.end) != Some(range.end)
        {
            return Err(CopyError::InvalidRange);
        }
        let selected = text.get(range).ok_or(CopyError::InvalidRange)?;
        if selected.len() > MAX_BYTES {
            return Err(CopyError::TooLarge);
        }
        Ok(selected)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::markdown;

    #[test]
    fn repaint_hover_false_with_measured_inside_pointer_preserves_drag() {
        let text = "日本語 wrapped paragraph";
        let mut selection = State::begin(text, 0).unwrap();
        assert!(selection.update(text, Some(9)));
        let before = selection.clone();
        assert!(!selection.leave(text, Some(3)));
        assert_eq!(selection, before);
        assert!(selection.dragging());
        assert!(selection.leave(text, None));
        assert!(!selection.dragging());
        assert_eq!(selection.range(), None);
        assert!(!selection.leave(text, Some(3)));
        let mut selection = State::begin(text, 0).unwrap();
        assert!(selection.leave(text, Some(text.len() + 1)));
    }

    #[test]
    fn only_unstyled_plain_paragraph_is_selectable() {
        assert!(eligible(&markdown::parse("Plain 日本語 paragraph.")[0]));
        for source in [
            "# Heading",
            "- item",
            "1. item",
            "- [ ] task",
            "> quote",
            "*emphasis*",
            "**strong**",
            "~~strike~~",
            "`code`",
            "[[Note]]",
            "[link](https://example.com)",
            "![image](local.png)",
            "```\ncode\n```",
            "[^n]: footnote",
        ] {
            let blocks = markdown::parse(source);
            assert!(!blocks.is_empty(), "{source}");
            assert!(blocks.iter().all(|block| !eligible(block)), "{source}");
        }
        let table = markdown::parse("| A | B |\n| - | - |\n| C | D |");
        assert!(table.iter().all(|block| !eligible(block)));
    }

    #[test]
    fn every_nondefault_inline_style_excludes_entire_paragraph() {
        for field in 0..6 {
            let mut block = markdown::parse("plain").remove(0);
            let mut style = InlineStyle::default();
            match field {
                0 => style.emphasis = true,
                1 => style.strong = true,
                2 => style.strike = true,
                3 => style.code = true,
                4 => style.link = true,
                _ => style.unresolved = true,
            }
            block.spans.push(markdown::Span { range: 0..1, style });
            assert!(!eligible(&block));
        }
    }

    #[test]
    fn byte_positions_snap_to_extended_graphemes() {
        let text = "日e\u{301}👩‍💻🇯🇵Z";
        for (start, grapheme) in text.grapheme_indices(true) {
            for byte in start..start + grapheme.len() {
                assert_eq!(snap_boundary(text, byte), Some(start));
            }
        }
        assert_eq!(snap_boundary(text, text.len()), Some(text.len()));
        assert_eq!(snap_boundary(text, text.len() + 1), None);
        assert_eq!(snap_boundary("", 0), Some(0));
    }

    #[test]
    fn forward_and_reverse_ranges_copy_same_exact_unicode() {
        let text = "A日本e\u{301}👩‍💻Z";
        let end = text.len() - 1;
        let mut forward = State::begin(text, 1).unwrap();
        assert!(forward.update(text, Some(end)));
        assert_eq!(forward.payload(text), Err(CopyError::Dragging));
        assert!(forward.finish(text, Some(end)));
        let mut reverse = State::begin(text, end).unwrap();
        assert!(reverse.finish(text, Some(1)));
        assert_eq!(forward.range(), reverse.range());
        assert_eq!(forward.payload(text), Ok("日本e\u{301}👩‍💻"));
        assert_eq!(reverse.payload(text), forward.payload(text));
    }

    #[test]
    fn empty_outside_invalid_and_cancelled_gestures_never_copy() {
        let mut click = State::begin("abc", 1).unwrap();
        assert!(click.finish("abc", Some(1)));
        assert_eq!(click.payload("abc"), Err(CopyError::Empty));
        assert!(State::begin("abc", 4).is_none());
        for end in [None, Some(4), Some(usize::MAX)] {
            let mut state = State::begin("abc", 0).unwrap();
            assert!(state.update("abc", Some(2)));
            assert!(!state.finish("abc", end));
            assert!(!state.dragging());
            assert!(state.range().is_none());
            assert_eq!(state.payload("abc"), Err(CopyError::Empty));
            assert!(!state.update("abc", Some(3)));
        }
    }

    #[test]
    fn payload_refuses_oversize_without_truncation_and_rechecks_boundaries() {
        let text = "x".repeat(MAX_BYTES + 1);
        let mut state = State::begin(&text, 0).unwrap();
        assert!(state.finish(&text, Some(text.len())));
        assert_eq!(state.payload(&text), Err(CopyError::TooLarge));
        let mut bounded = State::begin(&text, 0).unwrap();
        assert!(bounded.finish(&text, Some(MAX_BYTES)));
        assert_eq!(bounded.payload(&text).unwrap().len(), MAX_BYTES);
        let mut replaced = State::begin("ab", 0).unwrap();
        assert!(replaced.finish("ab", Some(1)));
        assert_eq!(replaced.payload("日"), Err(CopyError::InvalidRange));
    }

    #[test]
    fn displayed_line_breaks_are_preserved_without_soft_wrap_invention() {
        let text = "first line\n第二行\r\nlast";
        let mut state = State::begin(text, 0).unwrap();
        assert!(state.finish(text, Some(text.len())));
        assert_eq!(state.payload(text), Ok(text));
        state.cancel();
        assert_eq!(state.payload(text), Err(CopyError::Empty));
    }
}
