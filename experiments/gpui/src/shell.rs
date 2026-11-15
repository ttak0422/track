//! Native one-sheet shell. Only backed actions are interactive.
use super::*;
use gpui::{Div, Focusable, PathBuilder, Stateful, WindowAppearance, canvas, point};

fn native_find_enabled(
    active_workspace: bool,
    accepted: bool,
    blocked: bool,
    allowed_focus: bool,
    step: bool,
    has_query: bool,
) -> bool {
    active_workspace && accepted && !blocked && allowed_focus && (!step || has_query)
}

fn native_reload_enabled(
    active_workspace: bool,
    ready_route: bool,
    blocked: bool,
    pending: bool,
    allowed_focus: bool,
) -> bool {
    active_workspace && ready_route && !blocked && !pending && allowed_focus
}
fn native_reload_home(home: bool, route: Option<&tabs::TabKey>, safe_home: bool) -> bool {
    home && (matches!(route, Some(tabs::TabKey::Home)) || (route.is_none() && safe_home))
}
fn reload_reader_identity(
    identity: Option<&ReaderContentIdentity>,
    launch: u64,
    storage: Option<u64>,
) -> bool {
    identity
        .and_then(|identity| identity.live.as_ref())
        .is_some_and(|(accepted_launch, accepted_storage, _)| {
            *accepted_launch == launch && storage.is_some() && *accepted_storage == storage
        })
}

fn native_history_dispatch_allowed(active: bool, workspace_window: bool, available: bool) -> bool {
    active && workspace_window && available
}

fn stale_workspace_focus(
    reader_mounted: bool,
    reader_or_code_focused: bool,
    search_mounted: bool,
    search_focused: bool,
    graph_mounted: bool,
    graph_focused: bool,
) -> bool {
    (!reader_mounted && reader_or_code_focused)
        || (!search_mounted && search_focused)
        || (!graph_mounted && graph_focused)
}

fn workspace_root_focus_allowed(active: bool, focused: bool, blocked: bool) -> bool {
    active && !focused && !blocked
}

fn native_history_enabled(
    history: &navigation::History,
    forward: bool,
    invalid_tag: bool,
    blocked: bool,
) -> bool {
    !blocked
        && if invalid_tag {
            !forward
        } else {
            history.peek(forward).is_some()
        }
}

fn tab_activation_needed(
    tabs: &tabs::Tabs,
    key: &tabs::TabKey,
    load_current_after_close: bool,
) -> bool {
    tabs.get(key).is_some()
        && (load_current_after_close || !tabs.active().is_some_and(|active| &active.key == key))
}

/// Session-local display identity, not a security hash or a persisted checkpoint.
#[derive(Debug, PartialEq, Eq)]
struct ReaderContentIdentity {
    source_hash: u64,
    source_len: usize,
    live: Option<(u64, Option<u64>, String)>,
}

impl ReaderContentIdentity {
    fn new(source: &str, live: Option<(u64, Option<u64>, String)>) -> Self {
        use std::hash::{Hash, Hasher};
        let mut hasher = std::collections::hash_map::DefaultHasher::new();
        source.hash(&mut hasher);
        Self {
            source_hash: hasher.finish(),
            source_len: source.len(),
            live,
        }
    }
}

#[derive(Default)]
struct ReadingPositions {
    source: Option<(LinkSource, tabs::TabKey)>,
    anchors: Option<std::rc::Rc<markdown::PositionAnchors>>,
    interaction: u64,
    message: String,
    queued: Option<u64>,
}

impl Workspace {
    pub(crate) fn pause_reader_for_overlay(&mut self) {
        self.cancel_position_resume();
        if let Some(anchors) = &self.shell.positions.anchors {
            anchors.cancel_after_layout();
        }
        self.shell.pending_fragment = None;
    }
    pub(crate) fn suspend_host_reader_actions(&mut self) {
        self.cancel_position_resume();
        if let Some(anchors) = &self.shell.positions.anchors {
            anchors.cancel_after_layout();
        }
        self.shell.pending_fragment = None;
        if let Some(key) = self.tabs.active().map(|tab| tab.key.clone()) {
            self.shell.detach_reader_scroll(&key);
        }
    }
    pub(crate) fn cancel_position_resume(&mut self) {
        self.shell.reader_keys = None;
        self.shell.footnote_return = None;
        self.cancel_follow_reveal();
        self.shell.positions.interaction = self.shell.positions.interaction.wrapping_add(1);
    }
    /// Transient host measurement only: no checkpoint feature or persistence is enabled.
    pub(crate) fn follow_layout_source(&self, cx: &App) -> Option<(LinkSource, tabs::TabKey)> {
        if !self.follow.controller.enabled()
            || !self.follow_safe(cx)
            || !self.find_available()
            || self.shell.find.highlighted
            || self.partial_read
            || self.read_error.is_some()
            || self.shell.pending_fragment.is_some()
            || self.previews.model.transient.is_some()
            || self.displayed_content().as_ref() != &self.content
            || self.content.is_empty()
            || self.content.len() > 4096
        {
            return None;
        }
        let source = self.current_link_source()?;
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if storage.phase != "ready" || storage.generation != source.storage_generation {
            return None;
        }
        let route = self.current_note_request.as_ref()?;
        let key = self.tabs.active()?.key.clone();
        if source.etag.is_empty() || !position_route_matches(&source, route, &key) {
            return None;
        }
        Some((source, key))
    }
    fn block_fragment_layout_source(&self, cx: &App) -> Option<(LinkSource, tabs::TabKey)> {
        let pending = self.shell.pending_fragment.as_ref()?;
        if !pending.loaded
            || self.tag_routing_blocked(cx)
            || !self.find_available()
            || self.editing_ui.active()
            || self.partial_read
            || self.read_error.is_some()
            || self.content.is_empty()
            || self.content.len() > 4096
        {
            return None;
        }
        let anchor = markdown::fragment_anchor(&self.content, &pending.intent.target).ok()??;
        if !anchor.starts_with('^') {
            return None;
        }
        let source = self.current_link_source()?;
        let key = self.tabs.active()?.key.clone();
        let route = self.current_note_request.as_ref()?;
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if !position_route_matches(&source, route, &key)
            || storage.phase != "ready"
            || storage.generation != source.storage_generation
        {
            return None;
        }
        Some((source, key))
    }
    pub(crate) fn follow_layout_anchors(&self) -> Option<std::rc::Rc<markdown::PositionAnchors>> {
        self.shell.positions.anchors.clone()
    }
    pub(crate) fn follow_layout_interaction(&self) -> u64 {
        self.shell.positions.interaction
    }
    pub(crate) fn cancel_follow_layout(&self) {
        if let Some(anchors) = &self.shell.positions.anchors {
            anchors.cancel_after_layout();
        }
    }
    fn position_source(&self) -> Result<(LinkSource, tabs::TabKey), &'static str> {
        if !self.session_ui.positions_enabled() {
            return Err("Reading checkpoints require active disposable-fixture persistence.");
        }
        if build_info::checkpoint_renderer().is_none() {
            return Err("Reading checkpoints require a known clean renderer build.");
        }
        if !self.find_available()
            || self.shell.find.open
            || self.shell.find.highlighted
            || self.partial_read
            || self.read_error.is_some()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.shell.pending_fragment.is_some()
            || self.previews.model.transient.is_some()
        {
            return Err("Reading checkpoints are unavailable while the reader is busy.");
        }
        let source = self
            .current_link_source()
            .ok_or("Open a live note first.")?;
        let storage = self
            .live
            .as_ref()
            .and_then(|live| live.initial_storage.as_ref())
            .ok_or("Storage readiness is unknown.")?;
        if storage.phase != "ready" || storage.generation != source.storage_generation {
            return Err("Storage readiness changed.");
        }
        let route = self
            .current_note_request
            .as_ref()
            .ok_or("No accepted note route.")?;
        let key = self
            .tabs
            .active()
            .map(|tab| tab.key.clone())
            .ok_or("No active note.")?;
        if !position_route_matches(&source, route, &key) || source.etag.is_empty() {
            return Err("The accepted source does not match the active note.");
        }
        // Resolved-title projections can change without a new host etag. Refuse
        // those displays rather than persisting an ambiguous parser index.
        if self.displayed_content().as_ref() != &self.content {
            return Err("Checkpoints are unavailable for projected link titles.");
        }
        if self.content.is_empty() || self.content.len() > 4096 {
            return Err("This document exceeds supported checkpoint block bounds.");
        }
        Ok((source, key))
    }

    fn position_capture(&self, window: &Window) -> Option<markdown::PositionCapture> {
        let source = self.position_source().ok()?;
        if self.shell.positions.source.as_ref() != Some(&source) {
            return None;
        }
        self.shell
            .positions
            .anchors
            .as_ref()?
            .capture(window.viewport_size(), window.scale_factor())
    }

    fn position_record(
        &self,
        capture: &markdown::PositionCapture,
    ) -> Option<session_store::ReadingPosition> {
        let (source, key) = self.position_source().ok()?;
        let tabs::TabKey::Note { vault, note_id } = key else {
            return None;
        };
        Some(session_store::ReadingPosition {
            vault,
            note_id,
            etag: source.etag,
            body_bytes: self.current_note.as_ref()?.body.len() as u64,
            renderer: build_info::checkpoint_renderer()?,
            block_index: capture.block_index,
            geometry: capture.geometry.clone(),
        })
    }

    fn save_position(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.request_position(false, window, cx);
    }

    fn resume_position(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.request_position(true, window, cx);
    }

    fn request_position(&mut self, resume: bool, window: &mut Window, cx: &mut Context<Self>) {
        self.cancel_follow_reveal();
        let Some(requested) = self
            .position_capture(window)
            .and_then(|capture| self.position_record(&capture))
        else {
            self.shell.positions.message =
                "No fresh visible host block; nothing saved or moved.".into();
            cx.notify();
            return;
        };
        let source = self.position_source().unwrap();
        let saved = if resume {
            let Some(saved) = self
                .session_ui
                .reading_position(&source.1)
                .cloned()
                .filter(|saved| position_compatible(saved, &requested, self.content.len()))
            else {
                self.shell.positions.message =
                    "Saved position does not match this revision, renderer, or geometry.".into();
                cx.notify();
                return;
            };
            Some(saved)
        } else {
            None
        };
        let anchors = self.shell.positions.anchors.as_ref().unwrap().clone();
        let anchor_weak = std::rc::Rc::downgrade(&anchors);
        let interaction = self.shell.positions.interaction;
        let weak = cx.entity().downgrade();
        // on_next_frame runs BEFORE prepaint and can see stale image/reflow
        // geometry. Both actions are consumed only by the next completed host
        // prepaint, and only if its entire measurement still matches the click.
        // Weak captures ensure removed source renderers cannot retain commands.
        anchors.request_after_layout(move |unchanged, window, cx| {
            let _ = weak.update(cx, |this, cx| {
                let Some(anchors) = anchor_weak.upgrade() else { return; };
                let valid = unchanged && position_callback_current(
                    &source, this.position_source().ok().as_ref(), interaction,
                    this.shell.positions.interaction,
                ) && this.shell.positions.anchors.as_ref()
                    .is_some_and(|now| std::rc::Rc::ptr_eq(now, &anchors));
                let capture = valid.then(|| anchors.capture(window.viewport_size(), window.scale_factor())).flatten();
                let current = capture.as_ref().and_then(|capture| this.position_record(capture));
                if current.as_ref() != Some(&requested) {
                    this.shell.positions.message = "Checkpoint action canceled because source, interaction, or layout changed.".into();
                } else if let Some(saved) = saved {
                    let applied = position_compatible(&saved, current.as_ref().unwrap(), this.content.len())
                        && anchors.apply(saved.block_index, capture.as_ref().unwrap(), window.viewport_size(), window.scale_factor());
                    this.shell.positions.message = if applied {
                        "Resumed at the saved block beginning, not the exact line."
                    } else { "Resume canceled because source, interaction, or layout changed." }.into();
                } else {
                    this.shell.positions.message = match this.session_ui.save_reading_position(
                        current.unwrap(), &this.tabs, &this.history, &this.shell.working_scope,
                    ) {
                        Ok(sequence) => {
                            this.shell.positions.queued = Some(sequence);
                            "Reading position queued; disk completion pending.".into()
                        }
                        Err(error) => error,
                    };
                }
                cx.notify();
            });
        });
        // Do not change the controls' text before measurement: that can itself
        // shift host blocks. One render is requested, never a timer/retry loop.
        cx.notify();
    }

    fn position_controls(&mut self, window: &Window, cx: &mut Context<Self>) -> Div {
        if !self.session_ui.positions_enabled() {
            return div();
        }
        if let Some(sequence) = self.shell.positions.queued {
            if self.session_ui.position_save_completed(sequence) {
                self.shell.positions.queued = None;
                self.shell.positions.message = "Reading position saved.".into();
            }
        }
        let capture = self.position_capture(window);
        let save_enabled = capture.is_some();
        let resume_enabled = capture
            .as_ref()
            .and_then(|capture| self.position_record(capture))
            .zip(
                self.tabs
                    .active()
                    .and_then(|tab| self.session_ui.reading_position(&tab.key)),
            )
            .is_some_and(|(current, saved)| {
                position_compatible(saved, &current, self.content.len())
            });
        let explanation = self
            .position_source()
            .err()
            .map(str::to_owned)
            .unwrap_or_else(|| {
                if !self.shell.positions.message.is_empty() {
                    self.shell.positions.message.clone()
                } else if capture.is_none() {
                    "No fresh visible host block is measured.".into()
                } else if self
                    .tabs
                    .active()
                    .and_then(|tab| self.session_ui.reading_position(&tab.key))
                    .is_some()
                    && !resume_enabled
                {
                    "Saved position is incompatible with this revision, renderer, or geometry."
                        .into()
                } else {
                    "Coarse block checkpoints, not exact-line resume.".into()
                }
            });
        div()
            .mb_3()
            .text_size(px(11.))
            .child(
                div()
                    .flex()
                    .h(px(24.))
                    .min_h(px(24.))
                    .overflow_hidden()
                    .whitespace_nowrap()
                    .gap_3()
                    .child(
                        div()
                            .id("save-reading-position")
                            .flex_shrink_0()
                            .opacity(if save_enabled { 1. } else { 0.4 })
                            .child("Save reading position")
                            .when(save_enabled, |button| {
                                button.cursor_pointer().on_click(
                                    cx.listener(|this, _, window, cx| {
                                        this.save_position(window, cx)
                                    }),
                                )
                            }),
                    )
                    .child(
                        div()
                            .id("resume-reading-position")
                            .flex_shrink_0()
                            .opacity(if resume_enabled { 1. } else { 0.4 })
                            .child("Resume saved position")
                            .when(resume_enabled, |button| {
                                button.cursor_pointer().on_click(cx.listener(
                                    |this, _, window, cx| this.resume_position(window, cx),
                                ))
                            }),
                    ),
            )
            // Feedback must not shift the host block after a successful jump.
            // Keep both rows' geometry fixed; long diagnostics scroll locally.
            .child(
                div()
                    .id("reading-position-status")
                    .h(px(32.))
                    .min_h(px(32.))
                    .max_h(px(32.))
                    .line_height(px(16.))
                    .overflow_y_scroll()
                    .child(explanation),
            )
    }
}

fn position_route_matches(
    source: &LinkSource,
    route: &live::NoteKey,
    active: &tabs::TabKey,
) -> bool {
    route.note_id > 0
        && source.note.note_id == route.note_id
        && (route.vault.is_empty() || source.note.vault == route.vault)
        && matches!(active, tabs::TabKey::Note {vault, note_id} if vault == &route.vault && *note_id == route.note_id)
}
fn position_callback_current(
    expected: &(LinkSource, tabs::TabKey),
    current: Option<&(LinkSource, tabs::TabKey)>,
    interaction: u64,
    current_interaction: u64,
) -> bool {
    current == Some(expected) && interaction == current_interaction
}
fn position_compatible(
    saved: &session_store::ReadingPosition,
    current: &session_store::ReadingPosition,
    blocks: usize,
) -> bool {
    saved.vault == current.vault
        && saved.note_id == current.note_id
        && saved.etag == current.etag
        && saved.body_bytes == current.body_bytes
        && saved.renderer == current.renderer
        && saved.geometry == current.geometry
        && saved.block_index < blocks
        && blocks <= 4096
}

pub struct ShellState {
    pub(crate) handoff: workspace_handoff::HandoffUi,
    positions: ReadingPositions,
    footnote_return: Option<std::rc::Rc<FootnoteReturn>>,
    reader_keys: Option<ReaderKeyOwner>,
    reader_scroll_epoch: u64,
    code_copy: code_copy::State,
    block_arrival: block_arrival::State,
    title_copy: title_copy::State,
    markdown_copy: markdown_copy::State,
    pub(crate) accepted_title_request: Option<u64>,
    code_anchors: Option<std::rc::Rc<markdown::FindAnchors>>,
    code_focus: std::collections::BTreeMap<usize, gpui::FocusHandle>,
    pub(crate) find: workspace_find::FindState,
    pub(crate) search_refinement: Option<search_refinement::Stamp>,
    relation_pages: reader_relations::Pages,
    pub(crate) recent: workspace_recent::State,
    pub(crate) includes: Option<workspace_includes::IncludesState>,
    pub(crate) note_tasks: Option<workspace_note_tasks::NoteTasksState>,
    pub(crate) hierarchy: Option<workspace_hierarchy::HierarchyState>,
    pub(crate) reader_hierarchy: Option<workspace_hierarchy::ReaderHierarchy>,
    pub(crate) open_tasks: Option<workspace_open_tasks::OpenTasksState>,
    pub(crate) weekly: Option<workspace_weekly::WeeklyState>,
    pub(crate) activity: Option<workspace_activity::ActivityState>,
    pub(crate) calendar: workspace_calendar::CalendarState,
    pub(crate) journal_day: Option<workspace_journal_day::JournalDayState>,
    pub(crate) calendar_zone: workspace_activity::CalendarZone,
    pub home: bool,
    pub search_open: bool,
    search_return_focus: Option<gpui::FocusHandle>,
    search_scroll: gpui::UniformListScrollHandle,
    pub dark: Option<bool>,
    pub diagnostics: bool,
    pub tab_menu: bool,
    pub(crate) vault_menu: bool,
    pub(crate) working_scope: String,
    pub(crate) scope_revision: u64,
    pub(crate) restored_scope_launch: Option<u64>,
    vault_generation: Option<u64>,
    vault_discovery: Option<VaultDiscovery>,
    vault_loading: bool,
    vault_error: Option<String>,
    links: Option<LinkResolutionCache>,
    navigation_notice: Option<String>,
    pending_fragment: Option<PendingFragment>,
    pub(crate) reader_focus: Option<gpui::FocusHandle>,
    workspace_focus: Option<gpui::FocusHandle>,
    reader_hover: Option<ReaderHover>,
    selection: Option<ReaderSelection>,
    selection_measurements: markdown::SelectionMeasurements,
    selection_source_cache:
        crate::selection_lines::SourceMapCache<LinkSource, std::rc::Rc<Vec<markdown::Block>>>,
    selection_serial: u64,
    selection_suppress_click: bool,
    reader_scrolls: std::collections::HashMap<tabs::TabKey, gpui::ScrollHandle>,
    reader_content: std::collections::HashMap<tabs::TabKey, ReaderContentIdentity>,
    tab_cycle: Vec<tabs::TabKey>,
    tab_cycle_index: usize,
    heading_anchors:
        std::collections::HashMap<tabs::TabKey, (String, std::rc::Rc<markdown::HeadingAnchors>)>,
}

impl ShellState {
    fn take_footnote_return(&mut self, ticket: &std::rc::Rc<FootnoteReturn>) -> bool {
        if !self
            .footnote_return
            .as_ref()
            .is_some_and(|current| std::rc::Rc::ptr_eq(current, ticket))
        {
            return false;
        }
        self.footnote_return = None;
        true
    }
    /// GPUI ScrollAnchor queues its own unguarded next-frame callback. Retire the
    /// entire handle so callbacks already queued can only move an abandoned reader.
    /// Preserve the offset in a fresh handle and rebind every host anchor namespace.
    fn detach_reader_scroll(&mut self, key: &tabs::TabKey) {
        self.reader_keys = None;
        self.reader_scroll_epoch = self.reader_scroll_epoch.wrapping_add(1);
        self.footnote_return = None;
        let replacement = gpui::ScrollHandle::new();
        if let Some(previous) = self.reader_scrolls.get(key) {
            replacement.set_offset(previous.offset());
        }
        self.reader_scrolls.insert(key.clone(), replacement.clone());
        self.heading_anchors.remove(key);
        self.code_anchors = None;
        if let Some(anchors) = self.positions.anchors.take() {
            anchors.cancel_after_layout();
        }
        self.positions.source = None;
        self.find.rebind_scroll(&replacement);
    }

    /// Called only after a source revision passed its acceptance guards. A new
    /// request alone is not a new document. Keep the handle so cached anchors still
    /// address it; explicit Find/fragment jumps run after this baseline reset.
    pub(crate) fn accept_reader_content(
        &mut self,
        key: tabs::TabKey,
        source: &str,
        live: Option<(u64, Option<u64>, String)>,
    ) {
        self.positions.interaction = self.positions.interaction.wrapping_add(1);
        let identity = ReaderContentIdentity::new(source, live);
        if self.reader_content.get(&key) != Some(&identity) {
            self.reader_scrolls
                .entry(key.clone())
                .or_default()
                .set_offset(point(px(0.), px(0.)));
            self.reader_content.insert(key, identity);
        }
    }

    fn set_working_scope(&mut self, scope: String, launch: Option<u64>) {
        self.working_scope = scope;
        self.scope_revision = self.scope_revision.wrapping_add(1);
        self.restored_scope_launch = launch;
    }
    fn initialize_working_scope(&mut self, launch: u64) {
        // The marker preserves the current value, not an older captured value.
        if self.restored_scope_launch.take() != Some(launch) {
            self.working_scope.clear();
        }
    }
}

