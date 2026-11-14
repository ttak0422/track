//! Ephemeral external documents never enter the vault reader's state or caches.
use super::*;
use gpui::{Div, Stateful};
use markdown_handoff::{Capability, Snapshot};

#[derive(Default)]
pub(crate) struct HandoffUi {
    open: bool,
    launch: Option<u64>,
    serial: u64,
    // Kept even when the surface closes: a canceled consume may still be running.
    in_flight: Option<u64>,
    capability: Option<Capability>,
    snapshot: Option<Snapshot>,
    selected: String,
    blocks: Vec<markdown::Block>,
    page: usize,
    selecting: bool,
    selection_serial: u64,
    message: String,
    focus: Option<gpui::FocusHandle>,
    scroll: gpui::ScrollHandle,
}
impl HandoffUi {
    fn close(&mut self) {
        self.open = false;
        self.serial = self.serial.wrapping_add(1);
        self.capability = None;
        self.snapshot = None;
        self.blocks.clear();
        self.selected.clear();
        self.message.clear();
        self.page = 0;
        self.selecting = false;
        self.selection_serial = self.selection_serial.wrapping_add(1);
        self.scroll = gpui::ScrollHandle::new();
    }
    fn accepts(&self, serial: u64, launch: u64) -> bool {
        self.open && self.serial == serial && self.launch == Some(launch)
    }

