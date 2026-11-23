//! Explicit read-only local PDF inspector. No document actions or persistent cache.
#[path = "workspace_pdf_enlarge.rs"]
mod enlarge;
#[path = "workspace_pdf_inline.rs"]
mod inline;
use super::*;
pub(crate) use enlarge::Modal;
use std::sync::{
    Arc,
    atomic::{AtomicBool, AtomicU64, Ordering},
};
static JOB: AtomicBool = AtomicBool::new(false);
static SERIAL: AtomicU64 = AtomicU64::new(1);
struct Permit;
impl Permit {
    fn acquire() -> Option<Self> {
        JOB.compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        JOB.store(false, Ordering::Release);
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    source: shell::LinkSource,
    route: live::NoteKey,
    scope: String,
    scope_revision: u64,
    registry: Vec<String>,
    canonical: String,
}
struct Display {
    page: u32,
    pages: u32,
    width: u32,
    height: u32,
    image: Arc<gpui::RenderImage>,
}
pub(crate) struct State {
    identity: Identity,
    reference: String,
    block: usize,
    serial: u64,
    cancel: Arc<AtomicBool>,
    pending: bool,
    inline: bool,
    bytes: Option<Arc<Vec<u8>>>,
    display: Option<Display>,
    notice: Option<String>,
}
impl Drop for State {
    fn drop(&mut self) {
        self.cancel.store(true, Ordering::Release);
    }
}
impl State {
    fn matches(&self, identity: &Identity, reference: &str, serial: u64) -> bool {
        self.identity == *identity
            && self.reference == reference
            && self.serial == serial
            && !self.cancel.load(Ordering::Acquire)
    }
}
fn accepted_request(accepted: Option<u64>, current: u64) -> bool {
    accepted == Some(current)
}
impl Workspace {
    fn pdf_identity(&self) -> Option<Identity> {
        if !accepted_request(self.shell.accepted_title_request, self.request)
            || self.shell.home
            || self.read_error.is_some()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.editing_ui.active()
            || self.includes_preview_active()
        {
            return None;
        }
        let source = self.current_link_source()?;
        let route = self.current_note_request.as_ref()?.clone();
        let note = self.current_note.as_ref()?;
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if source.note != route
            || note.note.key() != route
            || storage.phase != "ready"
            || storage.generation != source.storage_generation
        {
            return None;
        }
        if self.tabs.active()?.key
            != (tabs::TabKey::Note {
                vault: route.vault.clone(),
                note_id: route.note_id,
            })
        {
            return None;
        }
        let (launch, registry) = self.follow_registry()?;
        let canonical = if source.note.vault.is_empty() {
            launch.clone()
        } else {
            source.note.vault.clone()
        };
        let route_vault = if route.vault.is_empty() {
            &launch
        } else {
            &route.vault
        };
        if source.note.note_id != route.note_id || route_vault != &canonical {
            return None;
        }
        if !registry.contains(&canonical) || !self.session_ui.permits(&canonical) {
            return None;
        }
        Some(Identity {
            source,
            route,
            scope: self.working_vault_scope().to_owned(),
            scope_revision: self.shell.scope_revision,
            registry,
            canonical,
        })
    }
    fn pdf_available(&self, cx: &App) -> bool {
        self.footnote_reader_available(cx)
            && !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.shell.find.is_composing(cx)
            && !self.reader_selection_dragging()
    }
    pub(crate) fn sync_pdf(&mut self) {
        let identity = self.pdf_identity();
        if self.reader_pdf.as_ref().is_some_and(|s| {
            identity.as_ref() != Some(&s.identity)
                || !reader_pdf::references(&self.content).contains(&(s.block, s.reference.clone()))
        }) {
            self.reader_pdf = None;
        }
    }
    fn load_pdf(
        &mut self,
        identity: Identity,
        block: usize,
        reference: String,
        page: u32,
        expected: Option<u64>,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || self.pdf_identity().as_ref() != Some(&identity)
            || !self.pdf_available(cx)
            || !reader_pdf::references(&self.content).contains(&(block, reference.clone()))
        {
            return;
        }
        if self.reader_pdf.as_ref().is_some_and(|s| {
            reader_pdf::switch_blocked(s.inline, (s.block, &s.reference), (block, &reference))
        }) {
            return;
        }
        if let Some(serial) = expected {
            if !self.reader_pdf.as_ref().is_some_and(|s| {
                s.matches(&identity, &reference, serial)
                    && !s.pending
                    && s.display
                        .as_ref()
                        .is_some_and(|d| page > 0 && page <= d.pages)
            }) {
                return;
            }
        }
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        let cancel = Arc::new(AtomicBool::new(false));
        let mut old = self.reader_pdf.take();
        let same = old
            .as_ref()
            .is_some_and(|s| s.identity == identity && s.reference == reference);
        let bytes = if same {
            old.as_mut().and_then(|s| s.bytes.take())
        } else {
            None
        };
        let display = if same {
            old.as_mut().and_then(|s| s.display.take())
        } else {
            None
        };
        let inline = same && old.as_ref().is_some_and(|s| s.inline);
        drop(old);
        self.reader_pdf = Some(State {
            identity: identity.clone(),
            reference: reference.clone(),
            block,
            serial,
            cancel: cancel.clone(),
            pending: false,
            inline,
            bytes: bytes.clone(),
            display,
            notice: None,
        });
        #[cfg(not(target_os = "macos"))]
        {
            self.reader_pdf.as_mut().unwrap().notice = Some(
                "Local PDF rendering is supported on macOS only; source remains unchanged.".into(),
            );
            cx.notify();
            return;
        }
        #[cfg(target_os = "macos")]
        {
            let Some(permit) = Permit::acquire() else {
                self.reader_pdf.as_mut().unwrap().notice =
                    Some("PDF renderer is busy; Load again after the earlier request ends.".into());
                cx.notify();
                return;
            };
            self.reader_pdf.as_mut().unwrap().pending = true;
            let client = self.live.as_ref().unwrap().client();
            let captured = identity.clone();
            let asset = reference.clone();
            let job = cx.background_executor().spawn(async move {
                let _permit = permit;
                let check = || -> Result<(), String> {
                    if cancel.load(Ordering::Acquire) {
                        return Err("PDF cancelled".into());
                    }
                    let storage = client.storage().map_err(|_| "PDF storage unavailable")?;
                    if storage.phase != "ready"
                        || storage.generation != captured.source.storage_generation
                    {
                        return Err("PDF storage changed".into());
                    }
                    Ok(())
                };
                check()?;
                let bytes = match bytes {
                    Some(bytes) => bytes,
                    None => Arc::new(
                        client
                            .pdf_asset(&asset, &captured.canonical)
                            .map_err(|_| "Local PDF asset could not be read")?,
                    ),
                };
                check()?;
                let frame = pdf_probe::run(&bytes, page, pdf_probe::MAX_EDGE, &cancel)?;
                check()?;
                Ok::<_, String>((bytes, frame))
            });
            cx.spawn(async move |this, cx| {
                let result = job.await;
                let _ = this.update(cx, |this, cx| {
                    if this.retire_invalid_read(cx) {
                        cx.notify();
                        return;
                    }
                    if this.pdf_identity().as_ref() != Some(&identity)
                        || !this
                            .reader_pdf
                            .as_ref()
                            .is_some_and(|s| s.matches(&identity, &reference, serial))
                    {
                        return;
                    }
                    let state = this.reader_pdf.as_mut().unwrap();
                    state.pending = false;
                    match result {
                        Ok((bytes, frame)) => {
                            if let Some(buffer) =
                                image::RgbaImage::from_raw(frame.width, frame.height, frame.bgra)
                            {
                                state.bytes = Some(bytes);
                                state.display = Some(Display {
                                    page: frame.page,
                                    pages: frame.pages,
                                    width: frame.width,
                                    height: frame.height,
                                    image: Arc::new(gpui::RenderImage::new([image::Frame::new(
                                        buffer,
                                    )])),
                                });
                                state.notice = None;
                            } else {
                                state.notice = Some("PDF raster rejected".into());
                            }
                        }
                        Err(error) => state.notice = Some(error),
                    };
                    cx.notify();
                });
            })
            .detach();
            cx.notify();
        }
    }
    pub(crate) fn pdf_inspector(&self, window: &Window, cx: &Context<Self>) -> Option<gpui::Div> {
        let identity = self.pdf_identity()?;
        let refs = reader_pdf::references(&self.content);
        if refs.is_empty() {
            return None;
        }
        let mut view = div()
            .mt_6()
            .flex()
            .flex_col()
            .gap_2()
            .text_size(px(12.))
            .child("LOCAL PDF · read-only");
        for (block, reference) in refs {
            let blocked = self.reader_pdf.as_ref().is_some_and(|s| {
                reader_pdf::switch_blocked(s.inline, (s.block, &s.reference), (block, &reference))
            });
            let source = identity.clone();
            let label = reference.chars().take(100).collect::<String>();
            view = view.child(
                div()
                    .id(("load-local-pdf", block))
                    .cursor_pointer()
                    .opacity(if blocked { 0.4 } else { 1. })
                    .child(if blocked {
                        format!("Hide reader PDF first: {label}")
                    } else {
                        format!("Load {label}")
                    })
                    .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.load_pdf(
                            source.clone(),
                            block,
                            reference.clone(),
                            1,
                            None,
                            window,
                            cx,
                        );
                        cx.stop_propagation();
                    })),
            );
        }
        if let Some(state) = self.reader_pdf.as_ref().filter(|s| s.identity == identity) {
            let serial = state.serial;
            let source = identity.clone();
            view = view.child(
                div()
                    .id("close-local-pdf")
                    .cursor_pointer()
                    .child(if state.inline {
                        "Hide reader PDF before closing"
                    } else {
                        "Close PDF"
                    })
                    .opacity(if state.inline { 0.4 } else { 1. })
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if window.is_window_active()
                            && this.pdf_available(cx)
                            && this.pdf_identity().as_ref() == Some(&source)
                            && this
                                .reader_pdf
                                .as_ref()
                                .is_some_and(|s| s.serial == serial && !s.inline)
                        {
                            this.reader_pdf = None;
                            cx.notify();
                        }
                        cx.stop_propagation();
                    })),
            );
            view = view.when_some(self.pdf_inline_control(cx), |view, control| {
                view.child(control)
            });
            if state.pending {
                view = view.child("Loading PDF page…");
            }
            if let Some(notice) = &state.notice {
                view = view.child(notice.clone());
            }
            view = view.when_some(self.pdf_enlarge_control(cx), |view, control| {
                view.child(control)
            });
            if let Some(display) = &state.display {
                view = view.child(format!("Page {} / {}", display.page, display.pages));
                let width =
                    (f32::from(window.viewport_size().width) * 0.24).clamp(240., 380.) - 24.;
                let scale = (width / display.width as f32)
                    .min((f32::from(window.viewport_size().height) * 0.6) / display.height as f32)
                    .min(1.);
                view = view.child(
                    gpui::img(display.image.clone())
                        .w(px(display.width as f32 * scale))
                        .h(px(display.height as f32 * scale)),
                );
                let mut buttons = div().flex().gap_3();
                for (label, page, enabled) in [
                    ("Previous", display.page.saturating_sub(1), display.page > 1),
                    ("Next", display.page + 1, display.page < display.pages),
                ] {
                    let source = identity.clone();
                    let reference = state.reference.clone();
                    let block = state.block;
                    let button = div()
                        .id(gpui::SharedString::from(format!("pdf-{label}")))
                        .child(label);
                    buttons = buttons.child(if enabled && !state.pending {
                        button
                            .cursor_pointer()
                            .on_click(cx.listener(move |this, _, window, cx| {
                                this.load_pdf(
                                    source.clone(),
                                    block,
                                    reference.clone(),
                                    page,
                                    Some(serial),
                                    window,
                                    cx,
                                );
                                cx.stop_propagation();
                            }))
                    } else {
                        button.opacity(0.4)
                    });
                }
                view = view.child(buttons);
            }
        }
        Some(view.child("Up to 8 local attachments; one PDF active. Fixed-resolution raster only, no text selection or PDF actions."))
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn reload_requires_new_accepted_body_before_pdf_admission() {
        assert!(accepted_request(Some(4), 4));
        assert!(!accepted_request(Some(4), 5));
        assert!(!accepted_request(None, 5));
        assert!(accepted_request(Some(5), 5));
        let mut reloaded = identity();
        reloaded.source.request = 5;
        assert_ne!(identity(), reloaded);
    }
    pub(super) fn identity() -> Identity {
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
            scope: "work".into(),
            scope_revision: 5,
            registry: vec!["work".into()],
            canonical: "work".into(),
        }
    }
    #[test]
    fn source_scope_and_generation_are_authority() {
        let base = identity();
        for field in 0..8 {
            let mut changed = base.clone();
            match field {
                0 => changed.source.request += 1,
                1 => changed.source.etag.push('x'),
                2 => changed.source.storage_generation += 1,
                3 => changed.source.launch_generation += 1,
                4 => changed.scope_revision += 1,
                5 => changed.canonical = "other".into(),
                6 => changed.registry.clear(),
                _ => changed.route.note_id += 1,
            }
            assert_ne!(base, changed);
        }
    }
    #[test]
    fn teardown_cancels_and_old_page_ticket_cannot_win() {
        let cancel = Arc::new(AtomicBool::new(false));
        let state = State {
            identity: identity(),
            reference: "assets/a.pdf".into(),
            block: 0,
            serial: 7,
            cancel: cancel.clone(),
            pending: true,
            inline: false,
            bytes: None,
            display: None,
            notice: None,
        };
        assert!(state.matches(&identity(), "assets/a.pdf", 7));
        assert!(!state.matches(&identity(), "assets/a.pdf", 8));
        assert!(!state.matches(&identity(), "assets/b.pdf", 7));
        drop(state);
        assert!(cancel.load(Ordering::Acquire));
    }
}
