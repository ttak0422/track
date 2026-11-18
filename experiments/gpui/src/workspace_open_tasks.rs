//! Explicit selected-vault Open tasks. Local paging only; no task writes.
use super::*;
use gpui::Div;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
static ACTIVE: AtomicBool = AtomicBool::new(false);
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
static SERIAL: AtomicU64 = AtomicU64::new(1);
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
pub(crate) struct OpenTasksState {
    identity: Identity,
    serial: u64,
    selection: u64,
    open: bool,
    page: usize,
    result: Option<Result<live::DatedTasksResponse, String>>,
}
impl OpenTasksState {
    fn accepts(&self, identity: &Identity, serial: u64) -> bool {
        self.open && self.identity == *identity && self.serial == serial
    }
    fn controls_current(&self, identity: &Identity, serial: u64, selection: u64) -> bool {
        self.accepts(identity, serial) && self.selection == selection
    }
}
fn response_matches(identity: &Identity, response: &live::DatedTasksResponse) -> bool {
    let canonical = if response.vault.is_empty() {
        identity.canonical.as_str()
    } else {
        response.vault.as_str()
    };
    response.launch_generation == identity.launch
        && response.source_vault == identity.vault
        && canonical == identity.canonical
}
fn page_range(total: usize, page: usize) -> std::ops::Range<usize> {
    let page = page.min(total.div_ceil(100).saturating_sub(1));
    let start = page * 100;
    start..(start + 100).min(total)
}
fn snapshot_label(total: usize, page: usize, incomplete: bool) -> String {
    if total == 0 {
        if incomplete {
            "No available rows; total open tasks is unknown.".into()
        } else {
            "No open dated tasks.".into()
        }
    } else {
        let range = page_range(total, page);
        format!(
            "{}–{} of {} {} rows",
            range.start + 1,
            range.end,
            total,
            if incomplete {
                "available (incomplete)"
            } else {
                "accepted"
            }
        )
    }
}
fn short_label(value: &str, limit: usize) -> String {
    let mut chars = value.chars();
    let mut label: String = chars.by_ref().take(limit).collect();
    if chars.next().is_some() {
        label.push('…');
    }
    label
}
impl Workspace {
    fn open_tasks_identity(&self) -> Option<Identity> {
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
            epoch: self.accepted_storage_generation?,
            request: self.request,
            scope_revision: self.shell.scope_revision,
            vault,
            canonical,
            launch_name,
            registry,
        })
    }
    fn open_tasks_available(&self, cx: &App) -> bool {
        !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.shell.find.is_composing(cx)
            && !self.reader_selection_dragging()
    }
    fn open_tasks_fetch(&mut self, identity: Identity, cx: &mut Context<Self>) {
        if self.open_tasks_identity().as_ref() != Some(&identity) || !self.open_tasks_available(cx)
        {
            return;
        }
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        self.shell.open_tasks = Some(OpenTasksState {
            identity: identity.clone(),
            serial,
            selection: 0,
            open: true,
            page: 0,
            result: None,
        });
        let Some(single) = Permit::acquire() else {
            self.shell.open_tasks.as_mut().unwrap().result = Some(Err(
                "An earlier open-tasks request is still finishing. Retry shortly.".into(),
            ));
            cx.notify();
            return;
        };
        let Some(permit) = workspace_activity::AgendaPermit::acquire() else {
            self.shell.open_tasks.as_mut().unwrap().result = Some(Err(
                "Earlier requests are still finishing. Retry shortly.".into(),
            ));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let _single = single;
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.epoch {
                return Err("Storage changed; reload the workspace.".into());
            }
            let response = client
                .open_tasks(&captured.vault, &captured.launch_name)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready"
                || after.generation != captured.epoch
                || !response_matches(&captured, &response)
            {
                return Err("Open tasks response became stale or changed vault.".into());
            }
            Ok(response)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.open_tasks_identity().as_ref() != Some(&identity) {
                    return;
                }
                if let Some(state) = this
                    .shell
                    .open_tasks
                    .as_mut()
                    .filter(|s| s.accepts(&identity, serial))
                {
                    state.result = Some(result);
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }
    fn open_tasks_page(
        &mut self,
        identity: &Identity,
        serial: u64,
        selection: u64,
        page: usize,
        cx: &mut Context<Self>,
    ) {
        if self.open_tasks_identity().as_ref() != Some(identity) || !self.open_tasks_available(cx) {
            return;
        }
        let Some(state) = self
            .shell
            .open_tasks
            .as_mut()
            .filter(|s| s.controls_current(identity, serial, selection))
        else {
            return;
        };
        let Some(Ok(response)) = &state.result else {
            return;
        };
        if page >= response.tasks.len().div_ceil(100) {
            return;
        }
        state.page = page;
        state.selection = state.selection.wrapping_add(1);
        cx.notify();
    }
    fn open_tasks_open_note(
        &mut self,
        identity: &Identity,
        serial: u64,
        selection: u64,
        index: usize,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || self.open_tasks_identity().as_ref() != Some(identity)
            || !self.open_tasks_available(cx)
        {
            return;
        }
        let Some(state) = self
            .shell
            .open_tasks
            .as_ref()
            .filter(|s| s.controls_current(identity, serial, selection))
        else {
            return;
        };
        let Some(Ok(response)) = &state.result else {
            return;
        };
        if !page_range(response.tasks.len(), state.page).contains(&index) {
            return;
        }
        let Some(task) = response.tasks.get(index) else {
            return;
        };
        let key = live::NoteKey {
            vault: identity.canonical.clone(),
            note_id: task.note_id,
        };
        self.open_live_key(key, cx);
    }
    pub(crate) fn home_open_tasks(&mut self, dark: bool, cx: &mut Context<Self>) -> Div {
        let mut view = div().w(px(520.)).max_w_full().mb_4().text_size(px(12.));
        let Some(identity) = self.open_tasks_identity() else {
            self.shell.open_tasks = None;
            return view.child("Open tasks unavailable while the working vault is not ready.");
        };
        if self
            .shell
            .open_tasks
            .as_ref()
            .is_some_and(|s| s.identity != identity)
        {
            self.shell.open_tasks = None;
        }
        let captured = identity.clone();
        let expected_serial = self.shell.open_tasks.as_ref().map(|s| s.serial);
        let open = self.shell.open_tasks.as_ref().is_some_and(|s| s.open);
        view = view.child(
            div()
                .id("open_tasks-toggle")
                .cursor_pointer()
                .child(if open {
                    "▾ Open tasks"
                } else {
                    "▸ Open tasks"
                })
                .on_click(cx.listener(move |this, _, window, cx| {
                    if !window.is_window_active()
                        || this.open_tasks_identity().as_ref() != Some(&captured)
                        || !this.open_tasks_available(cx)
                        || this.shell.open_tasks.as_ref().map(|s| s.serial) != expected_serial
                    {
                        return;
                    }
                    if let Some(state) = this
                        .shell
                        .open_tasks
                        .as_mut()
                        .filter(|s| s.open && s.identity == captured)
                    {
                        state.open = false;
                        state.serial = SERIAL.fetch_add(1, Ordering::Relaxed);
                        cx.notify();
                    } else {
                        this.open_tasks_fetch(captured.clone(), cx);
                    }
                })),
        );
        if !open {
            return view;
        }
        let state = self.shell.open_tasks.as_ref().unwrap();
        let serial = state.serial;
        let selection = state.selection;
        view = view.child(format!(
            "{} · dated nonterminal tasks · engine order",
            if identity.vault.is_empty() {
                format!("Launch vault ({})", identity.canonical)
            } else {
                format!("Working vault: {}", identity.canonical)
            }
        ));
        let mut controls = div().flex().flex_wrap().gap_3().py_2();
        let retry = identity.clone();
        controls = controls.child(
            div()
                .id("open_tasks-refresh")
                .cursor_pointer()
                .child("Refresh / Retry")
                .on_click(cx.listener(move |this, _, window, cx| {
                    if window.is_window_active()
                        && this
                            .shell
                            .open_tasks
                            .as_ref()
                            .is_some_and(|s| s.accepts(&retry, serial))
                    {
                        this.open_tasks_fetch(retry.clone(), cx);
                    }
                })),
        );
        view = view.child(controls);
        let response = match &state.result {
            None => return view.child("Loading open tasks…"),
            Some(Err(error)) => return view.child(format!("Open tasks unavailable: {error}")),
            Some(Ok(response)) => response,
        };
        let incomplete = workspace_activity::incomplete(&response.diagnostics);
        if incomplete {
            view = view.child("Incomplete or cached tasks; missing tasks are unknown.");
        }
        let range = page_range(response.tasks.len(), state.page);
        view = view.child(snapshot_label(response.tasks.len(), state.page, incomplete));
        let mut paging = div().flex().gap_3();
        for (label, target) in [
            ("Previous", state.page.checked_sub(1)),
            (
                "Next",
                (range.end < response.tasks.len()).then_some(state.page + 1),
            ),
        ] {
            if let Some(target) = target {
                let owner = identity.clone();
                paging = paging.child(
                    div()
                        .id(gpui::SharedString::from(format!("open-tasks-{label}")))
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if window.is_window_active() {
                                this.open_tasks_page(&owner, serial, selection, target, cx);
                            }
                        })),
                );
            }
        }
        view = view.child(paging);
        let mut list = div()
            .id("open_tasks-list")
            .max_h(px(380.))
            .overflow_y_scroll()
            .p_2()
            .bg(rgb(if dark { 0x191c1e } else { 0xf7f7f3 }));
        for index in range {
            let task = &response.tasks[index];
            let owner = identity.clone();
            let text = short_label(&task.text, 512);
            let title = short_label(&task.title, 256);
            let label = format!(
                "{} {} · {} · {} · scheduled {} · due {}",
                task.state,
                task.priority,
                text,
                if title.is_empty() { "Untitled" } else { &title },
                if task.scheduled.is_empty() {
                    "—"
                } else {
                    &task.scheduled
                },
                if task.due.is_empty() {
                    "—"
                } else {
                    &task.due
                }
            );
            list = list.child(
                div()
                    .id(gpui::SharedString::from(format!("open-tasks-task-{index}")))
                    .py_1()
                    .cursor_pointer()
                    .child(label)
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.open_tasks_open_note(&owner, serial, selection, index, window, cx)
                    })),
            );
        }
        view.child(list)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn owner() -> Identity {
        Identity {
            launch: 1,
            epoch: 2,
            request: 3,
            scope_revision: 4,
            vault: "work".into(),
            canonical: "work".into(),
            launch_name: "main".into(),
            registry: vec!["main".into(), "work".into()],
        }
    }
    #[test]
    fn response_and_callbacks_are_owned_by_exact_request_scope_and_page() {
        let identity = owner();
        let mut state = OpenTasksState {
            identity: identity.clone(),
            serial: 7,
            selection: 0,
            open: true,
            page: 0,
            result: None,
        };
        assert!(state.controls_current(&identity, 7, 0));
        for i in 0..7 {
            let mut stale = identity.clone();
            match i {
                0 => stale.launch += 1,
                1 => stale.epoch += 1,
                2 => stale.request += 1,
                3 => stale.scope_revision += 1,
                4 => stale.vault.clear(),
                5 => stale.registry.clear(),
                _ => stale.canonical = "main".into(),
            }
            assert!(!state.accepts(&stale, 7));
        }
        assert!(!state.accepts(&identity, 8));
        state.selection += 1;
        assert!(!state.controls_current(&identity, 7, 0));
        state.open = false;
        assert!(!state.accepts(&identity, 7));
        state.open = true;
        state.serial = 8;
        assert!(!state.accepts(&identity, 7));
        assert!(state.accepts(&identity, 8));
        state.selection += 1; // page away then back cannot revive an old callback
        assert!(!state.controls_current(&identity, 8, 0));
        let mut response = live::DatedTasksResponse {
            vault: "work".into(),
            tasks: vec![],
            source_vault: "work".into(),
            launch_generation: 1,
            diagnostics: Default::default(),
        };
        assert!(response_matches(&identity, &response));
        response.source_vault = "main".into();
        assert!(!response_matches(&identity, &response));
    }
    #[test]
    fn every_accepted_index_is_paged_once_without_sorting_or_deduplication() {
        for total in [0usize, 1, 100, 101, 4096] {
            let indices: Vec<_> = (0..total.div_ceil(100))
                .flat_map(|page| page_range(total, page))
                .collect();
            assert_eq!(indices, (0..total).collect::<Vec<_>>());
            assert!(page_range(total, usize::MAX).len() <= 100);
        }
    }
    #[test]
    fn cached_empty_is_not_complete_empty_and_totals_are_snapshot_only() {
        assert_eq!(snapshot_label(0, 0, false), "No open dated tasks.");
        assert!(snapshot_label(0, 0, true).contains("unknown"));
        assert_eq!(
            snapshot_label(101, 1, false),
            "101–101 of 101 accepted rows"
        );
        assert!(snapshot_label(101, 0, true).contains("available (incomplete)"));
    }
    #[test]
    fn one_open_task_job_owns_the_permit_until_it_finishes() {
        let first = Permit::acquire().unwrap();
        assert!(Permit::acquire().is_none());
        drop(first);
        assert!(Permit::acquire().is_some());
    }
}
