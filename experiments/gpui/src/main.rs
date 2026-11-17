mod inline_viewspec;
mod math_probe;
mod pdf_probe;
mod reader_pdf;
mod workspace_math;
mod workspace_pdf;
use gpui::{
    App, Application, Bounds, Context, KeyBinding, Menu, MenuItem, Window, WindowBounds,
    WindowOptions, actions, div, prelude::*, px, rgb, size, uniform_list,
};
mod about;
mod backend;
mod block_arrival;
mod build_info;
mod calendar_agenda;
mod calendar_month;
mod code_copy;
mod code_highlight;
mod draft_recovery;
mod editing;
mod editor_follow;
mod editor_input;
mod event_awareness;
mod external_links;
mod external_refs;
mod flowchart;
mod flowchart_view;
mod graph;
mod graph_preview;
mod graph_view;
mod included_notes;
mod input_trace;
mod live;
mod live_events;
mod live_refresh;
mod markdown;
mod markdown_copy;
mod markdown_handoff;
mod mindmap;
mod mindmap_view;
mod native_window;
mod navigation;
mod note_creation;
mod note_metadata;
mod note_properties;
mod numeric_chart;
mod numeric_chart_view;
mod preview_permit;
mod query_view;
mod raster;
mod reader_find;
mod reader_relations;
mod reader_scroll;
mod reader_selection;
mod recent_notes;
mod rendered_find_model;
mod search_excerpt;
mod search_input;
mod search_refinement;
mod selection_lines;
mod session_store;
mod shell;
mod snapshot_find;
mod snapshot_images;
mod snapshot_links;
mod tabs;
mod task_date_sort;
mod title_copy;
mod weekly_tasks;
mod workspace_activity;
mod workspace_calendar;
mod workspace_calendar_agenda;
mod workspace_edit;
mod workspace_find;
mod workspace_follow;
mod workspace_graph;
mod workspace_handoff;
mod workspace_hierarchy;
mod workspace_images;
mod workspace_includes;
mod workspace_journal_day;
mod workspace_live;
#[cfg(test)]
mod workspace_read_request;
#[cfg(test)]
mod workspace_read_request_view;
mod workspace_note_tasks;
mod workspace_open_tasks;
mod workspace_preview;
mod workspace_recent;
mod workspace_recent_created;
mod workspace_refresh;
mod workspace_session;
mod workspace_tags;
mod workspace_weekly;
use backend::{Backend, Note, Snapshot};
actions!(
    native_workspace,
    [
        Quit,
        CloseWindow,
        CloseTab,
        Save,
        MinimizeWindow,
        ZoomWindow,
        ToggleFullscreen,
        CopyBuildInfo,
        ShowAbout,
        HistoryBack,
        HistoryForward,
        NativeFind,
        NativeFindNext,
        NativeFindPrevious,
        NativeReload
    ]
);

fn workspace_key_bindings() -> Vec<KeyBinding> {
    vec![
        KeyBinding::new("cmd-q", Quit, None),
        KeyBinding::new("cmd-s", Save, None),
        KeyBinding::new("cmd-w", CloseTab, None),
        KeyBinding::new("cmd-[", HistoryBack, None),
        KeyBinding::new("cmd-]", HistoryForward, None),
        KeyBinding::new("cmd-r", NativeReload, None),
        KeyBinding::new("cmd-f", NativeFind, None),
        KeyBinding::new("cmd-g", NativeFindNext, None),
        KeyBinding::new("cmd-shift-g", NativeFindPrevious, None),
        KeyBinding::new("cmd-m", MinimizeWindow, None),
        KeyBinding::new("ctrl-cmd-f", ToggleFullscreen, None),
    ]
}

/// Route only to the native window that owned dispatch. Never use the quit
/// fallback or a retained workspace behind an informational window.
fn shortcut_workspace(handle: gpui::AnyWindowHandle) -> Option<gpui::WindowHandle<Workspace>> {
    handle.downcast::<Workspace>()
}

fn route_workspace_shortcut(cx: &mut App, save: bool) {
    let Some(handle) = cx.active_window().and_then(shortcut_workspace) else {
        return;
    };
    // App-level action dispatch holds the window; update after it releases it.
    cx.defer(move |cx| {
        if cx.active_window() != Some(handle.into()) {
            return;
        }
        let _ = handle.update(cx, |workspace, _, cx| {
            workspace.workspace_shortcut(save, cx);
        });
    });
}

