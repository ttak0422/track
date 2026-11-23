//! Home's engine-created listing, not the app's recent-visit history.
use super::*;
use gpui::Div;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
static ACTIVE: AtomicBool = AtomicBool::new(false);
static SERIAL: AtomicU64 = AtomicU64::new(1);
struct Permit;
impl Permit {
    fn acquire() -> Option<Self> {
        ACTIVE
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        ACTIVE.store(false, Ordering::Release);
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    launch: u64,
    epoch: u64,
    request: u64,
    scope_revision: u64,
    vault: String,
    canonical: String,
    launch_name: String,
    registry: Vec<String>,
}
pub(crate) struct State {
    identity: Identity,
    serial: u64,
    result: Option<Result<live::Notes, String>>,
}
impl State {
    fn accepts(&self, id: &Identity, serial: u64) -> bool {
        self.identity == *id && self.serial == serial
    }
}
fn label(title: &str, id: i64) -> String {
    if title.trim().is_empty() {
        return format!("Note {id}");
    }
    let mut chars = title.chars();
    let mut text: String = chars
        .by_ref()
        .take(256)
        .map(|c| if c.is_control() { ' ' } else { c })
        .collect();
    if chars.next().is_some() {
        text.push('…');
    }
    text
}
fn status(count: usize, partial: bool) -> String {
    match (count, partial) {
        (0, false) => "No recently created notes.".into(),
        (0, true) => "No available rows; recent notes remain unknown (partial/cached).".into(),
        (_, true) => {
            format!("{count} available recent notes (partial/cached; not a complete list).")
        }
        (_, false) => format!("{count} latest notes · engine creation order · at most 100"),
    }
}
impl Workspace {
    fn recent_created_identity(&self) -> Option<Identity> {
        if !self.shell.home
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
        {
            return None;
        }
        let session = self.live.as_ref()?;
        let storage = session.initial_storage.as_ref()?;
        if storage.phase != "ready" || Some(storage.generation) != self.accepted_storage_generation
        {
            return None;
        }
        let (launch_name, registry) = self.follow_registry()?;
        let vault = self.working_vault_scope().to_owned();
        let canonical = if vault.is_empty() {
            launch_name.clone()
        } else {
            vault.clone()
        };
        if !registry.contains(&canonical) || !self.session_ui.permits(&vault) {
            return None;
        }
        Some(Identity {
            launch: session.client().launch_generation,
            epoch: storage.generation,
            request: self.request,
            scope_revision: self.shell.scope_revision,
            vault,
            canonical,
            launch_name,
            registry,
        })
    }
    fn recent_created_allowed(&self, cx: &App) -> bool {
        !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.shell.find.is_composing(cx)
            && !self.reader_selection_dragging()
    }
    fn recent_created_fetch(&mut self, id: Identity, cx: &mut Context<Self>) {
        if self.recent_created_identity().as_ref() != Some(&id) || !self.recent_created_allowed(cx)
        {
            return;
        }
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        self.shell.recent_created = Some(State {
            identity: id.clone(),
            serial,
            result: None,
        });
        let single = Permit::acquire();
        let shared = workspace_activity::AgendaPermit::acquire();
        let (Some(single), Some(shared)) = (single, shared) else {
            self.shell.recent_created.as_mut().unwrap().result =
                Some(Err("Earlier reads are finishing. Retry shortly.".into()));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let captured = id.clone();
        let job = cx.background_executor().spawn(async move {
            let (_single, _shared) = (single, shared);
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.epoch {
                return Err("Storage changed; reload.".into());
            }
            let result = client
                .recently_created(&captured.vault, &captured.launch_name)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready"
                || after.generation != captured.epoch
                || client.launch_generation != captured.launch
            {
                return Err("Recent notes became stale; reload.".into());
            }
            Ok(result)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.retire_invalid_read(cx) {
                    cx.notify();
                    return;
                }
                if this.recent_created_identity().as_ref() != Some(&id) {
                    return;
                }
                if let Some(state) = this
                    .shell
                    .recent_created
                    .as_mut()
                    .filter(|s| s.accepts(&id, serial))
                {
                    state.result = Some(result);
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn home_recent_created(&mut self, cx: &mut Context<Self>) -> Div {
        let mut view = div().w(px(520.)).max_w_full().mb_4().text_size(px(12.));
        let Some(id) = self.recent_created_identity() else {
            self.shell.recent_created = None;
            return view.child("Recently created notes require a ready working vault.");
        };
        if self
            .shell
            .recent_created
            .as_ref()
            .is_some_and(|s| s.identity != id)
        {
            self.shell.recent_created = None;
        }
        let serial = self.shell.recent_created.as_ref().map(|s| s.serial);
        let captured = id.clone();
        view = view.child(
            div()
                .id("recent-created-toggle")
                .cursor_pointer()
                .child(if serial.is_some() {
                    "▾ Recently created notes"
                } else {
                    "▸ Recently created notes"
                })
                .on_click(cx.listener(move |this, _, window, cx| {
                    if !window.is_window_active()
                        || this.recent_created_identity().as_ref() != Some(&captured)
                        || !this.recent_created_allowed(cx)
                        || this.shell.recent_created.as_ref().map(|s| s.serial) != serial
                    {
                        return;
                    }
                    if serial.is_some() {
                        this.shell.recent_created = None;
                        cx.notify();
                    } else {
                        this.recent_created_fetch(captured.clone(), cx);
                    }
                })),
        );
        let Some(state) = &self.shell.recent_created else {
            return view;
        };
        let serial = state.serial;
        let captured = id.clone();
        view = view
            .child(format!(
                "Working vault: {} · distinct from Recent visits",
                id.canonical
            ))
            .child(
                div()
                    .id("recent-created-retry")
                    .cursor_pointer()
                    .child("Reload / Retry")
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if window.is_window_active()
                            && this
                                .shell
                                .recent_created
                                .as_ref()
                                .is_some_and(|s| s.accepts(&captured, serial))
                        {
                            this.recent_created_fetch(captured.clone(), cx);
                        }
                    })),
            );
        let result = match &state.result {
            None => return view.child("Loading recently created notes…"),
            Some(Err(e)) => return view.child(format!("Recent notes unavailable: {e}")),
            Some(Ok(r)) => r,
        };
        view = view.child(status(
            result.notes.len(),
            workspace_activity::incomplete(&result.diagnostics),
        ));
        let mut rows = div()
            .id("recent-created-list")
            .max_h(px(320.))
            .overflow_y_scroll();
        for (index, note) in result.notes.iter().enumerate() {
            let captured = id.clone();
            let key = note.key();
            rows = rows.child(
                div()
                    .id(("recent-created-row", index))
                    .py_1()
                    .cursor_pointer()
                    .child(label(&note.title, note.note_id))
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if !window.is_window_active()
                            || this.recent_created_identity().as_ref() != Some(&captured)
                            || !this.recent_created_allowed(cx)
                        {
                            return;
                        }
                        let Some(s) = this
                            .shell
                            .recent_created
                            .as_ref()
                            .filter(|s| s.accepts(&captured, serial))
                        else {
                            return;
                        };
                        if !s
                            .result
                            .as_ref()
                            .and_then(|r| r.as_ref().ok())
                            .and_then(|r| r.notes.get(index))
                            .is_some_and(|n| n.key() == key)
                        {
                            return;
                        }
                        this.open_live_key(key.clone(), cx);
                    })),
            );
        }
        view.child(rows)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn titles_are_bounded_and_empty_titles_use_only_id() {
        assert_eq!(label(" \n", 42), "Note 42");
        assert_eq!(label("日本語", 1), "日本語");
        assert_eq!(label(&"🦀".repeat(300), 1).chars().count(), 257);
    }
    #[test]
    fn partial_empty_never_claims_complete_absence() {
        assert!(status(0, true).contains("unknown"));
        assert_eq!(status(0, false), "No recently created notes.");
        assert!(status(100, false).contains("at most 100"));
    }
    #[test]
    fn close_reload_scope_and_epoch_revoke_old_response() {
        let id = Identity {
            launch: 1,
            epoch: 2,
            request: 3,
            scope_revision: 4,
            vault: "foreign".into(),
            canonical: "foreign".into(),
            launch_name: "host".into(),
            registry: vec!["host".into(), "foreign".into()],
        };
        let state = State {
            identity: id.clone(),
            serial: 5,
            result: None,
        };
        assert!(state.accepts(&id, 5));
        assert!(!state.accepts(&id, 6));
        for change in 0..6 {
            let mut next = id.clone();
            match change {
                0 => next.epoch += 1,
                1 => next.launch += 1,
                2 => next.request += 1,
                3 => next.scope_revision += 1,
                4 => next.vault = "host".into(),
                _ => next.registry.clear(),
            };
            assert!(!state.accepts(&next, 5));
        }
    }
}
