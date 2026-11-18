//! Read-only, explicit Home weekly task projection. No calendar or task writes.
use super::*;
use gpui::Div;
use std::sync::atomic::{AtomicU64, Ordering};
static SERIAL: AtomicU64 = AtomicU64::new(1);
#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    launch: u64,
    epoch: u64,
    request: u64,
    scope_revision: u64,
    vault: String,
    canonical: String,
    registry: Vec<String>,
}
pub(crate) struct WeeklyState {
    identity: Identity,
    serial: u64,
    selection: u64,
    open: bool,
    today: String,
    week: weekly_tasks::Week,
    result: Option<Result<live::DatedTasksResponse, String>>,
}
impl WeeklyState {
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
fn short_label(value: &str, limit: usize) -> String {
    let mut chars = value.chars();
    let mut label: String = chars.by_ref().take(limit).collect();
    if chars.next().is_some() {
        label.push('…');
    }
    label
}
impl Workspace {
    fn weekly_identity(&self) -> Option<Identity> {
        if !self.shell.home
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
        {
            return None;
        }
        let session = self.live.as_ref()?;
        let (launch_name, registry) = self.follow_registry()?;
        let vault = self.working_vault_scope().to_owned();
        let canonical = if vault.is_empty() {
            launch_name
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
            registry,
        })
    }
    fn weekly_available(&self, cx: &App) -> bool {
        !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.shell.find.is_composing(cx)
            && !self.reader_selection_dragging()
    }
    fn weekly_fetch(&mut self, identity: Identity, reset: bool, cx: &mut Context<Self>) {
        if self.weekly_identity().as_ref() != Some(&identity) || !self.weekly_available(cx) {
            return;
        }
        let Some((today, weekday)) = workspace_activity::device_today() else {
            return;
        };
        let Some(current) = weekly_tasks::Week::containing(&today, weekday) else {
            return;
        };
        let week = if reset {
            current
        } else {
            self.shell
                .weekly
                .as_ref()
                .filter(|s| s.identity == identity)
                .map(|s| s.week.clone())
                .unwrap_or(current)
        };
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        self.shell.weekly = Some(WeeklyState {
            identity: identity.clone(),
            serial,
            selection: 0,
            open: true,
            today,
            week,
            result: None,
        });
        let Some(permit) = workspace_activity::AgendaPermit::acquire() else {
            self.shell.weekly.as_mut().unwrap().result = Some(Err(
                "Earlier requests are still finishing. Retry shortly.".into(),
            ));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.epoch {
                return Err("Storage changed; reload the workspace.".into());
            }
            let response = client
                .dated_tasks(&captured.vault)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready"
                || after.generation != captured.epoch
                || !response_matches(&captured, &response)
            {
                return Err("Weekly task response became stale or changed vault.".into());
            }
            Ok(response)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.retire_invalid_read(cx) {
                    cx.notify();
                    return;
                }
                if this.weekly_identity().as_ref() != Some(&identity) {
                    return;
                }
                if let Some(state) = this
                    .shell
                    .weekly
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
    fn weekly_step(
        &mut self,
        identity: &Identity,
        serial: u64,
        selection: u64,
        forward: Option<bool>,
        cx: &mut Context<Self>,
    ) {
        if self.weekly_identity().as_ref() != Some(identity) || !self.weekly_available(cx) {
            return;
        }
        let Some(state) = self
            .shell
            .weekly
            .as_mut()
            .filter(|s| s.controls_current(identity, serial, selection))
        else {
            return;
        };
        let next = match forward {
            Some(forward) => state.week.shifted(forward),
            None => workspace_activity::device_today().and_then(|(today, weekday)| {
                state.today = today.clone();
                weekly_tasks::Week::containing(&today, weekday)
            }),
        };
        if let Some(week) = next {
            state.week = week;
            state.selection = state.selection.wrapping_add(1);
            cx.notify();
        }
    }
    fn weekly_open_note(
        &mut self,
        identity: &Identity,
        serial: u64,
        selection: u64,
        index: usize,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active()
            || self.weekly_identity().as_ref() != Some(identity)
            || !self.weekly_available(cx)
        {
            return;
        }
        let Some(state) = self
            .shell
            .weekly
            .as_ref()
            .filter(|s| s.controls_current(identity, serial, selection))
        else {
            return;
        };
        let Some(Ok(response)) = &state.result else {
            return;
        };
        let Some(task) = response.tasks.get(index) else {
            return;
        };
        let key = live::NoteKey {
            vault: identity.canonical.clone(),
            note_id: task.note_id,
        };
        self.open_live_key(key, cx);
    }
    pub(crate) fn home_weekly_tasks(&mut self, dark: bool, cx: &mut Context<Self>) -> Div {
        let mut view = div().w(px(520.)).max_w_full().mb_4().text_size(px(12.));
        let Some(identity) = self.weekly_identity() else {
            self.shell.weekly = None;
            return view.child("Weekly tasks unavailable while the working vault is not ready.");
        };
        if self
            .shell
            .weekly
            .as_ref()
            .is_some_and(|s| s.identity != identity)
        {
            self.shell.weekly = None;
        }
        let captured = identity.clone();
        let open = self.shell.weekly.as_ref().is_some_and(|s| s.open);
        view = view.child(
            div()
                .id("weekly-toggle")
                .cursor_pointer()
                .child(if open {
                    "▾ Weekly tasks"
                } else {
                    "▸ Weekly tasks"
                })
                .on_click(cx.listener(move |this, _, window, cx| {
                    if !window.is_window_active()
                        || this.weekly_identity().as_ref() != Some(&captured)
                        || !this.weekly_available(cx)
                    {
                        return;
                    }
                    if let Some(state) = this
                        .shell
                        .weekly
                        .as_mut()
                        .filter(|s| s.open && s.identity == captured)
                    {
                        state.open = false;
                        cx.notify();
                    } else {
                        this.weekly_fetch(captured.clone(), true, cx);
                    }
                })),
        );
        if !open {
            return view;
        }
        let state = self.shell.weekly.as_ref().unwrap();
        let serial = state.serial;
        let selection = state.selection;
        view = view.child(format!(
            "{} · device-local dates · Sunday–Saturday",
            if identity.vault.is_empty() {
                if identity.canonical.is_empty() {
                    "Launch vault".to_owned()
                } else {
                    format!("Launch vault ({})", identity.canonical)
                }
            } else {
                format!("Working vault: {}", identity.canonical)
            }
        ));
        let mut controls = div().flex().gap_3().py_2();
        for (name, label, direction) in [
            ("weekly-prev", "Previous week", Some(false)),
            ("weekly-current", "This week", None),
            ("weekly-next", "Next week", Some(true)),
        ] {
            let owner = identity.clone();
            controls = controls.child(div().id(name).cursor_pointer().child(label).on_click(
                cx.listener(move |this, _, window, cx| {
                    if window.is_window_active() {
                        this.weekly_step(&owner, serial, selection, direction, cx);
                    }
                }),
            ));
        }
        let retry = identity.clone();
        controls = controls.child(
            div()
                .id("weekly-refresh")
                .cursor_pointer()
                .child("Refresh / Retry")
                .on_click(cx.listener(move |this, _, window, cx| {
                    if window.is_window_active()
                        && this
                            .shell
                            .weekly
                            .as_ref()
                            .is_some_and(|s| s.accepts(&retry, serial))
                    {
                        this.weekly_fetch(retry.clone(), false, cx);
                    }
                })),
        );
        view = view.child(controls);
        let response = match &state.result {
            None => return view.child("Loading weekly tasks…"),
            Some(Err(error)) => return view.child(format!("Weekly tasks unavailable: {error}")),
            Some(Ok(response)) => response,
        };
        let projection = match weekly_tasks::project(&response.tasks, &state.week, &state.today) {
            Ok(value) => value,
            Err(error) => return view.child(error),
        };
        if workspace_activity::incomplete(&response.diagnostics) {
            view = view.child("Incomplete or cached tasks; missing tasks are unknown.");
        }
        if projection.invalid_dates > 0 {
            view = view.child(format!(
                "{} tasks have invalid civil dates; those dates are not placed.",
                projection.invalid_dates
            ));
        }
        let mut groups = Vec::new();
        if state.week.days.contains(&state.today) {
            groups.push(("Overdue".to_owned(), projection.overdue));
        }
        for (day, indices) in state.week.days.iter().zip(projection.days) {
            groups.push((
                format!("{day}{}", if day == &state.today { " · Today" } else { "" }),
                indices,
            ));
        }
        let mut list = div()
            .id("weekly-list")
            .max_h(px(380.))
            .overflow_y_scroll()
            .p_2()
            .bg(rgb(if dark { 0x191c1e } else { 0xf7f7f3 }));
        for (group, (label, indices)) in groups.into_iter().enumerate() {
            let mut section = div().mb_3().child(label);
            if indices.is_empty() {
                section = section.child("No matching open tasks in this snapshot.");
            }
            if indices.len() > 100 {
                section = section.child(format!(
                    "Showing 100 of {} tasks in this section.",
                    indices.len()
                ));
            }
            for index in indices.into_iter().take(100) {
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
                section = section.child(
                    div()
                        .id(gpui::SharedString::from(format!(
                            "weekly-task-{group}-{index}"
                        )))
                        .py_1()
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            this.weekly_open_note(&owner, serial, selection, index, window, cx)
                        })),
                );
            }
            list = list.child(section);
        }
        view.child(list)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn identity() -> Identity {
        Identity {
            launch: 1,
            epoch: 2,
            request: 3,
            scope_revision: 4,
            vault: "work".into(),
            canonical: "work".into(),
            registry: vec!["main".into(), "work".into()],
        }
    }
    #[test]
    fn same_id_in_another_vault_cannot_supply_this_snapshot() {
        let owner = identity();
        let mut response = live::DatedTasksResponse {
            vault: "work".into(),
            tasks: vec![],
            source_vault: "work".into(),
            launch_generation: 1,
            diagnostics: Default::default(),
        };
        assert!(response_matches(&owner, &response));
        response.vault = "main".into();
        assert!(!response_matches(&owner, &response));
        response.vault = "work".into();
        response.source_vault = "".into();
        assert!(!response_matches(&owner, &response));
        response.source_vault = "work".into();
        response.launch_generation = 2;
        assert!(!response_matches(&owner, &response));
    }
    #[test]
    fn old_reads_and_controls_cannot_survive_navigation_or_scope_changes() {
        let owner = identity();
        let mut state = WeeklyState {
            identity: owner.clone(),
            serial: 5,
            selection: 0,
            open: true,
            today: "2024-03-13".into(),
            week: weekly_tasks::Week::containing("2024-03-13", 3).unwrap(),
            result: None,
        };
        assert!(state.accepts(&owner, 5));
        assert!(!state.accepts(&owner, 6));
        assert!(state.controls_current(&owner, 5, 0));
        state.selection += 1;
        assert!(!state.controls_current(&owner, 5, 0));
        assert!(state.accepts(&owner, 5));
        let mut changed = owner.clone();
        changed.request += 1;
        assert!(!state.accepts(&changed, 5));
        changed = owner.clone();
        changed.vault = "main".into();
        assert!(!state.accepts(&changed, 5));
        changed = owner.clone();
        changed.scope_revision += 1;
        assert!(!state.accepts(&changed, 5));
        changed = owner.clone();
        changed.epoch += 1;
        assert!(!state.accepts(&changed, 5));
        changed = owner;
        changed.registry.clear();
        assert!(!state.accepts(&changed, 5));
    }
}
