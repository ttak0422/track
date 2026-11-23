//! Current-reader raster assets only. No URL loader, disk cache, or note mutation.
use super::*;
use std::{
    collections::BTreeMap,
    sync::atomic::{AtomicBool, Ordering},
};
static IMAGE_JOB: AtomicBool = AtomicBool::new(false);
pub(crate) struct Permit;
impl Permit {
    pub(crate) fn acquire() -> Option<Self> {
        IMAGE_JOB
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        IMAGE_JOB.store(false, Ordering::Release);
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    request: u64,
    launch: u64,
    epoch: u64,
    key: live::NoteKey,
    etag: String,
}
#[derive(Default)]
pub(crate) struct State {
    identity: Option<Identity>,
    pub(crate) images: BTreeMap<usize, raster::Decoded>,
    pub(crate) notice: Option<String>,
    modal: Option<Enlarged>,
}
impl Workspace {
    fn image_identity(&self) -> Option<Identity> {
        if self.shell.home
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.editing_ui.active()
        {
            return None;
        }
        let session = self.live.as_ref()?;
        let note = self.current_note.as_ref()?;
        let key = self.current_note_request.as_ref()?;
        let active = self.tabs.active()?;
        if active.key
            != (tabs::TabKey::Note {
                vault: key.vault.clone(),
                note_id: key.note_id,
            })
        {
            return None;
        }
        Some(Identity {
            request: self.request,
            launch: session.client().launch_generation,
            epoch: self.accepted_storage_generation?,
            key: key.clone(),
            etag: note.etag.clone(),
        })
    }
    pub(crate) fn ensure_reader_images(&mut self, cx: &mut Context<Self>) {
        let Some(identity) = self.image_identity() else {
            self.reader_images = State::default();
            return;
        };
        if self.reader_images.identity.as_ref() == Some(&identity) {
            return;
        }
        self.reader_images = State {
            identity: Some(identity.clone()),
            ..Default::default()
        };
        let references = markdown::standalone_images(&self.content);
        if references.is_empty() {
            return;
        }
        let Some(permit) = Permit::acquire() else {
            self.reader_images.notice =
                Some("Raster loading is busy; Reload after the earlier read finishes.".into());
            return;
        };
        self.reader_images.notice = Some("Loading up to four local raster images…".into());
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|_| ())?;
            if before.phase != "ready" || before.generation != captured.epoch {
                return Err(());
            }
            let mut images = BTreeMap::new();
            let mut failed = false;
            for (index, reference) in references {
                match client.raster_asset(&reference, &captured.key.vault) {
                    Ok(image) => {
                        images.insert(index, image);
                    }
                    Err(_) => failed = true,
                }
            }
            let after = client.storage().map_err(|_| ())?;
            if after.phase != "ready" || after.generation != captured.epoch {
                return Err(());
            }
            Ok((images, failed))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.retire_invalid_read(cx) { cx.notify(); return; }
                if this.image_identity().as_ref() != Some(&identity) || this.reader_images.identity.as_ref() != Some(&identity) { return; }
                match result {
                    Ok((images, failed)) => {
                        this.reader_images.images = images;
                        this.reader_images.notice = Some(if failed { "Some raster images could not be loaded; source captions remain. Reload to retry." } else { "Up to four local PNG/JPEG images shown; other image syntax remains literal." }.into());
                    }
                    Err(()) => this.reader_images.notice = Some("Raster images were not accepted after a storage change or read failure. Reload to retry.".into()),
                }
                cx.notify();
            });
        }).detach();
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn raster_identity_is_request_source_and_revision_specific() {
        let base = Identity {
            request: 1,
            launch: 2,
            epoch: 3,
            key: live::NoteKey {
                vault: "work".into(),
                note_id: 42,
            },
            etag: "rev".into(),
        };
        assert_eq!(base, base.clone());
        let mut other = base.clone();
        other.request += 1;
        assert_ne!(base, other);
        let mut other = base.clone();
        other.launch += 1;
        assert_ne!(base, other);
        let mut other = base.clone();
        other.epoch += 1;
        assert_ne!(base, other);
        let mut other = base.clone();
        other.key.vault = "other".into();
        assert_ne!(base, other);
        let mut other = base.clone();
        other.etag = "new".into();
        assert_ne!(base, other);
    }
    #[test]
    fn only_one_bounded_image_job_can_run_and_drop_releases_it() {
        let first = Permit::acquire().unwrap();
        assert!(Permit::acquire().is_none());
        drop(first);
        assert!(Permit::acquire().is_some());
    }
}

