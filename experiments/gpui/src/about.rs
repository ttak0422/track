//! Informational, executable-only identity. This view has no workspace access.
use gpui::{
    AnyWindowHandle, App, Bounds, Context, Window, WindowBounds, WindowHandle, WindowOptions, div,
    prelude::*, px, rgb, size,
};

struct About;

fn about_handle(handle: AnyWindowHandle) -> Option<WindowHandle<About>> {
    handle.downcast::<About>()
}

pub fn show(cx: &mut App) {
    // The window list contains only live windows, so dismissal needs no retained
    // handle or workspace callback. Repeated menu invocations focus the same view.
    if let Some(handle) = cx.windows().into_iter().find_map(about_handle) {
        let _ = handle.update(cx, |_, window, _| window.activate_window());
        return;
    }
    let bounds = Bounds::centered(None, size(px(600.), px(260.)), cx);
    if let Err(error) = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(gpui::TitlebarOptions {
                title: Some("About Track Native".into()),
                ..Default::default()
            }),
            is_resizable: false,
            is_minimizable: false,
            ..Default::default()
        },
        |window, cx| {
            window.on_window_should_close(cx, |_, _| true);
            cx.new(|_| About)
        },
    ) {
        eprintln!("Could not open About Track Native: {error}");
    }
}

/// Consume Close Window for About even if it is already being removed. Never
/// fall through to the workspace's guarded quit on an About dismissal.
pub fn dismiss_active(cx: &mut App) -> bool {
    let Some(handle) = cx.active_window().and_then(about_handle) else {
        return false;
    };
    let _ = handle.update(cx, |_, window, _| window.remove_window());
    true
}

impl Render for About {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        div()
            .size_full()
            .p_6()
            .flex()
            .flex_col()
            .gap_5()
            .bg(rgb(0x20242b))
            .text_color(rgb(0xf1f3f5))
            // All four metadata lines, including the complete 40-character
            // revision, fit at this font size. No truncation/overflow clipping.
            .child(div().text_size(px(14.)).child(crate::build_info::display()))
            .child(
                div()
                    .id("dismiss-about")
                    .px_4()
                    .py_2()
                    .rounded_md()
                    .bg(rgb(0x394455))
                    .cursor_pointer()
                    .child("Close")
                    .on_click(|_, window, _| window.remove_window()),
            )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn close_routing_recognizes_only_about_windows() {
        let about = WindowHandle::<About>::new(1_u64.into());
        let workspace = WindowHandle::<crate::Workspace>::new(2_u64.into());
        assert!(about_handle(about.into()).is_some());
        assert!(about_handle(workspace.into()).is_none());
        assert!(None::<AnyWindowHandle>.and_then(about_handle).is_none());
    }
}
