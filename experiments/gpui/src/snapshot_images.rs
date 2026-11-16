//! Memory-only snapshot raster preparation, performed on the parse worker.
use crate::{markdown, raster, snapshot_links};
use std::{
    collections::BTreeMap,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
};

const MAX_OCCURRENCES: usize = 16;
const MAX_UNIQUE: usize = 8;
const MAX_RETAINED: usize = 16 * 1024 * 1024;
static DECODING: AtomicBool = AtomicBool::new(false);
struct Permit;
impl Permit {
    fn acquire() -> Option<Self> {
        DECODING
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        DECODING.store(false, Ordering::Release);
    }
}
#[derive(Default)]
pub(crate) struct Prepared {
    pub images: BTreeMap<usize, raster::Decoded>,
    pub status: String,
}
fn copy(image: &raster::Decoded) -> raster::Decoded {
    raster::Decoded {
        image: image.image.clone(),
        width: image.width,
        height: image.height,
    }
}
pub(crate) fn prepare(
    files: &BTreeMap<String, Arc<[u8]>>,
    current: &str,
    blocks: &[markdown::Block],
    cancel: &AtomicBool,
) -> Prepared {
    let references: Vec<_> = blocks
        .iter()
        .enumerate()
        .filter_map(|(i, b)| b.image.as_ref().map(|s| (i, s)))
        .collect();
    if references.is_empty() {
        return Prepared::default();
    }
    let Some(_permit) = Permit::acquire() else {
        return Prepared {
            status:
                "Images unavailable: another decode is finishing. Select this file again to retry."
                    .into(),
            ..Default::default()
        };
    };
    prepare_inner(files, current, &references, cancel)
}
fn prepare_inner(
    files: &BTreeMap<String, Arc<[u8]>>,
    current: &str,
    references: &[(usize, &String)],
    cancel: &AtomicBool,
) -> Prepared {
    let mut out = Prepared::default();
    let mut cache: BTreeMap<String, Option<raster::Decoded>> = BTreeMap::new();
    let mut retained = 0usize;
    let mut unavailable = references.len().saturating_sub(MAX_OCCURRENCES);
    for (index, reference) in references.iter().take(MAX_OCCURRENCES) {
        if cancel.load(Ordering::Acquire) {
            return Prepared::default();
        }
        let Some(path) =
            snapshot_links::resolve_extension(current, reference, &["png", "jpg", "jpeg"])
        else {
            unavailable += 1;
            continue;
        };
        if !cache.contains_key(&path) {
            if cache.len() == MAX_UNIQUE {
                unavailable += 1;
                continue;
            }
            let decoded = files.get(&path).and_then(|bytes| {
                let mime = if path
                    .rsplit_once('.')
                    .is_some_and(|(_, e)| e.eq_ignore_ascii_case("png"))
                {
                    "image/png"
                } else {
                    "image/jpeg"
                };
                raster::decode(bytes, mime)
                    .ok()
                    .filter(|image| image.byte_len() <= MAX_RETAINED.saturating_sub(retained))
            });
            if let Some(image) = &decoded {
                retained += image.byte_len();
            }
            cache.insert(path.clone(), decoded);
        }
        if let Some(Some(image)) = cache.get(&path) {
            out.images.insert(*index, copy(image));
        } else {
            unavailable += 1;
        }
    }
    if cancel.load(Ordering::Acquire) {
        return Prepared::default();
    }
    out.status = format!(
        "{} image occurrences displayed; {} unavailable or over limits. Original image text remains. Select the file again to retry.",
        out.images.len(),
        unavailable
    );
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn worker_permit_is_retained_until_drop() {
        let first = Permit::acquire().unwrap();
        assert!(Permit::acquire().is_none());
        drop(first);
        assert!(Permit::acquire().is_some());
    }
    #[test]
    fn exact_paths_duplicates_and_cancel() {
        let mut files = BTreeMap::new();
        files.insert("img/a.png".into(), Arc::from(raster::tests::png(2, 3)));
        let a = "../img/a.png".to_owned();
        let refs = vec![(2, &a), (5, &a)];
        let result = prepare_inner(&files, "docs/a.md", &refs, &AtomicBool::new(false));
        assert_eq!(result.images.len(), 2);
        assert!(Arc::ptr_eq(
            &result.images[&2].image,
            &result.images[&5].image
        ));
        assert!(
            prepare_inner(&files, "docs/a.md", &refs, &AtomicBool::new(true))
                .images
                .is_empty()
        );
        for bad in [
            "../../img/a.png",
            "https://x/a.png",
            "data:image/png,x",
            "../img/missing.png",
            "../img/a.svg",
            "../img/a.png#x",
            "%GG.png",
            "C:/a.png",
        ] {
            let bad = bad.to_owned();
            assert!(
                prepare_inner(&files, "docs/a.md", &[(1, &bad)], &AtomicBool::new(false))
                    .images
                    .is_empty(),
                "{bad}"
            );
        }
    }
    #[test]
    fn occurrence_unique_memory_and_corrupt_limits() {
        let mut files = BTreeMap::new();
        for i in 0..9 {
            files.insert(format!("{i}.png"), Arc::from(raster::tests::png(2, 2)));
        }
        let names: Vec<String> = (0..9).map(|i| format!("{i}.png")).collect();
        let refs: Vec<_> = names.iter().enumerate().collect();
        assert_eq!(
            prepare_inner(&files, "a.md", &refs, &AtomicBool::new(false))
                .images
                .len(),
            8
        );
        let many: Vec<_> = (0..20).map(|i| (i, &names[0])).collect();
        assert_eq!(
            prepare_inner(&files, "a.md", &many, &AtomicBool::new(false))
                .images
                .len(),
            16
        );
        files.insert("0.png".into(), Arc::from(b"corrupt".as_slice()));
        assert!(
            prepare_inner(&files, "a.md", &[(0, &names[0])], &AtomicBool::new(false))
                .images
                .is_empty()
        );
        let big = raster::tests::png(2000, 2000);
        files.insert("0.png".into(), Arc::from(big.clone()));
        files.insert("1.png".into(), Arc::from(big));
        assert_eq!(
            prepare_inner(&files, "a.md", &refs[..2], &AtomicBool::new(false))
                .images
                .len(),
            1
        );
    }
}