fn fixture(count: usize) -> Snapshot {
    Snapshot {
        notes: (0..count)
            .map(|i| Note {
                note_id: i as i64,
                title: format!("Field note {:05} · Native workspace", i + 1),
                tags: vec![format!("topic-{}", i % 12)],
                path: Default::default(),
                file_kind: "note".into(),
            })
            .collect(),
        read_state: None,
    }
}

struct Workspace {
    shell: shell::ShellState,
    editing_ui: workspace_edit::EditingUi,
    tabs: tabs::Tabs,
    history: navigation::History,
    tag_page: Option<workspace_tags::Page>,
    session_ui: workspace_session::SessionUi,
    event_awareness: event_awareness::State,
    refresh: workspace_refresh::Ui,
    follow: workspace_follow::FollowUi,
    reader_images: workspace_images::State,
    reader_pdf: Option<workspace_pdf::State>,
    reader_math: Option<workspace_math::State>,
    math_reference_cache: std::cell::RefCell<workspace_math::ReferenceCache>,
    pdf_enlargement: Option<workspace_pdf::Modal>,
    snapshot: Snapshot,
    selected: Option<usize>,
    rendered_rows: usize,
    source: String,
    backend: Option<Backend>,
    content: Vec<markdown::Block>,
    current_note: Option<live::NoteBody>,
    current_note_request: Option<live::NoteKey>,
    read_error: Option<String>,
    partial_read: bool,
    current_related: std::collections::BTreeMap<String, serde_json::Value>,
    status: String,
    list_status: String,
    request: u64,
    live_reader_pending: Option<u64>,
    graph_request: u64,
    graph_view: gpui::Entity<graph_view::GraphView>,
    show_graph: bool,
    full_graph: workspace_graph::FullGraph,
    previews: workspace_preview::PreviewUi,
    subscriptions: Vec<gpui::Subscription>,
    live: Option<live::Session>,
    live_requested: bool,
    accepted_storage_generation: Option<u64>,
    live_notes: Vec<live::Note>,
    search_results: Vec<live::Note>,
    search: gpui::Entity<search_input::SearchInput>,
    query_epoch: std::sync::Arc<std::sync::atomic::AtomicU64>,
    quit_session: std::sync::Arc<live::LifecycleHandle>,
}

