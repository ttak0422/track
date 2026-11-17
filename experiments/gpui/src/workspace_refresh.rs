//! Session-only, current accepted note refresh. No navigation or write action.
use super::*;
use crate::workspace_live::partial;
use std::{
    sync::atomic::{AtomicBool, Ordering},
    time::Instant,
};
static BUSY: AtomicBool = AtomicBool::new(false);
struct Permit;
impl Drop for Permit {
    fn drop(&mut self) {
        BUSY.store(false, Ordering::Release);
    }
}
#[derive(Clone, PartialEq, Eq)]
struct Owner {
    source: shell::LinkSource,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
}
pub(crate) struct Ui {
    model: live_refresh::Model<Owner>,
    clock: Instant,
    notice: Option<String>,
}
impl Default for Ui {
    fn default() -> Self {
        Self {
            model: Default::default(),
            clock: Instant::now(),
            notice: None,
        }
    }
}
fn signal(session: Option<&live::Session>) -> live_refresh::Signal {
    session
        .map(|s| {
            let e = s.events();
            live_refresh::Signal {
                change: e.change,
                data: e.data,
                gaps: e.gaps,
                connected: e.connected,
            }
        })
        .unwrap_or_default()
}
fn unchanged(
    old: &live::NoteBody,
    related: &std::collections::BTreeMap<String, serde_json::Value>,
    new: &live::NoteResponse,
) -> bool {
    old == &new.note && related == &new.related_and_diagnostics
}
#[derive(Debug, PartialEq, Eq)]
enum DependencyUpdate {
    Unchanged,
    Defer,
    Replace,
}
fn dependency_update(
    old: Option<&crate::included_notes::Response>,
    new: Option<&crate::included_notes::Response>,
    rendered: bool,
    measured_top: bool,
) -> DependencyUpdate {
    if old == new {
        DependencyUpdate::Unchanged
    } else if rendered && !measured_top {
        DependencyUpdate::Defer
    } else {
        DependencyUpdate::Replace
    }
}
fn validate_dependencies(response: &crate::included_notes::Response) -> Result<(), live::Error> {
    if response.entries.iter().any(|entry| {
        entry.error.as_ref().is_some_and(|error| !error.is_empty()) || !entry.bad_options.is_empty()
    }) {
        return Err(live::Error::Shape(
            "Incomplete rendered dependencies; previous snapshot retained".into(),
        ));
    }
    markdown::included_preview(response)
        .map(|_| ())
        .map_err(live::Error::Shape)
}
fn same_body_inputs(old: &live::NoteBody, new: &live::NoteBody) -> bool {
    old.body == new.body && old.note.additional.get("tasks") == new.note.additional.get("tasks")
}
// The note-local graph is an aside, not a route. Its visibility alone must
// not block note refresh; full-graph routes and graph gestures still do.
fn graph_refresh_allowed(full_graph: bool, _aside_graph: bool, gesture: bool) -> bool {
    !full_graph && !gesture
}
/// The bounded read phase shared by the UI worker and loopback transport tests.
/// Display ownership and interaction checks still run on the UI thread before acceptance.
pub(crate) fn read_snapshot(
    client: &live::ReadClient,
    key: &live::NoteKey,
    launch_vault: &str,
    expected_storage: u64,
    dependency_tasks: Option<Result<std::sync::Arc<Vec<live::NoteTask>>, String>>,
) -> Result<(live::NoteResponse, Option<workspace_includes::Snapshot>), live::Error> {
    let before = client.storage()?;
    if before.phase != "ready" || before.generation != expected_storage {
        return Err(live::Error::Stale);
    }
    let result = client.note_for_refresh(key, launch_vault)?;
    let dependencies = if let Some(tasks) = dependency_tasks {
        let dependencies = client.included_notes(&result.note.note.vault, &result.note.body)?;
        validate_dependencies(&dependencies)?;
        Some(workspace_includes::Snapshot::build(
            dependencies,
            &result.note.body,
            tasks,
        ))
    } else {
        None
    };
    let after = client.storage()?;
    if after.phase != "ready" || after.generation != expected_storage {
        return Err(live::Error::Stale);
    }
    Ok((result, dependencies))
}
impl Workspace {
    fn refresh_owner(&self) -> Option<Owner> {
        let note = self.current_note.as_ref()?;
        let session = self.live.as_ref()?;
        if self.shell.home
            || self.shell.accepted_title_request != Some(self.request)
            || self.current_note_request.as_ref() != Some(&note.note.key())
        {
            return None;
        }
        let tabs::TabKey::Note { vault, note_id } = &self.tabs.active()?.key else {
            return None;
        };
        if vault != &note.note.vault || *note_id != note.note.note_id {
            return None;
        }
        let registry = self.follow_registry()?;
        let storage = self.accepted_storage_generation?;
        Some(Owner {
            source: shell::LinkSource {
                note: note.note.key(),
                etag: note.etag.clone(),
                request: self.request,
                launch_generation: session.client().launch_generation,
                storage_generation: storage,
            },
            scope: self.working_vault_scope().into(),
            scope_revision: self.shell.scope_revision,
            registry,
        })
    }
    fn refresh_safe(&self, window: &Window, cx: &Context<Self>) -> bool {
        self.refresh_blocker(window, cx).is_none()
    }
    fn refresh_blocker(&self, window: &Window, cx: &Context<Self>) -> Option<&'static str> {
        let Some(owner) = self.refresh_owner() else {
            return Some("accepted note ownership unavailable");
        };
        let Some(session) = self.live.as_ref() else {
            return Some("live session unavailable");
        };
        let vault = &owner.source.note.vault;
        let canonical = if vault.is_empty() {
            &owner.registry.0
        } else {
            vault
        };
        // Diagnose the same fail-closed gates without exposing note or vault data.
        let checks = [
            (!session.events().unavailable, "event stream unavailable"),
            (window.is_window_active(), "window inactive"),
            (session.failure().is_none(), "session failure"),
            (session.client().is_alive(), "owned server stopped"),
            (
                session.initial_storage.as_ref().is_some_and(|s| {
                    s.phase == "ready" && s.generation == owner.source.storage_generation
                }),
                "storage not ready or generation changed",
            ),
            (
                owner.registry.1.contains(canonical),
                "vault registry mismatch",
            ),
            (
                self.session_ui.permits(vault),
                "session restoration does not permit this vault",
            ),
            (
                self.editing_ui.plain_reader_mode(),
                "editor not in idle Preview mode",
            ),
            (
                self.editing_ui.follow_safe(),
                "editor or recovery interaction active",
            ),
            (!self.follow.controller.enabled(), "Editor Follow enabled"),
            (!self.quit_session.is_closing(), "session closing"),
            (
                self.quit_session.failure().is_none(),
                "session shutdown failure",
            ),
            (self.live_reader_pending.is_none(), "note read pending"),
            (self.read_error.is_none(), "note read error retained"),
            (
                graph_refresh_allowed(
                    self.full_graph_active(),
                    self.show_graph,
                    self.previews.model.geometry_active(),
                ),
                "full graph route or graph gesture active",
            ),
            (
                self.refresh_reader_idle(),
                "source selection or navigation pending",
            ),
            (!self.reader_hover_active(), "source hover action active"),
            (!self.recent_open(), "recent notes open"),
            (!self.creation_open(), "note creation open"),
            (!self.search.read(cx).is_composing(), "search IME composing"),
            (
                !self.shell.calendar_zone.is_composing(cx),
                "calendar IME composing",
            ),
            (!self.shell.find.open, "source Find open"),
            (
                !self.shell.find.is_composing(cx),
                "source Find IME composing",
            ),
            (
                !self.session_ui.recovery_enabled()
                    || self.session_ui.recovery_status()
                        == workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Empty,
                        ),
                "draft recovery not empty and ready",
            ),
        ];
        if let Some((_, reason)) = checks.into_iter().find(|(allowed, _)| !allowed) {
            return Some(reason);
        }
        if self.includes_preview_active() {
            self.refresh_rendered_blocker(cx)
        } else if !self.follow_safe(cx) {
            Some("source reader overlay or interaction active")
        } else {
            None
        }
    }
    pub(crate) fn poll_live_refresh(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .live
            .as_ref()
            .is_some_and(|session| session.events().unavailable)
        {
            if self.refresh.model.enabled {
                self.refresh.model.disable();
                cx.notify();
            }
            return;
        }
        if !self.refresh.model.enabled {
            return;
        }
        let now = self
            .refresh
            .clock
            .elapsed()
            .as_millis()
            .min(u128::from(u64::MAX)) as u64;
        let owner = self.refresh_owner();
        let safe = self.refresh_safe(window, cx);
        self.refresh
            .model
            .observe(owner, signal(self.live.as_ref()), now, safe);
        let Some(ticket) = self.refresh.model.claim(now) else {
            return;
        };
        if BUSY
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .is_err()
        {
            self.refresh
                .model
                .complete(&ticket, Some(&ticket.owner), true, now);
            self.refresh.notice =
                Some("Automatic refresh worker busy. Use Retry when it finishes.".into());
            cx.notify();
            return;
        }
        let permit = Permit;
        let client = self.live.as_ref().unwrap().client();
        let before_note = self.current_note.as_ref().unwrap().clone();
        let before_related = self.current_related.clone();
        let before_dependencies = self.refresh_dependencies().cloned();
        let dependency_tasks = before_dependencies.as_ref().map(|_| self.accepted_tasks());
        let mode = self.shell.rendered_reading.generation();
        let interaction = (
            self.refresh_reader_interaction(),
            self.refresh_rendered_interaction(),
        );
        let worker_ticket = ticket.clone();
        let Some(handle) = cx.active_window().and_then(shortcut_workspace) else {
            drop(permit);
            self.refresh.model.complete(&ticket, None, false, now);
            return;
        };
        self.refresh.notice =
            Some("Checking the current note; accepted text remains visible.".into());
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            read_snapshot(
                &client,
                &worker_ticket.owner.source.note,
                &worker_ticket.owner.registry.0,
                worker_ticket.owner.source.storage_generation,
                dependency_tasks,
            )
        });
        cx.spawn(async move|_,cx|{
            let result=job.await;
            let _=cx.update(|app|{let active=app.active_window()==Some(handle.into());let _=handle.update(app,|this,window,cx|{
                let now=this.refresh.clock.elapsed().as_millis().min(u128::from(u64::MAX)) as u64;
                let owner=this.refresh_owner();
                let safe=active&&this.refresh_safe(window,cx)&&this.shell.rendered_reading.generation()==mode
                    && (this.refresh_reader_interaction(),this.refresh_rendered_interaction())==interaction
                    && this.current_note.as_ref()==Some(&before_note)&&this.current_related==before_related
                    && this.refresh_dependencies() == before_dependencies.as_ref();
                if !this.refresh.model.complete(&ticket,owner.as_ref(),safe,now){
                    this.refresh.notice=Some("Automatic refresh deferred after an interaction or source change.".into());cx.notify();return;
                }
                match result {
                    Err(e)=>this.refresh.notice=Some(format!("Refresh failed; previous accepted snapshot retained: {e}. Use Retry.")),
                    Ok((response, dependencies))=>{
                        if response.note.note.key()!=ticket.owner.source.note || response.note.note.file_kind!=before_note.note.file_kind
                            || response.note.read_only || partial(&response.related_and_diagnostics){
                            this.refresh.notice=Some("Incomplete check. Return to Source, then View > Reload.".into());
                        }else if unchanged(&before_note,&before_related,&response){
                            match dependency_update(before_dependencies.as_ref(), dependencies.as_ref().map(|snapshot| &snapshot.response), this.includes_preview_active(), this.refresh_rendered_top()) {
                                DependencyUpdate::Unchanged => this.refresh.notice=Some("Current note checked; no accepted content changed.".into()),
                                DependencyUpdate::Defer => this.refresh.notice=Some("Rendered dependencies changed. Return to the top and Retry current-note check.".into()),
                                DependencyUpdate::Replace => if let Some(dependencies) = dependencies {
                                    this.accept_refresh_dependencies(dependencies, cx);
                                    this.refresh.notice=Some("Current note's rendered dependencies refreshed.".into());
                                },
                            }
                        }else{
                            let same=same_body_inputs(&before_note,&response.note);
                            let top=if this.includes_preview_active(){this.refresh_rendered_top()}else{this.refresh_reader_top(window)};
                            if !live_refresh::may_replace(same,this.includes_preview_active(),top) {this.refresh.notice=Some("Changed. Return to Source, then View > Reload.".into());}
                            else{this.accept_live_refresh(response,same,cx);if let Some(new)=this.refresh_owner(){this.refresh.model.accepted_owner(&ticket.owner,new);}this.refresh.notice=Some("Current note refreshed. Rendered content, if enabled, must be rebuilt for this accepted source.".into());}
                        }
                    }
                }
                cx.notify();
            });});
        }).detach();
        cx.notify();
    }
    fn accept_live_refresh(
        &mut self,
        response: live::NoteResponse,
        same: bool,
        cx: &mut Context<Self>,
    ) {
        let key = response.note.note.key();
        let tab = tabs::TabKey::Note {
            vault: key.vault.clone(),
            note_id: key.note_id,
        };
        self.pause_reader_for_overlay();
        self.request = self.request.wrapping_add(1);
        self.graph_request = self.graph_request.wrapping_add(1);
        self.query_epoch.fetch_add(1, Ordering::SeqCst);
        self.accept_refresh_content(
            tab.clone(),
            &response.note.body,
            response.note.etag.clone(),
            same,
        );
        if !same {
            self.content = markdown::parse_accepted_reader(
                &response.note.body,
                response.note.note.additional.get("tasks"),
            );
        }
        if let Some(i) = self
            .selected
            .filter(|&i| self.live_notes.get(i).is_some_and(|n| n.key() == key))
        {
            self.live_notes[i] = response.note.note.clone();
            if let Some(n) = self.snapshot.notes.get_mut(i) {
                n.title = response.note.note.title.clone();
                n.tags = response.note.note.tags.clone();
                n.path = response.note.note.path.clone().into();
                n.file_kind = response.note.note.file_kind.clone();
            }
        }
        self.tabs
            .update_title(&tab, response.note.note.title.clone());
        self.current_note = Some(response.note);
        self.current_related = response.related_and_diagnostics;
        self.shell.accepted_title_request = Some(self.request);
        self.current_note_request = Some(key);
        self.partial_read = false;
        self.reset_stale_includes();
        cx.notify();
    }
    pub(crate) fn live_refresh_controls(&self, cx: &mut Context<Self>) -> gpui::Div {
        let Some(owner) = self.refresh_owner() else {
            return div().h(px(48.));
        };
        if self
            .live
            .as_ref()
            .is_some_and(|session| session.events().unavailable)
        {
            return div().h(px(48.)).text_size(px(11.)).child(
                "Automatic refresh is unavailable in this session. Reload for current data.",
            );
        }
        let generation = self.refresh.model.generation();
        let enabled = self.refresh.model.enabled;
        let retry_owner = owner.clone();
        let mut controls = div()
            .h(px(24.))
            .flex_shrink_0()
            .flex()
            .gap_2()
            .overflow_hidden()
            .whitespace_nowrap()
            .text_size(px(11.))
            .child(
                div()
                    .id("toggle-current-note-refresh")
                    .cursor_pointer()
                    .child(if enabled {
                        "Stop current-note auto refresh"
                    } else {
                        "Auto refresh current note (session)"
                    })
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if this.refresh.model.generation() != generation
                            || this.refresh_owner().as_ref() != Some(&owner)
                            || !window.is_window_active()
                        {
                            return;
                        }
                        if enabled {
                            this.refresh.model.disable();
                            this.refresh.notice =
                                Some("Automatic current-note refresh stopped.".into());
                        } else if this.refresh_safe(window, cx) {
                            let now = this.refresh.clock.elapsed().as_millis() as u64;
                            this.refresh.model.enable(
                                owner.clone(),
                                signal(this.live.as_ref()),
                                now,
                            );
                            this.refresh.notice = Some(
                                "Automatic current-note refresh enabled for this session.".into(),
                            );
                        } else if let Some(reason) = this.refresh_blocker(window, cx) {
                            this.refresh.notice =
                                Some(format!("Automatic refresh paused: {reason}."));
                        }
                        cx.notify();
                    })),
            );
        if enabled {
            controls = controls.child(
                div()
                    .id("retry-current-note-refresh")
                    .cursor_pointer()
                    .child("Retry current-note check")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if this.refresh.model.generation() == generation
                            && this.refresh_owner().as_ref() == Some(&retry_owner)
                            && this.refresh_safe(window, cx)
                        {
                            let now = this.refresh.clock.elapsed().as_millis() as u64;
                            this.refresh.model.retry(now);
                            cx.notify();
                        }
                    })),
            );
        }
        div()
            .h(px(48.))
            .flex()
            .flex_col()
            .overflow_hidden()
            .child(controls)
            .child(
                div()
                    .h(px(24.))
                    .flex_shrink_0()
                    .overflow_hidden()
                    .whitespace_nowrap()
                    .text_size(px(11.))
                    .child(self.refresh.notice.clone().unwrap_or_default()),
            )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::Value;
    fn wire() -> Value {
        serde_json::json!({"note":{"note_id":7,"title":"日本語","file_kind":"note","path":"note/7.md","vault":"foreign","body":"hello","etag":"v1","tags":[],"days":[],"flags":[],"props":{},"tasks":{"items":[]}},"links":[]})
    }
    fn response(v: Value) -> live::NoteResponse {
        serde_json::from_value(v).unwrap()
    }
    #[test]
    fn note_graph_aside_never_blocks_but_full_route_and_gesture_do() {
        for aside in [false, true] {
            assert!(graph_refresh_allowed(false, aside, false));
            assert!(!graph_refresh_allowed(true, aside, false));
            assert!(!graph_refresh_allowed(false, aside, true));
            assert!(!graph_refresh_allowed(true, aside, true));
        }
    }
    fn dependencies(markdown: &str) -> crate::included_notes::Response {
        crate::included_notes::Response {
            markdown: markdown.into(),
            entries: vec![],
        }
    }
    #[test]
    fn unchanged_dependencies_preserve_projection_even_below_top() {
        let old = dependencies("query result A");
        let same = old.clone();
        assert_eq!(
            dependency_update(Some(&old), Some(&same), true, false),
            DependencyUpdate::Unchanged
        );
    }
    #[test]
    fn query_only_change_requires_top_but_source_discards_hidden_cache() {
        let old = dependencies("query result A");
        let new = dependencies("query result B");
        assert_eq!(
            dependency_update(Some(&old), Some(&new), true, false),
            DependencyUpdate::Defer
        );
        assert_eq!(
            dependency_update(Some(&old), Some(&new), true, true),
            DependencyUpdate::Replace
        );
        assert_eq!(
            dependency_update(Some(&old), Some(&new), false, false),
            DependencyUpdate::Replace
        );
    }
    #[test]
    fn include_metadata_and_excerpt_changes_are_not_host_noops() {
        let old = crate::included_notes::decode(serde_json::json!({
            "markdown": "![[target]]", "includes": [{"line":0,"source_line":0,"caption":"target","note_id":42,"kind":"note","etag":"v1","lines":["old"]}]
        }), "").unwrap();
        let mut new = old.clone();
        new.entries[0].excerpt = "new".into();
        assert_eq!(
            dependency_update(Some(&old), Some(&new), true, true),
            DependencyUpdate::Replace
        );
        new = old.clone();
        new.entries[0].title = Some("renamed".into());
        assert_eq!(
            dependency_update(Some(&old), Some(&new), true, true),
            DependencyUpdate::Replace
        );
        new.entries[0].error = Some("partial".into());
        assert!(validate_dependencies(&new).is_err());
        new.entries[0].error = None;
        new.entries[0].bad_options.push("unsupported".into());
        assert!(validate_dependencies(&new).is_err());
        assert!(validate_dependencies(&dependencies(&"x".repeat(16 * 1024 * 1024 + 1))).is_err());
    }
    #[test]
    fn all_accepted_fields_participate_in_noop() {
        let a = response(wire());
        assert!(unchanged(&a.note, &a.related_and_diagnostics, &a));
        for (k, v) in [
            ("title", serde_json::json!("changed")),
            ("path", serde_json::json!("note/other.md")),
            ("tags", serde_json::json!(["tag"])),
            ("days", serde_json::json!(["2026-11-16"])),
            ("flags", serde_json::json!(["flag"])),
            ("props", serde_json::json!({"x":1})),
            ("tasks", serde_json::json!({"items":[1]})),
            ("etag", serde_json::json!("v2")),
            ("read_only", serde_json::json!(true)),
            ("body", serde_json::json!("other")),
        ] {
            let mut b = wire();
            b["note"][k] = v;
            assert!(
                !unchanged(&a.note, &a.related_and_diagnostics, &response(b)),
                "{k}"
            );
        }
        let mut b = wire();
        b["links"] = serde_json::json!([1]);
        assert!(!unchanged(
            &a.note,
            &a.related_and_diagnostics,
            &response(b)
        ));
    }
    #[test]
    fn tasks_are_display_inputs_even_when_body_equal() {
        let a = response(wire());
        let mut b = wire();
        b["note"]["tasks"] = serde_json::json!({"items":[1]});
        assert!(!same_body_inputs(&a.note, &response(b).note));
        let mut b = wire();
        b["note"]["title"] = serde_json::json!("new");
        assert!(same_body_inputs(&a.note, &response(b).note));
    }
}
