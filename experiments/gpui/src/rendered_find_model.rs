//! Display-only addresses: occurrences are never resolved back to host source lines.
use crate::{
    markdown::Block,
    reader_find::{self, Hit, Matches, Target},
};
pub const MAX_TEXT_BYTES: usize = 2 * 1024 * 1024;
pub const MAX_BLOCKS: usize = 8192;
pub type Key = (usize, usize);
#[derive(Default)]
pub struct Corpus {
    pub keys: Vec<Key>,
    seen: std::collections::BTreeSet<Key>,
}
impl Corpus {
    pub fn observe(&mut self, key: Key) {
        if self.keys.len() <= MAX_BLOCKS && self.seen.insert(key) {
            self.keys.push(key);
        }
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Located {
    pub key: Key,
    pub hit: Hit,
}
#[derive(Default)]
pub struct Model {
    pub query: String,
    pub hits: Vec<Located>,
    pub active: Option<usize>,
    pub limited: bool,
    pub error: Option<String>,
    pub revision: u64,
    by_key: std::collections::BTreeMap<Key, Vec<usize>>,
}
impl Model {
    pub fn evaluate<'a>(&mut self, rows: impl IntoIterator<Item = (Key, &'a Block)>) {
        self.revision = self.revision.wrapping_add(1);
        self.hits.clear();
        self.by_key.clear();
        self.active = None;
        self.limited = false;
        self.error = None;
        let mut keys = Vec::new();
        let mut blocks = Vec::new();
        let mut bytes = 0usize;
        for (key, block) in rows {
            let size = if let Some(table) = &block.table {
                table
                    .rows
                    .iter()
                    .flatten()
                    .map(|c| c.text.len())
                    .fold(0usize, usize::saturating_add)
            } else {
                block.text.len()
            };
            if blocks.len() == MAX_BLOCKS || size > MAX_TEXT_BYTES.saturating_sub(bytes) {
                self.limited = true;
                break;
            }
            bytes += size;
            keys.push(key);
            blocks.push(block.clone());
        }
        let result = reader_find::search(&blocks, &self.query);
        self.limited |= result.truncated;
        self.error = result.error;
        self.hits = result
            .hits
            .into_iter()
            .map(|mut hit| {
                let index = match &mut hit.target {
                    Target::Block(i) | Target::Cell { block: i, .. } => {
                        let original = *i;
                        *i = 0;
                        original
                    }
                };
                Located {
                    key: keys[index],
                    hit,
                }
            })
            .collect();
        for (index, hit) in self.hits.iter().enumerate() {
            self.by_key.entry(hit.key).or_default().push(index);
        }
        self.active = (!self.hits.is_empty()).then_some(0);
    }
    pub fn step(&mut self, forward: bool) {
        self.active = reader_find::step(self.active, self.hits.len(), forward);
        self.revision = self.revision.wrapping_add(1);
    }
    pub fn slice(&self, item: usize, start: usize, len: usize) -> (Matches, Option<usize>) {
        let mut matches = Matches::default();
        let mut active = None;
        if len == 0 {
            return (matches, active);
        }
        for (_, indices) in self
            .by_key
            .range((item, start)..=(item, start.saturating_add(len - 1)))
        {
            for &index in indices {
                let located = &self.hits[index];
                if self.active == Some(index) {
                    active = Some(matches.hits.len());
                }
                let mut hit = located.hit.clone();
                match &mut hit.target {
                    Target::Block(i) | Target::Cell { block: i, .. } => *i = located.key.1 - start,
                }
                matches.hits.push(hit);
            }
        }
        (matches, active)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn repeated_occurrences_and_cells_remain_distinct() {
        let blocks = crate::markdown::parse("Hello **WORLD**\n\nnext");
        let mut m = Model {
            query: "hello world".into(),
            ..Default::default()
        };
        m.evaluate([((0, 3), &blocks[0]), ((2, 9), &blocks[0])]);
        assert_eq!(m.hits.len(), 2);
        assert_ne!(m.hits[0].key, m.hits[1].key);
        let (slice, active) = m.slice(2, 8, 2);
        assert_eq!(active, None);
        assert_eq!(slice.hits[0].target, Target::Block(1));
        m.step(true);
        assert_eq!(m.slice(2, 8, 2).1, Some(0));
        m.step(true);
        assert_eq!(m.active, Some(0));
        m.query = "world next".into();
        m.evaluate(blocks.iter().enumerate().map(|(i, b)| ((0, i), b)));
        assert!(m.hits.is_empty());
    }
    #[test]
    fn repeated_keys_and_block_budget_are_explicit() {
        let mut corpus = Corpus::default();
        corpus.observe((1, 0));
        corpus.observe((1, 0));
        assert_eq!(corpus.keys.len(), 1);
        for i in 1..MAX_BLOCKS + 4 {
            corpus.observe((1, i));
        }
        assert_eq!(corpus.keys.len(), MAX_BLOCKS + 1);
        let blocks = crate::markdown::parse("needle");
        let mut m = Model {
            query: "needle".into(),
            ..Default::default()
        };
        m.evaluate(corpus.keys.iter().map(|&key| (key, &blocks[0])));
        assert!(m.limited);
        assert_eq!(m.hits.len(), MAX_BLOCKS);
    }
    #[test]
    fn text_budget_preserves_utf8_and_marks_partial_count() {
        let small = crate::markdown::parse("日本語");
        let large = crate::markdown::parse(&"x".repeat(MAX_TEXT_BYTES));
        let mut m = Model {
            query: "日本語".into(),
            ..Default::default()
        };
        m.evaluate([((0, 0), &small[0]), ((0, 1), &large[0])]);
        assert!(m.limited);
        assert_eq!(m.hits.len(), 1);
        assert_eq!(m.hits[0].hit.range, 0..9);
    }
}