impl Default for ShellState {
    fn default() -> Self {
        Self {
            handoff: Default::default(),
            positions: Default::default(),
            footnote_return: None,
            reader_keys: None,
            reader_scroll_epoch: 0,
            code_copy: Default::default(),
            block_arrival: Default::default(),
            title_copy: Default::default(),
            markdown_copy: Default::default(),
            accepted_title_request: None,
            code_anchors: None,
            code_focus: Default::default(),
            find: Default::default(),
            search_refinement: None,
            hierarchy: None,
            reader_hierarchy: None,
            note_tasks: None,
            recent: Default::default(),
            includes: None,
            relation_pages: Default::default(),
            weekly: None,
            open_tasks: None,
            activity: None,
            calendar: Default::default(),
            journal_day: None,
            calendar_zone: Default::default(),
            home: true,
            search_open: false,
            search_return_focus: None,
            search_scroll: Default::default(),
            dark: None,
            diagnostics: false,
            tab_menu: false,
            vault_menu: false,
            working_scope: String::new(),
            scope_revision: 0,
            restored_scope_launch: None,
            vault_generation: None,
            vault_discovery: None,
            vault_loading: false,
            vault_error: None,
            links: None,
            navigation_notice: None,
            pending_fragment: None,
            reader_focus: None,
            workspace_focus: None,
            reader_hover: None,
            selection: None,
            selection_measurements: Default::default(),
            selection_source_cache: Default::default(),
            selection_serial: 0,
            selection_suppress_click: false,
            reader_scrolls: Default::default(),
            reader_content: Default::default(),
            tab_cycle: Vec::new(),
            tab_cycle_index: 0,
            heading_anchors: Default::default(),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct InternalLinkIntent {
    pub source: live::NoteKey,
    pub destination: live::NoteKey,
    pub target: markdown::LinkTarget,
    pub launch_generation: u64,
    pub storage_generation: u64,
    pub allow_epoch_advance: bool,
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct PendingFragment {
    intent: InternalLinkIntent,
    request: u64,
    loaded: bool,
    scheduled: bool,
}
fn fragment_accepts(
    intent: &InternalLinkIntent,
    expected_request: u64,
    loaded_request: u64,
    launch: Option<u64>,
    storage: Option<u64>,
    key: &live::NoteKey,
) -> bool {
    expected_request == loaded_request
        && launch == Some(intent.launch_generation)
        && key == &intent.destination
        && storage
            .is_some_and(|epoch| epoch == intent.storage_generation || intent.allow_epoch_advance)
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct LinkSource {
    pub(crate) note: live::NoteKey,
    pub(crate) etag: String,
    pub(crate) storage_generation: u64,
    pub(crate) launch_generation: u64,
    pub(crate) request: u64,
}
fn footnote_jump_allowed(
    source: &LinkSource,
    current: Option<&LinkSource>,
    displayed: &[markdown::Block],
    current_displayed: &[markdown::Block],
    link: &markdown::Link,
    available: bool,
) -> bool {
    available
        && current == Some(source)
        && displayed == current_displayed
        && markdown::footnote_link_valid(current_displayed, link)
}

fn selection_feedback_current(
    token: u64,
    feedback: Option<u64>,
    expected_token: u64,
    expected_feedback: u64,
) -> bool {
    token == expected_token && feedback == Some(expected_feedback)
}
struct ReaderSelection {
    source: LinkSource,
    document: code_copy::Document,
    displayed: std::rc::Rc<Vec<markdown::Block>>,
    index: usize,
    geometry: FootnoteGeometry,
    offset: gpui::Point<gpui::Pixels>,
    scroll_epoch: u64,
    scope_revision: u64,
    range: reader_selection::State,
    multi: Option<MultiReaderSelection>,
    source_lines: Option<(std::sync::Arc<str>, crate::selection_lines::Segment)>,
    markdown_feedback: bool,
    measured_layout: gpui::TextLayout,
    origin: gpui::Point<gpui::Pixels>,
    moved: bool,
    token: u64,
    feedback: Option<u64>,
}
struct MultiReaderSelection {
    state: crate::selection_lines::Selection,
    segments: std::rc::Rc<[crate::selection_lines::Segment]>,
}
impl ReaderSelection {
    fn dragging(&self) -> bool {
        self.multi
            .as_ref()
            .map_or_else(|| self.range.dragging(), |m| m.state.dragging())
    }
    fn ranges(&self) -> Vec<(usize, std::ops::Range<usize>)> {
        self.multi.as_ref().map_or_else(
            || {
                self.range
                    .range()
                    .map(|r| vec![(self.index, r)])
                    .unwrap_or_default()
            },
            |m| m.state.ranges(&m.segments),
        )
    }
    fn has_range(&self) -> bool {
        !self.ranges().is_empty()
    }

    fn matches(
        &self,
        source: Option<&LinkSource>,
        document: Option<&code_copy::Document>,
        displayed: &std::rc::Rc<Vec<markdown::Block>>,
        geometry: Option<&FootnoteGeometry>,
        offset: Option<gpui::Point<gpui::Pixels>>,
        scroll_epoch: u64,
        scope: u64,
    ) -> bool {
        source == Some(&self.source)
            && document == Some(&self.document)
            && std::rc::Rc::ptr_eq(displayed, &self.displayed)
            && geometry == Some(&self.geometry)
            && offset == Some(self.offset)
            && scroll_epoch == self.scroll_epoch
            && scope == self.scope_revision
    }
}
fn selection_rect(b: gpui::Bounds<gpui::Pixels>) -> crate::selection_lines::Rect {
    crate::selection_lines::Rect {
        left: f32::from(b.left()),
        top: f32::from(b.top()),
        right: f32::from(b.right()),
        bottom: f32::from(b.bottom()),
    }
}
fn selection_clip(g: &FootnoteGeometry) -> crate::selection_lines::Rect {
    let b = selection_rect(g.reader);
    crate::selection_lines::Rect {
        left: b.left.max(0.),
        top: b.top.max(0.),
        right: b.right.min(f32::from(g.viewport.width)),
        bottom: b.bottom.min(f32::from(g.viewport.height)),
    }
}
impl Workspace {
    pub(crate) fn reader_selection_dragging(&self) -> bool {
        self.shell.selection.as_ref().is_some_and(|s| s.dragging())
    }
    pub(crate) fn clear_reader_selection(&mut self) {
        if self.reader_selection_dragging() {
            self.shell.selection_suppress_click = true;
        }
        self.shell.selection = None;
    }
    fn reader_selection_document(&self, window: &Window, cx: &App) -> Option<code_copy::Document> {
        if self.inline_includes_active() {
            return None;
        }
        if !window.is_window_active() {
            return None;
        }
        let focus = window.focused(cx).is_none()
            || self
                .shell
                .workspace_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window))
            || self
                .shell
                .reader_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window));
        focus.then(|| self.accepted_reader_document(cx)).flatten()
    }
    fn reader_selection_current(&self, window: &Window, cx: &App) -> bool {
        let Some(s) = &self.shell.selection else {
            return false;
        };
        let Some(handle) = s
            .document
            .tab
            .as_ref()
            .and_then(|tab| self.shell.reader_scrolls.get(tab))
        else {
            return false;
        };
        s.displayed
            .get(s.index)
            .is_some_and(reader_selection::eligible_display)
            && s.matches(
                self.current_link_source().as_ref(),
                self.reader_selection_document(window, cx).as_ref(),
                &self.displayed_content(),
                FootnoteGeometry::capture(handle, window).as_ref(),
                Some(handle.offset()),
                self.shell.reader_scroll_epoch,
                self.shell.scope_revision,
            )
    }
    fn sync_reader_selection(&mut self, window: &Window, cx: &App) {
        if self.shell.selection.is_some() && !self.reader_selection_current(window, cx) {
            self.shell.selection_source_cache.clear();
            self.clear_reader_selection();
        }
    }
    fn reader_selection_pointer(
        &mut self,
        source: &LinkSource,
        displayed: &std::rc::Rc<Vec<markdown::Block>>,
        index: usize,
        phase: markdown::SelectionPhase,
        layout: &gpui::TextLayout,
        position: Option<gpui::Point<gpui::Pixels>>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        use markdown::SelectionPhase;
        if self.current_link_source().as_ref() != Some(source)
            || !std::rc::Rc::ptr_eq(displayed, &self.displayed_content())
        {
            return false;
        }
        if !matches!(phase, SelectionPhase::Begin)
            && self
                .shell
                .selection
                .as_ref()
                .is_some_and(|s| s.multi.is_some())
        {
            return self.reader_multi_pointer(phase, position, window, cx);
        }
        let Some(block) = displayed
            .get(index)
            .filter(|block| reader_selection::eligible_display(block))
        else {
            return false;
        };
        if matches!(phase, SelectionPhase::Begin) {
            let Some(document) = self.reader_selection_document(window, cx) else {
                return false;
            };
            let Some(position) = position else {
                return false;
            };
            let Ok(byte) = layout.index_for_position(position) else {
                return false;
            };
            let Some(range) = reader_selection::State::begin(&block.text, byte) else {
                return false;
            };
            let Some(handle) = document
                .tab
                .as_ref()
                .and_then(|tab| self.shell.reader_scrolls.get(tab))
            else {
                return false;
            };
            let Some(geometry) = FootnoteGeometry::capture(handle, window) else {
                return false;
            };
            let offset = handle.offset();
            self.cancel_transient_preview();
            self.pause_reader_for_overlay();
            self.observe_preview_activation(window, cx);
            self.shell.selection_serial = self.shell.selection_serial.wrapping_add(1);
            self.shell.selection_suppress_click = false;
            // Styled display text has no lossless plain source mapping. It must
            // never enroll in cross-block selection or Markdown-lines Copy.
            let source_map = self
                .current_note
                .as_ref()
                .filter(|_| reader_selection::eligible(block))
                .map(|note| {
                    let segments = self.shell.selection_source_cache.get_with(
                        source,
                        &note.body,
                        displayed,
                        || markdown::plain_source_segments(&note.body, displayed),
                    );
                    (std::sync::Arc::<str>::from(note.body.as_str()), segments)
                });
            let source_lines = source_map.as_ref().and_then(|(body, map)| {
                map.iter()
                    .find(|s| s.block == index)
                    .cloned()
                    .map(|s| (body.clone(), s))
            });
            let multi = source_map.as_ref().and_then(|(_, segments)| {
                let rows = self.shell.selection_measurements.snapshot()?;
                let clip = selection_clip(&geometry);
                let visible = |i| {
                    rows.get(&i)
                        .is_some_and(|r| selection_rect(r.bounds).inside(clip))
                };
                if !visible(index)
                    || !segments.iter().any(|s| {
                        s.block != index && s.block.abs_diff(index) == 1 && visible(s.block)
                    })
                {
                    return None;
                }
                Some(MultiReaderSelection {
                    state: crate::selection_lines::Selection::begin(
                        segments,
                        crate::selection_lines::Point { block: index, byte },
                    )?,
                    segments: segments.clone(),
                })
            });
            self.shell.selection = Some(ReaderSelection {
                source: source.clone(),
                document,
                displayed: displayed.clone(),
                index,
                geometry,
                offset,
                scroll_epoch: self.shell.reader_scroll_epoch,
                scope_revision: self.shell.scope_revision,
                range,
                source_lines,
                multi,
                markdown_feedback: false,
                measured_layout: layout.clone(),
                origin: position,
                moved: false,
                token: self.shell.selection_serial,
                feedback: None,
            });
            cx.notify();
            return true;
        }
        if !self
            .shell
            .selection
            .as_ref()
            .is_some_and(|s| s.index == index && s.dragging())
        {
            return false;
        }
        if !self.reader_selection_current(window, cx) {
            self.clear_reader_selection();
            cx.notify();
            return true;
        }
        let selection = self.shell.selection.as_mut().unwrap();
        if !matches!(phase, SelectionPhase::Leave) {
            selection.moved |= position.is_none_or(|p| p != selection.origin);
        }
        let hit_layout = if matches!(phase, SelectionPhase::Leave) {
            &selection.measured_layout
        } else {
            layout
        };
        let byte = position.and_then(|p| hit_layout.index_for_position(p).ok());
        let valid = match phase {
            SelectionPhase::Move => selection.range.update(&block.text, byte),
            SelectionPhase::End => selection.range.finish(&block.text, byte),
            SelectionPhase::Leave => {
                if !selection.range.leave(&block.text, byte) {
                    return false;
                }
                false
            }
            SelectionPhase::Cancel | SelectionPhase::EndOutside => {
                selection.range.cancel();
                false
            }
            SelectionPhase::Begin => unreachable!(),
        };
        self.shell.selection_suppress_click |= selection.moved || !valid;
        if !valid || (!selection.dragging() && selection.range.range().is_none()) {
            self.shell.selection = None;
        }
        cx.notify();
        true
    }
    fn reader_multi_pointer(
        &mut self,
        phase: markdown::SelectionPhase,
        position: Option<gpui::Point<gpui::Pixels>>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        use markdown::SelectionPhase;
        if !self
            .shell
            .selection
            .as_ref()
            .is_some_and(|s| s.multi.is_some() && s.dragging())
        {
            return false;
        }
        if !self.reader_selection_current(window, cx) {
            self.clear_reader_selection();
            cx.notify();
            return true;
        }
        // Hover-leave can fire during a repaint before the reader commits all
        // measurements. Do not use an older generation or invent movement then.
        let rows = self.shell.selection_measurements.snapshot();
        if matches!(phase, SelectionPhase::Leave) && rows.is_none() {
            return false;
        }
        let selected = self.shell.selection.as_ref().unwrap();
        let clip = selection_clip(&selected.geometry);
        let point = position.and_then(|position| {
            let rows = rows.as_ref()?;
            let measured: Vec<_> = rows
                .iter()
                .map(|(i, r)| (*i, selection_rect(r.bounds)))
                .collect();
            let hit = crate::selection_lines::hit(
                &measured,
                clip,
                (f32::from(position.x), f32::from(position.y)),
            )?;
            let p = match hit {
                crate::selection_lines::Hit::Text(block) => crate::selection_lines::Point {
                    block,
                    byte: rows.get(&block)?.layout.index_for_position(position).ok()?,
                },
                crate::selection_lines::Hit::Gap(block) => crate::selection_lines::Point {
                    block,
                    byte: selected
                        .multi
                        .as_ref()?
                        .segments
                        .iter()
                        .find(|s| s.block == block)?
                        .text
                        .len(),
                },
            };
            // No hidden/offscreen/intervening blocks, even for fast pointer jumps.
            for i in selected.index.min(p.block)..=selected.index.max(p.block) {
                if !rows
                    .get(&i)
                    .is_some_and(|r| selection_rect(r.bounds).inside(clip))
                {
                    return None;
                }
            }
            Some(p)
        });
        if matches!(phase, SelectionPhase::Leave) && point.is_some() {
            return false;
        }
        let selected = self.shell.selection.as_mut().unwrap();
        selected.moved |= position.is_none_or(|p| p != selected.origin);
        let m = selected.multi.as_mut().unwrap();
        let valid = if matches!(phase, SelectionPhase::Cancel | SelectionPhase::Leave) {
            m.state.cancel();
            false
        } else {
            m.state.update(
                &m.segments,
                point,
                matches!(phase, SelectionPhase::End | SelectionPhase::EndOutside),
            )
        };
        self.shell.selection_suppress_click |= selected.moved || !valid;
        if !valid || (!selected.dragging() && !selected.has_range()) {
            self.shell.selection = None;
        }
        cx.notify();
        true
    }

    fn copy_reader_selection(&mut self, token: u64, window: &Window, cx: &mut Context<Self>) {
        if !self.reader_selection_current(window, cx) {
            self.clear_reader_selection();
            cx.notify();
            return;
        }
        let Some(s) = self.shell.selection.as_ref().filter(|s| s.token == token) else {
            return;
        };
        let payload = s.multi.as_ref().map_or_else(
            || {
                s.range
                    .payload(&s.displayed[s.index].text)
                    .ok()
                    .map(str::to_owned)
            },
            |m| m.state.plain(&m.segments),
        );
        let Some(payload) = payload else {
            self.shell.navigation_notice = Some(
                "Selection copy refused: select a released nonempty range within 256 KiB.".into(),
            );
            cx.notify();
            return;
        };
        cx.write_to_clipboard(gpui::ClipboardItem::new_string(payload));
        self.shell.selection_serial = self.shell.selection_serial.wrapping_add(1);
        let feedback = self.shell.selection_serial;
        self.shell.selection.as_mut().unwrap().feedback = Some(feedback);
        self.shell.selection.as_mut().unwrap().markdown_feedback = false;
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(std::time::Duration::from_millis(1500))
                .await;
            let _ =
                this.update(cx, |this, cx| {
                    if let Some(s) = this.shell.selection.as_mut().filter(|s| {
                        selection_feedback_current(s.token, s.feedback, token, feedback)
                    }) {
                        s.feedback = None;
                        cx.notify();
                    }
                });
        })
        .detach();
    }
    fn selected_markdown_payload(&self, token: u64, window: &Window, cx: &App) -> Option<String> {
        let range = self.selected_source_range(token, window, cx)?;
        self.current_note
            .as_ref()?
            .body
            .get(range)
            .map(str::to_owned)
    }
    fn selected_source_range(
        &self,
        token: u64,
        window: &Window,
        cx: &App,
    ) -> Option<std::ops::Range<usize>> {
        if !self.reader_selection_current(window, cx) {
            return None;
        }
        let selected = self
            .shell
            .selection
            .as_ref()
            .filter(|s| s.token == token && !s.dragging())?;
        let (source, segment) = selected.source_lines.as_ref()?;
        if self.current_note.as_ref()?.body.as_str() != source.as_ref() {
            return None;
        }
        if let Some(m) = &selected.multi {
            return m.state.source_range(&m.segments, source);
        }
        let range = selected.range.range()?;
        crate::selection_lines::single_source_range(source, segment, range)
    }
    fn selected_reference_payload(&self, token: u64, window: &Window, cx: &App) -> Option<String> {
        let range = self.selected_source_range(token, window, cx)?;
        let note = self.current_note.as_ref()?;
        let path = note.note.additional.get("copy_path")?.as_str()?;
        crate::selection_lines::source_reference(path, &note.body, range)
    }
    fn selected_reference_control(
        &self,
        muted: u32,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let token = self.shell.selection.as_ref().map_or(0, |s| s.token);
        let enabled = self.selected_reference_payload(token, window, cx).is_some();
        div()
            .id("copy-selected-source-reference")
            .w(px(210.))
            .flex_shrink_0()
            .text_size(px(12.))
            .line_height(px(22.))
            .text_color(rgb(muted))
            .opacity(if enabled { 1. } else { 0.5 })
            .child("Copy source reference")
            .when(enabled, |el| el.cursor_pointer())
            .on_mouse_down(
                gpui::MouseButton::Left,
                cx.listener(move |this, _, window, cx| {
                    if this.selected_reference_payload(token, window, cx).is_some() {
                        cx.stop_propagation();
                    }
                }),
            )
            .on_click(cx.listener(move |this, _, window, cx| {
                if let Some(payload) = this.selected_reference_payload(token, window, cx) {
                    cx.write_to_clipboard(gpui::ClipboardItem::new_string(payload));
                    this.shell.navigation_notice =
                        Some("Source reference submitted to clipboard.".into());
                    cx.notify();
                }
                cx.stop_propagation();
            }))
    }
    fn copy_selected_markdown(&mut self, token: u64, window: &Window, cx: &mut Context<Self>) {
        let Some(payload) = self.selected_markdown_payload(token, window, cx) else {
            self.shell.navigation_notice = Some("Markdown line copy unavailable: select a released plain paragraph with exact source correspondence (256 KiB maximum).".into());
            cx.notify();
            return;
        };
        cx.write_to_clipboard(gpui::ClipboardItem::new_string(payload));
        self.shell.selection_serial = self.shell.selection_serial.wrapping_add(1);
        let feedback = self.shell.selection_serial;
        if let Some(s) = self.shell.selection.as_mut().filter(|s| s.token == token) {
            s.feedback = Some(feedback);
            s.markdown_feedback = true;
        }
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(std::time::Duration::from_millis(1500))
                .await;
            let _ =
                this.update(cx, |this, cx| {
                    if let Some(s) = this.shell.selection.as_mut().filter(|s| {
                        selection_feedback_current(s.token, s.feedback, token, feedback)
                    }) {
                        s.feedback = None;
                        cx.notify();
                    }
                });
        })
        .detach();
    }
    fn selected_markdown_control(
        &self,
        muted: u32,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let token = self.shell.selection.as_ref().map_or(0, |s| s.token);
        let enabled = self.reader_selection_current(window, cx)
            && self
                .shell
                .selection
                .as_ref()
                .is_some_and(|s| !s.dragging() && s.has_range() && s.source_lines.is_some());
        div()
            .id("copy-selected-markdown-lines")
            .w(px(210.))
            .flex_shrink_0()
            .text_size(px(12.))
            .line_height(px(22.))
            .text_color(rgb(muted))
            .opacity(if enabled { 1. } else { 0.5 })
            .when(enabled, |el| el.cursor_pointer())
            .child(
                if self
                    .shell
                    .selection
                    .as_ref()
                    .is_some_and(|s| s.feedback.is_some() && s.markdown_feedback)
                {
                    "Markdown lines submitted"
                } else {
                    "Copy selected Markdown lines"
                },
            )
            .on_mouse_down(
                gpui::MouseButton::Left,
                cx.listener(move |this, _, window, cx| {
                    if this.reader_selection_current(window, cx)
                        && this
                            .shell
                            .selection
                            .as_ref()
                            .is_some_and(|s| s.token == token && !s.dragging())
                    {
                        cx.stop_propagation();
                    }
                }),
            )
            .on_click(cx.listener(move |this, _, window, cx| {
                this.copy_selected_markdown(token, window, cx);
                cx.stop_propagation();
            }))
    }

    fn selection_copy_control(
        &self,
        muted: u32,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let selection = self.shell.selection.as_ref();
        let token = selection.map(|s| s.token).unwrap_or(0);
        let enabled = self.reader_selection_current(window, cx)
            && selection.is_some_and(|s| !s.dragging() && s.has_range());
        div()
            .id("copy-reader-selection")
            .w(px(155.))
            .flex_shrink_0()
            .text_size(px(12.))
            .line_height(px(22.))
            .text_color(rgb(muted))
            .opacity(if enabled { 1. } else { 0.5 })
            .when(enabled, |el| el.cursor_pointer())
            .child(
                if selection.is_some_and(|s| s.feedback.is_some() && !s.markdown_feedback) {
                    "Copy submitted"
                } else {
                    "Copy selection"
                },
            )
            .on_mouse_down(
                gpui::MouseButton::Left,
                cx.listener(move |this, _, window, cx| {
                    if this.reader_selection_current(window, cx)
                        && this
                            .shell
                            .selection
                            .as_ref()
                            .is_some_and(|s| s.token == token && !s.dragging())
                    {
                        cx.stop_propagation();
                    }
                }),
            )
            .on_click(cx.listener(move |this, _, window, cx| {
                this.copy_reader_selection(token, window, cx);
                cx.stop_propagation();
            }))
    }
}

struct ReaderHover {
    source: LinkSource,
    document: code_copy::Document,
    displayed: std::rc::Rc<Vec<markdown::Block>>,
    id: String,
    link: markdown::Link,
    target: live::NoteKey,
    geometry: FootnoteGeometry,
    offset: gpui::Point<gpui::Pixels>,
    interaction: u64,
    scroll_epoch: u64,
    scope_revision: u64,
    layout: gpui::TextLayout,
    rendered_range: std::ops::Range<usize>,
}
impl ReaderHover {
    fn source_matches(
        &self,
        source: Option<&LinkSource>,
        document: Option<&code_copy::Document>,
        displayed: &std::rc::Rc<Vec<markdown::Block>>,
        interaction: u64,
        scroll_epoch: u64,
        scope_revision: u64,
        offset: Option<gpui::Point<gpui::Pixels>>,
    ) -> bool {
        source == Some(&self.source)
            && document == Some(&self.document)
            && std::rc::Rc::ptr_eq(&self.displayed, displayed)
            && interaction == self.interaction
            && scroll_epoch == self.scroll_epoch
            && scope_revision == self.scope_revision
            && offset == Some(self.offset)
    }
}
impl Workspace {
    pub(crate) fn reader_hover_active(&self) -> bool {
        self.shell.reader_hover.is_some()
    }
    pub(crate) fn clear_reader_hover_owner(&mut self) {
        self.shell.reader_hover = None;
    }
    fn reader_hover_document(&self, window: &Window, cx: &App) -> Option<code_copy::Document> {
        if self.shell.selection.is_some()
            || !window.is_window_active()
            || !self.previews.model.pinned.is_empty()
        {
            return None;
        }
        let focus_allowed = window.focused(cx).is_none()
            || self
                .shell
                .reader_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window))
            || self
                .shell
                .workspace_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window));
        if !focus_allowed {
            return None;
        }
        self.accepted_reader_document_for(cx, self.reader_hover_active())
    }
    pub(crate) fn reader_hover_source_current(&self, cx: &App) -> bool {
        let Some(owner) = &self.shell.reader_hover else {
            return true;
        };
        owner.source_matches(
            self.current_link_source().as_ref(),
            self.accepted_reader_document_for(cx, true).as_ref(),
            &self.displayed_content(),
            self.shell.positions.interaction,
            self.shell.reader_scroll_epoch,
            self.shell.scope_revision,
            owner
                .document
                .tab
                .as_ref()
                .and_then(|tab| self.shell.reader_scrolls.get(tab))
                .map(|handle| handle.offset()),
        )
    }
    fn reader_hover_current(&self, window: &Window, cx: &App) -> bool {
        let Some(owner) = &self.shell.reader_hover else {
            return false;
        };
        let Some(tab) = &owner.document.tab else {
            return false;
        };
        let Some(handle) = self.shell.reader_scrolls.get(tab) else {
            return false;
        };
        self.reader_hover_source_current(cx)
            && self.reader_hover_document(window, cx).as_ref() == Some(&owner.document)
            && self.current_link_source().as_ref() == Some(&owner.source)
            && std::rc::Rc::ptr_eq(&owner.displayed, &self.displayed_content())
            && FootnoteGeometry::capture(handle, window).as_ref() == Some(&owner.geometry)
            && handle.offset() == owner.offset
            && self.shell.positions.interaction == owner.interaction
            && self.shell.reader_scroll_epoch == owner.scroll_epoch
            && self.shell.scope_revision == owner.scope_revision
            && markdown::resolve_term(&owner.link.target)
                .and_then(|term| self.shell.links.as_ref()?.resolved.get(&term))
                .is_some_and(|target| target == &owner.target)
    }
    fn reader_link_hover(
        &mut self,
        source: &LinkSource,
        displayed: &std::rc::Rc<Vec<markdown::Block>>,
        id: &str,
        link: Option<&markdown::Link>,
        event: Option<&gpui::MouseMoveEvent>,
        layout: Option<(&gpui::TextLayout, usize)>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if self.current_link_source().as_ref() != Some(source)
            || !std::rc::Rc::ptr_eq(displayed, &self.displayed_content())
        {
            return;
        }
        let link = link.filter(|_| event.is_some_and(|event| event.pressed_button.is_none()));
        let Some(link) = link else {
            if let Some(owner) = self
                .shell
                .reader_hover
                .as_ref()
                .filter(|owner| owner.id == id && owner.source == *source)
            {
                let inside = event.is_some_and(|event| {
                    event.pressed_button.is_none()
                        && owner
                            .layout
                            .index_for_position(event.position)
                            .ok()
                            .is_some_and(|index| owner.rendered_range.contains(&index))
                });
                if inside {
                    let target = graph_preview::Target {
                        vault: owner.target.vault.clone(),
                        note_id: owner.target.note_id,
                    };
                    let point = event.unwrap().position;
                    self.previews.model.hover(
                        target,
                        (f32::from(point.x), f32::from(point.y)),
                        self.previews.now(),
                    );
                } else {
                    self.previews.model.leave_opener(self.previews.now());
                }
                self.schedule_preview(cx);
                cx.notify();
            }
            return;
        };
        let Some(document) = self.reader_hover_document(window, cx) else {
            return;
        };
        if markdown::is_local_fragment(&link.target)
            || external_links::destination(&link.target).is_some()
        {
            return;
        }
        let Some(term) = markdown::resolve_term(&link.target) else {
            return;
        };
        let Some(target) = self
            .shell
            .links
            .as_ref()
            .filter(|cache| cache.source == *source && !cache.pending)
            .and_then(|cache| cache.resolved.get(&term))
            .cloned()
        else {
            return;
        };
        let Some((layout, offset_in_text)) = layout else {
            return;
        };
        if let Some(owner) = self
            .shell
            .reader_hover
            .as_mut()
            .filter(|owner| owner.id == id && owner.link == *link && owner.target == target)
        {
            owner.layout = layout.clone();
            let point = event.unwrap().position;
            self.previews.model.hover(
                graph_preview::Target {
                    vault: target.vault,
                    note_id: target.note_id,
                },
                (f32::from(point.x), f32::from(point.y)),
                self.previews.now(),
            );
            self.schedule_preview(cx);
            cx.notify();
            return;
        }
        let Some(handle) = document
            .tab
            .as_ref()
            .and_then(|tab| self.shell.reader_scrolls.get(tab))
        else {
            return;
        };
        let Some(geometry) = FootnoteGeometry::capture(handle, window) else {
            return;
        };
        let offset = handle.offset();
        self.cancel_transient_preview();
        self.shell.reader_hover = Some(ReaderHover {
            source: source.clone(),
            document,
            displayed: displayed.clone(),
            id: id.into(),
            link: link.clone(),
            target: target.clone(),
            geometry,
            offset,
            interaction: self.shell.positions.interaction,
            scroll_epoch: self.shell.reader_scroll_epoch,
            scope_revision: self.shell.scope_revision,
            layout: layout.clone(),
            rendered_range: link.range.start + offset_in_text..link.range.end + offset_in_text,
        });
        self.previews.model.set_scope(graph_preview::Scope {
            session: source.launch_generation,
            graph: source.request,
            vault: target.vault.clone(),
        });
        let event = event.unwrap();
        self.previews.model.hover(
            graph_preview::Target {
                vault: target.vault,
                note_id: target.note_id,
            },
            (f32::from(event.position.x), f32::from(event.position.y)),
            self.previews.now(),
        );
        self.observe_preview_activation(window, cx);
        self.schedule_preview(cx);
        cx.notify();
    }
    fn sync_reader_hover(&mut self, window: &Window, cx: &mut Context<Self>) {
        if !self.reader_hover_active() {
            return;
        }
        if !self.reader_hover_current(window, cx) {
            self.cancel_transient_preview();
            return;
        }
        if self.previews.reader_due
            && self.previews.model.transient.is_none()
            && !self.shell.reader_hover.as_ref().is_some_and(|owner| {
                owner
                    .layout
                    .index_for_position(window.mouse_position())
                    .ok()
                    .is_some_and(|index| owner.rendered_range.contains(&index))
            })
        {
            self.cancel_transient_preview();
            return;
        }
        if self.previews.reader_due {
            self.previews.reader_due = false;
            if let Some(read) = self.previews.model.advance(self.previews.now()) {
                self.read_graph_preview(read, cx);
            }
            self.schedule_preview(cx);
        }
    }
}

struct ReaderKeyOwner {
    source: LinkSource,
    tab: tabs::TabKey,
    displayed: std::rc::Rc<Vec<markdown::Block>>,
    geometry: FootnoteGeometry,
    epoch: u64,
}
impl ReaderKeyOwner {
    fn matches(
        &self,
        source: Option<&LinkSource>,
        tab: Option<&tabs::TabKey>,
        displayed: &[markdown::Block],
        epoch: u64,
        focused: bool,
        available: bool,
    ) -> bool {
        available
            && focused
            && source == Some(&self.source)
            && tab == Some(&self.tab)
            && displayed == self.displayed.as_ref()
            && epoch == self.epoch
    }
}
fn plain_click_admitted(
    left: bool,
    down_count: usize,
    up_count: usize,
    stationary: bool,
    unmodified: bool,
) -> bool {
    left && down_count == 1 && up_count == 1 && stationary && unmodified
}
pub(crate) fn plain_reader_click(event: &gpui::ClickEvent) -> bool {
    let gpui::ClickEvent::Mouse(event) = event else {
        return false;
    };
    let unmodified =
        |m: gpui::Modifiers| !m.shift && !m.control && !m.alt && !m.platform && !m.function;
    plain_click_admitted(
        event.down.button == gpui::MouseButton::Left && event.up.button == gpui::MouseButton::Left,
        event.down.click_count,
        event.up.click_count,
        event.down.position == event.up.position,
        unmodified(event.down.modifiers) && unmodified(event.up.modifiers),
    )
}
impl Workspace {
    fn reader_key_available(&self, cx: &App) -> bool {
        self.footnote_reader_available(cx)
            && self.editing_ui.plain_reader_mode()
            && !self.shell.find.is_composing(cx)
            && !self.follow.controller.enabled()
    }
    fn reader_key_owner_current(&self, owner: &ReaderKeyOwner, window: &Window, cx: &App) -> bool {
        owner.matches(
            self.current_link_source().as_ref(),
            self.tabs.active().map(|tab| &tab.key),
            &self.displayed_content(),
            self.shell.reader_scroll_epoch,
            self.shell
                .reader_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window)),
            self.reader_key_available(cx),
        ) && self
            .current_note_request
            .as_ref()
            .is_some_and(|route| position_route_matches(&owner.source, route, &owner.tab))
            && window.viewport_size() == owner.geometry.viewport
            && window.scale_factor() == owner.geometry.scale
    }
    fn claim_reader_keys(
        &mut self,
        source: &LinkSource,
        displayed: &std::rc::Rc<Vec<markdown::Block>>,
        index: usize,
        event: &gpui::ClickEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        // GPUI may deliver the row's ClickEvent before its mouse-up listener.
        // A genuinely stationary empty click retains the existing j/k focus path.
        if self
            .shell
            .selection
            .as_ref()
            .is_some_and(|s| !s.moved && s.range.range().is_none())
            && plain_reader_click(event)
        {
            self.shell.selection = None;
        }
        if self.shell.selection_suppress_click {
            self.shell.selection_suppress_click = false;
            return;
        }
        if self.shell.selection.is_some() {
            return;
        }
        if !plain_reader_click(event)
            || !self.reader_key_available(cx)
            || self.current_link_source().as_ref() != Some(source)
            || self.displayed_content().as_ref() != displayed.as_ref()
            || !displayed
                .get(index)
                .is_some_and(markdown::plain_reader_block)
        {
            return;
        }
        let Some(tab) = self.tabs.active().map(|tab| tab.key.clone()) else {
            return;
        };
        if !self
            .current_note_request
            .as_ref()
            .is_some_and(|route| position_route_matches(source, route, &tab))
        {
            return;
        }
        let Some(geometry) = self
            .shell
            .reader_scrolls
            .get(&tab)
            .and_then(|handle| FootnoteGeometry::capture(handle, window))
        else {
            return;
        };
        // Retire already-queued anchor callbacks before taking manual ownership.
        // The replacement handle keeps the current offset and lays out next frame.
        self.suspend_host_reader_actions();
        if let Some(focus) = &self.shell.reader_focus {
            window.focus(focus);
        }
        self.shell.reader_keys = Some(ReaderKeyOwner {
            source: source.clone(),
            tab,
            displayed: displayed.clone(),
            geometry,
            epoch: self.shell.reader_scroll_epoch,
        });
        cx.notify();
    }
    fn reader_scroll_keyboard(
        &mut self,
        event: &gpui::KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        if self.shell.selection.is_some() {
            return false;
        }
        let m = event.keystroke.modifiers;
        let Some(direction) = reader_scroll::direction(
            &event.keystroke.key,
            m.shift,
            m.control,
            m.alt,
            m.platform,
            m.function,
        ) else {
            return false;
        };
        let Some(owner) = &self.shell.reader_keys else {
            return false;
        };
        if !self.reader_key_owner_current(owner, window, cx) {
            self.shell.reader_keys = None;
            return false;
        }
        let Some(handle) = self.shell.reader_scrolls.get(&owner.tab) else {
            return false;
        };
        if FootnoteGeometry::capture(handle, window).as_ref() != Some(&owner.geometry) {
            self.shell.reader_keys = None;
            return false;
        }
        let current = handle.offset();
        let Some(y) = reader_scroll::offset(
            f32::from(current.y),
            f32::from(handle.max_offset().height),
            Some(markdown::BODY_LINE_HEIGHT),
            direction,
        ) else {
            return false;
        };
        // Root's manual-input path already cancels follow/position resumption.
        // No deferred callback, history entry, request or input-handler dispatch.
        handle.set_offset(point(current.x, px(y)));
        window.refresh();
        cx.notify();
        true
    }
}

