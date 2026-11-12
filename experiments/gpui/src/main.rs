use gpui::{
    App, Application, Bounds, Context, KeyBinding, Menu, MenuItem, Window, WindowBounds,
    WindowOptions, actions, div, prelude::*, px, rgb, size, uniform_list,
};
mod backend;
#[cfg(test)]
mod editing;
#[cfg(test)]
mod editor_input;
mod graph;
mod graph_view;
mod live;
mod markdown;
mod native_window;
mod search_input;
mod shell;
mod tabs;
mod workspace_live;
use backend::{Backend, Note, Snapshot};
actions!(
    native_workspace,
    [
        Quit,
        CloseWindow,
        MinimizeWindow,
        ZoomWindow,
        ToggleFullscreen
    ]
);

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
    tabs: tabs::Tabs,
    snapshot: Snapshot,
    selected: Option<usize>,
    rendered_rows: usize,
    source: String,
    backend: Option<Backend>,
    content: Vec<markdown::Block>,
    current_note: Option<live::NoteBody>,
    read_error: Option<String>,
    partial_read: bool,
    current_related: std::collections::BTreeMap<String, serde_json::Value>,
    status: String,
    list_status: String,
    request: u64,
    graph_request: u64,
    graph_view: gpui::Entity<graph_view::GraphView>,
    show_graph: bool,
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
                        if !this.snapshot.notes.is_empty() {
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

    fn select(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(note) = self.snapshot.notes.get(index) else {
            return;
        };
        if !self.shell.home || self.configured_home_index() == Some(index) {
            let vault = self
                .live_notes
                .get(index)
                .filter(|_| self.live.is_some())
                .map(|note| note.vault.clone())
                .unwrap_or_default();
            self.tabs.open_note(vault, note.note_id, note.title.clone());
        }
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.graph_request += 1;
        self.show_graph = false;
        self.selected = Some(index);
        self.request += 1;
        self.content.clear();
        self.current_note = None;
        self.current_related.clear();
        if self.live.is_some() {
            self.select_live(index, cx);
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

fn request_quit(cx: &mut App, owner: std::sync::Arc<live::LifecycleHandle>) {
    let job = cx
        .background_executor()
        .spawn(async move { owner.shutdown() });
    cx.spawn(async move |cx| {
        if let Err(error) = job.await {
            eprintln!("Server shutdown incomplete: {error}");
            return; // Lifecycle polling publishes the error; never claim a completed quit.
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

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let _ = log::set_logger(&LOGGER);
    log::set_max_level(log::LevelFilter::Warn);
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.first().is_some_and(|a| a == "--help") {
        println!(
            "track-gpui-poc [notes.json]\ntrack-gpui-poc --vault /absolute/vault --track-bin /absolute/track [--config /absolute/config] [--cache /absolute/cache] [--server-port 19000]\nNo arguments: synthetic fixture. Vault mode is read-only; CLI may refresh derived caches."
        );
        return Ok(());
    }
    let mut live_config = None;
    let (snapshot, source, backend) = if args.first().is_some_and(|a| a.starts_with("--")) {
        let mut server_port = None;
        let mut vault = None;
        let mut executable = None;
        let mut config = None;
        let mut cache = None;
        let mut iter = args.iter();
        while let Some(flag) = iter.next() {
            let value = iter.next().ok_or("missing option value")?;
            match flag.as_str() {
                "--server-port" => server_port = Some(value.parse::<u16>()?),
                "--vault" => vault = Some(value.into()),
                "--track-bin" => executable = Some(value.into()),
                "--config" => config = Some(value.into()),
                "--cache" => cache = Some(value.into()),
                _ => return Err(format!("unknown option: {flag}").into()),
            }
        }
        if let Some(port) = server_port {
            let mut config_live = live::LaunchConfig::new(
                executable.clone().ok_or("--track-bin is required")?,
                live::Vault::Path(vault.clone().ok_or("--vault is required")?),
                port,
            );
            config_live.config = config.clone();
            config_live.cache = cache.clone();
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
        (
            fixture(0),
            if live_config.is_some() {
                "explicit vault / owned HTTP".into()
            } else {
                "explicit vault / CLI".into()
            },
            Some(backend),
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
    Application::new().run(move |cx: &mut App| {
        let quit_session = std::sync::Arc::new(live::LifecycleHandle::default());
        let quit_handle = quit_session.clone();
        cx.on_action(move |_: &Quit, cx| request_quit(cx, quit_handle.clone()));
        let close_handle = quit_session.clone();
        cx.on_action(move |_: &CloseWindow, cx| request_quit(cx, close_handle.clone()));
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
        cx.bind_keys([
            KeyBinding::new("cmd-q", Quit, None),
            KeyBinding::new("cmd-m", MinimizeWindow, None),
            KeyBinding::new("ctrl-cmd-f", ToggleFullscreen, None),
        ]);
        cx.set_menus(vec![
            Menu {
                name: "Track Native".into(),
                items: vec![MenuItem::action("Quit", Quit)],
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
                window.on_window_should_close(cx, move |_, cx| {
                    request_quit(cx, close_owner.clone());
                    false // Keep the window available to report shutdown failures.
                });
                cx.new(|cx| {
                    let backend_is_fixture = backend.is_none();
                    let mut workspace = Workspace {
                        shell: shell::ShellState::default(),
                        tabs: tabs::Tabs::default(),
                        selected: (!snapshot.notes.is_empty()).then_some(0),
                        snapshot,
                        rendered_rows: 0,
                        source,
                        backend,
                        content: Vec::new(),
                        current_note: None,
                        read_error: None,
                        partial_read: false,
                        current_related: Default::default(),
                        status: String::new(),
                        list_status: String::new(),
                        request: 0,
                        graph_request: 0,
                        graph_view: cx.new(|_| {
                            graph_view::GraphView::new(if backend_is_fixture {
                                graph::fixture(100)
                            } else {
                                graph::Graph::default()
                            })
                        }),
                        show_graph: false,
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
                                this.select(index, cx);
                            } else {
                                this.status =
                                    "Selected node is not in the current note list".into();
                                cx.notify();
                            }
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
                })
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
