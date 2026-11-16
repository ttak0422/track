#[path = "rendered_reading.rs"]
mod reading_mode;
pub(crate) use reading_mode::Mode as ReadingMode;
#[path = "gallery_covers.rs"]
mod gallery_covers;
#[path = "include_navigation.rs"]
mod include_navigation;
#[path = "preview_copy.rs"]
mod preview_copy;
#[path = "query_inline.rs"]
mod query_inline;
#[path = "query_navigation.rs"]
mod query_navigation;
#[path = "rendered_find.rs"]
mod rendered_find;
#[path = "task_boards.rs"]
mod task_boards;
#[path = "viewspec_preview.rs"]
mod viewspec_preview;
// Explicit, inert engine-resolved snapshots bound to the accepted host source.
use super::*;
use crate::shell::LinkSource;
use gpui::Div;
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
    source: LinkSource,
    route: live::NoteKey,
}
pub(crate) struct IncludesState {
    identity: Identity,
    accepted_body: Option<String>,
    tasks_invalidated: bool,
    open: bool,
    inline_open: bool,
    serial: u64,
    result: Option<Result<Snapshot, String>>,
    fetching: bool,
    refresh_warning: Option<String>,
    reading_ticket: Option<reading_mode::Ticket>,
    preview_open: bool,
    preview_focus: Option<gpui::FocusHandle>,
    preview_scroll: gpui::ScrollHandle,
    preview_epoch: u64,
    query_inline: query_inline::InlineQuery,
    query_nav: query_navigation::Navigation,
    query_pages: std::collections::BTreeMap<(usize, usize, Option<usize>), usize>,
    query_sources: std::collections::BTreeSet<(usize, usize)>,
    gallery: gallery_covers::Gallery,
    viewspecs: viewspec_preview::Resolved,
    flowchart_cache: crate::flowchart_view::Cache,
    mindmap_cache: crate::mindmap_view::Cache,
    copy: preview_copy::CopyUi,
    find: rendered_find::Ui,
}
/// Preview controls must not use host-reader gates, which intentionally reject
/// this surface. Capture only competing actions and composition here.
#[derive(Default)]
struct QueryPageGate {
    preview_active: bool,
    source_matches: bool,
    handoff: bool,
    gesture: bool,
    editing: bool,
    pending: bool,
    find: bool,
    search: bool,
    menu: bool,
    closing: bool,
    composing: bool,
}
impl QueryPageGate {
    fn allowed(&self) -> bool {
        self.preview_active
            && self.source_matches
            && !(self.handoff
                || self.gesture
                || self.editing
                || self.pending
                || self.find
                || self.search
                || self.menu
                || self.closing
                || self.composing)
    }
}
struct Snapshot {
    query_positions: Result<std::collections::BTreeMap<usize, usize>, String>,
    inline: Result<markdown::InlineIncludes, String>,
    boards: task_boards::Boards,
    response: crate::included_notes::Response,
    preview: Result<markdown::IncludePreview, String>,
    queries: std::collections::BTreeMap<(usize, usize), Result<crate::query_view::View, String>>,
}
impl IncludesState {
    fn new(identity: Identity) -> Self {
        Self {
            identity,
            accepted_body: None,
            tasks_invalidated: false,
            open: false,
            inline_open: false,
            serial: 0,
            result: None,
            fetching: false,
            refresh_warning: None,
            reading_ticket: None,
            preview_open: false,
            preview_focus: None,
            preview_scroll: gpui::ScrollHandle::new(),
            preview_epoch: 0,
            query_inline: Default::default(),
            query_nav: Default::default(),
            query_pages: Default::default(),
            query_sources: Default::default(),
            gallery: Default::default(),
            viewspecs: Default::default(),
            flowchart_cache: Default::default(),
            mindmap_cache: Default::default(),
            copy: Default::default(),
            find: Default::default(),
        }
    }
    fn query_page_action(
        &mut self,
        gate: QueryPageGate,
        identity: &Identity,
        serial: u64,
        epoch: u64,
        key: (usize, usize),
        group: Option<usize>,
        page: usize,
    ) -> bool {
        gate.allowed() && self.set_query_page(identity, serial, epoch, key, group, page)
    }
    fn set_query_page(
        &mut self,
        identity: &Identity,
        serial: u64,
        epoch: u64,
        key: (usize, usize),
        group: Option<usize>,
        page: usize,
    ) -> bool {
        if !self.accepts(identity, serial) || !self.preview_open || self.preview_epoch != epoch {
            return false;
        }
        let Some(Ok(snapshot)) = &self.result else {
            return false;
        };
        if snapshot.boards.blocks.contains_key(&key) {
            return task_boards::set_page(self, key, group, page);
        }
        let Some(Ok(view)) = snapshot.queries.get(&key) else {
            return false;
        };
        let (count, size) = match (view.layout, group) {
            (crate::query_view::Layout::Gallery, None) => {
                (view.total_rows(), crate::query_view::GALLERY_PAGE)
            }
            (crate::query_view::Layout::List, None) => {
                (view.total_rows(), crate::query_view::LIST_PAGE)
            }
            (crate::query_view::Layout::Calendar, None) => {
                let count = view.calendar_months().len();
                if count == 0 {
                    return false;
                }
                (count, 1)
            }
            (crate::query_view::Layout::Board, None) => {
                (view.groups.len(), crate::query_view::LANE_PAGE)
            }
            (crate::query_view::Layout::Board, Some(index)) => {
                let lane_page = self
                    .query_pages
                    .get(&(key.0, key.1, None))
                    .copied()
                    .unwrap_or(0);
                if index / crate::query_view::LANE_PAGE != lane_page {
                    return false;
                }
                let Some(lane) = view.groups.get(index) else {
                    return false;
                };
                (lane.rows.len(), crate::query_view::CARD_PAGE)
            }
            _ => return false,
        };
        if page >= count.div_ceil(size).max(1) {
            return false;
        }
        self.query_pages.insert((key.0, key.1, group), page);
        true
    }
    fn query_source_action(
        &mut self,
        gate: QueryPageGate,
        identity: &Identity,
        serial: u64,
        epoch: u64,
        key: (usize, usize),
    ) -> bool {
        if !gate.allowed()
            || !self.accepts(identity, serial)
            || !self.preview_open
            || self.preview_epoch != epoch
        {
            return false;
        }
        let Some(Ok(snapshot)) = &self.result else {
            return false;
        };
        if !matches!(snapshot.queries.get(&key), Some(Ok(view)) if view.layout == crate::query_view::Layout::Calendar)
        {
            return false;
        }
        if !self.query_sources.remove(&key) {
            self.query_sources.insert(key);
        }
        true
    }
    fn accepts(&self, identity: &Identity, serial: u64) -> bool {
        self.open && &self.identity == identity && self.serial == serial
    }
}
fn source_position(line: i64) -> String {
    if line < 0 {
        "noncontiguous".into()
    } else {
        format!("line {}", line as u64 + 1)
    }
}
impl Workspace {
    /// The preview is a separate inert surface, never installed as host content.
    pub(crate) fn includes_preview_active(&self) -> bool {
        self.shell
            .includes
            .as_ref()
            .is_some_and(|s| s.open && s.preview_open)
    }
    fn open_includes_preview(
        &mut self,
        identity: &Identity,
        serial: u64,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.query_inline_active() {
            return;
        }
        self.stop_editor_follow();
        if self.includes_identity().as_ref() != Some(identity)
            || !self.find_available()
            || !self.pin_keyboard_available(cx)
        {
            return;
        }
        let Some(state) = self
            .shell
            .includes
            .as_mut()
            .filter(|s| s.accepts(identity, serial) && matches!(s.result, Some(Ok(_))))
        else {
            return;
        };
        state.preview_open = true;
        state.find = Default::default();
        state.copy =
            preview_copy::CopyUi::prepare(state.result.as_ref().and_then(|r| r.as_ref().ok()), cx);
        state.preview_epoch = SERIAL.fetch_add(1, Ordering::Relaxed);
        state.query_nav = Default::default();
        state.query_inline = Default::default();
        state.query_pages.clear();
        state.query_sources.clear();
        state.preview_scroll = gpui::ScrollHandle::new();
        let focus = state
            .preview_focus
            .get_or_insert_with(|| cx.focus_handle())
            .clone();
        // Invalidate deferred host checkpoint/layout work; never capture preview geometry.
        self.suspend_host_reader_actions();
        self.load_gallery(cx);
        self.load_viewspecs(cx);
        window.focus(&focus);
        cx.notify();
    }
    fn close_includes_preview(
        &mut self,
        identity: &Identity,
        serial: u64,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.rendered_find_composing(cx) {
            return;
        }
        if self.includes_identity().as_ref() != Some(identity) {
            return;
        }
        let Some(state) = self
            .shell
            .includes
            .as_mut()
            .filter(|s| s.accepts(identity, serial) && s.preview_open)
        else {
            return;
        };
        state.preview_open = false;
        state.find = Default::default();
        state.copy = Default::default();
        state.mindmap_cache.clear();
        state.flowchart_cache.clear();
        state.gallery.cancel();
        state.viewspecs.cancel();
        state.query_nav = Default::default();
        state.query_inline = Default::default();
        state.query_pages.clear();
        state.query_sources.clear();
        if let Some(focus) = &self.shell.reader_focus {
            window.focus(focus);
        } else {
            window.blur();
        }
        cx.notify();
    }
    pub(crate) fn rendered_includes_preview(
        &mut self,
        dark: bool,
        colors: (u32, u32, u32),
        window: &mut gpui::Window,
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<Div> {
        self.begin_rendered_find(dark, cx);
        let state = self.shell.includes.as_ref().unwrap();
        let identity = state.identity.clone();
        let serial = state.serial;
        let focus = state.preview_focus.as_ref().unwrap().clone();
        let scroll = state.preview_scroll.clone();
        let reading_controls = self.reading_controls(&identity, serial, true, cx);
        let refresh_status = if state.fetching {
            "Refreshing rendered content; previous same-source snapshot may be shown.".to_owned()
        } else {
            state
                .refresh_warning
                .as_ref()
                .map(|e| format!("Refresh failed; previous same-source snapshot retained: {e}"))
                .unwrap_or_default()
        };
        let (paper, ink, line) = colors;
        let close_identity = identity.clone();
        let mut body = div().flex().flex_col().gap_3();
        if let Some(notice) = self.query_navigation_notice() {
            body = body.child(notice);
        }
        if let Some(Ok(snapshot)) = &state.result {
            match &snapshot.preview {
                Err(error) => body = body.child(format!("Rendered preview unavailable: {error}")),
                Ok(preview) => {
                    for notice in &preview.unsupported {
                        body = body.child(notice.clone());
                    }
                    for (index, item) in preview.items.iter().enumerate() {
                        let mut row = div().id(gpui::SharedString::from(format!(
                            "include-preview-{serial}-{index}"
                        )));
                        row = match item {
                            markdown::PreviewItem::Host(blocks) => row.child(
                                self.render_query_blocks(index, blocks, dark, line, window, cx),
                            ),
                            markdown::PreviewItem::Include {
                                index: entry_index,
                                directive,
                                blocks,
                            } => {
                                let entry = &snapshot.response.entries[*entry_index];
                                let origin = if entry.vault.is_empty() {
                                    "launch vault"
                                } else {
                                    &entry.vault
                                };
                                let target = entry
                                    .note_id
                                    .map(|id| {
                                        format!(
                                            "{} #{id}",
                                            entry.kind.as_deref().unwrap_or("target")
                                        )
                                    })
                                    .unwrap_or_else(|| "unknown target".into());
                                row = row
                                    .border_1()
                                    .border_color(rgb(line))
                                    .p_3()
                                    .child(format!(
                                        "{} · {}",
                                        entry.caption,
                                        entry.title.as_deref().unwrap_or("Untitled include")
                                    ))
                                    .child(format!(
                                        "{origin} · {target} · source {}",
                                        source_position(entry.source_line)
                                    ));
                                row =
                                    row.child(self.include_source_control(index, *entry_index, cx));
                                if let Some(etag) = &entry.etag {
                                    row = row.child(format!("Snapshot revision: {etag}"));
                                }
                                for diagnostic in &entry.bad_options {
                                    row = row.child(format!("Invalid option: {diagnostic}"));
                                }
                                if let Some(error) = entry.error.as_ref().filter(|e| !e.is_empty())
                                {
                                    row = row
                                        .child(directive.clone())
                                        .child(format!("Include error: {error}"));
                                } else if entry.excerpt.is_empty() {
                                    row = row.child("Successful empty excerpt.");
                                } else {
                                    row = row.child(self.render_query_blocks(
                                        index, blocks, dark, line, window, cx,
                                    ));
                                }
                                row
                            }
                        };
                        body = body.child(row);
                    }
                }
            }
        }
        self.finish_rendered_find(window, cx);
        let find_controls = self.rendered_find_controls(cx);
        div().id("rendered-includes-preview").size_full().flex().flex_col().p_4()
            .bg(rgb(paper)).text_color(rgb(ink)).track_focus(&focus)
            .on_action(cx.listener(|this, _: &search_input::FindNext, window, cx| {this.rendered_find_key(Some(true), window, cx);cx.stop_propagation();}))
            .on_action(cx.listener(|this, _: &search_input::FindPrevious, window, cx| {this.rendered_find_key(Some(false), window, cx);cx.stop_propagation();}))
            .on_action(cx.listener(|this, _: &search_input::CloseFind, window, cx| {this.rendered_find_key(None, window, cx);cx.stop_propagation();}))
            .on_key_down(cx.listener(move |this, event: &gpui::KeyDownEvent, window, cx| {
                if event.keystroke.key == "escape" && !this.rendered_find_escape(window, cx) { this.close_rendered_user(&identity, serial, window, cx); }
                // No host Find, navigation, checkpoint, task, link or copy shortcuts.
                cx.stop_propagation();
            }))
            .child(div().flex().justify_between().child("Rendered content (read-only)").child(div().id("close-rendered-includes-preview").cursor_pointer().child("Close preview (Esc)").on_click(cx.listener(move |this, _, window, cx| this.close_rendered_user(&close_identity, serial, window, cx)))))
            .child("Engine-resolved snapshots; completeness and currentness are not guaranteed. Partial inline preview: only isolated top-level includes are embedded. General links, task changes, recursive includes are unavailable. Find searches displayed ordinary host/include prose, not rich projections or their source disclosures. Host query rows provide explicit Open note controls; included query rows remain inert. Gallery covers support bounded local PNG/JPEG only. Text selection is unavailable. Tagged code and displayed source have explicit Copy controls (plain text, up to 256 KiB).")
            .child(reading_controls).child(refresh_status)
            .child(find_controls)
            .child(div().id(gpui::SharedString::from(format!("rendered-includes-scroll-{serial}"))).track_scroll(&scroll).flex_1().min_h_0().overflow_y_scroll().child(body))
    }
    fn includes_identity(&self) -> Option<Identity> {
        if self.full_graph_active()
            || self.shell.home
            || self.editing_ui.active()
            || self.editing_ui.has_pending()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.read_error.is_some()
        {
            return None;
        }
        self.includes_source_identity()
    }
    fn includes_source_identity(&self) -> Option<Identity> {
        if self.read_error.is_some() {
            return None;
        }
        let source = self.current_link_source()?;
        let route = self.current_note_request.as_ref()?;
        if route.note_id != source.note.note_id
            || (!route.vault.is_empty() && route.vault != source.note.vault)
            || !matches!(self.tabs.active().map(|t| &t.key),Some(tabs::TabKey::Note {vault,note_id}) if vault == &route.vault && *note_id == route.note_id)
        {
            return None;
        }
        Some(Identity {
            source,
            route: route.clone(),
        })
    }
    fn includes_body_current(&self, state: &IncludesState) -> bool {
        state.accepted_body.as_ref().is_none_or(|body| {
            self.current_note.as_ref().is_some_and(|note| {
                &note.body == body
                    && note
                        .note
                        .additional
                        .get("tasks")
                        .is_some_and(serde_json::Value::is_null)
                        == state.tasks_invalidated
            })
        })
    }
    pub(crate) fn reset_stale_includes(&mut self) {
        if self.shell.includes.as_ref().is_some_and(|s| {
            if let Some(ticket) = &s.reading_ticket {
                !self.reading_ticket_lifetime(ticket) || !self.includes_body_current(s)
            } else {
                self.includes_identity().as_ref() != Some(&s.identity)
                    || !self.includes_body_current(s)
            }
        }) {
            if self
                .shell
                .includes
                .as_ref()
                .is_some_and(|s| s.reading_ticket.is_some())
            {
                self.shell.rendered_reading.retire();
            }
            self.shell.includes = None;
        }
    }
    fn includes_fetch(&mut self, identity: Identity, cx: &mut Context<Self>) {
        if self.shell.rendered_reading.enabled {
            return;
        }
        self.includes_fetch_owned(identity, None, cx);
    }
    fn includes_fetch_owned(
        &mut self,
        identity: Identity,
        mode: Option<reading_mode::Ticket>,
        cx: &mut Context<Self>,
    ) {
        if self.inline_includes_active() || self.query_inline_active() {
            return;
        }
        let allowed = if let Some(ticket) = &mode {
            self.reading_ticket_current(ticket)
                && if self.includes_preview_active() {
                    self.query_control_gate(&identity, cx).allowed()
                } else {
                    self.pin_keyboard_available(cx) && self.find_available()
                }
        } else {
            self.pin_keyboard_available(cx) && self.find_available()
        };
        if self.includes_identity().as_ref() != Some(&identity) || !allowed {
            return;
        }
        let tasks = self.accepted_tasks();
        let Some(state) = self
            .shell
            .includes
            .as_mut()
            .filter(|s| s.identity == identity && s.open)
        else {
            return;
        };
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        let retain = mode
            .as_ref()
            .zip(state.reading_ticket.as_ref())
            .is_some_and(|(new, old)| new.same_owner(old))
            && state.accepted_body.as_deref()
                == Some(self.current_note.as_ref().unwrap().body.as_str())
            && matches!(state.result, Some(Ok(_)));
        state.accepted_body = Some(self.current_note.as_ref().unwrap().body.clone());
        state.tasks_invalidated = self
            .current_note
            .as_ref()
            .unwrap()
            .note
            .additional
            .get("tasks")
            .is_some_and(serde_json::Value::is_null);
        state.serial = serial;
        if !retain {
            state.result = None;
        }
        state.fetching = true;
        state.refresh_warning = None;
        state.reading_ticket = mode.clone();
        if !retain {
            state.mindmap_cache.clear();
            state.flowchart_cache.clear();
            state.gallery.cancel();
            state.viewspecs.cancel();
            state.gallery = Default::default();
            state.viewspecs = Default::default();
            state.preview_open = false;
            state.copy = Default::default();
            state.query_nav = Default::default();
            state.query_inline = Default::default();
            state.query_pages.clear();
            state.query_sources.clear();
        }
        state.find = Default::default();
        let Some(permit) = Permit::acquire() else {
            state.fetching = false;
            let warning =
                "An earlier render request is still finishing. Use explicit Retry.".to_owned();
            if retain {
                state.refresh_warning = Some(warning);
            } else {
                state.result = Some(Err(warning));
            }
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let body = self.current_note.as_ref().unwrap().body.clone();
        let captured = identity.clone();
        let accepted_body = body.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.source.storage_generation {
                return Err("Storage changed. Reload the workspace.".into());
            }
            let response = client
                .included_notes(&captured.source.note.vault, &body)
                .map_err(|e| e.to_string())?;
            let mut preview = markdown::included_preview(&response);
            preview_copy::normalize(&mut preview);
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready" || after.generation != captured.source.storage_generation {
                return Err("Storage changed. Reload the workspace.".into());
            }
            let (queries, boards) = task_boards::project_with_source(&preview, tasks, Some(&body));
            let inline = markdown::inline_includes(&body, &response);
            Ok(Snapshot {
                query_positions: markdown::inline_query_positions(&body, &response),
                inline,
                boards,
                response,
                preview,
                queries,
            })
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                let source = if mode.is_some() {
                    this.includes_source_identity()
                } else {
                    this.includes_identity()
                };
                if mode
                    .as_ref()
                    .is_some_and(|ticket| !this.reading_ticket_lifetime(ticket))
                    || source.as_ref() != Some(&identity)
                    || this
                        .shell
                        .includes
                        .as_ref()
                        .is_none_or(|state| !this.includes_body_current(state))
                    || this
                        .current_note
                        .as_ref()
                        .is_none_or(|n| n.body != accepted_body)
                {
                    return;
                }
                if let Some(state) = this
                    .shell
                    .includes
                    .as_mut()
                    .filter(|s| s.accepts(&identity, serial))
                {
                    state.fetching = false;
                    match result {
                        Err(error) if matches!(state.result, Some(Ok(_))) && mode.is_some() => {
                            state.refresh_warning = Some(error)
                        }
                        result => {
                            state.result = Some(result);
                            state.refresh_warning = None;
                            state.preview_epoch = SERIAL.fetch_add(1, Ordering::Relaxed);
                            state.find = Default::default();
                            state.query_nav = Default::default();
                            state.query_pages.clear();
                            state.query_sources.clear();
                            state.mindmap_cache.clear();
                            state.flowchart_cache.clear();
                            state.gallery.cancel();
                            state.viewspecs.cancel();
                            state.gallery = Default::default();
                            state.viewspecs = Default::default();
                            if state.preview_open {
                                state.copy = preview_copy::CopyUi::prepare(
                                    state.result.as_ref().and_then(|r| r.as_ref().ok()),
                                    cx,
                                );
                            }
                        }
                    }
                    cx.notify();
                }
                if this.includes_preview_active() {
                    this.load_gallery(cx);
                    this.load_viewspecs(cx);
                }
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn inline_includes_sidebar(
        &self,
        cx: &mut Context<Self>,
    ) -> Option<gpui::Stateful<Div>> {
        let state = self.shell.includes.as_ref()?;
        let identity = self.includes_identity()?;
        if identity != state.identity
            || !self.includes_body_current(state)
            || !state.open
            || !matches!(&state.result, Some(Ok(snapshot)) if snapshot.inline.as_ref().is_ok_and(|rows| !rows.is_empty()))
        {
            return None;
        }
        let serial = state.serial;
        let label = if state.inline_open {
            "Collapse includes"
        } else {
            "Show includes"
        };
        Some(
            div()
                .id("inline-includes-sidebar-toggle")
                .w(px(46.))
                .py_2()
                .text_size(px(10.))
                .cursor_pointer()
                .child(label)
                .on_click(cx.listener(move |this, _, window, cx| {
                    this.toggle_inline_includes(identity.clone(), serial, window, cx)
                })),
        )
    }
    pub(crate) fn inline_includes_active(&self) -> bool {
        self.shell.includes.as_ref().is_some_and(|s| {
            s.inline_open
                && self.includes_identity().as_ref() == Some(&s.identity)
                && self.includes_body_current(s)
        })
    }
    pub(crate) fn inline_include_rows(&self) -> Option<&markdown::InlineIncludes> {
        if !self.inline_includes_active() {
            return None;
        }
        self.shell
            .includes
            .as_ref()?
            .result
            .as_ref()?
            .as_ref()
            .ok()?
            .inline
            .as_ref()
            .ok()
    }
    pub(crate) fn includes_layout_source(&self) -> Option<(LinkSource, tabs::TabKey)> {
        let state = self.shell.includes.as_ref()?;
        let identity = self.includes_identity()?;
        if identity != state.identity
            || !self.includes_body_current(state)
            || !state.open
            || !matches!(&state.result, Some(Ok(snapshot)) if snapshot.inline.as_ref().is_ok_and(|m| !m.is_empty()))
        {
            return None;
        }
        Some((identity.source, self.tabs.active()?.key.clone()))
    }
    fn toggle_inline_includes(
        &mut self,
        identity: Identity,
        serial: u64,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || !self.footnote_reader_available(cx)
            || !self.pin_keyboard_available(cx)
            || self.includes_identity().as_ref() != Some(&identity)
            || self.reader_selection_dragging()
        {
            return;
        }
        let Some(anchors) = self.follow_layout_anchors() else {
            return;
        };
        let Some(passage) = anchors.passage(window.viewport_size(), window.scale_factor()) else {
            return;
        };
        let Some(state) = self
            .shell
            .includes
            .as_mut()
            .filter(|s| s.accepts(&identity, serial))
        else {
            return;
        };
        if !matches!(&state.result, Some(Ok(snapshot)) if snapshot.inline.as_ref().is_ok_and(|m| !m.is_empty()))
        {
            return;
        }
        state.inline_open = !state.inline_open;
        let expanded = state.inline_open;
        self.pause_reader_for_overlay();
        self.clear_reader_selection();
        self.clear_reader_hover_owner();
        let interaction = self.follow_layout_interaction();
        let weak = std::rc::Rc::downgrade(&anchors);
        let entity = cx.entity().downgrade();
        anchors.request_after_layout(move |_, window, _| {
            let Some(measured) = weak.upgrade() else {
                return;
            };
            let generation = measured.current_generation();
            // Mutate scrolling only after the complete native prepaint, not
            // while an ancestor is still painting the old scroll geometry.
            window.on_next_frame(move |window, cx| {
                let Some(anchors) = weak.upgrade() else {
                    return;
                };
                let _ = entity.update(cx, |this, cx| {
                    if window.is_window_active()
                        && anchors.current_generation() == generation
                        && this.includes_identity().as_ref() == Some(&identity)
                        && this.follow_layout_interaction() == interaction
                        && this.footnote_reader_available(cx)
                        && this
                            .follow_layout_anchors()
                            .as_ref()
                            .is_some_and(|a| std::rc::Rc::ptr_eq(a, &anchors))
                        && this.shell.includes.as_ref().is_some_and(|s| {
                            s.accepts(&identity, serial)
                                && s.inline_open == expanded
                                && this.includes_body_current(s)
                        })
                        && anchors.restore_passage(
                            &passage,
                            window.viewport_size(),
                            window.scale_factor(),
                        )
                    {
                        cx.notify();
                    }
                });
            });
        });
        cx.notify();
    }
    pub(crate) fn includes_inspector(&mut self, cx: &mut Context<Self>) -> Div {
        let mut view = div().mb_3().text_size(px(12.));
        let Some(identity) = self.includes_identity() else {
            if !self
                .shell
                .includes
                .as_ref()
                .and_then(|s| s.reading_ticket.as_ref())
                .is_some_and(|ticket| self.reading_ticket_lifetime(ticket))
            {
                self.shell.includes = None;
            }
            return view;
        };
        if self
            .shell
            .includes
            .as_ref()
            .is_none_or(|s| s.identity != identity)
        {
            self.shell.includes = Some(IncludesState::new(identity.clone()));
        }
        let serial = self.shell.includes.as_ref().unwrap().serial;
        view = view.child(self.reading_controls(&identity, serial, false, cx));
        let reading = self.shell.rendered_reading.enabled;
        let open = self.shell.includes.as_ref().unwrap().open;
        let captured = identity.clone();
        view = view.child(
            div()
                .id("includes-toggle")
                .when(!reading, |d| d.cursor_pointer())
                .child(if reading {
                    "Rendered content (session mode active)"
                } else if open {
                    "▾ Rendered content (read-only)"
                } else {
                    "▸ Rendered content (read-only)"
                })
                .on_click(cx.listener(move |this, _, _, cx| {
                    if this.shell.rendered_reading.enabled
                        || this.includes_identity().as_ref() != Some(&captured)
                        || !this.pin_keyboard_available(cx)
                        || !this.find_available()
                    {
                        return;
                    }
                    if this.inline_includes_active() || this.query_inline_active() {
                        return;
                    }
                    let Some(state) = this
                        .shell
                        .includes
                        .as_mut()
                        .filter(|s| s.identity == captured)
                    else {
                        return;
                    };
                    state.open = !state.open;
                    state.serial = SERIAL.fetch_add(1, Ordering::Relaxed);
                    state.result = None;
                    state.mindmap_cache.clear();
                    state.flowchart_cache.clear();
                    state.gallery.cancel();
                    state.viewspecs.cancel();
                    if state.open {
                        this.includes_fetch(captured.clone(), cx);
                    }
                    cx.notify();
                })),
        );
        if !open {
            return view;
        }
        let captured = identity.clone();
        view=view.child("Engine-resolved snapshots; refresh to request updated excerpts. Completeness and currentness are not guaranteed. Host Find does not search excerpts.")
            .when(!reading, |v|v.child(div().id("includes-refresh").cursor_pointer().child("Refresh rendered content").on_click(cx.listener(move |this,_,_,cx|this.includes_fetch(captured.clone(),cx)))));
        let Some(result) = &self.shell.includes.as_ref().unwrap().result else {
            return view.child("Loading rendered content…");
        };
        let entries = match result {
            Ok(snapshot) => &snapshot.response,
            Err(error) => return view.child(format!("Included notes unavailable: {error}")),
        };
        let captured = identity.clone();
        let serial = self.shell.includes.as_ref().unwrap().serial;
        let inline = self
            .shell
            .includes
            .as_ref()
            .unwrap()
            .result
            .as_ref()
            .and_then(|r| r.as_ref().ok())
            .map(|s| &s.inline);
        match inline {
            Some(Ok(rows)) if !rows.is_empty() => {
                let source = identity.clone();
                let label = if self.inline_includes_active() {
                    "Collapse inline includes"
                } else {
                    "Show includes in reader"
                };
                view = view.child(div().id("inline-includes-toggle").cursor_pointer().child(label)
                    .on_click(cx.listener(move |this, _, window, cx| this.toggle_inline_includes(source.clone(), serial, window, cx))))
                    .child("Inline excerpts are inert snapshots. Find and text selection are unavailable while expanded; collapse before refresh.");
            }
            Some(Err(error)) => {
                view = view.child(error.clone());
            }
            _ => {}
        }
        view = view.child(
            div()
                .id("includes-rendered-preview")
                .cursor_pointer()
                .child("Rendered content (read-only)")
                .on_click(cx.listener(move |this, _, window, cx| {
                    this.open_includes_preview(&captured, serial, window, cx);
                })),
        );
        view = view.when_some(self.query_inline_sidebar(cx), |view, control| {
            view.child(control)
        });
        if entries.is_empty() {
            return view.child("No included notes in this response.");
        }
        let mut rows = div()
            .id("includes-scroll")
            .max_h(px(320.))
            .overflow_y_scroll();
        for entry in &entries.entries {
            let origin = if entry.vault.is_empty() {
                "launch vault"
            } else {
                &entry.vault
            };
            let target = match entry.note_id {
                Some(id) => format!("{} #{id}", entry.kind.as_deref().unwrap_or("target")),
                None => "unknown target".into(),
            };
            let mut row = div()
                .mb_3()
                .child(format!(
                    "{} · {}",
                    entry.caption,
                    entry.title.as_deref().unwrap_or("Untitled include")
                ))
                .child(format!(
                    "{origin} · {target} · rendered host line {} · source {}",
                    entry.line + 1,
                    source_position(entry.source_line)
                ));
            if let Some(etag) = &entry.etag {
                row = row.child(format!("Snapshot revision: {etag}"));
            }
            if let Some(error) = &entry.error {
                if !error.is_empty() {
                    row = row.child(format!("Include error: {error}"));
                }
            }
            for option in &entry.bad_options {
                row = row.child(format!("Invalid option: {option}"));
            }
            rows = rows.child(row.child(div().w_full().child(entry.excerpt.clone())));
        }
        view.child(rows)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn identity() -> Identity {
        Identity {
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
        }
    }
    #[test]
    fn includes_stale_source_route_and_serial_are_rejected() {
        let id = identity();
        let mut s = IncludesState::new(id.clone());
        s.serial = 10;
        assert!(!s.accepts(&id, 10));
        s.open = true;
        assert!(s.accepts(&id, 10));
        s.open = false;
        assert!(!s.accepts(&id, 10));
        s.open = true;
        s.serial = 11;
        assert!(!s.accepts(&id, 10));
        for dimension in 0..7 {
            let mut changed = id.clone();
            match dimension {
                0 => changed.source.request += 1,
                1 => changed.source.launch_generation += 1,
                2 => changed.source.storage_generation += 1,
                3 => changed.source.etag.push('x'),
                4 => changed.source.note.note_id += 1,
                5 => changed.source.note.vault.push('x'),
                _ => changed.route.vault.clear(),
            };
            assert!(!s.accepts(&changed, 11));
        }
    }
    #[test]
    fn preview_snapshot_is_separate_and_close_does_not_replace_source() {
        let id = identity();
        let source = "raw host not returned markdown".to_string();
        let original = source.clone();
        let response = crate::included_notes::decode(serde_json::json!({
            "markdown": "# Returned host\n\n![[Target]]\n",
            "includes": [{"line": 2,"source_line": 0,"caption":"Target","lines":["# Target"],"note_id":2,"kind":"note","etag":"target"}]
        }), "work").unwrap();
        let preview = markdown::included_preview(&response);
        assert!(preview.is_ok());
        let mut state = IncludesState::new(id.clone());
        state.open = true;
        state.serial = 5;
        let queries = project_queries(&preview);
        state.result = Some(Ok(Snapshot {
            query_positions: Ok(Default::default()),
            inline: Ok(Default::default()),
            boards: Default::default(),
            response,
            preview,
            queries,
        }));
        state.preview_open = true;
        state.preview_epoch = SERIAL.fetch_add(1, Ordering::Relaxed);
        state.query_nav = Default::default();
        state.query_inline = Default::default();
        state.query_pages.clear();
        state.query_sources.clear();
        assert!(state.accepts(&id, 5));
        state.preview_open = false;
        state.copy = Default::default();
        state.gallery.cancel();
        state.viewspecs.cancel();
        state.query_nav = Default::default();
        state.query_inline = Default::default();
        state.query_pages.clear();
        state.query_sources.clear();
        assert_eq!(source, original);
        assert_eq!(state.identity, id);
        assert!(state.result.is_some());
        // Closing the inspector discards the response and makes old preview clicks invalid.
        state.open = false;
        state.result = None;
        state.mindmap_cache.clear();
        state.flowchart_cache.clear();
        assert!(!state.accepts(&id, 5));
        state.open = true;
        state.serial = 6;
        assert!(!state.accepts(&id, 5));
    }
    #[test]
    fn query_pages_reject_stale_generation_and_preserve_reachable_bounds() {
        use serde_json::json;
        let id = identity();
        for (layout, page_size) in [("list", 100usize), ("gallery", 4usize)] {
            for count in [0usize, 1, 4, 5, 100, 101, 4096] {
                let rows = (0..count)
                    .map(|i| json!({"title":i.to_string(),"cells":[]}))
                    .collect::<Vec<_>>();
                let body = format!(
                    "```track-view\n{}\n```",
                    json!({"layout":layout,"columns":[],"groups":[{"rows":rows}]})
                );
                let response =
                    crate::included_notes::decode(json!({"markdown":body,"includes":[]}), "host")
                        .unwrap();
                let preview = markdown::included_preview(&response);
                let queries = project_queries(&preview);
                let mut state = IncludesState::new(id.clone());
                state.open = true;
                state.preview_open = true;
                state.serial = 10;
                state.preview_epoch = 20;
                state.result = Some(Ok(Snapshot {
                    query_positions: Ok(Default::default()),
                    inline: Ok(Default::default()),
                    boards: Default::default(),
                    response,
                    preview,
                    queries,
                }));
                for page in 0..count.div_ceil(page_size).max(1) {
                    assert!(state.set_query_page(&id, 10, 20, (0, 0), None, page));
                }
                assert!(!state.set_query_page(
                    &id,
                    10,
                    20,
                    (0, 0),
                    None,
                    count.div_ceil(page_size).max(1)
                ));
                assert!(!state.set_query_page(&id, 10, 19, (0, 0), None, 0));
                assert!(!state.set_query_page(&id, 9, 20, (0, 0), None, 0));
                assert!(!state.set_query_page(&id, 10, 20, (0, 1), None, 0));
                assert!(!state.set_query_page(&id, 10, 20, (0, 0), Some(0), 0));
                for dimension in 0..7 {
                    let mut changed = id.clone();
                    match dimension {
                        0 => changed.source.request += 1,
                        1 => changed.source.launch_generation += 1,
                        2 => changed.source.storage_generation += 1,
                        3 => changed.source.etag.push('x'),
                        4 => changed.source.note.note_id += 1,
                        5 => changed.source.note.vault.push('x'),
                        _ => changed.route.vault.clear(),
                    };
                    assert!(!state.set_query_page(&changed, 10, 20, (0, 0), None, 0));
                }
                state.preview_open = false;
                state.copy = Default::default();
                state.gallery.cancel();
                state.viewspecs.cancel();
                assert!(!state.set_query_page(&id, 10, 20, (0, 0), None, 0));
                state.preview_open = true;
                state.preview_epoch = 21;
                assert!(!state.set_query_page(&id, 10, 20, (0, 0), None, 0));
            }
        }
    }
    #[test]
    fn calendar_pages_and_source_use_exact_snapshot_guards() {
        use serde_json::json;
        let groups = (0..128)
            .map(|i| json!({"name":format!("{:04}-{:02}-01", 2000+i/12, i%12+1),"rows":[]}))
            .collect::<Vec<_>>();
        let body = format!(
            "```track-view\n{}\n```",
            json!({"layout":"calendar","columns":[],"groups":groups})
        );
        let response =
            crate::included_notes::decode(json!({"markdown":body,"includes":[]}), "host").unwrap();
        let preview = markdown::included_preview(&response);
        let queries = project_queries(&preview);
        let id = identity();
        let mut state = IncludesState::new(id.clone());
        state.open = true;
        state.preview_open = true;
        state.serial = 10;
        state.preview_epoch = 20;
        state.result = Some(Ok(Snapshot {
            query_positions: Ok(Default::default()),
            inline: Ok(Default::default()),
            boards: Default::default(),
            response,
            preview,
            queries,
        }));
        let ready = || QueryPageGate {
            preview_active: true,
            source_matches: true,
            ..Default::default()
        };
        for page in 0..128 {
            assert!(state.query_page_action(ready(), &id, 10, 20, (0, 0), None, page));
            assert_eq!(state.query_pages[&(0, 0, None)], page);
        }
        assert!(!state.set_query_page(&id, 10, 20, (0, 0), None, 128));
        assert!(!state.set_query_page(&id, 10, 20, (0, 0), Some(0), 0));
        assert!(state.query_source_action(ready(), &id, 10, 20, (0, 0)));
        assert!(state.query_sources.contains(&(0, 0)));
        for dimension in 0..7 {
            let mut changed = id.clone();
            match dimension {
                0 => changed.source.request += 1,
                1 => changed.source.launch_generation += 1,
                2 => changed.source.storage_generation += 1,
                3 => changed.source.etag.push('x'),
                4 => changed.source.note.note_id += 1,
                5 => changed.source.note.vault.push('x'),
                _ => changed.route.vault.clear(),
            }
            assert!(!state.query_source_action(ready(), &changed, 10, 20, (0, 0)));
            assert!(!state.set_query_page(&changed, 10, 20, (0, 0), None, 0));
        }
        for (serial, epoch, key) in [(9, 20, (0, 0)), (10, 19, (0, 0)), (10, 20, (0, 1))] {
            assert!(!state.query_source_action(ready(), &id, serial, epoch, key));
            assert!(!state.set_query_page(&id, serial, epoch, key, None, 0));
        }
        for blocker in 0..11 {
            let mut gate = ready();
            match blocker {
                0 => gate.preview_active = false,
                1 => gate.source_matches = false,
                2 => gate.handoff = true,
                3 => gate.gesture = true,
                4 => gate.editing = true,
                5 => gate.pending = true,
                6 => gate.find = true,
                7 => gate.search = true,
                8 => gate.menu = true,
                9 => gate.closing = true,
                _ => gate.composing = true,
            }
            assert!(!state.query_source_action(gate, &id, 10, 20, (0, 0)));
        }
        assert!(state.query_sources.contains(&(0, 0)));
        assert!(state.query_source_action(ready(), &id, 10, 20, (0, 0)));
        assert!(state.query_sources.is_empty());
        state.preview_open = false;
        state.copy = Default::default();
        state.gallery.cancel();
        state.viewspecs.cancel();
        assert!(!state.query_source_action(ready(), &id, 10, 20, (0, 0)));
        state.preview_open = true;
        state.preview_epoch += 1;
        assert!(!state.query_source_action(ready(), &id, 10, 20, (0, 0)));
        assert!(IncludesState::new(id).query_pages.is_empty());
    }
    #[test]
    fn board_lanes_and_cards_have_independent_bounded_pages() {
        use serde_json::json;
        let rows = (0..51)
            .map(|_| json!({"title":"duplicate","cells":[]}))
            .collect::<Vec<_>>();
        let groups = (0..5)
            .map(|_| json!({"name":"duplicate","rows":rows}))
            .collect::<Vec<_>>();
        let body = format!(
            "```track-view\n{}\n```",
            json!({"layout":"board","columns":[],"groups":groups})
        );
        let response =
            crate::included_notes::decode(json!({"markdown":body,"includes":[]}), "host").unwrap();
        let preview = markdown::included_preview(&response);
        let queries = project_queries(&preview);
        let id = identity();
        let mut state = IncludesState::new(id.clone());
        state.open = true;
        state.preview_open = true;
        state.serial = 10;
        state.preview_epoch = 20;
        state.result = Some(Ok(Snapshot {
            query_positions: Ok(Default::default()),
            inline: Ok(Default::default()),
            boards: Default::default(),
            response,
            preview,
            queries,
        }));
        // Exercise the exact action path called by the UI listener: an active
        // preview is required, not rejected as it is by host Find/pin gates.
        let ready = || QueryPageGate {
            preview_active: true,
            source_matches: true,
            ..Default::default()
        };
        assert!(state.query_page_action(ready(), &id, 10, 20, (0, 0), Some(0), 1));
        for blocker in 0..11 {
            let mut gate = ready();
            match blocker {
                0 => gate.preview_active = false,
                1 => gate.source_matches = false,
                2 => gate.handoff = true,
                3 => gate.gesture = true,
                4 => gate.editing = true,
                5 => gate.pending = true,
                6 => gate.find = true,
                7 => gate.search = true,
                8 => gate.menu = true,
                9 => gate.closing = true,
                _ => gate.composing = true,
            }
            assert!(!state.query_page_action(gate, &id, 10, 20, (0, 0), Some(0), 0));
            assert_eq!(state.query_pages[&(0, 0, Some(0))], 1);
        }
        assert!(!state.query_page_action(ready(), &id, 10, 19, (0, 0), Some(0), 0));
        assert!(state.set_query_page(&id, 10, 20, (0, 0), Some(0), 1));
        assert!(!state.set_query_page(&id, 10, 20, (0, 0), Some(0), 2));
        assert!(!state.set_query_page(&id, 10, 20, (0, 0), Some(4), 0));
        assert!(state.set_query_page(&id, 10, 20, (0, 0), None, 1));
        assert!(state.set_query_page(&id, 10, 20, (0, 0), Some(4), 1));
        assert!(!state.set_query_page(&id, 10, 20, (0, 0), Some(0), 0));
        assert!(!state.set_query_page(&id, 10, 20, (0, 0), None, 2));
        assert_eq!(state.query_pages[&(0, 0, Some(0))], 1);
    }
    #[test]
    fn includes_source_position_never_overflows_or_offers_a_jump() {
        assert_eq!(source_position(-1), "noncontiguous");
        assert_eq!(source_position(0), "line 1");
        assert_eq!(source_position(i64::MAX), "line 9223372036854775808");
    }
    #[test]
    fn includes_permit_is_bounded_and_released() {
        let p = Permit::acquire().unwrap();
        assert!(Permit::acquire().is_none());
        drop(p);
        assert!(Permit::acquire().is_some());
    }
}

/// Only parsed fenced blocks from the accepted response enter this cache.
#[cfg(test)]
fn project_queries(
    preview: &Result<markdown::IncludePreview, String>,
) -> std::collections::BTreeMap<(usize, usize), Result<crate::query_view::View, String>> {
    task_boards::project(preview, Err("No accepted task source".into())).0
}

impl Workspace {
    fn query_control_gate(&self, identity: &Identity, cx: &Context<Self>) -> QueryPageGate {
        QueryPageGate {
            preview_active: self.includes_preview_active(),
            source_matches: self.includes_identity().as_ref() == Some(identity)
                && self
                    .live
                    .as_ref()
                    .is_some_and(|session| session.failure().is_none())
                && self
                    .shell
                    .includes
                    .as_ref()
                    .is_some_and(|state| self.includes_body_current(state)),
            handoff: self.handoff_active(),
            gesture: self.previews.model.geometry_active(),
            editing: self.editing_ui.active(),
            pending: self.editing_ui.has_pending(),
            find: self.shell.find.open,
            search: self.shell.search_open,
            menu: self.shell.vault_menu || self.shell.tab_menu || self.shell.diagnostics,
            closing: self.quit_session.is_closing(),
            composing: self.search.read(cx).is_composing() || self.rendered_find_composing(cx),
        }
    }
    fn query_source_button(
        &self,
        key: (usize, usize),
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<Div> {
        let state = self.shell.includes.as_ref().unwrap();
        let identity = state.identity.clone();
        let serial = state.serial;
        let epoch = state.preview_epoch;
        div()
            .id(gpui::SharedString::from(format!(
                "query-source-{serial}-{epoch}-{}-{}",
                key.0, key.1
            )))
            .cursor_pointer()
            .child(if state.query_sources.contains(&key) {
                "Hide source"
            } else {
                "Show source (all supplied rows)"
            })
            .on_click(cx.listener(move |this, _, _, cx| {
                let gate = this.query_control_gate(&identity, cx);
                let Some(state) = this.shell.includes.as_mut() else {
                    return;
                };
                if state.query_source_action(gate, &identity, serial, epoch, key) {
                    cx.notify();
                }
            }))
    }
    fn query_page_button(
        &self,
        label: &str,
        key: (usize, usize),
        group: Option<usize>,
        page: usize,
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<Div> {
        let state = self.shell.includes.as_ref().unwrap();
        let identity = state.identity.clone();
        let serial = state.serial;
        let epoch = state.preview_epoch;
        div()
            .id(gpui::SharedString::from(format!(
                "query-{serial}-{epoch}-{}-{}-{group:?}-{label}",
                key.0, key.1
            )))
            .cursor_pointer()
            .px_2()
            .child(label.to_owned())
            .on_click(cx.listener(move |this, _, _, cx| {
                let gate = this.query_control_gate(&identity, cx);
                let Some(state) = this.shell.includes.as_mut() else {
                    return;
                };
                if !state.query_page_action(gate, &identity, serial, epoch, key, group, page) {
                    return;
                }
                this.load_gallery(cx);
                cx.notify();
            }))
    }
    fn query_pager(
        &self,
        key: (usize, usize),
        group: Option<usize>,
        count: usize,
        size: usize,
        cx: &mut Context<Self>,
    ) -> Div {
        let state = self.shell.includes.as_ref().unwrap();
        let page = state
            .query_pages
            .get(&(key.0, key.1, group))
            .copied()
            .unwrap_or(0);
        let pages = count.div_ceil(size).max(1);
        let page = page.min(pages - 1);
        let start = if count == 0 { 0 } else { page * size + 1 };
        let mut row = div().flex().flex_wrap().gap_2().child(format!(
            "{start}–{} of {count} · page {}/{pages}",
            ((page + 1) * size).min(count),
            page + 1
        ));
        if page > 0 {
            row = row.child(self.query_page_button("Previous", key, group, page - 1, cx));
        }
        if page + 1 < pages {
            row = row.child(self.query_page_button("Next", key, group, page + 1, cx));
        }
        row
    }
    fn render_query_blocks(
        &self,
        item: usize,
        blocks: &[markdown::Block],
        dark: bool,
        line: u32,
        window: &mut gpui::Window,
        cx: &mut Context<Self>,
    ) -> Div {
        use crate::query_view::{CARD_PAGE, LANE_PAGE, LIST_PAGE, Layout};
        let state = self.shell.includes.as_ref().unwrap();
        let Some(Ok(snapshot)) = &state.result else {
            return div();
        };
        let mut body = div().flex().flex_col().gap_3();
        let mut ordinary_start = 0;
        for (block_index, block) in blocks.iter().enumerate() {
            let key = (item, block_index);
            if let Some(admission) = snapshot.boards.viewspecs.get(&key) {
                if ordinary_start < block_index {
                    body = body.child(self.render_preview_copy(
                        item,
                        ordinary_start,
                        &blocks[ordinary_start..block_index],
                        dark,
                        cx,
                    ));
                }
                ordinary_start = block_index + 1;
                let mut visual = div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .ml(px(block.indent as f32 * 16.));
                if block.quoted {
                    visual = visual.border_l_2().border_color(rgb(line)).pl_3();
                }
                if let Some(marker) = &block.marker {
                    visual = visual.child(marker.clone());
                }
                visual = match admission {
                    Err(reason) => visual.child(reason.clone()),
                    Ok(()) => self.viewspec_face(visual, key, dark),
                };
                body = body.child(visual.child(self.render_preview_copy(
                    item,
                    block_index,
                    std::slice::from_ref(block),
                    dark,
                    cx,
                )));
                continue;
            }
            if let Some(map) = snapshot.boards.flowcharts.get(&key) {
                if ordinary_start < block_index {
                    body = body.child(self.render_preview_copy(
                        item,
                        ordinary_start,
                        &blocks[ordinary_start..block_index],
                        dark,
                        cx,
                    ));
                }
                ordinary_start = block_index + 1;
                let mut visual = div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .ml(px(block.indent as f32 * 16.));
                if block.quoted {
                    visual = visual.border_l_2().border_color(rgb(line)).pl_3();
                }
                if let Some(marker) = &block.marker {
                    visual = visual.child(marker.clone());
                }
                visual = match map {
                    Ok(tree) => visual.child(crate::flowchart_view::render(
                        tree.clone(),
                        dark,
                        format!(
                            "flowchart-{}-{}-{item}-{block_index}",
                            state.serial, state.preview_epoch
                        ),
                        &state.flowchart_cache,
                        window,
                    )),
                    Err(reason) => visual.child(format!("Flowchart unavailable: {reason}")),
                };
                // Inert original source remains visible; raw-reader Copy is unchanged.
                body = body.child(visual.child(self.render_preview_copy(
                    item,
                    block_index,
                    std::slice::from_ref(block),
                    dark,
                    cx,
                )));
                continue;
            }
            if let Some(map) = snapshot.boards.mindmaps.get(&key) {
                if ordinary_start < block_index {
                    body = body.child(self.render_preview_copy(
                        item,
                        ordinary_start,
                        &blocks[ordinary_start..block_index],
                        dark,
                        cx,
                    ));
                }
                ordinary_start = block_index + 1;
                let mut visual = div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .ml(px(block.indent as f32 * 16.));
                if block.quoted {
                    visual = visual.border_l_2().border_color(rgb(line)).pl_3();
                }
                if let Some(marker) = &block.marker {
                    visual = visual.child(marker.clone());
                }
                visual = match map {
                    Ok(tree) => visual.child(crate::mindmap_view::render(
                        tree.clone(),
                        dark,
                        format!(
                            "mindmap-{}-{}-{item}-{block_index}",
                            state.serial, state.preview_epoch
                        ),
                        &state.mindmap_cache,
                        window,
                    )),
                    Err(reason) => visual.child(format!("Mindmap unavailable: {reason}")),
                };
                // Inert original source remains visible; raw-reader Copy is unchanged.
                body = body.child(visual.child(self.render_preview_copy(
                    item,
                    block_index,
                    std::slice::from_ref(block),
                    dark,
                    cx,
                )));
                continue;
            }
            if let Some(chart) = snapshot.boards.charts.get(&key) {
                if ordinary_start < block_index {
                    body = body.child(self.render_preview_copy(
                        item,
                        ordinary_start,
                        &blocks[ordinary_start..block_index],
                        dark,
                        cx,
                    ));
                }
                ordinary_start = block_index + 1;
                let mut visual = div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .ml(px(block.indent as f32 * 16.));
                if block.quoted {
                    visual = visual.border_l_2().border_color(rgb(line)).pl_3();
                }
                if let Some(marker) = &block.marker {
                    visual = visual.child(marker.clone());
                }
                visual = match chart {
                    Ok(chart) => {
                        visual.child(crate::numeric_chart_view::render(chart.clone(), dark))
                    }
                    Err(reason) => visual.child(format!("Native chart unavailable: {reason}")),
                };
                // Exact supplied JSON remains available through the existing inert code/Copy surface.
                visual = visual.child(self.render_preview_copy(
                    item,
                    block_index,
                    std::slice::from_ref(block),
                    dark,
                    cx,
                ));
                body = body.child(visual);
                continue;
            }
            if snapshot.boards.blocks.contains_key(&key) {
                if ordinary_start < block_index {
                    body = body.child(self.render_preview_copy(
                        item,
                        ordinary_start,
                        &blocks[ordinary_start..block_index],
                        dark,
                        cx,
                    ));
                }
                ordinary_start = block_index + 1;
                body = body.child(self.render_task_board(key, block, dark, line, cx));
                continue;
            }
            let Some(query) = snapshot.queries.get(&key) else {
                continue;
            };
            if ordinary_start < block_index {
                body = body.child(self.render_preview_copy(
                    item,
                    ordinary_start,
                    &blocks[ordinary_start..block_index],
                    dark,
                    cx,
                ));
            }
            ordinary_start = block_index + 1;
            let view = match query {
                Ok(view) => view,
                Err(reason) => {
                    body = body.child(
                        div()
                            .child(format!("Query view unavailable: {reason}"))
                            .child(self.render_preview_copy(
                                item,
                                block_index,
                                std::slice::from_ref(block),
                                dark,
                                cx,
                            )),
                    );
                    continue;
                }
            };
            let page = state
                .query_pages
                .get(&(item, block_index, None))
                .copied()
                .unwrap_or(0);
            let mut visual = div()
                .flex()
                .flex_col()
                .gap_2()
                .ml(px(block.indent as f32 * 16.));
            if block.quoted {
                visual = visual.border_l_2().border_color(rgb(line)).pl_3();
            }
            if let Some(marker) = &block.marker {
                visual = visual.child(marker.clone());
            }
            match view.layout {
                Layout::Gallery => {
                    visual = visual.child(self.query_pager(
                        key,
                        None,
                        view.total_rows(),
                        crate::query_view::GALLERY_PAGE,
                        cx,
                    ));
                    if view.total_rows() == 0 {
                        visual = visual.child("No gallery rows in this snapshot.");
                    }
                    let mut grid = div().flex().flex_wrap().gap_3().max_w(px(672.));
                    for (group_index, row_index, row) in view.gallery_page(page) {
                        let mut card = div()
                            .id(gpui::SharedString::from(format!(
                                "gallery-{}-{}-{item}-{block_index}-{group_index}-{row_index}",
                                state.serial, state.preview_epoch
                            )))
                            .w(px(320.))
                            .max_w_full()
                            .flex()
                            .flex_col()
                            .gap_2()
                            .border_1()
                            .border_color(rgb(line))
                            .p_2();
                        let mut face = div()
                            .w_full()
                            .map(|mut face| {
                                face.style().aspect_ratio = Some(16. / 9.);
                                face
                            })
                            .overflow_hidden()
                            .flex()
                            .items_center()
                            .justify_center();
                        face = match state
                            .gallery
                            .face((item, block_index, group_index, row_index))
                        {
                            Ok(Some(image)) => face.child(
                                gpui::img(image.image.clone())
                                    .size_full()
                                    .object_fit(gpui::ObjectFit::Cover),
                            ),
                            Err(reason) => face.child(reason.to_owned()),
                            Ok(None) => face.child(
                                row.icon
                                    .as_deref()
                                    .filter(|s| !s.is_empty())
                                    .unwrap_or("No image")
                                    .to_owned(),
                            ),
                        };
                        card = card.child(face);
                        let metadata = view.metadata(row);
                        if view.show_title {
                            card = card.child(row.title.clone());
                        } else if metadata.is_empty() {
                            card = card.child("Gallery card");
                        }
                        for value in metadata {
                            card = card.child(value);
                        }
                        card = card.child(self.query_navigation_control(
                            key,
                            group_index,
                            row_index,
                            cx,
                        ));
                        grid = grid.child(card);
                    }
                    visual = visual.child(grid);
                }
                Layout::List => {
                    visual =
                        visual.child(self.query_pager(key, None, view.total_rows(), LIST_PAGE, cx));
                    if view.total_rows() == 0 {
                        visual = visual.child("No query rows in this snapshot.");
                    }
                    for (index, (group_index, row_index, row)) in view
                        .groups
                        .iter()
                        .enumerate()
                        .flat_map(|(g, group)| {
                            group
                                .rows
                                .iter()
                                .enumerate()
                                .map(move |(r, row)| (g, r, row))
                        })
                        .enumerate()
                        .skip(page * LIST_PAGE)
                        .take(LIST_PAGE)
                    {
                        let mut card = div()
                            .id(gpui::SharedString::from(format!(
                                "query-list-{}-{}-{item}-{block_index}-{index}",
                                state.serial, state.preview_epoch
                            )))
                            .border_b_1()
                            .border_color(rgb(line))
                            .p_2()
                            .child(row.title.clone());
                        for text in view.metadata(row) {
                            card = card.child(div().child(text));
                        }
                        card = card.child(self.query_navigation_control(
                            key,
                            group_index,
                            row_index,
                            cx,
                        ));
                        visual = visual.child(card);
                    }
                }
                Layout::Calendar => {
                    visual = visual.child("First 3 titles per date; Open note applies only to that row. +N and day labels are inert. Source retains all supplied rows.");
                    if let Some(month) = view.calendar_page(page) {
                        visual = visual.child(month.month.label()).child(self.query_pager(
                            key,
                            None,
                            month.count,
                            1,
                            cx,
                        ));
                        let mut headings = div().flex().w_full().min_w_0();
                        for heading in ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] {
                            headings = headings.child(
                                div()
                                    .flex_1()
                                    .min_w_0()
                                    .p_1()
                                    .overflow_hidden()
                                    .child(heading),
                            );
                        }
                        let mut grid = div().flex().flex_col().w_full().min_w_0().child(headings);
                        for week in month.cells.chunks(7) {
                            let mut week_row = div().flex().w_full().min_w_0();
                            for day in week {
                                let mut cell = div()
                                    .flex_1()
                                    .min_w_0()
                                    .min_h(px(80.))
                                    .p_1()
                                    .border_1()
                                    .border_color(rgb(line))
                                    .overflow_hidden();
                                if let Some(day) = day {
                                    let mut content = div()
                                        .id(gpui::SharedString::from(format!(
                                            "query-date-{}-{}-{item}-{block_index}-{}-{}-{}",
                                            state.serial,
                                            state.preview_epoch,
                                            month.month.year,
                                            month.month.month,
                                            day.day
                                        )))
                                        .flex()
                                        .flex_col()
                                        .gap_1()
                                        .child(day.day.to_string());
                                    if !day.titles.is_empty() {
                                        cell = cell.border_t_2();
                                    }
                                    for (row_index, title) in day.titles.iter().enumerate() {
                                        content = content.child(div().id(gpui::SharedString::from(format!(
                                            "query-date-row-{}-{}-{item}-{block_index}-{}-{}-{}-{row_index}",
                                            state.serial, state.preview_epoch, month.month.year, month.month.month, day.day
                                        ))).child(div().truncate().child(if title.is_empty() { "Untitled".to_owned() } else { (*title).to_owned() }))
                                            .when_some(day.group_index, |row, group| row.child(self.query_navigation_control(key, group, row_index, cx))));
                                    }
                                    if day.excess > 0 {
                                        content = content.child(format!("+{}", day.excess));
                                    }
                                    cell = cell.child(content);
                                }
                                // Blanks have no action, date or row identity.
                                week_row = week_row.child(cell);
                            }
                            grid = grid.child(week_row);
                        }
                        visual = visual.child(grid);
                    } else {
                        visual = visual.child("No dated query rows in this snapshot");
                    }
                    visual = visual.child(self.query_source_button(key, cx));
                    if state.query_sources.contains(&key) {
                        visual = visual.child(self.render_preview_copy(
                            item,
                            block_index,
                            std::slice::from_ref(block),
                            dark,
                            cx,
                        ));
                    }
                }
                Layout::Board => {
                    visual =
                        visual.child(self.query_pager(key, None, view.groups.len(), LANE_PAGE, cx));
                    if view.groups.is_empty() {
                        visual = visual.child("No query rows in this snapshot.");
                    }
                    let mut lanes = div()
                        .id(gpui::SharedString::from(format!(
                            "query-lanes-{}-{}-{item}-{block_index}",
                            state.serial, state.preview_epoch
                        )))
                        .flex()
                        .gap_3()
                        .overflow_x_scroll();
                    for (group_index, group) in view
                        .groups
                        .iter()
                        .enumerate()
                        .skip(page * LANE_PAGE)
                        .take(LANE_PAGE)
                    {
                        let card_page = state
                            .query_pages
                            .get(&(item, block_index, Some(group_index)))
                            .copied()
                            .unwrap_or(0);
                        let mut lane = div()
                            .flex()
                            .flex_col()
                            .flex_shrink_0()
                            .w(px(250.))
                            .gap_2()
                            .p_2()
                            .border_1()
                            .border_color(rgb(line))
                            .child(format!(
                                "{} ({})",
                                group
                                    .name
                                    .as_deref()
                                    .filter(|s| !s.is_empty())
                                    .unwrap_or("Ungrouped"),
                                group.rows.len()
                            ))
                            .child(self.query_pager(
                                key,
                                Some(group_index),
                                group.rows.len(),
                                CARD_PAGE,
                                cx,
                            ));
                        if group.rows.is_empty() {
                            lane = lane.child("No query rows in this lane.");
                        }
                        for (row_index, row) in group
                            .rows
                            .iter()
                            .enumerate()
                            .skip(card_page * CARD_PAGE)
                            .take(CARD_PAGE)
                        {
                            let mut card = div().id(gpui::SharedString::from(format!("query-card-{}-{}-{item}-{block_index}-{group_index}-{row_index}",state.serial,state.preview_epoch)))
                                .flex().flex_col().gap_1().p_2().border_1().border_color(rgb(line));
                            let metadata = view.metadata(row);
                            if view.show_title {
                                card = card.child(row.title.clone());
                            }
                            if !view.show_title && metadata.is_empty() {
                                card = card.child("Empty card");
                            }
                            for text in metadata {
                                card = card.child(text);
                            }
                            card = card.child(self.query_navigation_control(
                                key,
                                group_index,
                                row_index,
                                cx,
                            ));
                            lane = lane.child(card);
                        }
                        lanes = lanes.child(lane);
                    }
                    visual = visual.child(lanes);
                }
            }
            body = body.child(visual);
        }
        if ordinary_start < blocks.len() {
            body = body.child(self.render_preview_copy(
                item,
                ordinary_start,
                &blocks[ordinary_start..],
                dark,
                cx,
            ));
        }
        body
    }
}

#[cfg(test)]
mod query_projection_tests {
    use super::*;
    use serde_json::json;
    const JSON: &str = r#"{"layout":"list","columns":["title"],"groups":[{"rows":[{"title":"<b>literal</b>","cells":["value"]}]}]}"#;
    fn fence() -> String {
        format!("```track-view\n{JSON}\n```\n")
    }
    fn project(
        body: &str,
        includes: serde_json::Value,
    ) -> (
        markdown::IncludePreview,
        std::collections::BTreeMap<(usize, usize), Result<crate::query_view::View, String>>,
    ) {
        let response =
            crate::included_notes::decode(json!({"markdown":body,"includes":includes}), "host")
                .unwrap();
        let preview = markdown::included_preview(&response);
        let queries = project_queries(&preview);
        (preview.unwrap(), queries)
    }
    #[test]
    fn exact_fence_only_and_unchanged_raw_text() {
        for body in [
            format!("````text\n{}\n````", fence()),
            format!("<pre>\n{}\n</pre>", fence()),
            format!("`track-view {JSON}`"),
            fence().replace("```track-view", "```track-query"),
            fence().replace("```track-view", "```track-view extra"),
            format!("\\`\\`\\`track-view\n{JSON}"),
        ] {
            assert!(project(&body, json!([])).1.is_empty(), "{body}");
        }
        let source = format!("{}\n# Following", fence());
        let raw = markdown::parse(&source);
        let (preview, queries) = project(&source, json!([]));
        assert_eq!(queries.len(), 1);
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert_eq!(raw, *blocks);
        assert_eq!(blocks[0].text, format!("{JSON}\n"));
        assert_eq!(blocks[1].text, "Following");
    }
    #[test]
    fn nested_positions_and_included_origin_are_retained() {
        let quoted = fence()
            .lines()
            .map(|s| format!("> {s}\n"))
            .collect::<String>();
        let (preview, queries) = project(&quoted, json!([]));
        assert_eq!(queries.len(), 1);
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert!(blocks[0].quoted);
        let list = format!(
            "- item\n\n{}",
            fence()
                .lines()
                .map(|s| format!("  {s}\n"))
                .collect::<String>()
        );
        let (preview, queries) = project(&list, json!([]));
        assert_eq!(queries.len(), 1);
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert!(
            blocks
                .iter()
                .find(|b| b.fence_info.as_deref() == Some("track-view"))
                .unwrap()
                .indent
                > 0
        );
        let (preview, queries) = project(
            "![[Target]]\n",
            json!([{"line":0,"source_line":0,"caption":"Target","lines":[fence()],"note_id":42,"kind":"note","etag":"rev"}]),
        );
        assert_eq!(queries.len(), 1);
        assert!(matches!(
            preview.items[0],
            markdown::PreviewItem::Include { index: 0, .. }
        ));
    }
    #[test]
    fn calendar_fallback_retains_returned_source_and_include_origin() {
        for name in [
            "2026-02-30",
            "0000-01-01",
            "2026-13-01",
            "2026-7-01",
            "２０２６-07-01",
            "",
        ] {
            let source = json!({"layout":"calendar","columns":[],"groups":[{"name":name,"rows":[{"title":"retained","cells":[]}]}]}).to_string();
            let (preview, queries) = project(&format!("```track-view\n{source}\n```"), json!([]));
            assert!(queries[&(0, 0)].is_err());
            let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
                panic!()
            };
            assert_eq!(blocks[0].text, format!("{source}\n"));
        }
        let calendar =
            "```track-view\n{\"layout\":\"calendar\",\"columns\":[],\"groups\":[]}\n```\n";
        let (preview, queries) = project(
            "![[Target]]\n",
            json!([{"line":0,"source_line":0,"caption":"Target","lines":[calendar],"note_id":42,"kind":"note","etag":"rev"}]),
        );
        assert_eq!(queries.len(), 1);
        assert_eq!(
            queries[&(0, 0)].as_ref().unwrap().layout,
            crate::query_view::Layout::Calendar
        );
        assert!(matches!(
            preview.items[0],
            markdown::PreviewItem::Include { index: 0, .. }
        ));
        let (preview, queries) = project(&calendar.repeat(9), json!([]));
        assert_eq!(queries.values().filter(|q| q.is_ok()).count(), 8);
        assert!(queries[&(0, 8)].is_err());
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert_eq!(blocks.len(), 9);
        assert!(blocks[8].text.contains("calendar"));
        assert!(project("_No results._", json!([])).1.is_empty());
    }
    #[test]
    fn independent_blocks_and_total_visual_limit_keep_source() {
        let (preview, queries) = project(&fence().repeat(10), json!([]));
        assert_eq!(queries.len(), 10);
        assert_eq!(queries.values().filter(|v| v.is_ok()).count(), 8);
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert_eq!(blocks.len(), 10);
        assert!(
            queries[&(0, 8)]
                .as_ref()
                .unwrap_err()
                .contains("resource limit")
        );
        let (_, queries) = project(&fence().replace("\"list\"", "\"unsupported\""), json!([]));
        assert!(queries[&(0, 0)].is_err());
    }
}
