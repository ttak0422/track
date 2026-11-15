//! Native one-sheet shell. Only backed actions are interactive.
use super::*;
use gpui::{Div, Focusable, PathBuilder, Stateful, WindowAppearance, canvas, point};

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
    code_copy: code_copy::State,
    code_anchors: Option<std::rc::Rc<markdown::FindAnchors>>,
    code_focus: std::collections::BTreeMap<usize, gpui::FocusHandle>,
    pub(crate) find: workspace_find::FindState,
    relation_pages: reader_relations::Pages,
    pub(crate) includes: Option<workspace_includes::IncludesState>,
    pub(crate) note_tasks: Option<workspace_note_tasks::NoteTasksState>,
    pub(crate) hierarchy: Option<workspace_hierarchy::HierarchyState>,
    pub(crate) activity: Option<workspace_activity::ActivityState>,
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
    reader_scrolls: std::collections::HashMap<tabs::TabKey, gpui::ScrollHandle>,
    reader_content: std::collections::HashMap<tabs::TabKey, ReaderContentIdentity>,
    tab_cycle: Vec<tabs::TabKey>,
    tab_cycle_index: usize,
    heading_anchors:
        std::collections::HashMap<tabs::TabKey, (String, std::rc::Rc<markdown::HeadingAnchors>)>,
}