/// One explicit local return, never a route-history entry or persisted position.
struct FootnoteReturn {
    source: LinkSource,
    tab: tabs::TabKey,
    displayed: std::rc::Rc<Vec<markdown::Block>>,
    anchors: std::rc::Rc<markdown::HeadingAnchors>,
    interaction: u64,
    geometry: FootnoteGeometry,
    origin: gpui::Point<gpui::Pixels>,
}
impl FootnoteReturn {
    fn matches(
        &self,
        source: Option<&LinkSource>,
        tab: Option<&tabs::TabKey>,
        displayed: &[markdown::Block],
        anchors: Option<&std::rc::Rc<markdown::HeadingAnchors>>,
        geometry: Option<&FootnoteGeometry>,
        available: bool,
    ) -> bool {
        available
            && source == Some(&self.source)
            && tab == Some(&self.tab)
            && displayed == self.displayed.as_ref()
            && anchors.is_some_and(|anchors| std::rc::Rc::ptr_eq(anchors, &self.anchors))
            && geometry == Some(&self.geometry)
    }
}
#[derive(Clone, Debug, PartialEq)]
struct FootnoteGeometry {
    reader: gpui::Bounds<gpui::Pixels>,
    extent: gpui::Size<gpui::Pixels>,
    viewport: gpui::Size<gpui::Pixels>,
    scale: f32,
}
impl FootnoteGeometry {
    fn capture(handle: &gpui::ScrollHandle, window: &Window) -> Option<Self> {
        let geometry = Self {
            reader: handle.bounds(),
            extent: handle.max_offset(),
            viewport: window.viewport_size(),
            scale: window.scale_factor(),
        };
        geometry.valid().then_some(geometry)
    }
    fn valid(&self) -> bool {
        [
            self.reader.origin.x,
            self.reader.origin.y,
            self.extent.width,
            self.extent.height,
        ]
        .into_iter()
        .all(|v| f32::from(v).is_finite())
            && [
                self.reader.size.width,
                self.reader.size.height,
                self.viewport.width,
                self.viewport.height,
            ]
            .into_iter()
            .all(|v| f32::from(v).is_finite() && v > px(0.))
            && self.extent.width >= px(0.)
            && self.extent.height >= px(0.)
            && self.scale.is_finite()
            && self.scale > 0.
    }
    fn bounded_origin(
        &self,
        origin: gpui::Point<gpui::Pixels>,
    ) -> Option<gpui::Point<gpui::Pixels>> {
        if !self.valid() || !f32::from(origin.x).is_finite() || !f32::from(origin.y).is_finite() {
            return None;
        }
        Some(point(
            origin.x.max(-self.extent.width).min(px(0.)),
            origin.y.max(-self.extent.height).min(px(0.)),
        ))
    }
}
impl Workspace {
    pub(crate) fn footnote_reader_available(&self, cx: &App) -> bool {
        self.footnote_reader_available_for(cx, false)
    }
    fn footnote_reader_available_for(&self, cx: &App, own_preview: bool) -> bool {
        self.find_available()
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && self.tag_page.is_none()
            && !self.shell.find.open
            && !self.shell.find.highlighted
            && !self.partial_read
            && self.read_error.is_none()
            && !self.quit_session.is_closing()
            && self.quit_session.failure().is_none()
            && self.shell.pending_fragment.is_none()
            && (own_preview || self.previews.model.transient.is_none())
            && !self.previews.model.geometry_active()
            && !self.search.read(cx).is_composing()
            && !self.shell.calendar_zone.is_composing(cx)
    }
    fn footnote_return_current(&self, ticket: &FootnoteReturn, window: &Window, cx: &App) -> bool {
        ticket.matches(
            self.current_link_source().as_ref(),
            self.tabs.active().map(|tab| &tab.key),
            &self.displayed_content(),
            self.shell
                .heading_anchors
                .get(&ticket.tab)
                .map(|(_, anchors)| anchors),
            self.shell
                .reader_scrolls
                .get(&ticket.tab)
                .and_then(|handle| FootnoteGeometry::capture(handle, window))
                .as_ref(),
            self.footnote_reader_available(cx),
        )
    }
    fn footnote_jump_pending_current(
        &self,
        ticket: &FootnoteReturn,
        window: &Window,
        cx: &App,
    ) -> bool {
        self.shell.positions.interaction == ticket.interaction
            && self.footnote_return_current(ticket, window, cx)
    }
    fn return_from_footnote(
        &mut self,
        ticket: &std::rc::Rc<FootnoteReturn>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !self.shell.take_footnote_return(ticket) {
            return; // a stale control cannot consume a newer return
        }
        let valid = self.footnote_return_current(ticket, window, cx);
        if valid {
            if let Some(origin) = ticket.geometry.bounded_origin(ticket.origin) {
                self.cancel_position_resume();
                if let Some(handle) = self.shell.reader_scrolls.get(&ticket.tab) {
                    handle.set_offset(origin);
                }
                window.refresh();
            }
        }
        cx.notify();
    }
}

