//! Find owns only rendered-preview text and geometry, never host-reader positions.
use super::*;
use crate::{reader_find, rendered_find_model as model};
use std::{cell::RefCell, collections::BTreeMap, rc::Rc};
type Slice = (usize, usize, usize);
#[derive(Clone, PartialEq, Eq)]
struct Owner {
    identity: Identity,
    serial: u64,
    epoch: u64,
    scope: String,
    scope_revision: u64,
    registry: Option<(String, Vec<String>)>,
    pages: BTreeMap<(usize, usize, Option<usize>), usize>,
    sources: std::collections::BTreeSet<(usize, usize)>,
}
#[derive(Clone)]
struct Action {
    owner: Owner,
    revision: u64,
}
#[derive(Default)]
pub(super) struct Ui {
    open: bool,
    input: Option<gpui::Entity<search_input::SearchInput>>,
    subscription: Option<gpui::Subscription>,
    owner: Option<Owner>,
    model: model::Model,
    keys: Vec<model::Key>,
    dirty: bool,
    seen: RefCell<model::Corpus>,
    anchors: RefCell<BTreeMap<Slice, Rc<markdown::FindAnchors>>>,
    geometry: RefCell<Option<(Rc<markdown::FindAnchors>, reader_find::Hit)>>,
    pending: bool,
    retries: u8,
    frame: u64,
}
fn owner(
    state: &IncludesState,
    scope: String,
    scope_revision: u64,
    registry: Option<(String, Vec<String>)>,
) -> Owner {
    Owner {
        scope,
        scope_revision,
        registry,
        identity: state.identity.clone(),
        serial: state.serial,
        epoch: state.preview_epoch,
        pages: state.query_pages.clone(),
        sources: state.query_sources.clone(),
    }
}
fn block(snapshot: &Snapshot, key: model::Key) -> Option<&markdown::Block> {
    let blocks = match snapshot.preview.as_ref().ok()?.items.get(key.0)? {
        markdown::PreviewItem::Host(b) | markdown::PreviewItem::Include { blocks: b, .. } => b,
    };
    blocks.get(key.1)
}
impl Ui {
    pub(super) fn interaction_revision(&self) -> u64 {
        self.model.revision
    }
    pub(super) fn is_open(&self) -> bool {
        self.open
    }
    fn action(&self) -> Option<Action> {
        Some(Action {
            owner: self.owner.clone()?,
            revision: self.model.revision,
        })
    }
    fn accepts(&self, action: &Action, current: &Owner) -> bool {
        self.open
            && self.owner.as_ref() == Some(current)
            && &action.owner == current
            && self.model.revision == action.revision
            && !self.dirty
    }
    pub(super) fn presentation(
        &self,
        state: &IncludesState,
        item: usize,
        start: usize,
        blocks: &[markdown::Block],
    ) -> Option<(
        reader_find::Matches,
        Option<usize>,
        Rc<markdown::FindAnchors>,
    )> {
        if !self.open {
            return None;
        }
        let snapshot = state.result.as_ref()?.as_ref().ok()?;
        for local in 0..blocks.len() {
            let key = (item, start + local);
            if block(snapshot, key) != Some(&blocks[local]) {
                continue;
            }
            // Search ordinary displayed text only. Rich projections and their control/source
            // blocks are deliberately omitted, even when a source disclosure is visible.
            if snapshot.queries.contains_key(&key)
                || snapshot.boards.blocks.contains_key(&key)
                || snapshot.boards.viewspecs.contains_key(&key)
                || snapshot.boards.flowcharts.contains_key(&key)
                || snapshot.boards.mindmaps.contains_key(&key)
                || snapshot.boards.charts.contains_key(&key)
            {
                continue;
            }
            self.seen.borrow_mut().observe(key);
        }
        if self.dirty
            || self.owner.as_ref().is_none_or(|o| {
                o.serial != state.serial
                    || o.epoch != state.preview_epoch
                    || o.pages != state.query_pages
                    || o.sources != state.query_sources
            })
        {
            return None;
        }
        let (matches, active) = self.model.slice(item, start, blocks.len());
        let anchors = self
            .anchors
            .borrow_mut()
            .entry((item, start, blocks.len()))
            .or_insert_with(|| Rc::new(markdown::FindAnchors::new(blocks, &state.preview_scroll)))
            .clone();
        if let Some(hit) = active.and_then(|i| matches.hits.get(i)) {
            *self.geometry.borrow_mut() = Some((anchors.clone(), hit.clone()));
        }
        Some((matches, active, anchors))
    }
}
impl Workspace {
    fn rendered_find_owner(&self) -> Owner {
        owner(
            self.shell.includes.as_ref().unwrap(),
            self.working_vault_scope().to_owned(),
            self.shell.scope_revision,
            self.follow_registry(),
        )
    }
    pub(super) fn rendered_find_composing(&self, cx: &App) -> bool {
        self.shell
            .includes
            .as_ref()
            .and_then(|s| s.find.input.as_ref())
            .is_some_and(|i| i.read(cx).is_composing())
    }
    fn rendered_find_current(&self, cx: &Context<Self>) -> Option<Owner> {
        let state = self.shell.includes.as_ref()?;
        if !state.preview_open
            || !self.includes_body_current(state)
            || !self.query_control_gate(&state.identity, cx).allowed()
        {
            return None;
        }
        let current = self.rendered_find_owner();
        (current.registry.is_some() && self.shell.accepted_title_request == Some(self.request))
            .then_some(current)
    }
    pub(super) fn begin_rendered_find(&mut self, dark: bool, cx: &mut Context<Self>) {
        if self
            .shell
            .includes
            .as_ref()
            .is_some_and(|s| s.find.input.is_none())
        {
            let input = cx.new(|cx| {
                search_input::SearchInput::with_purpose(search_input::InputPurpose::ReaderFind, cx)
            });
            let sub = cx.subscribe(
                &input,
                |this, input, event: &search_input::SearchChanged, cx| {
                    let Some(state) = this.shell.includes.as_mut() else {
                        return;
                    };
                    if state
                        .find
                        .input
                        .as_ref()
                        .is_none_or(|current| current.entity_id() != input.entity_id())
                    {
                        return;
                    }
                    state.find.model.query = event.0.clone();
                    state.find.model.revision = state.find.model.revision.wrapping_add(1);
                    state.find.dirty = true;
                    state.find.pending = true;
                    state.find.retries = 0;
                    cx.notify();
                },
            );
            let state = self.shell.includes.as_mut().unwrap();
            state.find.input = Some(input);
            state.find.subscription = Some(sub);
        }
        let current = self.rendered_find_owner();
        let state = self.shell.includes.as_mut().unwrap();
        if state.find.owner.as_ref() != Some(&current) {
            state.find.owner = Some(current);
            state.find.dirty = true;
            state.find.pending = false;
            state.find.model.revision = state.find.model.revision.wrapping_add(1);
            state.find.anchors.borrow_mut().clear();
        }
        state
            .find
            .input
            .as_ref()
            .unwrap()
            .update(cx, |input, _| input.set_dark(dark));
        *state.find.seen.borrow_mut() = Default::default();
        *state.find.geometry.borrow_mut() = None;
        state.find.frame = state.find.frame.wrapping_add(1);
    }
    pub(super) fn finish_rendered_find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let state = self.shell.includes.as_mut().unwrap();
        if !state.find.open {
            return;
        }
        let keys = state.find.seen.borrow().keys.clone();
        if state.find.dirty || keys != state.find.keys {
            state.find.keys = keys;
            state.find.dirty = false;
            if let Some(Ok(snapshot)) = &state.result {
                state.find.model.evaluate(
                    state
                        .find
                        .keys
                        .iter()
                        .filter_map(|&key| block(snapshot, key).map(|b| (key, b))),
                );
            }
            state.find.anchors.borrow_mut().clear();
            *state.find.geometry.borrow_mut() = None;
            cx.notify();
            return;
        }
        if !state.find.pending {
            return;
        }
        let Some((anchors, hit)) = state.find.geometry.borrow().clone() else {
            state.find.pending = false;
            return;
        };
        state.find.pending = false;
        let action = state.find.action().unwrap();
        let frame = state.find.frame;
        let weak = cx.entity().downgrade();
        window.on_next_frame(move |window, cx| {
            let _ = weak.update(cx, |this, cx| {
                if !window.is_window_active() || this.rendered_find_composing(cx) {
                    return;
                }
                let Some(current) = this.rendered_find_current(cx) else {
                    return;
                };
                let state = this.shell.includes.as_ref().unwrap();
                if state.find.frame != frame
                    || !state.find.accepts(&action, &current)
                    || !state
                        .find
                        .geometry
                        .borrow()
                        .as_ref()
                        .is_some_and(|(a, h)| Rc::ptr_eq(a, &anchors) && h == &hit)
                {
                    return;
                }
                if !anchors.scroll_to_hit(&hit, window.viewport_size()) && state.find.retries == 0 {
                    let state = this.shell.includes.as_mut().unwrap();
                    state.find.retries = 1;
                    state.find.pending = true;
                }
                cx.notify();
            });
        });
    }
    fn rendered_find_action(
        &mut self,
        action: &Action,
        step: Option<bool>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active() || self.rendered_find_composing(cx) {
            return;
        }
        let Some(current) = self.rendered_find_current(cx) else {
            return;
        };
        let state = self.shell.includes.as_mut().unwrap();
        if !state.find.accepts(action, &current) {
            return;
        }
        if let Some(forward) = step {
            state.find.model.step(forward);
            state.find.pending = true;
            state.find.retries = 0;
        } else {
            state.find.open = false;
            state.find.pending = false;
            state.find.model.revision = state.find.model.revision.wrapping_add(1);
            state.find.anchors.borrow_mut().clear();
            if let Some(focus) = &state.preview_focus {
                window.focus(focus);
            }
        }
        cx.notify();
    }
    pub(super) fn rendered_find_key(
        &mut self,
        step: Option<bool>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        use gpui::Focusable;
        let Some(state) = self.shell.includes.as_ref() else {
            return;
        };
        if !state
            .find
            .input
            .as_ref()
            .is_some_and(|input| input.focus_handle(cx).is_focused(window))
        {
            return;
        }
        if let Some(action) = state.find.action() {
            self.rendered_find_action(&action, step, window, cx);
        }
    }
    pub(super) fn rendered_find_escape(
        &mut self,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        if self.rendered_find_composing(cx) {
            return true;
        }
        let Some(state) = self.shell.includes.as_ref() else {
            return true;
        };
        if !state.find.open {
            return false;
        }
        if let Some(action) = state.find.action() {
            self.rendered_find_action(&action, None, window, cx);
        }
        true
    }
    pub(super) fn rendered_find_controls(&self, cx: &mut Context<Self>) -> Div {
        let state = self.shell.includes.as_ref().unwrap();
        let expected = self.rendered_find_owner();
        if !state.find.open {
            return div().child(
                div()
                    .id("preview-find-open")
                    .cursor_pointer()
                    .child("Find in preview")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        use gpui::Focusable;
                        if !window.is_window_active()
                            || this.rendered_find_composing(cx)
                            || this.rendered_find_current(cx).as_ref() != Some(&expected)
                        {
                            return;
                        }
                        let state = this.shell.includes.as_mut().unwrap();
                        state.find.open = true;
                        state.find.dirty = true;
                        state.find.pending = true;
                        state.find.retries = 0;
                        if let Some(input) = &state.find.input {
                            input.update(cx, |i, cx| i.select_all(cx));
                            window.focus(&input.focus_handle(cx));
                        }
                        cx.notify();
                    })),
            );
        }
        let find = &state.find;
        let action = find.action();
        let status = if let Some(e) = &find.model.error {
            e.clone()
        } else {
            format!(
                "{} of {}{} · displayed prose only",
                find.model.active.map_or(0, |i| i + 1),
                find.model.hits.len(),
                if find.model.limited {
                    "+ (limited)"
                } else {
                    ""
                }
            )
        };
        let mut bar = div()
            .flex()
            .flex_wrap()
            .gap_2()
            .items_center()
            .child(div().w(px(250.)).max_w_full().children(find.input.clone()))
            .child(status);
        for (label, step) in [
            ("Previous", Some(false)),
            ("Next", Some(true)),
            ("Close Find", None),
        ] {
            let action = action.clone();
            bar = bar.child(
                div()
                    .id(gpui::SharedString::from(format!("rendered-find-{label}")))
                    .cursor_pointer()
                    .child(label)
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if let Some(action) = &action {
                            this.rendered_find_action(action, step, window, cx);
                        }
                        cx.stop_propagation();
                    })),
            );
        }
        bar
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn owner_fixture() -> Owner {
        let key = live::NoteKey {
            vault: "host".into(),
            note_id: 1,
        };
        Owner {
            identity: Identity {
                source: LinkSource {
                    note: key.clone(),
                    etag: "rev".into(),
                    storage_generation: 2,
                    launch_generation: 3,
                    request: 4,
                },
                route: key,
            },
            serial: 5,
            epoch: 6,
            scope: "host".into(),
            scope_revision: 7,
            registry: Some(("host".into(), vec!["host".into()])),
            pages: Default::default(),
            sources: Default::default(),
        }
    }
    #[test]
    fn old_toolbar_actions_reject_reopen_source_scope_page_and_query_changes() {
        let owner = owner_fixture();
        let mut ui = Ui {
            open: true,
            owner: Some(owner.clone()),
            ..Default::default()
        };
        let action = ui.action().unwrap();
        assert!(ui.accepts(&action, &owner));
        for dimension in 0..9 {
            let mut stale = owner.clone();
            match dimension {
                0 => stale.serial += 1,
                1 => stale.epoch += 1,
                2 => stale.scope_revision += 1,
                3 => stale.identity.source.request += 1,
                4 => stale.identity.source.storage_generation += 1,
                5 => stale.registry = None,
                6 => {
                    stale.pages.insert((0, 0, None), 1);
                }
                7 => {
                    stale.sources.insert((0, 0));
                }
                _ => stale.scope = "foreign".into(),
            }
            assert!(!ui.accepts(&action, &stale));
        }
        ui.dirty = true;
        assert!(!ui.accepts(&action, &owner));
        ui.dirty = false;
        ui.model.revision += 1;
        assert!(!ui.accepts(&action, &owner));
        ui.model.revision -= 1;
        ui.open = false;
        assert!(!ui.accepts(&action, &owner));
    }
}
