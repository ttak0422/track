//! Test-only visual fixture; no shell registration or launch side effects.
use crate::workspace_read_request::{
    CONSEQUENCES, Decision, Event, NOTICE, RequestDialog, WorkspaceRequest,
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
}
impl WorkspaceReadRequestView {
    pub fn fixture(request: WorkspaceRequest, palette: Palette, cx: &mut Context<Self>) -> Self {
        Self {
            model: RequestDialog::new(request),
            palette,
            cancel_focus: cx.focus_handle(),
            confirm_focus: cx.focus_handle(),
            focus_cancel: true,
        }
    }
    pub fn replace_fixture(&mut self, request: WorkspaceRequest, cx: &mut Context<Self>) {
        let _ = self.model.replace(request);
        self.focus_cancel = true;
        cx.notify();
    }
    fn choose(&mut self, generation: u64, event: Event, cx: &mut Context<Self>) {
        if self.model.apply(generation, event) {
            self.focus_cancel = true;
        }
        cx.notify();
    }
}
impl Render for WorkspaceReadRequestView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if self.focus_cancel {
            window.focus(&self.cancel_focus);
            self.focus_cancel = false;
        }
        let p = self.palette;
        let generation = self.model.generation();
        let pending = self.model.decision() == Decision::Pending;
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
                if generation != this.model.generation() { return; }
                match event.keystroke.key.as_str() {
                    "escape" => this.choose(generation, Event::Close, cx),
                    "enter" => { this.model.apply(generation, Event::Enter); },
                    "tab" => {
                        if this.model.decision() == Decision::Pending && this.cancel_focus.is_focused(window) { window.focus(&this.confirm_focus); }
                        else { window.focus(&this.cancel_focus); }
                    },
                    "space" if !event.is_held && !event.keystroke.modifiers.modified() => {
                        let action = if this.confirm_focus.is_focused(window) { Event::ExplicitConfirm } else { Event::Cancel };
                        this.choose(generation, action, cx);
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
            .child("This preview does not create a cache, start a server, or grant access. Enter does not confirm. Use the explicit control or focus it and press Space.")
            .child(format!("Preview state: {}", match self.model.decision() { Decision::Pending => "not confirmed", Decision::PreviewConfirmed => "choice recorded; access still blocked", Decision::Denied => "dismissed; access blocked" }))
            .when(!dismissed, |view| view.child(div().flex().gap_3()
                .child(div().id("cancel-workspace-read").track_focus(&self.cancel_focus).tab_index(0).px_3().py_2().border_1().border_color(rgb(if self.cancel_focus.is_focused(window) { p.mark } else { p.panel })).rounded_md().child(if pending { "Cancel" } else { "Dismiss preview choice" })
                    .on_click(cx.listener(move |this, _, _, cx| this.choose(generation, Event::Cancel, cx))))
                .when(pending, |row| row.child(div().id("confirm-workspace-read-preview").track_focus(&self.confirm_focus).tab_index(0).px_3().py_2().border_1().border_color(rgb(if self.confirm_focus.is_focused(window) { p.mark } else { p.panel })).rounded_md().child("Confirm preview only")
                    .on_click(cx.listener(move |this, _, _, cx| this.choose(generation, Event::ExplicitConfirm, cx)))))))
            .when(!dismissed, |view| view.child(div().id("close-workspace-read").text_color(rgb(p.muted)).child("Dismiss request")
                .on_click(cx.listener(move |this, _, _, cx| this.choose(generation, Event::Close, cx)))))
    }
}