// A single window-local owner. The pixels are the accepted reader allocation,
// never another request or decode. Neither this ticket nor its focus is persisted.
#[derive(Clone)]
struct ImageTicket {
    source: Identity,
    block: usize,
    image: std::sync::Arc<gpui::RenderImage>,
    width: u32,
    height: u32,
    caption: String,
}
impl ImageTicket {
    fn matches(
        &self,
        source: Option<&Identity>,
        image: Option<&raster::Decoded>,
        block: Option<&markdown::Block>,
    ) -> bool {
        source == Some(&self.source)
            && image.is_some_and(|image| {
                std::sync::Arc::ptr_eq(&image.image, &self.image)
                    && image.width == self.width
                    && image.height == self.height
            })
            && block.is_some_and(|block| block.image.is_some() && block.text == self.caption)
    }
}
struct Enlarged {
    ticket: ImageTicket,
    focus: gpui::FocusHandle,
    return_reader: Option<gpui::FocusHandle>,
}
fn enlargement_admitted(active: bool, available: bool, current: bool, occupied: bool) -> bool {
    active && available && current && !occupied
}
fn fitted_image(width: u32, height: u32, viewport: (f32, f32)) -> (f32, f32) {
    if width == 0 || height == 0 || !viewport.0.is_finite() || !viewport.1.is_finite() {
        return (0., 0.);
    }
    let scale = ((viewport.0 * 0.92 - 24.).max(0.) / width as f32)
        .min((viewport.1 * 0.92 - 100.).max(0.) / height as f32)
        .min(1.);
    (width as f32 * scale, height as f32 * scale)
}
impl Workspace {
    pub(crate) fn enlarged_image_open(&self) -> bool {
        self.reader_images.modal.is_some() || self.enlarged_pdf_open()
    }
    fn image_ticket_current(&self, ticket: &ImageTicket) -> bool {
        !self.full_graph_active()
            && self.tag_page.is_none()
            && !self.handoff_active()
            && !self.includes_preview_active()
            && !self.partial_read
            && self.current_read_error().is_none()
            && self
                .live
                .as_ref()
                .and_then(|live| live.initial_storage.as_ref())
                .is_some_and(|storage| {
                    storage.phase == "ready" && storage.generation == ticket.source.epoch
                })
            && self.image_identity().as_ref() == Some(&ticket.source)
            && self.reader_images.identity.as_ref() == Some(&ticket.source)
            && ticket.matches(
                self.reader_images.identity.as_ref(),
                self.reader_images.images.get(&ticket.block),
                self.content.get(ticket.block),
            )
    }
    fn enlarge_available(&self, cx: &App) -> bool {
        !self.enlarged_image_open()
            && self.footnote_reader_available(cx)
            && !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && !self.shell.find.is_composing(cx)
            && (!self.session_ui.recovery_enabled()
                || (self.session_ui.recovery_ready()
                    && self.session_ui.recovery_record().is_none()
                    && self.session_ui.recovery_status()
                        == workspace_session::RecoveryStatus::Ready(
                            draft_recovery::LocalStatus::Empty,
                        )))
    }
    pub(crate) fn enlarge_callback(
        &self,
        cx: &Context<Self>,
    ) -> Option<std::rc::Rc<dyn Fn(usize, &mut Window, &mut App)>> {
        if !self.enlarge_available(cx) {
            return None;
        }
        let source = self.image_identity()?;
        let tickets: BTreeMap<_, _> = self
            .reader_images
            .images
            .iter()
            .filter_map(|(&block, image)| {
                let caption = self.content.get(block)?.text.clone();
                Some((
                    block,
                    ImageTicket {
                        source: source.clone(),
                        block,
                        image: image.image.clone(),
                        width: image.width,
                        height: image.height,
                        caption,
                    },
                ))
            })
            .collect();
        let weak = cx.entity().downgrade();
        Some(std::rc::Rc::new(move |block, window, cx| {
            let Some(ticket) = tickets.get(&block) else {
                return;
            };
            let _ = weak.update(cx, |this, cx| {
                if !enlargement_admitted(
                    window.is_window_active(),
                    this.enlarge_available(cx),
                    this.image_ticket_current(ticket),
                    this.enlarged_image_open(),
                ) {
                    return;
                }
                // Retire queued reader positioning, not the reader's current offset.
                this.pause_reader_for_overlay();
                let return_reader = this
                    .shell
                    .reader_focus
                    .as_ref()
                    .filter(|f| f.is_focused(window))
                    .cloned();
                let focus = cx.focus_handle();
                window.focus(&focus);
                this.reader_images.modal = Some(Enlarged {
                    ticket: ticket.clone(),
                    focus,
                    return_reader,
                });
                cx.notify();
            });
        }))
    }
    pub(crate) fn sync_enlarged_image(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.sync_enlarged_pdf(window, cx);
        if self
            .reader_images
            .modal
            .as_ref()
            .is_some_and(|modal| !self.image_ticket_current(&modal.ticket))
        {
            self.dismiss_enlarged_image(false, window, cx);
        }
    }
    fn dismiss_enlarged_image(
        &mut self,
        restore: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Some(modal) = self.reader_images.modal.take() else {
            return;
        };
        // Never replace a newer input owner or restore a focus from another source.
        if modal.focus.is_focused(window) {
            if restore
                && window.is_window_active()
                && self.image_ticket_current(&modal.ticket)
                && self.enlarge_available(cx)
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
    pub(crate) fn render_enlarged_image(
        &self,
        window: &Window,
        cx: &mut Context<Self>,
    ) -> impl IntoElement {
        if self.enlarged_pdf_open() {
            return self.render_enlarged_pdf(window, cx);
        }
        let modal = self.reader_images.modal.as_ref().unwrap();
        let viewport = window.viewport_size();
        let (width, height) = fitted_image(
            modal.ticket.width,
            modal.ticket.height,
            (f32::from(viewport.width), f32::from(viewport.height)),
        );
        div()
            .id("enlarged-image-backdrop")
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
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                if event.keystroke.key == "escape" {
                    this.dismiss_enlarged_image(true, window, cx);
                }
                cx.stop_propagation();
            }))
            .on_scroll_wheel(|_, _, cx| cx.stop_propagation())
            .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_mouse_up(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_click(cx.listener(|this, _, window, cx| {
                this.dismiss_enlarged_image(true, window, cx);
                cx.stop_propagation();
            }))
            .child(
                div()
                    .id("enlarged-image-content")
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
                    .child(
                        div()
                            .id("enlarged-image-caption")
                            .max_h(px(40.))
                            .overflow_y_scroll()
                            .text_size(px(12.))
                            .child(modal.ticket.caption.clone()),
                    )
                    .child(
                        div()
                            .id("close-enlarged-image")
                            .cursor_pointer()
                            .child("Close")
                            .on_click(cx.listener(|this, _, window, cx| {
                                this.dismiss_enlarged_image(true, window, cx);
                                cx.stop_propagation();
                            })),
                    ),
            )
            .into_any_element()
    }
}