struct LinkResolutionCache {
    source: LinkSource,
    properties: std::rc::Rc<crate::note_properties::Projection>,
    pending: bool,
    deferred_targets: usize,
    canceled: std::sync::Arc<std::sync::atomic::AtomicBool>,
    resolved: std::collections::HashMap<String, live::NoteKey>,
    displayed: std::rc::Rc<Vec<markdown::Block>>,
    warnings: Vec<String>,
}
impl LinkResolutionCache {
    fn displayed_for(
        &self,
        source: Option<&LinkSource>,
    ) -> Option<std::rc::Rc<Vec<markdown::Block>>> {
        (source == Some(&self.source)).then(|| self.displayed.clone())
    }
    fn update_display(
        &mut self,
        authored: &[markdown::Block],
        titles: &std::collections::HashMap<String, String>,
    ) {
        // Compose host-only visual projections; the authored snapshot/targets stay intact.
        self.displayed = std::rc::Rc::new(markdown::with_footnote_numbers(
            &markdown::with_resolved_titles(authored, titles),
        ));
    }
}
impl Drop for LinkResolutionCache {
    fn drop(&mut self) {
        self.canceled
            .store(true, std::sync::atomic::Ordering::Release);
    }
}
pub(crate) fn uncertain_resolution(
    fields: &std::collections::BTreeMap<String, serde_json::Value>,
) -> bool {
    fn uncertain(value: &serde_json::Value) -> bool {
        let Some(object) = value.as_object() else {
            return false;
        };
        object.iter().any(|(key, value)| match key.as_str() {
            "partial" | "cache_used" => {
                value.as_bool() == Some(true) || value.as_array().is_some_and(|v| !v.is_empty())
            }
            "unavailable" | "issues" => value.as_array().is_some_and(|v| !v.is_empty()),
            "omitted" => value.as_u64().is_some_and(|v| v > 0),
            "storage_state" => value.as_str().is_some_and(|v| v != "ready"),
            "read_state" => uncertain(value),
            _ => false,
        })
    }
    uncertain(&serde_json::to_value(fields).unwrap_or_default())
}
fn internal_terms(blocks: &[markdown::Block]) -> Vec<String> {
    let mut terms = std::collections::BTreeSet::new();
    for block in blocks {
        for link in &block.links {
            if let Some(term) = markdown::resolve_term(&link.target) {
                terms.insert(term);
            }
        }
        if let Some(table) = &block.table {
            for row in &table.rows {
                for cell in row {
                    for link in &cell.links {
                        if let Some(term) = markdown::resolve_term(&link.target) {
                            terms.insert(term);
                        }
                    }
                }
            }
        }
    }
    terms.into_iter().collect()
}
fn property_callback_current(
    source: &LinkSource,
    current: Option<&LinkSource>,
    property: &crate::note_properties::Property,
    props: &serde_json::Value,
    verified: bool,
) -> bool {
    verified && current == Some(source) && property.current(props)
}
fn property_internal_terms(
    blocks: &[markdown::Block],
    properties: &crate::note_properties::Projection,
) -> Vec<String> {
    let mut terms: std::collections::BTreeSet<_> = internal_terms(blocks).into_iter().collect();
    terms.extend(
        properties
            .rows
            .iter()
            .filter_map(|row| row.target.as_ref().and_then(markdown::resolve_term)),
    );
    terms.into_iter().collect()
}
fn has_authored_fragment(target: &markdown::LinkTarget) -> bool {
    match target {
        markdown::LinkTarget::Footnote { .. } => false,
        markdown::LinkTarget::Wiki { fragment, .. } => fragment
            .as_ref()
            .is_some_and(|fragment| !fragment.is_empty()),
        markdown::LinkTarget::Markdown { destination } => destination
            .split_once('#')
            .is_some_and(|(_, fragment)| !fragment.is_empty()),
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct VaultChoice {
    name: String,
    error: Option<String>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct VaultDiscovery {
    launch_name: String,
    available: Vec<VaultChoice>,
    unavailable: Vec<VaultChoice>,
}
impl VaultDiscovery {
    /// Membership is independent of which registered peer views are open yet.
    /// This must not be used as the available-for-navigation list.
    fn session_membership(&self) -> Result<Vec<String>, String> {
        if self.unavailable.iter().any(|entry| entry.name.is_empty()) {
            return Err("Unavailable peer has no registered name".into());
        }
        if !self
            .available
            .iter()
            .any(|entry| entry.name == self.launch_name)
        {
            return Err("Launch vault is absent from available registry".into());
        }
        let mut names = self
            .available
            .iter()
            .chain(&self.unavailable)
            .map(|entry| entry.name.clone())
            .collect::<Vec<_>>();
        names.push(String::new());
        names.sort();
        names.dedup();
        Ok(names)
    }

    fn parse(value: &serde_json::Value) -> Result<Self, String> {
        if value
            .get("storage_state")
            .and_then(|v| v.as_str())
            .is_some_and(|phase| phase != "ready")
        {
            return Err("Vault discovery is incomplete. Retry after storage is ready.".into());
        }
        let launch_name = value
            .get("active")
            .and_then(|v| v.get("name"))
            .and_then(|v| v.as_str())
            .ok_or("Missing launch-vault identity")?
            .to_owned();
        let mut names = std::collections::HashSet::new();
        let mut parse_rows =
            |rows: &serde_json::Value, unavailable: bool| -> Result<Vec<VaultChoice>, String> {
                rows.as_array()
                    .ok_or("Invalid vault list")?
                    .iter()
                    .map(|row| {
                        let name = row
                            .get("name")
                            .and_then(|v| v.as_str())
                            .ok_or("Missing vault name")?
                            .to_owned();
                        if !names.insert(name.clone()) {
                            return Err("Ambiguous duplicate vault names".into());
                        }
                        let error = if unavailable {
                            Some(
                                row.get("error")
                                    .and_then(|v| v.as_str())
                                    .unwrap_or("Unavailable")
                                    .to_owned(),
                            )
                        } else {
                            None
                        };
                        Ok(VaultChoice { name, error })
                    })
                    .collect()
            };
        let available = parse_rows(value.get("vaults").ok_or("Missing vault list")?, false)?;
        let unavailable = parse_rows(
            value
                .get("unavailable")
                .unwrap_or(&serde_json::Value::Array(vec![])),
            true,
        )?;
        if available.is_empty() {
            return Err("No verified available vaults in this response".into());
        }
        Ok(Self {
            launch_name,
            available,
            unavailable,
        })
    }
    fn visible(&self) -> bool {
        self.available.len() != 1
            || !self.launch_name.is_empty()
            || !self.unavailable.is_empty()
            || !self.available[0].name.is_empty()
    }
    fn scope_for(&self, name: &str) -> Option<String> {
        self.available
            .iter()
            .find(|entry| entry.name == name)
            .map(|_| {
                if name == self.launch_name {
                    String::new()
                } else {
                    name.to_owned()
                }
            })
    }
    fn label(&self, scope: &str) -> String {
        let name = if scope.is_empty() {
            &self.launch_name
        } else {
            scope
        };
        if name.is_empty() {
            "local".into()
        } else {
            name.into()
        }
    }
    fn reconcile_scope(&self, scope: &str) -> String {
        if scope.is_empty() {
            String::new()
        } else {
            self.scope_for(scope).unwrap_or_default()
        }
    }
}

#[derive(Clone, Copy)]
struct Palette {
    bg: u32,
    paper: u32,
    ink: u32,
    muted: u32,
    line: u32,
    mark: u32,
}
impl Palette {
    fn new(dark: bool) -> Self {
        if dark {
            Self {
                bg: 0x141618,
                paper: 0x191c1e,
                ink: 0xe9e9e4,
                muted: 0xa2a29b,
                line: 0x282c2f,
                mark: 0xf4785e,
            }
        } else {
            Self {
                bg: 0xfbfaf8,
                paper: 0xffffff,
                ink: 0x1a1a18,
                muted: 0x5e5d58,
                line: 0xe6e4de,
                mark: 0xc13a1e,
            }
        }
    }
}

// Literal text only. Constrain each label and wrap the badge row; do not create
// per-character elements or alter user text to manufacture break opportunities.
fn metadata_text(value: &str) -> Div {
    div().min_w_0().max_w_full().child(value.to_owned())
}

fn metadata_badge(flag: &crate::note_metadata::Flag, p: Palette) -> Div {
    use crate::note_metadata::Flag;
    let (text, border) = match flag {
        Flag::Deprecated => (p.mark, p.mark),
        Flag::Confidential => (p.ink, p.mark),
        Flag::Other(_) => (p.muted, p.line),
    };
    div()
        .min_w_0()
        .max_w_full()
        .border_1()
        .border_color(rgb(border))
        .rounded_md()
        .px_2()
        .py_1()
        .text_color(rgb(text))
        .child(metadata_text(flag.label()))
}

fn rail_button(id: &'static str, glyph: &'static str, p: Palette, active: bool) -> Stateful<Div> {
    let color = if active { p.mark } else { p.muted };
    div()
        .id(id)
        .w(px(40.))
        .h(px(40.))
        .flex()
        .items_center()
        .justify_center()
        .rounded_md()
        .cursor_pointer()
        .child(
            canvas(
                |_, _, _| (),
                move |bounds, _, window, _| {
                    let mut path = PathBuilder::stroke(px(1.5));
                    let at = |x: f32, y: f32| {
                        point(
                            bounds.left() + px(x * 20. / 24.),
                            bounds.top() + px(y * 20. / 24.),
                        )
                    };
                    let segments: Vec<Vec<(f32, f32)>> = match glyph {
                        "home" => vec![
                            vec![(3., 11.), (12., 3.), (21., 11.)],
                            vec![
                                (5., 10.),
                                (5., 21.),
                                (10., 21.),
                                (10., 14.),
                                (14., 14.),
                                (14., 21.),
                                (19., 21.),
                                (19., 10.),
                            ],
                        ],
                        "search" => {
                            let mut ring: Vec<_> = (0..=32)
                                .map(|i| {
                                    let a = i as f32 * std::f32::consts::TAU / 32.;
                                    (10.5 + 6.5 * a.cos(), 10.5 + 6.5 * a.sin())
                                })
                                .collect();
                            ring.push((15.1, 15.1));
                            vec![ring, vec![(15.1, 15.1), (21., 21.)]]
                        }
                        "info" => vec![vec![(12., 5.), (12., 6.)], vec![(12., 10.), (12., 19.)]],
                        "reload" => {
                            let arc = (0..=24)
                                .map(|i| {
                                    let a = 0.6 + i as f32 * 5.0 / 24.;
                                    (12. + 8. * a.cos(), 12. + 8. * a.sin())
                                })
                                .collect();
                            vec![arc, vec![(21., 3.), (21., 9.), (15., 9.)]]
                        }
                        _ => {
                            let ring = (0..=32)
                                .map(|i| {
                                    let a = i as f32 * std::f32::consts::TAU / 32.;
                                    (12. + 8. * a.cos(), 12. + 8. * a.sin())
                                })
                                .collect();
                            vec![ring, vec![(12., 4.), (12., 20.)]]
                        }
                    };
                    for points in segments {
                        for (i, (x, y)) in points.into_iter().enumerate() {
                            if i == 0 {
                                path.move_to(at(x, y));
                            } else {
                                path.line_to(at(x, y));
                            }
                        }
                    }
                    if let Ok(path) = path.build() {
                        window.paint_path(path, rgb(color));
                    }
                },
            )
            .w(px(20.))
            .h(px(20.)),
        )
}

fn label(text: &'static str, p: Palette) -> Div {
    div()
        .font_family("monospace")
        .text_size(px(10.))
        .text_color(rgb(p.muted))
        .mb_3()
        .child(text)
}
fn display_date(seconds: i64) -> Option<String> {
    let seconds: libc::time_t = seconds.try_into().ok()?;
    let mut date: libc::tm = unsafe { std::mem::zeroed() };
    if unsafe { libc::localtime_r(&seconds, &mut date) }.is_null() {
        return None;
    }
    Some(format!(
        "{:04}-{:02}-{:02}",
        date.tm_year + 1900,
        date.tm_mon + 1,
        date.tm_mday
    ))
}
// A relation belongs to its source note's vault unless the server explicitly
// provides another vault. Null/non-string vaults and non-positive IDs are inert.
fn reference_key(value: &serde_json::Value, inherited_vault: &str) -> Option<live::NoteKey> {
    let note_id = value.get("note_id")?.as_i64().filter(|id| *id > 0)?;
    let vault = match value.get("vault") {
        None => inherited_vault,
        Some(value) => value.as_str()?,
    };
    Some(live::NoteKey {
        vault: vault.to_owned(),
        note_id,
    })
}
fn reference_row(
    value: &serde_json::Value,
    inherited_vault: Option<&str>,
    id: (&'static str, usize),
    source: Option<LinkSource>,
    scope_revision: u64,
    section: &'static str,
    cx: &mut Context<Workspace>,
) -> Option<Stateful<Div>> {
    let title = crate::note_metadata::relation_title(
        value.get("title")?.as_str()?,
        &crate::note_metadata::relation_flags(value),
    );
    let key = inherited_vault.and_then(|scope| reference_key(value, scope));
    let captured = value.clone();
    Some(div().id(id).py_1().child(title).when_some(key, |row, key| {
        row.cursor_pointer()
            .on_click(cx.listener(move |this, _, window, cx| {
                if source.as_ref().is_some_and(|source| {
                    reader_relations::allows(
                        source,
                        this.relation_source().as_ref(),
                        this.current_related.get(section),
                        id.1,
                        &captured,
                        this.relation_control_available(scope_revision, window, cx),
                    )
                }) {
                    this.open_live_key(key.clone(), cx);
                }
            }))
    }))
}
// show_graph only controls the reader's aside, never replaces the article.
// Keep it explicit here so the availability regression covers both aside states.
fn fragment_layout_current(
    source: bool,
    tab: bool,
    handle: bool,
    interaction: bool,
    viewport: bool,
    anchors: bool,
) -> bool {
    source && tab && handle && interaction && viewport && anchors
}
fn code_copy_available(
    reader_available: bool,
    find_open: bool,
    read_failed: bool,
    _aside_graph: bool,
) -> bool {
    reader_available && !find_open && !read_failed
}
fn cycle_index(index: usize, len: usize, backwards: bool) -> usize {
    if len == 0 {
        return 0;
    }
    if backwards {
        (index + len - 1) % len
    } else {
        (index + 1) % len
    }
}
impl Workspace {
    fn accepted_reader_document(&self, cx: &App) -> Option<code_copy::Document> {
        self.accepted_reader_document_for(cx, false)
    }
    fn accepted_reader_document_for(
        &self,
        cx: &App,
        own_preview: bool,
    ) -> Option<code_copy::Document> {
        if self.shell.accepted_title_request != Some(self.request)
            || !self.footnote_reader_available_for(cx, own_preview)
            || if own_preview {
                self.reader_preview_routing_blocked(cx)
            } else {
                self.tag_routing_blocked(cx)
            }
            || self.editor_shortcut_blocked(cx)
            || self.shell.find.is_composing(cx)
            || (self.session_ui.recovery_enabled()
                && (!self.session_ui.recovery_ready()
                    || self.session_ui.recovery_record().is_some()
                    || self.session_ui.recovery_status()
                        != workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Empty,
                        )))
        {
            return None;
        }
        let source = self.current_link_source()?;
        let note = self.current_note.as_ref()?;
        let request = self.current_note_request.as_ref()?;
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if &source.note != request
            || note.note.key() != *request
            || storage.phase != "ready"
            || storage.generation != source.storage_generation
            || self.tabs.active()?.key
                != (tabs::TabKey::Note {
                    vault: request.vault.clone(),
                    note_id: request.note_id,
                })
        {
            return None;
        }
        self.copy_document()
    }
    fn copy_title_source(&self, cx: &App) -> Option<title_copy::Source> {
        title_copy::Source::new(
            self.accepted_reader_document(cx)?,
            &self.current_note.as_ref()?.note.title,
        )
    }
    fn copy_title(&mut self, epoch: u64, window: &Window, cx: &mut Context<Self>) {
        if !window.is_window_active() {
            return;
        }
        let current = self.copy_title_source(cx);
        let Some(title) = self.shell.title_copy.text(epoch, current.as_ref()) else {
            return;
        };
        // GPUI has no write-result API. This acknowledges submission, never readback.
        cx.write_to_clipboard(gpui::ClipboardItem::new_string(title));
        let serial = self.shell.title_copy.submitted();
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(std::time::Duration::from_millis(1500))
                .await;
            let _ = this.update(cx, |this, cx| {
                let source = this.copy_title_source(cx);
                this.shell.title_copy.sync(source);
                if this.shell.title_copy.expire(epoch, serial) {
                    cx.notify();
                }
            });
        })
        .detach();
    }
    fn sync_markdown_copy(&mut self, cx: &App) {
        let document = self.accepted_reader_document(cx);
        let body = self.current_note.as_ref().map(|note| note.body.as_str());
        self.shell.markdown_copy.sync(document, body);
    }
    fn copy_markdown(&mut self, epoch: u64, window: &Window, cx: &mut Context<Self>) {
        if !self.native_find_available(false, window, cx) {
            return;
        }
        let document = self.accepted_reader_document(cx);
        let body = self.current_note.as_ref().map(|note| note.body.as_str());
        let Some(payload) = self
            .shell
            .markdown_copy
            .payload(epoch, document.as_ref(), body)
        else {
            return;
        };
        let payload = match payload {
            Ok(payload) => payload,
            Err(error) => {
                self.shell.navigation_notice = Some(error.into());
                cx.notify();
                return;
            }
        };
        // Submission only: GPUI provides no success/failure receipt or readback.
        cx.write_to_clipboard(gpui::ClipboardItem::new_string(payload));
        let serial = self.shell.markdown_copy.submitted();
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(std::time::Duration::from_millis(1500))
                .await;
            let _ = this.update(cx, |this, cx| {
                this.sync_markdown_copy(cx);
                if this.shell.markdown_copy.expire(epoch, serial) {
                    cx.notify();
                }
            });
        })
        .detach();
    }
    fn copy_markdown_control(
        &self,
        muted: u32,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let enabled =
            self.shell.markdown_copy.ready() && self.native_find_available(false, window, cx);
        let epoch = self.shell.markdown_copy.epoch;
        div()
            .id("copy-reader-markdown")
            .w(px(145.))
            .flex_shrink_0()
            .text_size(px(12.))
            .line_height(px(22.))
            .text_color(rgb(muted))
            .opacity(if enabled { 1. } else { 0.5 })
            .when(enabled, |button| button.cursor_pointer())
            .child(if self.shell.markdown_copy.oversized {
                "Copy MD >256 KiB"
            } else if self.shell.markdown_copy.feedback.is_some() {
                "Copy submitted"
            } else {
                "Copy MD"
            })
            .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_click(cx.listener(move |this, _, window, cx| {
                this.copy_markdown(epoch, window, cx);
                cx.stop_propagation();
            }))
    }
    fn block_arrival_source(&self, index: usize, cx: &App) -> Option<block_arrival::Source> {
        // Reuse accepted-reader identity admission, including pending reads, full
        // source/tab/storage readiness and competing input/recovery/modal owners.
        let document = self.accepted_reader_document(cx)?;
        let id = self.content.get(index)?.block_id.clone()?;
        Some(block_arrival::Source {
            document,
            index,
            id,
        })
    }
    fn sync_block_arrival(&mut self, cx: &App) {
        let current = self
            .shell
            .block_arrival
            .active
            .as_ref()
            .and_then(|ticket| self.block_arrival_source(ticket.source.index, cx));
        self.shell.block_arrival.retain(current.as_ref());
    }
    fn show_block_arrival(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(source) = self.block_arrival_source(index, cx) else {
            return;
        };
        let ticket = self.shell.block_arrival.show(source);
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(std::time::Duration::from_millis(1500))
                .await;
            let _ = this.update(cx, |this, cx| {
                this.sync_block_arrival(cx);
                if this.shell.block_arrival.expire(&ticket) {
                    cx.notify();
                }
            });
        })
        .detach();
    }
    fn copy_document(&self) -> Option<code_copy::Document> {
        code_copy_available(
            self.find_available(),
            self.shell.find.open,
            self.read_error.is_some(),
            self.show_graph,
        )
        .then(|| code_copy::Document {
            tab: self.tabs.active().map(|tab| tab.key.clone()),
            request: self.request,
            etag: self.current_note.as_ref().map(|note| note.etag.clone()),
            storage: self.accepted_storage_generation,
            launch: self
                .live
                .as_ref()
                .map(|live| live.client().launch_generation),
        })
    }
    fn copy_code(&mut self, epoch: u64, index: usize, cx: &mut Context<Self>) {
        let Some(item) =
            self.shell
                .code_copy
                .clipboard_item(epoch, index, &self.copy_document(), &self.content)
        else {
            return;
        };
        // GPUI offers no failure result: this acknowledges submission only. Never read back.
        cx.write_to_clipboard(item);
        let serial = self.shell.code_copy.submitted(index);
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(std::time::Duration::from_millis(1500))
                .await;
            let _ = this.update(cx, |this, cx| {
                let document = this.copy_document();
                if this.shell.code_copy.sync(document, &this.content) {
                    this.shell.code_focus.clear();
                    this.shell.code_anchors = None;
                }
                if this.shell.code_copy.expire(epoch, index, serial) {
                    cx.notify();
                }
            });
        })
        .detach();
    }
    fn code_controls(
        &mut self,
        scroll: &gpui::ScrollHandle,
        cx: &mut Context<Self>,
    ) -> markdown::CodeCallbacks {
        let document = self.copy_document();
        if self.shell.code_copy.sync(document, &self.content) {
            self.shell.code_focus.clear();
            self.shell.code_anchors = None;
        }
        if self.shell.code_copy.document.is_some() {
            self.shell.code_anchors.get_or_insert_with(|| {
                std::rc::Rc::new(markdown::FindAnchors::new(&self.content, scroll))
            });
            for (index, block) in self.shell.code_copy.blocks.iter().enumerate() {
                if block.language.is_some() {
                    self.shell
                        .code_focus
                        .entry(index)
                        .or_insert_with(|| cx.focus_handle());
                }
            }
        }
        let controls = self
            .shell
            .code_focus
            .iter()
            .map(|(&index, focus)| {
                (
                    index,
                    (
                        focus.clone(),
                        self.shell.code_copy.feedback.contains_key(&index),
                    ),
                )
            })
            .collect();
        let epoch = self.shell.code_copy.epoch;
        let weak = cx.entity().downgrade();
        markdown::CodeCallbacks {
            labels: None,
            anchors: self.shell.code_anchors.clone(),
            controls,
            activate: std::rc::Rc::new(move |index, _, cx| {
                let _ = weak.update(cx, |this, cx| this.copy_code(epoch, index, cx));
            }),
        }
    }
    fn code_keyboard(
        &mut self,
        event: &gpui::KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        if self.copy_document().is_none() {
            return false;
        }
        let current = self
            .shell
            .code_focus
            .values()
            .position(|focus| focus.is_focused(window));
        let reader_focused = self
            .shell
            .reader_focus
            .as_ref()
            .is_some_and(|focus| focus.is_focused(window));
        if current.is_none() && !reader_focused {
            return false;
        }
        let modifiers = event.keystroke.modifiers;
        if event.keystroke.key == "escape" && current.is_some() {
            if let Some(focus) = &self.shell.reader_focus {
                window.focus(focus);
            }
            cx.notify();
            return true;
        }
        if event.keystroke.key == "c" && modifiers.alt && !modifiers.platform && !modifiers.control
        {
            let handles: Vec<_> = self.shell.code_focus.iter().collect();
            if handles.is_empty() {
                return false;
            }
            let next = current.map_or(
                if modifiers.shift {
                    handles.len() - 1
                } else {
                    0
                },
                |index| cycle_index(index, handles.len(), modifiers.shift),
            );
            let (&index, focus) = handles[next];
            if !self.shell.code_copy.valid_block(
                self.shell.code_copy.epoch,
                index,
                &self.copy_document(),
                &self.content,
            ) {
                return false;
            }
            self.shell.positions.interaction = self.shell.positions.interaction.wrapping_add(1);
            let revealed = self.shell.code_anchors.as_ref().is_some_and(|anchors| {
                anchors.scroll_to(&reader_find::Target::Block(index), window, cx)
            });
            if !revealed {
                return false;
            }
            window.focus(focus);
            cx.notify();
            return true;
        }
        false
    }
    pub(crate) fn displayed_content(&self) -> std::rc::Rc<Vec<markdown::Block>> {
        self.shell
            .links
            .as_ref()
            .and_then(|cache| cache.displayed_for(self.current_link_source().as_ref()))
            .unwrap_or_else(|| std::rc::Rc::new(self.content.clone()))
    }
    // A retained body during Reload is not a newly accepted relation snapshot.
    fn relation_source(&self) -> Option<LinkSource> {
        let source = self.current_link_source()?;
        let route = self.current_note_request.as_ref()?;
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        let route_current = route.note_id == source.note.note_id
            && self.gallery_scope(&route.vault).is_some_and(|vault| {
                self.gallery_scope(&source.note.vault).as_ref() == Some(&vault)
            })
            && self.tabs.active().is_some_and(|tab| {
                tab.key
                    == tabs::TabKey::Note {
                        vault: route.vault.clone(),
                        note_id: route.note_id,
                    }
            });
        reader_relations::accepted(
            &source,
            self.shell.accepted_title_request,
            route_current,
            storage.phase == "ready" && storage.generation == source.storage_generation,
        )
        .then_some(source)
    }
    fn relation_control_available(&self, scope: u64, window: &Window, cx: &App) -> bool {
        window.is_window_active()
            && reader_relations::scope_current(scope, self.shell.scope_revision)
            && self.find_available()
            && !self.reader_preview_routing_blocked(cx)
            && !self.reader_selection_dragging()
            && !self.shell.find.is_composing(cx)
            && self.read_error.is_none()
            && !self.quit_session.is_closing()
            && self.quit_session.failure().is_none()
    }
    fn relation_section(&mut self, section: &'static str, cx: &mut Context<Self>) -> Div {
        let mut view = div().text_size(px(12.));
        let source = self.relation_source();
        let scope_revision = self.shell.scope_revision;
        let requested = self.shell.relation_pages.current(source.as_ref(), section);
        let rows = match reader_relations::entries(self.current_related.get(section)) {
            Ok(None) => return view,
            Ok(Some(rows)) => rows,
            Err(error) => return view.mt_6().child(section.to_uppercase()).child(error),
        };
        view = view.mt_6().child(section.to_uppercase());
        let summary = reader_relations::summary(
            section,
            rows.len(),
            workspace_activity::incomplete(&self.current_related),
        );
        if rows.is_empty() {
            return view.child(summary);
        }
        let (page, pages, range) = reader_relations::page(rows.len(), requested);
        let mut controls =
            div()
                .flex()
                .gap_2()
                .child(format!("{summary} · {}/{}", page + 1, pages));
        for (label, target) in [
            ("Previous", page.checked_sub(1)),
            ("Next", (page + 1 < pages).then_some(page + 1)),
        ] {
            if let (Some(target), Some(captured)) = (target, source.clone()) {
                controls = controls.child(
                    div()
                        .id(gpui::SharedString::from(format!("{section}-{label}")))
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if this.relation_source().as_ref() == Some(&captured)
                                && this.relation_control_available(scope_revision, window, cx)
                            {
                                this.shell.relation_pages.set(&captured, section, target);
                                cx.notify();
                            }
                        })),
                );
            }
        }
        let inherited = source.as_ref().map(|s| s.note.vault.as_str());
        let rows_view = div()
            .id(gpui::SharedString::from(reader_relations::scroll_id(
                section,
                source.as_ref(),
                page,
            )))
            .max_h(px(240.))
            .overflow_y_scroll()
            .children(range.filter_map(|index| {
                reference_row(
                    &rows[index],
                    inherited,
                    (
                        if section == "children" {
                            "child-reference"
                        } else {
                            "backlink-reference"
                        },
                        index,
                    ),
                    source.clone(),
                    scope_revision,
                    section,
                    cx,
                )
            }));
        view.child(controls).child(rows_view)
    }
    pub(crate) fn gallery_scope(&self, scope: &str) -> Option<String> {
        if self.shell.vault_generation
            != self
                .live
                .as_ref()
                .map(|session| session.client().launch_generation)
        {
            return None;
        }
        let discovery = self.shell.vault_discovery.as_ref()?;
        let name = if scope.is_empty() {
            &discovery.launch_name
        } else {
            scope
        };
        discovery
            .available
            .iter()
            .find(|v| v.name == name)
            .map(|v| v.name.clone())
    }
    pub(crate) fn follow_registry(&self) -> Option<(String, Vec<String>)> {
        if self.shell.vault_loading
            || self.shell.vault_error.is_some()
            || self.shell.vault_generation
                != self.live.as_ref().map(|s| s.client().launch_generation)
        {
            return None;
        }
        let discovery = self.shell.vault_discovery.as_ref()?;
        let mut names: Vec<_> = discovery.available.iter().map(|v| v.name.clone()).collect();
        if !names.contains(&discovery.launch_name) {
            return None;
        }
        names.sort();
        Some((discovery.launch_name.clone(), names))
    }
    pub(crate) fn gallery_scope_verified(&self, scope: &str) -> bool {
        self.gallery_scope(scope).is_some()
    }
    pub(crate) fn current_link_source(&self) -> Option<LinkSource> {
        if self.handoff_active() {
            return None;
        }
        let note = self.current_note.as_ref()?;
        Some(LinkSource {
            note: note.note.key(),
            etag: note.etag.clone(),
            storage_generation: self.accepted_storage_generation?,
            launch_generation: self.live.as_ref()?.client().launch_generation,
            request: self.request,
        })
    }
    fn ensure_link_resolutions(&mut self, cx: &mut Context<Self>) {
        let Some(source) = self.current_link_source() else {
            self.shell.links = None;
            return;
        };
        let properties = std::rc::Rc::new(crate::note_properties::project(
            &self.current_note.as_ref().unwrap().props,
        ));
        if self
            .shell
            .links
            .as_ref()
            .is_some_and(|cache| cache.source == source && cache.properties == properties)
        {
            return;
        }
        let mut terms = property_internal_terms(&self.content, &properties);
        let mut warnings = Vec::new();
        let deferred_targets = terms.len().saturating_sub(128);
        if terms.len() > 128 {
            warnings.push("Only the first 128 distinct internal targets were checked; remaining targets are inert.".into());
            terms.truncate(128);
        }
        if uncertain_resolution(&self.current_related) {
            warnings.push(
                "Source metadata is partial or cached; internal targets were not resolved.".into(),
            );
            terms.clear();
        }
        self.shell.links = Some(LinkResolutionCache {
            source: source.clone(),
            properties: properties.clone(),
            pending: !terms.is_empty(),
            deferred_targets,
            canceled: Default::default(),
            resolved: Default::default(),
            displayed: std::rc::Rc::new(markdown::with_footnote_numbers(&self.content)),
            warnings,
        });
        if terms.is_empty() {
            return;
        }
        let session = self.live.clone().unwrap();
        let source_task = source.clone();
        let canceled = self.shell.links.as_ref().unwrap().canceled.clone();
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let before = client.storage().map_err(|error| error.to_string())?;
            if before.generation != source_task.storage_generation || before.phase != "ready" {
                return Err(
                    "Storage changed or is incomplete; internal links were not resolved."
                        .to_owned(),
                );
            }
            let mut resolved = std::collections::HashMap::new();
            let mut titles = std::collections::HashMap::new();
            let mut warnings = Vec::new();
            for term in terms {
                if canceled.load(std::sync::atomic::Ordering::Acquire) {
                    return Err("Link lookup canceled after navigation.".into());
                }
                match client.resolve(&term, &source_task.note.vault) {
                    Ok(response) => {
                        if response.launch_generation != source_task.launch_generation
                            || response.source_vault != source_task.note.vault
                            || response.term != term
                            || uncertain_resolution(&response.diagnostics)
                        {
                            warnings.push(format!("Unverified internal target: {term}"));
                        } else if let Some(key) = response.key() {
                            titles.insert(term.clone(), response.note.title);
                            resolved.insert(term, key);
                        }
                    }
                    Err(error) => {
                        warnings.push(format!("Internal target {term}: {error}"));
                    }
                }
            }
            let after = client.storage().map_err(|error| error.to_string())?;
            if after.generation != before.generation || after.phase != "ready" {
                return Err(
                    "Storage changed during lookup; internal links remain inert. Reload the note."
                        .into(),
                );
            }
            Ok::<_, String>((resolved, titles, warnings))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.current_link_source().as_ref() != Some(&source) {
                    return;
                }
                let Some(cache) = &mut this.shell.links else {
                    return;
                };
                if cache.source != source || !std::rc::Rc::ptr_eq(&cache.properties, &properties) {
                    return;
                }
                cache.pending = false;
                match result {
                    Ok((resolved, titles, warnings)) => {
                        cache.update_display(&this.content, &titles);
                        cache.resolved = resolved;
                        cache.warnings.extend(warnings);
                    }
                    Err(error) => cache.warnings.push(error),
                }
                cx.notify();
            });
        })
        .detach();
    }
    fn property_link_verified(
        &self,
        property: &crate::note_properties::Property,
        source: &LinkSource,
    ) -> bool {
        let verified = self.shell.links.as_ref().is_some_and(|cache| {
            cache.source == *source
                && !cache.pending
                && property
                    .target
                    .as_ref()
                    .and_then(markdown::resolve_term)
                    .is_some_and(|term| cache.resolved.contains_key(&term))
        });
        self.current_note.as_ref().is_some_and(|note| {
            property_callback_current(
                source,
                self.current_link_source().as_ref(),
                property,
                &note.props,
                verified,
            )
        })
    }
    fn activate_internal_target(
        &mut self,
        target: &markdown::LinkTarget,
        source: &LinkSource,
        cx: &mut Context<Self>,
    ) {
        if self.includes_preview_active() || self.current_link_source().as_ref() != Some(source) {
            return;
        }
        let key = if markdown::is_local_fragment(target) {
            source.note.clone()
        } else {
            let Some(term) = markdown::resolve_term(target) else {
                return;
            };
            let Some(key) = self
                .shell
                .links
                .as_ref()
                .filter(|cache| cache.source == *source && !cache.pending)
                .and_then(|cache| cache.resolved.get(&term))
                .cloned()
            else {
                return;
            };
            key
        };
        self.open_internal_link(
            InternalLinkIntent {
                source: source.note.clone(),
                destination: key,
                target: target.clone(),
                launch_generation: source.launch_generation,
                storage_generation: source.storage_generation,
                allow_epoch_advance: false,
            },
            cx,
        );
    }
    pub(crate) fn open_internal_link(
        &mut self,
        intent: InternalLinkIntent,
        cx: &mut Context<Self>,
    ) {
        let launch = self
            .live
            .as_ref()
            .map(|session| session.client().launch_generation);
        let source_matches = self
            .current_note
            .as_ref()
            .is_some_and(|note| note.note.key() == intent.source);
        if !source_matches
            || launch != Some(intent.launch_generation)
            || !self.accepted_storage_generation.is_some_and(|epoch| {
                epoch == intent.storage_generation || intent.allow_epoch_advance
            })
        {
            self.shell.navigation_notice=Some("Link navigation was canceled because its source or storage changed. Activate the link again.".into());
            cx.notify();
            return;
        }
        if self.guard_transition(workspace_edit::Transition::InternalLink(intent.clone()), cx) {
            return;
        }
        self.shell.navigation_notice = None;
        self.shell.pending_fragment = None;
        self.shell.block_arrival.clear();
        self.open_live_key(intent.destination.clone(), cx);
        if has_authored_fragment(&intent.target) {
            self.shell.pending_fragment = Some(PendingFragment {
                intent,
                request: self.request,
                loaded: false,
                scheduled: false,
            });
        }
        cx.notify();
    }
    pub(crate) fn advance_fragment_request(
        &mut self,
        from_request: u64,
        to_request: u64,
        key: &live::NoteKey,
    ) {
        if let Some(pending) = &mut self.shell.pending_fragment {
            if pending.request == from_request && pending.intent.destination == *key {
                pending.request = to_request;
            }
        }
    }
    pub(crate) fn accept_fragment_destination(
        &mut self,
        loaded_request: u64,
        cx: &mut Context<Self>,
    ) {
        let Some(pending) = &mut self.shell.pending_fragment else {
            return;
        };
        let Some(note) = &self.current_note else {
            return;
        };
        let launch = self
            .live
            .as_ref()
            .map(|session| session.client().launch_generation);
        if !fragment_accepts(
            &pending.intent,
            pending.request,
            loaded_request,
            launch,
            self.accepted_storage_generation,
            &note.note.key(),
        ) {
            self.shell.pending_fragment = None;
            self.shell.navigation_notice = Some(
                "Fragment jump was canceled because the destination or storage changed.".into(),
            );
            cx.notify();
            return;
        }
        pending.request = self.request;
        pending.intent.storage_generation = self.accepted_storage_generation.unwrap();
        pending.intent.allow_epoch_advance = false;
        pending.loaded = true;
    }
    fn schedule_fragment_scroll(
        &mut self,
        anchors: std::rc::Rc<markdown::HeadingAnchors>,
        scroll: gpui::ScrollHandle,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(pending) = self.shell.pending_fragment.clone() else {
            return;
        };
        if pending.request != self.request {
            self.shell.pending_fragment = None;
            return;
        }
        if !pending.loaded || pending.scheduled {
            return;
        }
        let current_key = self.current_note.as_ref().map(|note| note.note.key());
        let launch = self
            .live
            .as_ref()
            .map(|session| session.client().launch_generation);
        if current_key.as_ref().is_none_or(|key| {
            !fragment_accepts(
                &pending.intent,
                pending.request,
                self.request,
                launch,
                self.accepted_storage_generation,
                key,
            )
        }) {
            self.shell.pending_fragment = None;
            return;
        }
        let anchor = match markdown::fragment_anchor(&self.content, &pending.intent.target) {
            Ok(Some(anchor)) => Some(anchor),
            Ok(None) => {
                self.shell.pending_fragment = None;
                return;
            }
            Err(markdown::FragmentError::MissingHeading(_)) => {
                self.shell.navigation_notice =
                    Some("Heading not found; showing the top of the note.".into());
                None
            }
            Err(markdown::FragmentError::MissingBlock(_)) => {
                self.shell.navigation_notice =
                    Some("Block target not found; no fragment jump was applied.".into());
                self.shell.pending_fragment = None;
                return;
            }
            Err(_) => {
                self.shell.navigation_notice = Some(
                    "This fragment could not be matched; the note was opened without a jump."
                        .into(),
                );
                self.shell.pending_fragment = None;
                return;
            }
        };
        let block_index = anchor
            .as_ref()
            .filter(|anchor| anchor.starts_with('^'))
            .and_then(|anchor| {
                markdown::authored_block_targets(&self.content)
                    .get(anchor)
                    .copied()
            });
        let block_positions = block_index.and(self.shell.positions.anchors.clone());
        let displayed = self.displayed_content();
        self.shell.pending_fragment.as_mut().unwrap().scheduled = true;
        let destination_source = self.current_link_source();
        let destination_tab = self.tabs.active().map(|tab| tab.key.clone());
        let reader_epoch = self.shell.reader_scroll_epoch;
        let interaction = self.shell.positions.interaction;
        let viewport = window.viewport_size();
        let weak = cx.entity().downgrade();
        window.on_next_frame(move |window, cx| {
            let _ = weak.update(cx, |this, cx| {
                let still_pending = this.shell.pending_fragment.as_ref().is_some_and(|now| {
                    now.request == pending.request && now.intent == pending.intent
                });
                let key = this.current_note.as_ref().map(|note| note.note.key());
                let launch = this
                    .live
                    .as_ref()
                    .map(|session| session.client().launch_generation);
                if !still_pending { return; }
                if !fragment_layout_current(
                    this.current_link_source() == destination_source,
                    this.tabs.active().map(|tab| tab.key.clone()) == destination_tab,
                    this.shell.reader_scroll_epoch == reader_epoch,
                    this.shell.positions.interaction == interaction,
                    window.viewport_size() == viewport,
                    destination_tab.as_ref().and_then(|tab| this.shell.heading_anchors.get(tab))
                        .is_some_and(|(_, current)| std::rc::Rc::ptr_eq(current, &anchors)),
                ) {
                    this.shell.pending_fragment = None;
                    this.shell.navigation_notice = Some("Fragment jump canceled after the reader changed; activate the link again.".into());
                    cx.notify();
                    return;
                }
                if this.request != pending.request
                    || key.as_ref().is_none_or(|key| {
                        !fragment_accepts(
                            &pending.intent,
                            pending.request,
                            this.request,
                            launch,
                            this.accepted_storage_generation,
                            key,
                        )
                    })
                {
                    return;
                }
                this.shell.pending_fragment = None;
                if this.editing_ui.active() || this.tag_routing_blocked(cx) {
                    return;
                }
                if let Some(index) = block_index {
                    // Direct measured application inside this guarded callback.
                    // ScrollAnchor would enqueue an additional unguarded frame.
                    let applied = block_positions.as_ref().is_some_and(|positions| {
                        this.displayed_content().as_ref() == displayed.as_ref()
                            && this.shell.positions.anchors.as_ref().is_some_and(|current| std::rc::Rc::ptr_eq(current, positions))
                            && positions.capture(window.viewport_size(), window.scale_factor()).is_some_and(|capture| {
                                positions.apply(index, &capture, window.viewport_size(), window.scale_factor())
                            })
                    });
                    if !applied {
                        this.shell.navigation_notice = Some("Block layout was unavailable or changed; no fragment jump was applied.".into());
                    } else {
                        this.show_block_arrival(index, cx);
                    }
                } else if let Some(anchor) = anchor {
                    if !anchors.scroll_to(&anchor, window, cx) {
                        this.shell.navigation_notice = Some(
                            "Fragment layout was unavailable; no fragment jump was applied.".into(),
                        );
                    } else {
                        // ScrollAnchor queues another next-frame callback. The
                        // notify below paints this outer frame, before that offset
                        // changes. Refresh AFTER the queued anchor application so
                        // an idle reader need not wait for an unrelated input.
                        window.on_next_frame(|window, _| window.refresh());
                    }
                } else {
                    scroll.set_offset(point(px(0.), px(0.)));
                }
                cx.notify();
            });
        });
        window.refresh();
    }
    /// Session-only working scope for future single-vault actions. Never use this
    /// to rewrite a note identity, scope federated search, or reinterpret Home.
    pub(crate) fn change_working_vault(&mut self, scope: String, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::WorkingVault(scope.clone()), cx) {
            return;
        }
        self.shell.set_working_scope(
            scope,
            self.live.as_ref().map(|s| s.client().launch_generation),
        );
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        if self.full_graph_active() {
            self.open_full_graph(cx);
        }
        self.shell.vault_menu = false;
        cx.notify();
    }
    pub fn working_vault_scope(&self) -> &str {
        &self.shell.working_scope
    }
    fn refresh_vault_discovery(&mut self, cx: &mut Context<Self>) {
        if self.shell.vault_loading {
            return;
        }
        let Some(session) = self.live.clone() else {
            return;
        };
        let generation = session.client().launch_generation;
        self.shell.vault_loading = true;
        let job = cx.background_executor().spawn(async move {
            session
                .client()
                .vaults()
                .map_err(|error| error.to_string())
                .and_then(|value| VaultDiscovery::parse(&value))
                .and_then(|discovery| {
                    let known = discovery.session_membership()?;
                    Ok((discovery, known))
                })
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.shell.vault_generation != Some(generation)
                    || !this
                        .live
                        .as_ref()
                        .is_some_and(|session| session.client().launch_generation == generation)
                {
                    return;
                }
                this.shell.vault_loading = false;
                match result {
                    Ok((discovery, known)) => {
                        this.session_ui.registry_changed(known);
                        this.invalidate_gallery();
                        this.shell.working_scope =
                            discovery.reconcile_scope(&this.shell.working_scope);
                        this.shell.vault_discovery = Some(discovery);
                        this.shell.vault_error = None;
                    }
                    Err(error) => {
                        this.shell.vault_error = Some(error);
                    }
                }
                cx.notify();
            });
        })
        .detach();
    }
    pub(crate) fn restore_reload_route(
        &mut self,
        route: Option<tabs::TabKey>,
        record_visit: bool,
        cx: &mut Context<Self>,
    ) -> bool {
        let Some(route) = route else {
            return false;
        };
        match route {
            tabs::TabKey::Tag { tag, scope } => self.open_tag_page(tag, scope, cx),
            tabs::TabKey::Home => self.show_empty_home(),
            tabs::TabKey::Graph => self.open_full_graph(cx),
            tabs::TabKey::Calendar => self.open_calendar(cx),
            tabs::TabKey::Note { vault, note_id } => {
                self.shell.home = false;
                if self.live.is_some() {
                    let previous = self.request;
                    self.open_live_key(live::NoteKey { vault, note_id }, cx);
                    if !record_visit && self.request != previous {
                        self.forget_recent_request(self.request);
                    }
                } else if let Some(i) = self
                    .snapshot
                    .notes
                    .iter()
                    .position(|n| n.note_id == note_id)
                {
                    self.select(i, cx);
                } else {
                    self.read_error =
                        Some("Selected note is absent after reload; route retained.".into());
                }
            }
        }
        true
    }
    pub(crate) fn reader_fragment_pending(&self) -> bool {
        self.shell.pending_fragment.is_some()
    }
    fn native_reload_available(&self, window: &Window, cx: &App) -> bool {
        let ready = self
            .live
            .as_ref()
            .filter(|session| session.failure().is_none())
            .and_then(|session| {
                let storage = session.initial_storage.as_ref()?;
                (storage.phase == "ready"
                    && Some(storage.generation) == self.accepted_storage_generation)
                    .then_some(session.client().launch_generation)
            });
        let reload_route = self.history.reload_route(&self.tabs);
        let home = native_reload_home(
            self.shell.home,
            reload_route.as_ref(),
            self.session_ui.safe_home,
        );
        let ready_route = ready.is_some_and(|launch| {
            home || self.tabs.active().is_some_and(|tab| match &tab.key {
                tabs::TabKey::Note { vault, .. } => {
                    !self.shell.home
                        && self.session_ui.permits(vault)
                        && reload_reader_identity(
                            self.shell.reader_content.get(&tab.key),
                            launch,
                            self.accepted_storage_generation,
                        )
                }
                tabs::TabKey::Calendar => {
                    self.calendar_active()
                        && self.follow_registry().is_some()
                        && self.session_ui.permits(self.working_vault_scope())
                }
                _ => false,
            })
        });
        let blocked = self.tag_page.is_some()
            || self.full_graph_active()
            || self.native_history_owner_blocked(cx)
            || !self.editing_ui.plain_reader_mode()
            || !self.editing_ui.follow_safe()
            || self.creation_open()
            || self.shell.find.open
            || self.reader_hover_active()
            || self.shell.pending_fragment.is_some()
            || self.search.read(cx).is_composing()
            || self.shell.calendar_zone.is_composing(cx)
            || (self.session_ui.recovery_enabled()
                && (!self.session_ui.recovery_ready()
                    || self.session_ui.recovery_record().is_some()
                    || self.session_ui.recovery_status()
                        != workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Empty,
                        )));
        let focus = window.focused(cx).is_none()
            || self
                .shell
                .workspace_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window))
            || self
                .shell
                .reader_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window));
        native_reload_enabled(
            window.is_window_active() && cx.active_window().and_then(shortcut_workspace).is_some(),
            ready_route,
            blocked,
            self.live_reader_pending == Some(self.request),
            focus,
        )
    }
    fn native_reload(&mut self, window: &Window, cx: &mut Context<Self>) {
        if self.native_reload_available(window, cx) {
            self.load_notes(cx);
        }
    }
    pub(crate) fn native_find_available(&self, step: bool, window: &Window, cx: &App) -> bool {
        let accepted = self.shell.accepted_title_request == Some(self.request)
            && self.current_link_source().is_some_and(|source| {
                self.current_note_request.as_ref().is_some_and(|route| {
                    source.note == *route
                        && self
                            .current_note
                            .as_ref()
                            .is_some_and(|note| note.note.key() == *route)
                        && self.tabs.active().is_some_and(|tab| {
                            tab.key
                                == (tabs::TabKey::Note {
                                    vault: route.vault.clone(),
                                    note_id: route.note_id,
                                })
                        })
                }) && self
                    .live
                    .as_ref()
                    .and_then(|live| live.initial_storage.as_ref())
                    .is_some_and(|storage| {
                        storage.phase == "ready" && storage.generation == source.storage_generation
                    })
            });
        let blocked = self.reader_selection_dragging()
            || !self.find_available()
            || !self.editing_ui.plain_reader_mode()
            || self.editor_shortcut_blocked(cx)
            || self.tag_page.is_some()
            || self.partial_read
            || self.read_error.is_some()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.shell.pending_fragment.is_some()
            || self.previews.model.transient.is_some()
            || self.previews.model.geometry_active()
            || self.search.read(cx).is_composing()
            || self.shell.calendar_zone.is_composing(cx)
            || self.shell.find.is_composing(cx)
            || (self.session_ui.recovery_enabled()
                && (!self.session_ui.recovery_ready()
                    || self.session_ui.recovery_record().is_some()
                    || self.session_ui.recovery_status()
                        != workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Empty,
                        )));
        let allowed_focus = window.focused(cx).is_none()
            || self
                .shell
                .workspace_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window))
            || self
                .shell
                .reader_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window))
            || self.shell.find.native_owns_focus(window, cx);
        native_find_enabled(
            window.is_window_active() && cx.active_window().and_then(shortcut_workspace).is_some(),
            accepted,
            blocked,
            allowed_focus,
            step,
            self.shell.find.native_has_query(),
        )
    }
    fn native_history_owner_blocked(&self, cx: &App) -> bool {
        self.reader_selection_dragging()
            || self.tag_routing_blocked(cx)
            || self.editor_shortcut_blocked(cx)
            || self.shell.find.is_composing(cx)
    }
    fn native_history_available(&self, forward: bool, cx: &App) -> bool {
        let invalid_tag = self
            .tag_page
            .as_ref()
            .is_some_and(|p| !tabs::TabKey::valid_tag_route(&p.tag, &p.scope));
        native_history_enabled(
            &self.history,
            forward,
            invalid_tag,
            self.native_history_owner_blocked(cx),
        )
    }
    pub(crate) fn navigate_history(&mut self, forward: bool, cx: &mut Context<Self>) {
        // Invalid input is an ephemeral error page, never a serialized route.
        // Back returns to its source without skipping that history entry.
        if self
            .tag_page
            .as_ref()
            .is_some_and(|p| !tabs::TabKey::valid_tag_route(&p.tag, &p.scope))
        {
            if !forward {
                if self.tag_routing_blocked(cx) {
                    return;
                }
                let route = self.history.current().cloned();
                self.restore_reload_route(route, true, cx);
                cx.notify();
            }
            return;
        }
        if let Some(ticket) = self.history.peek(forward) {
            self.replay_history(ticket, cx);
        }
    }
    pub(crate) fn replay_history(&mut self, ticket: navigation::Ticket, cx: &mut Context<Self>) {
        if !self.history.accepts(&ticket) {
            return;
        }
        if let Some(tabs::TabKey::Note { vault, note_id }) = &ticket.route {
            if !self.session_ui.permits(vault)
                || (self.live.is_none()
                    && !self.snapshot.notes.iter().any(|n| n.note_id == *note_id))
            {
                return;
            }
        }
        if self.guard_transition(workspace_edit::Transition::History(ticket.clone()), cx) {
            return;
        }
        if !self.history.commit(&ticket) {
            return;
        }
        self.shell.search_open = false;
        self.shell.vault_menu = false;
        self.shell.tab_menu = false;
        self.shell.tab_cycle.clear();
        match ticket.route {
            Some(tabs::TabKey::Note { vault, note_id }) => {
                self.shell.home = false;
                if self.live.is_some() {
                    self.open_live_key(live::NoteKey { vault, note_id }, cx);
                } else if let Some(i) = self
                    .snapshot
                    .notes
                    .iter()
                    .position(|n| n.note_id == note_id)
                {
                    self.select(i, cx);
                }
            }
            Some(tabs::TabKey::Tag { tag, scope }) => self.open_tag_page(tag, scope, cx),
            Some(tabs::TabKey::Graph) => self.open_full_graph(cx),
            Some(tabs::TabKey::Calendar) => self.open_calendar(cx),
            _ => {
                // History Home is the literal destination, not today's configured note.
                self.tabs.open_home();
                // Reopen literal Home while preserving background tabs.
                self.show_empty_home();
            }
        }
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        cx.notify();
    }
    pub(crate) fn activate_tab(&mut self, key: tabs::TabKey, cx: &mut Context<Self>) {
        self.activate_tab_inner(key, false, cx);
    }
    fn activate_tab_inner(
        &mut self,
        key: tabs::TabKey,
        load_current_after_close: bool,
        cx: &mut Context<Self>,
    ) {
        // An already-active tab is a focus click, not a navigation/reload request.
        // Check before the dirty gate so it cannot capture a spurious leave intent.
        if !tab_activation_needed(
            &self.tabs,
            &key,
            load_current_after_close || self.tag_page.is_some(),
        ) {
            return;
        }
        if self.guard_transition(workspace_edit::Transition::ActivateTab(key.clone()), cx) {
            return;
        }
        if let tabs::TabKey::Note { vault, .. } = &key {
            if !self.session_ui.permits(vault) {
                self.read_error =
                    Some("Restored vault is unavailable; no fallback request was sent.".into());
                cx.notify();
                return;
            }
        }
        self.shell.tab_cycle.clear();
        if !self.tabs.activate(&key) {
            return;
        }
        self.shell.tab_menu = false;
        self.shell.search_open = false;
        self.tag_page = None;
        match key {
            tabs::TabKey::Tag { .. } => return, // pages never occur in Tabs
            tabs::TabKey::Home => self.open_home(cx),
            tabs::TabKey::Graph => self.open_full_graph(cx),
            tabs::TabKey::Calendar => self.open_calendar(cx),
            tabs::TabKey::Note { vault, note_id } => {
                self.shell.home = false;
                self.selected = None;
                self.content.clear();
                self.current_note = None;
                self.current_note_request = None;
                self.current_related.clear();
                if self.live.is_some() {
                    self.open_live_key(live::NoteKey { vault, note_id }, cx);
                } else if let Some(index) = self
                    .snapshot
                    .notes
                    .iter()
                    .position(|note| note.note_id == note_id)
                {
                    self.select(index, cx);
                }
            }
        }
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        cx.notify();
    }
    pub(crate) fn workspace_shortcut(&mut self, save: bool, cx: &mut Context<Self>) {
        if self.recent_open() {
            return;
        }
        if self.enlarged_image_open() || self.reader_hierarchy_open() {
            return;
        }
        if self.tag_page.is_some()
            || self.shell.search_open
            || self.search.read(cx).is_composing()
            || self.shell.calendar_zone.is_composing(cx)
            || self.shell.vault_menu
            || self.shell.tab_menu
            || self.shell.diagnostics
            || self.quit_session.is_closing()
            || self.previews.model.geometry_active()
            || self.shell.find.open
            || self.handoff_active()
            || self.includes_preview_active()
            || self.previews.model.transient.is_some()
            || self.editor_shortcut_blocked(cx)
        {
            return;
        }
        if save {
            if !self.shell.home
                && !self.full_graph_active()
                && !self.calendar_active()
                && self.editor_shortcut_save_eligible()
            {
                self.editor_save(false, cx);
            }
        } else if let Some(key) = self.tabs.active().map(|tab| tab.key.clone()) {
            self.close_tab(&key, cx);
        }
    }

    pub(crate) fn close_tab(&mut self, key: &tabs::TabKey, cx: &mut Context<Self>) {
        if self.tabs.get(key).is_none() {
            return;
        }
        let was_active =
            self.tag_page.is_none() && self.tabs.active().is_some_and(|tab| &tab.key == key);
        if was_active
            && self.guard_transition(workspace_edit::Transition::CloseTab(key.clone()), cx)
        {
            return;
        }
        self.shell.tab_cycle.clear();
        self.shell.reader_scrolls.remove(key);
        self.shell.reader_content.remove(key);
        self.shell.heading_anchors.remove(key);
        if !self.tabs.close(key) {
            return;
        }
        self.shell.tab_menu = false;
        if was_active {
            if let Some(next) = self.tabs.active().map(|tab| tab.key.clone()) {
                // Removing the active entry already promotes its neighbor in the
                // model; its reader still needs loading despite being model-active.
                self.activate_tab_inner(next, true, cx);
            } else {
                // Do not reopen a configured home or accept an older async response.
                self.show_empty_home();
            }
        }
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        cx.notify();
    }
    pub fn configured_home_index(&self) -> Option<usize> {
        let id = self
            .live
            .as_ref()?
            .initial_storage
            .as_ref()?
            .home_note_id
            .as_ref()?
            .parse::<i64>()
            .ok()?;
        let active_vault = self
            .live
            .as_ref()?
            .initial_vaults
            .get("active")
            .and_then(|v| v.get("name"))
            .and_then(|v| v.as_str())
            .unwrap_or("");
        self.snapshot
            .notes
            .iter()
            .enumerate()
            .find(|(index, note)| {
                note.note_id == id
                    && self.live_notes.get(*index).is_some_and(|live_note| {
                        live_note.vault.is_empty() || live_note.vault == active_vault
                    })
            })
            .map(|(index, _)| index)
    }
    fn show_empty_home(&mut self) {
        self.tag_page = None;
        self.history.record(Some(tabs::TabKey::Home));
        self.request += 1;
        self.graph_request += 1;
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.shell.home = true;
        self.selected = None;
        self.content.clear();
        self.current_note = None;
        self.current_note_request = None;
        self.current_related.clear();
        self.show_graph = false;
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
    }
    pub(crate) fn open_home(&mut self, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::Home, cx) {
            return;
        }
        self.shell.tab_menu = false;
        self.shell.search_open = false;
        if let Some(index) = self.configured_home_index() {
            self.shell.home = false;
            self.select(index, cx);
        } else if self
            .live
            .as_ref()
            .and_then(|session| session.initial_storage.as_ref())
            .is_some_and(|storage| storage.home_note_id.is_some())
        {
            self.show_empty_home();
            self.load_live_notes(cx);
        } else {
            self.tabs.open_home();
            self.show_empty_home();
        }
        cx.notify();
    }
}
impl Workspace {
    pub(crate) fn search_refinement_stamp(&self, query: &str) -> Option<search_refinement::Stamp> {
        let session = self.live.as_ref()?;
        if session.failure().is_some() {
            return None;
        }
        let storage = session.initial_storage.as_ref()?;
        if storage.phase != "ready" || Some(storage.generation) != self.accepted_storage_generation
        {
            return None;
        }
        Some(search_refinement::Stamp {
            query: query.into(),
            epoch: self.query_epoch.load(std::sync::atomic::Ordering::SeqCst),
            request: self.request,
            launch: session.client().launch_generation,
            storage: storage.generation,
            scope_revision: self.shell.scope_revision,
        })
    }
    fn refine_search_tag(
        &mut self,
        stamp: &search_refinement::Stamp,
        key: &live::NoteKey,
        tag: &str,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        let input = self.search.read(cx);
        let current = self.search_refinement_stamp(input.query());
        let allowed = window.is_window_active()
            && cx.active_window().and_then(shortcut_workspace).is_some()
            && (window.focused(cx).is_none()
                || self.search.focus_handle(cx).is_focused(window)
                || self
                    .shell
                    .workspace_focus
                    .as_ref()
                    .is_some_and(|focus| focus.is_focused(window)))
            && self.shell.search_open
            && !self.tag_search_result_blocked(cx)
            && !self.reader_selection_dragging()
            && !self.shell.find.open
            && !self.shell.find.is_composing(cx)
            && !self.creation_open()
            && self.editing_ui.follow_safe()
            && (!self.session_ui.recovery_enabled()
                || (self.session_ui.recovery_ready()
                    && self.session_ui.recovery_record().is_none()
                    && self.session_ui.recovery_status()
                        == workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Empty,
                        )));
        if !search_refinement::admits(stamp, current.as_ref(), input.is_composing(), allowed)
            || self.shell.search_refinement.as_ref() != Some(stamp)
            || !self
                .search_results
                .iter()
                .any(|row| row.key() == *key && row.tags.iter().any(|value| value == tag))
        {
            return;
        }
        self.search.update(cx, |input, cx| {
            input.refine_tag(&stamp.query, tag, cx);
        });
    }
    fn search_result_indices(&self, cx: &App) -> Vec<usize> {
        let query = self.search.read(cx).query().to_lowercase();
        if query.trim().is_empty() {
            vec![]
        } else if self.live.is_some() {
            (0..self.search_results.len()).collect()
        } else {
            self.snapshot
                .notes
                .iter()
                .enumerate()
                .filter(|(_, note)| {
                    note.title.to_lowercase().contains(&query)
                        || note
                            .tags
                            .iter()
                            .any(|tag| tag.to_lowercase().contains(&query))
                })
                .map(|(i, _)| i)
                .collect()
        }
    }
    fn dismiss_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.enlarged_image_open() || self.reader_hierarchy_open() {
            return;
        }
        if self.search.read(cx).is_composing() {
            return;
        }
        self.shell.search_open = false;
        let focus = self
            .shell
            .search_return_focus
            .take()
            .or_else(|| self.shell.reader_focus.clone());
        if let Some(focus) = focus {
            window.focus(&focus);
        } else {
            window.blur();
        }
        cx.notify();
    }
    fn step_search_result(&mut self, forward: bool, cx: &mut Context<Self>) {
        if self.enlarged_image_open() || self.reader_hierarchy_open() {
            return;
        }
        if !self.shell.search_open || self.search.read(cx).is_composing() {
            return;
        }
        let len = self.search_result_indices(cx).len();
        if let Some(row) = self
            .search
            .update(cx, |search, _| search.step_result(len, forward))
        {
            self.shell
                .search_scroll
                .scroll_to_item(row, gpui::ScrollStrategy::Top);
        }
        cx.notify();
    }
    fn open_search_result(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.enlarged_image_open() || self.reader_hierarchy_open() {
            return;
        }
        if !self.shell.search_open || self.search.read(cx).is_composing() {
            return;
        }
        let indices = self.search_result_indices(cx);
        let row = self.search.read(cx).selected_result().unwrap_or(0);
        let Some(&index) = indices.get(row) else {
            return;
        };
        // These existing entry points snapshot immutable identity before dirty gating.
        if self.live.is_some() {
            self.select_search_result(index, cx);
        } else {
            self.select_from_ui(index, cx);
        }
        if !self.shell.search_open {
            self.shell.search_return_focus = None;
            if let Some(focus) = &self.shell.reader_focus {
                window.focus(focus);
            } else {
                window.blur();
            }
        }
    }
}

