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
                    Err(error) => this.status = error.to_string(),
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
                        this.list_status
                            .push_str(&diagnostics(&response.diagnostics));
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
                        if !this.snapshot.notes.is_empty() {
                            this.select(0, cx);
                        }
                    }
                    Err(error) => this.status = error.to_string(),
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
            self.load_live_notes(cx);
            return;
        }
        let Some(session) = self.live.clone() else {
            return;
        };
        self.request += 1;
        self.graph_request += 1;
        self.show_graph = false;
        let request = self.request;
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
                if this.request != request {
                    return;
                }
                let Some(result) = result else {
                    return;
                };
                match result {
                    Ok(response) => {
                        this.live_notes = response.results;
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
                        this.list_status = format!(
                            "Workspace-wide search; diagnostics: {:?}",
                            response.diagnostics
                        );
                        this.selected = None;
                        this.content.clear();
                        this.status.clear();
                    }
                    Err(error) => this.status = error.to_string(),
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
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
                        this.content = markdown::parse(&response.note.body);
                        this.status = format!(
                            "Read only: {}; diagnostics: {:?}; server: {failure:?}",
                            response.note.read_only,
                            diagnostics(&response.related_and_diagnostics)
                        );
                    }
                    Err(error) => this.status = error.to_string(),
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