#[cfg(test)]
mod enlargement_tests {
    use super::*;
    #[test]
    fn only_one_current_active_unblocked_owner_is_admitted() {
        assert!(enlargement_admitted(true, true, true, false));
        for flags in [
            (false, true, true, false),
            (true, false, true, false),
            (true, true, false, false),
            (true, true, true, true),
        ] {
            assert!(!enlargement_admitted(flags.0, flags.1, flags.2, flags.3));
        }
    }
    #[test]
    fn fit_is_aspect_preserving_bounded_and_never_upscales() {
        for (w, h) in [(2000, 300), (300, 2000), (300, 300)] {
            for viewport in [(1000., 800.), (260., 600.), (600., 200.)] {
                let fit = fitted_image(w, h, viewport);
                assert!(fit.0 <= w as f32 && fit.1 <= h as f32);
                assert!(fit.0 <= (viewport.0 * 0.92 - 24.).max(0.) + 0.001);
                assert!(fit.1 <= (viewport.1 * 0.92 - 100.).max(0.) + 0.001);
                assert!((fit.0 / fit.1 - w as f32 / h as f32).abs() < 0.001);
            }
        }
        assert_eq!(fitted_image(10, 20, (1000., 800.)), (10., 20.));
    }
    #[test]
    fn invalid_or_tiny_viewport_never_creates_invalid_dimensions() {
        for viewport in [
            (0., 0.),
            (-1., -1.),
            (20., 20.),
            (f32::NAN, 400.),
            (400., f32::INFINITY),
        ] {
            assert_eq!(fitted_image(200, 100, viewport), (0., 0.));
        }
        assert_eq!(fitted_image(0, 100, (400., 500.)), (0., 0.));
    }
}

#[cfg(test)]
mod enlargement_identity_tests {
    use super::*;
    #[test]
    fn stale_source_replaced_pixels_missing_or_changed_block_are_rejected() {
        let pixels = raster::tests::png(2, 3);
        let image = raster::decode(&pixels, "image/png").unwrap();
        let replacement = raster::decode(&pixels, "image/png").unwrap();
        let blocks = markdown::parse("![literal caption](assets/a.png)");
        let source = Identity {
            request: 1,
            launch: 2,
            epoch: 3,
            key: live::NoteKey {
                vault: "work".into(),
                note_id: 42,
            },
            etag: "revision".into(),
        };
        let ticket = ImageTicket {
            source: source.clone(),
            block: 0,
            image: image.image.clone(),
            width: 2,
            height: 3,
            caption: blocks[0].text.clone(),
        };
        assert!(ticket.matches(Some(&source), Some(&image), blocks.first()));
        assert!(!ticket.matches(Some(&source), Some(&replacement), blocks.first()));
        assert!(!ticket.matches(Some(&source), None, blocks.first()));
        assert!(!ticket.matches(Some(&source), Some(&image), None));
        assert!(!ticket.matches(
            Some(&source),
            Some(&image),
            markdown::parse("![changed](assets/a.png)").first()
        ));
        let mut changes = vec![source.clone(); 6];
        changes[0].request += 1;
        changes[1].launch += 1;
        changes[2].epoch += 1;
        changes[3].etag.push('x');
        changes[4].key.vault.push('x');
        changes[5].key.note_id += 1;
        for changed in changes {
            assert!(!ticket.matches(Some(&changed), Some(&image), blocks.first()));
        }
        assert!(!ticket.matches(None, Some(&image), blocks.first()));
    }
}
