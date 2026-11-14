//! Reader-local copy identity and feedback, independent of clipboard/window APIs.
use crate::{markdown::Block, tabs::TabKey};
use std::collections::BTreeMap;

#[derive(Clone, Debug, PartialEq)]
pub struct Document {
    pub tab: Option<TabKey>,
    pub request: u64,
    pub etag: Option<String>,
    pub storage: Option<u64>,
    pub launch: Option<u64>,
}
#[derive(Default)]
pub struct State {
    pub document: Option<Document>,
    pub blocks: Vec<Block>,
    pub epoch: u64,
    serial: u64,
    pub feedback: BTreeMap<usize, u64>,
}
impl State {
    pub fn sync(&mut self, document: Option<Document>, blocks: &[Block]) -> bool {
        let blocks = if document.is_some() { blocks } else { &[] };
        if self.document == document && self.blocks == blocks {
            return false;
        }
        self.document = document;
        self.blocks = blocks.to_vec();
        self.epoch = self.epoch.wrapping_add(1);
        self.feedback.clear();
        true
    }
    pub fn valid_block(
        &self,
        epoch: u64,
        index: usize,
        current: &Option<Document>,
        blocks: &[Block],
    ) -> bool {
        epoch == self.epoch
            && self.document.is_some()
            && &self.document == current
            && self
                .blocks
                .get(index)
                .is_some_and(|block| block.language.is_some() && blocks.get(index) == Some(block))
    }
    pub fn body(
        &self,
        epoch: u64,
        index: usize,
        current: &Option<Document>,
        blocks: &[Block],
    ) -> Option<String> {
        self.valid_block(epoch, index, current, blocks)
            .then(|| blocks[index].text.clone())
    }
    /// Build the same transfer value used by the platform path without accessing a pasteboard.
    pub fn clipboard_item(
        &self,
        epoch: u64,
        index: usize,
        current: &Option<Document>,
        blocks: &[Block],
    ) -> Option<gpui::ClipboardItem> {
        self.body(epoch, index, current, blocks)
            .map(gpui::ClipboardItem::new_string)
    }
    /// Called only after the platform's write request has returned, not on hover/focus.
    pub fn submitted(&mut self, index: usize) -> u64 {
        self.serial = self.serial.wrapping_add(1);
        self.feedback.insert(index, self.serial);
        self.serial
    }
    pub fn expire(&mut self, epoch: u64, index: usize, serial: u64) -> bool {
        if epoch != self.epoch || self.feedback.get(&index) != Some(&serial) {
            return false;
        }
        self.feedback.remove(&index);
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn document() -> Option<Document> {
        Some(Document {
            tab: None,
            request: 1,
            etag: None,
            storage: None,
            launch: None,
        })
    }
    #[test]
    fn exact_body_per_block_and_stale_clicks() {
        let blocks = crate::markdown::parse(
            "```lua\n\t日本語 🦀  \n```\n\n```lua\nsecond\n```\n\n```\nplain\n```",
        );
        let doc = document();
        let mut state = State::default();
        state.sync(doc.clone(), &blocks);
        let epoch = state.epoch;
        assert_eq!(
            state.body(epoch, 0, &doc, &blocks).as_deref(),
            Some("\t日本語 🦀  \n")
        );
        assert_eq!(
            state.body(epoch, 1, &doc, &blocks).as_deref(),
            Some("second\n")
        );
        assert_eq!(state.body(epoch, 2, &doc, &blocks), None);
        assert_eq!(state.body(epoch, 99, &doc, &blocks), None);
        let mut newer = doc.clone();
        newer.as_mut().unwrap().request += 1;
        assert_eq!(state.body(epoch, 0, &newer, &blocks), None);
        let mut other_tab = doc.clone();
        other_tab.as_mut().unwrap().tab = Some(TabKey::Note {
            vault: "other".into(),
            note_id: 1,
        });
        assert_eq!(state.body(epoch, 0, &other_tab, &blocks), None);
        let mut revised = doc.clone();
        revised.as_mut().unwrap().etag = Some("new".into());
        assert_eq!(state.body(epoch, 0, &revised, &blocks), None);
        let mut restarted = doc.clone();
        restarted.as_mut().unwrap().launch = Some(2);
        assert_eq!(state.body(epoch, 0, &restarted, &blocks), None);
        let mut reindexed = doc.clone();
        reindexed.as_mut().unwrap().storage = Some(2);
        assert_eq!(state.body(epoch, 0, &reindexed, &blocks), None);
        assert_eq!(
            state.body(
                epoch,
                0,
                &doc,
                &crate::markdown::parse("```lua\nchanged\n```")
            ),
            None
        );
        assert_eq!(state.body(epoch, 0, &None, &blocks), None);
        state.sync(None, &[]);
        state.sync(doc.clone(), &blocks);
        assert_eq!(state.body(epoch, 0, &doc, &blocks), None);
    }
    #[test]
    fn unchanged_render_retains_snapshot_but_changed_body_invalidates_reveal_and_copy() {
        let blocks = crate::markdown::parse("prose\n\n```lua\na\n```\n\n```lua\nb\n```");
        let doc = document();
        let mut state = State::default();
        state.sync(doc.clone(), &blocks);
        let epoch = state.epoch;
        let allocation = state.blocks.as_ptr();
        assert!(!state.sync(doc.clone(), &blocks));
        assert_eq!(state.blocks.as_ptr(), allocation);
        assert!(state.valid_block(epoch, 2, &doc, &blocks));
        assert!(!state.valid_block(epoch, 0, &doc, &blocks));
        let mut changed = blocks.clone();
        changed[2].text = "changed".into();
        assert!(!state.valid_block(epoch, 2, &doc, &changed));
        assert!(state.sync(doc.clone(), &changed));
        assert!(!state.valid_block(epoch, 2, &doc, &changed));
        assert!(state.valid_block(state.epoch, 2, &doc, &changed));
    }
    #[test]
    fn clipboard_transfer_value_preserves_body_without_platform_access() {
        let blocks = crate::markdown::parse("```lua\n\t日本語 🦀  \n```\n\n```\nno control\n```");
        let doc = document();
        let mut state = State::default();
        state.sync(doc.clone(), &blocks);
        let item = state.clipboard_item(state.epoch, 0, &doc, &blocks).unwrap();
        assert_eq!(item.text().as_deref(), Some("\t日本語 🦀  \n"));
        assert!(state.feedback.is_empty()); // Preparing a transfer is not a write acknowledgement.
        assert!(
            state
                .clipboard_item(state.epoch, 1, &doc, &blocks)
                .is_none()
        );
        assert!(
            state
                .clipboard_item(state.epoch.wrapping_sub(1), 0, &doc, &blocks)
                .is_none()
        );
        state.sync(None, &[]);
        assert!(
            state
                .clipboard_item(state.epoch, 0, &doc, &blocks)
                .is_none()
        );
    }
    #[test]
    fn feedback_is_per_block_renewable_and_reader_scoped() {
        let blocks = crate::markdown::parse("```lua\na\n```\n```lua\nb\n```");
        let doc = document();
        let mut state = State::default();
        state.sync(doc.clone(), &blocks);
        let epoch = state.epoch;
        let first = state.submitted(0);
        let other = state.submitted(1);
        let repeat = state.submitted(0);
        assert!(!state.expire(epoch, 0, first));
        assert!(state.expire(epoch, 1, other));
        assert_eq!(state.feedback.get(&0), Some(&repeat));
        assert!(state.expire(epoch, 0, repeat));
        let late = state.submitted(0);
        state.sync(None, &[]);
        state.sync(doc, &blocks);
        assert!(!state.expire(epoch, 0, late));
        assert!(state.feedback.is_empty());
    }
}
