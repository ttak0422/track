//! Explicit rendered-preview-only inline resolution. No scheduling during paint.
use super::*;
use std::{collections::BTreeMap, sync::Arc, time::Duration};
type Key = (usize, usize);
type Asset = (String, String); // verified scope and exact admitted bytes
static BUSY: AtomicBool = AtomicBool::new(false);
struct Worker;
impl Worker {
    fn acquire() -> Option<Self> {
        BUSY.compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Worker {
    fn drop(&mut self) {
        BUSY.store(false, Ordering::Release);
    }
}
#[derive(Default)]
pub(super) struct Resolved {
    started: bool,
    cancelled: Arc<AtomicBool>,
    bindings: BTreeMap<Key, Result<Asset, String>>,
    results: BTreeMap<Asset, Result<Arc<crate::numeric_chart::Chart>, String>>,
}
impl Drop for Resolved {
    fn drop(&mut self) {
        self.cancelled.store(true, Ordering::Release);
    }
}
impl Resolved {
    pub fn cancel(&mut self) {
        self.cancelled.store(true, Ordering::Release);
    }
    fn prepare(&mut self, bindings: BTreeMap<Key, Result<Asset, String>>) -> Vec<Asset> {
        if self.started {
            return vec![];
        }
        self.started = true;
        self.cancelled = Arc::new(AtomicBool::new(false));
        self.bindings = bindings;
        let mut unique = std::collections::BTreeSet::new();
        for binding in self.bindings.values_mut() {
            if let Ok(asset) = binding {
                if unique.len() == 8 && !unique.contains(asset) {
                    *binding = Err("ViewSpec queue limit; source retained".into());
                } else {
                    unique.insert(asset.clone());
                }
            }
        }
        unique.into_iter().collect()
    }
}
fn accepts_snapshot(state: &IncludesState, identity: &Identity, serial: u64, epoch: u64) -> bool {
    state.accepts(identity, serial)
        && state.preview_open
        && state.preview_epoch == epoch
        && !state.viewspecs.cancelled.load(Ordering::Acquire)
}
impl Workspace {
    pub(super) fn load_viewspecs(&mut self, cx: &mut Context<Self>) {
        let Some(identity) = self.includes_identity() else {
            return;
        };
        let Some(registry) = self.follow_registry() else {
            return;
        };
        let Some(state) = &self.shell.includes else {
            return;
        };
        if !state.accepts(&identity, state.serial)
            || !state.preview_open
            || state.viewspecs.started
            || !self.includes_body_current(state)
        {
            return;
        }
        let Some(Ok(snapshot)) = &state.result else {
            return;
        };
        let Ok(preview) = &snapshot.preview else {
            return;
        };
        let mut bindings = BTreeMap::new();
        for (&key, admission) in &snapshot.boards.viewspecs {
            if admission.is_err() {
                continue;
            }
            let (blocks, scope) = match &preview.items[key.0] {
                markdown::PreviewItem::Host(blocks) => {
                    (blocks, Some(identity.source.note.vault.as_str()))
                }
                markdown::PreviewItem::Include { blocks, index, .. } => {
                    let entry = &snapshot.response.entries[*index];
                    (
                        blocks,
                        entry.explicit_origin.then_some(entry.vault.as_str()),
                    )
                }
            };
            let asset = scope
                .and_then(|scope| self.gallery_scope(scope))
                .filter(|scope| registry.1.contains(scope))
                .ok_or_else(|| "ViewSpec unavailable: unverified source origin".to_owned())
                .map(|scope| (scope, blocks[key.1].text.clone()));
            bindings.insert(key, asset);
        }
        let state = self.shell.includes.as_mut().unwrap();
        let pending = state.viewspecs.prepare(bindings);
        if pending.is_empty() {
            return;
        }
        let serial = state.serial;
        let epoch = state.preview_epoch;
        let cancelled = state.viewspecs.cancelled.clone();
        let client = self.live.as_ref().unwrap().client();
        // A single bounded task drains this snapshot. Permit contention waits without
        // blocking the UI or spawning jobs per frame. Close invalidates its queue.
        cx.spawn(async move |this, cx| {
            for asset in pending {
                let mut worker = None;
                while worker.is_none() {
                    let valid = this
                        .update(cx, |this, _| {
                            this.viewspec_current(&identity, serial, epoch, &registry)
                        })
                        .unwrap_or(false);
                    if !valid || cancelled.load(Ordering::Acquire) {
                        return;
                    }
                    worker = Worker::acquire();
                    if worker.is_none() {
                        cx.background_executor()
                            .timer(Duration::from_millis(40))
                            .await;
                    }
                }
                if cancelled.load(Ordering::Acquire) {
                    break;
                }
                let valid = this
                    .update(cx, |this, _| {
                        this.viewspec_current(&identity, serial, epoch, &registry)
                    })
                    .unwrap_or(false);
                if !valid {
                    break;
                }
                let request = asset.clone();
                let client = client.clone();
                let stop = cancelled.clone();
                let storage = identity.source.storage_generation;
                let launch = identity.source.launch_generation;
                let result = cx
                    .background_executor()
                    .spawn(async move {
                        // The actual blocking request owns the permit even if its UI task disappears.
                        let _worker = worker;
                        let check = || {
                            if stop.load(Ordering::Acquire)
                                || !client.is_alive()
                                || client.launch_generation != launch
                            {
                                return Err("ViewSpec unavailable: source changed".to_owned());
                            }
                            let state = client.storage().map_err(|_| {
                                "ViewSpec unavailable: storage check failed".to_owned()
                            })?;
                            if state.phase != "ready" || state.generation != storage {
                                return Err("ViewSpec unavailable: storage changed".to_owned());
                            }
                            Ok(())
                        };
                        check()?;
                        if stop.load(Ordering::Acquire) {
                            return Err("ViewSpec cancelled".into());
                        }
                        let chart = client.inline_viewspec(&request.0, &request.1)?;
                        check()?;
                        Ok(Arc::new(chart))
                    })
                    .await;
                let installed = this
                    .update(cx, |this, cx| {
                        if cancelled.load(Ordering::Acquire)
                            || !this.viewspec_current(&identity, serial, epoch, &registry)
                        {
                            return false;
                        }
                        let state = this.shell.includes.as_mut().unwrap();
                        if !state
                            .viewspecs
                            .bindings
                            .values()
                            .any(|b| b.as_ref().ok() == Some(&asset))
                        {
                            return false;
                        }
                        state.viewspecs.results.insert(asset, result);
                        cx.notify();
                        true
                    })
                    .unwrap_or(false);
                if !installed {
                    break;
                }
            }
        })
        .detach();
    }
    fn viewspec_current(
        &self,
        identity: &Identity,
        serial: u64,
        epoch: u64,
        registry: &(String, Vec<String>),
    ) -> bool {
        self.includes_identity().as_ref() == Some(identity)
            && self.follow_registry().as_ref() == Some(registry)
            && self.shell.includes.as_ref().is_some_and(|s| {
                accepts_snapshot(s, identity, serial, epoch) && self.includes_body_current(s)
            })
    }
    pub(super) fn viewspec_face(&self, visual: Div, key: Key, dark: bool) -> Div {
        let state = self.shell.includes.as_ref().unwrap();
        if state.viewspecs.cancelled.load(Ordering::Acquire)
            || self.includes_identity().as_ref() != Some(&state.identity)
            || !self.includes_body_current(state)
        {
            return visual
                .child("ViewSpec unavailable: preview changed; refresh rendered content to retry");
        }
        match state.viewspecs.bindings.get(&key) {
            Some(Err(reason)) => visual.child(reason.clone()),
            Some(Ok(asset))
                if self.follow_registry().is_some() && self.gallery_scope_verified(&asset.0) =>
            {
                match state.viewspecs.results.get(asset) {
                    Some(Ok(chart)) => {
                        visual.child(crate::numeric_chart_view::render(chart.clone(), dark))
                    }
                    Some(Err(reason)) => visual.child(reason.clone()),
                    None => visual.child("Loading ViewSpec…"),
                }
            }
            _ => visual.child("ViewSpec unavailable: unverified source origin"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn spec() -> String {
        r#"{"version":2,"mark":"line","data":{"kind":"metric","records":[{"version":1,"name":"score","time":"A","value":1},{"version":1,"name":"score","time":"B","value":3}]},"encoding":{"x":{"field":"time"},"y":[{"field":"value"}]}}"#.into()
    }
    #[test]
    fn snapshot_guards_reject_close_epoch_aba_and_every_source_dimension() {
        let id = Identity {
            source: LinkSource {
                note: live::NoteKey {
                    vault: "work".into(),
                    note_id: 1,
                },
                etag: "rev".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            route: live::NoteKey {
                vault: "work".into(),
                note_id: 1,
            },
        };
        let mut state = IncludesState::new(id.clone());
        state.open = true;
        state.preview_open = true;
        state.serial = 5;
        state.preview_epoch = 6;
        assert!(accepts_snapshot(&state, &id, 5, 6));
        for dimension in 0..8 {
            let mut changed = id.clone();
            match dimension {
                0 => changed.source.request += 1,
                1 => changed.source.launch_generation += 1,
                2 => changed.source.storage_generation += 1,
                3 => changed.source.etag.push('x'),
                4 => changed.source.note.note_id += 1,
                5 => changed.source.note.vault.push('x'),
                6 => changed.route.vault.clear(),
                _ => changed.route.note_id += 1,
            }
            assert!(!accepts_snapshot(&state, &changed, 5, 6));
        }
        assert!(!accepts_snapshot(&state, &id, 4, 6));
        state.preview_open = false;
        assert!(!accepts_snapshot(&state, &id, 5, 6));
        state.preview_open = true;
        state.preview_epoch = 7;
        assert!(!accepts_snapshot(&state, &id, 5, 6));
        assert!(accepts_snapshot(&state, &id, 5, 7));
        state.viewspecs.cancel();
        assert!(!accepts_snapshot(&state, &id, 5, 7));
    }
    #[test]
    fn bounded_queue_coalesces_only_identical_scope_and_exact_bytes() {
        let mut resolved = Resolved::default();
        let source = spec();
        let mut bindings = BTreeMap::new();
        for i in 0..10 {
            bindings.insert((0, i), Ok(("work".into(), source.clone())));
        }
        bindings.insert((1, 0), Ok(("other".into(), source.clone())));
        bindings.insert((1, 1), Ok(("work".into(), format!("{source}\n"))));
        let pending = resolved.prepare(bindings);
        assert_eq!(pending.len(), 3);
        assert_eq!(resolved.bindings[&(0, 0)].as_ref().unwrap().1, source);
        assert!(resolved.prepare(BTreeMap::new()).is_empty());
        let stop = resolved.cancelled.clone();
        resolved.cancel();
        assert!(stop.load(Ordering::Acquire));
        assert!(resolved.prepare(BTreeMap::new()).is_empty());
        let mut bounded = Resolved::default();
        let bindings = (0..20)
            .map(|i| ((0, i), Ok((i.to_string(), source.clone()))))
            .collect();
        assert_eq!(bounded.prepare(bindings).len(), 8);
        assert_eq!(bounded.bindings.values().filter(|v| v.is_err()).count(), 12);
        let stop = bounded.cancelled.clone();
        drop(bounded);
        assert!(stop.load(Ordering::Acquire));
    }
    #[test]
    fn mixed_visual_budget_is_reserved_before_resolution_and_source_is_unchanged() {
        let source = spec();
        let body = format!(
            "{}{}",
            format!("```viewspec\n{source}\n```\n").repeat(8),
            "```track-view\n{\"layout\":\"list\",\"columns\":[],\"groups\":[]}\n```\n"
        );
        let response = crate::included_notes::decode(
            serde_json::json!({"markdown":body,"includes":[]}),
            "work",
        )
        .unwrap();
        let preview = markdown::included_preview(&response);
        let (queries, boards) = task_boards::project(&preview, Ok(Arc::new(vec![])));
        assert_eq!(boards.viewspecs.values().filter(|v| v.is_ok()).count(), 8);
        assert!(queries[&(0, 8)].is_err());
        let markdown::PreviewItem::Host(blocks) = &preview.unwrap().items[0] else {
            panic!()
        };
        assert_eq!(blocks[0].text, format!("{source}\n"));
    }
}
