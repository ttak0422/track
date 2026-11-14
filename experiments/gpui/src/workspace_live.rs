//! UI integration for the owned read-only server. Requests retain the session and
//! publish only if their navigation generation is still current.
use super::*;

/// The accepted route is visible even if its GET fails. Admission is checked
/// before either success or error can touch the reader/history.
fn accept_note_route(
    tabs: &mut tabs::Tabs,
    history: &mut navigation::History,
    key: &live::NoteKey,
) {
    tabs.open_note(
        key.vault.clone(),
        key.note_id,
        format!("Note {}", key.note_id),
    );
    history.record(tabs.active().map(|t| t.key.clone()));
}
fn note_read_is_current(
    request: u64,
    current: u64,
    key: &live::NoteKey,
    tabs: &tabs::Tabs,
) -> bool {
    request == current
        && tabs.active().is_some_and(|t| {
            t.key
                == tabs::TabKey::Note {
                    vault: key.vault.clone(),
                    note_id: key.note_id,
                }
        })
}

/// A startup result may attach its owned server, but must not replace a newer route.
#[derive(Clone)]
struct StartupNavigation {
    request: u64,
    graph_request: u64,
    tabs: tabs::Tabs,
    home: bool,
}
impl StartupNavigation {
    fn accepts(&self, current: &Self, closing: bool) -> bool {
        !closing
            && self.request == current.request
            && self.graph_request == current.graph_request
            && self.tabs == current.tabs
            && self.home == current.home
    }
}

impl Workspace {
    fn watch_live_lifecycle(&mut self, cx: &mut Context<Self>) {
        cx.spawn(async move |this, cx| {
            let mut previous = None;
            loop {
                cx.background_executor()
                    .timer(std::time::Duration::from_millis(250))
                    .await;
                if this
                    .update(cx, |this, cx| {
                        let warning = this.session_ui.warning.clone();
                        this.session_ui.poll_warning();
                        if warning != this.session_ui.warning {
                            cx.notify();
                        }
                        let failure = this.quit_session.failure();
                        if failure != previous {
                            previous = failure.clone();
                            if let Some(error) = failure {
                                this.editor_environment_changed("Owned server failed.");
                                this.request += 1;
                                this.graph_request += 1;
                                this.invalidate_full_graph(cx);
                                this.query_epoch
                                    .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                                this.partial_read = true;
                                this.accepted_storage_generation = None;
                                this.status = format!("Owned server failure: {error}");
                                this.read_error = Some(this.status.clone());
                                cx.notify();
                            }
                        }
                    })
                    .is_err()
                {
                    break;
                }
            }
        })
        .detach();
    }

    /// A newer epoch invalidates all previously displayed body/graph data, not just
    /// requests whose before/after probes straddle the transition.
    fn observe_storage(&mut self, state: &live::Storage, cx: &mut Context<Self>) {
        if epoch_changed(self.accepted_storage_generation, state.generation) {
            self.editor_environment_changed("Storage generation changed.");
            self.request += 1;
            self.graph_request += 1;
            self.invalidate_full_graph(cx);
            self.query_epoch
                .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            self.content.clear();
            self.current_note = None;
            self.current_note_request = None;
            self.current_related.clear();
            self.search_results.clear();
            self.show_graph = false;
            self.graph_view
                .update(cx, |view, cx| view.set_graph(graph::Graph::default(), cx));
            self.partial_read = true;
        }
        self.accepted_storage_generation = Some(state.generation);
        if let Some(session) = &mut self.live {
            session.initial_storage = Some(state.clone());
        }
    }

    fn startup_navigation(&self) -> StartupNavigation {
        StartupNavigation {
            request: self.request,
            graph_request: self.graph_request,
            tabs: self.tabs.clone(),
            home: self.shell.home,
        }
    }

