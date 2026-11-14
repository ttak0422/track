//! Explicit read-only editor navigation with one guarded proportional reveal.
use super::*;
use std::time::Instant;

#[derive(Default)]
pub(crate) struct FollowUi {
    pub controller: editor_follow::Controller,
    pub routing: bool,
    load: Option<FollowLoad>,
    reveal: Option<FollowReveal>,
    reveal_serial: u64,
    layout_pending: bool,
    viewport: Option<gpui::Size<gpui::Pixels>>,
    scroll_status: &'static str,
}
#[derive(Clone)]
struct FollowReveal {
    ticket: editor_follow::Request,
    key: live::NoteKey,
    kind: String,
    request: u64,
    ratio: f64,
    attempts: u8,
}
#[derive(Clone)]
struct FollowLoad {
    request: u64,
    key: live::NoteKey,
    kind: String,
    identity: editor_follow::Identity,
    signal: u64,
    gaps: u64,
}

/// Empty publisher scope always means launch, never the UI working scope.
fn resolve_scope(scope: &str, launch: &str, names: &[String]) -> Option<String> {
    let name = if scope.is_empty() { launch } else { scope };
    names
        .iter()
        .find(|candidate| candidate.as_str() == name)
        .cloned()
}

fn reveal_source_current(
    expected: &(shell::LinkSource, tabs::TabKey),
    current: Option<&(shell::LinkSource, tabs::TabKey)>,
    interaction: u64,
    current_interaction: u64,
) -> bool {
    current == Some(expected) && interaction == current_interaction
}

/// Shared enable/poll/completion gate. Idle pinned cards are deliberately not a blocker.
#[derive(Default)]
struct FollowGate {
    edit_busy: bool,
    handoff: bool,
    rendered_preview: bool,
    graph: bool,
    find: bool,
    search: bool,
    menu: bool,
    diagnostics: bool,
    geometry: bool,
    composing: bool,
}
impl FollowGate {
    fn allowed(&self) -> bool {
        !(self.edit_busy
            || self.handoff
            || self.rendered_preview
            || self.graph
            || self.find
            || self.search
            || self.menu
            || self.diagnostics
            || self.geometry
            || self.composing)
    }
}

