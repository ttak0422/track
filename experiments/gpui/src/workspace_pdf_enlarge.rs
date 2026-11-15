//! A static, already-accepted page. No bytes, decode, fetch, or PDF interaction.
use super::*;
#[derive(Clone)]
struct Ticket {
    identity: Identity,
    reference: String,
    serial: u64,
    page: u32,
    pages: u32,
    width: u32,
    height: u32,
    image: Arc<gpui::RenderImage>,
}
impl Ticket {
    fn matches(&self, state: &State) -> bool {
        state.matches(&self.identity, &self.reference, self.serial)
            && !state.pending
            && state.display.as_ref().is_some_and(|d| {
                d.page == self.page
                    && d.pages == self.pages
                    && d.width == self.width
                    && d.height == self.height
                    && Arc::ptr_eq(&d.image, &self.image)
            })
    }
}
pub(crate) struct Modal {
    ticket: Ticket,
    token: u64,
    focus: gpui::FocusHandle,
    return_reader: Option<gpui::FocusHandle>,
}
fn fit(width: u32, height: u32, viewport: (f32, f32)) -> (f32, f32) {
    if width == 0 || height == 0 || !viewport.0.is_finite() || !viewport.1.is_finite() {
        return (0., 0.);
    }
    let scale = ((viewport.0 * 0.92 - 24.).max(0.) / width as f32)
        .min((viewport.1 * 0.85 - 90.).max(0.) / height as f32)
        .min(1.);
    (width as f32 * scale, height as f32 * scale)
}
impl Workspace {
    pub(crate) fn enlarged_pdf_open(&self) -> bool {
        self.pdf_enlargement.is_some()
    }
    fn pdf_ticket_current(&self, ticket: &Ticket) -> bool {
        self.pdf_identity().as_ref() == Some(&ticket.identity)
            && self.reader_pdf.as_ref().is_some_and(|s| ticket.matches(s))
    }
    fn open_pdf_enlargement(
        &mut self,
        ticket: Ticket,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || self.enlarged_image_open()
            || !self.pdf_available(cx)
            || !self.pdf_ticket_current(&ticket)
        {
            return;
        }
        self.pause_reader_for_overlay();
        let return_reader = self
            .shell
            .reader_focus
            .as_ref()
            .filter(|f| f.is_focused(window))
            .cloned();
        let focus = cx.focus_handle();
        window.focus(&focus);
        self.pdf_enlargement = Some(Modal {
            ticket,
            token: SERIAL.fetch_add(1, Ordering::Relaxed),
            focus,
            return_reader,
        });
        cx.notify();
    }
    pub(crate) fn pdf_enlarge_control(
        &self,
        cx: &Context<Self>,
    ) -> Option<gpui::Stateful<gpui::Div>> {
        let available = self.pdf_available(cx) && !self.enlarged_image_open();
        let state = self.reader_pdf.as_ref()?;
        let d = state.display.as_ref()?;
        let ticket = Ticket {
            identity: state.identity.clone(),
            reference: state.reference.clone(),
            serial: state.serial,
            page: d.page,
            pages: d.pages,
            width: d.width,
            height: d.height,
            image: d.image.clone(),
        };
        if !self.pdf_ticket_current(&ticket) {
            return None;
        }
        if !available {
            return Some(
                div()
                    .id("enlarge-local-pdf")
                    .opacity(0.4)
                    .child("Enlarge displayed page"),
            );
        }
        Some(
            div()
                .id("enlarge-local-pdf")
                .cursor_pointer()
                .child("Enlarge displayed page")
                .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_click(cx.listener(move |this, _, window, cx| {
                    this.open_pdf_enlargement(ticket.clone(), window, cx);
                    cx.stop_propagation();
                })),
        )
    }
    pub(crate) fn sync_enlarged_pdf(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.sync_pdf();
        if self
            .pdf_enlargement
            .as_ref()
            .is_some_and(|m| !self.pdf_ticket_current(&m.ticket))
        {
            self.dismiss_pdf_enlargement(false, window, cx);
        }
    }
    fn dismiss_pdf_enlargement(
        &mut self,
        restore: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(modal) = self.pdf_enlargement.take() else {
            return;
        };
        if modal.focus.is_focused(window) {
            if restore
                && window.is_window_active()
                && self.pdf_ticket_current(&modal.ticket)
                && self.pdf_available(cx)
            {
                if let Some(focus) = modal.return_reader {
                    window.focus(&focus);
                } else {
                    window.blur();
                }
            } else {
                window.blur();
            }
        }
        cx.notify();
    }
    fn close_pdf_enlargement(&mut self, token: u64, window: &mut Window, cx: &mut Context<Self>) {
        if window.is_window_active()
            && self
                .pdf_enlargement
                .as_ref()
                .is_some_and(|m| m.token == token && m.focus.is_focused(window))
        {
            self.dismiss_pdf_enlargement(true, window, cx);
        }
    }
    pub(crate) fn render_enlarged_pdf(
        &self,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let modal = self.pdf_enlargement.as_ref().unwrap();
        let token = modal.token;
        let viewport = window.viewport_size();
        let (width, height) = fit(
            modal.ticket.width,
            modal.ticket.height,
            (f32::from(viewport.width), f32::from(viewport.height)),
        );
        div()
            .id("enlarged-pdf-backdrop")
            .absolute()
            .inset_0()
            .bg(gpui::rgba(0x000000cc))
            .flex()
            .items_center()
            .justify_center()
            .track_focus(&modal.focus)
            .on_action(|_: &Save, _, cx| cx.stop_propagation())
            .on_action(|_: &CloseTab, _, cx| cx.stop_propagation())
            .on_action(|_: &ShowAbout, _, cx| cx.stop_propagation())
            .on_action(|_: &CopyBuildInfo, _, cx| cx.stop_propagation())
            .on_key_down(
                cx.listener(move |this, event: &gpui::KeyDownEvent, window, cx| {
                    if event.keystroke.key == "escape" {
                        this.close_pdf_enlargement(token, window, cx);
                    }
                    cx.stop_propagation();
                }),
            )
            .on_scroll_wheel(|_, _, cx| cx.stop_propagation())
            .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_up(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_click(cx.listener(move |this, _, window, cx| {
                this.close_pdf_enlargement(token, window, cx);
                cx.stop_propagation();
            }))
            .child(
                div()
                    .id("enlarged-pdf-content")
                    .flex()
                    .flex_col()
                    .items_center()
                    .gap_2()
                    .w(px(
                        (width + 24.).min((f32::from(viewport.width) * 0.92).max(0.))
                    ))
                    .max_h(px((f32::from(viewport.height) * 0.92).max(0.)))
                    .p_3()
                    .bg(rgb(0x191c1e))
                    .text_color(rgb(0xe9e9e4))
                    .on_click(|_, _, cx| cx.stop_propagation())
                    .child(
                        gpui::img(modal.ticket.image.clone())
                            .w(px(width))
                            .h(px(height)),
                    )
                    .child(div().text_size(px(12.)).child(format!(
                        "Page {} / {} · static snapshot",
                        modal.ticket.page, modal.ticket.pages
                    )))
                    .child(
                        div()
                            .id("close-enlarged-pdf")
                            .cursor_pointer()
                            .child("Close")
                            .on_click(cx.listener(move |this, _, window, cx| {
                                this.close_pdf_enlargement(token, window, cx);
                                cx.stop_propagation();
                            })),
                    ),
            )
            .into_any_element()
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn page_fit_keeps_ratio_and_never_upscales() {
        let (w, h) = fit(600, 800, (1200., 1000.));
        assert!((w - 570.).abs() < 0.001);
        assert!((h - 760.).abs() < 0.001);
        let (w, h) = fit(800, 600, (700., 500.));
        assert!(w <= 620. && h <= 335.);
        assert!((w / h - 4. / 3.).abs() < 0.001);
        assert_eq!(fit(2, 2, (2000., 2000.)), (2., 2.));
        assert_eq!(fit(1, 1, (f32::NAN, 200.)), (0., 0.));
    }
    #[test]
    fn ticket_requires_same_page_and_shared_image() {
        let image = || {
            Arc::new(gpui::RenderImage::new([image::Frame::new(
                image::RgbaImage::new(1, 1),
            )]))
        };
        let identity = super::super::tests::identity();
        let shared = image();
        let ticket = Ticket {
            identity: identity.clone(),
            reference: "assets/a.pdf".into(),
            serial: 7,
            page: 1,
            pages: 2,
            width: 1,
            height: 1,
            image: shared.clone(),
        };
        let mut state = State {
            identity,
            reference: "assets/a.pdf".into(),
            block: 0,
            serial: 7,
            cancel: Arc::new(AtomicBool::new(false)),
            pending: false,
            inline: false,
            bytes: None,
            display: Some(Display {
                page: 1,
                pages: 2,
                width: 1,
                height: 1,
                image: shared.clone(),
            }),
            notice: None,
        };
        assert!(ticket.matches(&state));
        assert_eq!(Arc::strong_count(&shared), 3);
        state.pending = true;
        assert!(!ticket.matches(&state));
        state.pending = false;
        state.display.as_mut().unwrap().page = 2;
        assert!(!ticket.matches(&state));
        state.display.as_mut().unwrap().page = 1;
        state.display.as_mut().unwrap().image = image();
        assert!(!ticket.matches(&state));
    }
}
