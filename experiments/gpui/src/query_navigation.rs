//! Explicit host-query row navigation. Payloads are untrusted wiki resolver data.
use super::*;
use std::sync::atomic::{AtomicBool, Ordering};
static BUSY: AtomicBool = AtomicBool::new(false);
struct Permit;
impl Drop for Permit {
    fn drop(&mut self) {
        BUSY.store(false, Ordering::Release);
    }
}
#[derive(Default)]
pub(super) struct Navigation {
    pending: Option<u64>,
    notice: Option<String>,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Surface {
    Modal,
    Reader { block: usize, epoch: u64 },
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Ticket {
    surface: Surface,
    identity: Identity,
    serial: u64,
    epoch: u64,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
    key: (usize, usize),
    group: usize,
    row: usize,
    value: crate::query_view::Row,
    pages: std::collections::BTreeMap<(usize, usize, Option<usize>), usize>,
}
fn target(row: &crate::query_view::Row) -> Option<markdown::LinkTarget> {
    let raw = row
        .target
        .as_deref()
        .filter(|s| !s.trim().is_empty())
        .unwrap_or(&row.title)
        .trim();
    if raw.is_empty()
        || raw.len() > 4096
        || raw.chars().any(char::is_control)
        || raw.contains("://")
        || ["javascript:", "data:", "file:", "mailto:"]
            .iter()
            .any(|s| raw.to_ascii_lowercase().starts_with(s))
        || raw.contains(['[', ']', '|'])
    {
        return None;
    }
    let (key, fragment) = raw.split_once('#').map_or((raw, None), |(key, rest)| {
        (key.trim(), Some(format!("#{rest}")))
    });
    if key.is_empty() {
        return None;
    }
    Some(markdown::LinkTarget::Wiki {
        key: key.to_owned(),
        fragment,
        authored_label: true,
    })
}
fn verified_key(
    response: &live::Resolution,
    source: &shell::LinkSource,
    term: &str,
) -> Result<live::NoteKey, String> {
    if response.launch_generation != source.launch_generation
        || response.source_vault != source.note.vault
        || response.term != term
        || crate::shell::uncertain_resolution(&response.diagnostics)
    {
        return Err("Query resolution was not authoritative".into());
    }
    response
        .key()
        .filter(|k| k.note_id > 0)
        .ok_or_else(|| "Query target was not found; no note was created.".into())
}
fn host_row(
    state: &IncludesState,
    key: (usize, usize),
    group: usize,
    row: usize,
) -> Option<&crate::query_view::Row> {
    let snapshot = state.result.as_ref()?.as_ref().ok()?;
    if !matches!(
        snapshot.preview.as_ref().ok()?.items.get(key.0)?,
        markdown::PreviewItem::Host(_)
    ) {
        return None;
    }
    snapshot
        .queries
        .get(&key)?
        .as_ref()
        .ok()?
        .groups
        .get(group)?
        .rows
        .get(row)
}
impl Workspace {
    fn query_ticket(
        &self,
        surface: Surface,
        key: (usize, usize),
        group: usize,
        row: usize,
    ) -> Option<Ticket> {
        let state = self.shell.includes.as_ref()?;
        let identity = self.includes_identity()?;
        let surface_valid = match surface {
            Surface::Modal => state.preview_open,
            Surface::Reader { block, epoch } => self.query_inline_matches(block, epoch, key),
        };
        if !surface_valid
            || state.identity != identity
            || !self.includes_body_current(state)
            || self.shell.accepted_title_request != Some(self.request)
        {
            return None;
        }
        let value = host_row(state, key, group, row)?.clone();
        target(&value)?;
        Some(Ticket {
            surface,
            identity,
            serial: state.serial,
            epoch: state.preview_epoch,
            scope: self.working_vault_scope().to_owned(),
            scope_revision: self.shell.scope_revision,
            registry: self.follow_registry()?,
            key,
            group,
            row,
            value,
            pages: state.query_pages.clone(),
        })
    }
    fn query_ticket_current(&self, t: &Ticket, cx: &Context<Self>) -> bool {
        (match t.surface {
            Surface::Modal => self.query_control_gate(&t.identity, cx).allowed(),
            Surface::Reader { .. } => self.query_reader_available(cx),
        }) && self
            .query_ticket(t.surface.clone(), t.key, t.group, t.row)
            .as_ref()
            == Some(t)
            && !self.recent_open()
            && !self.reader_hierarchy_open()
            && !self.enlarged_image_open()
            && !self.creation_open()
            && !self.reader_selection_dragging()
            && !self.shell.find.is_composing(cx)
            && !self.shell.calendar_zone.is_composing(cx)
    }
    pub(super) fn query_navigation_notice(&self) -> Option<String> {
        self.shell.includes.as_ref()?.query_nav.notice.clone()
    }
    pub(super) fn query_navigation_control(
        &self,
        key: (usize, usize),
        group: usize,
        row: usize,
        cx: &Context<Self>,
    ) -> gpui::Div {
        self.query_surface_control(Surface::Modal, key, group, row, cx)
    }
    pub(super) fn query_reader_navigation_control(
        &self,
        block: usize,
        epoch: u64,
        key: (usize, usize),
        group: usize,
        row: usize,
        cx: &Context<Self>,
    ) -> gpui::Div {
        self.query_surface_control(Surface::Reader { block, epoch }, key, group, row, cx)
    }
    fn query_surface_control(
        &self,
        surface: Surface,
        key: (usize, usize),
        group: usize,
        row: usize,
        cx: &Context<Self>,
    ) -> gpui::Div {
        let Some(ticket) = self.query_ticket(surface, key, group, row) else {
            return div();
        };
        div().child(
            div()
                .id(gpui::SharedString::from(format!(
                    "query-open-{}-{}-{}-{group}-{row}",
                    ticket.epoch, key.0, key.1
                )))
                .text_size(px(12.))
                .cursor_pointer()
                .child("Open note")
                .on_click(
                    cx.listener(move |this, event: &gpui::ClickEvent, window, cx| {
                        // No modified/new-tab contract in this first slice.
                        if !crate::shell::plain_reader_click(event) {
                            cx.stop_propagation();
                            return;
                        }
                        this.activate_query_row(ticket.clone(), window, cx);
                        cx.stop_propagation();
                    }),
                ),
        )
    }
    fn activate_query_row(&mut self, ticket: Ticket, window: &mut Window, cx: &mut Context<Self>) {
        if !window.is_window_active() || !self.query_ticket_current(&ticket, cx) {
            return;
        }
        let Some(handle) = cx.active_window().and_then(shortcut_workspace) else {
            return;
        };
        let Some(target) = target(&ticket.value) else {
            return;
        };
        let Some(term) = markdown::resolve_term(&target) else {
            return;
        };
        let state = self.shell.includes.as_mut().unwrap();
        if BUSY
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .is_err()
        {
            state.query_nav.pending = None;
            state.query_nav.notice=Some("Previous query navigation canceled; its lookup is still finishing. Try again shortly.".into());
            cx.notify();
            return;
        }
        let permit = Permit;
        let token = SERIAL.fetch_add(1, Ordering::Relaxed);
        state.query_nav.pending = Some(token);
        state.query_nav.notice = Some("Resolving query note…".into());
        let client = self.live.as_ref().unwrap().client();
        let source = ticket.identity.source.clone();
        let lookup = term.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|_| "Query storage unavailable")?;
            if before.phase != "ready" || before.generation != source.storage_generation {
                return Err("Query storage changed".to_owned());
            }
            let response = client
                .resolve(&lookup, &source.note.vault)
                .map_err(|_| "Query target could not be resolved")?;
            let after = client.storage().map_err(|_| "Query storage unavailable")?;
            if after.phase != "ready" || after.generation != source.storage_generation {
                return Err("Query resolution was not authoritative".to_owned());
            }
            verified_key(&response, &source, &lookup)
        });
        cx.spawn(async move |_, cx| {
            let result = job.await;
            let _ = cx.update(|app| {
                let active = app.active_window() == Some(handle.into());
                let _ = handle.update(app, |this, window, cx| {
                    if !this
                        .shell
                        .includes
                        .as_ref()
                        .is_some_and(|s| s.query_nav.pending == Some(token))
                    {
                        return;
                    }
                    this.shell.includes.as_mut().unwrap().query_nav.pending = None;
                    if !active
                        || !window.is_window_active()
                        || !this.query_ticket_current(&ticket, cx)
                    {
                        this.shell.includes.as_mut().unwrap().query_nav.notice = Some(
                            "Query navigation canceled after the view changed; activate again."
                                .into(),
                        );
                        cx.notify();
                        return;
                    }
                    let key = match result {
                        Ok(key) => key,
                        Err(error) => {
                            this.shell.includes.as_mut().unwrap().query_nav.notice = Some(error);
                            cx.notify();
                            return;
                        }
                    };
                    let canonical = if key.vault.is_empty() {
                        &ticket.registry.0
                    } else {
                        &key.vault
                    };
                    if !ticket.registry.1.contains(canonical)
                        || !this.session_ui.permits(&key.vault)
                    {
                        this.shell.includes.as_mut().unwrap().query_nav.notice = Some(
                            "Query destination vault is unavailable; no fallback was used.".into(),
                        );
                        cx.notify();
                        return;
                    }
                    let intent = shell::InternalLinkIntent {
                        source: ticket.identity.source.note.clone(),
                        destination: key,
                        target,
                        launch_generation: ticket.identity.source.launch_generation,
                        storage_generation: ticket.identity.source.storage_generation,
                        allow_epoch_advance: false,
                    };
                    // Ask the existing transition guard before closing the accepted preview.
                    if this.guard_transition(
                        workspace_edit::Transition::InternalLink(intent.clone()),
                        cx,
                    ) {
                        return;
                    }
                    if ticket.surface == Surface::Modal {
                        this.close_includes_preview(&ticket.identity, ticket.serial, window, cx);
                    }
                    this.open_internal_link(intent, cx);
                });
            });
        })
        .detach();
        cx.notify();
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn row(title: &str, target: Option<&str>) -> crate::query_view::Row {
        crate::query_view::Row {
            title: title.into(),
            target: target.map(str::to_owned),
            cells: vec![],
            cover: None,
            icon: None,
        }
    }
    #[test]
    fn opaque_targets_and_title_fallback_are_not_id_guesses() {
        for value in ["42", "@42", "id:42", "work:id:42"] {
            assert_eq!(
                markdown::resolve_term(&target(&row("other", Some(value))).unwrap()).as_deref(),
                Some(value)
            );
        }
        assert_eq!(
            markdown::resolve_term(&target(&row("Title", None)).unwrap()).as_deref(),
            Some("Title")
        );
        assert_eq!(
            markdown::resolve_term(&target(&row("Title", Some(""))).unwrap()).as_deref(),
            Some("Title")
        );
        assert_eq!(
            markdown::resolve_term(&target(&row("Title", Some("id:invalid"))).unwrap()).as_deref(),
            Some("id:invalid")
        );
        assert_ne!(
            target(&row("same", Some("id:1"))),
            target(&row("same", Some("id:2")))
        );
    }
    #[test]
    fn hostile_explicit_target_never_falls_back() {
        for value in [
            "https://evil.invalid",
            "javascript:alert(1)",
            "file:///tmp/a",
            "x\ny",
            "[[injected]]",
        ] {
            assert!(target(&row("valid title", Some(value))).is_none());
        }
        assert!(target(&row("", None)).is_none());
    }
}

