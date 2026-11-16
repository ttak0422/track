//! Ephemeral external documents never enter the vault reader's state or caches.
use super::*;
use gpui::{Div, Stateful};
use markdown_handoff::{Capability, Snapshot};
use snapshot_links::Location;
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

#[derive(Default)]
pub(crate) struct HandoffUi {
    open: bool,
    find: SnapshotFindUi,
    launch: Option<u64>,
    serial: u64,
    // Kept even when the surface closes: a canceled consume may still be running.
    in_flight: Option<u64>,
    capability: Option<Capability>,
    snapshot: Option<Snapshot>,
    selected: String,
    history: snapshot_links::History,
    history_target: Option<usize>,
    links: Vec<(markdown::Link, Location)>,
    blocks: Vec<markdown::Block>,
    images: snapshot_images::Prepared,
    image_cancel: Arc<AtomicBool>,
    page: usize,
    selecting: bool,
    selection_serial: u64,
    message: String,
    focus: Option<gpui::FocusHandle>,
    scroll: gpui::ScrollHandle,
}
impl HandoffUi {
    fn renew_image_job(&mut self) -> Arc<AtomicBool> {
        self.image_cancel.store(true, Ordering::Release);
        self.image_cancel = Arc::new(AtomicBool::new(false));
        self.image_cancel.clone()
    }
    fn close(&mut self) {
        self.find.model.close();
        self.retire_find();
        self.image_cancel.store(true, Ordering::Release);
        self.images = Default::default();
        self.open = false;
        self.serial = self.serial.wrapping_add(1);
        self.capability = None;
        self.snapshot = None;
        self.blocks.clear();
        self.selected.clear();
        self.history = Default::default();
        self.history_target = None;
        self.links.clear();
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
        self.retire_find();
        self.renew_image_job();
        self.serial = self.serial.wrapping_add(1);
        self.selection_serial = self.selection_serial.wrapping_add(1);
        self.selecting = false;
        self.history_target = None;
        self.in_flight = Some(self.serial);
        self.serial
    }
    fn selection_available(&self) -> bool {
        self.open && !self.selecting && self.in_flight.is_none()
    }
    fn link_target(
        &self,
        serial: u64,
        selection: u64,
        source: &str,
        link: &markdown::Link,
    ) -> Option<&Location> {
        if !self.selection_available()
            || self.serial != serial
            || self.selection_serial != selection
            || self.selected != source
        {
            return None;
        }
        self.links
            .iter()
            .find(|(candidate, _)| candidate == link)
            .map(|(_, path)| path)
    }
    fn accept_images(
        &mut self,
        serial: u64,
        selection: u64,
        launch: u64,
        path: &str,
        images: snapshot_images::Prepared,
    ) -> bool {
        if !self.accepts(serial, launch)
            || self.selection_serial != selection
            || self.selected != path
            || !self.selection_available()
        {
            return false;
        }
        self.images = images;
        true
    }
    fn complete_selection(
        &mut self,
        selection: u64,
        serial: u64,
        launch: u64,
        destination: Location,
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
                let target = match heading_index(&blocks, &destination) {
                    Ok(target) => target,
                    Err(error) => {
                        self.history_target = None;
                        self.message = error.into();
                        return false;
                    }
                };
                let path = destination.file.clone();
                self.links = self
                    .snapshot
                    .as_ref()
                    .map(|s| snapshot_links_for(s, &path, &blocks))
                    .unwrap_or_default();
                self.images = Default::default();
                self.blocks = blocks;
                self.history.accept(destination, self.history_target.take());
                self.selected = path;
                self.scroll = gpui::ScrollHandle::new();
                if let Some(index) = target {
                    // Two direct prefix children: file title and image status.
                    self.scroll.scroll_to_top_of_item(2 + index);
                }
                self.message.clear();
            }
            Err(_) => {
                self.history_target = None;
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
                self.history.reset(snapshot.active.clone());
                self.history_target = None;
                self.links = snapshot_links_for(&snapshot, &snapshot.active, &blocks);
                self.images = Default::default();
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
fn heading_index(
    blocks: &[markdown::Block],
    destination: &Location,
) -> Result<Option<usize>, &'static str> {
    match &destination.heading {
        None => Ok(None),
        Some(heading) => markdown::outline(blocks).into_iter().find(|entry| &entry.anchor == heading)
            .map(|entry| Some(entry.block_index)).ok_or("Heading not reached: no exact native heading anchor in the target document. Selection and history unchanged."),
    }
}
fn snapshot_links_for(
    snapshot: &Snapshot,
    current: &str,
    blocks: &[markdown::Block],
) -> Vec<(markdown::Link, Location)> {
    admit_snapshot_links(
        snapshot,
        current,
        blocks.iter().flat_map(|b| {
            b.links.iter().chain(
                b.table
                    .iter()
                    .flat_map(|t| t.rows.iter().flatten().flat_map(|c| c.links.iter())),
            )
        }),
    )
}
fn admit_snapshot_links<'a>(
    snapshot: &Snapshot,
    current: &str,
    links: impl Iterator<Item = &'a markdown::Link>,
) -> Vec<(markdown::Link, Location)> {
    let paths: std::collections::HashSet<&str> =
        snapshot.files.iter().map(|f| f.path.as_str()).collect();
    links
        .take(256)
        .filter_map(|link| {
            let markdown::LinkTarget::Markdown { destination } = &link.target else {
                return None;
            };
            let path = snapshot_links::location(current, destination)?;
            paths
                .contains(path.file.as_str())
                .then(|| (link.clone(), path))
        })
        .collect()
}
impl Workspace {
    fn select_handoff(&mut self, path: &str, cx: &mut Context<Self>) {
        self.select_handoff_target(&Location::from(path), None, cx);
    }
    fn select_handoff_target(
        &mut self,
        destination: &Location,
        history_target: Option<usize>,
        cx: &mut Context<Self>,
    ) {
        if !self.handoff_gate() || !self.shell.handoff.selection_available() {
            return;
        }
        let state = &mut self.shell.handoff;
        let Some(file) = state
            .snapshot
            .as_ref()
            .and_then(|s| s.selected(&destination.file))
        else {
            return;
        };
        let path = file.path.clone();
        let destination = destination.clone();
        let source = file.text.clone();
        let image_files = state.snapshot.as_ref().unwrap().images.clone();
        let cancel = state.renew_image_job();
        let serial = state.serial;
        let Some(launch) = state.launch else { return };
        state.history_target = history_target;
        state.selecting = true;
        state.retire_find();
        state.selection_serial = state.selection_serial.wrapping_add(1);
        let selection = state.selection_serial;
        state.message = "Preparing selected document…".into();
        let image_path = path.clone();
        let job = cx.background_executor().spawn(async move {
            let blocks = markdown::preview_blocks(&source)?;
            let images = snapshot_images::prepare(&image_files, &image_path, &blocks, &cancel);
            Ok::<_, String>((blocks, images))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                let current = this.handoff_gate()
                    && this.live.as_ref().map(|s| s.client().launch_generation) == Some(launch);
                let state = &mut this.shell.handoff;
                if current {
                    let (result, images) = match result {
                        Ok((blocks, images)) => (Ok(blocks), Some(images)),
                        Err(error) => (Err(error), None),
                    };
                    if state.complete_selection(selection, serial, launch, destination, result) {
                        if let Some(images) = images {
                            state.accept_images(serial, selection, launch, &path, images);
                        }
                    }
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
        let cancel = self.shell.handoff.image_cancel.clone();
        let job = cx.background_executor().spawn(async move {
            let snapshot = client
                .consume_markdown(capability)
                .map_err(|e| e.to_string())?;
            let file = snapshot
                .selected(&snapshot.active)
                .ok_or("Invalid active document.")?;
            let blocks = markdown::preview_blocks(&file.text)
                .map_err(|_| "Document is too complex to render safely.".to_owned())?;
            let images =
                snapshot_images::prepare(&snapshot.images, &snapshot.active, &blocks, &cancel);
            Ok::<_, String>((snapshot, blocks, images))
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
                let (result, images) = match result {
                    Ok((snapshot, blocks, images)) => (Ok((snapshot, blocks)), Some(images)),
                    Err(error) => (Err(error), None),
                };
                if this.shell.handoff.complete(serial, launch, result) {
                    if let Some(images) = images {
                        let state = &mut this.shell.handoff;
                        let path = state.selected.clone();
                        state.accept_images(serial, state.selection_serial, launch, &path, images);
                    }
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    fn close_handoff(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.shell.handoff.open
            || !window.is_window_active()
            || self
                .shell
                .handoff
                .find
                .input
                .as_ref()
                .is_some_and(|input| input.read(cx).is_composing())
        {
            return;
        }
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
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        self.sync_handoff_find(dark, window, cx);
        let (paper, ink, line) = colors;
        let state = &self.shell.handoff;
        let focus = state.focus.as_ref().unwrap().clone();
        let find_owner = state.find_identity();
        let mut header = div()
            .flex()
            .flex_wrap()
            .gap_3()
            .child("External snapshot · read-only")
            .child(
                div()
                    .id("snapshot-find-open")
                    .cursor_pointer()
                    .child("Find")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.open_handoff_find(&find_owner, window, cx);
                        cx.stop_propagation();
                    })),
            )
            .child(
                div()
                    .id("close-handoff")
                    .cursor_pointer()
                    .child("Close viewer")
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
        let serial = state.serial;
        let selection = state.selection_serial;
        let selected = state.selected.clone();
        for (label, forward) in [("Back", false), ("Forward", true)] {
            if let Some((target, path)) = state.history.target(forward) {
                let path = path.to_owned();
                let selected = selected.clone();
                header = header.child(
                    div()
                        .id(gpui::SharedString::from(format!("handoff-history-{label}")))
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            let state = &this.shell.handoff;
                            if !window.is_window_active()
                                || !state.selection_available()
                                || state.serial != serial
                                || state.selection_serial != selection
                                || state.selected != selected
                                || state.history.target(forward) != Some((target, &path))
                            {
                                return;
                            }
                            this.select_handoff_target(&path, Some(target), cx);
                        })),
                );
            }
        }
        let mut files = div().flex().flex_col().gap_1();
        if let Some(snapshot) = &state.snapshot {
            let page = state.page;
            let count = snapshot.page_count();
            files = files.child(format!("Markdown files · page {} of {}", page + 1, count));
            for (index, file) in snapshot.page(page).iter().enumerate() {
                let path = file.path.clone();
                let selected = selected.clone();
                files = files.child(
                    div()
                        .id(("handoff-file", index))
                        .cursor_pointer()
                        .child(path.clone())
                        .on_click(cx.listener(move |this, _, window, cx| {
                            let state = &this.shell.handoff;
                            if !window.is_window_active()
                                || !state.selection_available()
                                || state.serial != serial
                                || state.selection_serial != selection
                                || state.selected != selected
                            {
                                return;
                            }
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
        let allowed = state.links.clone();
        let resolved = allowed.clone();
        let source = state.selected.clone();
        let weak = cx.entity().downgrade();
        let callbacks = markdown::LinkCallbacks {
            snapshot_local: true,
            pdf: None,
            math: None,
            selection: None,
            hover: None,
            arrival_block: None,
            enlarge: None,
            plain_click: None,
            resolved: std::rc::Rc::new(move |target| {
                resolved.iter().any(|(link, _)| &link.target == target)
            }),
            activate: std::rc::Rc::new(move |link, window, cx| {
                if !window.is_window_active() {
                    return;
                }
                let Some((_, path)) = allowed.iter().find(|(candidate, _)| candidate == link)
                else {
                    return;
                };
                let _ = weak.update(cx, |this, cx| {
                    let state = &this.shell.handoff;
                    if state.link_target(serial, selection, &source, link) != Some(path) {
                        return;
                    }
                    this.select_handoff_target(path, None, cx);
                });
            }),
        };
        let body = div()
            .id("handoff-document")
            .track_scroll(&state.scroll)
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .p_4()
            .child(state.selected.clone())
            .child(state.images.status.clone())
            .children(markdown::render_snapshot_elements(
                &state.blocks,
                dark,
                &callbacks,
                &state.images.images,
                state
                    .find
                    .anchors
                    .as_ref()
                    .filter(|_| state.find.model.open && state.find_identity().is_some())
                    .map(|anchors| markdown::FindPresentation {
                        matches: &state.find.model.matches,
                        active: state.find.model.active,
                        anchors,
                    }),
            ));
        let message = state.message.clone();
        let find_open = state.find.model.open;
        div().id("external-markdown-viewer").size_full().flex().flex_col().p_4().gap_3().bg(rgb(paper)).text_color(rgb(ink)).track_focus(&focus)
            .on_action(cx.listener(|this,_:&search_input::FindNext,window,cx| {if this.handoff_find_focused(window,cx) {this.step_handoff_find(true,window,cx);}cx.stop_propagation();}))
            .on_action(cx.listener(|this,_:&search_input::FindPrevious,window,cx| {if this.handoff_find_focused(window,cx) {this.step_handoff_find(false,window,cx);}cx.stop_propagation();}))
            .on_action(cx.listener(|this,_:&search_input::CloseFind,window,cx| {if this.handoff_find_focused(window,cx) {this.close_handoff_find(window,cx);}cx.stop_propagation();}))
            .on_key_down(cx.listener(|this,event:&gpui::KeyDownEvent,window,cx| {
                if event.keystroke.key == "escape" && !this.shell.handoff.find.input.as_ref().is_some_and(|i| i.read(cx).is_composing()) {
                    if this.shell.handoff.find.model.open {this.close_handoff_find(window,cx);} else {this.close_handoff(window,cx);}
                }
                cx.stop_propagation();
            }))
            .child(header).child(self.handoff_entry(cx))
            .when(find_open, |root| root.child(self.handoff_find_bar(cx)))
            .child("Open consumes this one-use handoff. If opening fails, run track markdown again.")
            .child("Memory-only snapshot. Local Markdown file/heading links and viewer Back/Forward use native heading anchors. Standalone snapshot PNG/JPEG images are bounded; original image text remains. Other images, external/wiki links, embeds, editing and host navigation remain disabled. Find searches only this displayed document. Generate a new handoff to refresh.")
            .child(message)
            .child(div().flex().flex_1().min_h_0().gap_3()
                .child(div().id("handoff-files").w(px(260.)).overflow_y_scroll().border_r_1().border_color(rgb(line)).child(files)).child(body))
    }
}

#[derive(Default)]
struct SnapshotFindUi {
    model: snapshot_find::Model,
    input: Option<gpui::Entity<search_input::SearchInput>>,
    anchors: Option<std::rc::Rc<markdown::FindAnchors>>,
    pending: bool,
    retries: u8,
}
impl HandoffUi {
    fn find_context(&self) -> snapshot_find::Identity {
        snapshot_find::Identity {
            snapshot: self.serial,
            selection: self.selection_serial,
            file: self.selected.clone(),
        }
    }
    fn find_identity(&self) -> Option<snapshot_find::Identity> {
        (self.selection_available() && self.snapshot.is_some()).then(|| self.find_context())
    }
    fn retire_find(&mut self) {
        self.find.model.bind(None, &[]);
        self.find.anchors = None;
        self.find.pending = false;
    }
}
impl Workspace {
    fn sync_handoff_find(&mut self, dark: bool, window: &mut Window, cx: &mut Context<Self>) {
        if self.shell.handoff.find.input.is_none() {
            let input = cx.new(|cx| {
                search_input::SearchInput::with_purpose(search_input::InputPurpose::ReaderFind, cx)
            });
            self.subscriptions.push(cx.subscribe(
                &input,
                |this, _, event: &search_input::SearchChanged, cx| {
                    let state = &mut this.shell.handoff;
                    state.find.model.query = event.0.clone();
                    let identity = state.find_identity();
                    if state.find.model.bind(identity.clone(), &state.blocks) {
                        state.find.anchors = identity.map(|_| {
                            std::rc::Rc::new(markdown::FindAnchors::new(
                                &state.blocks,
                                &state.scroll,
                            ))
                        });
                    }
                    if state.open && state.find.model.open {
                        state.find.model.evaluate(&state.blocks);
                        state.find.pending = state.find.model.active.is_some();
                        state.find.retries = 0;
                    }
                    cx.notify();
                },
            ));
            self.shell.handoff.find.input = Some(input);
        }
        let state = &mut self.shell.handoff;
        state
            .find
            .input
            .as_ref()
            .unwrap()
            .update(cx, |input, _| input.set_dark(dark));
        let identity = state.find_identity();
        let changed = state.find.model.bind(identity.clone(), &state.blocks);
        if changed || state.find.anchors.is_none() {
            state.find.anchors = identity.as_ref().map(|_| {
                std::rc::Rc::new(markdown::FindAnchors::new(&state.blocks, &state.scroll))
            });
        }
        if changed {
            state.find.pending = false; // Navigation/heading intent wins over passive re-evaluation.
            state.find.retries = 0;
        }
        if !state.find.pending {
            return;
        }
        state.find.pending = false;
        let Some(ticket) = state.find.model.ticket(identity.as_ref()) else {
            return;
        };
        let Some(anchors) = state.find.anchors.clone() else {
            return;
        };
        let weak = cx.entity().downgrade();
        window.on_next_frame(move |window, cx| {
            let _ = weak.update(cx, |this, cx| {
                let state = &mut this.shell.handoff;
                if !state.find.model.reveal_current(
                    &ticket,
                    state.find_identity().as_ref(),
                    state
                        .find
                        .anchors
                        .as_ref()
                        .is_some_and(|current| std::rc::Rc::ptr_eq(current, &anchors)),
                    window.is_window_active(),
                    state
                        .find
                        .input
                        .as_ref()
                        .is_some_and(|i| i.read(cx).is_composing()),
                ) {
                    return;
                }
                if !anchors.scroll_to_hit(&ticket.hit, window.viewport_size())
                    && state.find.retries == 0
                {
                    state.find.retries = 1;
                    state.find.pending = true;
                }
                cx.notify();
            });
        });
    }
    fn open_handoff_find(
        &mut self,
        expected: &Option<snapshot_find::Identity>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        use gpui::Focusable;
        let state = &mut self.shell.handoff;
        if !window.is_window_active() || expected.is_none() || state.find_identity() != *expected {
            return;
        }
        let Some(input) = state.find.input.clone() else {
            return;
        };
        if input.read(cx).is_composing() {
            return;
        }
        state.find.model.open = true;
        state.find.model.evaluate(&state.blocks);
        state.find.pending = state.find.model.active.is_some();
        state.find.retries = 0;
        input.update(cx, |input, cx| input.select_all(cx));
        window.focus(&input.focus_handle(cx));
        cx.notify();
    }
    fn step_handoff_find(&mut self, forward: bool, window: &Window, cx: &mut Context<Self>) {
        let state = &mut self.shell.handoff;
        if !window.is_window_active()
            || !state.find.model.open
            || !state.find.model.current(state.find_identity().as_ref())
            || state
                .find
                .input
                .as_ref()
                .is_some_and(|i| i.read(cx).is_composing())
        {
            return;
        }
        state.find.model.step(forward);
        state.find.pending = state.find.model.active.is_some();
        state.find.retries = 0;
        cx.notify();
    }
    fn handoff_find_focused(&self, window: &Window, cx: &App) -> bool {
        use gpui::Focusable;
        let state = &self.shell.handoff;
        state.open
            && state.find.model.open
            && state
                .find
                .input
                .as_ref()
                .is_some_and(|input| input.focus_handle(cx).is_focused(window))
    }
    fn click_handoff_find(
        &mut self,
        expected: &snapshot_find::Identity,
        revision: u64,
        forward: bool,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        let state = &self.shell.handoff;
        if !state
            .find
            .model
            .action_current(expected, &state.find_context(), revision)
        {
            return;
        }
        self.step_handoff_find(forward, window, cx);
    }
    fn close_handoff_find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let state = &mut self.shell.handoff;
        if !state.open
            || !state.find.model.open
            || !window.is_window_active()
            || state
                .find
                .input
                .as_ref()
                .is_some_and(|i| i.read(cx).is_composing())
        {
            return;
        }
        state.find.model.close();
        state.find.pending = false;
        state.find.anchors = None;
        if let Some(focus) = &state.focus {
            window.focus(focus);
        }
        cx.notify();
    }
    fn handoff_find_bar(&self, cx: &mut Context<Self>) -> Div {
        let find = &self.shell.handoff.find;
        let previous_owner = self.shell.handoff.find_context();
        let next_owner = previous_owner.clone();
        let close_owner = previous_owner.clone();
        let revision = find.model.revision();
        let status = if let Some(error) = &find.model.matches.error {
            error.clone()
        } else if find.model.query.is_empty() {
            "Current document only".into()
        } else if self.shell.handoff.find_identity().is_none() {
            "Find paused while the document changes".into()
        } else if find.model.matches.hits.is_empty() {
            "No matches".into()
        } else {
            format!(
                "{} of {}{}",
                find.model.active.map_or(0, |i| i + 1),
                find.model.matches.hits.len(),
                if find.model.matches.truncated {
                    "+ (limited)"
                } else {
                    ""
                }
            )
        };
        div()
            .flex()
            .flex_wrap()
            .gap_2()
            .items_center()
            .child(div().w(px(250.)).max_w_full().children(find.input.clone()))
            .child(status)
            .child(
                div()
                    .id("snapshot-find-prev")
                    .cursor_pointer()
                    .child("Previous")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.click_handoff_find(&previous_owner, revision, false, window, cx);
                        cx.stop_propagation();
                    })),
            )
            .child(
                div()
                    .id("snapshot-find-next")
                    .cursor_pointer()
                    .child("Next")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.click_handoff_find(&next_owner, revision, true, window, cx);
                        cx.stop_propagation();
                    })),
            )
            .child(
                div()
                    .id("snapshot-find-close")
                    .cursor_pointer()
                    .child("Close Find")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        let state = &this.shell.handoff;
                        if !state.find.model.action_current(
                            &close_owner,
                            &state.find_context(),
                            revision,
                        ) {
                            return;
                        }
                        this.close_handoff_find(window, cx);
                        cx.stop_propagation();
                    })),
            )
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
    #[test]
    fn images_publish_only_for_current_selection_and_cancel_releases_on_close() {
        let mut state = HandoffUi {
            open: true,
            launch: Some(1),
            serial: 2,
            selection_serial: 3,
            selected: "a.md".into(),
            ..Default::default()
        };
        assert!(state.accept_images(
            2,
            3,
            1,
            "a.md",
            snapshot_images::Prepared {
                status: "ready".into(),
                ..Default::default()
            }
        ));
        for (serial, revision, launch, path) in [
            (1, 3, 1, "a.md"),
            (2, 2, 1, "a.md"),
            (2, 3, 2, "a.md"),
            (2, 3, 1, "b.md"),
        ] {
            assert!(!state.accept_images(serial, revision, launch, path, Default::default()));
            assert_eq!(state.images.status, "ready");
        }
        let old = state.renew_image_job();
        state.begin_consume();
        assert!(old.load(Ordering::Acquire));
        assert_eq!(state.images.status, "ready"); // retained until replacement succeeds
        let cancel = state.image_cancel.clone();
        state.close();
        assert!(cancel.load(Ordering::Acquire));
        assert!(state.images.status.is_empty());
        assert!(!state.accept_images(2, 3, 1, "a.md", Default::default()));
    }
    #[test]
    fn missing_heading_and_stale_completion_preserve_selection_and_history() {
        let mut state = HandoffUi {
            open: true,
            launch: Some(1),
            serial: 2,
            selection_serial: 3,
            selected: "a.md".into(),
            ..Default::default()
        };
        state.history.reset("a.md".into());
        state.images.status = "old image".into();
        state.history_target = Some(0);
        let missing = snapshot_links::location("a.md", "b.md#missing").unwrap();
        assert!(!state.complete_selection(3, 2, 1, missing.clone(), Ok(vec![])));
        assert_eq!(state.selected, "a.md");
        assert_eq!(state.images.status, "old image");
        assert!(state.history.target(false).is_none());
        assert!(state.history.target(true).is_none());
        assert!(state.message.contains("Heading not reached"));
        assert!(state.history_target.is_none());
        state.begin_consume();
        assert!(!state.complete_selection(3, 2, 1, missing.clone(), Ok(vec![])));
        state.close();
        assert!(!state.complete_selection(3, 2, 1, missing, Ok(vec![])));
        assert!(state.selected.is_empty());
    }
    #[test]
    fn repeated_heading_uses_fresh_selection_and_native_duplicate_anchor() {
        let blocks = markdown::preview_blocks("# One\n\n# One\n\nOne\n===\n\n# 日本語\n").unwrap();
        let mut state = HandoffUi {
            open: true,
            launch: Some(1),
            serial: 2,
            selection_serial: 3,
            selected: "a.md".into(),
            ..Default::default()
        };
        state.history.reset("a.md".into());
        let location = snapshot_links::location("a.md", "#one-2").unwrap();
        assert_eq!(heading_index(&blocks, &location), Ok(Some(1)));
        assert!(state.complete_selection(3, 2, 1, location.clone(), Ok(blocks.clone())));
        state.selection_serial = 4;
        assert!(!state.complete_selection(3, 2, 1, location.clone(), Ok(blocks.clone())));
        assert!(state.complete_selection(4, 2, 1, location.clone(), Ok(blocks.clone())));
        assert_eq!(
            state.history.target(false),
            Some((0, &Location::from("a.md")))
        );
        assert_eq!(
            heading_index(
                &blocks,
                &snapshot_links::location("a.md", "#one-3").unwrap()
            ),
            Ok(Some(2))
        );
        assert_eq!(
            heading_index(
                &blocks,
                &snapshot_links::location("a.md", "#%E6%97%A5%E6%9C%AC%E8%AA%9E").unwrap()
            ),
            Ok(Some(3))
        );
        state.selection_serial = 5;
        assert!(state.complete_selection(5, 2, 1, Location::from("a.md"), Ok(blocks)));
        assert_eq!(state.history.target(false), Some((1, &location)));
    }
    fn snapshot(path: &str) -> Snapshot {
        Snapshot {
            images: Default::default(),
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
    #[test]
    fn local_link_authority_is_selected_revision_and_replacement_scoped() {
        let link = markdown::Link {
            range: 0..4,
            target: markdown::LinkTarget::Markdown {
                destination: "b.md".into(),
            },
        };
        let mut state = HandoffUi {
            open: true,
            launch: Some(1),
            serial: 2,
            selection_serial: 3,
            selected: "a.md".into(),
            links: vec![(link.clone(), "b.md".into())],
            ..Default::default()
        };
        assert_eq!(
            state
                .link_target(2, 3, "a.md", &link)
                .map(|l| l.file.as_str()),
            Some("b.md")
        );
        assert!(state.link_target(1, 3, "a.md", &link).is_none());
        assert!(state.link_target(2, 2, "a.md", &link).is_none());
        assert!(state.link_target(2, 3, "b.md", &link).is_none());
        state.selecting = true;
        assert!(state.link_target(2, 3, "a.md", &link).is_none());
        state.selecting = false;
        state.begin_consume();
        assert!(state.link_target(2, 3, "a.md", &link).is_none());
        state.close();
        assert!(state.links.is_empty());
    }
    #[test]
    fn accepted_selections_only_update_viewer_history() {
        let mut state = HandoffUi {
            open: true,
            launch: Some(1),
            serial: 2,
            selection_serial: 3,
            ..Default::default()
        };
        state.complete(2, 1, Ok((snapshot("a.md"), vec![])));
        let revision = state.selection_serial;
        assert!(state.complete_selection(revision, 2, 1, "b.md".into(), Ok(vec![])));
        assert_eq!(
            state.history.target(false),
            Some((0, &Location::from("a.md")))
        );
        state.history_target = Some(0);
        assert!(state.complete_selection(revision, 2, 1, "a.md".into(), Err("bad".into())));
        assert_eq!(state.selected, "b.md");
        assert_eq!(
            state.history.target(false),
            Some((0, &Location::from("a.md")))
        );
        state.history_target = Some(0);
        assert!(state.complete_selection(revision, 2, 1, "a.md".into(), Ok(vec![])));
        assert_eq!(
            state.history.target(true),
            Some((1, &Location::from("b.md")))
        );
    }
    #[test]
    fn failed_consume_retains_old_links_under_new_revision_only() {
        let link = markdown::Link {
            range: 0..1,
            target: markdown::LinkTarget::Markdown {
                destination: "b.md".into(),
            },
        };
        let mut state = HandoffUi {
            open: true,
            launch: Some(1),
            serial: 2,
            selection_serial: 3,
            selected: "a.md".into(),
            snapshot: Some(snapshot("a.md")),
            links: vec![(link.clone(), "b.md".into())],
            ..Default::default()
        };
        let serial = state.begin_consume();
        assert!(state.link_target(2, 3, "a.md", &link).is_none());
        state.in_flight = None;
        assert!(state.complete(serial, 1, Err("offline".into())));
        assert_eq!(state.selected, "a.md");
        assert_eq!(
            state
                .link_target(serial, state.selection_serial, "a.md", &link)
                .map(|l| l.file.as_str()),
            Some("b.md")
        );
    }
    #[test]
    fn local_link_cache_requires_exact_existing_path_and_is_bounded() {
        let snap = snapshot("dir/Target.md");
        let link = |target: &str| markdown::Link {
            range: 0..1,
            target: markdown::LinkTarget::Markdown {
                destination: target.into(),
            },
        };
        let links = vec![
            link("Target.md"),
            link("target.md"),
            link("../Target.md"),
            link("Target.md#part"),
            link("missing.md"),
        ];
        let accepted = admit_snapshot_links(&snap, "dir/start.md", links.iter());
        assert_eq!(accepted.len(), 2);
        assert_eq!(accepted[0].1.file, "dir/Target.md");
        assert_eq!(accepted[1].1.heading.as_deref(), Some("part"));
        let many = vec![link("Target.md"); 300];
        assert_eq!(
            admit_snapshot_links(&snap, "dir/start.md", many.iter()).len(),
            256
        );
    }
}