impl Render for Workspace {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if !self.shell.home {
            self.shell.open_tasks = None;
        }
        self.sync_reader_selection(window, cx);
        self.sync_reader_hover(window, cx);
        self.sync_reader_hierarchy(window, cx);
        self.sync_enlarged_image(window, cx);
        let title_source = self.copy_title_source(cx);
        self.shell.title_copy.sync(title_source);
        self.sync_markdown_copy(cx);
        self.sync_creation_journal(cx);
        self.sync_block_arrival(cx);
        if self.shell.reader_keys.as_ref().is_some_and(|owner| {
            !self.reader_key_owner_current(owner, window, cx)
                || self
                    .shell
                    .reader_scrolls
                    .get(&owner.tab)
                    .and_then(|handle| FootnoteGeometry::capture(handle, window))
                    .is_some_and(|geometry| geometry != owner.geometry)
        }) {
            self.shell.reader_keys = None;
        }
        self.sync_journal_day(cx);
        if self
            .shell
            .footnote_return
            .as_ref()
            .is_some_and(|ticket| !self.footnote_return_current(ticket, window, cx))
        {
            self.shell.footnote_return = None;
        }
        let dark = self.shell.dark.unwrap_or(matches!(
            window.appearance(),
            WindowAppearance::Dark | WindowAppearance::VibrantDark
        ));
        let p = Palette::new(dark);
        self.sync_pdf();
        self.sync_math();
        self.reset_stale_handoff();
        if self.handoff_active() {
            return self.render_handoff(dark, (p.paper, p.ink, p.line), cx);
        }
        self.invalidate_recent_panel(window);
        self.reset_stale_includes();
        if self.includes_preview_active() {
            return self.rendered_includes_preview(dark, (p.paper, p.ink, p.line), window, cx);
        }
        self.sync_editor_theme(dark, cx);
        self.ensure_link_resolutions(cx);
        self.ensure_reader_images(cx);
        let link_warning=self.shell.links.as_ref().filter(|cache|!cache.warnings.is_empty()).map(|cache|if cache.deferred_targets>0 {format!("Internal-link checking is limited to 128 targets; {} additional targets remain inactive. Details in diagnostics.",cache.deferred_targets)} else {"Some internal links could not be verified and remain inactive. Open diagnostics for details.".into()});
        let resolved_terms = std::rc::Rc::new(
            self.shell
                .links
                .as_ref()
                .map(|cache| cache.resolved.clone())
                .unwrap_or_default(),
        );
        let link_source = self.current_link_source();
        let external_displayed = self.displayed_content();
        let local_fragments_ready = link_source.is_some();
        let external_ready =
            local_fragments_ready && self.find_available() && self.read_error.is_none();
        let footnote_labels = markdown::footnote_targets(&external_displayed);
        let weak = cx.entity().downgrade();
        let plain_source = link_source.clone();
        let plain_displayed = external_displayed.clone();
        let plain_weak = cx.entity().downgrade();
        let hover_source = link_source.clone();
        let hover_displayed = external_displayed.clone();
        let hover_weak = cx.entity().downgrade();
        let select_source = link_source.clone();
        let select_displayed = external_displayed.clone();
        let select_weak = cx.entity().downgrade();
        let link_callbacks = markdown::LinkCallbacks {
            pdf: self.pdf_projection(),
            math: self.math_projection(),
            selection: Some(markdown::SelectionCallbacks {
                selected: self
                    .shell
                    .selection
                    .as_ref()
                    .map(ReaderSelection::ranges)
                    .unwrap_or_default(),
                measurements: self.shell.selection_measurements.clone(),
                generation: self.shell.selection_measurements.begin(),
                pointer: std::rc::Rc::new(move |index, phase, layout, position, window, cx| {
                    let Some(source) = &select_source else {
                        return false;
                    };
                    select_weak
                        .update(cx, |this, cx| {
                            this.reader_selection_pointer(
                                source,
                                &select_displayed,
                                index,
                                phase,
                                layout,
                                position,
                                window,
                                cx,
                            )
                        })
                        .unwrap_or(false)
                }),
            }),
            hover: Some(std::rc::Rc::new(
                move |id, link, event, layout, window, cx| {
                    if let Some(source) = &hover_source {
                        let _ = hover_weak.update(cx, |this, cx| {
                            this.reader_link_hover(
                                source,
                                &hover_displayed,
                                id,
                                link,
                                event,
                                layout,
                                window,
                                cx,
                            )
                        });
                    }
                },
            )),
            arrival_block: self
                .shell
                .block_arrival
                .active
                .as_ref()
                .map(|ticket| ticket.source.index),
            enlarge: self.enlarge_callback(cx),
            plain_click: Some(std::rc::Rc::new(move |index, event, window, cx| {
                if let Some(source) = &plain_source {
                    let _ = plain_weak.update(cx, |this, cx| {
                        this.claim_reader_keys(source, &plain_displayed, index, event, window, cx)
                    });
                }
            })),
            resolved: std::rc::Rc::new(move |target| {
                (external_ready
                    && matches!(target, markdown::LinkTarget::Footnote { label } if footnote_labels.contains_key(label)))
                    || (local_fragments_ready && markdown::is_local_fragment(target))
                    || (external_ready && external_links::destination(target).is_some())
                    || markdown::resolve_term(target)
                        .is_some_and(|term| resolved_terms.contains_key(&term))
            }),
            activate: std::rc::Rc::new(move |link, window, cx| {
                let _ = weak.update(cx, |this, _| this.cancel_transient_preview());
                if let Some(source) = &link_source {
                    let _ = weak.update(cx, |this, cx| {
                        if matches!(link.target, markdown::LinkTarget::Footnote { .. }) {
                            if !footnote_jump_allowed(
                                source,
                                this.current_link_source().as_ref(),
                                &external_displayed,
                                &this.displayed_content(),
                                link,
                                this.footnote_reader_available(cx),
                            ) {
                                return;
                            }
                            let Some(tab) = this.tabs.active().map(|tab| tab.key.clone()) else {
                                return;
                            };
                            let Some(handle) = this.shell.reader_scrolls.get(&tab) else {
                                return;
                            };
                            let Some(geometry) = FootnoteGeometry::capture(handle, window) else {
                                return;
                            };
                            let origin = handle.offset();
                            if geometry.bounded_origin(origin) != Some(origin) {
                                return;
                            }
                            let Some((_, anchors)) = this.shell.heading_anchors.get(&tab) else {
                                return;
                            };
                            let anchors = anchors.clone();
                            this.cancel_position_resume();
                            let ticket = std::rc::Rc::new(FootnoteReturn {
                                source: source.clone(),
                                tab,
                                displayed: external_displayed.clone(),
                                anchors,
                                interaction: this.shell.positions.interaction,
                                geometry,
                                origin,
                            });
                            let source = source.clone();
                            let displayed = external_displayed.clone();
                            let link = link.clone();
                            let weak = cx.entity().downgrade();
                            window.on_next_frame(move |window, cx| {
                                let _ = weak.update(cx, |this, cx| {
                                    if !footnote_jump_allowed(
                                        &source,
                                        this.current_link_source().as_ref(),
                                        &displayed,
                                        &this.displayed_content(),
                                        &link,
                                        this.footnote_reader_available(cx),
                                    ) {
                                        return;
                                    }
                                    let markdown::LinkTarget::Footnote { label } = &link.target
                                    else {
                                        return;
                                    };
                                    if !this.footnote_jump_pending_current(&ticket, window, cx)
                                        || this
                                            .shell
                                            .reader_scrolls
                                            .get(&ticket.tab)
                                            .map(|h| h.offset())
                                            != Some(ticket.origin)
                                    {
                                        return;
                                    }
                                    if ticket.anchors.scroll_to_footnote(label, window, cx) {
                                        // A return restores the actual clicked origin. Scheduling
                                        // the target does not claim its movement has completed.
                                        let weak = cx.entity().downgrade();
                                        window.on_next_frame(move |window, cx| {
                                            let _ = weak.update(cx, |this, cx| {
                                                if this.footnote_jump_pending_current(
                                                    &ticket, window, cx,
                                                ) {
                                                    this.shell.footnote_return = Some(ticket);
                                                    cx.notify();
                                                }
                                            });
                                            window.refresh();
                                        });
                                        cx.notify();
                                    }
                                });
                            });
                            window.refresh();
                        } else if external_links::destination(&link.target).is_some() {
                            // Browser dispatch is not a note transition: never save, discard,
                            // or mutate the editor, tab, selection, or draft here.
                            if !this.find_available() || this.read_error.is_some() {
                                return;
                            }
                            let current_source = this.current_link_source();
                            let current_displayed = this.displayed_content();
                            external_links::dispatch(
                                source,
                                current_source.as_ref(),
                                &external_displayed,
                                &current_displayed,
                                link,
                                |url| cx.open_url(url),
                            );
                        } else {
                            this.activate_internal_target(&link.target, source, cx);
                        }
                    });
                }
            }),
        };
        if let Some(session) = &self.live {
            let generation = session.client().launch_generation;
            if self.shell.vault_generation != Some(generation) {
                self.shell.vault_generation = Some(generation);
                self.shell.initialize_working_scope(generation);
                self.shell.vault_discovery = VaultDiscovery::parse(&session.initial_vaults).ok();
                self.shell.vault_loading = false;
                self.shell.vault_error = None;
                self.refresh_vault_discovery(cx);
            }
        }
        let show_vault_switcher = self.live.is_some()
            && self
                .shell
                .vault_discovery
                .as_ref()
                .map(|discovery| discovery.visible())
                .unwrap_or(!self.shell.vault_loading && self.shell.vault_error.is_some());
        let vault_label = self
            .shell
            .vault_discovery
            .as_ref()
            .map(|discovery| discovery.label(self.working_vault_scope()))
            .unwrap_or_else(|| "Vaults unavailable".into());
        let vault_width = if show_vault_switcher { 160. } else { 0. };
        let reference_vault = self
            .current_note
            .as_ref()
            .map(|body| body.note.vault.clone());
        let wide = window.viewport_size().width >= px(1100.);
        let height = window.viewport_size().height;
        let note = self
            .selected
            .and_then(|i| self.snapshot.notes.get(i))
            .cloned();
        let home = self.shell.home && !self.editing_ui.active();
        if let Some(body) = &self.current_note {
            self.tabs.update_title(
                &tabs::TabKey::Note {
                    vault: body.note.vault.clone(),
                    note_id: body.note.note_id,
                },
                body.note.title.clone(),
            );
        }
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        self.search.update(cx, |search, _| search.set_dark(dark));
        let full_graph_active = self.full_graph_active();
        let calendar_active = self.calendar_active();
        self.graph_view.update(cx, |graph, cx| {
            graph.set_dark(dark, cx);
            graph.set_compact(true, cx);
            graph.set_overview(full_graph_active, cx);
        });
        let title = self
            .tag_page
            .as_ref()
            .map(|page| format!("#{}", page.tag))
            .or_else(|| {
                self.current_note
                    .as_ref()
                    .map(|body| body.note.title.clone())
                    .or_else(|| note.as_ref().map(|n| n.title.clone()))
                    .or_else(|| self.tabs.active().map(|tab| tab.title.clone()))
            })
            .unwrap_or_default();
        let reader_focus = self
            .shell
            .reader_focus
            .get_or_insert_with(|| cx.focus_handle())
            .clone();
        let reader_key = self
            .tag_page
            .as_ref()
            .map(|page| tabs::TabKey::Tag {
                tag: page.tag.clone(),
                scope: page.scope.clone(),
            })
            .unwrap_or_else(|| {
                self.tabs
                    .active()
                    .map(|tab| tab.key.clone())
                    .unwrap_or(tabs::TabKey::Home)
            });
        let task_inspector = self.note_tasks_inspector(cx);
        let includes_inspector = self.includes_inspector(cx);
        let children_relations = self.relation_section("children", cx);
        let backlink_relations = self.relation_section("backlinks", cx);
        let journal_day = self.journal_day_section(cx);
        let note_labels = crate::note_metadata::Metadata::current(
            self.live.is_some(),
            &reader_key,
            self.current_note.as_ref(),
            self.current_note_request.as_ref(),
            note.as_ref()
                .map(|note| note.tags.as_slice())
                .unwrap_or_default(),
        );
        let reader_scroll = if self.tag_page.is_some() {
            gpui::ScrollHandle::new() // no note scroll or retained per-tag reader allocation
        } else {
            self.shell
                .reader_scrolls
                .entry(reader_key.clone())
                .or_default()
                .clone()
        };
        self.sync_find(reader_key.clone(), &reader_scroll, dark, window, cx);
        let code_controls = self.code_controls(&reader_scroll, cx);
        let content_ready = self.current_note.is_some() || !self.content.is_empty();
        let displayed_content = self.displayed_content();
        let position_source = self
            .position_source()
            .ok()
            .or_else(|| self.follow_layout_source(cx))
            .or_else(|| self.block_fragment_layout_source(cx))
            .or_else(|| self.includes_layout_source())
            .or_else(|| self.query_layout_source())
            .or_else(|| self.pdf_layout_source())
            .or_else(|| self.math_layout_source());
        if self.shell.positions.source != position_source {
            self.shell.positions.source = position_source.clone();
            self.shell.positions.anchors = position_source.as_ref().and_then(|_| {
                markdown::PositionAnchors::new(self.content.len(), &reader_scroll)
                    .map(std::rc::Rc::new)
            });
        }
        let position_anchors = self.shell.positions.anchors.clone();
        self.schedule_follow_reveal(window, cx);
        let position_controls = self.position_controls(window, cx);
        let mut headings = markdown::outline(&self.content);
        let outline_signature = format!(
            "{headings:?}:{:?}:{:?}",
            markdown::footnote_targets(&self.content),
            markdown::authored_block_targets(&self.content)
        );
        let heading_anchors = if self.tag_page.is_some() {
            std::rc::Rc::new(markdown::HeadingAnchors::new(&self.content, &reader_scroll))
        } else {
            let anchor_entry = self
                .shell
                .heading_anchors
                .entry(reader_key.clone())
                .or_insert_with(|| {
                    (
                        outline_signature.clone(),
                        std::rc::Rc::new(markdown::HeadingAnchors::new(
                            &self.content,
                            &reader_scroll,
                        )),
                    )
                });
            if anchor_entry.0 != outline_signature {
                *anchor_entry = (
                    outline_signature,
                    std::rc::Rc::new(markdown::HeadingAnchors::new(&self.content, &reader_scroll)),
                );
            }
            anchor_entry.1.clone()
        };
        self.schedule_fragment_scroll(heading_anchors.clone(), reader_scroll.clone(), window, cx);
        for entry in &mut headings {
            entry.title = displayed_content[entry.block_index].text.clone();
        }
        let minimum_heading_level = headings.iter().map(|entry| entry.level).min().unwrap_or(1);
        let tab_layout = self
            .tabs
            .layout((f32::from(window.viewport_size().width) - vault_width).max(0.) as u32);
        let tab = div()
            .h(px(38.))
            .flex_shrink_0()
            .flex()
            .items_center()
            .border_b_1()
            .border_color(rgb(p.line))
            .children(
                tab_layout
                    .visible
                    .iter()
                    .enumerate()
                    .filter_map(|(index, key)| {
                        let entry = self.tabs.get(key)?;
                        let title = format!(
                            "{}{}",
                            entry.title,
                            if self.editing_ui.dirty_for(key) {
                                " •"
                            } else {
                                ""
                            }
                        );
                        let activate = key.clone();
                        let close = key.clone();
                        let middle_close = key.clone();
                        Some(
                            div()
                                .id(("tab", index))
                                .w(px(if index == 0 {
                                    tabs::ACTIVE_WIDTH
                                } else {
                                    tabs::INACTIVE_WIDTH
                                } as f32))
                                .min_w_0()
                                .flex_shrink_0()
                                .max_w(
                                    (window.viewport_size().width
                                        - px(vault_width)
                                        - px(if tab_layout.overflow_count() > 0 {
                                            tabs::OVERFLOW_WIDTH as f32
                                        } else {
                                            0.
                                        }))
                                    .max(px(0.)),
                                )
                                .h_full()
                                .px_3()
                                .flex()
                                .items_center()
                                .border_b_2()
                                .border_color(rgb(if index == 0 { p.mark } else { p.paper }))
                                .text_size(px(14.))
                                .font_weight(if index == 0 {
                                    gpui::FontWeight::MEDIUM
                                } else {
                                    gpui::FontWeight::NORMAL
                                })
                                .text_color(rgb(if index == 0 { p.ink } else { p.muted }))
                                .cursor_pointer()
                                .child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .overflow_hidden()
                                        .text_ellipsis()
                                        .child(title),
                                )
                                .child(
                                    div()
                                        .id(("close-tab", index))
                                        .w(px(24.))
                                        .h(px(24.))
                                        .flex_shrink_0()
                                        .flex()
                                        .items_center()
                                        .justify_center()
                                        .text_color(rgb(p.muted))
                                        .child("×")
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            cx.stop_propagation();
                                            this.close_tab(&close, cx);
                                        })),
                                )
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    this.activate_tab(activate.clone(), cx)
                                }))
                                .on_mouse_down(
                                    gpui::MouseButton::Middle,
                                    cx.listener(move |this, _, _, cx| {
                                        cx.stop_propagation();
                                        this.close_tab(&middle_close, cx);
                                    }),
                                ),
                        )
                    }),
            )
            .when(tab_layout.overflow_count() > 0, |bar| {
                bar.child(
                    div()
                        .id("tab-overflow")
                        .w(px(tabs::OVERFLOW_WIDTH as f32))
                        .h_full()
                        .flex_shrink_0()
                        .flex()
                        .items_center()
                        .justify_center()
                        .cursor_pointer()
                        .text_size(px(13.))
                        .child(format!("+{}", tab_layout.overflow_count()))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.tab_menu = !this.shell.tab_menu;
                            cx.notify();
                        })),
                )
            });
        let tab = tab.child(div().flex_1()).when(show_vault_switcher, |bar| {
            bar.child(
                div()
                    .id("vault-switcher")
                    .w(px(vault_width))
                    .h_full()
                    .flex_shrink_0()
                    .px_3()
                    .flex()
                    .items_center()
                    .justify_end()
                    .text_size(px(13.))
                    .cursor_pointer()
                    .child(vault_label)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.shell.vault_menu = !this.shell.vault_menu;
                        this.shell.tab_menu = false;
                        cx.notify();
                    })),
            )
        });
        let vault_menu=div().id("vault-menu").absolute().right(px(8.)).top(px(40.)).w(px(320.)).max_h(px(460.)).overflow_y_scroll().bg(rgb(p.paper)).border_1().border_color(rgb(p.line)).rounded_lg().py_2()
            .when_some(self.shell.vault_discovery.clone(),|menu,discovery| {
                let launch=discovery.launch_name.clone();
                menu.children(discovery.available.iter().enumerate().map(|(index,entry)|{
                    let name=entry.name.clone();
                    let label=if name.is_empty(){"local".to_owned()}else{name.clone()};
                    let is_launch=name==launch;
                    div().id(("available-vault",index)).px_4().py_2().text_size(px(13.)).cursor_pointer().child(format!("{}{}{}",label,if is_launch{" · launch"}else{""},if discovery.scope_for(&name).as_deref()==Some(self.working_vault_scope()){" ✓"}else{""}))
                        .on_click(cx.listener(move|this,_,_,cx|{if let Some(scope)=this.shell.vault_discovery.as_ref().and_then(|d|d.scope_for(&name)){this.change_working_vault(scope,cx);}}))
                })).children(discovery.unavailable.iter().map(|entry|div().px_4().py_2().text_size(px(12.)).text_color(rgb(p.muted)).child(format!("{} · unavailable",if entry.name.is_empty(){"local"}else{&entry.name})).child(div().text_size(px(11.)).child(entry.error.clone().unwrap_or_default()))))
            })
            .when_some(self.shell.vault_error.clone(),|menu,error|menu.child(div().px_4().py_2().text_size(px(11.)).text_color(rgb(p.mark)).child(format!("{} {error}", if self.shell.vault_discovery.is_some() {"Discovery incomplete; prior list may be stale."} else {"Vault discovery unavailable."}))))
            .child(div().px_4().py_2().text_size(px(11.)).text_color(rgb(p.muted)).child("Session only. Existing tabs, launch Home, and workspace search are unchanged."))
            .child(div().id("refresh-vaults").px_4().py_2().text_size(px(12.)).when(!self.shell.vault_loading,|row|row.cursor_pointer().on_click(cx.listener(|this,_,_,cx|{this.refresh_vault_discovery(cx);cx.notify();}))).child(if self.shell.vault_loading{"Refreshing…"}else{"Refresh vault list"}));
        let tab_menu = div()
            .id("tab-overflow-menu")
            .absolute()
            .right(px(8.))
            .top(px(40.))
            .w(px(300.))
            .max_h(px(400.))
            .overflow_y_scroll()
            .bg(rgb(p.paper))
            .border_1()
            .border_color(rgb(p.line))
            .rounded_lg()
            .py_2()
            .children(
                tab_layout
                    .overflow
                    .iter()
                    .enumerate()
                    .filter_map(|(index, key)| {
                        let title = format!(
                            "{}{}",
                            self.tabs.get(key)?.title,
                            if self.editing_ui.dirty_for(key) {
                                " •"
                            } else {
                                ""
                            }
                        );
                        let activate = key.clone();
                        Some(
                            div()
                                .id(("overflow-tab", index))
                                .px_4()
                                .py_2()
                                .text_size(px(14.))
                                .cursor_pointer()
                                .child(title)
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    this.activate_tab(activate.clone(), cx)
                                })),
                        )
                    }),
            );
        let rail = div()
            .absolute()
            .left(px(8.))
            .top((height - px(250.)) / 2.)
            .w(px(50.))
            .py_2()
            .flex()
            .flex_col()
            .items_center()
            .gap_1()
            .bg(rgb(p.paper))
            .border_1()
            .border_color(rgb(p.line))
            .rounded_lg()
            .children(
                [
                    (false, "history-back", "Back"),
                    (true, "history-forward", "Forward"),
                ]
                .into_iter()
                .map(|(forward, id, label)| {
                    let enabled = self.history.peek(forward).is_some();
                    div()
                        .id(id)
                        .text_size(px(10.))
                        .text_color(rgb(p.muted))
                        .opacity(if enabled { 1.0 } else { 0.4 })
                        .child(label)
                        .when(enabled, |button| {
                            button.cursor_pointer().on_click(
                                cx.listener(move |this, _, _, cx| {
                                    this.navigate_history(forward, cx)
                                }),
                            )
                        })
                }),
            )
            .child(
                div()
                    .id("recent-toggle")
                    .py_2()
                    .text_size(px(10.))
                    .cursor_pointer()
                    .child("Recent")
                    .on_click(cx.listener(|this, _, window, cx| this.toggle_recent(window, cx))),
            )
            .when(
                !home && !full_graph_active && !calendar_active && self.tag_page.is_none(),
                |rail| rail.child(self.reader_hierarchy_entry(window, cx)),
            )
            .child(
                rail_button("home", "home", p, home).on_click(cx.listener(|this, _, _, cx| {
                    this.open_home(cx);
                })),
            )
            .child(
                rail_button("search", "search", p, self.shell.search_open).on_click(cx.listener(
                    |this, _, window, cx| {
                        if this.shell.search_open {
                            this.dismiss_search(window, cx);
                        } else {
                            this.shell.search_return_focus = window
                                .focused(cx)
                                .filter(|focus| focus != &this.search.focus_handle(cx));
                            this.shell.search_open = true;
                            window.focus(&this.search.focus_handle(cx));
                        }
                        cx.notify();
                    },
                )),
            )
            .child(
                div()
                    .id("calendar-open")
                    .text_size(px(9.))
                    .cursor_pointer()
                    .child("Calendar")
                    .on_click(cx.listener(|this, _, _, cx| this.open_calendar(cx))),
            )
            .child(
                div()
                    .id("full-graph-open")
                    .text_size(px(9.))
                    .cursor_pointer()
                    .child("Full graph")
                    .on_click(cx.listener(|this, _, _, cx| this.open_full_graph(cx))),
            )
            .when(self.find_available(), |rail| {
                rail.child(
                    div()
                        .id("reader-find-open")
                        .w(px(32.))
                        .h(px(28.))
                        .flex()
                        .items_center()
                        .justify_center()
                        .text_size(px(10.))
                        .text_color(rgb(p.muted))
                        .cursor_pointer()
                        .child("Find")
                        .on_click(cx.listener(|this, _, window, cx| this.open_find(window, cx))),
                )
            })
            .when_some(self.inline_includes_sidebar(cx), |rail, control| {
                rail.child(control)
            })
            .when_some(self.shell.footnote_return.clone(), |rail, ticket| {
                rail.child(
                    div()
                        .id("footnote-return")
                        .w(px(46.))
                        .py_1()
                        .text_size(px(9.))
                        .text_color(rgb(p.mark))
                        .cursor_pointer()
                        .child("Return to reference")
                        .on_click(cx.listener(move |this, _, window, cx| {
                            this.return_from_footnote(&ticket, window, cx);
                        })),
                )
            })
            .when(self.creation_available(), |rail| {
                rail.child(
                    div()
                        .id("create-note-open")
                        .px_1()
                        .py_2()
                        .text_xs()
                        .cursor_pointer()
                        .child("New")
                        .on_click(cx.listener(|this, _, _, cx| this.request_create_note(cx))),
                )
            })
            .child(
                rail_button("reload", "reload", p, false)
                    .on_click(cx.listener(|this, _, _, cx| this.load_notes(cx))),
            )
            .when(
                self.editing_ui.enabled()
                    && !full_graph_active
                    && !calendar_active
                    && self.tag_page.is_none(),
                |rail| rail.child(self.render_edit_controls(cx)),
            )
            .child(div().w(px(24.)).h(px(1.)).my_1().bg(rgb(p.line)))
            .child(
                rail_button("theme", "theme", p, false).on_click(cx.listener(
                    move |this, _, _, cx| {
                        this.shell.dark = Some(!dark);
                        cx.notify();
                    },
                )),
            )
            .child(
                rail_button("diagnostics", "info", p, self.shell.diagnostics).on_click(
                    cx.listener(|this, _, _, cx| {
                        this.shell.diagnostics = !this.shell.diagnostics;
                        cx.notify();
                    }),
                ),
            );
        let aside = div()
            .id("context-rail")
            .overflow_y_scroll()
            .w(px(
                (f32::from(window.viewport_size().width) * 0.24).clamp(240., 380.)
            ))
            .flex_shrink_0()
            .when(wide, |aside| aside.h_full())
            .pt(px(48.))
            .pr_6()
            .text_size(px(14.))
            .text_color(rgb(p.muted))
            .when(!note_labels.tags.is_empty(), |aside| {
                aside.child(div().mb_8().child(label("TAGS", p)).children(
                    note_labels.tags.iter().enumerate().map(|(index, tag)| {
                        let source = self.current_link_source();
                        let tag = tag.clone();
                        div()
                            .id(("reader-tag", index))
                            .mb_2()
                            .text_color(rgb(p.ink))
                            .cursor_pointer()
                            .child(metadata_text(&format!("#{tag}")))
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.activate_reader_tag(source.as_ref(), &tag, cx);
                            }))
                    }),
                ))
            })
            .when(headings.len() > 1, |aside| {
                aside.child(div().mb_8().child(label("CONTENTS", p)).children(
                    headings.into_iter().enumerate().map(|(index, heading)| {
                        let anchors = heading_anchors.clone();
                        let anchor = heading.anchor;
                        div()
                            .id(("outline", index))
                            .mb_2()
                            .pl(px((heading.level.saturating_sub(minimum_heading_level)
                                as f32)
                                * 28.))
                            .cursor_pointer()
                            .child(heading.title)
                            .on_click(cx.listener(move |this, _, window, cx| {
                                this.cancel_position_resume();
                                if anchors.scroll_to(&anchor, window, cx) {
                                    cx.notify();
                                }
                                if let Some(focus) = &this.shell.reader_focus {
                                    window.focus(focus);
                                }
                            }))
                    }),
                ))
            });
        let aside = aside
            .when_some(self.math_inspector(cx), |aside, math| aside.child(math))
            .when_some(self.pdf_inspector(window, cx), |aside, pdf| {
                aside.child(pdf)
            })
            .child(journal_day)
            .child(children_relations)
            .child(backlink_relations);
        // Derive only from the accepted response; no additional reads or cache.
        let external =
            crate::external_refs::ExternalReferences::from_related(&self.current_related);
        let external_source = self.relation_source();
        let external_scope = self.shell.scope_revision;
        let aside = aside.when(external.visible(), |aside| {
            aside.child(
                div()
                    .mt_6()
                    .child(label("FROM OTHER VAULTS", p))
                    .children(external.references.into_iter().enumerate().map(
                        |(index, reference)| {
                            let vault_label = if reference.key.vault.is_empty() {
                                "Launch vault"
                            } else {
                                &reference.key.vault
                            };
                            div()
                                .id(("external-reference", index))
                                .py_1()
                                .child(div().text_size(px(12.)).child(vault_label.to_owned()))
                                .child(div().text_color(rgb(p.ink)).child(
                                    crate::note_metadata::relation_title(
                                        &reference.title,
                                        &reference.flags,
                                    ),
                                ))
                                .when_some(external_source.clone(), |row, source| {
                                    row.cursor_pointer().on_click(cx.listener(
                                        move |this, _, window, cx| {
                                            if this.relation_source().as_ref() == Some(&source)
                                                && this.relation_control_available(
                                                    external_scope,
                                                    window,
                                                    cx,
                                                )
                                                && crate::external_refs::contains_at(
                                                    &this.current_related,
                                                    index,
                                                    &reference,
                                                )
                                            {
                                                this.open_live_key(reference.key.clone(), cx);
                                            }
                                        },
                                    ))
                                })
                        },
                    ))
                    .children(
                        external
                            .notices
                            .into_iter()
                            .map(|notice| div().py_1().child(notice)),
                    ),
            )
        });
        let aside = aside.when(self.live.is_some() || self.backend.is_some(), |aside| {
            aside.child(
                div()
                    .mb_4()
                    .border_t_1()
                    .border_color(rgb(p.line))
                    .mt_6()
                    .pt_4()
                    .child(label("GRAPH", p))
                    .when(!self.show_graph, |graph| {
                        graph.child(
                            div()
                                .id("load-local-graph")
                                .cursor_pointer()
                                .text_color(rgb(p.mark))
                                .child("Show connections")
                                .on_click(cx.listener(|this, _, _, cx| this.load_graph(cx))),
                        )
                    })
                    .when(self.show_graph, |graph| {
                        graph.child(div().h(px(280.)).child(self.graph_view.clone()))
                    }),
            )
        });
        let mut aside = Some(aside);
        let property_projection = self
            .shell
            .links
            .as_ref()
            .filter(|cache| self.current_link_source().as_ref() == Some(&cache.source))
            .map(|cache| cache.properties.clone())
            .unwrap_or_else(|| {
                std::rc::Rc::new(crate::note_properties::project(
                    &self
                        .current_note
                        .as_ref()
                        .map(|note| &note.props)
                        .unwrap_or(&serde_json::Value::Null),
                ))
            });
        let property_source = self.current_link_source();
        let mut metadata: Vec<(String, Vec<(String, Option<usize>)>)> = property_projection
            .groups()
            .into_iter()
            .map(|group| {
                (
                    group.key,
                    group
                        .rows
                        .into_iter()
                        .map(|index| (property_projection.rows[index].value.clone(), Some(index)))
                        .collect(),
                )
            })
            .collect();
        if let Some(body) = &self.current_note {
            if let Some(created) = body.note.additional.get("created").and_then(|v| v.as_str()) {
                metadata.push(("CREATED".into(), vec![(created.into(), None)]));
            }
            if let Some(updated) = body
                .note
                .additional
                .get("updated")
                .and_then(|v| v.as_i64())
                .and_then(display_date)
            {
                metadata.push(("UPDATED".into(), vec![(updated, None)]));
            }
        }
        let trail_result = reader_relations::entries(self.current_related.get("trail"));
        let trail_error = trail_result.as_ref().err().copied();
        let trail = trail_result.ok().flatten().unwrap_or_default().to_vec();
        let reader = div()
            .flex_1()
            .min_w_0()
            .h_full()
            .flex()
            .justify_center()
            .pl(px(64.))
            .pr(px(32.))
            .pt(px(32.))
            .child(
                div()
                    .id(gpui::SharedString::from(format!(
                        "note-reader:{reader_key:?}"
                    )))
                    .track_focus(&reader_focus)
                    .when(content_ready, |reader| reader.track_scroll(&reader_scroll))
                    .flex_1()
                    .max_w(px(880.))
                    .min_w_0()
                    .h_full()
                    .overflow_y_scroll()
                    .pb_12()
                    .when_some(trail_error, |reader, error| reader.child(error))
                    .when(!trail.is_empty(), |reader| {
                        reader.child(
                            div()
                                .text_size(px(13.))
                                .mb_4()
                                .flex()
                                .flex_wrap()
                                .gap_1()
                                .children(trail.iter().enumerate().filter_map(|(index, entry)| {
                                    reference_row(
                                        entry,
                                        reference_vault.as_deref(),
                                        ("breadcrumb-reference", index),
                                        self.relation_source(),
                                        self.shell.scope_revision,
                                        "trail",
                                        cx,
                                    )
                                    .map(|row| {
                                        div().flex().items_center().gap_1().child(row).child("›")
                                    })
                                })),
                        )
                    })
                    .child(position_controls)
                    .child(task_inspector)
                    .child(includes_inspector)
                    .when_some(note_labels.milestone, |reader, label| {
                        reader.child(
                            div()
                                .text_size(px(12.))
                                .text_color(rgb(p.muted))
                                .mb_2()
                                .child(label),
                        )
                    })
                    .when(!note_labels.flags.is_empty(), |reader| {
                        reader.child(
                            div()
                                .mb_4()
                                .text_size(px(13.))
                                .min_w_0()
                                .max_w_full()
                                .flex()
                                .flex_wrap()
                                .gap_2()
                                .children(
                                    note_labels.flags.iter().map(|flag| metadata_badge(flag, p)),
                                ),
                        )
                    })
                    .when(!metadata.is_empty(), |reader| {
                        reader.child(
                            div().flex().flex_col().gap_2().mb(px(36.)).children(
                                metadata
                                    .into_iter()
                                    .enumerate()
                                    .map(|(index, (key, values))| {
                                        let values = div()
                                            .min_w_0()
                                            .flex_1()
                                            .flex()
                                            .flex_wrap()
                                            .gap_3()
                                            .children(values.into_iter().enumerate().map(
                                                |(value_index, (value, property_index))| {
                                                    let verified = property_index
                                                        .and_then(|i| {
                                                            property_projection.rows.get(i)
                                                        })
                                                        .zip(property_source.as_ref())
                                                        .is_some_and(|(row, source)| {
                                                            self.footnote_reader_available(cx)
                                                                && self.property_link_verified(
                                                                    row, source,
                                                                )
                                                        });
                                                    let captured = property_projection.clone();
                                                    let source = property_source.clone();
                                                    let value = div()
                                                    .id(gpui::SharedString::from(format!(
                                                        "typed-property-{index}-{value_index}"
                                                    )))
                                                    .min_w_0()
                                                    .max_w_full()
                                                    .text_size(px(13.))
                                                    .text_color(rgb(if verified {
                                                        p.mark
                                                    } else {
                                                        p.muted
                                                    }))
                                                    .child(metadata_text(&value))
                                                    .when(verified, |value| {
                                                        value.cursor_pointer().on_click(cx.listener(
                                                move |this, _, _, cx| {
                                                    let Some(row) = property_index
                                                        .and_then(|i| captured.rows.get(i))
                                                    else {
                                                        return;
                                                    };
                                                    let Some(source) = &source else {
                                                        return;
                                                    };
                                                    if this.footnote_reader_available(cx)
                                                        && this.property_link_verified(row, source)
                                                    {
                                                        if let Some(target) = &row.target {
                                                            this.activate_internal_target(
                                                                target, source, cx,
                                                            );
                                                        }
                                                    }
                                                },
                                            ))
                                                    });
                                                    value
                                                },
                                            ));
                                        div()
                                            .min_w_0()
                                            .max_w_full()
                                            .flex()
                                            .gap_3()
                                            .child(
                                                div()
                                                    .min_w_0()
                                                    .max_w(px(160.))
                                                    .font_family("monospace")
                                                    .text_size(px(11.))
                                                    .text_color(rgb(p.muted))
                                                    .child(metadata_text(&key)),
                                            )
                                            .child(values)
                                    }),
                            ),
                        )
                    })
                    .when(
                        self.current_note.is_some() && property_projection.unavailable,
                        |reader| {
                            reader.child(
                                div()
                                    .text_size(px(11.))
                                    .text_color(rgb(p.muted))
                                    .mb_6()
                                    .child(
                                        "Some additional metadata is unavailable in this response.",
                                    ),
                            )
                        },
                    )
                    .child(
                        div()
                            .max_w(px(656.))
                            .pl(px(16.))
                            .text_size(px(16.))
                            .line_height(px(29.6))
                            .child(
                                div()
                                    .text_size(px(26.))
                                    .line_height(px(36.4))
                                    .font_weight(gpui::FontWeight::MEDIUM)
                                    .mb(px(6.))
                                    .flex()
                                    .flex_wrap()
                                    .items_start()
                                    .gap_2()
                                    .child(div().min_w_0().max_w_full().child(title))
                                    .when(self.current_note.is_some(), |row| {
                                        let epoch = self.shell.title_copy.epoch;
                                        row.child(
                                            div()
                                                .id("copy-reader-title")
                                                .w(px(110.))
                                                .flex_shrink_0()
                                                .text_size(px(12.))
                                                .line_height(px(22.))
                                                .when(
                                                    self.shell.title_copy.source.is_some(),
                                                    |button| button.cursor_pointer(),
                                                )
                                                .text_color(rgb(p.muted))
                                                .child(
                                                    if self.shell.title_copy.feedback.is_some() {
                                                        "Copy submitted"
                                                    } else {
                                                        "Copy title"
                                                    },
                                                )
                                                .on_mouse_down(
                                                    gpui::MouseButton::Left,
                                                    |_, _, cx| cx.stop_propagation(),
                                                )
                                                .on_click(cx.listener(
                                                    move |this, _, window, cx| {
                                                        this.copy_title(epoch, window, cx);
                                                        cx.stop_propagation();
                                                    },
                                                )),
                                        )
                                    })
                                    .when(self.current_note.is_some(), |row| {
                                        row.child(self.copy_markdown_control(p.muted, window, cx))
                                            .child(self.selection_copy_control(p.muted, window, cx))
                                            .child(
                                                self.selected_markdown_control(p.muted, window, cx),
                                            )
                                            .child(
                                                self.selected_reference_control(
                                                    p.muted, window, cx,
                                                ),
                                            )
                                    }),
                            )
                            .child(markdown::render_with_positions(
                                &displayed_content,
                                dark,
                                Some(&heading_anchors),
                                &link_callbacks,
                                self.shell
                                    .find
                                    .highlighted
                                    .then(|| markdown::FindPresentation {
                                        matches: &self.shell.find.matches,
                                        active: self.shell.find.active,
                                        anchors: self.shell.find.anchors.as_ref().unwrap(),
                                    }),
                                Some(&code_controls),
                                Some(&self.reader_images.images),
                                position_anchors.as_deref(),
                                self.inline_include_rows(),
                                self.query_inline_panel(cx),
                            ))
                            .when_some(self.reader_images.notice.clone(), |view, notice| {
                                view.child(div().text_size(px(11.)).child(notice))
                            })
                            .on_mouse_down(
                                gpui::MouseButton::Left,
                                cx.listener(|this, _, window, _| {
                                    if let Some(focus) = &this.shell.reader_focus {
                                        window.focus(focus);
                                    }
                                }),
                            ),
                    )
                    .when(!wide, |body| body.child(aside.take().unwrap())),
            );
        let home_view = div()
            .flex_1()
            .flex()
            .flex_col()
            .items_center()
            .justify_center()
            .pl(px(70.))
            .pb(px(100.))
            .child(if home {
                self.home_activity(
                    f32::from(window.viewport_size().width - px(120.))
                        .min(520.)
                        .max(80.),
                    dark,
                    cx,
                )
            } else {
                div()
            })
            .child(if home {
                div()
                    .child(self.home_open_tasks(dark, cx))
                    .child(self.home_weekly_tasks(dark, cx))
            } else {
                div()
            })
            .child(if home {
                self.home_hierarchy(dark, cx)
            } else {
                div()
            })
            .when(!self.shell.search_open, |home| {
                home.child(
                    div()
                        .id("home-search")
                        .w(px(520.))
                        .max_w_full()
                        .child(self.search.clone())
                        .on_mouse_down(
                            gpui::MouseButton::Left,
                            cx.listener(|this, _, window, cx| {
                                this.shell.search_return_focus = window
                                    .focused(cx)
                                    .filter(|focus| focus != &this.search.focus_handle(cx));
                                this.shell.search_open = true;
                                cx.notify();
                            }),
                        ),
                )
            });
        let query = self.search.read(cx).query().to_lowercase();
        let result_indices = self.search_result_indices(cx);
        let search_panel = div()
            .absolute()
            .left(
                (window.viewport_size().width
                    - px(640.).min(window.viewport_size().width - px(48.)))
                    / 2.,
            )
            .top(height * 0.12)
            .h(if query.trim().is_empty() {
                px(72.)
            } else {
                (px(74.) + px(result_indices.len().max(1) as f32 * 64.)).min(height * 0.70)
            })
            .w(px(640.).min(window.viewport_size().width - px(48.)))
            .bg(rgb(p.paper))
            .border_1()
            .border_color(rgb(p.line))
            .rounded_lg()
            .flex()
            .flex_col()
            .overflow_hidden()
            .child(div().px_4().py_3().child(self.search.clone()))
            .child(
                uniform_list(
                    "notes",
                    result_indices.len(),
                    cx.processor(move |this, range: std::ops::Range<usize>, _, cx| {
                        this.rendered_rows = range.len();
                        let query_generation =
                            this.query_epoch.load(std::sync::atomic::Ordering::SeqCst);
                        range
                            .filter_map(|row| {
                                let i = *result_indices.get(row)?;
                                let (title, tags) = if this.live.is_some() {
                                    let note = this.search_results.get(i)?;
                                    (note.title.clone(), note.tags.clone())
                                } else {
                                    let note = this.snapshot.notes.get(i)?;
                                    (note.title.clone(), note.tags.clone())
                                };
                                let refinement = this
                                    .shell
                                    .search_refinement
                                    .clone()
                                    .filter(|stamp| {
                                        this.search_refinement_stamp(this.search.read(cx).query())
                                            .as_ref()
                                            == Some(stamp)
                                    })
                                    .map(std::rc::Rc::new);
                                let key = this
                                    .search_results
                                    .get(i)
                                    .filter(|_| this.live.is_some())
                                    .map(|row| row.key());
                                Some(
                                    div()
                                        .id(i)
                                        .h(px(64.))
                                        .px_5()
                                        .py_3()
                                        .overflow_hidden()
                                        .cursor_pointer()
                                        .border_b_1()
                                        .border_color(rgb(p.line))
                                        .text_color(rgb(p.ink))
                                        .when(
                                            this.search.read(cx).selected_result() == Some(row),
                                            |row| row.bg(rgb(p.line)),
                                        )
                                        .child(
                                            div()
                                                .id(("search-result-title", i))
                                                .text_size(px(13.))
                                                .cursor_pointer()
                                                .child(title)
                                                .on_click(cx.listener(move |this, _, _, cx| {
                                                    if this
                                                        .query_epoch
                                                        .load(std::sync::atomic::Ordering::SeqCst)
                                                        != query_generation
                                                    {
                                                        return;
                                                    }
                                                    if this.live.is_some() {
                                                        this.select_search_result(i, cx);
                                                    } else {
                                                        this.select_from_ui(i, cx);
                                                    }
                                                })),
                                        )
                                        .child(
                                            div()
                                                .text_size(px(10.))
                                                .text_color(rgb(p.muted))
                                                .flex()
                                                .gap_2()
                                                .overflow_hidden()
                                                .children(tags.into_iter().enumerate().map(
                                                    |(tag_index, tag)| {
                                                        let ticket = refinement.clone();
                                                        let key = key.clone();
                                                        let enabled = ticket.is_some()
                                                            && key.is_some()
                                                            && search_refinement::valid_tag(&tag);
                                                        div()
                                                            .id(gpui::SharedString::from(format!(
                                                                "search-result-tag-{i}-{tag_index}"
                                                            )))
                                                            .child(format!("#{tag}"))
                                                            .when(enabled, |tag_view| {
                                                                tag_view.cursor_pointer()
                                                            })
                                                            .on_mouse_down(
                                                                gpui::MouseButton::Left,
                                                                |_, _, cx| cx.stop_propagation(),
                                                            )
                                                            .on_click(cx.listener(
                                                                move |this, _, window, cx| {
                                                                    if let (
                                                                        Some(ticket),
                                                                        Some(key),
                                                                    ) = (&ticket, &key)
                                                                    {
                                                                        this.refine_search_tag(
                                                                            ticket, key, &tag,
                                                                            window, cx,
                                                                        );
                                                                    }
                                                                    cx.stop_propagation();
                                                                },
                                                            ))
                                                    },
                                                )),
                                        ),
                                )
                            })
                            .collect()
                    }),
                )
                .track_scroll(self.shell.search_scroll.clone())
                .flex_1()
                .min_h_0(),
            );
        // A stable workspace focus target makes the native menu dispatch path
        // available on unfocused Home/Tag/Graph, without taking any existing focus.
        // It is intentionally distinct from reader_focus and never grants j/k.
        let workspace_focus = self
            .shell
            .workspace_focus
            .get_or_insert_with(|| cx.focus_handle())
            .clone();
        // FocusHandle can outlive its mounted element. Repair only recognized
        // handles whose exact surface is absent, never an arbitrary live input.
        let stale_focus = stale_workspace_focus(
            !home
                && !full_graph_active
                && !calendar_active
                && self.tag_page.is_none()
                && !self.editing_ui.active(),
            self.shell
                .reader_focus
                .as_ref()
                .is_some_and(|focus| focus.is_focused(window))
                || self
                    .shell
                    .code_focus
                    .values()
                    .any(|focus| focus.is_focused(window)),
            home || self.shell.search_open,
            self.search.focus_handle(cx).is_focused(window),
            full_graph_active
                || (!home
                    && self.tag_page.is_none()
                    && !self.editing_ui.active()
                    && self.show_graph),
            self.graph_view.read(cx).canvas_has_focus(window),
        );
        if workspace_root_focus_allowed(
            window.is_window_active(),
            window.focused(cx).is_some() && !stale_focus,
            self.native_history_owner_blocked(cx) && !self.native_find_available(false, window, cx),
        ) {
            window.focus(&workspace_focus);
        }
        div()
            .id("workspace-shell")
            .track_focus(&workspace_focus)
            .when(self.native_reload_available(window, cx), |root| {
                root.on_action(cx.listener(|this, _: &NativeReload, window, cx| {
                    this.native_reload(window, cx);
                    cx.stop_propagation();
                }))
            })
            .when(
                self.native_find_available(false, window, cx) && !self.inline_includes_active(),
                |root| {
                    root.on_action(cx.listener(|this, _: &NativeFind, window, cx| {
                        if this.native_find_available(false, window, cx) {
                            this.open_find(window, cx);
                        }
                        cx.stop_propagation();
                    }))
                },
            )
            .when(
                self.native_find_available(true, window, cx) && !self.inline_includes_active(),
                |root| {
                    root.on_action(cx.listener(|this, _: &NativeFindNext, window, cx| {
                        if this.native_find_available(true, window, cx) {
                            this.move_find(true, cx);
                        }
                        cx.stop_propagation();
                    }))
                    .on_action(cx.listener(
                        |this, _: &NativeFindPrevious, window, cx| {
                            if this.native_find_available(true, window, cx) {
                                this.move_find(false, cx);
                            }
                            cx.stop_propagation();
                        },
                    ))
                },
            )
            .when(self.native_history_available(false, cx), |root| {
                root.on_action(cx.listener(|this, _: &HistoryBack, window, cx| {
                    if native_history_dispatch_allowed(
                        window.is_window_active(),
                        cx.active_window().and_then(shortcut_workspace).is_some(),
                        this.native_history_available(false, cx),
                    ) {
                        this.navigate_history(false, cx);
                    }
                }))
            })
            .when(self.native_history_available(true, cx), |root| {
                root.on_action(cx.listener(|this, _: &HistoryForward, window, cx| {
                    if native_history_dispatch_allowed(
                        window.is_window_active(),
                        cx.active_window().and_then(shortcut_workspace).is_some(),
                        this.native_history_available(true, cx),
                    ) {
                        this.navigate_history(true, cx);
                    }
                }))
            })
            .child(self.handoff_entry(cx))
            .child(self.render_follow_controls(cx))
            .on_action(
                cx.listener(|this, _: &search_input::FindNext, _, cx| this.move_find(true, cx)),
            )
            .on_action(
                cx.listener(|this, _: &search_input::FindPrevious, _, cx| {
                    this.move_find(false, cx)
                }),
            )
            .on_action(
                cx.listener(|this, _: &search_input::CloseFind, window, cx| {
                    this.close_find(window, cx)
                }),
            )
            .on_action(
                cx.listener(|this, _: &search_input::PreviousResult, _, cx| {
                    this.step_search_result(false, cx)
                }),
            )
            .on_action(cx.listener(|this, _: &search_input::NextResult, _, cx| {
                this.step_search_result(true, cx)
            }))
            .on_action(
                cx.listener(|this, _: &search_input::OpenResult, window, cx| {
                    this.open_search_result(window, cx)
                }),
            )
            .on_action(
                cx.listener(|this, _: &search_input::DismissSearch, window, cx| {
                    this.dismiss_search(window, cx)
                }),
            )
            .on_mouse_down(
                gpui::MouseButton::Left,
                cx.listener(|this, _, _, _| {
                    this.clear_reader_selection();
                    this.shell.tab_cycle.clear();
                    this.shell.reader_keys = None;
                    this.cancel_follow_reveal();
                    this.shell.positions.interaction =
                        this.shell.positions.interaction.wrapping_add(1);
                }),
            )
            .on_mouse_move(
                cx.listener(|this, event: &gpui::MouseMoveEvent, window, cx| {
                    if event.pressed_button.is_some() {
                        this.shell.reader_keys = None;
                    }
                    let phase = if event.pressed_button == Some(gpui::MouseButton::Left) {
                        markdown::SelectionPhase::Move
                    } else {
                        markdown::SelectionPhase::Cancel
                    };
                    if this.reader_multi_pointer(phase, Some(event.position), window, cx) {
                        cx.stop_propagation();
                    }
                }),
            )
            .on_mouse_up(
                gpui::MouseButton::Left,
                cx.listener(|this, event: &gpui::MouseUpEvent, window, cx| {
                    if this.reader_multi_pointer(
                        markdown::SelectionPhase::End,
                        Some(event.position),
                        window,
                        cx,
                    ) {
                        cx.stop_propagation();
                    }
                }),
            )
            .on_mouse_down(
                gpui::MouseButton::Right,
                cx.listener(|this, _, _, _| this.shell.reader_keys = None),
            )
            .on_scroll_wheel(cx.listener(|this, _, _, _| {
                this.cancel_follow_reveal();
                this.shell.positions.interaction = this.shell.positions.interaction.wrapping_add(1);
            }))
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                if this.shell.selection.is_some() && event.keystroke.key == "escape" {
                    this.clear_reader_selection();
                    cx.notify();
                    cx.stop_propagation();
                    return;
                }
                if this.reader_selection_dragging() {
                    cx.stop_propagation();
                    return;
                }
                if this.recent_open() {
                    if event.keystroke.key == "escape" {
                        this.close_recent(window, cx);
                    }
                    cx.stop_propagation();
                    return;
                }
                if this.enlarged_image_open()
                    || this.reader_hierarchy_open()
                    || this.creation_open()
                {
                    cx.stop_propagation();
                    return;
                }
                this.cancel_follow_reveal();
                this.shell.positions.interaction = this.shell.positions.interaction.wrapping_add(1);
                if this.graph_entry_keyboard(event, window, cx) {
                    cx.stop_propagation();
                    return;
                }
                if this.preview_entry_keyboard(event, window, cx) {
                    cx.stop_propagation();
                    return;
                }
                if this.code_keyboard(event, window, cx) {
                    cx.stop_propagation();
                    return;
                }
                if this.reader_scroll_keyboard(event, window, cx) {
                    cx.stop_propagation();
                    return;
                }
                if event.keystroke.key == "escape" {
                    this.shell.reader_keys = None;
                    if this.shell.vault_menu {
                        this.shell.vault_menu = false;
                        cx.notify();
                        cx.stop_propagation();
                        return;
                    }
                    if this.shell.search_open {
                        // Leave Escape to the platform candidate/preedit handling.
                        if this.search.read(cx).is_composing() {
                            return;
                        }
                        this.dismiss_search(window, cx);
                        cx.stop_propagation();
                        return;
                    }
                    if this.shell.tab_menu {
                        this.shell.tab_menu = false;
                        cx.notify();
                        cx.stop_propagation();
                        return;
                    }
                    if this
                        .shell
                        .reader_focus
                        .as_ref()
                        .is_some_and(|focus| focus.is_focused(window))
                    {
                        window.blur();
                        cx.stop_propagation();
                    }
                    return;
                }
                if this.shell.search_open
                    || this.shell.vault_menu
                    || this.shell.tab_menu
                    || !this
                        .shell
                        .reader_focus
                        .as_ref()
                        .is_some_and(|focus| focus.is_focused(window))
                {
                    return;
                }
                let modifiers = event.keystroke.modifiers;
                if event.keystroke.key == "tab"
                    && !modifiers.platform
                    && !modifiers.control
                    && !modifiers.alt
                {
                    if this.tabs.entries().len() > 1 {
                        let mut order = std::mem::take(&mut this.shell.tab_cycle);
                        if order.len() != this.tabs.entries().len()
                            || !order.iter().all(|key| this.tabs.get(key).is_some())
                        {
                            order = this
                                .tabs
                                .entries()
                                .iter()
                                .map(|tab| tab.key.clone())
                                .collect();
                            this.shell.tab_cycle_index = 0;
                        }
                        let index =
                            cycle_index(this.shell.tab_cycle_index, order.len(), modifiers.shift);
                        this.activate_tab(order[index].clone(), cx);
                        this.shell.tab_cycle = order;
                        this.shell.tab_cycle_index = index;
                        cx.stop_propagation();
                    }
                }
            }))
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .bg(rgb(p.paper))
            .text_color(rgb(p.ink))
            .font_family(".SystemUIFont")
            .child(tab)
            .when(self.editing_ui.enabled(), |root| {
                root.child(
                    div()
                        .pl(px(64.))
                        .py_1()
                        .text_xs()
                        .text_color(rgb(p.muted))
                        .child(self.editor_fixture_notice()),
                )
            })
            .when_some(self.creation_journal_notice(), |root, notice| {
                root.child(div().pl(px(64.)).py_1().text_xs().child(notice))
            })
            .when_some(link_warning, |root, warning| {
                root.child(
                    div()
                        .pl(px(64.))
                        .py_1()
                        .text_size(px(11.))
                        .text_color(rgb(p.mark))
                        .child(warning),
                )
            })
            .when_some(self.event_awareness.notice(), |root, notice| {
                root.child(
                    div()
                        .id("live-change-notice")
                        .pl(px(64.))
                        .py_1()
                        .flex()
                        .flex_wrap()
                        .gap_3()
                        .text_size(px(11.))
                        .text_color(rgb(p.mark))
                        .child(notice.message)
                        .child(
                            div()
                                .id("live-change-reload")
                                .cursor_pointer()
                                .child("Reload")
                                .on_click(cx.listener(|this, _, _, cx| this.load_notes(cx))),
                        )
                        .when_some(notice.dismiss, |row, ticket| {
                            row.child(
                                div()
                                    .id("live-change-dismiss")
                                    .cursor_pointer()
                                    .child("Dismiss notice")
                                    .on_click(cx.listener(move |this, _, _, cx| {
                                        if this.event_awareness.dismiss(ticket) {
                                            cx.notify();
                                        }
                                    })),
                            )
                        }),
                )
            })
            .when_some(self.shell.navigation_notice.clone(), |root, notice| {
                root.child(
                    div()
                        .id("navigation-notice")
                        .pl(px(64.))
                        .py_1()
                        .text_size(px(11.))
                        .text_color(rgb(p.muted))
                        .cursor_pointer()
                        .child(format!("{notice}  ×"))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.navigation_notice = None;
                            cx.notify();
                        })),
                )
            })
            .when(
                self.partial_read
                    || self
                        .snapshot
                        .read_state
                        .as_ref()
                        .is_some_and(|state| state.partial),
                |root| {
                    root.child(
                        div()
                            .pl(px(64.))
                            .py_1()
                            .text_size(px(11.))
                            .text_color(rgb(p.mark))
                            .child("Some notes could not be read. Open diagnostics for details."),
                    )
                },
            )
            .when(self.session_ui.warning.is_some(), |root| {
                root.child(
                    div()
                        .pl(px(64.))
                        .py_1()
                        .text_size(px(11.))
                        .text_color(rgb(p.mark))
                        .child(self.session_ui.warning.clone().unwrap_or_default()),
                )
            })
            .when(self.read_error.is_some(), |root| {
                root.child(
                    div()
                        .pl(px(64.))
                        .py_1()
                        .text_size(px(11.))
                        .text_color(rgb(p.mark))
                        .child(
                            self.read_error
                                .as_ref()
                                .filter(|e| e.starts_with("Shutdown failed:"))
                                .cloned()
                                .unwrap_or_else(|| {
                                    "A read failed. Open diagnostics for details.".into()
                                }),
                        ),
                )
            })
            .when(self.tag_page.is_some(), |root| {
                root.child(self.render_tag_page(p.ink, p.muted, p.line, cx))
            })
            .when(home, |root| root.child(home_view))
            .when(calendar_active, |root| {
                root.child(self.render_calendar(f32::from(window.viewport_size().width), dark, cx))
            })
            .when(full_graph_active, |root| {
                root.child(self.render_full_graph(cx))
            })
            .when(
                !home
                    && !full_graph_active
                    && !calendar_active
                    && self.tag_page.is_none()
                    && !self.editing_ui.active(),
                |root| {
                    root.child(
                        div()
                            .flex()
                            .flex_1()
                            .min_h_0()
                            .child(reader)
                            .when(wide, |row| row.child(aside.take().unwrap())),
                    )
                },
            )
            .when(self.editing_ui.active(), |root| {
                root.child(self.render_editor(dark))
            })
            .child(rail)
            .children(self.render_graph_previews(dark, window.viewport_size(), cx))
            .when(self.recent_open(), |root| {
                root.child(self.render_recent((p.paper, p.ink, p.muted, p.line), cx))
            })
            .when(self.shell.find.open, |root| {
                root.child(self.find_overlay(dark, window.viewport_size().width, cx))
            })
            .when(self.shell.vault_menu, |root| {
                root.child(
                    div()
                        .id("vault-menu-backdrop")
                        .absolute()
                        .size_full()
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.vault_menu = false;
                            cx.notify();
                        })),
                )
                .child(vault_menu)
            })
            .when(self.shell.tab_menu, |root| {
                root.child(
                    div()
                        .id("tab-menu-backdrop")
                        .absolute()
                        .size_full()
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.tab_menu = false;
                            cx.notify();
                        })),
                )
                .child(tab_menu)
            })
            .when(self.shell.diagnostics, |root| {
                root.child(
                    div()
                        .id("diagnostics-drawer")
                        .absolute()
                        .right(px(16.))
                        .top(px(48.))
                        .w(px(420.))
                        .max_w_full()
                        .h(px(240.))
                        .overflow_y_scroll()
                        .p_4()
                        .bg(rgb(p.paper))
                        .border_1()
                        .border_color(rgb(p.line))
                        .rounded_lg()
                        .text_size(px(11.))
                        .text_color(rgb(p.muted))
                        .child(format!(
                            "{}\n{}\n{}\n{}\n{}",
                            self.source,
                            self.list_status,
                            self.status,
                            self.read_error.as_deref().unwrap_or(""),
                            self.shell
                                .links
                                .as_ref()
                                .map(|cache| cache.warnings.join("\n"))
                                .unwrap_or_default()
                        ))
                        .child(crate::build_info::display())
                        // Design: text control, explicit metadata-only clipboard submission.
                        .child(
                            div()
                                .id("copy-build-info")
                                .cursor_pointer()
                                .text_color(rgb(p.muted))
                                .hover(|style| style.text_color(rgb(p.ink)))
                                .child("Copy Build Info")
                                .on_click(|_, _, cx| {
                                    cx.write_to_clipboard(gpui::ClipboardItem::new_string(
                                        crate::build_info::clipboard_payload(),
                                    ));
                                    cx.stop_propagation();
                                }),
                        )
                        .child(self.render_edit_diagnostics(cx)),
                )
            })
            .when(self.shell.search_open, |root| {
                root.child(
                    div()
                        .id("search-backdrop")
                        .absolute()
                        .size_full()
                        .bg(gpui::rgba(0x00000047))
                        .on_click(cx.listener(|this, _, window, cx| {
                            this.dismiss_search(window, cx);
                        })),
                )
                .child(search_panel)
            })
            .when(self.editing_ui.enabled(), |root| {
                root.child(self.render_editor_mode_menu(dark, height, cx))
            })
            .when(self.editing_ui.has_pending(), |root| {
                root.child(self.render_leave_prompt(dark, cx))
            })
            .when(self.creation_open(), |root| {
                root.child(self.render_creation(dark, cx))
            })
            .when(self.enlarged_image_open(), |root| {
                root.child(self.render_enlarged_image(window, cx))
            })
            .when(self.reader_hierarchy_open(), |root| {
                root.child(self.render_reader_hierarchy(dark, window, cx))
            })
    }
}

