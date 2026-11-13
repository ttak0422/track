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

pub struct ShellState {
    pub home: bool,
    pub search_open: bool,
    search_return_focus: Option<gpui::FocusHandle>,
    search_scroll: gpui::UniformListScrollHandle,
    pub dark: Option<bool>,
    pub diagnostics: bool,
    pub tab_menu: bool,
    vault_menu: bool,
    working_scope: String,
    vault_generation: Option<u64>,
    vault_discovery: Option<VaultDiscovery>,
    vault_loading: bool,
    vault_error: Option<String>,
    links: Option<LinkResolutionCache>,
    navigation_notice: Option<String>,
    pending_fragment: Option<PendingFragment>,
    reader_focus: Option<gpui::FocusHandle>,
    reader_scrolls: std::collections::HashMap<tabs::TabKey, gpui::ScrollHandle>,
    tab_cycle: Vec<tabs::TabKey>,
    tab_cycle_index: usize,
    heading_anchors:
        std::collections::HashMap<tabs::TabKey, (String, std::rc::Rc<markdown::HeadingAnchors>)>,
}

impl Default for ShellState {
    fn default() -> Self {
        Self {
            home: true,
            search_open: false,
            search_return_focus: None,
            search_scroll: Default::default(),
            dark: None,
            diagnostics: false,
            tab_menu: false,
            vault_menu: false,
            working_scope: String::new(),
            vault_generation: None,
            vault_discovery: None,
            vault_loading: false,
            vault_error: None,
            links: None,
            navigation_notice: None,
            pending_fragment: None,
            reader_focus: None,
            reader_scrolls: Default::default(),
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
struct LinkSource {
    note: live::NoteKey,
    etag: String,
    storage_generation: u64,
    launch_generation: u64,
    request: u64,
}
struct LinkResolutionCache {
    source: LinkSource,
    pending: bool,
    deferred_targets: usize,
    canceled: std::sync::Arc<std::sync::atomic::AtomicBool>,
    resolved: std::collections::HashMap<String, live::NoteKey>,
    warnings: Vec<String>,
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
    cx: &mut Context<Workspace>,
) -> Option<Stateful<Div>> {
    let title = value.get("title")?.as_str()?.to_owned();
    let key = inherited_vault.and_then(|scope| reference_key(value, scope));
    Some(div().id(id).py_1().child(title).when_some(key, |row, key| {
        row.cursor_pointer()
            .on_click(cx.listener(move |this, _, _, cx| {
                this.open_live_key(key.clone(), cx);
            }))
    }))
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
    fn current_link_source(&self) -> Option<LinkSource> {
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
            Ok::<_, String>((resolved, warnings))
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
                    Ok((resolved, warnings)) => {
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
        if self.current_link_source().as_ref() != Some(source) {
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
        self.shell.working_scope = scope;
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
        if !tab_activation_needed(&self.tabs, &key, load_current_after_close) {
            return;
        }
        if self.guard_transition(workspace_edit::Transition::ActivateTab(key.clone()), cx) {
            return;
        }
        self.shell.tab_cycle.clear();
        if !self.tabs.activate(&key) {
            return;
        }
        self.shell.tab_menu = false;
        self.shell.search_open = false;
        match key {
            tabs::TabKey::Home => self.open_home(cx),
            tabs::TabKey::Note { vault, note_id } => {
                self.shell.home = false;
                self.selected = None;
                self.content.clear();
                self.current_note = None;
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
        cx.notify();
    }
    pub(crate) fn close_tab(&mut self, key: &tabs::TabKey, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::CloseTab(key.clone()), cx) {
            return;
        }
        self.shell.tab_cycle.clear();
        self.shell.reader_scrolls.remove(key);
        self.shell.heading_anchors.remove(key);
        let was_active = self.tabs.active().is_some_and(|tab| &tab.key == key);
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
        self.request += 1;
        self.graph_request += 1;
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.shell.home = true;
        self.selected = None;
        self.content.clear();
        self.current_note = None;
        self.current_related.clear();
        self.show_graph = false;
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
        let dark = self.shell.dark.unwrap_or(matches!(
            window.appearance(),
            WindowAppearance::Dark | WindowAppearance::VibrantDark
        ));
        let p = Palette::new(dark);
        self.sync_editor_theme(dark, cx);
        self.ensure_link_resolutions(cx);
        let link_warning=self.shell.links.as_ref().filter(|cache|!cache.warnings.is_empty()).map(|cache|if cache.deferred_targets>0 {format!("Internal-link checking is limited to 128 targets; {} additional targets remain inactive. Details in diagnostics.",cache.deferred_targets)} else {"Some internal links could not be verified and remain inactive. Open diagnostics for details.".into()});
        let resolved_terms = std::rc::Rc::new(
            self.shell
                .links
                .as_ref()
                .map(|cache| cache.resolved.clone())
                .unwrap_or_default(),
        );
        let link_source = self.current_link_source();
        let local_fragments_ready = link_source.is_some();
        let weak = cx.entity().downgrade();
        let link_callbacks = markdown::LinkCallbacks {
            resolved: std::rc::Rc::new(move |target| {
                (local_fragments_ready && markdown::is_local_fragment(target))
                    || markdown::resolve_term(target)
                        .is_some_and(|term| resolved_terms.contains_key(&term))
            }),
            activate: std::rc::Rc::new(move |target, _, cx| {
                if let Some(source) = &link_source {
                    let _ = weak.update(cx, |this, cx| {
                        this.activate_internal_target(target, source, cx)
                    });
                }
            }),
        };
        if let Some(session) = &self.live {
            let generation = session.client().launch_generation;
            if self.shell.vault_generation != Some(generation) {
                self.shell.vault_generation = Some(generation);
                self.shell.working_scope.clear();
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
        self.search.update(cx, |search, _| search.set_dark(dark));
        self.graph_view.update(cx, |graph, cx| {
            graph.set_dark(dark, cx);
            graph.set_compact(true, cx);
        });
        let title = self
            .current_note
            .as_ref()
            .map(|body| body.note.title.clone())
            .or_else(|| note.as_ref().map(|n| n.title.clone()))
            .or_else(|| self.tabs.active().map(|tab| tab.title.clone()))
            .unwrap_or_default();
        let reader_focus = self
            .shell
            .reader_focus
            .get_or_insert_with(|| cx.focus_handle())
            .clone();
        let reader_key = self
            .tabs
            .active()
            .map(|tab| tab.key.clone())
            .unwrap_or(tabs::TabKey::Home);
        let reader_scroll = self
            .shell
            .reader_scrolls
            .entry(reader_key.clone())
            .or_default()
            .clone();
        let content_ready = self.current_note.is_some() || !self.content.is_empty();
        let headings = markdown::outline(&self.content);
        let outline_signature = format!("{headings:?}");
        let anchor_entry = self
            .shell
            .heading_anchors
            .entry(reader_key.clone())
            .or_insert_with(|| {
                (
                    outline_signature.clone(),
                    std::rc::Rc::new(markdown::HeadingAnchors::new(&self.content, &reader_scroll)),
                )
            });
        if anchor_entry.0 != outline_signature {
            *anchor_entry = (
                outline_signature,
                std::rc::Rc::new(markdown::HeadingAnchors::new(&self.content, &reader_scroll)),
            );
        }
        let heading_anchors = anchor_entry.1.clone();
        self.schedule_fragment_scroll(heading_anchors.clone(), reader_scroll.clone(), window, cx);
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
                rail_button("reload", "reload", p, false)
                    .on_click(cx.listener(|this, _, _, cx| this.load_notes(cx))),
            )
            .when(self.editing_ui.enabled(), |rail| {
                rail.child(self.render_edit_controls(cx))
            })
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
        let aside =
            div()
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
                .when(note.as_ref().is_some_and(|n| !n.tags.is_empty()), |aside| {
                    aside.child(div().mb_8().child(label("TAGS", p)).children(
                        note.as_ref().unwrap().tags.iter().map(|tag| {
                            div().mb_2().text_color(rgb(p.ink)).child(format!("#{tag}"))
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
        let aside = aside.when(
            self.current_related
                .get("children")
                .and_then(|v| v.as_array())
                .is_some_and(|children| !children.is_empty()),
            |aside| {
                aside.child(
                    div().mt_6().child(label("CHILDREN", p)).children(
                        self.current_related["children"]
                            .as_array()
                            .unwrap()
                            .iter()
                            .enumerate()
                            .filter_map(|(index, entry)| {
                                reference_row(
                                    entry,
                                    reference_vault.as_deref(),
                                    ("child-reference", index),
                                    cx,
                                )
                            }),
                    ),
                )
            },
        );
        let aside = aside.when(self.current_related.contains_key("backlinks"), |aside| {
            let links = self
                .current_related
                .get("backlinks")
                .and_then(|v| v.as_array())
                .cloned()
                .unwrap_or_default();
            aside.child(
                div()
                    .mt_6()
                    .child(label("BACKLINKS", p))
                    .when(links.is_empty(), |section| section.child("No backlinks."))
                    .children(links.iter().enumerate().filter_map(|(index, link)| {
                        reference_row(
                            link,
                            reference_vault.as_deref(),
                            ("backlink-reference", index),
                            cx,
                        )
                    })),
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
        let trail = self
            .current_related
            .get("trail")
            .and_then(|v| v.as_array())
            .cloned()
            .unwrap_or_default();
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
                                        cx,
                                    )
                                    .map(|row| {
                                        div().flex().items_center().gap_1().child(row).child("›")
                                    })
                                })),
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
                            .child(markdown::render_with_links(
                                &self.content,
                                dark,
                                Some(&heading_anchors),
                                &link_callbacks,
                            ))
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
            .child(
                div()
                    .w(px(520.))
                    .max_w_full()
                    .text_size(px(11.))
                    .text_color(rgb(p.muted))
                    .mb_8()
                    .child("Activity history is not available in this preview."),
            )
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
                cx.listener(|this, _, _, _| this.shell.tab_cycle.clear()),
            )
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
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
                if event.keystroke.key == "w"
                    && modifiers.platform
                    && !modifiers.control
                    && !modifiers.alt
                    && !modifiers.shift
                {
                    if let Some(key) = this.tabs.active().map(|tab| tab.key.clone()) {
                        this.close_tab(&key, cx);
                        cx.stop_propagation();
                    }
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
            .when(home, |root| root.child(home_view))
            .when(!home && !self.editing_ui.active(), |root| {
                root.child(
                    div()
                        .flex()
                        .flex_1()
                        .min_h_0()
                        .child(reader)
                        .when(wide, |row| row.child(aside.take().unwrap())),
                )
            })
            .when(self.editing_ui.active(), |root| {
                root.child(self.render_editor(dark))
            })
            .child(rail)
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
    }
}

#[cfg(test)]
mod reference_tests {
    use super::reference_key;
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
    fn terms_deduplicate_and_exclude_external_and_includes() {
        let blocks =
            markdown::parse("[[One]] [[One#part]] [web](https://example.com) ![[include]]");
        assert_eq!(internal_terms(&blocks), vec!["One"]);
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