impl Workspace {
    fn follow_identity(&self) -> Option<editor_follow::Identity> {
        let session = self.live.as_ref()?;
        if !session.client().is_alive()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
        {
            return None;
        }
        let storage = self.accepted_storage_generation?;
        let (launch_name, names) = self.follow_registry()?;
        Some(editor_follow::Identity {
            launch: session.client().launch_generation,
            storage: Some(storage),
            registry: serde_json::to_string(&(launch_name, names)).ok()?,
            observer: session.client().launch_generation,
        })
    }
    pub(crate) fn follow_safe(&self, cx: &App) -> bool {
        FollowGate {
            edit_busy: !self.editing_ui.follow_safe(),
            handoff: self.handoff_active(),
            rendered_preview: self.includes_preview_active(),
            graph: self.full_graph_active(),
            find: self.shell.find.open,
            search: self.shell.search_open,
            menu: self.shell.tab_menu || self.shell.vault_menu,
            diagnostics: self.shell.diagnostics,
            geometry: self.previews.model.geometry_active(),
            composing: self.search.read(cx).is_composing(),
        }
        .allowed()
    }
    fn cancel_follow_load(&mut self) {
        self.cancel_follow_reveal();
        if let Some(load) = self.follow.load.take() {
            if self.request == load.request {
                self.request += 1;
            }
        }
    }
    pub(crate) fn stop_editor_follow(&mut self) {
        self.cancel_follow_load();
        self.follow.controller.stop();
    }
    pub(crate) fn pause_editor_follow(&mut self, reason: &'static str) {
        if self.follow.controller.enabled() {
            self.cancel_follow_load();
            self.follow.controller.pause(reason);
        }
    }
    fn toggle_editor_follow(&mut self, cx: &mut Context<Self>) {
        if self.follow.controller.enabled() {
            self.stop_editor_follow();
        } else if self.follow_safe(cx) {
            if let Some(identity) = self.follow_identity() {
                let events = self.live.as_ref().unwrap().events();
                self.follow.controller.enable(identity, events);
                self.poll_editor_follow(cx);
            }
        }
        cx.notify();
    }
    pub(crate) fn poll_editor_follow(&mut self, cx: &mut Context<Self>) {
        if !self.follow.controller.enabled() {
            return;
        }
        if !self.follow_safe(cx) {
            self.pause_editor_follow("Reader busy; enable again.");
            cx.notify();
            return;
        }
        let Some(identity) = self.follow_identity() else {
            self.pause_editor_follow("Session or registry unavailable; enable again.");
            cx.notify();
            return;
        };
        let session = self.live.as_ref().unwrap().clone();
        let events = session.events();
        if self
            .follow
            .load
            .as_ref()
            .is_some_and(|load| load.signal != events.follow)
        {
            self.cancel_follow_load();
        }
        self.follow.controller.observe(&identity, events);
        self.follow.controller.expire(Instant::now());
        if self.follow.controller.status() != editor_follow::Status::Following {
            self.cancel_follow_load();
        }
        let Some(ticket) = self.follow.controller.take_request(Instant::now()) else {
            cx.notify();
            return;
        };
        let job = cx
            .background_executor()
            .spawn(async move { session.client().follow() });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                // Refresh the signal before accepting a result, not just at the 250ms UI tick.
                let identity = this.follow_identity();
                if let Some(current) = identity.as_ref() {
                    if let Some(session) = this.live.as_ref() {
                        this.follow.controller.observe(current, session.events());
                    }
                } else {
                    this.pause_editor_follow("Session or registry changed; enable again.");
                }
                if !this.follow_safe(cx) {
                    this.pause_editor_follow("Reader busy; enable again.");
                }
                let current = identity.as_ref().unwrap_or(&ticket.identity);
                if let Some(state) =
                    this.follow
                        .controller
                        .finish(&ticket, current, result, Instant::now())
                {
                    this.apply_editor_follow(state, &ticket, cx);
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    fn apply_editor_follow(
        &mut self,
        state: editor_follow::FollowState,
        ticket: &editor_follow::Request,
        cx: &mut Context<Self>,
    ) {
        let Some(identity) = self.follow_identity() else {
            return;
        };
        let events = self.live.as_ref().unwrap().events();
        self.follow.controller.observe(&identity, events);
        if !self.follow.controller.is_current(ticket, &identity) {
            return;
        }
        if !self.follow_safe(cx) {
            self.pause_editor_follow("Reader busy; enable again.");
            return;
        }
        let Some((launch, names)) = self.follow_registry() else {
            return;
        };
        let Some(vault) = resolve_scope(&state.vault, &launch, &names) else {
            self.pause_editor_follow("Editor vault is not served.");
            return;
        };
        let key = live::NoteKey {
            vault,
            note_id: state.note_id,
        };
        let ratio = markdown::follow_scroll_ratio(state.top_line as u64, state.line_count as u64);
        // Repeated cursor notifications never refetch an already accepted saved body.
        if self.current_note.as_ref().is_some_and(|body| {
            body.note.note_id == key.note_id
                && body.note.file_kind == state.file_kind
                && resolve_scope(&body.note.vault, &launch, &names).as_ref() == Some(&key.vault)
        }) && !self.shell.home
        {
            self.queue_follow_reveal(ticket, key, state.file_kind, ratio);
            return;
        }
        if self
            .live_notes
            .iter()
            .any(|note| note.key() == key && note.file_kind != state.file_kind)
        {
            self.pause_editor_follow("Editor note kind does not match the indexed note.");
            return;
        }
        self.cancel_follow_load();
        self.follow.routing = true;
        self.open_live_key(key.clone(), cx);
        self.follow.routing = false;
        if self.read_error.is_some() {
            self.pause_editor_follow("Note could not be opened.");
            return;
        }
        self.queue_follow_reveal(ticket, key.clone(), state.file_kind.clone(), ratio);
        self.follow.load = Some(FollowLoad {
            request: self.request,
            key,
            kind: state.file_kind,
            identity,
            signal: ticket.signal,
            gaps: events.gaps,
        });
    }
    /// Shared raw-read acceptance hook; checks before any response body is published.
    pub(crate) fn accept_follow_read(&mut self, request: u64, note: &live::Note, cx: &App) -> bool {
        let Some(load) = self
            .follow
            .load
            .as_ref()
            .filter(|load| load.request == request)
            .cloned()
        else {
            return true;
        };
        let scope = self
            .follow_registry()
            .and_then(|(launch, names)| resolve_scope(&note.vault, &launch, &names));
        let current = self.follow.controller.status() == editor_follow::Status::Following
            && self.follow_safe(cx)
            && self.follow_identity().as_ref() == Some(&load.identity)
            && self.live.as_ref().is_some_and(|s| {
                let events = s.events();
                events.connected && events.follow == load.signal && events.gaps == load.gaps
            })
            && scope.as_ref() == Some(&load.key.vault)
            && note.note_id == load.key.note_id
            && note.file_kind == load.kind;
        if !current {
            self.pause_editor_follow(
                "Editor target changed or note identity did not match; enable again.",
            );
        }
        current
    }
    pub(crate) fn following_read(&self, request: u64) -> bool {
        self.follow
            .load
            .as_ref()
            .is_some_and(|load| load.request == request)
    }
    pub(crate) fn transfer_follow_read(&mut self, previous: u64) {
        if let Some(reveal) = self
            .follow
            .reveal
            .as_mut()
            .filter(|r| r.request == previous)
        {
            reveal.request = self.request;
        }
        if let Some(load) = self
            .follow
            .load
            .as_mut()
            .filter(|load| load.request == previous)
        {
            load.request = self.request;
        }
    }
    pub(crate) fn finish_follow_read(&mut self, request: u64) {
        if self.following_read(request) {
            self.follow.load = None;
        }
    }
    pub(crate) fn cancel_follow_reveal(&mut self) {
        if self.follow.layout_pending {
            self.cancel_follow_layout();
        }
        self.follow.reveal_serial = self
            .follow
            .reveal_serial
            .checked_add(1)
            .expect("follow reveal exhausted");
        self.follow.reveal = None;
        self.follow.layout_pending = false;
    }
    fn queue_follow_reveal(
        &mut self,
        ticket: &editor_follow::Request,
        key: live::NoteKey,
        kind: String,
        ratio: f64,
    ) {
        self.cancel_follow_reveal();
        self.follow.scroll_status = "";
        self.follow.reveal = Some(FollowReveal {
            ticket: ticket.clone(),
            key,
            kind,
            ratio,
            request: self.request,
            attempts: 0,
        });
    }
    pub(crate) fn schedule_follow_reveal(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let viewport = window.viewport_size();
        if self
            .follow
            .viewport
            .is_some_and(|previous| previous != viewport)
        {
            self.cancel_follow_reveal();
        }
        self.follow.viewport = Some(viewport);
        let Some(mut reveal) = self.follow.reveal.take() else {
            return;
        };
        let Some(identity) = self.follow_identity() else {
            return;
        };
        let events = self.live.as_ref().unwrap().events();
        self.follow.controller.observe(&identity, events);
        if !self.follow.controller.is_current(&reveal.ticket, &identity)
            || !self.follow_safe(cx)
            || reveal.request != self.request
        {
            return;
        }
        // A followed note may still be loading. Only that exact request may supply its body.
        if self.following_read(reveal.request) {
            self.follow.reveal = Some(reveal);
            return;
        }
        let source = self.follow_layout_source(cx);
        let anchors = self.follow_layout_anchors();
        let ready = source.as_ref().is_some_and(|(source, _)| {
            let scope = self
                .follow_registry()
                .and_then(|(launch, names)| resolve_scope(&source.note.vault, &launch, &names));
            scope.as_ref() == Some(&reveal.key.vault)
                && source.note.note_id == reveal.key.note_id
                && self
                    .current_note
                    .as_ref()
                    .is_some_and(|n| n.note.file_kind == reveal.kind)
        });
        if !ready
            || anchors
                .as_ref()
                .and_then(|a| a.capture(viewport, window.scale_factor()))
                .is_none()
        {
            reveal.attempts += 1;
            if reveal.attempts < 3 {
                self.follow.reveal = Some(reveal);
            } else {
                self.follow.scroll_status = "; scroll unavailable for current layout";
            }
            return;
        }
        let source = source.unwrap();
        let anchors = anchors.unwrap();
        let anchor_weak = std::rc::Rc::downgrade(&anchors);
        let serial = self.follow.reveal_serial;
        let interaction = self.follow_layout_interaction();
        let weak = cx.entity().downgrade();
        self.follow.layout_pending = anchors.request_after_layout(move |unchanged, window, cx| {
            let _ = weak.update(cx, |this, cx| {
                if serial != this.follow.reveal_serial {
                    return;
                }
                this.follow.layout_pending = false;
                let Some(anchors) = anchor_weak.upgrade() else {
                    return;
                };
                let Some(identity) = this.follow_identity() else {
                    return;
                };
                if let Some(session) = this.live.as_ref() {
                    this.follow.controller.observe(&identity, session.events());
                }
                this.follow.controller.expire(Instant::now());
                let valid = unchanged
                    && this.follow.controller.status() == editor_follow::Status::Following
                    && this.follow.controller.is_current(&reveal.ticket, &identity)
                    && this.follow_safe(cx)
                    && this.request == reveal.request
                    && reveal_source_current(
                        &source,
                        this.follow_layout_source(cx).as_ref(),
                        interaction,
                        this.follow_layout_interaction(),
                    )
                    && this
                        .current_note
                        .as_ref()
                        .is_some_and(|n| n.note.file_kind == reveal.kind)
                    && this
                        .follow_layout_anchors()
                        .as_ref()
                        .is_some_and(|current| std::rc::Rc::ptr_eq(current, &anchors));
                let applied = valid
                    && anchors
                        .capture(window.viewport_size(), window.scale_factor())
                        .is_some_and(|capture| {
                            anchors.apply_proportion(
                                reveal.ratio,
                                &capture,
                                window.viewport_size(),
                                window.scale_factor(),
                            )
                        });
                this.follow.scroll_status = if applied {
                    ""
                } else {
                    "; scroll canceled because reader or layout changed"
                };
                cx.notify();
            });
        });
    }
    pub(crate) fn render_follow_controls(&self, cx: &mut Context<Self>) -> gpui::Div {
        let enabled = self.follow.controller.enabled();
        let available = self.follow_safe(cx) && self.follow_identity().is_some();
        div()
            .px_3()
            .py_1()
            .flex()
            .gap_3()
            .text_size(px(11.))
            .child(
                div()
                    .id("editor-follow-toggle")
                    .cursor_pointer()
                    .child(if enabled {
                        "Stop following editor"
                    } else {
                        "Follow editor"
                    })
                    .when(enabled || available, |button| {
                        button.on_click(cx.listener(|this, _, _, cx| this.toggle_editor_follow(cx)))
                    }),
            )
            .child(format!(
                "{} · saved/indexed note · approximate proportional scroll{}",
                match self.follow.controller.status() {
                    editor_follow::Status::Off => "Off".to_owned(),
                    editor_follow::Status::Waiting => "Waiting for editor".to_owned(),
                    editor_follow::Status::Following => "Following".to_owned(),
                    editor_follow::Status::Paused(reason) => format!("Paused: {reason}"),
                },
                self.follow.scroll_status,
            ))
            .when(!enabled && !available, |row| {
                row.child("Requires an idle reader and verified owned session")
            })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn reveal_rejects_body_source_request_and_interaction_changes() {
        let source = (
            shell::LinkSource {
                note: live::NoteKey {
                    vault: "one".into(),
                    note_id: 42,
                },
                etag: "body-a".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            tabs::TabKey::Note {
                vault: "one".into(),
                note_id: 42,
            },
        );
        assert!(reveal_source_current(&source, Some(&source), 7, 7));
        assert!(!reveal_source_current(&source, None, 7, 7));
        assert!(!reveal_source_current(&source, Some(&source), 7, 8));
        for mutation in 0..6 {
            let mut current = source.clone();
            match mutation {
                0 => current.0.etag = "body-b".into(),
                1 => current.0.note.vault = "other".into(),
                2 => current.0.request += 1,
                3 => current.0.storage_generation += 1,
                4 => current.0.launch_generation += 1,
                _ => current.1 = tabs::TabKey::Home,
            }
            assert!(!reveal_source_current(&source, Some(&current), 7, 7));
        }
    }
    #[test]
    fn follow_gate_allows_idle_reader_and_blocks_each_active_interaction() {
        assert!(FollowGate::default().allowed());
        for gate in [
            FollowGate {
                edit_busy: true,
                ..Default::default()
            },
            FollowGate {
                handoff: true,
                ..Default::default()
            },
            FollowGate {
                rendered_preview: true,
                ..Default::default()
            },
            FollowGate {
                graph: true,
                ..Default::default()
            },
            FollowGate {
                find: true,
                ..Default::default()
            },
            FollowGate {
                search: true,
                ..Default::default()
            },
            FollowGate {
                menu: true,
                ..Default::default()
            },
            FollowGate {
                diagnostics: true,
                ..Default::default()
            },
            FollowGate {
                geometry: true,
                ..Default::default()
            },
            FollowGate {
                composing: true,
                ..Default::default()
            },
        ] {
            assert!(!gate.allowed());
        }
    }
    #[test]
    fn editor_scope_is_registry_bound_and_empty_means_launch() {
        let names = vec!["launch".into(), "other".into()];
        assert_eq!(
            resolve_scope("", "launch", &names).as_deref(),
            Some("launch")
        );
        assert_eq!(
            resolve_scope("other", "launch", &names).as_deref(),
            Some("other")
        );
        assert_eq!(resolve_scope("unknown", "launch", &names), None);
        assert_eq!(resolve_scope("", "missing", &names), None);
    }
}