impl Workspace {
    fn load_graph(&mut self, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::Graph, cx) {
            return;
        }
        if live_mode_waiting(self.live_requested, self.live.is_some()) {
            self.read_error = Some("Owned server is not ready; CLI fallback is disabled.".into());
            cx.notify();
            return;
        }
        if self.live.is_some() {
            self.load_live_graph(cx);
            return;
        }
        self.show_graph = true;
        self.graph_request += 1;
        let request = self.graph_request;
        let Some(backend) = self.backend.clone() else {
            self.graph_view
                .update(cx, |view, cx| view.set_graph(graph::fixture(100), cx));
            cx.notify();
            return;
        };
        let Some(note) = self.selected.and_then(|i| self.snapshot.notes.get(i)) else {
            return;
        };
        let id = note.note_id;
        self.status = "Loading graph…".into();
        self.graph_view
            .update(cx, |view, cx| view.set_graph(graph::Graph::default(), cx));
        let job = cx
            .background_executor()
            .spawn(async move { backend.graph(id) });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if request != this.graph_request {
                    return;
                }
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        this.graph_view
                            .update(cx, |view, cx| view.set_graph(response.value, cx));
                        this.status = response.warnings;
                    }
                    Err(error) => {
                        this.status = error.to_string();
                        this.read_error = Some(error.to_string());
                    }
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }

    fn load_notes(&mut self, cx: &mut Context<Self>) {
        if let Some(page) = &self.tag_page {
            self.open_tag_page(page.tag.clone(), page.scope.clone(), cx);
            return;
        }
        if self.guard_transition(workspace_edit::Transition::Reload, cx) {
            return;
        }
        if live_mode_waiting(self.live_requested, self.live.is_some()) {
            self.read_error = Some("Owned server is not ready; CLI fallback is disabled.".into());
            cx.notify();
            return;
        }
        if self.live.is_some() {
            self.load_live_notes(cx);
            return;
        }
        let Some(backend) = self.backend.clone() else {
            return;
        };
        let reload_route = self.history.reload_route(&self.tabs);
        self.request += 1;
        let request = self.request;
        self.status = "Loading notes…".into();
        let job = cx
            .background_executor()
            .spawn(async move { backend.notes() });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        this.snapshot = response.value;
                        this.partial_read = this.snapshot.read_state.as_ref().is_some_and(|s|s.partial);
                        this.list_status = response.warnings;
                        if let Some(state) = &this.snapshot.read_state {
                            this.list_status.push_str(&format!("\nRead state: partial={}, cached={}, checked={}, omitted={}, issues={:?}, extra={:?}", state.partial, state.cache_used, state.checked_at, state.omitted, state.issues, state.additional));
                        }
                        this.selected = None;
                        this.content.clear();
                        if !this.restore_reload_route(reload_route.clone(), false, cx) && !this.snapshot.notes.is_empty() {
                            this.select(0, cx);
                        }
                    }
                    Err(error) => { this.status = error.to_string(); this.read_error = Some(error.to_string()); },
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }

    fn select_from_ui(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(note) = self.snapshot.notes.get(index) else {
            return;
        };
        let intent = self
            .live_notes
            .get(index)
            .filter(|_| self.live.is_some())
            .map(|n| workspace_edit::Transition::Note(n.key()))
            .unwrap_or(workspace_edit::Transition::LegacyNote(note.note_id));
        if self.guard_transition(intent, cx) {
            return;
        }
        self.shell.home = false;
        self.shell.search_open = false;
        self.search.update(cx, |search, cx| search.clear(cx));
        let previous = self.request;
        let recent_key = self.live_notes.get(index).map(|note| note.key());
        self.select(index, cx);
        if let Some(key) = recent_key {
            self.recent_note_requested(previous, key);
        }
    }
    fn select(&mut self, index: usize, cx: &mut Context<Self>) {
        self.select_expected(index, None, cx);
    }
    fn select_expected(
        &mut self,
        index: usize,
        expected: Option<live::JournalExpectation>,
        cx: &mut Context<Self>,
    ) {
        if live_mode_waiting(self.live_requested, self.live.is_some()) {
            return;
        }
        let Some(note) = self.snapshot.notes.get(index) else {
            return;
        };
        let intent = self
            .live_notes
            .get(index)
            .filter(|_| self.live.is_some())
            .map(|n| workspace_edit::Transition::Note(n.key()))
            .unwrap_or(workspace_edit::Transition::LegacyNote(note.note_id));
        if self.guard_transition(intent, cx) {
            return;
        }
        self.tag_page = None;
        let note = &self.snapshot.notes[index];
        if !self.shell.home || self.configured_home_index() == Some(index) {
            let vault = self
                .live_notes
                .get(index)
                .filter(|_| self.live.is_some())
                .map(|note| note.vault.clone())
                .unwrap_or_default();
            self.tabs.open_note(vault, note.note_id, note.title.clone());
            self.history
                .record(self.tabs.active().map(|t| t.key.clone()));
            self.session_ui
                .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        }
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.graph_request += 1;
        self.show_graph = false;
        self.selected = Some(index);
        self.request += 1;
        self.content.clear();
        self.current_note = None;
        self.current_note_request = None;
        self.current_related.clear();
        if self.live.is_some() {
            self.select_live(index, expected, cx);
            return;
        }
        let Some(backend) = self.backend.clone() else {
            cx.notify();
            return;
        };
        let id = self.snapshot.notes[index].note_id;
        let request = self.request;
        self.status = "Loading portable Markdown…".into();
        let job = cx
            .background_executor()
            .spawn(async move { backend.export_markdown(id) });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        let key = tabs::TabKey::Note {
                            vault: String::new(),
                            note_id: id,
                        };
                        if this.tabs.active().is_some_and(|tab| tab.key == key) {
                            this.shell.accept_reader_content(key, &response.value, None);
                        }
                        this.content = markdown::parse(&response.value);
                        this.status = response.warnings;
                    }
                    Err(error) => {
                        this.status = error.to_string();
                        this.read_error = Some(error.to_string());
                    }
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
}

fn live_mode_waiting(requested: bool, has_session: bool) -> bool {
    requested && !has_session
}

fn route_workspace_quit(
    cx: &mut App,
    target: &std::rc::Rc<std::cell::RefCell<Option<gpui::WeakEntity<Workspace>>>>,
    owner: std::sync::Arc<live::LifecycleHandle>,
) {
    let entity = target.borrow().as_ref().and_then(|weak| weak.upgrade());
    if let Some(entity) = entity {
        entity.update(cx, |workspace, cx| workspace.request_exit(cx));
    } else {
        request_quit(cx, owner, None);
    } // No workspace/draft exists during startup/teardown.
}

