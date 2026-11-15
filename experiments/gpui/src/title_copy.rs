//! Exact-title clipboard submission and source-qualified transient feedback.
//! No clipboard reads, route changes, or persistence.
use crate::code_copy::Document;

pub const MAX_TITLE_BYTES: usize = 64 * 1024;
#[derive(Clone, Debug, PartialEq)]
pub struct Source {
    pub document: Document,
    pub title: String,
}
impl Source {
    pub fn new(document: Document, title: &str) -> Option<Self> {
        (!title.is_empty() && title.len() <= MAX_TITLE_BYTES).then(|| Self {
            document,
            title: title.to_owned(),
        })
    }
}
#[derive(Default)]
pub struct State {
    pub source: Option<Source>,
    pub epoch: u64,
    serial: u64,
    pub feedback: Option<u64>,
}
impl State {
    pub fn sync(&mut self, source: Option<Source>) {
        if self.source != source {
            self.source = source;
            self.epoch = self.epoch.wrapping_add(1);
            self.feedback = None;
        }
    }
    pub fn text(&self, epoch: u64, current: Option<&Source>) -> Option<String> {
        if epoch != self.epoch || self.source.as_ref() != current {
            return None;
        }
        Some(current?.title.clone())
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
    fn source(title: &str) -> Source {
        Source::new(
            Document {
                tab: Some(crate::tabs::TabKey::Note {
                    vault: "work".into(),
                    note_id: 42,
                }),
                request: 1,
                etag: Some("revision".into()),
                storage: Some(2),
                launch: Some(3),
            },
            title,
        )
        .unwrap()
    }
    #[test]
    fn exact_unicode_title_and_byte_bounds_without_truncation() {
        let title = "  日本語 🦀 [literal] # title\nsecond  ";
        let source = source(title);
        let mut state = State::default();
        state.sync(Some(source.clone()));
        assert_eq!(
            state.text(state.epoch, Some(&source)).as_deref(),
            Some(title)
        );
        assert!(Source::new(source.document.clone(), "").is_none());
        assert!(Source::new(source.document.clone(), &"a".repeat(MAX_TITLE_BYTES)).is_some());
        assert!(Source::new(source.document.clone(), &"a".repeat(MAX_TITLE_BYTES + 1)).is_none());
        assert!(Source::new(source.document, &"🦀".repeat(MAX_TITLE_BYTES / 4 + 1)).is_none());
    }
    #[test]
    fn callbacks_require_full_source_and_current_availability() {
        let source = source("Exact title");
        let mut state = State::default();
        state.sync(Some(source.clone()));
        let mut changes = vec![source.clone(); 7];
        changes[0].document.request += 1;
        changes[1].document.etag = Some("new".into());
        changes[2].document.storage = Some(4);
        changes[3].document.launch = Some(4);
        changes[4].document.tab = Some(crate::tabs::TabKey::Note {
            vault: "other".into(),
            note_id: 42,
        });
        changes[5].document.tab = Some(crate::tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 43,
        });
        changes[6].title.push('!');
        for changed in changes {
            assert_eq!(state.text(state.epoch, Some(&changed)), None);
        }
        assert_eq!(state.text(state.epoch, None), None);
        let stale_epoch = state.epoch;
        state.sync(None);
        state.sync(Some(source.clone()));
        assert_eq!(state.text(stale_epoch, Some(&source)), None);
    }
    #[test]
    fn repeated_copy_renews_feedback_and_stale_timer_cannot_clear_it() {
        let source = source("title");
        let mut state = State::default();
        state.sync(Some(source.clone()));
        let epoch = state.epoch;
        let old = state.submitted();
        let latest = state.submitted();
        assert!(!state.expire(epoch, old));
        assert_eq!(state.feedback, Some(latest));
        assert!(state.expire(epoch, latest));
        assert!(!state.expire(epoch, latest));
        let old = state.submitted();
        state.sync(None);
        state.sync(Some(source));
        let newer = state.submitted();
        assert!(!state.expire(epoch, old));
        assert_eq!(state.feedback, Some(newer));
    }
}