    fn begin_consume(&mut self) -> u64 {
        self.serial = self.serial.wrapping_add(1);
        self.selection_serial = self.selection_serial.wrapping_add(1);
        self.selecting = false;
        self.in_flight = Some(self.serial);
        self.serial
    }
    fn selection_available(&self) -> bool {
        self.open && !self.selecting && self.in_flight.is_none()
    }
    fn complete_selection(
        &mut self,
        selection: u64,
        serial: u64,
        launch: u64,
        path: String,
        result: Result<Vec<markdown::Block>, String>,
    ) -> bool {
        if self.selection_serial != selection {
            return false;
        }
        self.selecting = false;
        if !self.accepts(serial, launch) || self.in_flight.is_some() {
            return false;
        }
        match result {
            Ok(blocks) => {
                self.blocks = blocks;
                self.selected = path;
                self.scroll = gpui::ScrollHandle::new();
                self.message.clear();
            }
            Err(_) => {
                self.message =
                    "Document is too complex to render safely; selection unchanged.".into()
            }
        }
        true
    }
    fn complete(
        &mut self,
        serial: u64,
        launch: u64,
        result: Result<(Snapshot, Vec<markdown::Block>), String>,
    ) -> bool {
        if !self.accepts(serial, launch) {
            return false;
        }
        match result {
            Ok((snapshot, blocks)) => {
                self.selection_serial = self.selection_serial.wrapping_add(1);
                self.selecting = false;
                self.selected = snapshot.active.clone();
                self.blocks = blocks;
                self.snapshot = Some(snapshot);
                self.page = 0;
                self.scroll = gpui::ScrollHandle::new();
                self.message.clear();
            }
            Err(error) => {
                self.message = format!(
                    "{error} The handoff may have been consumed. Run track markdown again. Existing snapshot, if any, is unchanged."
                )
            }
        }
        true
    }
}
impl Workspace {
    fn select_handoff(&mut self, path: &str, cx: &mut Context<Self>) {
        if !self.handoff_gate() || !self.shell.handoff.selection_available() {
            return;
        }
        let state = &mut self.shell.handoff;
        let Some(file) = state.snapshot.as_ref().and_then(|s| s.selected(path)) else {
            return;
        };
        let path = file.path.clone();
        let source = file.text.clone();
        let serial = state.serial;
        let Some(launch) = state.launch else { return };
        state.selecting = true;
        state.selection_serial = state.selection_serial.wrapping_add(1);
        let selection = state.selection_serial;
        state.message = "Preparing selected document…".into();
        let job = cx
            .background_executor()
            .spawn(async move { markdown::preview_blocks(&source) });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                let current = this.handoff_gate()
                    && this.live.as_ref().map(|s| s.client().launch_generation) == Some(launch);
                let state = &mut this.shell.handoff;
                if current {
                    state.complete_selection(selection, serial, launch, path, result);
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn handoff_active(&self) -> bool {
        self.shell.handoff.open
    }
    fn handoff_gate(&self) -> bool {
        !self.editing_ui.active()
            && !self.editing_ui.has_pending()
            && !self.quit_session.is_closing()
            && self.quit_session.failure().is_none()
            && self.live.as_ref().is_some_and(|s| s.client().is_alive())
    }
    pub(crate) fn reset_stale_handoff(&mut self) {
        if self.shell.handoff.open
            && (!self.handoff_gate()
                || self.live.as_ref().map(|s| s.client().launch_generation)
                    != self.shell.handoff.launch)
        {
            self.shell.handoff.close();
        }
    }
    fn paste_handoff(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.stop_editor_follow();
        // Check before touching clipboard or taking over the host surface.
        if !self.handoff_gate() || self.shell.handoff.in_flight.is_some() {
            return;
        }
        let client = self.live.as_ref().unwrap().client();
        let launch = client.launch_generation;
        let input = cx.read_from_clipboard().and_then(|item| item.text());
        let capability = input
            .as_deref()
            .ok_or("Clipboard does not contain a handoff URL.")
            .and_then(|text| Capability::parse(text, client.markdown_origin(), launch));
        drop(input);
        let state = &mut self.shell.handoff;
        state.open = true;
        state.launch = Some(launch);
        state.serial = state.serial.wrapping_add(1);
        state.capability = None;
        match capability {
            Ok(capability) => {
                state.capability = Some(capability);
                state.message.clear();
            }
            Err(error) => state.message = error.into(),
        }
        let focus = state.focus.get_or_insert_with(|| cx.focus_handle()).clone();
        self.suspend_host_reader_actions();
        window.focus(&focus);
        cx.notify();
    }
    fn open_handoff(&mut self, cx: &mut Context<Self>) {
        self.stop_editor_follow();
        if !self.handoff_gate() || self.shell.handoff.in_flight.is_some() {
            return;
        }
        let client = self.live.as_ref().unwrap().client();
        let launch = client.launch_generation;
        if self.shell.handoff.launch != Some(launch) {
            return;
        }
        let Some(capability) = self.shell.handoff.capability.take() else {
            return;
        };
        let serial = self.shell.handoff.begin_consume(); // acquire and retire selection before spawning
        self.shell.handoff.message = "Opening once… Closing cannot undo consumption.".into();
        let job = cx.background_executor().spawn(async move {
            let snapshot = client
                .consume_markdown(capability)
                .map_err(|e| e.to_string())?;
            let file = snapshot
                .selected(&snapshot.active)
                .ok_or("Invalid active document.")?;
            let blocks = markdown::preview_blocks(&file.text)
                .map_err(|_| "Document is too complex to render safely.".to_owned())?;
            Ok::<_, String>((snapshot, blocks))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.shell.handoff.in_flight == Some(serial) {
                    this.shell.handoff.in_flight = None;
                }
                if !this.handoff_gate()
                    || this.live.as_ref().map(|s| s.client().launch_generation) != Some(launch)
                    || !this.shell.handoff.accepts(serial, launch)
                {
                    cx.notify();
                    return;
                }
                this.shell.handoff.complete(serial, launch, result);
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    fn close_handoff(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.shell.handoff.close();
        if let Some(focus) = &self.shell.reader_focus {
            window.focus(focus);
        } else {
            window.blur();
        }
        cx.notify();
    }
    pub(crate) fn handoff_entry(&mut self, cx: &mut Context<Self>) -> Div {
        let origin = self
            .live
            .as_ref()
            .map(|s| s.client().markdown_origin().to_owned());
        div()
            .flex()
            .gap_3()
            .px_4()
            .py_1()
            .text_xs()
            .child(match origin {
                Some(origin) => format!("External Markdown · owned server {origin}"),
                None => "External Markdown requires an owned server.".into(),
            })
            .child(
                div()
                    .id("paste-markdown-handoff")
                    .cursor_pointer()
                    .child(if self.shell.handoff.in_flight.is_some() {
                        "Waiting for canceled request…"
                    } else if self.handoff_gate() {
                        "Paste handoff"
                    } else {
                        "Paste handoff unavailable"
                    })
                    .on_click(cx.listener(|this, _, window, cx| this.paste_handoff(window, cx))),
            )
    }
    pub(crate) fn render_handoff(
        &mut self,
        dark: bool,
        colors: (u32, u32, u32),
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let (paper, ink, line) = colors;
        let state = &self.shell.handoff;
        let focus = state.focus.as_ref().unwrap().clone();
        let mut header = div()
            .flex()
            .flex_wrap()
            .gap_3()
            .child("External snapshot · read-only")
            .child(
                div()
                    .id("close-handoff")
                    .cursor_pointer()
                    .child("Close (Esc)")
                    .on_click(cx.listener(|this, _, window, cx| this.close_handoff(window, cx))),
            );
        if state.capability.is_some() {
            header = header
                .child(
                    div()
                        .id("confirm-handoff")
                        .cursor_pointer()
                        .child("Open one-time snapshot")
                        .on_click(cx.listener(|this, _, _, cx| this.open_handoff(cx))),
                )
                .child(
                    div()
                        .id("cancel-handoff-input")
                        .cursor_pointer()
                        .child("Cancel pasted handoff")
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.handoff.capability = None;
                            this.shell.handoff.message =
                                "Canceled before Open; no request was sent.".into();
                            cx.notify();
                        })),
                );
        }
        let mut files = div().flex().flex_col().gap_1();
        if let Some(snapshot) = &state.snapshot {
            let page = state.page;
            let count = snapshot.page_count();
            files = files.child(format!("Markdown files · page {} of {}", page + 1, count));
            for (index, file) in snapshot.page(page).iter().enumerate() {
                let path = file.path.clone();
                files = files.child(
                    div()
                        .id(("handoff-file", index))
                        .cursor_pointer()
                        .child(path.clone())
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.select_handoff(&path, cx);
                        })),
                );
            }
            files = files.child(
                div()
                    .flex()
                    .gap_3()
                    .child(
                        div()
                            .id("handoff-prev")
                            .cursor_pointer()
                            .child("Previous page")
                            .on_click(cx.listener(|this, _, _, cx| {
                                this.shell.handoff.page = this.shell.handoff.page.saturating_sub(1);
                                cx.notify();
                            })),
                    )
                    .child(
                        div()
                            .id("handoff-next")
                            .cursor_pointer()
                            .child("Next page")
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.shell.handoff.page =
                                    (this.shell.handoff.page + 1).min(count.saturating_sub(1));
                                cx.notify();
                            })),
                    ),
            );
        }
        let body = div()
            .id("handoff-document")
            .track_scroll(&state.scroll)
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .p_4()
            .child(state.selected.clone())
            .child(markdown::render_themed(&state.blocks, dark));
        let message = state.message.clone();
        div().id("external-markdown-viewer").size_full().flex().flex_col().p_4().gap_3().bg(rgb(paper)).text_color(rgb(ink)).track_focus(&focus)
            .on_key_down(cx.listener(|this,event:&gpui::KeyDownEvent,window,cx| { if event.keystroke.key == "escape" { this.close_handoff(window,cx); } cx.stop_propagation(); }))
            .child(header).child(self.handoff_entry(cx))
            .child("Open consumes this one-use handoff. If opening fails, run track markdown again.")
            .child("Memory-only snapshot. Images, links, embeds, tasks, editing, Find and host navigation are disabled. Generate a new handoff to refresh.")
            .child(message)
            .child(div().flex().flex_1().min_h_0().gap_3()
                .child(div().id("handoff-files").w(px(260.)).overflow_y_scroll().border_r_1().border_color(rgb(line)).child(files)).child(body))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn close_invalidates_callback_but_retains_inflight_lock() {
        let mut state = HandoffUi {
            open: true,
            launch: Some(7),
            serial: 9,
            in_flight: Some(9),
            ..Default::default()
        };
        assert!(state.accepts(9, 7));
        assert!(!state.accepts(9, 8));
        assert!(!state.accepts(8, 7));
        state.close();
        assert!(!state.accepts(9, 7));
        assert_eq!(state.in_flight, Some(9));
        assert!(state.snapshot.is_none());
        assert!(state.blocks.is_empty());
    }
    fn snapshot(path: &str) -> Snapshot {
        Snapshot {
            active: path.into(),
            files: vec![markdown_handoff::MarkdownFile {
                path: path.into(),
                text: "fixture".into(),
            }],
        }
    }
    #[test]
    fn failed_replacement_retains_old_snapshot_stale_success_cannot_publish() {
        let mut state = HandoffUi {
            open: true,
            launch: Some(3),
            serial: 4,
            ..Default::default()
        };
        assert!(state.complete(4, 3, Ok((snapshot("old.md"), vec![]))));
        assert!(state.complete(4, 3, Err("Synthetic disconnect".into())));
        assert_eq!(state.snapshot.as_ref().unwrap().active, "old.md");
        assert!(state.message.contains("may have been consumed"));
        assert!(!state.complete(3, 3, Ok((snapshot("stale.md"), vec![]))));
        assert!(!state.complete(4, 2, Ok((snapshot("restarted.md"), vec![]))));
        assert_eq!(state.snapshot.as_ref().unwrap().active, "old.md");
        state.close();
        assert!(!state.complete(4, 3, Ok((snapshot("closed.md"), vec![]))));
        assert!(state.snapshot.is_none());
    }
    #[test]
    fn replacement_retires_old_selection_before_and_after_publication() {
        let mut state = HandoffUi {
            open: true,
            launch: Some(3),
            serial: 4,
            ..Default::default()
        };
        state.complete(
            4,
            3,
            Ok((snapshot("old.md"), markdown::preview_blocks("old").unwrap())),
        );
        state.selecting = true;
        state.selection_serial += 1;
        let old_selection = state.selection_serial;
        let old_serial = state.serial;
        let consume = state.begin_consume();
        assert!(!state.selection_available());
        assert!(!state.complete_selection(
            old_selection,
            old_serial,
            3,
            "old.md".into(),
            Ok(markdown::preview_blocks("late old").unwrap())
        ));
        // Reproduce a captured callback whose viewer serial matches consume as well:
        // selection epoch must independently reject it after replacement publication.
        state.in_flight = None;
        state.complete(
            consume,
            3,
            Ok((snapshot("new.md"), markdown::preview_blocks("new").unwrap())),
        );
        assert!(!state.complete_selection(
            old_selection,
            consume,
            3,
            "old.md".into(),
            Ok(markdown::preview_blocks("late old").unwrap())
        ));
        assert_eq!(state.selected, "new.md");
        assert_eq!(state.blocks[0].text, "new");
        assert!(state.selection_available());
        let failed = state.begin_consume();
        state.in_flight = None;
        state.complete(failed, 3, Err("disconnect".into()));
        assert_eq!(state.selected, "new.md");
        assert_eq!(state.blocks[0].text, "new");
        assert!(state.selection_available());
    }
    #[test]
    fn inert_parse_refuses_complex_input() {
        assert!(markdown::preview_blocks(&"paragraph\n\n".repeat(4097)).is_err());
        assert!(markdown::preview_blocks("[link](https://example.test) ![asset](file:///secret)\n\n- [ ] task\n\n<script>alert(1)</script>").is_ok());
    }
}