#[cfg(test)]
mod reference_tests {
    use super::{LinkSource, code_copy_available, reference_key};

    #[test]
    fn copy_controls_remain_available_with_graph_in_reader_aside() {
        for aside_graph in [false, true] {
            assert!(code_copy_available(true, false, false, aside_graph));
            assert!(!code_copy_available(false, false, false, aside_graph));
            assert!(!code_copy_available(true, true, false, aside_graph));
            assert!(!code_copy_available(true, false, true, aside_graph));
        }
    }
    #[test]
    fn external_click_source_changes_on_navigation_session_or_storage() {
        let source = LinkSource {
            note: crate::live::NoteKey {
                vault: "work".into(),
                note_id: 42,
            },
            etag: "a".into(),
            storage_generation: 1,
            launch_generation: 2,
            request: 3,
        };
        let mut changed = vec![source.clone(); 6];
        changed[0].note.vault = "research".into();
        changed[1].note.note_id = 43;
        changed[2].etag = "b".into();
        changed[3].storage_generation += 1;
        changed[4].launch_generation += 1;
        changed[5].request += 1;
        assert_eq!(Some(&source), Some(&source.clone()));
        let displayed = crate::markdown::parse("[link](https://example.com)");
        for stale in changed {
            assert_ne!(Some(&source), Some(&stale));
            assert!(!crate::external_links::dispatch(
                &source,
                Some(&stale),
                &displayed,
                &displayed,
                &displayed[0].links[0],
                |_| panic!("stale provenance must not open")
            ));
        }
        assert_ne!(Some(&source), None);
    }

