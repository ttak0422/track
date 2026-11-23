//! Production read-only review. No serving authority exists before explicit confirmation.
use super::*;
use crate::workspace_read_request::{Decision, Event, EventStamp, RequestDialog};
use gpui::FocusHandle;

#[derive(Default)]
pub(crate) struct Ui {
    lifetime: Lifetime,
    ordinary: bool,
    active: bool,
    preparing: bool,
    retired: bool,
    panel: Option<Panel>,
    message: String,
    cache_parent: String,
    cancel_focus: Option<FocusHandle>,
    confirm_focus: Option<FocusHandle>,
    focus_cancel: bool,
}
#[derive(Default)]
struct Lifetime {
    generation: u64,
    cancelled: bool,
}
impl Lifetime {
    fn advance(&mut self) -> Option<u64> {
        if self.cancelled {
            return None;
        }
        self.generation = self.generation.checked_add(1)?;
        Some(self.generation)
    }
    fn accepts(&self, generation: u64) -> bool {
        !self.cancelled && self.generation == generation
    }
    fn cancel(&mut self) {
        self.cancelled = true;
    }
}
struct Panel {
    review: live::ordinary::ReadReview,
    dialog: RequestDialog,
}
impl Ui {
    pub(crate) fn generation(&self) -> u64 {
        self.lifetime.generation
    }
    pub(crate) fn accepts(&self, generation: u64) -> bool {
        !self.ordinary || self.lifetime.accepts(generation)
    }
    pub(crate) fn active(&self) -> bool {
        self.active
    }
    pub(crate) fn finished(&mut self, succeeded: bool) {
        if self.ordinary {
            self.active = !succeeded;
            self.preparing = false;
        }
    }
}
impl Workspace {
    pub(crate) fn review_ordinary_read(
        &mut self,
        request: live::ordinary::ReadRequest,
        cx: &mut Context<Self>,
    ) {
        let Some(generation) = self.admission.lifetime.advance() else {
            return;
        };
        self.admission.ordinary = true;
        self.admission.active = true;
        self.admission.preparing = true;
        self.admission.message =
            "Checking workspace configuration and private-cache separation…".into();
        if let Err(error) = self.quit_session.begin_start() {
            self.admission.message = error.to_string();
            self.admission.preparing = false;
            return;
        }
        let owner = self.quit_session.clone();
        let job = cx
            .background_executor()
            .spawn(async move { owner.review_read(request) });
        cx.spawn(async move |this,cx| {
            let result = job.await;
            let _ = this.update(cx, |this,cx| {
                if this.quit_session.is_closing() || !this.admission.accepts(generation) { return; }
                this.admission.preparing = false;
                match result {
                    Ok(mut review) => {
                        this.admission.cache_parent = review.cache_parent().display().to_string();
                        let dialog = review.dialog();
                        this.admission.panel = Some(Panel { review, dialog });
                        this.admission.message = "Review the active vault and every registered peer below before allowing reads.".into();
                        this.admission.focus_cancel = true;
                    }
                    Err(error) => this.admission.message = format!("Read-only request rejected: {error}. No CLI or writable fallback. Quit and restart to review again."),
                }
                cx.notify();
            });
        }).detach();
    }
    fn choose_ordinary_read(
        &mut self,
        generation: u64,
        stamp: Option<&EventStamp>,
        event: Event,
        cx: &mut Context<Self>,
    ) {
        if self.quit_session.is_closing() || !self.admission.accepts(generation) {
            return;
        }
        if let Some(panel) = &mut self.admission.panel {
            if let Some(stamp) = stamp {
                if !panel.dialog.apply(stamp, event) {
                    return;
                }
            } else if event == Event::ExplicitConfirm {
                return;
            }
        } else if event == Event::ExplicitConfirm {
            return;
        }
        match event {
            Event::ExplicitConfirm => {
                let Some(panel) = self.admission.panel.take() else {
                    return;
                };
                match panel.review.authorize(&panel.dialog) {
                    Ok(grant) => {
                        if self.admission.lifetime.advance().is_none() {
                            return;
                        }
                        self.admission.preparing = true;
                        self.admission.message = "Rechecking the reviewed configuration and starting the owned read-only server…".into();
                        self.start_ordinary_read(grant, cx);
                    }
                    Err(error) => self.admission.message = error.to_string(),
                }
            }
            Event::Cancel | Event::Close => {
                self.admission.lifetime.cancel();
                self.quit_session.cancel_startup();
                self.admission.panel.take();
                self.admission.preparing = false;
                self.admission.message = "Workspace read request cancelled. Quit and relaunch to review a new request. Any nonempty private cache is retained.".into();
                let owner = self.quit_session.clone();
                let job = cx.background_executor().spawn(async move {
                    let result = owner.shutdown();
                    (result, owner.failure(), owner.runtime_cache())
                });
                cx.spawn(async move |this, cx| {
                    let (result, failure, cache) = job.await;
                    let _ = this.update(cx, |this, cx| {
                        if let Err(error) = result {
                            this.admission.message =
                                format!("Cancellation cleanup is unconfirmed: {error}");
                        }
                        if let Some(path) = cache {
                            this.admission.message.push_str(&format!(
                                " Private runtime retained at {}.",
                                path.display()
                            ));
                        }
                        if let Some(error) = failure {
                            this.admission.message.push_str(&format!(" {error}"));
                        }
                        cx.notify();
                    });
                })
                .detach();
            }
            Event::Enter => {}
        }
        cx.notify();
    }
    /// Shared atomic authority is checked before publishing the next frame.
    /// Old accepted source stays visible only with a persistent stale notice.
    pub(crate) fn retire_invalid_read(&mut self, cx: &mut Context<Self>) -> bool {
        let invalid =
            self.admission.ordinary && self.live.as_ref().is_some_and(|s| !s.client().is_alive());
        if invalid && !self.admission.retired {
            self.admission.retired = true;
            self.editor_environment_changed("Read-only session invalidated.");
            self.request += 1;
            self.graph_request += 1;
            self.query_epoch
                .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            self.invalidate_full_graph(cx);
            self.accepted_storage_generation = None;
            self.partial_read = true;
        }
        if invalid {
            self.read_error = Some("Read-only session is unavailable. Displayed source is stale; queued and future reads are rejected. Quit and review a new session.".into());
        }
        invalid
    }
    pub(crate) fn render_read_admission(
        &mut self,
        colors: (u32, u32, u32, u32, u32),
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<gpui::Div> {
        let (paper, ink, muted, line, mark) = colors;
        let cancel = self
            .admission
            .cancel_focus
            .get_or_insert_with(|| cx.focus_handle())
            .clone();
        let confirm = self
            .admission
            .confirm_focus
            .get_or_insert_with(|| cx.focus_handle())
            .clone();
        if self.admission.focus_cancel
            || self
                .admission
                .cancel_focus
                .as_ref()
                .is_some_and(|_| !cancel.is_focused(window) && !confirm.is_focused(window))
        {
            window.focus(&cancel);
            self.admission.focus_cancel = false;
        }
        let stamp = self.admission.panel.as_ref().map(|p| p.dialog.stamp());
        let generation = self.admission.generation();
        let keyboard = stamp.clone();
        let confirm_stamp = stamp.clone();
        let cancel_stamp = stamp;
        let request = self.admission.panel.as_ref().map(|p| p.dialog.request());
        let active = request.map(|r| {
            let (n, p) = r.active();
            format!(
                "Active vault: {}\n{p}",
                if n.is_empty() { "Explicit path" } else { n }
            )
        });
        let peers = request
            .map(|r| {
                r.rows()
                    .map(|(n, p, _)| format!("{n}\n{p}"))
                    .collect::<Vec<_>>()
            })
            .unwrap_or_default();
        let pending = self
            .admission
            .panel
            .as_ref()
            .is_some_and(|p| p.dialog.decision() == Decision::Pending);
        let key_cancel = cancel.clone();
        let key_confirm = confirm.clone();
        div().id("workspace-read-admission").size_full().overflow_y_scroll().bg(rgb(paper)).text_color(rgb(ink)).p_6().flex().flex_col().gap_4()
            .on_key_down(cx.listener(move|this,event:&gpui::KeyDownEvent,window,cx|{
                match event.keystroke.key.as_str(){
                    "escape"=>this.choose_ordinary_read(generation,keyboard.as_ref(),Event::Close,cx),
                    "enter"=>{},
                    "tab"=>{if pending&&key_cancel.is_focused(window){window.focus(&key_confirm)}else{window.focus(&key_cancel)}},
                    "space" if !event.is_held && !event.keystroke.modifiers.modified()=>this.choose_ordinary_read(generation,keyboard.as_ref(),if key_confirm.is_focused(window){Event::ExplicitConfirm}else{Event::Cancel},cx),
                    _=>return,
                }
                cx.stop_propagation();
            }))
            .child(div().text_lg().child("Open workspace read-only"))
            .child(self.admission.message.clone())
            .when_some(active,|v,s|v.child(div().id("read-active-path").overflow_x_scroll().child(s)))
            .child(div().id("read-peer-paths").max_h_64().overflow_y_scroll().overflow_x_scroll().flex().flex_col().gap_3().children(peers.into_iter().map(|s|div().child(s))))
            .child("Allow reading this active vault and all listed registered peers. Notes, metadata and activity remain read-only. Editing, creation and persisted sessions are disabled. Live updates, Editor Follow and automatic refresh are unavailable; use explicit Reload.")
            .child(div().text_color(rgb(muted)).child(format!("Derived indexes use a newly owned private directory beneath: {}. Nonempty caches remain after exit or failure; they are never recursively deleted.",self.admission.cache_parent)))
            .child("Cancel is focused first. Enter never confirms. Click Allow read-only access, or focus it and press Space. A changed configuration requires a new review.")
            .when_some(self.read_error.clone(),|v,error|v.child(div().text_color(rgb(mark)).child(error)))
            .child(div().flex().gap_3()
                .child(div().id("cancel-read-only-workspace").track_focus(&cancel).tab_index(0).px_3().py_2().border_1().border_color(rgb(line)).rounded_md().child(if self.admission.preparing{"Cancel pending request"}else{"Cancel"}).on_click(cx.listener(move|this,_,_,cx|this.choose_ordinary_read(generation,cancel_stamp.as_ref(),Event::Cancel,cx))))
                .when(pending,|v|v.child(div().id("allow-read-only-workspace").track_focus(&confirm).tab_index(0).px_3().py_2().border_1().border_color(rgb(mark)).rounded_md().child("Allow read-only access").on_click(cx.listener(move|this,_,_,cx|this.choose_ordinary_read(generation,confirm_stamp.as_ref(),Event::ExplicitConfirm,cx))))))
    }
}

#[cfg(test)]
mod tests {
    use super::Lifetime;
    #[test]
    fn cancellation_rejects_worker_finished_before_or_after_cancel() {
        for worker_already_finished in [false, true] {
            let mut state = Lifetime::default();
            let ticket = state.advance().unwrap();
            let queued_result = worker_already_finished.then_some(ticket);
            state.cancel();
            assert!(!state.accepts(queued_result.unwrap_or(ticket)));
            assert!(state.advance().is_none());
        }
    }
    #[test]
    fn entering_startup_retires_every_old_control_and_completion() {
        let mut state = Lifetime::default();
        let review = state.advance().unwrap();
        let startup = state.advance().unwrap();
        assert!(!state.accepts(review));
        assert!(state.accepts(startup));
        state.cancel();
        assert!(!state.accepts(startup));
    }
}