#[cfg(test)]
mod authority_tests {
    use super::*;
    fn identity() -> Identity {
        Identity {
            source: shell::LinkSource {
                note: live::NoteKey {
                    vault: "work".into(),
                    note_id: 1,
                },
                etag: "rev".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            route: live::NoteKey {
                vault: "work".into(),
                note_id: 1,
            },
        }
    }
    fn row() -> crate::query_view::Row {
        crate::query_view::Row {
            title: "Same".into(),
            target: Some("id:42".into()),
            cells: vec![],
            cover: None,
            icon: None,
        }
    }
    #[test]
    fn ticket_rejects_stale_snapshot_and_row_authority() {
        let base = Ticket {
            surface: Surface::Modal,
            identity: identity(),
            serial: 5,
            epoch: 6,
            scope: "work".into(),
            scope_revision: 7,
            registry: ("main".into(), vec!["work".into(), "main".into()]),
            key: (0, 0),
            group: 0,
            row: 0,
            value: row(),
            pages: Default::default(),
        };
        let mut reader = base.clone();
        reader.surface = Surface::Reader { block: 2, epoch: 1 };
        assert_ne!(base, reader);
        let mut hidden = reader.clone();
        hidden.surface = Surface::Reader { block: 2, epoch: 2 };
        assert_ne!(reader, hidden);
        hidden.surface = Surface::Reader { block: 3, epoch: 1 };
        assert_ne!(reader, hidden);
        for i in 0..10 {
            let mut stale = base.clone();
            match i {
                0 => stale.serial += 1,
                1 => stale.epoch += 1,
                2 => stale.identity.source.request += 1,
                3 => stale.identity.source.storage_generation += 1,
                4 => stale.scope_revision += 1,
                5 => stale.registry.1.clear(),
                6 => stale.row += 1,
                7 => stale.value.target = Some("id:99".into()),
                8 => {
                    stale.pages.insert((0, 0, None), 1);
                }
                _ => stale.scope = "other".into(),
            }
            assert_ne!(base, stale);
        }
    }
    #[test]
    fn resolver_result_preserves_destination_vault_and_never_falls_back() {
        let source = identity().source;
        let mut response:live::Resolution=serde_json::from_value(serde_json::json!({"found":true,"note":{"note_id":42,"title":"Same","file_kind":"note","vault":"other"}})).unwrap();
        response.source_vault = "work".into();
        response.launch_generation = 3;
        response.term = "id:42".into();
        assert_eq!(
            verified_key(&response, &source, "id:42").unwrap().vault,
            "other"
        );
        response.note.vault.clear();
        assert_eq!(verified_key(&response, &source, "id:42").unwrap().vault, "");
        response.found = false;
        assert!(verified_key(&response, &source, "id:42").is_err());
        response.found = true;
        assert!(verified_key(&response, &source, "Same").is_err());
        response.source_vault = "other".into();
        assert!(verified_key(&response, &source, "id:42").is_err());
    }
    #[test]
    fn included_query_rows_are_inert_even_with_matching_targets() {
        let fence = "```track-view\n{\"layout\":\"list\",\"columns\":[],\"groups\":[{\"rows\":[{\"title\":\"Same\",\"target\":\"id:42\",\"cells\":[]}]}]}\n```";
        let response=crate::included_notes::decode(serde_json::json!({"markdown":format!("{fence}\n\n![[Included]]\n"),"includes":[{"line":4,"source_line":0,"caption":"Included","lines":[fence],"note_id":9,"kind":"note","etag":"inc","vault":"other"}]}),"work").unwrap();
        let preview = markdown::included_preview(&response);
        let queries = project_queries(&preview);
        let mut state = IncludesState::new(identity());
        state.result = Some(Ok(Snapshot {
            query_positions: Ok(Default::default()),
            inline: Ok(Default::default()),
            boards: Default::default(),
            response,
            preview,
            queries,
        }));
        let snapshot = state.result.as_ref().unwrap().as_ref().unwrap();
        let mut host = 0;
        let mut included = 0;
        for (&key, view) in &snapshot.queries {
            if view.is_ok() {
                match &snapshot.preview.as_ref().unwrap().items[key.0] {
                    markdown::PreviewItem::Host(_) => {
                        assert!(host_row(&state, key, 0, 0).is_some());
                        host += 1;
                    }
                    _ => {
                        assert!(host_row(&state, key, 0, 0).is_none());
                        included += 1;
                    }
                }
            }
        }
        assert_eq!((host, included), (1, 1));
    }
}
