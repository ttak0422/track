//! Read-only full-graph overlays, isolated from reader/editor document state.
use super::*;
use graph_preview::{Content, PinResult, Previews, Read, State, Target};
use std::time::{Duration, Instant};

pub struct PreviewUi {
    pub model: Previews,
    clock: Instant,
    wake: Option<gpui::Task<()>>,
    deadline: Option<u64>,
    generation: u64,
    message: String,
    parsed: std::collections::BTreeMap<u64, Vec<markdown::Block>>,
}
impl Default for PreviewUi {
    fn default() -> Self {
        Self {
            model: Previews::default(),
            clock: Instant::now(),
            wake: None,
            deadline: None,
            generation: 0,
            message: String::new(),
            parsed: Default::default(),
        }
    }
}
impl PreviewUi {
    fn now(&self) -> u64 {
        self.clock.elapsed().as_millis().min(u64::MAX as u128) as u64
    }
    fn prune_content(&mut self) {
        let live: Vec<_> = self
            .model
            .transient
            .as_ref()
            .into_iter()
            .chain(self.model.pinned.as_ref())
            .map(|p| p.read.token)
            .collect();
        self.parsed.retain(|token, _| live.contains(token));
    }
    pub fn cancel_wake(&mut self) {
        self.prune_content();
        self.wake = None;
        self.deadline = None;
        self.generation += 1;
    }
}
impl Workspace {
    pub(crate) fn cancel_transient_preview(&mut self) {
        self.previews.model.navigate();
        self.previews.cancel_wake();
        self.previews.message.clear();
    }
    pub(crate) fn graph_preview_intent(
        &mut self,
        event: &graph_view::PreviewIntent,
        cx: &mut Context<Self>,
    ) {
        if !self.full_graph_active() {
            return;
        }
        let now = self.previews.now();
        match event {
            graph_view::PreviewIntent::Hover(id, anchor) => {
                if !self.full_graph.nodes.contains(id) {
                    return;
                }
                let Some(vault) = self.full_graph.scope.clone() else {
                    return;
                };
                self.previews.model.hover(
                    Target {
                        vault,
                        note_id: *id,
                    },
                    *anchor,
                    now,
                );
            }
            graph_view::PreviewIntent::Leave => self.previews.model.leave_opener(now),
            graph_view::PreviewIntent::Cancel => self.cancel_transient_preview(),
        }
        self.schedule_preview(cx);
        cx.notify();
    }
    fn schedule_preview(&mut self, cx: &mut Context<Self>) {
        self.previews.prune_content();
        let deadline = self.previews.model.next_deadline();
        if deadline == self.previews.deadline {
            return;
        }
        self.previews.cancel_wake();
        self.previews.deadline = deadline;
        let Some(deadline) = deadline else {
            return;
        };
        let generation = self.previews.generation;
        let wait = Duration::from_millis(deadline.saturating_sub(self.previews.now()));
        self.previews.wake = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(wait).await;
            let _ = this.update(cx, |this, cx| {
                if this.previews.generation != generation {
                    return;
                }
                this.previews.deadline = None;
                let read = this.previews.model.advance(this.previews.now());
                if let Some(read) = read {
                    this.read_graph_preview(read, cx);
                }
                this.schedule_preview(cx);
                cx.notify();
            });
        }));
    }
    fn read_graph_preview(&mut self, read: Read, cx: &mut Context<Self>) {
        let Some(session) = self.live.clone() else {
            return;
        };
        let epoch = self.accepted_storage_generation;
        if session.client().launch_generation != read.scope.session {
            return;
        }
        let request = read.clone();
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            fetch_preview(
                &request,
                epoch,
                || client.storage().map(|storage| storage.generation),
                |key| client.note(key),
            )
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if !completion_current(
                    read.scope.session,
                    epoch,
                    this.live.as_ref().map(|s| s.client().launch_generation),
                    this.accepted_storage_generation,
                ) || this.quit_session.failure().is_some()
                    || this.quit_session.is_closing()
                {
                    return;
                }
                if !this.preview_present(&read) {
                    return;
                }
                let blocks = result
                    .as_ref()
                    .ok()
                    .map(|content| markdown::parse(&content.body));
                if this
                    .previews
                    .model
                    .complete(&read, result.map_err(|e| e.to_string()))
                {
                    if let Some(blocks) = blocks {
                        this.previews.parsed.insert(read.token, blocks);
                    }
                    this.previews.prune_content();
                }
                cx.notify();
            });
        })
        .detach();
    }
    fn preview_present(&self, read: &Read) -> bool {
        self.previews
            .model
            .transient
            .as_ref()
            .into_iter()
            .chain(self.previews.model.pinned.as_ref())
            .any(|preview| &preview.read == read)
    }
    pub(crate) fn render_graph_previews(
        &self,
        dark: bool,
        viewport: gpui::Size<gpui::Pixels>,
        cx: &mut Context<Self>,
    ) -> Vec<gpui::AnyElement> {
        let paper = if dark { 0x222629 } else { 0xffffff };
        let ink = if dark { 0xe9e9e4 } else { 0x1a1a18 };
        let line = if dark { 0x454b4e } else { 0xd9d6cc };
        [self.previews.model.pinned.as_ref().map(|p| (true,p)), self.previews.model.transient.as_ref().map(|p| (false,p))]
            .into_iter().flatten().map(|(pinned, preview)| {
                let identity = preview.read.clone();
                let hover_identity = identity.clone();
                let open_identity = identity.clone();
                let pin_identity = identity.clone();
                let close_identity = identity.clone();
                let title = match &preview.state {
                    State::Ready(content) => content.title.clone(),
                    _ => format!("Note {}", identity.target.note_id),
                };
                let (x,y,width,height) = bounds(preview.anchor, (f32::from(viewport.width), f32::from(viewport.height)));
                let mut body = div().id(("graph-preview-body", identity.token as usize))
                    .flex_1().min_h_0().overflow_y_scroll().p_3();
                body = match &preview.state {
                    State::Loading => body.child("Loading note…"),
                    State::Failed(error) => body.child(format!("Unable to load note: {error}")),
                    State::Ready(content) => {
                        let blocks = self.previews.parsed.get(&identity.token);
                        body.child(div().text_xs().child(format!("{}{}", if content.read_only { "Read-only source" } else { "Read-only preview" }, if content.partial { " · Partial / unavailable data" } else { "" })))
                            .children(content.diagnostics.iter().map(|message| div().text_xs().child(message.clone())))
                            .when(content.body.is_empty(), |el| el.child("Empty note"))
                            .when(!content.body.is_empty(), |el| if let Some(blocks) = blocks { el.child(markdown::render_themed(blocks, dark)) } else { el.child(content.body.clone()) })
                    }
                };
                div().id(("graph-preview-card", identity.token as usize))
                    .absolute().left(px(x)).top(px(y)).w(px(width)).h(px(height))
                    .flex().flex_col().bg(rgb(paper)).text_color(rgb(ink)).border_1().border_color(rgb(line)).rounded_lg().shadow_lg().overflow_hidden()
                    .on_mouse_down(gpui::MouseButton::Left, |_,_,cx| cx.stop_propagation())
                    .on_mouse_move(|_,_,cx| cx.stop_propagation())
                    .on_scroll_wheel(|_,_,cx| cx.stop_propagation())
                    .on_hover(cx.listener(move |this, inside, _, cx| {
                        if *inside { this.previews.model.enter_preview(&hover_identity); }
                        else { this.previews.model.leave_preview(&hover_identity, this.previews.now()); }
                        this.schedule_preview(cx); cx.notify();
                    }))
                    .child(div().p_2().border_b_1().border_color(rgb(line)).child(title)
                        .child(div().text_xs().child(format!("{} · ID {}", if identity.target.vault.is_empty() { "launch vault" } else { &identity.target.vault }, identity.target.note_id)))
                        .child(div().flex().gap_3().text_xs()
                            .child(div().id(("preview-pin", identity.token as usize)).cursor_pointer().child(if pinned { "Unpin" } else { "Pin" })
                                .on_click(cx.listener(move |this,_,_,cx| {
                                    if !this.preview_present(&pin_identity) { return; }
                                    this.previews.message.clear();
                                    if pinned { this.previews.model.unpin_identity(&pin_identity, this.previews.now()); }
                                    else if this.previews.model.pin_identity(&pin_identity) == PinResult::CapacityReached {
                                        this.previews.message = "One pinned preview maximum. Close or unpin the existing preview first.".into();
                                    }
                                    this.schedule_preview(cx); cx.stop_propagation(); cx.notify();
                                })))
                            .child(div().id(("preview-open", identity.token as usize)).cursor_pointer().child("Open as page")
                                .on_click(cx.listener(move |this,_,_,cx| {
                                    if !this.preview_present(&open_identity) { return; }
                                    this.open_live_key(live::NoteKey { vault: open_identity.target.vault.clone(), note_id: open_identity.target.note_id }, cx);
                                    cx.stop_propagation();
                                })))
                            .child(div().id(("preview-close", identity.token as usize)).cursor_pointer().child("Close")
                                .on_click(cx.listener(move |this,_,_,cx| {
                                    if !this.preview_present(&close_identity) { return; }
                                    this.previews.model.close_identity(&close_identity);
                                    this.previews.message.clear(); this.schedule_preview(cx); cx.stop_propagation(); cx.notify();
                                })))
                        ))
                    .child(body)
                    .child(div().p_2().text_xs().child("Native Markdown subset · links / embeds / actions inert"))
                    .when(!self.previews.message.is_empty(), |el| el.child(div().p_2().text_xs().child(self.previews.message.clone())))
                    .into_any_element()
            }).collect()
    }
}
fn fetch_preview(
    request: &Read,
    epoch: Option<u64>,
    mut storage: impl FnMut() -> Result<u64, live::Error>,
    note: impl FnOnce(&live::NoteKey) -> Result<live::NoteResponse, live::Error>,
) -> Result<Content, live::Error> {
    let before = storage()?;
    if Some(before) != epoch {
        return Err(live::Error::Stale);
    }
    let result = note(&live::NoteKey {
        vault: request.target.vault.clone(),
        note_id: request.target.note_id,
    });
    if storage()? != before {
        return Err(live::Error::Stale);
    }
    result.map(preview_content)
}
fn completion_current(
    launch: u64,
    epoch: Option<u64>,
    current_launch: Option<u64>,
    current_epoch: Option<u64>,
) -> bool {
    current_launch == Some(launch) && epoch.is_some() && epoch == current_epoch
}
fn preview_content(response: live::NoteResponse) -> Content {
    let diagnostics: Vec<_> = response
        .related_and_diagnostics
        .iter()
        .filter(|(key, _)| {
            matches!(
                key.as_str(),
                "read_state" | "partial" | "unavailable" | "storage_state"
            )
        })
        .map(|(key, value)| format!("{key}: {value}"))
        .collect();
    let partial = workspace_live::partial(&response.related_and_diagnostics);
    Content {
        title: response.note.note.title,
        body: response.note.body,
        read_only: response.note.read_only,
        partial,
        diagnostics,
    }
}
fn bounds(anchor: (f32, f32), viewport: (f32, f32)) -> (f32, f32, f32, f32) {
    let width = 380_f32.min((viewport.0 - 24.).max(1.));
    let height = 330_f32.min((viewport.1 - 24.).max(1.));
    (
        (anchor.0 + 16.).clamp(12., (viewport.0 - width - 12.).max(12.)),
        (anchor.1 + 16.).clamp(12., (viewport.1 - height - 12.).max(12.)),
        width,
        height,
    )
}
#[cfg(test)]
mod tests {
    use super::*;
    fn response() -> live::NoteResponse {
        serde_json::from_value(serde_json::json!({"note":{"note_id":7,"vault":"fixture","title":"Fixture","path":"fixture.md","file_kind":"note","body":"# Authored heading","etag":"x","read_only":true}})).unwrap()
    }
    #[test]
    fn intent_dispatches_one_scoped_read_with_fake_transport() {
        let mut model = Previews::default();
        model.set_scope(graph_preview::Scope {
            session: 1,
            graph: 2,
            vault: "fixture".into(),
        });
        model.hover(
            Target {
                vault: "fixture".into(),
                note_id: 7,
            },
            (12., 20.),
            0,
        );
        let mut requests = Vec::new();
        assert!(model.advance(259).is_none());
        assert!(requests.is_empty());
        let read = model.advance(260).unwrap();
        let content = fetch_preview(
            &read,
            Some(3),
            || Ok(3),
            |key| {
                requests.push(key.clone());
                Ok(response())
            },
        )
        .unwrap();
        assert_eq!(
            requests,
            vec![live::NoteKey {
                vault: "fixture".into(),
                note_id: 7
            }]
        );
        assert!(model.complete(&read, Ok(content)));
        assert!(model.advance(1000).is_none());
    }
    #[test]
    fn stale_storage_never_publishes_and_preflight_does_not_read() {
        let read = Read {
            scope: graph_preview::Scope {
                session: 1,
                graph: 2,
                vault: "fixture".into(),
            },
            target: Target {
                vault: "fixture".into(),
                note_id: 7,
            },
            token: 1,
        };
        let mut reads = 0;
        assert!(
            fetch_preview(
                &read,
                Some(3),
                || Ok(4),
                |_| {
                    reads += 1;
                    Ok(response())
                }
            )
            .is_err()
        );
        assert_eq!(reads, 0);
        let mut epochs = [3, 4].into_iter();
        assert!(
            fetch_preview(
                &read,
                Some(3),
                || Ok(epochs.next().unwrap()),
                |_| {
                    reads += 1;
                    Ok(response())
                }
            )
            .is_err()
        );
        assert_eq!(reads, 1);
    }
    #[test]
    fn parsed_cache_is_retained_for_pin_and_pruned_with_slots() {
        let mut ui = PreviewUi::default();
        ui.model.set_scope(graph_preview::Scope {
            session: 1,
            graph: 2,
            vault: "fixture".into(),
        });
        ui.model.hover(
            Target {
                vault: "fixture".into(),
                note_id: 7,
            },
            (0., 0.),
            0,
        );
        let a = ui.model.advance(260).unwrap();
        ui.parsed.insert(a.token, markdown::parse("# A"));
        ui.model.pin_identity(&a);
        ui.model.navigate();
        ui.prune_content();
        assert_eq!(ui.parsed.len(), 1);
        ui.model.hover(
            Target {
                vault: "fixture".into(),
                note_id: 8,
            },
            (0., 0.),
            300,
        );
        let b = ui.model.advance(560).unwrap();
        ui.parsed.insert(b.token, markdown::parse("# B"));
        assert_eq!(ui.parsed.len(), 2);
        ui.model.close_identity(&b);
        ui.prune_content();
        assert_eq!(ui.parsed.len(), 1);
        ui.model.invalidate_session();
        ui.cancel_wake();
        assert!(ui.parsed.is_empty());
    }
    #[test]
    fn placement_is_bounded() {
        assert_eq!(bounds((999., 999.), (800., 600.)), (408., 258., 380., 330.));
        assert_eq!(bounds((-100., -100.), (300., 200.)), (12., 12., 276., 176.));
    }
    #[test]
    fn session_and_storage_gate() {
        assert!(completion_current(1, Some(2), Some(1), Some(2)));
        assert!(!completion_current(1, Some(2), Some(3), Some(2)));
        assert!(!completion_current(1, Some(2), Some(1), Some(3)));
        assert!(!completion_current(1, None, Some(1), None));
    }
    #[test]
    fn empty_and_partial_diagnostics_are_preserved() {
        let response = serde_json::from_value(serde_json::json!({"note":{"note_id":7,"vault":"fixture","title":"Empty","path":"fixture.md","file_kind":"note","body":"","etag":"x","read_only":true},"read_state":{"partial":true},"unavailable":["include"]})).unwrap();
        let content = preview_content(response);
        assert!(content.body.is_empty() && content.partial && content.read_only);
        assert_eq!(content.title, "Empty");
        assert_eq!(content.diagnostics.len(), 2);
    }
}
