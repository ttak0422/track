//! Explicit local display-math snapshots. No network, paths, or document writes.
use super::*;
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

pub(crate) struct State {
    identity: Identity,
    reference: markdown::MathReference,
    serial: u64,
    cancel: Arc<AtomicBool>,
    pending: bool,
    inline: bool,
    image: Option<Arc<gpui::RenderImage>>,
    notice: Option<String>,
}
impl Drop for State {
    fn drop(&mut self) {
        self.cancel.store(true, Ordering::Release);
    }
}
fn accepted_request(accepted: Option<u64>, current: u64) -> bool {
    accepted == Some(current)
}
fn dimensions_allowed(width: u32, height: u32) -> bool {
    width > 0
        && height > 0
        && width <= 1536
        && height <= 1536
        && u64::from(width) * u64::from(height) <= 2_000_000
}
fn png_reader(bytes: &[u8]) -> image::ImageReader<std::io::Cursor<&[u8]>> {
    let mut reader =
        image::ImageReader::with_format(std::io::Cursor::new(bytes), image::ImageFormat::Png);
    let mut limits = image::Limits::default();
    limits.max_image_width = Some(1536);
    limits.max_image_height = Some(1536);
    limits.max_alloc = Some(8 * 1024 * 1024);
    reader.limits(limits);
    reader
}
fn decode_png(bytes: &[u8]) -> Result<Arc<gpui::RenderImage>, String> {
    if bytes.len() > 2 * 1024 * 1024 || !bytes.starts_with(b"\x89PNG\r\n\x1a\n") {
        return Err("Math raster rejected".into());
    }
    let (w, h) = png_reader(bytes)
        .into_dimensions()
        .map_err(|_| "Math raster dimensions unavailable")?;
    if !dimensions_allowed(w, h) {
        return Err("Math raster dimensions exceed limits".into());
    }
    let mut buffer = png_reader(bytes)
        .decode()
        .map_err(|_| "Math raster decode failed")?
        .into_rgba8();
    if buffer.dimensions() != (w, h) {
        return Err("Math raster dimensions changed".into());
    }
    for pixel in buffer.pixels_mut() {
        pixel.0.swap(0, 2);
    }
    Ok(Arc::new(gpui::RenderImage::new([image::Frame::new(
        buffer,
    )])))
}
fn occurrence_current(
    serial: u64,
    expected: u64,
    actual: &markdown::MathReference,
    captured: &markdown::MathReference,
    cancelled: bool,
) -> bool {
    serial == expected && actual == captured && !cancelled
}

