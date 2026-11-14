//! Owned, explicit-preview-only thumbnail work. No render-frame scheduling.
use super::*;
use std::{
    collections::{BTreeMap, BTreeSet},
    sync::{Arc, atomic::AtomicBool},
};
type Asset = (String, String);
type Card = (usize, usize, usize, usize);
#[derive(Default)]
pub(super) struct Gallery {
    pub bindings: BTreeMap<Card, Result<Asset, String>>,
    pub images: BTreeMap<Asset, Result<raster::Decoded, String>>,
    attempted: BTreeSet<Asset>,
    touched: BTreeMap<Asset, u64>,
    cancelled: Arc<AtomicBool>,
    generation: u64,
}
impl Drop for Gallery {
    fn drop(&mut self) {
        self.cancelled.store(true, Ordering::Release);
    }
}
impl Gallery {
    pub fn cancel(&mut self) {
        self.cancelled.store(true, Ordering::Release);
        self.generation = self.generation.wrapping_add(1);
        self.images.clear();
        self.touched.clear();
        for binding in self.bindings.values_mut() {
            *binding = Err("Cover unavailable: preview changed".into());
        }
    }
    fn prepare(&mut self, bindings: BTreeMap<Card, Result<Asset, String>>) -> Vec<Asset> {
        self.cancelled.store(true, Ordering::Release);
        self.generation = self.generation.wrapping_add(1);
        self.cancelled = Arc::new(AtomicBool::new(false));
        self.bindings = bindings;
        let mut accepted = BTreeSet::new();
        for binding in self.bindings.values_mut() {
            if let Ok(asset) = binding {
                if accepted.len() == 32 && !accepted.contains(asset) {
                    *binding = Err("Cover unavailable: queue limit".into());
                } else {
                    accepted.insert(asset.clone());
                }
            }
        }
        let visible: BTreeSet<_> = self
            .bindings
            .values()
            .filter_map(|v| v.as_ref().ok().cloned())
            .collect();
        let mut pending = Vec::new();
        for asset in &visible {
            self.touched.insert(asset.clone(), self.generation);
            if self.images.contains_key(asset) {
                continue;
            }
            if !self.attempted.insert(asset.clone()) {
                self.images.insert(
                    asset.clone(),
                    Err(
                        "Cover unavailable: already attempted; refresh rendered content to retry"
                            .into(),
                    ),
                );
            } else {
                pending.push(asset.clone());
            }
        }
        while self.images.len() + pending.len() > 32 {
            let oldest = self
                .images
                .keys()
                .filter(|asset| !visible.contains(*asset))
                .min_by_key(|asset| self.touched.get(*asset).copied().unwrap_or(0))
                .cloned();
            let Some(oldest) = oldest else {
                break;
            };
            self.images.remove(&oldest);
            self.touched.remove(&oldest);
        }
        self.touched
            .retain(|asset, _| self.images.contains_key(asset) || pending.contains(asset));
        pending
    }
    fn publish(
        &mut self,
        generation: u64,
        results: BTreeMap<Asset, Result<raster::Decoded, String>>,
    ) -> bool {
        if generation != self.generation
            || self.cancelled.load(Ordering::Acquire)
            || results.keys().any(|asset| {
                !self
                    .bindings
                    .values()
                    .any(|binding| binding.as_ref().ok() == Some(asset))
            })
        {
            return false;
        }
        let bytes: usize = results
            .values()
            .chain(self.images.values())
            .filter_map(|v| v.as_ref().ok())
            .map(raster::Decoded::byte_len)
            .sum();
        if results.len() + self.images.len() > 32 || bytes > 8 * 1024 * 1024 {
            self.cancel();
            return false;
        }
        self.images.extend(results);
        true
    }
    pub fn face(&self, card: Card) -> Result<Option<&raster::Decoded>, &str> {
        match self.bindings.get(&card) {
            Some(Err(reason)) => Err(reason),
            Some(Ok(asset)) => match self.images.get(asset) {
                Some(Ok(image)) => Ok(Some(image)),
                Some(Err(reason)) => Err(reason),
                None => Err("Loading cover…"),
            },
            None => Ok(None),
        }
    }
}
impl Workspace {
    pub(crate) fn invalidate_gallery(&mut self) {
        if let Some(state) = &mut self.shell.includes {
            state.gallery.cancel();
        }
    }
    pub(super) fn load_gallery(&mut self, cx: &mut Context<Self>) {
        let Some(identity) = self.includes_identity() else {
            self.invalidate_gallery();
            return;
        };
        let Some(state) = &self.shell.includes else {
            return;
        };
        if !state.open || !state.preview_open || state.identity != identity {
            return;
        }
        let Some(Ok(snapshot)) = &state.result else {
            return;
        };
        let Ok(preview) = &snapshot.preview else {
            return;
        };
        let mut bindings = BTreeMap::new();
        for (&(item, block), query) in &snapshot.queries {
            let Ok(view) = query else {
                continue;
            };
            if view.layout != crate::query_view::Layout::Gallery {
                continue;
            }
            let origin = match &preview.items[item] {
                markdown::PreviewItem::Host(_) => Some(identity.source.note.vault.as_str()),
                markdown::PreviewItem::Include { index, .. } => {
                    let entry = &snapshot.response.entries[*index];
                    entry.explicit_origin.then_some(entry.vault.as_str())
                }
            }
            .and_then(|scope| self.gallery_scope(scope));
            let page = state
                .query_pages
                .get(&(item, block, None))
                .copied()
                .unwrap_or(0);
            for (group, row_index, row) in view.gallery_page(page) {
                let Some(reference) = row.cover.as_deref().filter(|s| !s.is_empty()) else {
                    continue;
                };
                let asset = origin
                    .as_deref()
                    .ok_or_else(|| "Cover unavailable: unverified origin".to_string())
                    .and_then(|scope| {
                        raster::asset_name(reference)
                            .map(|_| (scope.to_owned(), reference.to_owned()))
                    });
                bindings.insert((item, block, group, row_index), asset);
            }
        }
        let state = self.shell.includes.as_mut().unwrap();
        let pending = state.gallery.prepare(bindings);
        if pending.is_empty() {
            return;
        }
        let Some(permit) = crate::workspace_images::Permit::acquire() else {
            for asset in pending {
                state.gallery.images.insert(
                    asset,
                    Err("Cover unavailable: image worker busy; refresh to retry".into()),
                );
            }
            return;
        };
        let serial = state.serial;
        let epoch = state.preview_epoch;
        let generation = state.gallery.generation;
        let cancelled = state.gallery.cancelled.clone();
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let mut results = BTreeMap::new();
            for asset in pending {
                if cancelled.load(Ordering::Acquire) {
                    break;
                }
                let result = (|| {
                    let before = client
                        .storage()
                        .map_err(|_| "Cover unavailable: storage read failed")?;
                    if before.phase != "ready"
                        || before.generation != captured.source.storage_generation
                    {
                        return Err("Cover unavailable: storage changed");
                    }
                    if cancelled.load(Ordering::Acquire) {
                        return Err("Cover unavailable: cancelled");
                    }
                    let image = client
                        .raster_thumbnail(&asset.1, &asset.0)
                        .map_err(|_| "Cover unavailable: local PNG/JPEG read failed")?;
                    let after = client
                        .storage()
                        .map_err(|_| "Cover unavailable: storage read failed")?;
                    if after.phase != "ready"
                        || after.generation != captured.source.storage_generation
                    {
                        return Err("Cover unavailable: storage changed");
                    }
                    Ok(image)
                })()
                .map_err(str::to_owned);
                if cancelled.load(Ordering::Acquire) {
                    break;
                }
                results.insert(asset, result);
            }
            results
        });
        cx.spawn(async move |this, cx| {
            let results = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.includes_identity().as_ref() != Some(&identity) {
                    return;
                }
                let valid = results
                    .keys()
                    .all(|(scope, _)| this.gallery_scope_verified(scope));
                let Some(state) = &mut this.shell.includes else {
                    return;
                };
                if !valid
                    || !state.accepts(&identity, serial)
                    || !state.preview_open
                    || state.preview_epoch != epoch
                    || state.gallery.generation != generation
                {
                    return;
                }
                state.gallery.publish(generation, results);
                cx.notify();
            });
        })
        .detach();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn bindings(count: usize, scope: &str) -> BTreeMap<Card, Result<Asset, String>> {
        (0..count)
            .map(|i| ((0, 0, 0, i), Ok((scope.into(), format!("assets/{i}.png")))))
            .collect()
    }
    #[test]
    fn queue_deduplicates_scope_and_bounds_unique_visible_work() {
        let mut gallery = Gallery::default();
        let mut cards = bindings(40, "a");
        cards.insert((1, 0, 0, 0), Ok(("a".into(), "assets/0.png".into())));
        assert_eq!(gallery.prepare(cards).len(), 32);
        assert_eq!(gallery.bindings.values().filter(|v| v.is_err()).count(), 8);
        assert_eq!(gallery.images.len(), 0);
        assert_eq!(gallery.prepare(bindings(1, "b")).len(), 1);
        assert!(gallery.prepare(bindings(1, "a")).is_empty());
        assert!(
            gallery
                .face((0, 0, 0, 0))
                .err()
                .unwrap()
                .contains("already attempted")
        );
    }
    #[test]
    fn page_change_cancels_work_retains_bounded_lru_and_close_drops_handles() {
        let mut gallery = Gallery::default();
        gallery.prepare(bindings(2, "a"));
        let old = gallery.cancelled.clone();
        let generation = gallery.generation;
        let image = raster::decode_thumbnail(&raster::tests::png(1, 1), "image/png").unwrap();
        let handle = Arc::downgrade(&image.image);
        gallery
            .images
            .insert(("a".into(), "assets/1.png".into()), Ok(image));
        let visible = raster::decode_thumbnail(&raster::tests::png(1, 1), "image/png").unwrap();
        let visible_handle = Arc::downgrade(&visible.image);
        gallery
            .images
            .insert(("a".into(), "assets/0.png".into()), Ok(visible));
        assert!(gallery.prepare(bindings(1, "a")).is_empty());
        assert!(old.load(Ordering::Acquire));
        assert_ne!(generation, gallery.generation);
        assert!(handle.upgrade().is_some());
        assert!(visible_handle.upgrade().is_some());
        assert!(gallery.face((0, 0, 0, 0)).unwrap().is_some());
        gallery.cancel();
        assert!(handle.upgrade().is_none());
        assert!(visible_handle.upgrade().is_none());
        assert!(gallery.face((0, 0, 0, 0)).is_err());
        assert!(gallery.face((9, 9, 9, 9)).unwrap().is_none());
    }
    #[test]
    fn back_page_hits_lru_and_eviction_never_retries_or_crosses_scope() {
        let mut gallery = Gallery::default();
        gallery.prepare(bindings(32, "a"));
        let results = (0..32)
            .map(|i| {
                (
                    ("a".into(), format!("assets/{i}.png")),
                    Ok(raster::decode_thumbnail(&raster::tests::png(1, 1), "image/png").unwrap()),
                )
            })
            .collect();
        assert!(gallery.publish(gallery.generation, results));
        assert!(gallery.prepare(bindings(4, "a")).is_empty());
        assert!(gallery.face((0, 0, 0, 0)).unwrap().is_some());
        let old_generation = gallery.generation;
        assert_eq!(gallery.prepare(bindings(4, "b")).len(), 4);
        assert_eq!(gallery.images.len(), 28);
        assert!(
            gallery
                .images
                .contains_key(&("a".into(), "assets/0.png".into()))
        );
        assert!(!gallery.publish(old_generation, BTreeMap::new()));
        gallery.prepare(bindings(32, "b"));
        assert!(gallery.images.len() <= 4);
        assert!(gallery.prepare(bindings(1, "a")).is_empty());
        assert!(gallery.face((0, 0, 0, 0)).is_err());
        gallery.cancel();
        assert!(!gallery.publish(gallery.generation, BTreeMap::new()));
    }
    #[test]
    fn drop_cancels_worker_and_a_fresh_snapshot_can_retry() {
        let token;
        {
            let mut gallery = Gallery::default();
            gallery.prepare(bindings(1, "a"));
            token = gallery.cancelled.clone();
        }
        assert!(token.load(Ordering::Acquire));
        assert_eq!(Gallery::default().prepare(bindings(1, "a")).len(), 1);
    }
}
