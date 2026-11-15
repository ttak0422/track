//! Exact accepted-source correspondence for plain prose only. No clipboard/I/O.
use std::ops::Range;
use unicode_segmentation::UnicodeSegmentation;
pub const MAX_COPY_BYTES: usize = 256 * 1024;
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Segment {
    pub block: usize,
    pub source: Range<usize>,
    pub text: String,
}
impl Segment {
    /// Only newline normalization is allowed. Escapes/entities/hardbreak suffixes
    /// never acquire guessed correspondence to their rendered representation.
    pub fn exact(block: usize, source: &str, range: Range<usize>, text: &str) -> Option<Self> {
        let raw = source.get(range.clone())?;
        let raw = raw
            .strip_suffix("\r\n")
            .or_else(|| raw.strip_suffix('\n'))
            .unwrap_or(raw);
        if raw.replace("\r\n", "\n") != text || text.is_empty() {
            return None;
        }
        Some(Self {
            block,
            source: range.start..range.start + raw.len(),
            text: text.into(),
        })
    }
    fn raw_offset(&self, source: &str, byte: usize) -> Option<usize> {
        if byte > self.text.len() || !self.text.is_char_boundary(byte) {
            return None;
        }
        let raw = source.get(self.source.clone())?;
        if raw.replace("\r\n", "\n") != self.text {
            return None;
        }
        let (mut input, mut output) = (0, 0);
        while output < byte {
            if raw[input..].starts_with("\r\n") {
                input += 2;
                output += 1;
            } else {
                let n = raw[input..].chars().next()?.len_utf8();
                input += n;
                output += n;
            }
        }
        (output == byte).then_some(self.source.start + input)
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct Point {
    pub block: usize,
    pub byte: usize,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Selection {
    anchor: Point,
    caret: Point,
    valid: bool,
    dragging: bool,
}
fn segment(segments: &[Segment], block: usize) -> Option<&Segment> {
    let index = segments.binary_search_by_key(&block, |s| s.block).ok()?;
    segments.get(index)
}
fn snapped(segments: &[Segment], point: Point) -> Option<Point> {
    let text = &segment(segments, point.block)?.text;
    if point.byte > text.len() {
        return None;
    }
    let byte = if point.byte == text.len() {
        point.byte
    } else {
        text.grapheme_indices(true)
            .take_while(|(start, _)| *start <= point.byte)
            .last()?
            .0
    };
    Some(Point {
        block: point.block,
        byte,
    })
}
fn connected(segments: &[Segment], a: Point, b: Point) -> bool {
    let (start, end) = (a.block.min(b.block), a.block.max(b.block));
    let Some(first) = segments.binary_search_by_key(&start, |s| s.block).ok() else {
        return false;
    };
    let Some(last) = segments.binary_search_by_key(&end, |s| s.block).ok() else {
        return false;
    };
    // Every reader block crossed must itself be a mapped eligible paragraph.
    last - first == end - start
        && segments[first..=last].windows(2).all(|w| {
            w[0].block.checked_add(1) == Some(w[1].block) && w[0].source.end <= w[1].source.start
        })
}
impl Selection {
    pub fn begin(segments: &[Segment], point: Point) -> Option<Self> {
        let anchor = snapped(segments, point)?;
        Some(Self {
            anchor,
            caret: anchor,
            valid: true,
            dragging: true,
        })
    }
    pub fn dragging(&self) -> bool {
        self.valid && self.dragging
    }
    pub fn cancel(&mut self) {
        self.valid = false;
        self.dragging = false;
    }
    /// None represents unknown/offscreen geometry, not a nearest paragraph guess.
    pub fn update(&mut self, segments: &[Segment], point: Option<Point>, finish: bool) -> bool {
        if !self.valid || !self.dragging {
            return false;
        }
        let Some(point) = point
            .and_then(|p| snapped(segments, p))
            .filter(|p| connected(segments, self.anchor, *p))
        else {
            self.cancel();
            return false;
        };
        self.caret = point;
        self.dragging = !finish;
        true
    }
    pub fn endpoints(&self) -> Option<(Point, Point)> {
        (self.valid && self.anchor != self.caret)
            .then(|| (self.anchor.min(self.caret), self.anchor.max(self.caret)))
    }
    pub fn ranges(&self, segments: &[Segment]) -> Vec<(usize, Range<usize>)> {
        let Some((a, b)) = self
            .endpoints()
            .filter(|(a, b)| connected(segments, *a, *b))
        else {
            return vec![];
        };
        segments
            .iter()
            .filter(|s| s.block >= a.block && s.block <= b.block)
            .filter_map(|s| {
                let start = if s.block == a.block { a.byte } else { 0 };
                let end = if s.block == b.block {
                    b.byte
                } else {
                    s.text.len()
                };
                (start < end).then_some((s.block, start..end))
            })
            .collect()
    }
    pub fn plain(&self, segments: &[Segment]) -> Option<String> {
        if self.dragging {
            return None;
        }
        let ranges = self.ranges(segments);
        if ranges.is_empty() {
            return None;
        }
        let mut result = String::new();
        for (index, (block, range)) in ranges.iter().enumerate() {
            let text = segment(segments, *block)?.text.get(range.clone())?;
            let added = text.len() + if index > 0 { 2 } else { 0 };
            if result.len().checked_add(added)? > MAX_COPY_BYTES {
                return None;
            }
            if index > 0 {
                result.push_str("\n\n");
            }
            result.push_str(text);
        }
        Some(result)
    }
    /// Whole touched source lines; use end-1 so a zero-byte endpoint in the next
    /// paragraph does not copy that paragraph. Interior blank lines stay exact.
    pub fn markdown<'a>(&self, segments: &[Segment], source: &'a str) -> Option<&'a str> {
        if self.dragging {
            return None;
        }
        let (a, b) = self
            .endpoints()
            .filter(|(a, b)| connected(segments, *a, *b))?;
        // Validate all traversed mappings against these exact source bytes.
        for s in segments
            .iter()
            .filter(|s| s.block >= a.block && s.block <= b.block)
        {
            s.raw_offset(source, 0)?;
        }
        let start = segment(segments, a.block)?.raw_offset(source, a.byte)?;
        let end = segment(segments, b.block)?.raw_offset(source, b.byte)?;
        if start >= end {
            return None;
        }
        let line_start = source[..start].rfind('\n').map_or(0, |i| i + 1);
        let line_end = source.as_bytes()[end - 1..]
            .iter()
            .position(|&b| b == b'\n')
            .map_or(source.len(), |i| end + i);
        if line_end - line_start > MAX_COPY_BYTES {
            return None;
        }
        source.get(line_start..line_end)
    }
}
/// Current UI slice: one released paragraph only. It intentionally shares the
/// exact source-line primitive with the future cross-paragraph state model.
pub fn single_markdown<'a>(
    source: &'a str,
    segment: &Segment,
    range: Range<usize>,
) -> Option<&'a str> {
    if range.start >= range.end {
        return None;
    }
    let segments = std::slice::from_ref(segment);
    let mut state = Selection::begin(
        segments,
        Point {
            block: segment.block,
            byte: range.start,
        },
    )?;
    if !state.update(
        segments,
        Some(Point {
            block: segment.block,
            byte: range.end,
        }),
        true,
    ) {
        return None;
    }
    state.markdown(segments, source)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn fixture() -> (&'static str, Vec<Segment>) {
        let source = "日本語 first\r\nnext\r\n\r\n\r\nsame\r\n\r\nsame\n";
        let starts = [
            0,
            source.find("same").unwrap(),
            source.rfind("same").unwrap(),
        ];
        let ends = [
            source.find("\r\n\r\n").unwrap() + 2,
            starts[1] + 6,
            source.len(),
        ];
        (
            source,
            (0..3)
                .map(|i| {
                    Segment::exact(
                        i,
                        source,
                        starts[i]..ends[i],
                        if i == 0 {
                            "日本語 first\nnext"
                        } else {
                            "same"
                        },
                    )
                    .unwrap()
                })
                .collect(),
        )
    }
    #[test]
    fn current_single_ui_slice_returns_exact_line_and_bounds() {
        let source = "before\n\n日本語 line\r\n\nafter";
        let start = source.find("日本語").unwrap();
        let segment = Segment::exact(
            2,
            source,
            start..start + "日本語 line\r\n".len(),
            "日本語 line",
        )
        .unwrap();
        assert_eq!(
            single_markdown(source, &segment, 0..3),
            Some("日本語 line\r\n")
        );
        assert_eq!(single_markdown(source, &segment, 3..3), None);
        assert_eq!(single_markdown(source, &segment, 0..999), None);
        let huge = "x".repeat(MAX_COPY_BYTES + 1);
        let segment = Segment::exact(0, &huge, 0..huge.len(), &huge).unwrap();
        assert_eq!(single_markdown(&huge, &segment, 0..1), None);
    }

    #[test]
    fn forward_reverse_exact_lines_and_duplicates() {
        let (source, s) = fixture();
        for (a, b) in [
            (Point { block: 0, byte: 10 }, Point { block: 2, byte: 2 }),
            (Point { block: 2, byte: 2 }, Point { block: 0, byte: 10 }),
        ] {
            let mut state = Selection::begin(&s, a).unwrap();
            assert!(state.update(&s, Some(b), true));
            assert_eq!(state.markdown(&s, source), Some(source));
            assert_eq!(
                state.ranges(&s).iter().map(|(i, _)| *i).collect::<Vec<_>>(),
                vec![0, 1, 2]
            );
        }
        let mut state = Selection::begin(&s, Point { block: 1, byte: 1 }).unwrap();
        state.update(&s, Some(Point { block: 1, byte: 2 }), true);
        assert_eq!(state.markdown(&s, source), Some("same\r\n"));
    }
    #[test]
    fn end_zero_does_not_copy_next_line_and_pending_is_not_copyable() {
        let (source, s) = fixture();
        let mut state = Selection::begin(&s, Point { block: 0, byte: 0 }).unwrap();
        state.update(&s, Some(Point { block: 1, byte: 0 }), false);
        assert!(state.markdown(&s, source).is_none());
        state.update(&s, Some(Point { block: 1, byte: 0 }), true);
        assert_eq!(
            state.markdown(&s, source),
            Some(&source[..s[1].source.start])
        );
        assert_eq!(state.ranges(&s).len(), 1);
    }
    #[test]
    fn mismatch_unknown_and_noneligible_cancel() {
        let (source, mut s) = fixture();
        let mut state = Selection::begin(&s, Point { block: 0, byte: 0 }).unwrap();
        s.remove(1);
        assert!(!state.update(&s, Some(Point { block: 2, byte: 2 }), true));
        assert!(state.markdown(&s, source).is_none());
        let (_, s) = fixture();
        let mut state = Selection::begin(&s, Point { block: 0, byte: 0 }).unwrap();
        assert!(!state.update(&s, None, true));
        assert!(state.plain(&s).is_none());
        assert!(Segment::exact(0, "a\\*b", 0..4, "a*b").is_none());
        assert!(Segment::exact(0, "&amp;", 0..5, "&").is_none());
        assert!(Segment::exact(0, "a  \nb", 0..5, "a\nb").is_none());
    }
    #[test]
    fn grapheme_and_source_staleness() {
        let source = "e\u{301}🦀\n";
        let s = vec![Segment::exact(0, source, 0..source.len(), "e\u{301}🦀").unwrap()];
        let mut state = Selection::begin(&s, Point { block: 0, byte: 1 }).unwrap();
        state.update(&s, Some(Point { block: 0, byte: 7 }), true);
        assert_eq!(state.plain(&s).as_deref(), Some("e\u{301}🦀"));
        assert_eq!(state.markdown(&s, source), Some(source));
        assert!(state.markdown(&s, "x\u{301}🦀\n").is_none());
    }
}