fn request_quit(
    cx: &mut App,
    owner: std::sync::Arc<live::LifecycleHandle>,
    workspace: Option<gpui::WeakEntity<Workspace>>,
) {
    let job = cx
        .background_executor()
        .spawn(async move { owner.shutdown() });
    cx.spawn(async move |cx| {
        if let Err(error) = job.await {
            eprintln!("Server shutdown incomplete: {error}");
            let message = format!(
                "Shutdown failed: {error}. Window retained; retry Quit after resolving the error."
            );
            let _ = cx.update(|cx| {
                if let Some(entity) = workspace.and_then(|weak| weak.upgrade()) {
                    entity.update(cx, |this, cx| this.editor_shutdown_failed(message, cx));
                }
            });
            return; // Never exit or leave the transition gate permanently blocked.
        }
        let _ = cx.update(|cx| cx.quit());
    })
    .detach();
}

struct StderrLog;
impl log::Log for StderrLog {
    fn enabled(&self, metadata: &log::Metadata) -> bool {
        metadata.level() <= log::Level::Warn
    }
    fn log(&self, record: &log::Record) {
        if self.enabled(record.metadata()) {
            eprintln!("{}: {}", record.level(), record.args());
        }
    }
    fn flush(&self) {}
}
static LOGGER: StderrLog = StderrLog;

