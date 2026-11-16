//! Selection belongs to one displayed preview occurrence, never host source lines.
use super::*;
use crate::reader_selection;
use std::{
    collections::{BTreeMap, BTreeSet},
    rc::Rc,
};
type Slice = (usize, usize, usize);
#[derive(Clone, PartialEq, Eq)]
struct Owner {
    identity: Identity,
    serial: u64,
    epoch: u64,
    mode: u64,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
    pages: BTreeMap<(usize, usize, Option<usize>), usize>,
    sources: BTreeSet<(usize, usize)>,
}
struct Slot {
    measurements: markdown::SelectionMeasurements,
    allowed: BTreeSet<usize>,
}
#[derive(Clone, PartialEq)]
struct Geometry {
    row: gpui::Bounds<gpui::Pixels>,
    reader: gpui::Bounds<gpui::Pixels>,
    offset: gpui::Point<gpui::Pixels>,
    extent: gpui::Size<gpui::Pixels>,
    viewport: gpui::Size<gpui::Pixels>,
    scale: f32,
}
impl Geometry {
    fn capture(state: &IncludesState, slot: &Slot, local: usize, window: &Window) -> Option<Self> {
        let row = slot.measurements.measured_row(local)?.bounds;
        let reader = state.preview_scroll.bounds();
        let viewport = window.viewport_size();
        // Only wholly visible measured paragraphs may begin or retain selection.
        if row.size.width <= px(0.)
            || row.size.height <= px(0.)
            || row.origin.x < reader.origin.x
            || row.origin.y < reader.origin.y
            || row.right() > reader.right()
            || row.bottom() > reader.bottom()
            || row.origin.x < px(0.)
            || row.origin.y < px(0.)
            || row.right() > viewport.width
            || row.bottom() > viewport.height
        {
            return None;
        }
        Some(Self {
            row,
            reader,
            offset: state.preview_scroll.offset(),
            extent: state.preview_scroll.max_offset(),
            viewport,
            scale: window.scale_factor(),
        })
    }
}
struct Selected {
    slice: Slice,
    local: usize,
    slot: Rc<Slot>,
    geometry: Geometry,
    layout: gpui::TextLayout,
    range: reader_selection::State,
    revision: u64,
}
#[derive(Default)]
pub(super) struct Ui {
    owner: Option<Owner>,
    slots: BTreeMap<Slice, Rc<Slot>>,
    selected: Option<Selected>,
    revision: u64,
    feedback: Option<&'static str>,
}
impl Ui {
    fn clear(&mut self) {
        self.selected = None;
        self.feedback = None;
        self.revision = self.revision.wrapping_add(1);
    }
    fn bind(&mut self, owner: Option<Owner>) {
        if self.owner != owner {
            self.clear();
            self.slots.clear();
            self.owner = owner;
        }
    }
}
fn block(state: &IncludesState, key: (usize, usize)) -> Option<&markdown::Block> {
    let snapshot = state.result.as_ref()?.as_ref().ok()?;
    if snapshot.queries.contains_key(&key)
        || snapshot.boards.blocks.contains_key(&key)
        || snapshot.boards.viewspecs.contains_key(&key)
        || snapshot.boards.charts.contains_key(&key)
        || snapshot.boards.flowcharts.contains_key(&key)
        || snapshot.boards.mindmaps.contains_key(&key)
    {
        return None;
    }
    let blocks = match snapshot.preview.as_ref().ok()?.items.get(key.0)? {
        markdown::PreviewItem::Host(blocks) | markdown::PreviewItem::Include { blocks, .. } => {
            blocks
        }
    };
    blocks
        .get(key.1)
        .filter(|b| reader_selection::eligible_display(b))
}
impl Workspace {
    fn preview_selection_owner(&self, cx: &Context<Self>) -> Option<Owner> {
        let s = self.shell.includes.as_ref()?;
        if self.recent_open()
            || self.reader_hierarchy_open()
            || self.enlarged_image_open()
            || self.creation_open()
            || self.reader_selection_dragging()
            || self.shell.find.is_composing(cx)
            || self.shell.calendar_zone.is_composing(cx)
            || s.fetching
            || s.query_nav.pending.is_some()
            || s.find.is_open()
            || !s.preview_open
            || !self.query_control_gate(&s.identity, cx).allowed()
            || self.shell.accepted_title_request != Some(self.request)
            || !self.includes_body_current(s)
        {
            return None;
        }
        let registry = self.follow_registry()?;
        let vault = &s.identity.source.note.vault;
        let canonical = if vault.is_empty() { &registry.0 } else { vault };
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if !registry.1.contains(canonical)
            || !self.session_ui.permits(vault)
            || storage.phase != "ready"
            || storage.generation != s.identity.source.storage_generation
            || self.accepted_storage_generation != Some(storage.generation)
        {
            return None;
        }
        Some(Owner {
            identity: s.identity.clone(),
            serial: s.serial,
            epoch: s.preview_epoch,
            mode: self.shell.rendered_reading.generation(),
            scope: self.working_vault_scope().to_owned(),
            scope_revision: self.shell.scope_revision,
            registry,
            pages: s.query_pages.clone(),
            sources: s.query_sources.clone(),
        })
    }
    pub(super) fn clear_preview_selection(&mut self) {
        if let Some(s) = self.shell.includes.as_ref() {
            s.selection.borrow_mut().clear();
        }
    }
    pub(super) fn begin_preview_selection(&mut self, window: &Window, cx: &Context<Self>) {
        let owner = self
            .preview_selection_owner(cx)
            .filter(|_| window.is_window_active());
        let Some(s) = self.shell.includes.as_ref() else {
            return;
        };
        let mut ui = s.selection.borrow_mut();
        ui.bind(owner);
        if ui.selected.as_ref().is_some_and(|v| {
            Geometry::capture(s, &v.slot, v.local, window).as_ref() != Some(&v.geometry)
        }) {
            ui.clear();
        }
    }
    pub(super) fn preview_selection_presentation(
        &self,
        item: usize,
        start: usize,
        blocks: &[markdown::Block],
        cx: &mut Context<Self>,
    ) -> Option<markdown::SelectionCallbacks> {
        let owner = self.preview_selection_owner(cx)?;
        let state = self.shell.includes.as_ref()?;
        let mut ui = state.selection.borrow_mut();
        if ui.owner.as_ref() != Some(&owner) {
            return None;
        }
        let slice = (item, start, blocks.len());
        if !ui.slots.contains_key(&slice) && ui.slots.len() >= 256 {
            return None;
        }
        let allowed: BTreeSet<_> = blocks
            .iter()
            .enumerate()
            .filter_map(|(i, b)| (block(state, (item, start + i)) == Some(b)).then_some(i))
            .collect();
        if ui.slots.get(&slice).is_some_and(|s| s.allowed != allowed) {
            ui.clear();
            ui.slots.remove(&slice);
        }
        let slot = ui
            .slots
            .entry(slice)
            .or_insert_with(|| {
                Rc::new(Slot {
                    measurements: Default::default(),
                    allowed,
                })
            })
            .clone();
        if slot.allowed.is_empty() {
            return None;
        }
        let selected = ui
            .selected
            .as_ref()
            .filter(|s| s.slice == slice)
            .and_then(|s| s.range.range().map(|r| vec![(s.local, r)]))
            .unwrap_or_default();
        let generation = slot.measurements.begin();
        let measurements = slot.measurements.clone();
        let weak = cx.entity().downgrade();
        Some(markdown::SelectionCallbacks {
            selected,
            measurements,
            generation,
            pointer: Rc::new(move |local, phase, layout, position, window, cx| {
                weak.update(cx, |this, cx| {
                    this.preview_selection_pointer(
                        &owner, slice, &slot, local, phase, layout, position, window, cx,
                    )
                })
                .unwrap_or(false)
            }),
        })
    }
    fn preview_selection_pointer(
        &mut self,
        owner: &Owner,
        slice: Slice,
        slot: &Rc<Slot>,
        local: usize,
        phase: markdown::SelectionPhase,
        layout: &gpui::TextLayout,
        position: Option<gpui::Point<gpui::Pixels>>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> bool {
        use markdown::SelectionPhase as P;
        if !window.is_window_active() || self.preview_selection_owner(cx).as_ref() != Some(owner) {
            return false;
        }
        let state = self.shell.includes.as_ref().unwrap();
        let mut ui = state.selection.borrow_mut();
        if ui.owner.as_ref() != Some(owner)
            || !ui.slots.get(&slice).is_some_and(|s| Rc::ptr_eq(s, slot))
            || !slot.allowed.contains(&local)
        {
            return false;
        }
        let Some(b) = block(state, (slice.0, slice.1 + local)) else {
            return false;
        };
        let geometry = Geometry::capture(state, slot, local, window);
        if !matches!(phase, P::Leave | P::Cancel | P::EndOutside)
            && geometry.as_ref().is_none_or(|g| layout.bounds() != g.row)
        {
            ui.clear();
            cx.notify();
            return false;
        }
        if matches!(phase, P::Begin) {
            ui.clear();
            let Some(geometry) = geometry else {
                return false;
            };
            let Some(byte) = position.and_then(|p| layout.index_for_position(p).ok()) else {
                return false;
            };
            let Some(range) = reader_selection::State::begin(&b.text, byte) else {
                return false;
            };
            ui.selected = Some(Selected {
                slice,
                local,
                slot: slot.clone(),
                geometry,
                layout: layout.clone(),
                range,
                revision: ui.revision,
            });
            cx.notify();
            return true;
        }
        let Some(selected) = ui
            .selected
            .as_mut()
            .filter(|v| v.slice == slice && v.local == local && v.range.dragging())
        else {
            return false;
        };
        if geometry.as_ref() != Some(&selected.geometry) {
            ui.clear();
            cx.notify();
            return true;
        }
        let hit_layout = if matches!(phase, P::Leave) {
            &selected.layout
        } else {
            layout
        };
        let byte = position.and_then(|p| hit_layout.index_for_position(p).ok());
        match phase {
            P::Move => {
                selected.range.update(&b.text, byte);
            }
            P::End => {
                selected.range.finish(&b.text, byte);
            }
            P::Leave => {
                if !selected.range.leave(&b.text, byte) {
                    return false;
                }
            }
            P::Cancel | P::EndOutside => selected.range.cancel(),
            P::Begin => unreachable!(),
        }
        cx.notify();
        true
    }
    pub(super) fn preview_selection_controls(&self, cx: &mut Context<Self>) -> Div {
        let help = "Select one fully visible non-link paragraph to copy displayed text (up to 256 KiB). Styled text and inline code are supported; multiple paragraphs, links, tables and rich projections are not. Selection covers at most 256 rendered slices.";
        let Some(owner) = self.preview_selection_owner(cx) else {
            return div()
                .child(help)
                .child(div().h(px(24.)).child("Copy selected text unavailable"));
        };
        let state = self.shell.includes.as_ref().unwrap();
        let ui = state.selection.borrow();
        let Some(selected) = ui
            .selected
            .as_ref()
            .filter(|s| !s.range.dragging() && s.range.range().is_some())
        else {
            return div()
                .child(help)
                .child(div().h(px(24.)).child("Copy selected text unavailable"));
        };
        let revision = selected.revision;
        let feedback = ui.feedback.unwrap_or("Copy selected text");
        div().child(help).child(
            div()
                .id("copy-preview-selected-text")
                .h(px(24.))
                .overflow_hidden()
                .whitespace_nowrap()
                .cursor_pointer()
                .child(feedback)
                .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_click(cx.listener(move |this, _, window, cx| {
                    this.copy_preview_selection(&owner, revision, window, cx)
                })),
        )
    }
    fn copy_preview_selection(
        &mut self,
        owner: &Owner,
        revision: u64,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active() || self.preview_selection_owner(cx).as_ref() != Some(owner) {
            return;
        }
        let state = self.shell.includes.as_ref().unwrap();
        let mut ui = state.selection.borrow_mut();
        if ui.owner.as_ref() != Some(owner) {
            return;
        }
        let Some(s) = ui.selected.as_ref().filter(|s| s.revision == revision) else {
            return;
        };
        if Geometry::capture(state, &s.slot, s.local, window).as_ref() != Some(&s.geometry) {
            ui.clear();
            cx.notify();
            return;
        }
        let Some(b) = block(state, (s.slice.0, s.slice.1 + s.local)) else {
            ui.clear();
            return;
        };
        match s.range.payload(&b.text) {
            Ok(text) => {
                cx.write_to_clipboard(gpui::ClipboardItem::new_string(text.to_owned()));
                ui.feedback = Some("Copy submitted");
            }
            Err(reader_selection::CopyError::TooLarge) => {
                ui.feedback = Some("Copy unavailable: exceeds 256 KiB")
            }
            Err(_) => {
                ui.clear();
            }
        }
        cx.notify();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn owner() -> Owner {
        let note = live::NoteKey {
            vault: "host".into(),
            note_id: 1,
        };
        Owner {
            identity: Identity {
                source: LinkSource {
                    note: note.clone(),
                    etag: "e".into(),
                    request: 1,
                    launch_generation: 2,
                    storage_generation: 3,
                },
                route: note,
            },
            serial: 4,
            epoch: 5,
            mode: 6,
            scope: "host".into(),
            scope_revision: 7,
            registry: ("host".into(), vec!["host".into(), "foreign".into()]),
            pages: BTreeMap::new(),
            sources: BTreeSet::new(),
        }
    }
    #[test]
    fn independent_owner_changes_retire_slots_and_old_copy_revision() {
        let original = owner();
        let mut variants = Vec::new();
        macro_rules! changed {
            ($field:ident, $value:expr) => {{
                let mut v = original.clone();
                v.$field = $value;
                variants.push(v);
            }};
        }
        changed!(serial, 99);
        changed!(epoch, 99);
        changed!(mode, 99);
        changed!(scope, "foreign".into());
        changed!(scope_revision, 99);
        changed!(registry, ("host".into(), vec!["host".into()]));
        changed!(pages, BTreeMap::from([((0, 0, None), 1)]));
        changed!(sources, BTreeSet::from([(0, 0)]));
        let mut v = original.clone();
        v.identity.source.request += 1;
        variants.push(v);
        let mut v = original.clone();
        v.identity.source.storage_generation += 1;
        variants.push(v);
        let mut v = original.clone();
        v.identity.source.note.vault = "foreign".into();
        variants.push(v);
        for variant in variants {
            let mut ui = Ui::default();
            ui.bind(Some(original.clone()));
            ui.slots.insert(
                (0, 0, 1),
                Rc::new(Slot {
                    measurements: Default::default(),
                    allowed: BTreeSet::from([0]),
                }),
            );
            let revision = ui.revision;
            ui.bind(Some(original.clone()));
            assert_eq!(ui.revision, revision);
            assert_eq!(ui.slots.len(), 1);
            ui.bind(Some(variant));
            assert_ne!(ui.revision, revision);
            assert!(ui.slots.is_empty());
        }
        let mut ui = Ui::default();
        ui.bind(Some(original.clone()));
        let first = ui.revision;
        ui.bind(None);
        ui.bind(Some(original));
        assert_ne!(first, ui.revision);
    }
    #[test]
    fn repeated_include_occurrences_have_independent_slots() {
        let mut ui = Ui::default();
        ui.bind(Some(owner()));
        for item in [1, 3] {
            ui.slots.insert(
                (item, 0, 1),
                Rc::new(Slot {
                    measurements: Default::default(),
                    allowed: BTreeSet::from([0]),
                }),
            );
        }
        assert!(!Rc::ptr_eq(&ui.slots[&(1, 0, 1)], &ui.slots[&(3, 0, 1)]));
    }
    #[test]
    fn displayed_unicode_reverse_and_styled_copy_never_uses_source() {
        let source = "Hello **日本語** and `code` 👩‍💻.";
        let b = markdown::parse(source).remove(0);
        assert!(reader_selection::eligible_display(&b));
        let mut state = reader_selection::State::begin(&b.text, b.text.len()).unwrap();
        assert!(state.finish(&b.text, Some(0)));
        assert_eq!(state.payload(&b.text).unwrap(), b.text);
        assert_ne!(b.text, source);
        for source in [
            "A [[Link]]",
            "[target](Target.md)",
            "> quote",
            "- item",
            "![alt](x.png)",
        ] {
            assert!(!reader_selection::eligible_display(
                &markdown::parse(source)[0]
            ));
        }
    }
}