    use serde_json::json;

    #[test]
    fn missing_vault_inherits_source_not_search_scope() {
        let key = reference_key(&json!({"note_id":42}), "source-vault").unwrap();
        assert_eq!(key.vault, "source-vault");
        assert_eq!(key.note_id, 42);
    }
    #[test]
    fn explicit_vault_preserves_same_id_distinction() {
        let a = reference_key(&json!({"note_id":42,"vault":"one"}), "source").unwrap();
        let b = reference_key(&json!({"note_id":42,"vault":"two"}), "source").unwrap();
        assert_ne!(a, b);
        assert_eq!(a.note_id, b.note_id);
    }
    #[test]
    fn explicit_empty_vault_means_launch_vault() {
        assert_eq!(
            reference_key(&json!({"note_id":42,"vault":""}), "source")
                .unwrap()
                .vault,
            ""
        );
    }
    #[test]
    fn malformed_and_nonpositive_references_are_inert() {
        for value in [
            json!({}),
            json!({"note_id":0}),
            json!({"note_id":-1}),
            json!({"note_id":"42"}),
            json!({"note_id":42.5}),
            json!({"note_id":42,"vault":null}),
            json!({"note_id":42,"vault":7}),
            json!({"note_id":18446744073709551615u64}),
        ] {
            assert!(reference_key(&value, "source").is_none(), "{value}");
        }
    }
}

#[cfg(test)]
mod reader_navigation_tests {
    use super::*;
    #[test]
    fn active_tab_focus_click_does_not_enter_dirty_navigation_gate() {
        let mut tabs = tabs::Tabs::default();
        let other_vault = tabs.open_note("other", 42, "Other vault");
        let current = tabs.open_note("work", 42, "Current dirty draft");
        assert!(!tab_activation_needed(&tabs, &current, false));
        assert!(tab_activation_needed(&tabs, &other_vault, false));
        assert!(!tab_activation_needed(&tabs, &tabs::TabKey::Home, false));
        assert_eq!(tabs.active().unwrap().key, current);
        tabs.open_home();
        assert!(!tab_activation_needed(&tabs, &tabs::TabKey::Home, false));
    }
    #[test]
    fn closing_active_tab_still_loads_its_newly_promoted_neighbor() {
        let mut tabs = tabs::Tabs::default();
        let neighbor = tabs.open_note("work", 1, "Neighbor");
        let current = tabs.open_note("work", 2, "Closing");
        assert!(tabs.close(&current));
        assert_eq!(tabs.active().unwrap().key, neighbor);
        assert!(!tab_activation_needed(&tabs, &neighbor, false));
        assert!(tab_activation_needed(&tabs, &neighbor, true));
        assert!(!tab_activation_needed(&tabs, &current, true));
    }
    #[test]
    fn tab_cycle_wraps_both_directions() {
        assert_eq!(cycle_index(0, 3, false), 1);
        assert_eq!(cycle_index(2, 3, false), 0);
        assert_eq!(cycle_index(0, 3, true), 2);
        assert_eq!(cycle_index(0, 0, true), 0);
    }
    #[test]
    fn preview_detaches_queued_anchor_handle_preserving_host_offset() {
        let mut shell = ShellState::default();
        let key = tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 42,
        };
        let other = tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 43,
        };
        shell.accept_reader_content(key.clone(), "# Host", None);
        shell.accept_reader_content(other.clone(), "Other", None);
        let queued_handle = shell.reader_scrolls[&key].clone();
        queued_handle.set_offset(point(px(0.), px(-200.)));
        let blocks = markdown::parse("# Host");
        shell.heading_anchors.insert(
            key.clone(),
            (
                "outline".into(),
                std::rc::Rc::new(markdown::HeadingAnchors::new(&blocks, &queued_handle)),
            ),
        );
        shell.code_anchors = Some(std::rc::Rc::new(markdown::FindAnchors::new(
            &blocks,
            &queued_handle,
        )));
        shell.find.active = Some(2);
        shell.find.highlighted = true;
        shell.detach_reader_scroll(&key);
        // Model the inner GPUI next-frame callback after the preview has opened.
        queued_handle.set_offset(point(px(0.), px(-900.)));
        assert_eq!(shell.reader_scrolls[&key].offset().y, px(-200.));
        assert_eq!(shell.reader_scrolls[&other].offset().y, px(0.));
        assert!(!shell.heading_anchors.contains_key(&key));
        assert!(shell.code_anchors.is_none());
        assert!(shell.positions.anchors.is_none());
        assert!(shell.find.anchors.is_some());
        assert_eq!(shell.find.active, Some(2));
        assert!(shell.find.highlighted);
        // Returning to the exact same accepted body retains the fresh handle offset.
        shell.accept_reader_content(key.clone(), "# Host", None);
        assert_eq!(shell.reader_scrolls[&key].offset().y, px(-200.));
    }
    #[test]
    fn accepted_reader_content_preserves_revisits_but_resets_changed_identity() {
        let mut shell = ShellState::default();
        let key = tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 42,
        };
        let live = Some((1, Some(7), "etag".into()));
        shell.accept_reader_content(key.clone(), "日本語 body", live.clone());
        let handle = shell.reader_scrolls[&key].clone();
        handle.set_offset(point(px(0.), px(-200.)));
        // Request IDs are intentionally absent: identical reload/tab/history reads
        // retain positions. Loading/error paths do not call this method at all.
        shell.accept_reader_content(key.clone(), "日本語 body", live.clone());
        assert_eq!(handle.offset().y, px(-200.));
        for (body, revision) in [
            ("changed body", live.clone()),
            ("changed body", Some((1, Some(7), "new-etag".into()))),
            ("changed body", Some((1, Some(8), "new-etag".into()))),
            ("changed body", Some((2, Some(8), "new-etag".into()))),
            ("changed body", None),
        ] {
            shell.accept_reader_content(key.clone(), body, revision);
            // Previously cloned anchor handle observes the reset too.
            assert_eq!(handle.offset().y, px(0.));
            handle.set_offset(point(px(0.), px(-50.)));
        }
        shell.accept_reader_content(key.clone(), "changed body", None);
        assert_eq!(handle.offset().y, px(-50.));
        shell.accept_reader_content(key.clone(), "portable changed", None);
        assert_eq!(handle.offset().y, px(0.));
        // An explicit fragment/Find scroll applied after acceptance wins, including
        // subsequent notifications for exactly the same content.
        handle.set_offset(point(px(0.), px(-75.)));
        shell.accept_reader_content(key.clone(), "portable changed", None);
        assert_eq!(handle.offset().y, px(-75.));
    }

    #[test]
    fn reader_identity_is_vault_scoped_and_close_reopen_has_no_old_offset() {
        let mut shell = ShellState::default();
        let a = tabs::TabKey::Note {
            vault: "a".into(),
            note_id: 42,
        };
        let b = tabs::TabKey::Note {
            vault: "b".into(),
            note_id: 42,
        };
        for key in [&a, &b] {
            shell.accept_reader_content(key.clone(), "same", None);
            shell.reader_scrolls[key].set_offset(point(px(0.), px(-90.)));
        }
        shell.accept_reader_content(a.clone(), "new", None);
        assert_eq!(shell.reader_scrolls[&a].offset().y, px(0.));
        assert_eq!(shell.reader_scrolls[&b].offset().y, px(-90.));
        shell.reader_scrolls.remove(&b);
        shell.reader_content.remove(&b);
        shell.accept_reader_content(b.clone(), "same", None);
        assert_eq!(shell.reader_scrolls[&b].offset().y, px(0.));
    }

    #[test]
    fn reader_offsets_are_isolated_by_vault_and_survive_unbound_loading() {
        let mut shell = ShellState::default();
        let a = tabs::TabKey::Note {
            vault: "a".into(),
            note_id: 42,
        };
        let b = tabs::TabKey::Note {
            vault: "b".into(),
            note_id: 42,
        };
        shell
            .reader_scrolls
            .entry(a.clone())
            .or_default()
            .set_offset(point(px(0.), px(-200.)));
        shell
            .reader_scrolls
            .entry(b.clone())
            .or_default()
            .set_offset(point(px(0.), px(-80.)));
        assert_eq!(shell.reader_scrolls[&a].offset().y, px(-200.));
        assert_eq!(shell.reader_scrolls[&b].offset().y, px(-80.));
        shell.reader_scrolls.remove(&a);
        assert_eq!(shell.reader_scrolls[&b].offset().y, px(-80.));
    }
}

#[cfg(test)]
mod vault_scope_tests {
    use super::*;
    use serde_json::json;
    fn fixture() -> serde_json::Value {
        json!({"active":{"name":"main"},"vaults":[{"name":"main"},{"name":"work"}],"unavailable":[{"name":"archive","error":"unmounted"}]})
    }
    #[test]
    fn startup_completion_cannot_overwrite_later_scope_selection_on_first_render() {
        let mut shell = ShellState::default();
        shell.working_scope = "saved".into();
        shell.restored_scope_launch = Some(7);
        shell.set_working_scope("newer".into(), Some(7));
        shell.initialize_working_scope(7);
        assert_eq!(shell.working_scope, "newer");
        assert!(shell.restored_scope_launch.is_none());
        // A later ordinary generation does not reuse the restored scope.
        shell.initialize_working_scope(8);
        assert_eq!(shell.working_scope, "");
        shell.set_working_scope("during-startup".into(), None);
        shell.restored_scope_launch = Some(9); // accepted newer startup choice
        shell.initialize_working_scope(9);
        assert_eq!(shell.working_scope, "during-startup");
        shell.restored_scope_launch = Some(9);
        shell.initialize_working_scope(10);
        assert_eq!(shell.working_scope, "");
    }

    #[test]
    fn launch_selection_is_empty_and_unavailable_is_inert() {
        let d = VaultDiscovery::parse(&fixture()).unwrap();
        assert_eq!(d.scope_for("main"), Some("".into()));
        assert_eq!(d.scope_for("work"), Some("work".into()));
        assert_eq!(d.scope_for("archive"), None);
        assert_eq!(d.unavailable[0].error.as_deref(), Some("unmounted"));
    }
    #[test]
    fn local_single_is_hidden_named_single_is_visible() {
        let local =
            VaultDiscovery::parse(&json!({"active":{"name":""},"vaults":[{"name":""}]})).unwrap();
        assert!(!local.visible());
        assert_eq!(local.label(""), "local");
        let named =
            VaultDiscovery::parse(&json!({"active":{"name":"main"},"vaults":[{"name":"main"}]}))
                .unwrap();
        assert!(named.visible());
    }
    #[test]
    fn incomplete_or_malformed_is_not_empty_success() {
        for value in [
            json!({"active":{"name":"main"},"vaults":[],"storage_state":"initializing"}),
            json!({"active":{"name":"main"},"vaults":[]}),
            json!({"active":{},"vaults":[{"name":"x"}]}),
            json!({"active":{"name":"main"},"vaults":[{"name":"main"},{"name":"main"}]}),
        ] {
            assert!(VaultDiscovery::parse(&value).is_err());
        }
    }
    #[test]
    fn authoritative_list_reconciles_only_working_scope() {
        let d = VaultDiscovery::parse(&fixture()).unwrap();
        assert_eq!(d.reconcile_scope("missing"), "");
        assert_eq!(d.reconcile_scope("main"), "");
        assert_eq!(d.reconcile_scope("work"), "work");
        let key = tabs::TabKey::Note {
            vault: "archive".into(),
            note_id: 42,
        };
        let mut tabs = tabs::Tabs::default();
        tabs.open_note("archive", 42, "Old tab");
        let _ = d.reconcile_scope("archive");
        assert_eq!(tabs.active().unwrap().key, key);
    }
}

#[cfg(test)]
mod internal_link_tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn cached_title_snapshot_reuses_allocation_and_rejects_stale_sources() {
        let source = LinkSource {
            note: live::NoteKey {
                vault: "a".into(),
                note_id: 1,
            },
            etag: "v1".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        let raw = markdown::parse("[[id:123]] tail");
        let mut cache = LinkResolutionCache {
            source: source.clone(),
            properties: Default::default(),
            pending: true,
            deferred_targets: 0,
            canceled: Default::default(),
            resolved: Default::default(),
            displayed: std::rc::Rc::new(raw.clone()),
            warnings: vec![],
        };
        let pending = cache.displayed_for(Some(&source)).unwrap();
        assert_eq!(*pending, raw);
        assert!(std::rc::Rc::ptr_eq(
            &pending,
            &cache.displayed_for(Some(&source)).unwrap()
        ));
        cache.update_display(
            &raw,
            &[("id:123".into(), "First title".into())]
                .into_iter()
                .collect(),
        );
        let first = cache.displayed_for(Some(&source)).unwrap();
        assert!(!std::rc::Rc::ptr_eq(&pending, &first));
        assert_eq!(first[0].text, "First title tail");
        cache.update_display(
            &raw,
            &[("id:123".into(), "新しいタイトル".into())]
                .into_iter()
                .collect(),
        );
        let next = cache.displayed_for(Some(&source)).unwrap();
        assert!(!std::rc::Rc::ptr_eq(&first, &next));
        assert_eq!(next[0].text, "新しいタイトル tail");
        assert_eq!(pending[0].text, "id:123 tail");
        assert_eq!(first[0].text, "First title tail");
        assert_eq!(next[0].links[0].target, raw[0].links[0].target);
        assert!(cache.displayed_for(None).is_none());
        for field in 0..6 {
            let mut stale = source.clone();
            match field {
                0 => stale.note.vault = "b".into(),
                1 => stale.note.note_id += 1,
                2 => stale.etag = "v2".into(),
                3 => stale.storage_generation += 1,
                4 => stale.launch_generation += 1,
                _ => stale.request += 1,
            }
            assert!(cache.displayed_for(Some(&stale)).is_none());
        }
    }
    #[test]
    fn terms_deduplicate_and_exclude_external_and_includes() {
        let blocks =
            markdown::parse("[[One]] [[One#part]] [web](https://example.com) ![[include]]");
        assert_eq!(internal_terms(&blocks), vec!["One"]);
    }
    #[test]
    fn title_projection_does_not_add_resolution_terms_or_change_fragments() {
        let raw = markdown::parse("[[id:123]] [[id:123#Part]] [[id:123|Alias]] [[work:id:123]]");
        let titles = [
            ("id:123".into(), "[[injected:id:9]]".into()),
            ("work:id:123".into(), "Other vault".into()),
        ]
        .into_iter()
        .collect();
        let displayed = markdown::with_resolved_titles(&raw, &titles);
        assert_eq!(internal_terms(&raw), vec!["id:123", "work:id:123"]);
        assert_eq!(internal_terms(&displayed), internal_terms(&raw));
        for (before, after) in raw[0].links.iter().zip(&displayed[0].links) {
            assert_eq!(before.target, after.target);
        }
    }
    #[test]
    fn partial_and_cached_targets_are_unverified() {
        for value in [
            json!({"read_state":{"partial":true}}),
            json!({"read_state":{"cache_used":true}}),
            json!({"partial":[{"vault":"other"}]}),
            json!({"unavailable":[{"name":"offline"}]}),
            json!({"storage_state":"initializing"}),
        ] {
            assert!(uncertain_resolution(
                &serde_json::from_value(value).unwrap()
            ));
        }
        assert!(!uncertain_resolution(&Default::default()));
    }
    #[test]
    fn cache_identity_includes_vault_storage_and_navigation() {
        let source = LinkSource {
            note: live::NoteKey {
                vault: "a".into(),
                note_id: 42,
            },
            etag: "v1".into(),
            storage_generation: 9,
            launch_generation: 1,
            request: 3,
        };
        let mut other = source.clone();
        other.note.vault = "b".into();
        assert_ne!(source, other);
        other = source.clone();
        other.storage_generation = 10;
        assert_ne!(source, other);
        other = source.clone();
        other.request = 4;
        assert_ne!(source, other);
        other = source.clone();
        other.etag = "v2".into();
        assert_ne!(source, other);
        other = source.clone();
        other.launch_generation = 2;
        assert_ne!(source, other);
        other = source.clone();
        other.note.note_id = 43;
        assert_ne!(source, other);
    }
    #[test]
    fn fragment_navigation_is_explicitly_identified() {
        assert!(has_authored_fragment(&markdown::LinkTarget::Wiki {
            key: "One".into(),
            fragment: Some("part".into()),
            authored_label: false
        }));
        assert!(has_authored_fragment(&markdown::LinkTarget::Markdown {
            destination: "One.md#part".into()
        }));
        assert!(!has_authored_fragment(&markdown::LinkTarget::Markdown {
            destination: "One.md".into()
        }));
    }
}

#[cfg(test)]
mod fragment_navigation_tests {
    use super::*;
    fn intent() -> InternalLinkIntent {
        InternalLinkIntent {
            source: live::NoteKey {
                vault: "source".into(),
                note_id: 1,
            },
            destination: live::NoteKey {
                vault: "dest".into(),
                note_id: 2,
            },
            target: markdown::LinkTarget::Wiki {
                key: "Target".into(),
                fragment: Some("Heading".into()),
                authored_label: false,
            },
            launch_generation: 7,
            storage_generation: 9,
            allow_epoch_advance: false,
        }
    }
    #[test]
    fn accepted_fragment_requires_exact_destination_launch_and_request() {
        let i = intent();
        assert!(fragment_accepts(
            &i,
            10,
            10,
            Some(7),
            Some(9),
            &i.destination
        ));
        assert!(!fragment_accepts(
            &i,
            10,
            11,
            Some(7),
            Some(9),
            &i.destination
        ));
        assert!(!fragment_accepts(
            &i,
            10,
            10,
            Some(8),
            Some(9),
            &i.destination
        ));
        assert!(!fragment_accepts(
            &i,
            10,
            10,
            Some(7),
            Some(9),
            &live::NoteKey {
                vault: "other".into(),
                note_id: 2
            }
        ));
    }
    #[test]
    fn only_acknowledged_save_allows_destination_epoch_change() {
        let mut i = intent();
        assert!(!fragment_accepts(
            &i,
            10,
            10,
            Some(7),
            Some(10),
            &i.destination
        ));
        i.allow_epoch_advance = true;
        assert!(fragment_accepts(
            &i,
            10,
            10,
            Some(7),
            Some(2),
            &i.destination
        ));
        assert!(!fragment_accepts(
            &i,
            10,
            10,
            Some(8),
            Some(2),
            &i.destination
        ));
        assert!(!fragment_accepts(&i, 10, 10, Some(7), None, &i.destination));
    }
}

/// Strict named registry membership, including peers not opened yet.
/// Navigation continues to use discovery.available, never this union.
pub(crate) fn session_registry(value: &serde_json::Value) -> Result<Vec<String>, String> {
    VaultDiscovery::parse(value)?.session_membership()
}

#[cfg(test)]
mod session_membership_tests {
    use super::*;
    #[test]
    fn peer_readiness_does_not_change_membership_or_navigation_availability() {
        let pending = serde_json::json!({"active":{"name":"host"},
            "vaults":[{"name":"host","path":"/host"}],
            "unavailable":[{"name":"foreign","path":"/foreign","error":"pending or unavailable"}]});
        let ready = serde_json::json!({"active":{"name":"host"},
            "vaults":[{"name":"host","path":"/host"},{"name":"foreign","path":"/foreign"}],"unavailable":[]});
        let d = VaultDiscovery::parse(&pending).unwrap();
        assert_eq!(d.available.len(), 1);
        assert_eq!(d.unavailable.len(), 1);
        assert_eq!(d.session_membership().unwrap(), vec!["", "foreign", "host"]);
        assert_eq!(
            session_registry(&pending).unwrap(),
            session_registry(&ready).unwrap()
        );
        for invalid in [
            serde_json::json!({"active":{"name":"host"},"vaults":[{"name":"host"}],"unavailable":[{"name":"","error":"pending"}]}),
            serde_json::json!({"active":{"name":"host"},"vaults":[{"name":"host"}],"unavailable":[{"error":"initializing"}],"storage_state":"initializing"}),
            serde_json::json!({"active":{"name":"host"},"vaults":[{"name":"host"}],"unavailable":[{"error":"pending"}]}),
            serde_json::json!({"active":{"name":"host"},"vaults":[{"name":"host"}],"unavailable":[{"name":"host"}]}),
        ] {
            assert!(session_registry(&invalid).is_err());
        }
    }
}

