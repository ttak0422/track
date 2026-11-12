//! UI integration for the owned read-only server. Requests retain the session and
//! publish only if their navigation generation is still current.
use super::*;
impl Workspace {
    pub fn start_live(&mut self, config: live::LaunchConfig, cx: &mut Context<Self>) {
        self.status = "Starting owned loopback server…".into();
        let job = cx.background_executor().spawn(async move {
            let mut session = live::Session::start(config)?;
            let deadline = std::time::Instant::now() + std::time::Duration::from_secs(15);
            while session
                .initial_storage
                .as_ref()
                .is_some_and(|s| s.phase == "initializing")
                && std::time::Instant::now() < deadline
            {
                std::thread::sleep(std::time::Duration::from_millis(100));
                match session.client().storage() {
                    Ok(state) => session.initial_storage = Some(state),
                    Err(error) => {
                        session.storage_error = Some(error.to_string());
                        break;
                    }
                }
            }
            Ok::<_, live::Error>(session)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                match result {
                    Ok(session) => {
                        this.list_status = format!(
                            "Storage: {:?}; {}",
                            session.initial_storage,
                            session.storage_error.clone().unwrap_or_default()
                        );
                        *this.quit_session.lock().unwrap() = Some(session.clone());
                        this.live = Some(session);
                        this.load_live_notes(cx);
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
        let Some(session) = self.live.clone() else {
            return;
        };
        self.request += 1;
        let request = self.request;
        self.status = "Reading server notes…".into();
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
                        if let Some(index) = this.configured_home_index() {
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
                return Some(Err(live::Error::Stale));
            }
            Some(result)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.query_epoch.load(std::sync::atomic::Ordering::SeqCst) != epoch {
                    return;
                }
                let Some(result) = result else {
                    return;
                };
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        this.partial_read = partial(&response.diagnostics);
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
        self.select(index, cx);
    }

    pub fn load_live_graph(&mut self, cx: &mut Context<Self>) {
        let Some(session) = self.live.clone() else {
            return;
        };
        let Some(note) = &self.current_note else {
            return;
        };
        let key = note.note.key();
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
            if !matches!((&before,&after),(Ok(a),Ok(b)) if a.generation==b.generation) {
                return Err(live::Error::Stale);
            }
            result
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.graph_request != request {
                    return;
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
        let Some(session) = self.live.clone() else {
            return;
        };
        let Some(current) = &self.current_note else {
            return;
        };
        let key = live::NoteKey {
            vault: current.note.vault.clone(),
            note_id: id,
        };
        if let Some(index) = self.live_notes.iter().position(|n| n.key() == key) {
            self.select(index, cx);
            return;
        }
        self.request += 1;
        self.graph_request += 1;
        let request = self.request;
        let job = cx
            .background_executor()
            .spawn(async move { session.client().note(&key) });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                match result {
                    Ok(response) => {
                        let note = response.note.note;
                        this.snapshot.notes.push(Note {
                            note_id: note.note_id,
                            title: note.title.clone(),
                            tags: note.tags.clone(),
                            path: note.path.clone().into(),
                            file_kind: note.file_kind.clone(),
                        });
                        this.live_notes.push(note);
                        this.select(this.live_notes.len() - 1, cx);
                    }
                    Err(error) => this.read_error = Some(error.to_string()),
                }
                cx.notify();
            });
        })
        .detach();
    }

    pub fn select_live(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(session) = self.live.clone() else {
            return;
        };
        let Some(note) = self.live_notes.get(index) else {
            return;
        };
        let key = note.key();
        let request = self.request;
        self.status = "Reading raw body (read-only)…".into();
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let before = client.storage();
            let mut result = client.note(&key);
            let after = client.storage();
            if !matches!((&before,&after), (Ok(a),Ok(b)) if a.generation == b.generation) {
                result = Err(live::Error::Stale);
            }
            (result, session.failure())
        });
        cx.spawn(async move |this, cx| {
            let (result, failure) = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                match result {
                    Ok(response) => {
                        this.read_error = None;
                        this.content = markdown::parse(&response.note.body);
                        this.partial_read =
                            response.note.read_only || partial(&response.related_and_diagnostics);
                        this.current_note = Some(response.note.clone());
                        this.load_live_graph(cx);
                        this.current_related = response.related_and_diagnostics.clone();
                        this.status = format!(
                            "Read only: {}; diagnostics: {:?}; server: {failure:?}",
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

fn partial(fields: &std::collections::BTreeMap<String, serde_json::Value>) -> bool {
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
