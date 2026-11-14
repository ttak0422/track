//! Current-reader raster assets only. No URL loader, disk cache, or note mutation.
use super::*;
use std::{
    collections::BTreeMap,
    sync::atomic::{AtomicBool, Ordering},
};
static IMAGE_JOB: AtomicBool = AtomicBool::new(false);
pub(crate) struct Permit;
impl Permit {
    pub(crate) fn acquire() -> Option<Self> {
        IMAGE_JOB
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        IMAGE_JOB.store(false, Ordering::Release);
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    request: u64,
    launch: u64,
    epoch: u64,
    key: live::NoteKey,
    etag: String,
}
#[derive(Default)]
pub(crate) struct State {
    identity: Option<Identity>,
    pub(crate) images: BTreeMap<usize, raster::Decoded>,
    pub(crate) notice: Option<String>,
}
impl Workspace {
    fn image_identity(&self) -> Option<Identity> {
        if self.shell.home
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.editing_ui.active()
        {
            return None;
        }
        let session = self.live.as_ref()?;
        let note = self.current_note.as_ref()?;
        let key = self.current_note_request.as_ref()?;
        let active = self.tabs.active()?;
        if active.key
            != (tabs::TabKey::Note {
                vault: key.vault.clone(),
                note_id: key.note_id,
            })
        {
            return None;
        }
        Some(Identity {
            request: self.request,
            launch: session.client().launch_generation,
            epoch: self.accepted_storage_generation?,
            key: key.clone(),
            etag: note.etag.clone(),
        })
    }
    pub(crate) fn ensure_reader_images(&mut self, cx: &mut Context<Self>) {
        let Some(identity) = self.image_identity() else {
            self.reader_images = State::default();
            return;
        };
        if self.reader_images.identity.as_ref() == Some(&identity) {
            return;
        }
        self.reader_images = State {
            identity: Some(identity.clone()),
            ..Default::default()
        };
        let references = markdown::standalone_images(&self.content);
        if references.is_empty() {
            return;
        }
        let Some(permit) = Permit::acquire() else {
            self.reader_images.notice =
                Some("Raster loading is busy; Reload after the earlier read finishes.".into());
            return;
        };
        self.reader_images.notice = Some("Loading up to four local raster images…".into());
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|_| ())?;
            if before.phase != "ready" || before.generation != captured.epoch {
                return Err(());
            }
            let mut images = BTreeMap::new();
            let mut failed = false;
            for (index, reference) in references {
                match client.raster_asset(&reference, &captured.key.vault) {
                    Ok(image) => {
                        images.insert(index, image);
                    }
                    Err(_) => failed = true,
                }
            }
            let after = client.storage().map_err(|_| ())?;
            if after.phase != "ready" || after.generation != captured.epoch {
                return Err(());
            }
            Ok((images, failed))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.image_identity().as_ref() != Some(&identity) || this.reader_images.identity.as_ref() != Some(&identity) { return; }
                match result {
                    Ok((images, failed)) => {
                        this.reader_images.images = images;
                        this.reader_images.notice = Some(if failed { "Some raster images could not be loaded; source captions remain. Reload to retry." } else { "Up to four local PNG/JPEG images shown; other image syntax remains literal." }.into());
                    }
                    Err(()) => this.reader_images.notice = Some("Raster images were not accepted after a storage change or read failure. Reload to retry.".into()),
                }
                cx.notify();
            });
        }).detach();
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn raster_identity_is_request_source_and_revision_specific() {
        let base = Identity {
            request: 1,
            launch: 2,
            epoch: 3,
            key: live::NoteKey {
                vault: "work".into(),
                note_id: 42,
            },
            etag: "rev".into(),
        };
        assert_eq!(base, base.clone());
        let mut other = base.clone();
        other.request += 1;
        assert_ne!(base, other);
        let mut other = base.clone();
        other.launch += 1;
        assert_ne!(base, other);
        let mut other = base.clone();
        other.epoch += 1;
        assert_ne!(base, other);
        let mut other = base.clone();
        other.key.vault = "other".into();
        assert_ne!(base, other);
        let mut other = base.clone();
        other.etag = "new".into();
        assert_ne!(base, other);
    }
    #[test]
    fn only_one_bounded_image_job_can_run_and_drop_releases_it() {
        let first = Permit::acquire().unwrap();
        assert!(Permit::acquire().is_none());
        drop(first);
        assert!(Permit::acquire().is_some());
    }
}
