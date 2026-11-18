//! Test-only presentation with an optional independently authorized fixture preflight.
//! No production shell registration, ordinary admission, or serving.
use crate::live::preview_fixture_gate_tests::{FixturePreflightReview, FixtureWorkerResult};
use crate::workspace_preflight_task::{Phase, PreviewFlight};
use crate::workspace_read_request::{
    CONSEQUENCES, Decision, Event, EventStamp, NOTICE, RequestDialog, WorkspaceRequest,
};
use gpui::{Context, FocusHandle, Window, div, prelude::*, rgb};

/// Supplied existing theme tokens, not an independent color scheme.
#[derive(Clone, Copy)]
pub struct Palette {
    pub panel: u32,
    pub ink: u32,
    pub muted: u32,
    pub line: u32,
    pub mark: u32,
}
pub struct WorkspaceReadRequestView {
    model: RequestDialog,
    palette: Palette,
    cancel_focus: FocusHandle,
    confirm_focus: FocusHandle,
    focus_cancel: bool,
    review: Option<FixturePreflightReview>,
    flight: PreviewFlight,
    result: Option<FixtureWorkerResult>,
}
impl WorkspaceReadRequestView {
    pub fn fixture(request: WorkspaceRequest, palette: Palette, cx: &mut Context<Self>) -> Self {
        Self {
            model: RequestDialog::new(request),
            palette,
            cancel_focus: cx.focus_handle(),
            confirm_focus: cx.focus_handle(),
            focus_cancel: true,
            review: None,
            flight: PreviewFlight::new(),
            result: None,
        }
    }
    /// Accept only an independently prepared synthetic review. No filesystem
    /// work is performed here; the ordinary display-only fixture stays inert.
    pub(crate) fn with_prepared_fixture(
        model: RequestDialog,
        review: FixturePreflightReview,
        palette: Palette,
        cx: &mut Context<Self>,
    ) -> Self {
        let mut view = Self::fixture(model.request().clone(), palette, cx);
        view.model = model;
        view.review = Some(review);
        view
    }
    fn start_fixture_worker(&mut self, cx: &mut Context<Self>) {
        let Some(review) = self.review.take() else {
            return;
        };
        let Some(ticket) = self.flight.start(&self.model) else {
            return;
        };
        let worker = match review.into_worker(&self.model, ticket.canceled().clone()) {
            Ok(worker) => worker,
            Err(_) => {
                self.flight.cancel();
                return;
            }
        };
        let job = cx.background_executor().spawn(async move { worker.run() });
        // Detached awaiter retains the background task even if the view is gone.
        // Weak update cannot resurrect the view. Drop only sets cancellation;
        // no UI-thread wait/join and no filesystem cleanup occurs here.
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.flight.accept(&ticket, &this.model) {
                    this.result = Some(result);
                    cx.notify();
                }
            });
        })
        .detach();
    }
    pub fn replace_fixture(&mut self, request: WorkspaceRequest, cx: &mut Context<Self>) {
        self.flight.cancel();
        self.review.take();
        self.result.take();
        let _ = self.model.replace(request);
        self.focus_cancel = true;
        cx.notify();
    }
    fn choose(&mut self, stamp: &EventStamp, event: Event, cx: &mut Context<Self>) {
        if self.model.apply(stamp, event) {
            self.focus_cancel = true;
            match event {
                Event::ExplicitConfirm => self.start_fixture_worker(cx),
                Event::Cancel | Event::Close => {
                    self.flight.cancel();
                    self.review.take();
                    self.result.take();
                }
                Event::Enter => {}
            }
        }
        cx.notify();
    }
}
impl Drop for WorkspaceReadRequestView {
    fn drop(&mut self) {
        self.flight.cancel();
    }
}
impl Render for WorkspaceReadRequestView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.focus_cancel {
            window.focus(&self.cancel_focus);
            self.focus_cancel = false;
        }
        let p = self.palette;
        let keyboard_stamp = self.model.stamp();
        let cancel_stamp = keyboard_stamp.clone();
        let confirm_stamp = keyboard_stamp.clone();
        let dismiss_stamp = keyboard_stamp.clone();
        let pending = self.model.decision() == Decision::Pending;
        let prepared = self.review.is_some();
        let dismissed = self.model.decision() == Decision::Denied;
        let (active_name, active_path) = self.model.request().active();
        let active_label = format!(
            "Active vault · {}",
            if active_name.is_empty() {
                "Explicit path (unregistered)"
            } else {
                active_name
            }
        );
        let active_path = active_path.to_owned();
        let rows = self
            .model
            .request()
            .rows()
            .map(|(name, path, active)| {
                div()
                    .flex()
                    .flex_col()
                    .gap_1()
                    .child(format!(
                        "{name} · {}",
                        if active {
                            "Active vault"
                        } else {
                            "Registered peer"
                        }
                    ))
                    .child(div().text_color(rgb(p.muted)).child(path.to_owned()))
            })
            .collect::<Vec<_>>();
        div().id("fixture-workspace-read-dialog").when(dismissed, |view| view.track_focus(&self.cancel_focus)).flex().flex_col().gap_3().p_4().max_w_full()
            .bg(rgb(p.panel)).text_color(rgb(p.ink)).border_1().border_color(rgb(p.line)).rounded_lg().shadow_lg()
            .on_key_down(cx.listener(move |this, event: &gpui::KeyDownEvent, window, cx| {
                if keyboard_stamp != this.model.stamp() { return; }
                match event.keystroke.key.as_str() {
                    "escape" => this.choose(&keyboard_stamp, Event::Close, cx),
                    "enter" => { this.model.apply(&keyboard_stamp, Event::Enter); },
                    "tab" => {
                        if this.model.decision() == Decision::Pending && this.cancel_focus.is_focused(window) { window.focus(&this.confirm_focus); }
                        else { window.focus(&this.cancel_focus); }
                    },
                    "space" if !event.is_held && !event.keystroke.modifiers.modified() => {
                        let action = if this.confirm_focus.is_focused(window) { Event::ExplicitConfirm } else { Event::Cancel };
                        this.choose(&keyboard_stamp, action, cx);
                    },
                    _ => return,
                }
                cx.stop_propagation();
            }))
            .child(div().text_color(rgb(p.mark)).child(NOTICE))
            .child(div().text_lg().child("Review workspace read request"))
            .child(div().child("Active vault and all registered peers in this supplied request:"))
            .child(div().id("active-vault-display").overflow_x_scroll().child(active_label).child(div().text_color(rgb(p.muted)).child(active_path)))
            .child(div().id("requested-vault-list").max_h_64().overflow_y_scroll()
                    .overflow_x_scroll().flex().flex_col().gap_3().children(rows))
            .child(CONSEQUENCES)
            .child("No ordinary access or serving is enabled. A separately supplied synthetic fixture review may run one background preflight; display-only previews run no process. Enter does not confirm. Focus the explicit control and press Space.")
            .child(format!("Fixture preflight: {}. No automatic private-runtime cleanup.", match self.flight.phase() { Phase::Idle=>"not started",Phase::Running=>"running",Phase::Cancelled=>"cancelled; old results ignored",Phase::Completed=>if self.result.as_ref().is_some_and(|result|result.succeeded){"checked; still no access grant"}else{"failed; new review required"} }))
            .child(format!("Preview state: {}", match self.model.decision() { Decision::Pending => "not confirmed", Decision::PreviewConfirmed => "choice recorded; access still blocked", Decision::Denied => "dismissed; access blocked" }))
            .when(!dismissed, |view| view.child(div().flex().gap_3()
                .child(div().id("cancel-workspace-read").track_focus(&self.cancel_focus).tab_index(0).px_3().py_2().border_1().border_color(rgb(if self.cancel_focus.is_focused(window) { p.mark } else { p.panel })).rounded_md().child(if pending { "Cancel" } else { "Dismiss preview choice" })
                    .on_click(cx.listener(move |this, _, _, cx| this.choose(&cancel_stamp, Event::Cancel, cx))))
                .when(pending, |row| row.child(div().id("confirm-workspace-read-preview").track_focus(&self.confirm_focus).tab_index(0).px_3().py_2().border_1().border_color(rgb(if self.confirm_focus.is_focused(window) { p.mark } else { p.panel })).rounded_md().child(if prepared { "Run fixture preflight" } else { "Confirm preview only" })
                    .on_click(cx.listener(move |this, _, _, cx| this.choose(&confirm_stamp, Event::ExplicitConfirm, cx)))))))
            .when(!dismissed, |view| view.child(div().id("close-workspace-read").text_color(rgb(p.muted)).child("Dismiss request")
                .on_click(cx.listener(move |this, _, _, cx| this.choose(&dismiss_stamp, Event::Close, cx)))))
    }
}