impl ShellState {
    /// GPUI ScrollAnchor queues its own unguarded next-frame callback. Retire the
    /// entire handle so callbacks already queued can only move an abandoned reader.
    /// Preserve the offset in a fresh handle and rebind every host anchor namespace.
    fn detach_reader_scroll(&mut self, key: &tabs::TabKey) {
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
            code_copy: Default::default(),
            code_anchors: None,
            code_focus: Default::default(),
            find: Default::default(),
            hierarchy: None,
            note_tasks: None,
            includes: None,
            relation_pages: Default::default(),
            activity: None,
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

struct LinkResolutionCache {
    source: LinkSource,
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
        self.displayed = std::rc::Rc::new(markdown::with_resolved_titles(authored, titles));
    }
}
impl Drop for LinkResolutionCache {
    fn drop(&mut self) {
        self.canceled
            .store(true, std::sync::atomic::Ordering::Release);
    }
}
fn uncertain_resolution(fields: &std::collections::BTreeMap<String, serde_json::Value>) -> bool {
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
            .on_click(cx.listener(move |this, _, _, cx| {
                if source.as_ref().is_some_and(|source| {
                    reader_relations::allows(
                        source,
                        this.current_link_source().as_ref(),
                        this.current_related.get(section),
                        id.1,
                        &captured,
                        this.find_available()
                            && this.read_error.is_none()
                            && !this.quit_session.is_closing()
                            && this.quit_session.failure().is_none(),
                    )
                }) {
                    this.open_live_key(key.clone(), cx);
                }
            }))
    }))
}
// show_graph only controls the reader's aside, never replaces the article.
// Keep it explicit here so the availability regression covers both aside states.
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
    fn relation_section(&mut self, section: &'static str, cx: &mut Context<Self>) -> Div {
        let mut view = div().text_size(px(12.));
        let source = self.current_link_source();
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
                        .on_click(cx.listener(move |this, _, _, cx| {
                            if this.current_link_source().as_ref() == Some(&captured)
                                && this.find_available()
                                && this.read_error.is_none()
                                && !this.quit_session.is_closing()
                                && this.quit_session.failure().is_none()
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
        if self
            .shell
            .links
            .as_ref()
            .is_some_and(|cache| cache.source == source)
        {
            return;
        }
        let mut terms = internal_terms(&self.content);
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
            pending: !terms.is_empty(),
            deferred_targets,
            canceled: Default::default(),
            resolved: Default::default(),
            displayed: std::rc::Rc::new(self.content.clone()),
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
                if cache.source != source {
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
            Err(markdown::FragmentError::UnsupportedBlock) => {
                self.shell.navigation_notice =
                    Some("Block fragment jumps are unavailable; the note was opened.".into());
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
        self.shell.pending_fragment.as_mut().unwrap().scheduled = true;
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
                if !still_pending
                    || this.request != pending.request
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
                if this.editing_ui.active() {
                    return;
                }
                if let Some(anchor) = anchor {
                    if !anchors.scroll_to(&anchor, window, cx) {
                        this.shell.navigation_notice = Some(
                            "Heading layout was unavailable; no fragment jump was applied.".into(),
                        );
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
                    Ok(discovery) => {
                        let mut known: Vec<_> =
                            discovery.available.iter().map(|v| v.name.clone()).collect();
                        if known.contains(&discovery.launch_name) {
                            known.push(String::new());
                        }
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
        cx: &mut Context<Self>,
    ) -> bool {
        let Some(route) = route else {
            return false;
        };
        match route {
            tabs::TabKey::Tag { tag, scope } => self.open_tag_page(tag, scope, cx),
            tabs::TabKey::Home => self.show_empty_home(),
            tabs::TabKey::Graph => self.open_full_graph(cx),
            tabs::TabKey::Note { vault, note_id } => {
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
                } else {
                    self.read_error =
                        Some("Selected note is absent after reload; route retained.".into());
                }
            }
        }
        true
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
                self.restore_reload_route(route, cx);
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
            if !self.shell.home && !self.full_graph_active() && self.editor_shortcut_save_eligible()
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
        self.sync_creation_journal(cx);
        let dark = self.shell.dark.unwrap_or(matches!(
            window.appearance(),
            WindowAppearance::Dark | WindowAppearance::VibrantDark
        ));
        let p = Palette::new(dark);
        self.reset_stale_handoff();
        if self.handoff_active() {
            return self.render_handoff(dark, (p.paper, p.ink, p.line), cx);
        }
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
        let link_callbacks = markdown::LinkCallbacks {
            resolved: std::rc::Rc::new(move |target| {
                (external_ready
                    && matches!(target, markdown::LinkTarget::Footnote { label } if footnote_labels.contains_key(label)))
                    || (local_fragments_ready && markdown::is_local_fragment(target))
                    || (external_ready && external_links::destination(target).is_some())
                    || markdown::resolve_term(target)
                        .is_some_and(|term| resolved_terms.contains_key(&term))
            }),
            activate: std::rc::Rc::new(move |link, window, cx| {
                if let Some(source) = &link_source {
                    let _ = weak.update(cx, |this, cx| {
                        if matches!(link.target, markdown::LinkTarget::Footnote { .. }) {
                            if !footnote_jump_allowed(
                                source,
                                this.current_link_source().as_ref(),
                                &external_displayed,
                                &this.displayed_content(),
                                link,
                                this.find_available()
                                    && this.read_error.is_none()
                                    && !this.quit_session.is_closing(),
                            ) {
                                return;
                            }
                            let source = source.clone();
                            let displayed = external_displayed.clone();
                            let link = link.clone();
                            let weak = cx.entity().downgrade();
                            window.on_next_frame(move |window, cx| {
                                let _ =
                                    weak.update(cx, |this, cx| {
                                        if !footnote_jump_allowed(
                                            &source,
                                            this.current_link_source().as_ref(),
                                            &displayed,
                                            &this.displayed_content(),
                                            &link,
                                            this.find_available()
                                                && this.read_error.is_none()
                                                && !this.quit_session.is_closing(),
                                        ) {
                                            return;
                                        }
                                        let markdown::LinkTarget::Footnote { label } = &link.target
                                        else {
                                            return;
                                        };
                                        this.cancel_position_resume();
                                        if let Some(anchors) = this.tabs.active().and_then(|tab| {
                                            this.shell.heading_anchors.get(&tab.key)
                                        }) {
                                            anchors.1.scroll_to_footnote(label, window, cx);
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
            .or_else(|| self.follow_layout_source(cx));
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
            "{headings:?}:{:?}",
            markdown::footnote_targets(&self.content)
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
                self.editing_ui.enabled() && !full_graph_active && self.tag_page.is_none(),
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
        let aside = aside.child(children_relations).child(backlink_relations);
        // Derive only from the accepted response; no additional reads or cache.
        let external =
            crate::external_refs::ExternalReferences::from_related(&self.current_related);
        let external_source = self.current_link_source();
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
                                        move |this, _, _, cx| {
                                            // Reject a click from a previous note/session/storage render.
                                            if this.current_link_source().as_ref() == Some(&source)
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
        let metadata: Vec<(String, String)> = self
            .current_note
            .as_ref()
            .map(|body| {
                let mut fields: Vec<_> = body
                    .props
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(|prop| {
                        let key = prop.get("key")?.as_str()?;
                        if key == "up" {
                            return None;
                        }
                        Some((key.to_uppercase(), prop.get("value")?.as_str()?.to_owned()))
                    })
                    .collect();
                if let Some(created) = body.note.additional.get("created").and_then(|v| v.as_str())
                {
                    fields.push(("CREATED".into(), created.into()));
                }
                if let Some(updated) = body
                    .note
                    .additional
                    .get("updated")
                    .and_then(|v| v.as_i64())
                    .and_then(display_date)
                {
                    fields.push(("UPDATED".into(), updated));
                }
                fields
            })
            .unwrap_or_default();
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
                                        self.current_link_source(),
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
                        reader.child(div().flex().gap(px(28.)).mb(px(36.)).children(
                            metadata.into_iter().map(|(key, value)| {
                                div()
                                    .child(
                                        div()
                                            .font_family("monospace")
                                            .text_size(px(11.))
                                            .text_color(rgb(p.muted))
                                            .child(key),
                                    )
                                    .child(
                                        div()
                                            .text_size(px(13.))
                                            .text_color(rgb(p.muted))
                                            .child(value),
                                    )
                            }),
                        ))
                    })
                    .when(
                        self.current_note.is_some()
                            && self
                                .current_note
                                .as_ref()
                                .is_some_and(|note| note.props.is_null())
                            && trail.is_empty(),
                        |reader| {
                            reader.child(
                                div()
                                    .text_size(px(11.))
                                    .text_color(rgb(p.muted))
                                    .mb_6()
                                    .child("Additional metadata is unavailable in this response."),
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
                                    .child(title),
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
                                    (note.title.clone(), note.tags.join(" · "))
                                } else {
                                    let note = this.snapshot.notes.get(i)?;
                                    (note.title.clone(), note.tags.join(" · "))
                                };
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
                                        .child(div().text_size(px(13.)).child(title))
                                        .child(
                                            div()
                                                .text_size(px(10.))
                                                .text_color(rgb(p.muted))
                                                .child(tags),
                                        )
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
                            })
                            .collect()
                    }),
                )
                .track_scroll(self.shell.search_scroll.clone())
                .flex_1()
                .min_h_0(),
            );
        div()
            .id("workspace-shell")
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
                    this.shell.tab_cycle.clear();
                    this.cancel_follow_reveal();
                    this.shell.positions.interaction =
                        this.shell.positions.interaction.wrapping_add(1);
                }),
            )
            .on_scroll_wheel(cx.listener(|this, _, _, _| {
                this.cancel_follow_reveal();
                this.shell.positions.interaction = this.shell.positions.interaction.wrapping_add(1);
            }))
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                if this.creation_open() {
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
                if this.find_keyboard(event, window, cx) {
                    cx.stop_propagation();
                    return;
                }
                if event.keystroke.key == "escape" {
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
                if matches!(event.keystroke.key.as_str(), "[" | "]")
                    && modifiers.platform
                    && !modifiers.control
                    && !modifiers.alt
                    && !modifiers.shift
                {
                    this.navigate_history(event.keystroke.key == "]", cx);
                    cx.stop_propagation();
                } else if event.keystroke.key == "tab"
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
            .when(full_graph_active, |root| {
                root.child(self.render_full_graph(cx))
            })
            .when(
                !home && !full_graph_active && self.tag_page.is_none() && !self.editing_ui.active(),
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

/// Reuse strict current discovery; only available names and the verified launch alias.
pub(crate) fn session_registry(value: &serde_json::Value) -> Result<Vec<String>, String> {
    let discovery = VaultDiscovery::parse(value)?;
    if !discovery
        .available
        .iter()
        .any(|entry| entry.name == discovery.launch_name)
    {
        return Err("Launch vault is absent from available registry".into());
    }
    let mut names: Vec<String> = discovery
        .available
        .into_iter()
        .map(|entry| entry.name)
        .collect();
    names.push(String::new());
    names.sort();
    names.dedup();
    Ok(names)
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
