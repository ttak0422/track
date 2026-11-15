//! One explicitly requested, source-retaining accepted query projection.
use super::*;
#[derive(Default)]
pub(super) struct InlineQuery {
    pub selected: Option<usize>,
    pub epoch: u64,
}
impl Workspace {
    fn inline_query_map(&self) -> Option<std::collections::BTreeMap<usize, usize>> {
        let state = self.shell.includes.as_ref()?;
        if self.includes_identity().as_ref() != Some(&state.identity)
            || !self.includes_body_current(state)
            || self.shell.accepted_title_request != Some(self.request)
        {
            return None;
        }
        let snapshot = state.result.as_ref()?.as_ref().ok()?;
        let map = snapshot.query_positions.as_ref().ok()?.clone();
        // With no includes, the full returned host is one preview item.
        if !matches!(
            snapshot.preview.as_ref().ok()?.items.as_slice(),
            [markdown::PreviewItem::Host(_)]
        ) {
            return None;
        }
        Some(
            map.into_iter()
                .filter(|(_, b)| snapshot.queries.get(&(0, *b)).is_some_and(|v| v.is_ok()))
                .collect(),
        )
    }
    pub(crate) fn query_inline_active(&self) -> bool {
        self.shell
            .includes
            .as_ref()
            .is_some_and(|s| s.query_inline.selected.is_some())
    }
    pub(super) fn query_reader_available(&self, cx: &App) -> bool {
        self.footnote_reader_available(cx)
            && self.pin_keyboard_available(cx)
            && !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.reader_selection_dragging()
            && !self.shell.find.is_composing(cx)
            && !self.shell.calendar_zone.is_composing(cx)
            && !self.inline_includes_active()
    }
    pub(super) fn query_inline_matches(
        &self,
        block: usize,
        epoch: u64,
        key: (usize, usize),
    ) -> bool {
        self.shell.includes.as_ref().is_some_and(|s| {
            !s.preview_open
                && s.query_inline.selected == Some(block)
                && s.query_inline.epoch == epoch
        }) && key.0 == 0
            && self
                .inline_query_map()
                .is_some_and(|m| m.get(&block) == Some(&key.1))
    }
    pub(crate) fn query_layout_source(&self) -> Option<(LinkSource, tabs::TabKey)> {
        if self.inline_query_map()?.is_empty() {
            return None;
        }
        Some((
            self.includes_identity()?.source,
            self.tabs.active()?.key.clone(),
        ))
    }
    fn toggle_query_inline(
        &mut self,
        identity: Identity,
        serial: u64,
        block: usize,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || !self.query_reader_available(cx)
            || self.includes_identity().as_ref() != Some(&identity)
            || !self
                .inline_query_map()
                .is_some_and(|m| m.contains_key(&block))
        {
            return;
        }
        let Some(anchors) = self.follow_layout_anchors() else {
            return;
        };
        let Some(passage) = anchors.passage(window.viewport_size(), window.scale_factor()) else {
            return;
        };
        let Some(state) = self
            .shell
            .includes
            .as_mut()
            .filter(|s| s.accepts(&identity, serial))
        else {
            return;
        };
        if state.query_inline.selected.is_some_and(|v| v != block) {
            return;
        }
        state.query_inline.selected = if state.query_inline.selected.is_some() {
            None
        } else {
            Some(block)
        };
        state.query_inline.epoch = state.query_inline.epoch.wrapping_add(1);
        let epoch = state.query_inline.epoch;
        let selected = state.query_inline.selected;
        state.query_nav = Default::default();
        self.pause_reader_for_overlay();
        self.clear_reader_selection();
        self.clear_reader_hover_owner();
        let interaction = self.follow_layout_interaction();
        let weak = std::rc::Rc::downgrade(&anchors);
        let entity = cx.entity().downgrade();
        anchors.request_after_layout(move |_, window, _| {
            let Some(measured) = weak.upgrade() else {
                return;
            };
            let generation = measured.current_generation();
            window.on_next_frame(move |window, cx| {
                let Some(anchors) = weak.upgrade() else {
                    return;
                };
                let _ = entity.update(cx, |this, cx| {
                    if window.is_window_active()
                        && anchors.current_generation() == generation
                        && this.includes_identity().as_ref() == Some(&identity)
                        && this.follow_layout_interaction() == interaction
                        && this.query_reader_available(cx)
                        && this
                            .follow_layout_anchors()
                            .as_ref()
                            .is_some_and(|a| std::rc::Rc::ptr_eq(a, &anchors))
                        && this.shell.includes.as_ref().is_some_and(|s| {
                            s.accepts(&identity, serial)
                                && s.query_inline.epoch == epoch
                                && s.query_inline.selected == selected
                                && this.includes_body_current(s)
                        })
                        && anchors.restore_passage(
                            &passage,
                            window.viewport_size(),
                            window.scale_factor(),
                        )
                    {
                        cx.notify();
                    }
                });
            });
        });
        cx.notify();
    }
    pub(crate) fn query_inline_sidebar(&self, cx: &Context<Self>) -> Option<Div> {
        let map = self.inline_query_map()?;
        if map.is_empty() {
            return None;
        }
        let state = self.shell.includes.as_ref()?;
        let serial = state.serial;
        let identity = state.identity.clone();
        let selected = state.query_inline.selected;
        let mut controls = div().flex().flex_col().gap_2().child("Query snapshots");
        for (ordinal, block) in map.keys().copied().enumerate() {
            let id = identity.clone();
            let enabled = self.query_reader_available(cx) && selected.is_none_or(|v| v == block);
            let control = div()
                .id(gpui::SharedString::from(format!("inline-query-{block}")))
                .child(format!(
                    "{} query {}",
                    if selected == Some(block) {
                        "Hide"
                    } else {
                        "Show"
                    },
                    ordinal + 1
                ));
            controls = controls.child(if enabled {
                control
                    .cursor_pointer()
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.toggle_query_inline(id.clone(), serial, block, window, cx);
                        cx.stop_propagation();
                    }))
            } else {
                control.opacity(0.4)
            });
        }
        Some(controls)
    }
    pub(crate) fn query_inline_panel(
        &self,
        cx: &Context<Self>,
    ) -> Option<(usize, gpui::AnyElement)> {
        let state = self.shell.includes.as_ref()?;
        let block = state.query_inline.selected?;
        let target = *self.inline_query_map()?.get(&block)?;
        let view = state
            .result
            .as_ref()?
            .as_ref()
            .ok()?
            .queries
            .get(&(0, target))?
            .as_ref()
            .ok()?;
        let mut panel=div().id("inline-query-panel").h(px(360.)).w_full().overflow_y_scroll().p_3().border_1().child("Inline summary · source retained · first 100 rows · Hide before reloading/opening preview");
        if let Some(notice) = self.query_navigation_notice() {
            panel = panel.child(notice);
        }
        let mut count = 0;
        for (group, g) in view.groups.iter().enumerate() {
            if count >= 100 {
                break;
            }
            if let Some(name) = &g.name {
                panel = panel.child(div().child(name.clone()));
            }
            for (row, r) in g.rows.iter().enumerate() {
                if count >= 100 {
                    break;
                }
                count += 1;
                panel = panel.child(
                    div()
                        .mb_2()
                        .child(if r.title.is_empty() {
                            "Untitled".to_owned()
                        } else {
                            r.title.clone()
                        })
                        .child(div().text_size(px(12.)).child(r.cells.join(" · ")))
                        .child(self.query_reader_navigation_control(
                            block,
                            state.query_inline.epoch,
                            (0, target),
                            group,
                            row,
                            cx,
                        )),
                );
            }
        }
        if count == 0 {
            panel = panel.child("No results.");
        }
        if view.total_rows() > count {
            panel = panel.child("More rows are available in the rendered preview.");
        }
        Some((block, panel.into_any_element()))
    }
}