#[cfg(test)]
mod footnote_jump_tests {
    use super::*;
    #[test]
    fn footnote_jump_rechecks_source_content_membership_and_interaction_gate() {
        let source = LinkSource {
            note: live::NoteKey {
                vault: "work".into(),
                note_id: 42,
            },
            etag: "a".into(),
            storage_generation: 1,
            launch_generation: 2,
            request: 3,
        };
        let blocks = markdown::parse("Ref[^x].\n\n[^x]: body");
        let link = &blocks[0].links[0];
        assert!(footnote_jump_allowed(
            &source,
            Some(&source),
            &blocks,
            &blocks,
            link,
            true
        ));
        assert!(!footnote_jump_allowed(
            &source,
            Some(&source),
            &blocks,
            &blocks,
            link,
            false
        ));
        assert!(!footnote_jump_allowed(
            &source, None, &blocks, &blocks, link, true
        ));
        for index in 0..6 {
            let mut changed = source.clone();
            match index {
                0 => changed.note.vault = "other".into(),
                1 => changed.note.note_id = 43,
                2 => changed.etag = "b".into(),
                3 => changed.storage_generation += 1,
                4 => changed.launch_generation += 1,
                _ => changed.request += 1,
            };
            assert!(!footnote_jump_allowed(
                &source,
                Some(&changed),
                &blocks,
                &blocks,
                link,
                true
            ));
        }
        let changed = markdown::parse("Ref[^x].\n\n[^x]: changed");
        assert!(!footnote_jump_allowed(
            &source,
            Some(&source),
            &blocks,
            &changed,
            link,
            true
        ));
        let mut forged = link.clone();
        forged.range = 999..1000;
        assert!(!footnote_jump_allowed(
            &source,
            Some(&source),
            &blocks,
            &blocks,
            &forged,
            true
        ));
    }
}

#[cfg(test)]
mod reading_position_tests {
    use super::*;
    fn record() -> session_store::ReadingPosition {
        session_store::ReadingPosition {
            vault: "work".into(),
            note_id: 7,
            etag: "revision".into(),
            body_bytes: 100,
            renderer: "native-block-v1:clean-build".into(),
            block_index: 2,
            geometry: session_store::PositionGeometry {
                viewport_width: 1000.,
                viewport_height: 800.,
                reader_width: 600.,
                reader_height: 700.,
                scale: 2.,
            },
        }
    }
    fn source() -> LinkSource {
        LinkSource {
            note: live::NoteKey {
                vault: "work".into(),
                note_id: 7,
            },
            etag: "revision".into(),
            storage_generation: 3,
            launch_generation: 4,
            request: 5,
        }
    }
    #[test]
    fn position_compatibility_requires_exact_revision_build_route_and_geometry() {
        let saved = record();
        assert!(position_compatible(&saved, &saved, 3));
        assert!(!position_compatible(&saved, &saved, 2));
        assert!(!position_compatible(&saved, &saved, 4097));
        for field in 0..10 {
            let mut changed = saved.clone();
            match field {
                0 => changed.vault = "foreign".into(),
                1 => changed.note_id += 1,
                2 => changed.etag.push('x'),
                3 => changed.body_bytes += 1,
                4 => changed.renderer.push('x'),
                5 => changed.geometry.viewport_width += 0.01,
                6 => changed.geometry.viewport_height += 0.01,
                7 => changed.geometry.reader_width += 0.01,
                8 => changed.geometry.reader_height += 0.01,
                _ => changed.geometry.scale += 0.01,
            }
            assert!(!position_compatible(&saved, &changed, 3));
        }
    }
    #[test]
    fn position_callbacks_cannot_resurrect_after_navigation_or_interaction() {
        let expected = (
            source(),
            tabs::TabKey::Note {
                vault: "work".into(),
                note_id: 7,
            },
        );
        assert!(position_callback_current(&expected, Some(&expected), 1, 1));
        assert!(!position_callback_current(&expected, None, 1, 1));
        // Even scroll-away-then-back or a jump to the same offset cancels.
        assert!(!position_callback_current(&expected, Some(&expected), 1, 2));
        for field in 0..6 {
            let mut changed = expected.clone();
            match field {
                0 => changed.0.request += 1,
                1 => changed.0.launch_generation += 1,
                2 => changed.0.storage_generation += 1,
                3 => changed.0.etag.push('x'),
                4 => changed.0.note.vault = "foreign".into(),
                _ => changed.1 = tabs::TabKey::Home,
            }
            assert!(!position_callback_current(&expected, Some(&changed), 1, 1));
        }
    }
    #[test]
    fn position_route_uses_accepted_launch_alias_not_equal_foreign_id() {
        let source = source();
        let route = live::NoteKey {
            vault: String::new(),
            note_id: 7,
        };
        let key = tabs::TabKey::Note {
            vault: String::new(),
            note_id: 7,
        };
        assert!(position_route_matches(&source, &route, &key));
        let foreign = live::NoteKey {
            vault: "foreign".into(),
            note_id: 7,
        };
        assert!(!position_route_matches(&source, &foreign, &key));
        assert!(!position_route_matches(
            &source,
            &route,
            &tabs::TabKey::Note {
                vault: "foreign".into(),
                note_id: 7
            }
        ));
    }
}

#[cfg(test)]
mod footnote_return_tests {
    use super::*;
    fn ticket(y: f32) -> FootnoteReturn {
        let displayed = std::rc::Rc::new(markdown::parse(
            "First[^x].\n\nSecond[^x].\n\n| Ref |\n| --- |\n| table[^x] |\n\n[^x]: definition",
        ));
        FootnoteReturn {
            source: LinkSource {
                note: live::NoteKey {
                    vault: "work".into(),
                    note_id: 7,
                },
                etag: "a".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            tab: tabs::TabKey::Note {
                vault: "work".into(),
                note_id: 7,
            },
            anchors: std::rc::Rc::new(markdown::HeadingAnchors::new(
                &displayed,
                &gpui::ScrollHandle::new(),
            )),
            displayed,
            interaction: 5,
            geometry: FootnoteGeometry {
                reader: gpui::Bounds::new(point(px(60.), px(40.)), gpui::size(px(800.), px(600.))),
                extent: gpui::size(px(0.), px(4000.)),
                viewport: gpui::size(px(1000.), px(800.)),
                scale: 2.,
            },
            origin: point(px(0.), px(y)),
        }
    }
    fn matches(ticket: &FootnoteReturn, source: &LinkSource, geometry: &FootnoteGeometry) -> bool {
        ticket.matches(
            Some(source),
            Some(&ticket.tab),
            &ticket.displayed,
            Some(&ticket.anchors),
            Some(geometry),
            true,
        )
    }
    #[test]
    fn return_rejects_revisions_owner_geometry_and_detached_reader() {
        let ticket = ticket(-100.);
        assert!(matches(&ticket, &ticket.source, &ticket.geometry));
        for field in 0..6 {
            let mut source = ticket.source.clone();
            match field {
                0 => source.note.vault = "other".into(),
                1 => source.note.note_id += 1,
                2 => source.etag.push('b'),
                3 => source.storage_generation += 1,
                4 => source.launch_generation += 1,
                _ => source.request += 1,
            }
            assert!(!matches(&ticket, &source, &ticket.geometry));
        }
        let mut geometry = ticket.geometry.clone();
        geometry.scale = 1.;
        assert!(!matches(&ticket, &ticket.source, &geometry));
        let detached = std::rc::Rc::new(markdown::HeadingAnchors::new(
            &ticket.displayed,
            &gpui::ScrollHandle::new(),
        ));
        assert!(!ticket.matches(
            Some(&ticket.source),
            Some(&ticket.tab),
            &ticket.displayed,
            Some(&detached),
            Some(&ticket.geometry),
            true
        ));
        assert!(!ticket.matches(
            Some(&ticket.source),
            Some(&tabs::TabKey::Home),
            &ticket.displayed,
            Some(&ticket.anchors),
            Some(&ticket.geometry),
            true
        ));
        assert!(!ticket.matches(
            Some(&ticket.source),
            Some(&ticket.tab),
            &markdown::parse("changed display"),
            Some(&ticket.anchors),
            Some(&ticket.geometry),
            true
        ));
        assert!(!ticket.matches(
            Some(&ticket.source),
            Some(&ticket.tab),
            &ticket.displayed,
            Some(&ticket.anchors),
            Some(&ticket.geometry),
            false
        ));
    }
    #[test]
    fn repeated_and_table_references_keep_clicked_origin_not_first_reference() {
        for y in [-20., -700., -1600.] {
            let ticket = ticket(y);
            assert_eq!(
                ticket.geometry.bounded_origin(ticket.origin),
                Some(point(px(0.), px(y)))
            );
        }
        let ticket = ticket(-1600.);
        let links: Vec<_> = ticket
            .displayed
            .iter()
            .flat_map(|b| {
                b.links.iter().chain(
                    b.table
                        .iter()
                        .flat_map(|t| t.rows.iter().flatten().flat_map(|c| c.links.iter())),
                )
            })
            .collect();
        assert_eq!(
            links
                .iter()
                .filter(|link| matches!(link.target, markdown::LinkTarget::Footnote { .. }))
                .count(),
            3
        );
        for link in links {
            if matches!(link.target, markdown::LinkTarget::Footnote { .. }) {
                assert!(markdown::footnote_link_valid(&ticket.displayed, link));
            }
        }
    }
    #[test]
    fn stale_return_control_cannot_consume_new_ticket_and_return_is_one_shot() {
        let old = std::rc::Rc::new(ticket(-10.));
        let new = std::rc::Rc::new(ticket(-700.));
        let mut shell = ShellState::default();
        shell.footnote_return = Some(new.clone());
        // Root mouse-down / wheel / key handlers increment the pending-action
        // counter. Retained Return survives ordinary reading and its own click.
        shell.positions.interaction = new.interaction.wrapping_add(3);
        assert!(matches(&new, &new.source, &new.geometry));
        assert!(!shell.take_footnote_return(&old));
        assert!(shell.take_footnote_return(&new));
        assert!(!shell.take_footnote_return(&new));
    }
    #[test]
    fn return_offset_is_finite_bounded_and_detachment_discards_ticket() {
        let ticket = std::rc::Rc::new(ticket(-100.));
        assert_eq!(
            ticket.geometry.bounded_origin(point(px(20.), px(-5000.))),
            Some(point(px(0.), px(-4000.)))
        );
        assert!(
            ticket
                .geometry
                .bounded_origin(point(px(0.), px(f32::NAN)))
                .is_none()
        );
        let mut invalid = ticket.geometry.clone();
        invalid.extent.height = px(f32::INFINITY);
        assert!(invalid.bounded_origin(ticket.origin).is_none());
        let mut shell = ShellState::default();
        shell.footnote_return = Some(ticket.clone());
        shell.detach_reader_scroll(&ticket.tab);
        assert!(shell.footnote_return.is_none()); // revisiting the note cannot revive it
    }
}

#[cfg(test)]
mod typed_property_link_tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn metadata_only_targets_share_bounded_body_resolution_without_duplicates() {
        let properties = crate::note_properties::project(&json!([
            {"key":"related","value":"OnlyMetadata#Detail","type":"link","line":1},
            {"key":"related","value":"OnlyMetadata","type":"link","line":2},
            {"key":"plain","value":"NotATarget","type":"string","line":3},
            {"key":"up","value":"HiddenParent","type":"link","line":4}
        ]));
        assert_eq!(
            property_internal_terms(&markdown::parse("[[Body]]"), &properties),
            ["Body", "OnlyMetadata"]
        );
        assert_eq!(property_internal_terms(&[], &properties), ["OnlyMetadata"]);
    }
    #[test]
    fn property_callbacks_recheck_exact_source_row_and_verification() {
        let source = LinkSource {
            note: live::NoteKey {
                vault: "work".into(),
                note_id: 7,
            },
            etag: "a".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        let props = json!([{"key":"related","value":"file:///tmp/name","type":"link","line":1}]);
        let projection = crate::note_properties::project(&props);
        let property = &projection.rows[0];
        assert!(!property_callback_current(
            &source,
            Some(&source),
            property,
            &props,
            false
        ));
        assert!(property_callback_current(
            &source,
            Some(&source),
            property,
            &props,
            true
        ));
        for field in 0..6 {
            let mut current = source.clone();
            match field {
                0 => current.note.vault = "other".into(),
                1 => current.note.note_id += 1,
                2 => current.etag.push('b'),
                3 => current.storage_generation += 1,
                4 => current.launch_generation += 1,
                _ => current.request += 1,
            }
            assert!(!property_callback_current(
                &source,
                Some(&current),
                property,
                &props,
                true
            ));
        }
        assert!(!property_callback_current(
            &source, None, property, &props, true
        ));
        let mut changed = props.clone();
        changed[0]["value"] = json!("Different");
        assert!(!property_callback_current(
            &source,
            Some(&source),
            property,
            &changed,
            true
        ));
    }
}

#[cfg(test)]
mod reader_key_ownership_tests {
    use super::*;
    #[test]
    fn plain_single_click_excludes_modified_multiclick_and_drag_selection_intent() {
        assert!(plain_click_admitted(true, 1, 1, true, true));
        for args in [
            (false, 1, 1, true, true),
            (true, 2, 2, true, true),
            (true, 1, 2, true, true),
            (true, 1, 1, false, true),
            (true, 1, 1, true, false),
        ] {
            assert!(!plain_click_admitted(
                args.0, args.1, args.2, args.3, args.4
            ));
        }
    }
    #[test]
    fn reader_keys_require_explicit_focus_current_source_and_uncontested_owner() {
        let source = LinkSource {
            note: live::NoteKey {
                vault: "work".into(),
                note_id: 7,
            },
            etag: "v1".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        let tab = tabs::TabKey::Note {
            vault: "work".into(),
            note_id: 7,
        };
        let displayed = std::rc::Rc::new(markdown::parse("Plain paragraph"));
        let owner = ReaderKeyOwner {
            source: source.clone(),
            tab: tab.clone(),
            displayed: displayed.clone(),
            epoch: 9,
            geometry: FootnoteGeometry {
                reader: gpui::Bounds::new(point(px(0.), px(0.)), gpui::size(px(800.), px(600.))),
                extent: gpui::size(px(0.), px(4000.)),
                viewport: gpui::size(px(1000.), px(800.)),
                scale: 2.,
            },
        };
        assert!(owner.matches(Some(&source), Some(&tab), &displayed, 9, true, true));
        for field in 0..6 {
            let mut stale = source.clone();
            match field {
                0 => stale.note.vault = "other".into(),
                1 => stale.note.note_id += 1,
                2 => stale.etag.push('x'),
                3 => stale.storage_generation += 1,
                4 => stale.launch_generation += 1,
                _ => stale.request += 1,
            }
            assert!(!owner.matches(Some(&stale), Some(&tab), &displayed, 9, true, true));
        }
        assert!(!owner.matches(Some(&source), Some(&tab), &displayed, 9, false, true)); // Escape / other focus
        assert!(!owner.matches(Some(&source), Some(&tab), &displayed, 9, true, false)); // composition/menu/other owner
        assert!(!owner.matches(Some(&source), Some(&tab), &displayed, 10, true, true)); // detached handle
        assert!(!owner.matches(
            Some(&source),
            Some(&tabs::TabKey::Home),
            &displayed,
            9,
            true,
            true
        ));
        assert!(!owner.matches(
            Some(&source),
            Some(&tab),
            &markdown::parse("Changed display"),
            9,
            true,
            true
        ));
        let mut shell = ShellState::default();
        shell.reader_keys = Some(owner);
        shell.detach_reader_scroll(&tab);
        assert!(shell.reader_keys.is_none());
    }
}

#[cfg(test)]
mod native_history_menu_tests {
    use super::*;
    #[test]
    fn history_controls_follow_home_tag_graph_boundaries_and_current_runtime_state() {
        let mut history = navigation::History::default();
        assert!(!native_history_enabled(&history, false, false, false));
        assert!(!native_history_enabled(&history, true, false, false));
        history.record(Some(tabs::TabKey::Tag {
            tag: "alpha".into(),
            scope: String::new(),
        }));
        history.record(Some(tabs::TabKey::Graph));
        assert!(native_history_enabled(&history, false, false, false));
        assert!(!native_history_enabled(&history, true, false, false));
        let back = history.peek(false).unwrap();
        assert!(history.commit(&back));
        assert!(native_history_enabled(&history, false, false, false));
        assert!(native_history_enabled(&history, true, false, false));
        assert!(history.commit(&history.peek(false).unwrap()));
        assert!(!native_history_enabled(&history, false, false, false));
        assert!(native_history_enabled(&history, true, false, false));
        // The same fresh predicate drives menu registration and runtime dispatch.
        assert!(!native_history_enabled(&history, true, false, true));
        assert!(native_history_enabled(&history, false, true, false)); // invalid tag returns to source
        assert!(!native_history_enabled(&history, true, true, false));
    }
    #[test]
    fn unmounted_known_focus_is_repaired_without_taking_live_or_unknown_focus() {
        assert!(stale_workspace_focus(
            false, true, true, false, false, false
        )); // reader -> Home
        assert!(stale_workspace_focus(
            true, false, false, true, false, false
        )); // search -> note
        assert!(stale_workspace_focus(
            false, false, true, false, false, true
        )); // graph -> Home
        assert!(!stale_workspace_focus(
            true, true, false, false, false, false
        )); // live reader/code
        assert!(!stale_workspace_focus(
            false, false, true, true, false, false
        )); // live Home search
        assert!(!stale_workspace_focus(
            false, false, false, false, true, true
        )); // live graph
        assert!(!stale_workspace_focus(
            false, false, false, false, false, false
        )); // unknown/input focus
        let stale = stale_workspace_focus(false, true, true, false, false, false);
        assert!(workspace_root_focus_allowed(true, !stale, false));
        assert!(!workspace_root_focus_allowed(false, !stale, false));
        assert!(!workspace_root_focus_allowed(true, !stale, true)); // owner/composition still wins
        assert!(!workspace_root_focus_allowed(true, true, false)); // repaired root settles
    }
    #[test]
    fn history_dispatch_requires_active_workspace_and_fresh_admission() {
        assert!(native_history_dispatch_allowed(true, true, true));
        for args in [
            (false, true, true),
            (true, false, true),
            (true, true, false),
            (false, false, false),
        ] {
            assert!(!native_history_dispatch_allowed(args.0, args.1, args.2));
        }
    }
    #[test]
    fn root_focus_never_steals_existing_input_or_background_focus_or_reacquires_reader_keys() {
        assert!(workspace_root_focus_allowed(true, false, false));
        for args in [
            (false, false, false),
            (true, true, false),
            (true, false, true),
            (false, true, true),
        ] {
            assert!(!workspace_root_focus_allowed(args.0, args.1, args.2));
        }
        let shell = ShellState::default();
        assert!(shell.reader_keys.is_none());
        // The fallback policy grants only the separate workspace handle. Once
        // focus exists the predicate is false, so renders cannot repeatedly focus.
        assert!(!workspace_root_focus_allowed(true, true, false));
        assert!(shell.reader_keys.is_none());
    }
}

#[cfg(test)]
mod fragment_layout_tests {
    #[test]
    fn deferred_fragment_requires_current_source_tab_handle_interaction_geometry_and_anchors() {
        assert!(super::fragment_layout_current(
            true, true, true, true, true, true
        ));
        for stale in 0..6 {
            let mut current = [true; 6];
            current[stale] = false;
            assert!(!super::fragment_layout_current(
                current[0], current[1], current[2], current[3], current[4], current[5]
            ));
        }
    }
}

#[cfg(test)]
mod native_find_tests {
    use super::native_find_enabled;
    #[test]
    fn native_find_requires_active_accepted_unblocked_reader_and_allowed_focus() {
        assert!(native_find_enabled(true, true, false, true, false, false));
        for flags in [
            (false, true, false, true),
            (true, false, false, true),
            (true, true, true, true),
            (true, true, false, false),
        ] {
            assert!(!native_find_enabled(
                flags.0, flags.1, flags.2, flags.3, false, true
            ));
            assert!(!native_find_enabled(
                flags.0, flags.1, flags.2, flags.3, true, true
            ));
        }
    }
    #[test]
    fn native_next_previous_require_query_but_open_does_not() {
        assert!(native_find_enabled(true, true, false, true, false, false));
        assert!(!native_find_enabled(true, true, false, true, true, false));
        assert!(native_find_enabled(true, true, false, true, true, true));
    }
}

#[cfg(test)]
mod reader_hover_tests {
    use super::*;
    fn owner() -> ReaderHover {
        ReaderHover {
            source: LinkSource {
                note: live::NoteKey {
                    vault: "work".into(),
                    note_id: 7,
                },
                etag: "a".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            document: code_copy::Document {
                tab: Some(tabs::TabKey::Note {
                    vault: "work".into(),
                    note_id: 7,
                }),
                request: 4,
                etag: Some("a".into()),
                storage: Some(2),
                launch: Some(3),
            },
            displayed: std::rc::Rc::new(markdown::parse("[[Target]]")),
            id: "md-block-0".into(),
            link: markdown::parse("[[Target]]")[0].links[0].clone(),
            target: live::NoteKey {
                vault: "work".into(),
                note_id: 8,
            },
            geometry: FootnoteGeometry {
                reader: gpui::Bounds {
                    origin: point(px(0.), px(0.)),
                    size: size(px(600.), px(500.)),
                },
                extent: size(px(0.), px(700.)),
                viewport: size(px(900.), px(800.)),
                scale: 1.,
            },
            offset: point(px(0.), px(-30.)),
            interaction: 10,
            scroll_epoch: 11,
            scope_revision: 12,
            layout: Default::default(),
            rendered_range: 0..6,
        }
    }
    #[test]
    fn reader_hover_requires_exact_source_and_accepted_document() {
        let o = owner();
        let matches = |source: Option<&LinkSource>, document: Option<&code_copy::Document>| {
            o.source_matches(source, document, &o.displayed, 10, 11, 12, Some(o.offset))
        };
        assert!(matches(Some(&o.source), Some(&o.document)));
        assert!(!matches(None, Some(&o.document)));
        assert!(!matches(Some(&o.source), None));
        for i in 0..6 {
            let mut source = o.source.clone();
            match i {
                0 => source.note.vault = "other".into(),
                1 => source.note.note_id += 1,
                2 => source.etag += "new",
                3 => source.storage_generation += 1,
                4 => source.launch_generation += 1,
                _ => source.request += 1,
            };
            assert!(!matches(Some(&source), Some(&o.document)));
        }
        let mut doc = o.document.clone();
        doc.tab = Some(tabs::TabKey::Graph);
        assert!(!matches(Some(&o.source), Some(&doc)));
    }
    #[test]
    fn reader_hover_rejects_reflow_scroll_and_replaced_equal_content() {
        let o = owner();
        let matches =
            |displayed: &std::rc::Rc<Vec<markdown::Block>>, interaction, epoch, scope, offset| {
                o.source_matches(
                    Some(&o.source),
                    Some(&o.document),
                    displayed,
                    interaction,
                    epoch,
                    scope,
                    offset,
                )
            };
        assert!(!matches(
            &std::rc::Rc::new((*o.displayed).clone()),
            10,
            11,
            12,
            Some(o.offset)
        ));
        assert!(!matches(&o.displayed, 11, 11, 12, Some(o.offset)));
        assert!(!matches(&o.displayed, 10, 12, 12, Some(o.offset)));
        assert!(!matches(&o.displayed, 10, 11, 13, Some(o.offset)));
        assert!(!matches(
            &o.displayed,
            10,
            11,
            12,
            Some(point(px(0.), px(-31.)))
        ));
        assert!(!matches(&o.displayed, 10, 11, 12, None));
        let mut changed = o.geometry.clone();
        changed.viewport.width = px(680.);
        assert_ne!(changed, o.geometry);
    }
}

#[cfg(test)]
mod native_reload_tests {
    use super::*;
    #[test]
    fn home_reload_uses_existing_route_even_without_active_tab() {
        assert!(native_reload_home(true, Some(&tabs::TabKey::Home), false));
        assert!(native_reload_home(true, None, true));
        assert!(!native_reload_home(true, None, false));
        assert!(!native_reload_home(false, Some(&tabs::TabKey::Home), true));
        assert!(!native_reload_home(true, Some(&tabs::TabKey::Graph), true));
        assert!(!native_reload_home(
            true,
            Some(&tabs::TabKey::Note {
                vault: String::new(),
                note_id: 7
            }),
            true
        ));
    }
    #[test]
    fn reload_requires_active_ready_idle_reader_owner_and_eligible_focus() {
        assert!(native_reload_enabled(true, true, false, false, true));
        for (active, ready, blocked, pending, focus) in [
            (false, true, false, false, true),
            (true, false, false, false, true),
            (true, true, true, false, true),
            (true, true, false, true, true),
            (true, true, false, false, false),
        ] {
            assert!(!native_reload_enabled(
                active, ready, blocked, pending, focus
            ));
        }
    }
    #[test]
    fn retained_accepted_reader_can_retry_only_same_live_storage_identity() {
        let identity = ReaderContentIdentity::new(
            "prior accepted body",
            Some((7, Some(8), "old revision".into())),
        );
        // Previous read failure/partial status is not itself an admission input.
        assert!(reload_reader_identity(Some(&identity), 7, Some(8)));
        assert!(!reload_reader_identity(Some(&identity), 9, Some(8)));
        assert!(!reload_reader_identity(Some(&identity), 7, Some(9)));
        assert!(!reload_reader_identity(Some(&identity), 7, None));
        assert!(!reload_reader_identity(None, 7, Some(8)));
        assert!(!reload_reader_identity(
            Some(&ReaderContentIdentity::new("snapshot", None)),
            7,
            Some(8)
        ));
    }
}

#[cfg(test)]
mod reader_selection_owner_tests {
    use super::*;
    #[test]
    fn selection_feedback_cannot_expire_a_new_copy_or_new_source() {
        assert!(selection_feedback_current(3, Some(8), 3, 8));
        assert!(!selection_feedback_current(3, Some(9), 3, 8));
        assert!(!selection_feedback_current(4, Some(8), 3, 8));
        assert!(!selection_feedback_current(3, None, 3, 8));
    }
    #[test]
    fn selection_copy_rejects_source_content_geometry_and_owner_changes() {
        let displayed = std::rc::Rc::new(markdown::parse("日本語 plain paragraph"));
        let source = LinkSource {
            note: live::NoteKey {
                vault: "work".into(),
                note_id: 7,
            },
            etag: "etag".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        let doc = code_copy::Document {
            tab: Some(tabs::TabKey::Note {
                vault: "work".into(),
                note_id: 7,
            }),
            request: 4,
            etag: Some("etag".into()),
            storage: Some(2),
            launch: Some(3),
        };
        let geometry = FootnoteGeometry {
            reader: gpui::Bounds {
                origin: point(px(0.), px(0.)),
                size: size(px(500.), px(600.)),
            },
            extent: size(px(0.), px(700.)),
            viewport: size(px(800.), px(900.)),
            scale: 1.,
        };
        let s = ReaderSelection {
            source: source.clone(),
            document: doc.clone(),
            displayed: displayed.clone(),
            index: 0,
            geometry: geometry.clone(),
            offset: point(px(0.), px(-20.)),
            scroll_epoch: 1,
            scope_revision: 2,
            range: reader_selection::State::begin(&displayed[0].text, 0).unwrap(),
            source_lines: None,
            multi: None,
            markdown_feedback: false,
            measured_layout: Default::default(),
            origin: point(px(1.), px(1.)),
            moved: false,
            token: 1,
            feedback: None,
        };
        assert!(s.matches(
            Some(&source),
            Some(&doc),
            &displayed,
            Some(&geometry),
            Some(s.offset),
            1,
            2
        ));
        for i in 0..6 {
            let mut changed = source.clone();
            match i {
                0 => changed.note.vault = "other".into(),
                1 => changed.note.note_id += 1,
                2 => changed.etag += "new",
                3 => changed.storage_generation += 1,
                4 => changed.launch_generation += 1,
                _ => changed.request += 1,
            };
            assert!(!s.matches(
                Some(&changed),
                Some(&doc),
                &displayed,
                Some(&geometry),
                Some(s.offset),
                1,
                2
            ));
        }
        assert!(!s.matches(
            Some(&source),
            None,
            &displayed,
            Some(&geometry),
            Some(s.offset),
            1,
            2
        ));
        assert!(!s.matches(
            Some(&source),
            Some(&doc),
            &std::rc::Rc::new((*displayed).clone()),
            Some(&geometry),
            Some(s.offset),
            1,
            2
        ));
        let mut reflow = geometry.clone();
        reflow.viewport.width = px(400.);
        assert!(!s.matches(
            Some(&source),
            Some(&doc),
            &displayed,
            Some(&reflow),
            Some(s.offset),
            1,
            2
        ));
        assert!(!s.matches(
            Some(&source),
            Some(&doc),
            &displayed,
            Some(&geometry),
            Some(point(px(0.), px(-21.))),
            1,
            2
        ));
        assert!(!s.matches(
            Some(&source),
            Some(&doc),
            &displayed,
            Some(&geometry),
            Some(s.offset),
            2,
            2
        ));
        assert!(!s.matches(
            Some(&source),
            Some(&doc),
            &displayed,
            Some(&geometry),
            Some(s.offset),
            1,
            3
        ));
    }
}
