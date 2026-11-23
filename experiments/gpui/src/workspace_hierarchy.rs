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
    reader: Option<(live::NoteKey, String)>,
}
pub(crate) struct HierarchyState {
    identity: Identity,
    open: bool,
    serial: u64,
    result: Option<Result<live::HierarchyResponse, String>>,
    expanded: HashSet<usize>,
    page: usize,
}
pub(crate) struct ReaderHierarchy {
    state: HierarchyState,
    focus: gpui::FocusHandle,
    return_focus: Option<gpui::FocusHandle>,
}
fn reader_control_allowed(current: bool, active: bool, focused: bool) -> bool {
    current && active && focused
}
fn root_expansion(rows: &[live::HierarchyRow]) -> HashSet<usize> {
    rows.iter()
        .enumerate()
        .filter_map(|(index, row)| row.parent.is_none().then_some(index))
        .collect()
}
fn toggle_allowed(reader: bool, row: &live::HierarchyRow) -> bool {
    row.has_children && (!reader || row.parent.is_some())
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
            reader: None,
        })
    }
    fn hierarchy_interactive(&self, cx: &App) -> bool {
        self.pin_keyboard_available(cx) && self.hierarchy_identity().is_some()
    }
    fn hierarchy_fetch(&mut self, identity: Identity, cx: &mut Context<Self>) {
        if !self.hierarchy_current(&identity, cx) {
            return;
        }
        let Some(state) = self
            .hierarchy_state_mut(&identity)
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
                if this.retire_invalid_read(cx) {
                    cx.notify();
                    return;
                }
                let current = if identity.reader.is_some() {
                    this.hierarchy_current(&identity, cx)
                } else {
                    this.hierarchy_identity().as_ref() == Some(&identity)
                };
                if !current {
                    return;
                }
                if let Some(state) = this
                    .hierarchy_state_mut(&identity)
                    .filter(|s| s.accepts(&identity, serial))
                {
                    if identity.reader.is_some() {
                        if let Ok(response) = &result {
                            state.expanded = root_expansion(&response.rows);
                        }
                    }
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
        self.hierarchy_contents(view, identity, cx)
    }
    fn hierarchy_contents(&self, mut view: Div, identity: Identity, cx: &mut Context<Self>) -> Div {
        let refresh_identity = identity.clone();
        view = view.child(
            div()
                .id("home-hierarchy-refresh")
                .mt_1()
                .cursor_pointer()
                .child("Refresh hierarchy")
                .on_click(cx.listener(move |this, _, window, cx| {
                    if this.hierarchy_control_current(&refresh_identity, window, cx) {
                        this.hierarchy_fetch(refresh_identity.clone(), cx);
                    }
                })),
        );
        let state = self.hierarchy_state(&identity).unwrap();
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
            if toggle_allowed(identity.reader.is_some(), row) {
                line = line.child(
                    div()
                        .id(("hierarchy-expand", index))
                        .cursor_pointer()
                        .child(if expanded { "▾" } else { "▸" })
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if !this.hierarchy_control_current(&toggle_identity, window, cx) {
                                return;
                            }
                            if let Some(state) = this
                                .hierarchy_state_mut(&toggle_identity)
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
                line = line.child(div().w(px(10.)).when(
                    identity.reader.is_some() && row.parent.is_none() && row.has_children,
                    |view| view.child("▾"),
                ));
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
                    .min_w_0()
                    .flex_1()
                    .child(crate::note_metadata::relation_title(&row.title, &flags))
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if !this.hierarchy_control_current(&open_identity, window, cx) {
                            return;
                        }
                        if this.hierarchy_state(&open_identity).is_some_and(|s| {
                            s.accepts(&open_identity, serial)
                                && s.result.as_ref().is_some_and(|r| r.is_ok())
                        }) {
                            if open_identity.reader.is_some() {
                                this.close_reader_hierarchy(window, cx);
                            }
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
                            .on_click(cx.listener(move |this, _, window, cx| {
                                if !this.hierarchy_control_current(&page_identity, window, cx) {
                                    return;
                                }
                                if let Some(state) = this
                                    .hierarchy_state_mut(&page_identity)
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
            reader: None,
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

impl Workspace {
    pub(crate) fn reader_hierarchy_open(&self) -> bool {
        self.shell.reader_hierarchy.is_some()
    }
    fn reader_hierarchy_identity(&self) -> Option<Identity> {
        if self.shell.home
            || self.full_graph_active()
            || self.tag_page.is_some()
            || self.editing_ui.active()
            || self.partial_read
            || self.read_error.is_some()
            || self.shell.accepted_title_request != Some(self.request)
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
        {
            return None;
        }
        let source = self.current_link_source()?;
        let route = self.current_note_request.as_ref()?;
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if source.note != *route
            || self.current_note.as_ref()?.note.key() != *route
            || self.tabs.active()?.key
                != (tabs::TabKey::Note {
                    vault: route.vault.clone(),
                    note_id: route.note_id,
                })
            || storage.phase != "ready"
            || storage.generation != source.storage_generation
        {
            return None;
        }
        Some(Identity {
            launch: source.launch_generation,
            storage: source.storage_generation,
            request: source.request,
            scope_revision: self.shell.scope_revision,
            vault: self.working_vault_scope().into(),
            reader: Some((source.note, source.etag)),
        })
    }
    fn hierarchy_other_owner_blocked(&self, cx: &App) -> bool {
        !self.editing_ui.plain_reader_mode()
            || self.editor_shortcut_blocked(cx)
            || self.enlarged_image_open()
            || self.handoff_active()
            || self.includes_preview_active()
            || self.shell.search_open
            || self.shell.find.open
            || self.shell.find.highlighted
            || self.shell.tab_menu
            || self.shell.vault_menu
            || self.shell.diagnostics
            || self.reader_fragment_pending()
            || self.previews.model.transient.is_some()
            || self.previews.model.geometry_active()
            || self.search.read(cx).is_composing()
            || self.shell.find.is_composing(cx)
            || self.shell.calendar_zone.is_composing(cx)
            || (self.session_ui.recovery_enabled()
                && (!self.session_ui.recovery_ready()
                    || self.session_ui.recovery_record().is_some()
                    || self.session_ui.recovery_status()
                        != workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Empty,
                        )))
    }
    fn hierarchy_current(&self, identity: &Identity, cx: &App) -> bool {
        if identity.reader.is_some() {
            self.reader_hierarchy_identity().as_ref() == Some(identity)
                && !self.hierarchy_other_owner_blocked(cx)
                && self
                    .shell
                    .reader_hierarchy
                    .as_ref()
                    .is_some_and(|ui| ui.state.identity == *identity)
        } else {
            self.hierarchy_identity().as_ref() == Some(identity) && self.hierarchy_interactive(cx)
        }
    }
    fn hierarchy_control_current(&self, identity: &Identity, window: &Window, cx: &App) -> bool {
        if identity.reader.is_none() {
            return self.hierarchy_current(identity, cx);
        }
        reader_control_allowed(
            self.hierarchy_current(identity, cx),
            window.is_window_active(),
            self.shell
                .reader_hierarchy
                .as_ref()
                .is_some_and(|ui| ui.focus.is_focused(window)),
        )
    }
    fn hierarchy_state(&self, identity: &Identity) -> Option<&HierarchyState> {
        if identity.reader.is_some() {
            self.shell.reader_hierarchy.as_ref().map(|ui| &ui.state)
        } else {
            self.shell.hierarchy.as_ref()
        }
    }
    fn hierarchy_state_mut(&mut self, identity: &Identity) -> Option<&mut HierarchyState> {
        if identity.reader.is_some() {
            self.shell.reader_hierarchy.as_mut().map(|ui| &mut ui.state)
        } else {
            self.shell.hierarchy.as_mut()
        }
    }
    pub(crate) fn reader_hierarchy_entry(
        &self,
        color: u32,
        background: u32,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let identity = self.reader_hierarchy_identity();
        let enabled = !self.reader_hierarchy_open()
            && !self.hierarchy_other_owner_blocked(cx)
            && self.native_find_available(false, window, cx)
            && identity.is_some();
        crate::rail_icons::button(
            "reader-hierarchy-entry",
            crate::rail_icons::Icon::Hierarchy,
            color,
            background,
        )
        .opacity(if enabled { 1. } else { 0.4 })
        .when(enabled, |entry| {
            entry
                .cursor_pointer()
                .on_click(cx.listener(move |this, _, window, cx| {
                    if this.reader_hierarchy_open()
                        || this.hierarchy_other_owner_blocked(cx)
                        || !this.native_find_available(false, window, cx)
                        || this.reader_hierarchy_identity() != identity
                    {
                        return;
                    }
                    let Some(identity) = identity.clone() else {
                        return;
                    };
                    this.pause_reader_for_overlay();
                    let return_focus = window.focused(cx);
                    let focus = cx.focus_handle();
                    window.focus(&focus);
                    let mut state = HierarchyState::new(identity.clone());
                    state.open = true;
                    this.shell.reader_hierarchy = Some(ReaderHierarchy {
                        state,
                        focus,
                        return_focus,
                    });
                    this.hierarchy_fetch(identity, cx);
                    cx.stop_propagation();
                    cx.notify();
                }))
        })
    }
    pub(crate) fn close_reader_hierarchy(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(ui) = self.shell.reader_hierarchy.take() else {
            return;
        };
        // Dropping this owner invalidates every request/control serial. No retry.
        if ui.focus.is_focused(window) {
            if window.is_window_active()
                && self.reader_hierarchy_identity().as_ref() == Some(&ui.state.identity)
                && !self.hierarchy_other_owner_blocked(cx)
            {
                if let Some(focus) = ui.return_focus {
                    window.focus(&focus);
                } else {
                    window.blur();
                }
            } else {
                window.blur();
            }
        }
        cx.notify();
    }
    pub(crate) fn sync_reader_hierarchy(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .shell
            .reader_hierarchy
            .as_ref()
            .is_some_and(|ui| !self.hierarchy_current(&ui.state.identity, cx))
        {
            self.close_reader_hierarchy(window, cx);
        }
    }
    pub(crate) fn render_reader_hierarchy(
        &self,
        dark: bool,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        let ui = self.shell.reader_hierarchy.as_ref().unwrap();
        let identity = ui.state.identity.clone();
        let viewport = window.viewport_size();
        let contents = div()
            .text_size(px(12.))
            .flex()
            .flex_col()
            .gap_2()
            .child("Hierarchy")
            .child(format!(
                "Working-vault scope: {}",
                if identity.vault.is_empty() {
                    "launch vault"
                } else {
                    &identity.vault
                }
            ))
            .child("Click-only panel · roots stay expanded")
            .child(
                div()
                    .id("close-reader-hierarchy")
                    .cursor_pointer()
                    .child("Close")
                    .on_click(cx.listener(|this, _, window, cx| {
                        this.close_reader_hierarchy(window, cx);
                        cx.stop_propagation();
                    })),
            );
        let contents = self.hierarchy_contents(contents, identity, cx);
        div()
            .id("reader-hierarchy-backdrop")
            .absolute()
            .inset_0()
            .bg(gpui::rgba(0x00000066))
            .flex()
            .items_center()
            .justify_center()
            .track_focus(&ui.focus)
            .on_action(|_: &Save, _, cx| cx.stop_propagation())
            .on_action(|_: &CloseTab, _, cx| cx.stop_propagation())
            .on_action(|_: &ShowAbout, _, cx| cx.stop_propagation())
            .on_action(|_: &CopyBuildInfo, _, cx| cx.stop_propagation())
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                if event.keystroke.key == "escape" {
                    this.close_reader_hierarchy(window, cx);
                }
                cx.stop_propagation();
            }))
            .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_up(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_scroll_wheel(|_, _, cx| cx.stop_propagation())
            .on_click(cx.listener(|this, _, window, cx| {
                this.close_reader_hierarchy(window, cx);
                cx.stop_propagation();
            }))
            .child(
                div()
                    .id("reader-hierarchy-panel")
                    .w(px(520.).min((viewport.width - px(32.)).max(px(0.))))
                    .max_h((viewport.height - px(32.)).max(px(0.)))
                    .overflow_y_scroll()
                    .p_3()
                    .bg(rgb(if dark { 0x191c1e } else { 0xffffff }))
                    .text_color(rgb(if dark { 0xe9e9e4 } else { 0x1a1a18 }))
                    .on_click(|_, _, cx| cx.stop_propagation())
                    .child(contents),
            )
    }
}

#[cfg(test)]
mod reader_panel_tests {
    use super::*;
    fn row(id: i64, parent: Option<usize>, has_children: bool) -> live::HierarchyRow {
        live::HierarchyRow {
            note_id: id,
            title: "Repeated title 日本語".into(),
            flags: vec![],
            parent,
            depth: usize::from(parent.is_some()),
            has_children,
        }
    }
    fn identity() -> Identity {
        Identity {
            launch: 1,
            storage: 2,
            request: 3,
            scope_revision: 4,
            vault: "working-scope".into(),
            reader: Some((
                live::NoteKey {
                    vault: "foreign-reader".into(),
                    note_id: 42,
                },
                "revision".into(),
            )),
        }
    }
    #[test]
    fn reader_controls_require_current_owner_active_window_and_panel_focus() {
        assert!(reader_control_allowed(true, true, true));
        for flags in [
            (false, true, true),
            (true, false, true),
            (true, true, false),
        ] {
            assert!(!reader_control_allowed(flags.0, flags.1, flags.2));
        }
    }
    #[test]
    fn roots_always_expanded_nonroot_branches_toggle_leaves_do_not() {
        let rows = vec![
            row(1, None, true),
            row(2, Some(0), true),
            row(3, Some(1), false),
            row(4, Some(0), false),
            row(5, None, false),
        ];
        let mut expanded = root_expansion(&rows);
        assert_eq!(visible_rows(&rows, &expanded), vec![0, 1, 3, 4]);
        assert!(!toggle_allowed(true, &rows[0]));
        assert!(toggle_allowed(true, &rows[1]));
        assert!(!toggle_allowed(true, &rows[2]));
        expanded.insert(1);
        assert_eq!(visible_rows(&rows, &expanded), vec![0, 1, 2, 3, 4]);
        expanded.remove(&1);
        assert_eq!(visible_rows(&rows, &expanded), vec![0, 1, 3, 4]);
        assert!(toggle_allowed(false, &rows[0])); // Home keeps its prior root behavior.
        assert_eq!(visible_rows(&rows, &HashSet::new()), vec![0, 4]);
    }
    #[test]
    fn reader_source_scope_close_and_new_request_reject_old_results_controls() {
        let id = identity();
        let mut state = HierarchyState::new(id.clone());
        state.open = true;
        state.serial = 10;
        assert!(state.accepts(&id, 10));
        for dimension in 0..8 {
            let mut changed = id.clone();
            match dimension {
                0 => changed.launch += 1,
                1 => changed.storage += 1,
                2 => changed.request += 1,
                3 => changed.scope_revision += 1,
                4 => changed.vault = "other scope".into(),
                5 => changed.reader.as_mut().unwrap().0.vault = "another reader".into(),
                6 => changed.reader.as_mut().unwrap().0.note_id += 1,
                _ => changed.reader.as_mut().unwrap().1.push('x'),
            }
            assert!(!state.accepts(&changed, 10));
        }
        state.open = false;
        assert!(!state.accepts(&id, 10));
        state.open = true;
        state.serial = 11;
        assert!(!state.accepts(&id, 10));
        assert_eq!(id.vault, "working-scope");
        assert_ne!(id.vault, id.reader.as_ref().unwrap().0.vault);
    }
    #[test]
    fn repeated_labels_preserve_ids_order_and_page_bound() {
        let rows = (0..450).map(|id| row(id, None, false)).collect::<Vec<_>>();
        let visible = visible_rows(&rows, &root_expansion(&rows));
        assert_eq!(visible.len(), 450);
        assert_eq!(
            visible
                .iter()
                .skip(200)
                .take(200)
                .map(|&i| rows[i].note_id)
                .collect::<Vec<_>>(),
            (200..400).collect::<Vec<_>>()
        );
        assert_eq!(visible.iter().skip(400).take(200).count(), 50);
    }
    #[test]
    fn failed_result_is_not_successful_empty_and_retains_explicit_error() {
        let mut state = HierarchyState::new(identity());
        state.open = true;
        state.result = Some(Err("storage changed".into()));
        assert!(matches!(state.result,Some(Err(ref message)) if message=="storage changed"));
        assert!(state.expanded.is_empty());
    }
}
