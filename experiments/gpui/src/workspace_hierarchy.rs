//! Explicit read-only Home hierarchy. No eager requests or inferred parent edges.
use super::*;
use gpui::{Div, SharedString};
use std::collections::HashSet;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};

static ACTIVE: AtomicBool = AtomicBool::new(false);
static SERIAL: AtomicU64 = AtomicU64::new(1);
struct Permit;
impl Permit {
    fn acquire() -> Option<Self> {
        ACTIVE
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        ACTIVE.store(false, Ordering::Release);
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    launch: u64,
    storage: u64,
    request: u64,
    scope_revision: u64,
    vault: String,
}
pub(crate) struct HierarchyState {
    identity: Identity,
    open: bool,
    serial: u64,
    result: Option<Result<live::HierarchyResponse, String>>,
    expanded: HashSet<usize>,
    page: usize,
}
impl HierarchyState {
    fn new(identity: Identity) -> Self {
        Self {
            identity,
            open: false,
            serial: 0,
            result: None,
            expanded: HashSet::new(),
            page: 0,
        }
    }
    fn accepts(&self, identity: &Identity, serial: u64) -> bool {
        self.open && &self.identity == identity && self.serial == serial
    }
}
fn visible_rows(rows: &[live::HierarchyRow], expanded: &HashSet<usize>) -> Vec<usize> {
    let mut visible = vec![false; rows.len()];
    let mut result = Vec::new();
    for (i, row) in rows.iter().enumerate() {
        visible[i] = row
            .parent
            .is_none_or(|p| p < i && visible[p] && expanded.contains(&p));
        if visible[i] {
            result.push(i);
        }
    }
    result
}
impl Workspace {
    fn hierarchy_identity(&self) -> Option<Identity> {
        if !self.shell.home
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
        {
            return None;
        }
        Some(Identity {
            launch: self.live.as_ref()?.client().launch_generation,
            storage: self.accepted_storage_generation?,
            request: self.request,
            scope_revision: self.shell.scope_revision,
            vault: self.working_vault_scope().into(),
        })
    }
    fn hierarchy_interactive(&self, cx: &App) -> bool {
        self.pin_keyboard_available(cx) && self.hierarchy_identity().is_some()
    }
    fn hierarchy_fetch(&mut self, identity: Identity, cx: &mut Context<Self>) {
        if self.hierarchy_identity().as_ref() != Some(&identity) || !self.hierarchy_interactive(cx)
        {
            return;
        }
        let Some(state) = self
            .shell
            .hierarchy
            .as_mut()
            .filter(|s| s.identity == identity && s.open)
        else {
            return;
        };
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        state.serial = serial;
        state.result = None;
        state.expanded.clear();
        state.page = 0;
        let Some(permit) = Permit::acquire() else {
            state.result = Some(Err(
                "An earlier hierarchy request is still finishing. Refresh shortly.".into(),
            ));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.storage {
                return Err("Storage changed. Reload the workspace.".into());
            }
            let response = client
                .hierarchy(&captured.vault)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready"
                || after.generation != captured.storage
                || response.launch_generation != captured.launch
                || response.source_vault != captured.vault
            {
                return Err("Hierarchy became stale. Reload the workspace.".into());
            }
            Ok(response)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.hierarchy_identity().as_ref() != Some(&identity) {
                    return;
                }
                if let Some(state) = this
                    .shell
                    .hierarchy
                    .as_mut()
                    .filter(|s| s.accepts(&identity, serial))
                {
                    state.result = Some(result);
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn home_hierarchy(&mut self, dark: bool, cx: &mut Context<Self>) -> Div {
        let mut view = div()
            .w(px(520.))
            .max_w_full()
            .mb_4()
            .text_size(px(12.))
            .text_color(rgb(if dark { 0x969a96 } else { 0x77776f }));
        let Some(identity) = self.hierarchy_identity() else {
            self.shell.hierarchy = None;
            return view.child("Hierarchy requires a ready owned workspace.");
        };
        if self
            .shell
            .hierarchy
            .as_ref()
            .is_none_or(|s| s.identity != identity)
        {
            self.shell.hierarchy = Some(HierarchyState::new(identity.clone()));
        }
        let state = self.shell.hierarchy.as_ref().unwrap();
        let open = state.open;
        let control_identity = identity.clone();
        view = view.child(
            div()
                .id("home-hierarchy-toggle")
                .cursor_pointer()
                .child(if open {
                    "▾ Hierarchy"
                } else {
                    "▸ Hierarchy"
                })
                .on_click(cx.listener(move |this, _, _, cx| {
                    if this.hierarchy_identity().as_ref() != Some(&control_identity)
                        || !this.hierarchy_interactive(cx)
                    {
                        return;
                    }
                    let Some(state) = this
                        .shell
                        .hierarchy
                        .as_mut()
                        .filter(|s| s.identity == control_identity)
                    else {
                        return;
                    };
                    state.open = !state.open;
                    state.serial = SERIAL.fetch_add(1, Ordering::Relaxed);
                    state.result = None;
                    state.expanded.clear();
                    state.page = 0;
                    if state.open {
                        this.hierarchy_fetch(control_identity.clone(), cx);
                    }
                    cx.notify();
                })),
        );
        if !open {
            return view;
        }
        let refresh_identity = identity.clone();
        view = view.child(
            div()
                .id("home-hierarchy-refresh")
                .mt_1()
                .cursor_pointer()
                .child("Refresh hierarchy")
                .on_click(cx.listener(move |this, _, _, cx| {
                    this.hierarchy_fetch(refresh_identity.clone(), cx)
                })),
        );
        let state = self.shell.hierarchy.as_ref().unwrap();
        let serial = state.serial;
        let Some(result) = &state.result else {
            return view.child("Loading hierarchy…");
        };
        let response = match result {
            Ok(r) => r,
            Err(error) => return view.child(error.clone()),
        };
        let partial = crate::workspace_activity::incomplete(&response.diagnostics);
        if partial {
            view = view.child("Incomplete or cached hierarchy; missing branches are unknown.");
        }
        if response.rows.is_empty() && !partial {
            view = view.child("No placed hierarchy nodes.");
        }
        let visible = visible_rows(&response.rows, &state.expanded);
        let mut tree = div()
            .id("home-hierarchy-tree")
            .mt_2()
            .max_h(px(240.))
            .overflow_y_scroll()
            .flex()
            .flex_col()
            .gap_1();
        let page = state.page.min(visible.len().saturating_sub(1) / 200);
        for &index in visible.iter().skip(page * 200).take(200) {
            let row = &response.rows[index];
            let expanded = state.expanded.contains(&index);
            let toggle_identity = identity.clone();
            let mut line = div()
                .flex()
                .items_center()
                .gap_2()
                .pl(px((row.depth.min(8) * 12) as f32));
            if row.has_children {
                line = line.child(
                    div()
                        .id(("hierarchy-expand", index))
                        .cursor_pointer()
                        .child(if expanded { "▾" } else { "▸" })
                        .on_click(cx.listener(move |this, _, _, cx| {
                            if this.hierarchy_identity().as_ref() != Some(&toggle_identity)
                                || !this.hierarchy_interactive(cx)
                            {
                                return;
                            }
                            if let Some(state) = this
                                .shell
                                .hierarchy
                                .as_mut()
                                .filter(|s| s.accepts(&toggle_identity, serial))
                            {
                                if !state.expanded.remove(&index) {
                                    state.expanded.insert(index);
                                }
                                state.page = 0;
                                cx.notify();
                            }
                        })),
                );
            } else {
                line = line.child(div().w(px(10.)));
            }
            let key = live::NoteKey {
                vault: response.vault.clone(),
                note_id: row.note_id,
            };
            let open_identity = identity.clone();
            let flags = row
                .flags
                .iter()
                .map(|f| format!("[{f}]"))
                .collect::<Vec<_>>()
                .join(" ");
            line = line.child(
                div()
                    .id(("hierarchy-note", index))
                    .cursor_pointer()
                    .child(crate::note_metadata::relation_title(&row.title, &flags))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if this.hierarchy_identity().as_ref() != Some(&open_identity)
                            || !this.hierarchy_interactive(cx)
                        {
                            return;
                        }
                        if this.shell.hierarchy.as_ref().is_some_and(|s| {
                            s.accepts(&open_identity, serial)
                                && s.result.as_ref().is_some_and(|r| r.is_ok())
                        }) {
                            this.open_live_key(key.clone(), cx);
                        }
                    })),
            );
            tree = tree.child(line);
        }
        view = view.child(tree);
        if visible.len() > 200 {
            view = view.child(format!(
                "Rows {}–{} of {} expanded rows",
                page * 200 + 1,
                ((page + 1) * 200).min(visible.len()),
                visible.len()
            ));
            let mut pages = div().flex().gap_3();
            for (label, target) in [
                ("Previous", page.checked_sub(1)),
                (
                    "Next",
                    ((page + 1) * 200 < visible.len()).then_some(page + 1),
                ),
            ] {
                if let Some(target) = target {
                    let page_identity = identity.clone();
                    pages = pages.child(
                        div()
                            .id(SharedString::from(format!("hierarchy-{label}")))
                            .cursor_pointer()
                            .child(label)
                            .on_click(cx.listener(move |this, _, _, cx| {
                                if this.hierarchy_identity().as_ref() != Some(&page_identity)
                                    || !this.hierarchy_interactive(cx)
                                {
                                    return;
                                }
                                if let Some(state) = this
                                    .shell
                                    .hierarchy
                                    .as_mut()
                                    .filter(|s| s.accepts(&page_identity, serial))
                                {
                                    state.page = target;
                                    cx.notify();
                                }
                            })),
                    );
                }
            }
            view = view.child(pages);
        }
        view.child("Engine up-hierarchy only: unplaced notes and cycles are omitted. Read-only; branch order is preserved.")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn identity() -> Identity {
        Identity {
            launch: 1,
            storage: 2,
            request: 3,
            scope_revision: 4,
            vault: "work".into(),
        }
    }
    #[test]
    fn hierarchy_results_reject_closed_reopened_and_changed_sources() {
        let id = identity();
        let mut state = HierarchyState::new(id.clone());
        state.serial = 8;
        assert!(!state.accepts(&id, 8));
        state.open = true;
        assert!(state.accepts(&id, 8));
        state.open = false;
        assert!(!state.accepts(&id, 8));
        state.open = true;
        state.serial = 9;
        assert!(!state.accepts(&id, 8));
        for dim in 0..5 {
            let mut changed = id.clone();
            match dim {
                0 => changed.launch += 1,
                1 => changed.storage += 1,
                2 => changed.request += 1,
                3 => changed.scope_revision += 1,
                _ => changed.vault = "other".into(),
            };
            assert!(!state.accepts(&changed, 9));
        }
        assert!(state.accepts(&id, 9));
    }
    #[test]
    fn hierarchy_single_worker_permit_releases_after_failure_or_cancellation() {
        let permit = Permit::acquire().unwrap();
        assert!(Permit::acquire().is_none());
        drop(permit);
        assert!(Permit::acquire().is_some());
    }
    fn row(id: i64, parent: Option<usize>, depth: usize) -> live::HierarchyRow {
        live::HierarchyRow {
            note_id: id,
            title: id.to_string(),
            flags: vec![],
            parent,
            depth,
            has_children: true,
        }
    }
    #[test]
    fn hierarchy_expansion_preserves_order_and_hidden_descendants() {
        let rows = vec![
            row(3, None, 0),
            row(9, Some(0), 1),
            row(1, Some(1), 2),
            row(4, None, 0),
        ];
        assert_eq!(visible_rows(&rows, &HashSet::new()), vec![0, 3]);
        assert_eq!(visible_rows(&rows, &HashSet::from([0])), vec![0, 1, 3]);
        assert_eq!(
            visible_rows(&rows, &HashSet::from([0, 1])),
            vec![0, 1, 2, 3]
        );
        assert_eq!(visible_rows(&rows, &HashSet::from([1])), vec![0, 3]);
        let roots = (0..4096).map(|id| row(id, None, 0)).collect::<Vec<_>>();
        assert_eq!(
            visible_rows(&roots, &HashSet::new())
                .iter()
                .skip(200)
                .take(200)
                .count(),
            200
        );
    }
}