    pub fn start_live(&mut self, config: live::LaunchConfig, cx: &mut Context<Self>) {
        self.previews.model.invalidate_session();
        self.previews.cancel_wake();
        self.status = "Starting owned loopback server…".into();
        if let Err(error) = self.quit_session.begin_start() {
            self.read_error = Some(error.to_string());
            cx.notify();
            return;
        }
        self.watch_live_lifecycle(cx);
        let startup_navigation = self.startup_navigation();
        let owner = self.quit_session.clone();
        let mut session_ui = std::mem::take(&mut self.session_ui);
        self.session_ui.safe_home = session_ui.safe_home;
        let job = cx.background_executor().spawn(async move {
            let mut session = owner.start(config)?;
            let deadline = std::time::Instant::now() + std::time::Duration::from_secs(15);
            // Partial is also a startup state: publishing that first list used to
            // leave it incomplete until a manual Reload. Wait before exposing the
            // interactive session, never reload underneath navigation or a draft.
            while startup_readiness_pending(
                session.initial_storage.as_ref().map(|s| s.phase.as_str()),
                std::time::Instant::now() < deadline,
                owner.is_closing() || owner.failure().is_some(),
            ) {
                std::thread::sleep(std::time::Duration::from_millis(100));
                match session.client().storage() {
                    Ok(state) => session.initial_storage = Some(state),
                    Err(error) => {
                        session.storage_error = Some(error.to_string());
                        break;
                    }
                }
            }
            let tabs = session_ui.restore(shell::session_registry(&session.initial_vaults));
            Ok::<_, live::Error>((session, session_ui, tabs))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.quit_session.is_closing() {
                    return;
                }
                match result {
                    Ok((session, session_ui, tabs)) => {
                        this.list_status = format!(
                            "Storage: {:?}; {}",
                            session.initial_storage,
                            session.storage_error.clone().unwrap_or_default()
                        );
                        let apply_startup_route = startup_navigation
                            .accepts(&this.startup_navigation(), this.quit_session.is_closing());
                        this.session_ui = session_ui;
                        let restored_history =
                            this.session_ui.take_restored_history(apply_startup_route);
                        if apply_startup_route {
                            if let Some(tabs) = tabs {
                                if let Some(history) = restored_history {
                                    this.history = history;
                                }
                                this.tabs = tabs;
                            }
                            this.live = Some(session);
                            this.load_live_notes(cx);
                        } else {
                            // Keep accepted Graph/Home/tab navigation. A later explicit
                            // Reload may fetch the list; startup performs no route reset/GET.
                            this.accepted_storage_generation = session
                                .initial_storage
                                .as_ref()
                                .map(|state| state.generation);
                            this.live = Some(session);
                            this.session_ui.sync(&this.tabs, &this.history);
                            this.status =
                                "Owned server ready; current view retained. Reload for fresh data."
                                    .into();
                        }
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
    }

    pub fn load_live_notes(&mut self, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::Reload, cx) {
            return;
        }
        let Some(session) = self.live.clone() else {
            return;
        };
        let reload_route = self.history.reload_route(&self.tabs);
        self.request += 1;
        let request = self.request;
        self.status = "Reading server notes…".into();
        let expected_epoch = self.accepted_storage_generation;
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let state = client.storage();
            let mut notes = client.notes("");
            let after = client.storage();
            if !matches!((&state, &after), (Ok(a), Ok(b)) if a.generation == b.generation) {
                notes = Err(live::Error::Stale);
            }
            (notes, after, session.failure())
        });
        cx.spawn(async move |this, cx| {
            let (result, storage, failure) = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                if !epoch_compatible(
                    expected_epoch,
                    this.accepted_storage_generation,
                    storage.as_ref().ok().map(|s| s.generation),
                ) {
                    return;
                }
                if let Ok(state) = &storage {
                    this.observe_storage(state, cx);
                }
                this.list_status = match &storage {
                    Ok(state) => format!(
                        "Storage: {} · {} verified{}",
                        state.phase,
                        state.verified,
                        state
                            .reason
                            .as_ref()
                            .map(|s| format!(" · {s}"))
                            .unwrap_or_default()
                    ),
                    Err(error) => error.to_string(),
                };
                if let Some(error) = failure {
                    this.list_status.push_str(&format!(" · {error}"));
                }
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        this.list_status
                            .push_str(&diagnostics(&response.diagnostics));
                        this.partial_read = partial(&response.diagnostics);
                        this.live_notes = response.notes;
                        this.snapshot.notes = this
                            .live_notes
                            .iter()
                            .map(|n| Note {
                                note_id: n.note_id,
                                title: n.title.clone(),
                                tags: n.tags.clone(),
                                path: n.path.clone().into(),
                                file_kind: n.file_kind.clone(),
                            })
                            .collect();
                        this.selected = None;
                        this.content.clear();
                        this.status.clear();
                        if this.restore_reload_route(reload_route.clone(), cx) {
                            // Refresh the accepted route, not a new first/Home-note visit.
                        } else if this.session_ui.safe_home {
                            this.shell.home = true;
                        } else if let Some(index) = this.configured_home_index() {
                            this.shell.home = false;
                            this.select(index, cx);
                        } else if !this.snapshot.notes.is_empty() {
                            this.select(0, cx);
                        }
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

    pub fn search_live(&mut self, query: String, cx: &mut Context<Self>) {
        // Old-query rows must not remain selectable during the debounce/request.
        self.search_results.clear();
        self.search.update(cx, |search, _| search.reset_result());
        cx.notify();
        let epoch = self
            .query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst)
            + 1;
        if query.trim().is_empty() {
            self.search_results.clear();
            cx.notify();
            return;
        }
        let Some(session) = self.live.clone() else {
            return;
        };
        let current = self.query_epoch.clone();
        self.status = "Searching all served vaults…".into();
        let expected_epoch = self.accepted_storage_generation;
        let job = cx.background_executor().spawn(async move {
            std::thread::sleep(std::time::Duration::from_millis(200));
            if current.load(std::sync::atomic::Ordering::SeqCst) != epoch {
                return None;
            }
            let client = session.client();
            let before = client.storage();
            let result = client.search(&query, "");
            let after = client.storage();
            if !matches!((&before,&after), (Ok(a),Ok(b)) if a.generation == b.generation) {
                return Some((Err(live::Error::Stale), after));
            }
            Some((result, after))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.query_epoch.load(std::sync::atomic::Ordering::SeqCst) != epoch {
                    return;
                }
                let Some((result, storage)) = result else {
                    return;
                };
                if !epoch_compatible(
                    expected_epoch,
                    this.accepted_storage_generation,
                    storage.as_ref().ok().map(|s| s.generation),
                ) {
                    return;
                }
                if let Ok(state) = &storage {
                    this.observe_storage(state, cx);
                }
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        this.partial_read = partial(&response.diagnostics);
                        this.search.update(cx, |search, _| search.reset_result());
                        this.search_results = response.results;
                        this.list_status = format!(
                            "Workspace-wide search{}",
                            diagnostics(&response.diagnostics)
                        );
                        this.status.clear();
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

    pub fn select_search_result(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(note) = self.search_results.get(index).cloned() else {
            return;
        };
        if self.guard_transition(workspace_edit::Transition::Note(note.key()), cx) {
            return;
        }
        let index = if let Some(index) = self.live_notes.iter().position(|n| n.key() == note.key())
        {
            index
        } else {
            self.snapshot.notes.push(Note {
                note_id: note.note_id,
                title: note.title.clone(),
                tags: note.tags.clone(),
                path: note.path.clone().into(),
                file_kind: note.file_kind.clone(),
            });
            self.live_notes.push(note);
            self.live_notes.len() - 1
        };
        self.select_from_ui(index, cx);
    }

    pub fn load_live_graph(&mut self, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::Graph, cx) {
            return;
        }
        let Some(session) = self.live.clone() else {
            return;
        };
        let Some(note) = &self.current_note else {
            return;
        };
        let key = note.note.key();
        let expected_epoch = self.accepted_storage_generation;
        self.graph_request += 1;
        let request = self.graph_request;
        self.show_graph = true;
        self.graph_view
            .update(cx, |view, cx| view.set_graph(graph::Graph::default(), cx));
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let before = client.storage();
            let result = client.local_graph(&key);
            let after = client.storage();
            if !matches!((&before,&after),(Ok(a),Ok(b)) if a.generation==b.generation && expected_epoch == Some(b.generation)) {
                return (Err(live::Error::Stale), after);
            }
            (result, after)
        });
        cx.spawn(async move |this, cx| {
            let (result, storage) = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.graph_request != request {
                    return;
                }
                if let Ok(state) = &storage {
                    this.observe_storage(state, cx);
                }
                match result {
                    Ok((graph, fields)) => {
                        this.partial_read |= partial(&fields);
                        this.graph_view
                            .update(cx, |view, cx| view.set_graph(graph, cx));
                    }
                    Err(error) => {
                        this.read_error = Some(format!("Graph: {error}"));
                    }
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }

    pub fn open_live_note(&mut self, id: i64, cx: &mut Context<Self>) {
        let Some(current) = &self.current_note else {
            return;
        };
        self.open_live_key(
            live::NoteKey {
                vault: current.note.vault.clone(),
                note_id: id,
            },
            cx,
        );
    }

    pub fn open_live_key(&mut self, key: live::NoteKey, cx: &mut Context<Self>) {
        if !self.session_ui.permits(&key.vault) {
            self.read_error = Some("Vault is absent from the validated session registry; no fallback request was sent.".into());
            cx.notify();
            return;
        }
        if self.guard_transition(workspace_edit::Transition::Note(key.clone()), cx) {
            return;
        }
        self.shell.home = false;
        self.shell.search_open = false;
        self.search.update(cx, |search, cx| search.clear(cx));
        self.shell.tab_menu = false;
        let Some(session) = self.live.clone() else {
            return;
        };
        if let Some(index) = self.live_notes.iter().position(|n| n.key() == key) {
            self.select(index, cx);
            return;
        }
        // Accept before I/O; completion fills this route without adding a visit.
        accept_note_route(&mut self.tabs, &mut self.history, &key);
        self.session_ui.sync(&self.tabs, &self.history);
        self.selected = None;
        self.content.clear();
        self.current_note = None;
        self.current_note_request = None;
        self.current_related.clear();
        self.show_graph = false;
        self.read_error = None;
        self.request += 1;
        self.graph_request += 1;
        cx.notify(); // Publish the accepted destination before its asynchronous read.
        let request = self.request;
        let destination = key.clone();
        let expected_epoch = self.accepted_storage_generation;
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let before = client.storage();
            let result = client.note(&key);
            let after = client.storage();
            if !matches!((&before,&after),(Ok(a),Ok(b)) if a.generation == b.generation) {
                return (Err(live::Error::Stale), after);
            }
            (result, after)
        });
        cx.spawn(async move |this, cx| {
            let (result, storage) = job.await;
            let _ = this.update(cx, |this, cx| {
                if !note_read_is_current(request, this.request, &destination, &this.tabs) {
                    return;
                }
                if !epoch_compatible(
                    expected_epoch,
                    this.accepted_storage_generation,
                    storage.as_ref().ok().map(|s| s.generation),
                ) {
                    return;
                }
                if let Ok(state) = &storage {
                    this.observe_storage(state, cx);
                }
                match result {
                    Ok(response) => {
                        let note = response.note.note;
                        let fragment_destination = note.key();
                        this.snapshot.notes.push(Note {
                            note_id: note.note_id,
                            title: note.title.clone(),
                            tags: note.tags.clone(),
                            path: note.path.clone().into(),
                            file_kind: note.file_kind.clone(),
                        });
                        this.live_notes.push(note);
                        this.select(this.live_notes.len() - 1, cx);
                        this.advance_fragment_request(request, this.request, &fragment_destination);
                    }
                    Err(error) => this.read_error = Some(error.to_string()),
                }
                cx.notify();
            });
        })
        .detach();
    }

    pub fn select_live(&mut self, index: usize, cx: &mut Context<Self>) {
        if let Some(note) = self.live_notes.get(index) {
            if self.guard_transition(workspace_edit::Transition::Note(note.key()), cx) {
                return;
            }
        }
        let Some(session) = self.live.clone() else {
            return;
        };
        let Some(note) = self.live_notes.get(index) else {
            return;
        };
        let key = note.key();
        let accepted_request = key.clone();
        let request = self.request;
        self.status = "Reading raw body (read-only)…".into();
        let expected_epoch = self.accepted_storage_generation;
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let before = client.storage();
            let mut result = client.note(&key);
            let after = client.storage();
            if !matches!((&before,&after), (Ok(a),Ok(b)) if a.generation == b.generation) {
                result = Err(live::Error::Stale);
            }
            (result, after, session.failure())
        });
        cx.spawn(async move |this, cx| {
            let (result, storage, failure) = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                if !epoch_compatible(
                    expected_epoch,
                    this.accepted_storage_generation,
                    storage.as_ref().ok().map(|s| s.generation),
                ) {
                    return;
                }
                if let Ok(state) = &storage {
                    this.observe_storage(state, cx);
                }
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        this.content = markdown::parse(&response.note.body);
                        this.partial_read =
                            response.note.read_only || partial(&response.related_and_diagnostics);
                        this.current_note = Some(response.note.clone());
                        this.current_note_request = Some(accepted_request.clone());
                        this.accept_fragment_destination(request, cx);
                        this.load_live_graph(cx);
                        this.current_related = response.related_and_diagnostics.clone();
                        this.status = format!(
                            "Read only: {}; diagnostics: {:?}; server failure: {failure:?}",
                            response.note.read_only,
                            diagnostics(&response.related_and_diagnostics)
                        );
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

/// Only known transient phases are retried. Errors, unknown/terminal phases,
/// shutdown and the existing deadline leave the normal explicit Reload path.
fn startup_readiness_pending(phase: Option<&str>, within_deadline: bool, stopping: bool) -> bool {
    within_deadline && !stopping && matches!(phase, Some("initializing" | "partial"))
}

fn epoch_compatible(started: Option<u64>, current: Option<u64>, response: Option<u64>) -> bool {
    current == started || response.is_some() && current == response
}
fn epoch_changed(accepted: Option<u64>, incoming: u64) -> bool {
    accepted.is_some_and(|previous| previous != incoming)
}

fn diagnostics(fields: &std::collections::BTreeMap<String, serde_json::Value>) -> String {
    fields
        .iter()
        .filter(|(key, value)| {
            matches!(
                key.as_str(),
                "read_state" | "partial" | "unavailable" | "storage_state"
            ) && !value.as_array().is_some_and(|v| v.is_empty())
        })
        .map(|(key, value)| format!(" · {key}: {value}"))
        .collect()
}

pub(super) fn partial(fields: &std::collections::BTreeMap<String, serde_json::Value>) -> bool {
    fields
        .get("read_state")
        .and_then(|v| v.get("partial"))
        .and_then(|v| v.as_bool())
        .unwrap_or(false)
        || ["partial", "unavailable"].iter().any(|key| {
            fields
                .get(*key)
                .and_then(|v| v.as_array())
                .is_some_and(|v| !v.is_empty())
        })
        || fields
            .get("storage_state")
            .and_then(|v| v.as_str())
            .is_some_and(|s| s != "ready")
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn startup_waits_through_partial_until_ready() {
        let phases = ["initializing", "partial", "partial", "ready"];
        let observed: Vec<_> = phases
            .into_iter()
            .take_while(|phase| startup_readiness_pending(Some(phase), true, false))
            .collect();
        assert_eq!(observed, ["initializing", "partial", "partial"]);
    }

    #[test]
    fn startup_readiness_wait_is_bounded_and_does_not_retry_terminal_states() {
        for phase in [Some("initializing"), Some("partial")] {
            assert!(!startup_readiness_pending(phase, false, false));
            assert!(!startup_readiness_pending(phase, true, true));
        }
        for phase in [
            None,
            Some("ready"),
            Some("failed"),
            Some("unavailable"),
            Some("unknown"),
        ] {
            assert!(!startup_readiness_pending(phase, true, false));
        }
    }

    #[test]
    fn epoch_changes_invalidate_previous_results_without_assuming_numeric_order() {
        assert!(!epoch_changed(None, 9));
        assert!(!epoch_changed(Some(9), 9));
        assert!(epoch_changed(Some(9), 2));
        assert!(epoch_compatible(Some(9), Some(9), Some(2)));
        assert!(!epoch_compatible(Some(9), Some(2), Some(9)));
        assert!(epoch_compatible(Some(9), Some(2), Some(2)));
        assert!(!epoch_compatible(None, Some(2), Some(9)));
    }
    #[test]
    fn preserves_partial_and_unavailable_states() {
        let fields =
            serde_json::from_value(serde_json::json!({"read_state":{"partial":true}})).unwrap();
        assert!(partial(&fields));
        let fields =
            serde_json::from_value(serde_json::json!({"unavailable":[{"name":"other"}]})).unwrap();
        assert!(partial(&fields));
        let fields = serde_json::from_value(
            serde_json::json!({"partial":[],"unavailable":[],"storage_state":"ready"}),
        )
        .unwrap();
        assert!(!partial(&fields));
    }
}

#[cfg(test)]
mod startup_navigation_tests {
    use super::*;
    fn initial() -> StartupNavigation {
        StartupNavigation {
            request: 0,
            graph_request: 0,
            tabs: tabs::Tabs::default(),
            home: true,
        }
    }
    #[test]
    fn untouched_startup_accepts_but_quit_never_applies_restoration() {
        let before = initial();
        assert!(before.accepts(&initial(), false));
        assert!(!before.accepts(&initial(), true));
    }
    #[test]
    fn accepted_graph_home_and_tab_changes_reject_late_restore() {
        let before = initial();
        let mut graph = initial();
        graph.tabs.open_graph();
        graph.home = false;
        graph.request += 1;
        graph.graph_request += 1;
        assert!(!before.accepts(&graph, false));
        let mut home = initial();
        home.tabs.open_home();
        home.request += 1;
        assert!(!before.accepts(&home, false));
        let mut closed = initial();
        closed.tabs.open_graph();
        closed.tabs.close(&tabs::TabKey::Graph);
        closed.request += 1; // A route returning to the same tab set is still newer.
        assert!(!before.accepts(&closed, false));
        let mut tab_only = initial();
        tab_only.tabs.open_note("work", 7, "Accepted");
        assert!(!before.accepts(&tab_only, false));
        let mut route_only = initial();
        route_only.home = false;
        assert!(!before.accepts(&route_only, false));
        let mut graph_only = initial();
        graph_only.graph_request += 1;
        assert!(!before.accepts(&graph_only, false));
    }
}

#[cfg(test)]
mod history_read_tests {
    use super::*;
    #[test]
    fn failed_read_keeps_destination_and_late_completion_cannot_branch() {
        let mut tabs = tabs::Tabs::default();
        let mut history = navigation::History::default();
        let key = live::NoteKey {
            vault: "one".into(),
            note_id: 7,
        };
        accept_note_route(&mut tabs, &mut history, &key);
        assert!(note_read_is_current(1, 1, &key, &tabs));
        // An error only sets read_error, leaving the accepted route/cursor intact.
        assert_eq!(
            tabs.active().unwrap().key,
            tabs::TabKey::Note {
                vault: "one".into(),
                note_id: 7
            }
        );
        assert!(history.peek(false).is_some());
        assert!(history.commit(&history.peek(false).unwrap()));
        tabs.open_home();
        let before = history.clone();
        assert!(!note_read_is_current(1, 2, &key, &tabs));
        assert_eq!(history, before);
        assert!(history.peek(true).is_some());
        // Even returning to the identical note cannot admit its earlier response.
        assert!(history.commit(&history.peek(true).unwrap()));
        accept_note_route(&mut tabs, &mut history, &key);
        assert!(!note_read_is_current(1, 3, &key, &tabs));
        let before = history.clone();
        assert!(note_read_is_current(3, 3, &key, &tabs));
        tabs.open_note("one", 7, "Resolved");
        history.record(tabs.active().map(|t| t.key.clone()));
        assert_eq!(history, before);
    }
}