/// Inputs are the canonical roots returned by the existing fixture validator.
/// This gate does not probe storage, especially when recovery is not requested.
fn validate_recovery_binding(
    enabled: bool,
    owned_server: bool,
    edit_root: Option<&std::path::Path>,
    session_root: Option<&std::path::Path>,
) -> Result<(), &'static str> {
    if !enabled {
        return Ok(());
    }
    if !owned_server {
        return Err("Developer draft recovery requires an owned --server-port.");
    }
    match (edit_root, session_root) {
        (Some(edit), Some(session)) if edit == session => Ok(()),
        _ => Err(
            "Developer draft recovery requires both verified --developer-edit-fixture and --developer-session-fixture bindings to the same disposable fixture.",
        ),
    }
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    #[cfg(target_os = "macos")]
    if let Some(result) = pdf_probe::early_helper(&args) {
        return result.map_err(Into::into);
    }
    #[cfg(unix)]
    if let Some(result) = math_probe::early_helper(&args) {
        return result.map_err(Into::into);
    }
    let _ = log::set_logger(&LOGGER);
    log::set_max_level(log::LevelFilter::Warn);
    if args.as_slice() == ["--version"] {
        print!("{}", build_info::clipboard_payload());
        return Ok(());
    }
    if args.first().is_some_and(|a| a == "--help") {
        println!(
            "track-gpui-poc --version\ntrack-gpui-poc [notes.json]\ntrack-gpui-poc --vault /absolute/vault --track-bin /absolute/track [--config /absolute/config] [--cache /absolute/cache] [--server-port 19000] [--developer-read-fixture /canonical/temporary/disposable/vault] [--developer-edit-fixture /temporary/disposable/vault] [--developer-session-fixture /canonical/temporary/disposable/vault] [--developer-draft-recovery]\nFixture-only draft recovery requires both fixture options bound to the same root and isolated config/cache.\nTab persistence is a separate opt-in: cache/track-native-tabs-v1.json, metadata only, safe Home on restart.\nDeveloper editing requires a disposable marker and isolated temporary config/cache; not release-ready.\nNo arguments: synthetic fixture. Ordinary vault launch is disabled: engine reads can write canonical activity metadata. Disposable launches require --developer-read-fixture (or the explicit edit/session fixture option), a temporary marked vault, and isolated config containing only {{}} plus cache. Read-fixture permission does not enable editing."
        );
        return Ok(());
    }
    let mut live_config = None;
    let mut developer_fixture = None;
    let mut session_options = None;
    let mut developer_draft_recovery = false;
    let (snapshot, source, backend) = if args.first().is_some_and(|a| a.starts_with("--")) {
        let mut server_port = None;
        let mut verified_session_fixture = None;
        let mut session_fixture: Option<std::path::PathBuf> = None;
        let mut fixture_option: Option<std::path::PathBuf> = None;
        let mut read_fixture_root: Option<std::path::PathBuf> = None;
        let mut vault = None;
        let mut executable = None;
        let mut config = None;
        let mut cache = None;
        let mut iter = args.iter();
        while let Some(flag) = iter.next() {
            if flag == "--developer-draft-recovery" {
                developer_draft_recovery = true;
                continue;
            }
            let value = iter.next().ok_or("missing option value")?;
            match flag.as_str() {
                "--developer-session-fixture" => session_fixture = Some(value.into()),
                "--developer-read-fixture" => read_fixture_root = Some(value.into()),
                "--developer-edit-fixture" => fixture_option = Some(value.into()),
                "--server-port" => server_port = Some(value.parse::<u16>()?),
                "--vault" => vault = Some(value.into()),
                "--track-bin" => executable = Some(value.into()),
                "--config" => config = Some(value.into()),
                "--cache" => cache = Some(value.into()),
                _ => return Err(format!("unknown option: {flag}").into()),
            }
        }
        let read_root=read_fixture_root.as_ref().or(fixture_option.as_ref()).or(session_fixture.as_ref()).ok_or("Ordinary vault launch remains disabled. Use synthetic/JSON input or --developer-read-fixture with an isolated disposable fixture and {} config.")?;
        let read_proof = live::ReadFixture::capture(
            read_root,
            vault.as_deref().ok_or("--vault is required")?,
            config.as_deref(),
            cache.as_deref(),
        )?;
        if let Some(root) = fixture_option {
            if server_port.is_none() {
                return Err("Developer editing requires an owned --server-port.".into());
            }
            developer_fixture = Some(workspace_edit::validate_fixture(
                &root,
                vault.as_deref().ok_or("--vault is required")?,
                config.as_deref(),
                cache.as_deref(),
            )?);
        }
        if let Some(root) = session_fixture {
            if server_port.is_none() {
                return Err("Developer tab persistence requires an owned --server-port.".into());
            }
            verified_session_fixture = Some(workspace_edit::validate_fixture(
                &root,
                vault.as_deref().ok_or("--vault is required")?,
                config.as_deref(),
                cache.as_deref(),
            )?);
            session_options = Some(workspace_session::SessionOptions {
                fixture: root,
                config: config.clone().ok_or("--config is required")?,
                cache: cache.clone().ok_or("--cache is required")?,
            });
        }
        validate_recovery_binding(
            developer_draft_recovery,
            server_port.is_some(),
            developer_fixture.as_deref(),
            verified_session_fixture.as_deref(),
        )?;
        if let Some(port) = server_port {
            let mut config_live = live::LaunchConfig::new(
                executable.clone().ok_or("--track-bin is required")?,
                live::Vault::Path(vault.clone().ok_or("--vault is required")?),
                port,
            );
            config_live.read_fixture = Some(read_proof.clone());
            config_live.config = config.clone();
            config_live.cache = cache.clone();
            if let Some(root) = developer_fixture.as_ref() {
                config_live.creation_fixture = Some(live::CreateFixture::capture(
                    root,
                    vault.as_deref().ok_or("--vault is required")?,
                    config.as_deref(),
                    cache.as_deref(),
                )?);
            }
            live_config = Some(config_live);
        }
        let mut backend = Backend::new(
            executable.ok_or("--track-bin is required")?,
            vault.ok_or("--vault is required")?,
        )?;
        if let Some(config) = config {
            backend = backend.with_config(config)?;
        }
        if let Some(cache) = cache {
            backend = backend.with_cache(cache)?;
        }
        backend = backend.with_read_fixture(read_proof)?;
        (
            fixture(0),
            if live_config.is_some() {
                "explicit vault / owned HTTP".into()
            } else {
                "explicit vault / CLI".into()
            },
            if live_config.is_some() {
                None
            } else {
                Some(backend)
            },
        )
    } else if args.len() == 1 {
        (
            serde_json::from_reader(std::fs::File::open(&args[0])?)?,
            "imported JSON".into(),
            None,
        )
    } else if args.is_empty() {
        (fixture(10_000), "synthetic fixture".into(), None)
    } else {
        return Err("expected a single JSON path or explicit vault options".into());
    };
    let input_trace_enabled = input_trace::initialize(developer_fixture.is_some());
    Application::new().run(move |cx: &mut App| {
        if input_trace_enabled {
            cx.observe_keystrokes(|event, _, _| input_trace::key(event))
                .detach();
        }
        let quit_session = std::sync::Arc::new(live::LifecycleHandle::default());
        let workspace_target: std::rc::Rc<std::cell::RefCell<Option<gpui::WeakEntity<Workspace>>>> =
            Default::default();
        let native_target = workspace_target.clone();
        let native_quit_owner = quit_session.clone();
        let native_quit_supported = cx.on_native_quit_requested(move |cx| {
            route_workspace_quit(cx, &native_target, native_quit_owner.clone());
        });
        #[cfg(target_os = "macos")]
        assert!(
            native_quit_supported,
            "native termination guard is required"
        );
        #[cfg(not(target_os = "macos"))]
        let _ = native_quit_supported;
        let quit_handle = quit_session.clone();
        let quit_target = workspace_target.clone();
        cx.on_action(move |_: &Quit, cx| {
            route_workspace_quit(cx, &quit_target, quit_handle.clone())
        });
        cx.on_action(|_: &Save, cx| route_workspace_shortcut(cx, true));
        cx.on_action(|_: &CloseTab, cx| {
            if !about::dismiss_active(cx) {
                route_workspace_shortcut(cx, false);
            }
        });
        let close_handle = quit_session.clone();
        let close_target = workspace_target.clone();
        cx.on_action(move |_: &CloseWindow, cx| {
            if !about::dismiss_active(cx) {
                route_workspace_quit(cx, &close_target, close_handle.clone());
            }
        });
        cx.on_action(|_: &MinimizeWindow, cx| {
            if let Some(handle) = cx.active_window() {
                let _ = handle.update(cx, |_, window, _| window.minimize_window());
            }
        });
        cx.on_action(|_: &ZoomWindow, cx| {
            if let Some(handle) = cx.active_window() {
                let _ = handle.update(cx, |_, window, _| window.zoom_window());
            }
        });
        cx.on_action(|_: &ToggleFullscreen, cx| {
            if let Some(handle) = cx.active_window() {
                let _ = handle.update(cx, |_, window, _| window.toggle_fullscreen());
            }
        });
        cx.on_action(|_: &ShowAbout, cx| about::show(cx));
        cx.on_action(|_: &CopyBuildInfo, cx| {
            cx.write_to_clipboard(gpui::ClipboardItem::new_string(
                build_info::clipboard_payload(),
            ));
        });
        cx.bind_keys(workspace_key_bindings());
        cx.set_menus(vec![
            Menu {
                name: "Track Native".into(),
                items: vec![
                    MenuItem::action("About Track Native", ShowAbout),
                    MenuItem::action("Copy Build Info", CopyBuildInfo),
                    MenuItem::action("Quit", Quit),
                ],
            },
            Menu {
                name: "File".into(),
                items: vec![
                    MenuItem::action("Save", Save),
                    MenuItem::action("Close Tab", CloseTab),
                ],
            },
            Menu {
                name: "Edit".into(),
                items: vec![MenuItem::submenu(Menu {
                    name: "Find".into(),
                    items: vec![
                        MenuItem::action("Find…", NativeFind),
                        MenuItem::action("Find Next", NativeFindNext),
                        MenuItem::action("Find Previous", NativeFindPrevious),
                    ],
                })],
            },
            Menu {
                name: "View".into(),
                items: vec![
                    MenuItem::action("Back", HistoryBack),
                    MenuItem::action("Forward", HistoryForward),
                    MenuItem::action("Reload", NativeReload),
                ],
            },
            Menu {
                name: "Window".into(),
                items: vec![
                    MenuItem::action("Close Window", CloseWindow),
                    MenuItem::action("Minimize", MinimizeWindow),
                    MenuItem::action("Zoom", ZoomWindow),
                    MenuItem::action("Toggle Full Screen", ToggleFullscreen),
                ],
            },
        ]);
        let bounds = Bounds::centered(None, size(px(1200.), px(800.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                titlebar: if cfg!(target_os = "macos") {
                    Some(gpui::TitlebarOptions {
                        title: None,
                        appears_transparent: true,
                        traffic_light_position: None,
                    })
                } else {
                    None
                },
                window_min_size: Some(size(px(680.), px(480.))),
                is_movable: true,
                is_resizable: true,
                is_minimizable: true,
                ..Default::default()
            },
            |window, cx| {
                // Refuse startup if the isolated AppKit adaptation fails rather
                // than silently launching with different visible native controls.
                native_window::configure(window)
                    .expect("could not configure this window's native controls safely");
                let close_owner = quit_session.clone();
                let close_target = workspace_target.clone();
                window.on_window_should_close(cx, move |_, cx| {
                    route_workspace_quit(cx, &close_target, close_owner.clone());
                    false // Keep the window available to report shutdown failures.
                });
                let workspace = cx.new(|cx| {
                    let backend_is_fixture = backend.is_none() && live_config.is_none();
                    let mut session_ui = workspace_session::SessionUi::new(session_options);
                    if developer_draft_recovery {
                        session_ui.enable_recovery();
                    }
                    let mut workspace = Workspace {
                        shell: shell::ShellState::default(),
                        editing_ui: workspace_edit::EditingUi::new(developer_fixture),
                        tabs: tabs::Tabs::default(),
                        history: navigation::History::default(),
                        tag_page: None,
                        session_ui,
                        event_awareness: Default::default(),
                        refresh: Default::default(),
                        follow: Default::default(),
                        reader_images: Default::default(),
                        reader_pdf: None,
                        reader_math: None,
                        math_reference_cache: Default::default(),
                        pdf_enlargement: None,
                        selected: (!snapshot.notes.is_empty()).then_some(0),
                        snapshot,
                        rendered_rows: 0,
                        source,
                        backend,
                        content: Vec::new(),
                        current_note: None,
                        current_note_request: None,
                        read_error: None,
                        partial_read: false,
                        current_related: Default::default(),
                        status: String::new(),
                        list_status: String::new(),
                        request: 0,
                        live_reader_pending: None,
                        graph_request: 0,
                        graph_view: cx.new(|_| {
                            graph_view::GraphView::new(if backend_is_fixture {
                                graph::fixture(100)
                            } else {
                                graph::Graph::default()
                            })
                        }),
                        show_graph: false,
                        full_graph: workspace_graph::FullGraph::default(),
                        previews: workspace_preview::PreviewUi::default(),
                        subscriptions: Vec::new(),
                        live: None,
                        live_requested: live_config.is_some(),
                        accepted_storage_generation: None,
                        live_notes: Vec::new(),
                        search_results: Vec::new(),
                        search: cx.new(search_input::SearchInput::new),
                        query_epoch: Default::default(),
                        quit_session: quit_session.clone(),
                    };
                    workspace.subscriptions.push(cx.subscribe(
                        &workspace.graph_view,
                        |this: &mut Workspace, _, event: &graph_view::OpenNote, cx| {
                            if this.full_graph_active() {
                                this.open_full_graph_node(event.0, cx);
                                return;
                            }
                            if this.live.is_some() {
                                this.open_live_note(event.0, cx);
                                return;
                            }
                            if let Some(index) = this
                                .snapshot
                                .notes
                                .iter()
                                .position(|note| note.note_id == event.0)
                            {
                                this.select_from_ui(index, cx);
                            } else {
                                this.status =
                                    "Selected node is not in the current note list".into();
                                cx.notify();
                            }
                        },
                    ));
                    workspace.subscriptions.push(cx.subscribe(
                        &workspace.graph_view,
                        |this: &mut Workspace, _, event: &graph_view::PreviewIntent, cx| {
                            this.graph_preview_intent(event, cx);
                        },
                    ));
                    workspace.subscriptions.push(cx.subscribe(
                        &workspace.search,
                        |this: &mut Workspace, _, event: &search_input::SearchChanged, cx| {
                            this.search_live(event.0.clone(), cx);
                            cx.notify();
                        },
                    ));
                    if let Some(config) = live_config {
                        workspace.start_live(config, cx);
                    } else {
                        workspace.load_notes(cx);
                    }
                    workspace
                });
                *workspace_target.borrow_mut() = Some(workspace.downgrade());
                workspace
            },
        )
        .expect("open GPUI window");
        cx.activate(true);
    });
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn shortcuts_resolve_with_focused_editor_without_changing_quit() {
        let keymap = gpui::Keymap::new(workspace_key_bindings());
        for context in ["TrackSourceEditor", "TrackSearch composing", ""] {
            let contexts = [gpui::KeyContext::parse(context).unwrap()];
            for (key, action) in [
                ("cmd-s", Box::new(Save) as Box<dyn gpui::Action>),
                ("cmd-w", Box::new(CloseTab)),
                ("cmd-q", Box::new(Quit)),
            ] {
                let (matches, pending) =
                    keymap.bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts);
                assert!(!pending);
                assert_eq!(matches.len(), 1);
                assert!(matches[0].action().partial_eq(action.as_ref()));
            }
        }
    }

    #[test]
    fn history_bracket_bindings_are_workspace_actions_not_reader_local_aliases() {
        let keymap = gpui::Keymap::new(workspace_key_bindings());
        for context in ["", "TrackSourceEditor", "TrackSearch composing"] {
            let contexts = [gpui::KeyContext::parse(context).unwrap()];
            for (key, action) in [
                ("cmd-[", Box::new(HistoryBack) as Box<dyn gpui::Action>),
                ("cmd-]", Box::new(HistoryForward)),
            ] {
                let (bindings, pending) =
                    keymap.bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts);
                assert!(!pending);
                assert_eq!(bindings.len(), 1);
                assert!(bindings[0].action().partial_eq(action.as_ref()));
            }
            for key in ["[", "]", "cmd-shift-[", "ctrl-]"] {
                let (bindings, _) =
                    keymap.bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts);
                assert!(bindings.is_empty());
            }
        }
    }

    #[test]
    fn shortcut_route_declines_unknown_or_absent_window() {
        let workspace = gpui::WindowHandle::<Workspace>::new(1_u64.into());
        let other = gpui::WindowHandle::<gpui::Empty>::new(2_u64.into());
        assert!(shortcut_workspace(workspace.into()).is_some());
        assert!(shortcut_workspace(other.into()).is_none());
        assert!(
            None::<gpui::AnyWindowHandle>
                .and_then(shortcut_workspace)
                .is_none()
        );
    }

    #[test]
    fn recovery_requires_both_verified_fixture_bindings_and_owned_server() {
        let root = std::path::Path::new("/verified/fixture");
        let other = std::path::Path::new("/verified/other");
        assert!(validate_recovery_binding(true, true, Some(root), Some(root)).is_ok());
        for (edit, session) in [
            (None, None),
            (Some(root), None),
            (None, Some(root)),
            (Some(root), Some(other)),
        ] {
            assert!(validate_recovery_binding(true, true, edit, session).is_err());
        }
        assert!(validate_recovery_binding(true, false, Some(root), Some(root)).is_err());
        assert!(validate_recovery_binding(false, false, None, None).is_ok());
    }

    #[test]
    fn requested_live_mode_never_falls_back_to_cli_before_or_after_startup_failure() {
        assert!(live_mode_waiting(true, false));
        assert!(!live_mode_waiting(true, true));
        assert!(!live_mode_waiting(false, false));
    }
    #[test]
    fn accepts_cli_snapshot_and_partial_read() {
        let s: Snapshot = serde_json::from_str(r#"{"notes":[{"note_id":1785024017000,"title":"日本語","path":"/ignored","file_kind":"note"}],"read_state":{"partial":true,"issues":[]}}"#).unwrap();
        assert_eq!(s.notes[0].title, "日本語");
        assert!(s.notes[0].tags.is_empty());
        assert!(s.read_state.unwrap().partial);
    }
    #[test]
    fn empty_snapshot_is_valid() {
        assert!(
            serde_json::from_str::<Snapshot>(r#"{"notes":[]}"#)
                .unwrap()
                .notes
                .is_empty()
        );
    }
    #[test]
    fn invalid_snapshot_is_rejected() {
        assert!(serde_json::from_str::<Snapshot>(r#"{"notes":[{"title":"missing id"}]}"#).is_err());
    }
    #[test]
    fn fixture_has_unique_ids() {
        let s = fixture(10_000);
        assert_eq!(s.notes.len(), 10_000);
        assert_eq!(s.notes[9999].note_id, 9999);
    }
}

#[cfg(test)]
mod native_reload_binding_tests {
    use super::*;
    #[test]
    fn command_r_has_exactly_one_native_workspace_action() {
        let keymap = gpui::Keymap::new(workspace_key_bindings());
        for context in ["", "TrackSourceEditor", "TrackSearch composing"] {
            let contexts = [gpui::KeyContext::parse(context).unwrap()];
            let (matches, pending) =
                keymap.bindings_for_input(&[gpui::Keystroke::parse("cmd-r").unwrap()], &contexts);
            assert!(!pending);
            assert_eq!(matches.len(), 1);
            assert!(matches[0].action().partial_eq(&NativeReload));
            for key in ["r", "ctrl-r", "cmd-shift-r"] {
                let (matches, _) =
                    keymap.bindings_for_input(&[gpui::Keystroke::parse(key).unwrap()], &contexts);
                assert!(matches.is_empty());
            }
        }
    }
}
