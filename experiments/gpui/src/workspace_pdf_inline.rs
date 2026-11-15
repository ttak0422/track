//! One accepted local page projected below its authored source label.
use super::*;
impl Workspace {
    pub(crate) fn pdf_layout_source(&self) -> Option<(shell::LinkSource, tabs::TabKey)> {
        let id = self.pdf_identity()?;
        let s = self.reader_pdf.as_ref()?;
        if s.identity != id || s.display.is_none() {
            return None;
        }
        Some((id.source, self.tabs.active()?.key.clone()))
    }
    pub(crate) fn pdf_projection(&self) -> Option<markdown::PdfProjection> {
        let id = self.pdf_identity()?;
        let s = self.reader_pdf.as_ref()?;
        let d = s.display.as_ref()?;
        if !s.inline
            || s.identity != id
            || !reader_pdf::references(&self.content).contains(&(s.block, s.reference.clone()))
        {
            return None;
        }
        Some(markdown::PdfProjection {
            block: s.block,
            reference: s.reference.clone(),
            page: d.page,
            pages: d.pages,
            image: d.image.clone(),
        })
    }
    fn toggle_pdf_inline(
        &mut self,
        id: Identity,
        serial: u64,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || !self.pdf_available(cx)
            || self.pdf_identity().as_ref() != Some(&id)
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
            .reader_pdf
            .as_mut()
            .filter(|s| s.identity == id && s.serial == serial && s.display.is_some())
        else {
            return;
        };
        state.inline = !state.inline;
        let expanded = state.inline;
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
                        && this.pdf_identity().as_ref() == Some(&id)
                        && this.follow_layout_interaction() == interaction
                        && this.pdf_available(cx)
                        && this
                            .follow_layout_anchors()
                            .as_ref()
                            .is_some_and(|a| std::rc::Rc::ptr_eq(a, &anchors))
                        && this.reader_pdf.as_ref().is_some_and(|s| {
                            s.identity == id
                                && s.serial == serial
                                && s.inline == expanded
                                && s.display.is_some()
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
    pub(crate) fn pdf_inline_control(
        &self,
        cx: &Context<Self>,
    ) -> Option<gpui::Stateful<gpui::Div>> {
        let id = self.pdf_identity()?;
        let state = self.reader_pdf.as_ref()?;
        if state.identity != id || state.display.is_none() {
            return None;
        }
        let serial = state.serial;
        let label = if state.inline {
            "Hide PDF in reader"
        } else {
            "Show PDF in reader"
        };
        let control = div().id("pdf-inline-toggle").child(label);
        Some(if self.pdf_available(cx) {
            control
                .cursor_pointer()
                .on_click(cx.listener(move |this, _, window, cx| {
                    this.toggle_pdf_inline(id.clone(), serial, window, cx);
                    cx.stop_propagation();
                }))
        } else {
            control.opacity(0.4)
        })
    }
}