impl Workspace {
    fn math_identity(&self) -> Option<Identity> {
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
    fn math_available(&self, cx: &App) -> bool {
        self.footnote_reader_available(cx)
            && !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.shell.find.is_composing(cx)
            && !self.reader_selection_dragging()
    }

    fn math_references(&self) -> Vec<markdown::MathReference> {
        self.current_note
            .as_ref()
            .map(|n| markdown::display_math_references(&n.body, &self.content))
            .unwrap_or_default()
    }
    pub(crate) fn sync_math(&mut self) {
        let id = self.math_identity();
        let refs = self.math_references();
        if self
            .reader_math
            .as_ref()
            .is_some_and(|s| id.as_ref() != Some(&s.identity) || !refs.contains(&s.reference))
        {
            self.reader_math = None;
        }
    }
    pub(crate) fn math_layout_source(&self) -> Option<(shell::LinkSource, tabs::TabKey)> {
        let id = self.math_identity()?;
        let s = self.reader_math.as_ref()?;
        (s.identity == id && s.image.is_some())
            .then(|| (id.source, self.tabs.active().unwrap().key.clone()))
    }
    pub(crate) fn math_projection(&self) -> Option<markdown::MathProjection> {
        let id = self.math_identity()?;
        let s = self.reader_math.as_ref()?;
        if !s.inline || s.identity != id || !self.math_references().contains(&s.reference) {
            return None;
        }
        Some(markdown::MathProjection {
            block: s.reference.block,
            source_text: s.reference.source_text.clone(),
            image: s.image.clone()?,
        })
    }
    fn render_math(
        &mut self,
        id: Identity,
        reference: markdown::MathReference,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || !self.math_available(cx)
            || self.math_identity().as_ref() != Some(&id)
            || !self.math_references().contains(&reference)
            || self.reader_math.as_ref().is_some_and(|s| s.inline)
        {
            return;
        }
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        let cancel = Arc::new(AtomicBool::new(false));
        self.reader_math = Some(State {
            identity: id.clone(),
            reference: reference.clone(),
            serial,
            cancel: cancel.clone(),
            pending: false,
            inline: false,
            image: None,
            notice: None,
        });
        let Some(permit) = Permit::acquire() else {
            self.reader_math.as_mut().unwrap().notice =
                Some("Math renderer busy; retry after the previous job ends.".into());
            cx.notify();
            return;
        };
        self.reader_math.as_mut().unwrap().pending = true;
        let formula = reference.formula.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let bytes = crate::math_probe::run(formula.as_bytes(), &cancel)?;
            if cancel.load(Ordering::Acquire) {
                return Err("Math cancelled".into());
            }
            decode_png(&bytes)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.math_identity().as_ref() != Some(&id)
                    || !this.math_references().contains(&reference)
                    || !this.reader_math.as_ref().is_some_and(|s| {
                        s.identity == id
                            && occurrence_current(
                                s.serial,
                                serial,
                                &s.reference,
                                &reference,
                                s.cancel.load(Ordering::Acquire),
                            )
                    })
                {
                    return;
                }
                let s = this.reader_math.as_mut().unwrap();
                s.pending = false;
                match result {
                    Ok(decoded) => {
                        s.image = Some(decoded);
                        s.notice = None;
                    }
                    Err(error) => s.notice = Some(error),
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    fn toggle_math(
        &mut self,
        id: Identity,
        serial: u64,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || !self.math_available(cx)
            || self.math_identity().as_ref() != Some(&id)
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
            .reader_math
            .as_mut()
            .filter(|s| s.identity == id && s.serial == serial && s.image.is_some())
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
                        && this.math_identity().as_ref() == Some(&id)
                        && this.follow_layout_interaction() == interaction
                        && this.math_available(cx)
                        && this
                            .follow_layout_anchors()
                            .as_ref()
                            .is_some_and(|a| std::rc::Rc::ptr_eq(a, &anchors))
                        && this.reader_math.as_ref().is_some_and(|s| {
                            s.identity == id
                                && s.serial == serial
                                && s.inline == expanded
                                && s.image.is_some()
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

    pub(crate) fn math_inspector(&self, cx: &Context<Self>) -> Option<gpui::Div> {
        let id = self.math_identity()?;
        let refs = self.math_references();
        if refs.is_empty() {
            return None;
        }
        let mut view = div()
            .mt_6()
            .flex()
            .flex_col()
            .gap_2()
            .text_size(px(12.))
            .child("DISPLAY MATH · local · source retained");
        for reference in refs {
            let source = id.clone();
            let blocked = self.reader_math.as_ref().is_some_and(|s| s.inline);
            view = view.child(
                div()
                    .id(("render-display-math", reference.block))
                    .opacity(if blocked { 0.4 } else { 1. })
                    .child(format!("Render math · block {}", reference.block + 1))
                    .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.render_math(source.clone(), reference.clone(), window, cx);
                        cx.stop_propagation();
                    })),
            );
        }
        // Stable footprint even when a job finishes or is closed, including
        // narrow layouts where the inspector belongs to the reader scroller.
        let mut footer = div()
            .id("math-status-controls")
            .h(px(160.))
            .flex_shrink_0()
            .overflow_y_scroll()
            .flex()
            .flex_col()
            .gap_2();
        if let Some(s) = self.reader_math.as_ref().filter(|s| s.identity == id) {
            let serial = s.serial;
            let source = id.clone();
            footer = footer.child(
                div()
                    .id("close-display-math")
                    .child(if s.inline {
                        "Hide math before closing"
                    } else {
                        "Close math"
                    })
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if window.is_window_active()
                            && this.math_available(cx)
                            && this.math_identity().as_ref() == Some(&source)
                            && this
                                .reader_math
                                .as_ref()
                                .is_some_and(|s| s.serial == serial && !s.inline)
                        {
                            this.reader_math = None;
                            cx.notify();
                        }
                        cx.stop_propagation();
                    })),
            );
            if s.image.is_some() {
                let source = id.clone();
                footer = footer.child(
                    div()
                        .id("toggle-display-math")
                        .child(if s.inline {
                            "Hide math in reader"
                        } else {
                            "Show math in reader"
                        })
                        .on_click(cx.listener(move |this, _, window, cx| {
                            this.toggle_math(source.clone(), serial, window, cx);
                            cx.stop_propagation();
                        })),
                );
            }
            if s.pending {
                footer = footer.child("Rendering locally…");
            }
            if let Some(notice) = &s.notice {
                footer = footer.child(notice.clone());
            }
        }
        Some(view.child(footer))
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn raster_bounds_and_request_ownership() {
        assert!(dimensions_allowed(1200, 1000));
        assert!(!dimensions_allowed(1536, 1536));
        assert!(!dimensions_allowed(0, 1));
        assert!(!dimensions_allowed(1537, 1));
        assert!(accepted_request(Some(7), 7));
        assert!(!accepted_request(Some(6), 7));
        assert!(!accepted_request(None, 7));
        assert!(decode_png(b"not png").is_err());
        assert!(decode_png(&vec![0; 2 * 1024 * 1024 + 1]).is_err());
    }
}
#[cfg(test)]
mod occurrence_tests {
    use super::*;
    #[test]
    fn duplicate_formulas_do_not_share_occurrence_or_old_completion() {
        let a = markdown::MathReference {
            block: 1,
            formula: "x^2".into(),
            source_text: "$$x^2$$".into(),
        };
        let mut b = a.clone();
        b.block = 3;
        assert!(occurrence_current(7, 7, &a, &a, false));
        assert!(!occurrence_current(8, 7, &a, &a, false));
        assert!(!occurrence_current(7, 7, &b, &a, false));
        assert!(!occurrence_current(7, 7, &a, &a, true));
        b = a.clone();
        b.formula = "x^3".into();
        assert!(!occurrence_current(7, 7, &b, &a, false));
    }
}
