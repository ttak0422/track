//! Developer-only disposable-fixture editing. One active draft, no autosave/retry.
//! All transitions pass the same gate before any note/tab/reader state changes.
use super::*;
use editing::{EditSession, LeaveAction, LeaveChoice, LeavePrompt, SaveOutcome};
use gpui::{Div, Entity, Subscription};
use std::path::{Path, PathBuf};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum EditorMode {
    Preview,
    Edit,
    Split,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Transition {
    InternalLink(shell::InternalLinkIntent),
    Note(live::NoteKey),
    LegacyNote(i64),
    ActivateTab(tabs::TabKey),
    CloseTab(tabs::TabKey),
    Home,
    Reload,
    Graph,
    FullGraph,
    WorkingVault(String),
    Quit,
}
fn acknowledge_saved_transition(intent: &mut Transition) {
    if let Transition::InternalLink(link) = intent {
        link.allow_epoch_advance = true;
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum GateDecision {
    Proceed,
    Prompt,
    Block,
}
fn transition_decision(pending: bool, exiting: bool, prompt: Option<LeavePrompt>) -> GateDecision {
    if pending || exiting {
        return GateDecision::Block;
    }
    match prompt {
        None | Some(LeavePrompt::Leave) => GateDecision::Proceed,
        Some(
            LeavePrompt::AskSaveDiscardCancel
            | LeavePrompt::WaitForSave
            | LeavePrompt::WaitForComposition,
        ) => GateDecision::Prompt,
    }
}
fn comparison_context_matches(
    expected_launch: u64,
    expected_storage: u64,
    current_launch: Option<u64>,
    current_storage: Option<u64>,
    failed: bool,
) -> bool {
    !failed && current_launch == Some(expected_launch) && current_storage == Some(expected_storage)
}
struct ActiveDraft {
    session: EditSession,
    input: Entity<editor_input::TextEditor>,
    _subscription: Subscription,
    source_path: PathBuf,
    title: String,
}
pub struct EditingUi {
    fixture_root: Option<PathBuf>,
    active: Option<ActiveDraft>,
    mode: EditorMode,
    mode_menu: bool,
    pending: Option<Transition>,
    bypass: bool,
    inspecting: bool,
    inspection_ticket: u64,
    exit_requested: bool,
    message: String,
}
impl EditingUi {
    pub fn new(fixture_root: Option<PathBuf>) -> Self {
        Self {
            fixture_root,
            active: None,
            mode: EditorMode::Preview,
            mode_menu: false,
            pending: None,
            bypass: false,
            inspecting: false,
            inspection_ticket: 0,
            exit_requested: false,
            message: String::new(),
        }
    }
    fn shutdown_failed(&mut self, message: String) {
        self.exit_requested = false;
        self.pending = None;
        self.message = message;
    }
    pub fn active(&self) -> bool {
        self.active.is_some()
    }
    pub fn has_pending(&self) -> bool {
        self.pending.is_some()
    }
    pub fn enabled(&self) -> bool {
        self.fixture_root.is_some()
    }
    pub fn dirty_for(&self, key: &tabs::TabKey) -> bool {
        self.active.as_ref().is_some_and(|a| {
            a.session.dirty()
                && *key
                    == tabs::TabKey::Note {
                        vault: a.session.key().vault.clone(),
                        note_id: a.session.key().note_id,
                    }
        })
    }
}
/// Explicit opt-in is restricted to an existing canonical directory underneath
/// the OS temporary root. It must be exactly the configured launch vault, with
/// isolated temporary config/cache. Not a sandbox against concurrent filesystem changes.
pub fn validate_fixture(
    root: &Path,
    vault: &Path,
    config: Option<&Path>,
    cache: Option<&Path>,
) -> Result<PathBuf, String> {
    let root = root
        .canonicalize()
        .map_err(|e| format!("Fixture root: {e}"))?;
    let vault = vault
        .canonicalize()
        .map_err(|e| format!("Fixture vault: {e}"))?;
    let temp_roots: Vec<_> = [std::env::temp_dir(), PathBuf::from("/tmp")]
        .into_iter()
        .filter_map(|p| p.canonicalize().ok())
        .collect();
    if root != vault
        || !root.is_dir()
        || !temp_roots.iter().any(|p| root != *p && root.starts_with(p))
    {
        return Err("Developer editing requires --developer-edit-fixture to equal the disposable --vault directory beneath the OS temporary directory.".into());
    }
    let marker = root.join(".track-native-edit-fixture");
    if marker.canonicalize().ok().as_ref() != Some(&marker)
        || std::fs::read_to_string(&marker).ok().as_deref() != Some("disposable-fixture-only\n")
    {
        return Err("Fixture root requires .track-native-edit-fixture containing exactly disposable-fixture-only followed by LF.".into());
    }
    for (label, path) in [("config", config), ("cache", cache)] {
        let path =
            path.ok_or_else(|| format!("Developer editing requires explicit isolated --{label}."))?;
        let path = path
            .canonicalize()
            .map_err(|e| format!("Fixture {label} must already exist: {e}"))?;
        if (label == "config" && !path.is_file()) || (label == "cache" && !path.is_dir()) {
            return Err(format!("Fixture {label} has the wrong filesystem type."));
        }
        if !temp_roots.iter().any(|p| path != *p && path.starts_with(p)) {
            return Err(format!(
                "Developer fixture {label} must be under the OS temporary directory."
            ));
        }
    }
    Ok(root)
}
fn fixture_note(root: &Path, path: &Path) -> bool {
    let marker = root.join(".track-native-edit-fixture");
    if marker.canonicalize().ok().as_ref() != Some(&marker)
        || std::fs::read_to_string(&marker).ok().as_deref() != Some("disposable-fixture-only\n")
    {
        return false;
    }
    let path = if path.is_absolute() {
        path.to_owned()
    } else {
        root.join(path)
    };
    path.canonicalize()
        .ok()
        .is_some_and(|path| path.is_file() && path.starts_with(root))
}
impl Workspace {
    /// true means caller must stop before changing any navigation state.
    pub(crate) fn guard_transition(&mut self, intent: Transition, cx: &mut Context<Self>) -> bool {
        if self.editing_ui.bypass {
            self.cancel_transient_preview();
            return false;
        }
        if transition_decision(
            self.editing_ui.pending.is_some(),
            self.editing_ui.exit_requested,
            None,
        ) == GateDecision::Block
        {
            cx.notify();
            return true;
        }
        if let Err(message) = self.sync_editor(cx) {
            self.editing_ui.message = message;
            cx.notify();
            return true;
        }
        match transition_decision(
            false,
            false,
            self.editing_ui
                .active
                .as_ref()
                .map(|a| a.session.leave_prompt()),
        ) {
            GateDecision::Proceed => {
                self.cancel_transient_preview();
                self.clear_editor();
                false
            }
            GateDecision::Prompt => {
                self.editing_ui.pending = Some(intent);
                self.editing_ui.message =
                    "Resolve this draft before leaving. No save happens automatically.".into();
                cx.notify();
                true
            }
            GateDecision::Block => true,
        }
    }
    fn clear_editor(&mut self) {
        self.editing_ui.active = None;
        self.editing_ui.pending = None;
        self.editing_ui.inspecting = false;
        self.editing_ui.inspection_ticket += 1;
        self.editing_ui.mode = EditorMode::Preview;
    }
    fn sync_editor(&mut self, cx: &mut Context<Self>) -> Result<(), String> {
        let Some(active) = self.editing_ui.active.as_mut() else {
            return Ok(());
        };
        let editor = active.input.read(cx);
        if active.session.is_saving() {
            if editor.text() != active.session.draft() {
                return Err(
                    "Editor/save buffer mismatch; retaining both buffers and refusing navigation."
                        .into(),
                );
            }
            return Ok(());
        }
        active
            .session
            .replace_text_with_composition(editor.text().to_owned(), editor.is_composing())
    }
    pub(crate) fn request_exit(&mut self, cx: &mut Context<Self>) {
        if !self.guard_transition(Transition::Quit, cx) {
            self.execute_transition(Transition::Quit, cx);
        }
    }
    fn execute_transition(&mut self, intent: Transition, cx: &mut Context<Self>) {
        self.clear_editor();
        self.editing_ui.bypass = true;
        match intent {
            Transition::InternalLink(intent) => self.open_internal_link(intent, cx),
            Transition::Note(key) => self.open_live_key(key, cx),
            Transition::LegacyNote(id) => {
                if let Some(i) = self.snapshot.notes.iter().position(|n| n.note_id == id) {
                    self.select_from_ui(i, cx);
                }
            }
            Transition::ActivateTab(key) => self.activate_tab(key, cx),
            Transition::CloseTab(key) => self.close_tab(&key, cx),
            Transition::Home => self.open_home(cx),
            Transition::Reload => self.load_notes(cx),
            Transition::Graph => self.load_graph(cx),
            Transition::FullGraph => self.open_full_graph(cx),
            Transition::WorkingVault(scope) => self.change_working_vault(scope, cx),
            Transition::Quit => {
                self.editing_ui.exit_requested = true;
                let target = cx.entity().downgrade();
                request_quit(cx, self.quit_session.clone(), Some(target));
            }
        }
        self.editing_ui.bypass = false;
        cx.notify();
    }
    pub(crate) fn editor_shutdown_failed(&mut self, message: String, cx: &mut Context<Self>) {
        self.editing_ui.shutdown_failed(message.clone());
        self.status = message.clone();
        self.read_error = Some(message);
        cx.notify();
    }
    pub(crate) fn editor_environment_changed(&mut self, message: &str) {
        if self.editing_ui.active.is_some() {
            self.editing_ui.pending = None;
            self.editing_ui.message = format!(
                "{message} Local draft retained; saving requires the original verified server/storage identity."
            );
        }
        self.editing_ui.exit_requested = false;
    }
    fn editing_source(&self) -> Result<(u64, live::Storage, live::NoteResponse), String> {
        if !self.editing_ui.enabled() {
            return Err("Editing is disabled. Developer disposable-fixture opt-in is required; this build is not release-ready.".into());
        }
        if self.partial_read || self.read_error.is_some() || self.quit_session.failure().is_some() {
            return Err("Source/storage is partial or unavailable; editing is disabled.".into());
        }
        let live = self
            .live
            .as_ref()
            .ok_or("Only owned live raw source is editable.")?;
        let storage = live
            .initial_storage
            .clone()
            .ok_or("Storage readiness is unknown.")?;
        if self.accepted_storage_generation != Some(storage.generation) {
            return Err("Storage generation is not verified.".into());
        }
        let note = self
            .current_note
            .clone()
            .ok_or("Read a note before editing.")?;
        if !fixture_note(
            self.editing_ui.fixture_root.as_ref().unwrap(),
            Path::new(&note.note.path),
        ) {
            return Err(
                "Note path is outside the opted-in disposable fixture, missing, or unresolved."
                    .into(),
            );
        }
        Ok((
            live.client().launch_generation,
            storage,
            live::NoteResponse {
                note,
                related_and_diagnostics: self.current_related.clone(),
            },
        ))
    }
    pub(crate) fn set_editor_mode(&mut self, mode: EditorMode, cx: &mut Context<Self>) {
        if self.editing_ui.pending.is_some() {
            return;
        }
        if self.editing_ui.active.is_none() && mode != EditorMode::Preview {
            let result = self.editing_source().and_then(|(launch, storage, source)| {
                EditSession::from_note(launch, &storage, &source).map(|session| (session, source))
            });
            let (session, source) = match result {
                Ok(value) => value,
                Err(message) => {
                    self.editing_ui.message = message;
                    self.shell.diagnostics = true;
                    cx.notify();
                    return;
                }
            };
            let input = cx.new(|cx| editor_input::TextEditor::new(session.draft(), cx));
            let subscription = cx.subscribe(
                &input,
                |this, entity, event: &editor_input::EditorChanged, cx| {
                    let composing = entity.read(cx).is_composing();
                    if let Some(active) = this.editing_ui.active.as_mut() {
                        if active.input != entity {
                            return;
                        }
                        if let Err(message) = active
                            .session
                            .replace_text_with_composition(event.0.clone(), composing)
                        {
                            this.editing_ui.message = message;
                        }
                    }
                    cx.notify();
                },
            );
            self.request += 1;
            self.graph_request += 1;
            self.query_epoch
                .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            self.shell.home = false;
            self.show_graph = false;
            self.editing_ui.active = Some(ActiveDraft {
                session,
                input,
                _subscription: subscription,
                source_path: source.note.note.path.into(),
                title: source.note.note.title,
            });
            self.editing_ui.message = "Developer fixture only. One draft; memory-only recovery. Native dirty/quit GUI QA is still pending.".into();
        }
        self.editing_ui.mode = mode;
        self.editing_ui.mode_menu = false;
        cx.notify();
    }
    pub(crate) fn editor_save(&mut self, leaving: bool, cx: &mut Context<Self>) {
        if self
            .editing_ui
            .active
            .as_ref()
            .is_some_and(|a| a.session.is_saving())
        {
            self.editing_ui.message =
                "A save is already in flight. Editing remains locked; no duplicate request sent."
                    .into();
            cx.notify();
            return;
        }
        if let Err(message) = self.sync_editor(cx) {
            self.editing_ui.message = message;
            cx.notify();
            return;
        }
        let Some(live) = self.live.clone() else {
            self.editing_ui.message = "Owned server unavailable; draft retained.".into();
            cx.notify();
            return;
        };
        let Some(storage) = live.initial_storage.clone() else {
            return;
        };
        if self.quit_session.failure().is_some()
            || self.accepted_storage_generation != Some(storage.generation)
        {
            self.editing_ui.pending = None;
            self.editing_ui.message = "Storage/server is no longer verified; no save sent.".into();
            cx.notify();
            return;
        }
        let Some(active) = self.editing_ui.active.as_mut() else {
            return;
        };
        if !self
            .editing_ui
            .fixture_root
            .as_ref()
            .is_some_and(|root| fixture_note(root, &active.source_path))
        {
            self.editing_ui.message = "Fixture path check failed; no save sent.".into();
            cx.notify();
            return;
        }
        if let Err(message) = active
            .input
            .update(cx, |editor, cx| editor.set_read_only(true, cx))
        {
            self.editing_ui.message = message.into();
            cx.notify();
            return;
        }
        let launch = live.client().launch_generation;
        let prepared = if leaving {
            active
                .session
                .choose_leave(LeaveChoice::Save, launch, &storage)
                .map(|action| match action {
                    LeaveAction::Save(save) => Some(save),
                    _ => None,
                })
        } else {
            active.session.prepare_save(launch, &storage)
        };
        let prepared = match prepared {
            Ok(Some(save)) => save,
            Ok(None) => {
                let _ = active
                    .input
                    .update(cx, |editor, cx| editor.set_read_only(false, cx));
                if leaving {
                    if let Some(intent) = self.editing_ui.pending.take() {
                        self.execute_transition(intent, cx);
                    }
                }
                cx.notify();
                return;
            }
            Err(message) => {
                let _ = active
                    .input
                    .update(cx, |editor, cx| editor.set_read_only(false, cx));
                self.editing_ui.pending = None;
                self.editing_ui.message = message;
                self.shell.diagnostics = true;
                cx.notify();
                return;
            }
        };
        self.editing_ui.inspection_ticket += 1;
        self.editing_ui.inspecting = false;
        self.editing_ui.message =
            "Saving explicit fixture draft… Editing is locked; no duplicate save is sent.".into();
        let attempt = prepared.attempt;
        let key = prepared.request.key.clone();
        let job = cx
            .background_executor()
            .spawn(async move { live.client().save(&prepared.request) });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                let current_launch = this
                    .live
                    .as_ref()
                    .map(|s| s.client().launch_generation)
                    .unwrap_or(u64::MAX);
                let current_storage = this.accepted_storage_generation.unwrap_or(u64::MAX);
                let Some(active) = this.editing_ui.active.as_mut() else {
                    return;
                };
                if active.session.key() != &key || active.session.launch_generation() != launch {
                    return;
                }
                let outcome =
                    active
                        .session
                        .finish_save(attempt, current_launch, current_storage, result);
                if outcome == SaveOutcome::StaleResponse {
                    return;
                }
                let _ = active
                    .input
                    .update(cx, |editor, cx| editor.set_read_only(false, cx));
                match outcome {
                    SaveOutcome::Saved { leave } => {
                        let baseline = active.session.baseline().to_owned();
                        if let Err(message) = active
                            .input
                            .update(cx, |editor, cx| editor.mark_saved(&baseline, cx))
                        {
                            this.editing_ui.pending = None;
                            this.editing_ui.message = format!(
                                "Save acknowledged, but {message}; editor buffer preserved."
                            );
                            cx.notify();
                            return;
                        }
                        if let Some(note) =
                            this.current_note.as_mut().filter(|n| n.note.key() == key)
                        {
                            note.body = baseline.clone();
                            note.etag = active.session.etag().into();
                        }
                        this.content = markdown::parse(&baseline);
                        this.editing_ui.message =
                            "Fixture save acknowledged. Authored text and undo history retained."
                                .into();
                        if leave {
                            if let Some(mut intent) = this.editing_ui.pending.take() {
                                acknowledge_saved_transition(&mut intent);
                                this.execute_transition(intent, cx);
                            }
                        }
                    }
                    SaveOutcome::Retained => {
                        this.shell.diagnostics = true;
                        this.editing_ui.pending = None;
                        this.editing_ui.message = format!(
                            "{:?}. Local draft retained; no automatic retry.",
                            active.session.state()
                        );
                    }
                    SaveOutcome::StaleResponse => {}
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn editor_leave_choice(&mut self, choice: LeaveChoice, cx: &mut Context<Self>) {
        if choice == LeaveChoice::Save {
            self.editor_save(true, cx);
            return;
        }
        if let Err(message) = self.sync_editor(cx) {
            self.editing_ui.message = message;
            cx.notify();
            return;
        }
        let Some(active) = self.editing_ui.active.as_mut() else {
            return;
        };
        // Cancel and Discard send no request and do not need current storage readiness.
        let fallback = live::Storage {
            phase: "unavailable".into(),
            read_only: true,
            verified: 0,
            generation: active.session.storage_generation(),
            reason: None,
            home_note_id: None,
            additional: Default::default(),
        };
        match active
            .session
            .choose_leave(choice, active.session.launch_generation(), &fallback)
        {
            Ok(LeaveAction::Leave) => {
                if let Some(intent) = self.editing_ui.pending.take() {
                    self.execute_transition(intent, cx);
                }
            }
            Ok(LeaveAction::Stay) => {
                self.editing_ui.pending = None;
            }
            Ok(LeaveAction::Save(_)) => unreachable!(),
            Err(message) => self.editing_ui.message = message,
        }
        cx.notify();
    }
    fn editor_inspect(&mut self, cx: &mut Context<Self>) {
        if self.editing_ui.inspecting {
            return;
        }
        let Some(active) = self.editing_ui.active.as_ref() else {
            return;
        };
        if active.session.is_saving() {
            return;
        }
        let Some(live) = self.live.clone() else {
            return;
        };
        let key = active.session.key().clone();
        let expected_input = active.input.clone();
        let expected_launch = active.session.launch_generation();
        let expected_generation = active.session.storage_generation();
        self.editing_ui.inspecting = true;
        self.editing_ui.inspection_ticket += 1;
        let inspection_ticket = self.editing_ui.inspection_ticket;
        let job = cx.background_executor().spawn(async move {
            let client = live.client();
            if client.launch_generation != expected_launch {
                return Err(live::Error::Stale);
            }
            let before = client.storage()?;
            let source = client.note(&key)?;
            let after = client.storage()?;
            if before.generation != expected_generation || after.generation != expected_generation {
                return Err(live::Error::Stale);
            }
            Ok((key, source, after))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.editing_ui.inspection_ticket != inspection_ticket { return; }
                let Some(active) = this.editing_ui.active.as_mut() else { return; };
                if active.input != expected_input { return; }
                this.editing_ui.inspecting = false;
                if this.quit_session.failure().is_some()
                    || this.accepted_storage_generation != Some(expected_generation)
                    || !this.live.as_ref().is_some_and(|session| session.client().launch_generation == expected_launch) {
                    this.editing_ui.message = "Server/storage changed during comparison; draft retained.".into();
                    cx.notify(); return;
                }
                if active.session.launch_generation() != expected_launch || active.session.storage_generation() != expected_generation { return; }
                match result {
                    Ok((key, source, storage)) if &key == active.session.key() => {
                        this.editing_ui.message = match active.session.inspect_latest(expected_launch, &storage, &source) {
                            Ok(()) => "Comparison fetched without replacing draft. GET hides final LF count; ambiguous LF reconciliation is not enabled in this fixture UI.".into(),
                            Err(message) => message,
                        };
                    }
                    Ok(_) => {}, Err(error) => this.editing_ui.message = format!("Comparison unavailable: {error}. Draft retained."),
                }
                cx.notify();
            });
        }).detach();
        cx.notify();
    }
    fn editor_accept_comparison(&mut self, cx: &mut Context<Self>) {
        if self.editing_ui.inspecting {
            self.editing_ui.message =
                "Wait for the current comparison before accepting a revision.".into();
            cx.notify();
            return;
        }
        if let Err(message) = self.sync_editor(cx) {
            self.editing_ui.message = message;
            cx.notify();
            return;
        }
        let Some(active) = self.editing_ui.active.as_mut() else {
            return;
        };
        if !comparison_context_matches(
            active.session.launch_generation(),
            active.session.storage_generation(),
            self.live.as_ref().map(|s| s.client().launch_generation),
            self.accepted_storage_generation,
            self.quit_session.failure().is_some(),
        ) {
            self.editing_ui.message =
                "Comparison identity is no longer current; draft retained. No revision accepted."
                    .into();
            cx.notify();
            return;
        }
        self.editing_ui.message = match active.session.accept_matching_comparison() {
            Ok(_) => {
                let text = active.session.draft().to_owned();
                if let Some(note) = self
                    .current_note
                    .as_mut()
                    .filter(|n| &n.note.key() == active.session.key())
                {
                    note.body = text.clone();
                    note.etag = active.session.etag().into();
                }
                self.content = markdown::parse(&text);
                match active.input.update(cx, |editor, cx| editor.mark_saved(&text, cx)) {
                    Ok(()) => "Explicitly accepted matching GET revision. No write sent; authored text retained.".into(), Err(message) => message.into(),
                }
            }
            Err(message) => message,
        };
        cx.notify();
    }
    pub(crate) fn sync_editor_theme(&mut self, dark: bool, cx: &mut Context<Self>) {
        if let Some(active) = &self.editing_ui.active {
            active
                .input
                .update(cx, |editor, cx| editor.set_dark(dark, cx));
        }
    }
    pub(crate) fn render_edit_controls(&self, cx: &mut Context<Self>) -> Div {
        let mut rail = div().flex().flex_col().items_center().gap_1();
        rail = rail.child(
            div()
                .id("editor-mode-menu")
                .w(px(38.))
                .h(px(32.))
                .flex()
                .items_center()
                .justify_center()
                .cursor_pointer()
                .child("▦")
                .on_click(cx.listener(|this, _, _, cx| {
                    this.editing_ui.mode_menu = !this.editing_ui.mode_menu;
                    cx.notify();
                })),
        );
        if self.editing_ui.active.is_some() && self.editing_ui.mode != EditorMode::Preview {
            let saving = self
                .editing_ui
                .active
                .as_ref()
                .is_some_and(|a| a.session.is_saving());
            rail = rail.child(
                div()
                    .id("editor-save")
                    .w(px(42.))
                    .h(px(28.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .text_xs()
                    .cursor_pointer()
                    .child(if saving { "…" } else { "Save" })
                    .on_click(cx.listener(|this, _, _, cx| this.editor_save(false, cx))),
            );
        }
        rail
    }
    pub(crate) fn render_editor_mode_menu(
        &self,
        dark: bool,
        height: gpui::Pixels,
        cx: &mut Context<Self>,
    ) -> Div {
        let mut menu = div()
            .absolute()
            .left(px(64.))
            .top((height - px(250.)) / 2. + px(100.))
            .w(px(140.))
            .p_2()
            .rounded_lg()
            .bg(rgb(if dark { 0x222222 } else { 0xffffff }))
            .border_1()
            .border_color(rgb(if dark { 0x444444 } else { 0xdddddd }));
        if !self.editing_ui.mode_menu {
            return div();
        }
        for (mode, label) in [
            (EditorMode::Preview, "Preview"),
            (EditorMode::Edit, "Edit"),
            (EditorMode::Split, "Split"),
        ] {
            menu = menu.child(
                div()
                    .id(label)
                    .px_3()
                    .py_2()
                    .cursor_pointer()
                    .child(label)
                    .on_click(cx.listener(move |this, _, _, cx| this.set_editor_mode(mode, cx))),
            );
        }
        menu
    }
    pub(crate) fn editor_fixture_notice(&self) -> &'static str {
        match self.editing_ui.active.as_ref().map(|a| a.session.state()) {
            Some(editing::DraftState::Uncertain(_)) => {
                "Fixture only · save outcome unknown; draft kept. See diagnostics."
            }
            Some(
                editing::DraftState::Conflict(_)
                | editing::DraftState::Rejected(_)
                | editing::DraftState::NotSent(_),
            ) => "Fixture only · save not completed; draft kept. See diagnostics.",
            _ => "Fixture editing · not release-ready · drafts kept in memory only",
        }
    }
    pub(crate) fn render_edit_diagnostics(&self, cx: &mut Context<Self>) -> Div {
        let mut panel = div()
            .flex()
            .flex_col()
            .gap_2()
            .child(self.editing_ui.message.clone());
        if let Some(active) = &self.editing_ui.active {
            panel = panel
                .child(format!(
                    "Draft: {} / {} · {} · {:?} · one active draft",
                    active.session.key().vault,
                    active.session.key().note_id,
                    active.title,
                    active.session.state()
                ))
                .child(
                    div()
                        .id("editor-inspect")
                        .cursor_pointer()
                        .child("Inspect server content (no replacement)")
                        .on_click(cx.listener(|this, _, _, cx| this.editor_inspect(cx))),
                );
            if let Some(comparison) = active.session.comparison() {
                panel = panel
                    .child("Server GET comparison (final LF bytes not observable):")
                    .child(comparison.body.clone())
                    .child(
                        div()
                            .id("accept-comparison")
                            .cursor_pointer()
                            .child("Accept exact matching revision (no write)")
                            .on_click(
                                cx.listener(|this, _, _, cx| this.editor_accept_comparison(cx)),
                            ),
                    );
            }
        }
        panel
    }
    pub(crate) fn render_editor(&self, dark: bool) -> Div {
        let Some(active) = &self.editing_ui.active else {
            return div();
        };
        let mut panes = div()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .h_full()
            .flex()
            .gap_3()
            .pl(px(64.))
            .pr_4()
            .py_4();
        if self.editing_ui.mode != EditorMode::Preview {
            panes = panes.child(
                div()
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .child(active.input.clone()),
            );
        }
        if self.editing_ui.mode != EditorMode::Edit {
            panes = panes.child(
                div()
                    .id("draft-preview")
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .overflow_y_scroll()
                    .p_3()
                    .child(markdown::render_themed(
                        &markdown::parse(active.session.draft()),
                        dark,
                    )),
            );
        }
        panes
    }
    pub(crate) fn render_leave_prompt(&self, dark: bool, cx: &mut Context<Self>) -> Div {
        let Some(intent) = &self.editing_ui.pending else {
            return div();
        };
        let mut modal = div()
            .absolute()
            .inset_0()
            .bg(gpui::rgba(0x000000aa))
            .flex()
            .items_center()
            .justify_center();
        let heading = match intent {
            Transition::Quit => "Save changes before quitting?",
            Transition::CloseTab(_) => "Save changes before closing this tab?",
            Transition::Reload => "Save changes before reloading?",
            _ => "Save changes before leaving this note?",
        };
        let paper = if dark { 0x191c1e } else { 0xffffff };
        let ink = if dark { 0xe9e9e4 } else { 0x1a1a18 };
        let line = if dark { 0x282c2f } else { 0xe6e4de };
        let mark = if dark { 0xf4785e } else { 0xc13a1e };
        let mut dialog = div().w(px(460.)).p_5().rounded_lg()
            .bg(rgb(paper)).text_color(rgb(ink)).border_1().border_color(rgb(line))
            .flex().flex_col().gap_3()
            .child(heading)
            .child("Discard removes only the unsaved draft. Cancel keeps this note open. Finish text composition or an in-flight save before leaving.")
            .child(self.editing_ui.message.clone());
        for (choice, label) in [
            (LeaveChoice::Save, "Save and continue"),
            (LeaveChoice::Discard, "Discard and continue"),
            (LeaveChoice::Cancel, "Cancel"),
        ] {
            dialog = dialog.child(
                div()
                    .id(label)
                    .p_2()
                    .rounded_md()
                    .border_1()
                    .border_color(rgb(line))
                    .text_color(rgb(if choice == LeaveChoice::Save {
                        mark
                    } else {
                        ink
                    }))
                    .cursor_pointer()
                    .child(label)
                    .on_click(
                        cx.listener(move |this, _, _, cx| this.editor_leave_choice(choice, cx)),
                    ),
            );
        }
        modal = modal.child(dialog);
        modal
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn developer_editing_is_disabled_without_opt_in() {
        assert!(!EditingUi::new(None).enabled());
    }
    #[test]
    fn transitions_capture_vault_identity_not_list_index() {
        assert_ne!(
            Transition::Note(live::NoteKey {
                vault: "a".into(),
                note_id: 1
            }),
            Transition::Note(live::NoteKey {
                vault: "b".into(),
                note_id: 1
            })
        );
    }
    #[test]
    fn invalid_fixture_opt_in_fails_closed() {
        assert!(validate_fixture(Path::new("/"), Path::new("/"), None, None).is_err());
        assert!(
            validate_fixture(
                Path::new("/does-not-exist"),
                Path::new("/does-not-exist"),
                None,
                None
            )
            .is_err()
        );
    }
    struct FixtureDir(PathBuf);
    impl FixtureDir {
        fn new() -> Self {
            let nonce = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos();
            let path = std::env::temp_dir().join(format!(
                "track-native-edit-validator-{}-{nonce}",
                std::process::id()
            ));
            std::fs::create_dir_all(path.join("vault")).unwrap();
            std::fs::create_dir_all(path.join("cache")).unwrap();
            std::fs::write(path.join("config"), "{}\n").unwrap();
            Self(path.canonicalize().unwrap())
        }
        fn vault(&self) -> PathBuf {
            self.0.join("vault")
        }
        fn marker(&self) {
            std::fs::write(
                self.vault().join(".track-native-edit-fixture"),
                "disposable-fixture-only\n",
            )
            .unwrap();
        }
        fn validate(&self) -> Result<PathBuf, String> {
            validate_fixture(
                &self.vault(),
                &self.vault(),
                Some(&self.0.join("config")),
                Some(&self.0.join("cache")),
            )
        }
    }
    impl Drop for FixtureDir {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }
    #[test]
    fn fixture_requires_preexisting_exact_marker_and_matching_vault() {
        let fixture = FixtureDir::new();
        assert!(fixture.validate().is_err());
        assert!(!fixture.vault().join(".track-native-edit-fixture").exists());
        std::fs::write(fixture.vault().join(".track-native-edit-fixture"), "yes").unwrap();
        assert!(fixture.validate().is_err());
        fixture.marker();
        assert_eq!(fixture.validate().unwrap(), fixture.vault());
        assert!(
            validate_fixture(
                &fixture.vault(),
                &fixture.0,
                Some(&fixture.0.join("config")),
                Some(&fixture.0.join("cache"))
            )
            .is_err()
        );
        assert!(
            validate_fixture(
                &fixture.vault(),
                &fixture.vault(),
                None,
                Some(&fixture.0.join("cache"))
            )
            .is_err()
        );
        assert!(
            validate_fixture(
                &fixture.vault(),
                &fixture.vault(),
                Some(Path::new("/")),
                Some(&fixture.0.join("cache"))
            )
            .is_err()
        );
        assert!(
            validate_fixture(
                &fixture.vault(),
                &fixture.vault(),
                Some(&fixture.0.join("config")),
                Some(Path::new("/"))
            )
            .is_err()
        );
    }
    #[test]
    fn fixture_note_paths_stay_under_opted_in_root_and_recheck_marker() {
        let fixture = FixtureDir::new();
        fixture.marker();
        std::fs::write(fixture.vault().join("note.md"), "disposable").unwrap();
        std::fs::write(fixture.0.join("outside.md"), "disposable outside").unwrap();
        assert!(fixture_note(&fixture.vault(), Path::new("note.md")));
        assert!(!fixture_note(
            &fixture.vault(),
            &fixture.0.join("outside.md")
        ));
        assert!(!fixture_note(&fixture.vault(), Path::new("../outside.md")));
        assert!(!fixture_note(&fixture.vault(), Path::new("missing.md")));
        std::fs::remove_file(fixture.vault().join(".track-native-edit-fixture")).unwrap();
        assert!(!fixture_note(&fixture.vault(), Path::new("note.md")));
    }
    #[cfg(unix)]
    #[test]
    fn canonical_alias_is_accepted_but_symlink_escape_is_not() {
        use std::os::unix::fs::symlink;
        let fixture = FixtureDir::new();
        fixture.marker();
        symlink(fixture.vault(), fixture.0.join("alias")).unwrap();
        assert_eq!(
            validate_fixture(
                &fixture.0.join("alias"),
                &fixture.vault(),
                Some(&fixture.0.join("config")),
                Some(&fixture.0.join("cache"))
            )
            .unwrap(),
            fixture.vault()
        );
        std::fs::write(fixture.0.join("outside.md"), "disposable outside").unwrap();
        symlink(
            fixture.0.join("outside.md"),
            fixture.vault().join("escaped.md"),
        )
        .unwrap();
        assert!(!fixture_note(&fixture.vault(), Path::new("escaped.md")));
        std::fs::remove_file(fixture.vault().join(".track-native-edit-fixture")).unwrap();
        std::fs::write(fixture.0.join("marker"), "disposable-fixture-only\n").unwrap();
        symlink(
            fixture.0.join("marker"),
            fixture.vault().join(".track-native-edit-fixture"),
        )
        .unwrap();
        assert!(fixture.validate().is_err());
    }
    #[test]
    fn unified_gate_blocks_repeated_destinations_and_protects_dirty_saving_and_ime() {
        let intents = [
            Transition::Home,
            Transition::Reload,
            Transition::Graph,
            Transition::FullGraph,
            Transition::Quit,
            Transition::Note(live::NoteKey {
                vault: "a".into(),
                note_id: 1,
            }),
            Transition::LegacyNote(1),
            Transition::ActivateTab(tabs::TabKey::Home),
            Transition::CloseTab(tabs::TabKey::Home),
            Transition::WorkingVault("another".into()),
        ];
        for _ in intents {
            assert_eq!(
                transition_decision(false, false, None),
                GateDecision::Proceed
            );
            assert_eq!(
                transition_decision(false, false, Some(LeavePrompt::Leave)),
                GateDecision::Proceed
            );
            for prompt in [
                LeavePrompt::AskSaveDiscardCancel,
                LeavePrompt::WaitForSave,
                LeavePrompt::WaitForComposition,
            ] {
                assert_eq!(
                    transition_decision(false, false, Some(prompt)),
                    GateDecision::Prompt
                );
                assert_eq!(
                    transition_decision(true, false, Some(prompt)),
                    GateDecision::Block
                );
            }
            assert_eq!(transition_decision(false, true, None), GateDecision::Block);
        }
    }
    #[test]
    fn delayed_comparison_cannot_apply_after_epoch_or_server_failure() {
        assert!(comparison_context_matches(1, 2, Some(1), Some(2), false));
        assert!(!comparison_context_matches(1, 2, Some(1), Some(3), false));
        assert!(!comparison_context_matches(1, 2, Some(2), Some(2), false));
        assert!(!comparison_context_matches(1, 2, Some(1), None, false));
        assert!(!comparison_context_matches(1, 2, Some(1), Some(2), true));
    }
    #[test]
    fn fixture_config_must_be_file_and_cache_must_be_directory() {
        let fixture = FixtureDir::new();
        fixture.marker();
        assert!(
            validate_fixture(
                &fixture.vault(),
                &fixture.vault(),
                Some(&fixture.0.join("cache")),
                Some(&fixture.0.join("cache"))
            )
            .is_err()
        );
        assert!(
            validate_fixture(
                &fixture.vault(),
                &fixture.vault(),
                Some(&fixture.0.join("config")),
                Some(&fixture.0.join("config"))
            )
            .is_err()
        );
    }
    #[test]
    fn shutdown_failure_reopens_transition_gate_for_explicit_retry() {
        let mut ui = EditingUi::new(None);
        ui.exit_requested = true;
        assert_eq!(
            transition_decision(false, ui.exit_requested, None),
            GateDecision::Block
        );
        ui.shutdown_failed("shutdown failed; retry available".into());
        assert_eq!(
            transition_decision(ui.pending.is_some(), ui.exit_requested, None),
            GateDecision::Proceed
        );
        assert!(ui.message.contains("retry"));
    }
    #[test]
    fn outside_temp_regular_config_and_directory_note_are_rejected() {
        let fixture = FixtureDir::new();
        fixture.marker();
        assert!(!fixture_note(&fixture.vault(), &fixture.vault()));
        let outside = Path::new("/etc/hosts");
        if outside.is_file() {
            assert!(
                validate_fixture(
                    &fixture.vault(),
                    &fixture.vault(),
                    Some(outside),
                    Some(&fixture.0.join("cache"))
                )
                .is_err()
            );
        }
    }
}

#[cfg(test)]
mod fragment_transition_tests {
    use super::*;
    #[test]
    fn save_replay_preserves_frozen_link_payload_and_only_enables_epoch_adoption() {
        let payload = shell::InternalLinkIntent {
            source: live::NoteKey {
                vault: "one".into(),
                note_id: 1,
            },
            destination: live::NoteKey {
                vault: "two".into(),
                note_id: 2,
            },
            target: markdown::LinkTarget::Markdown {
                destination: "Target.md#heading".into(),
            },
            launch_generation: 3,
            storage_generation: 4,
            allow_epoch_advance: false,
        };
        let mut transition = Transition::InternalLink(payload.clone());
        assert_eq!(transition, Transition::InternalLink(payload.clone()));
        acknowledge_saved_transition(&mut transition);
        let Transition::InternalLink(saved) = transition else {
            panic!("lost link payload")
        };
        assert!(saved.allow_epoch_advance);
        assert_eq!(saved.destination, payload.destination);
        assert_eq!(saved.target, payload.target);
        assert_eq!(saved.launch_generation, payload.launch_generation);
    }
}
