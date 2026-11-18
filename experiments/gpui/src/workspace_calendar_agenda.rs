//! Explicit period agenda. One serial batch; no note/event/task mutations.
use super::*;
use calendar_agenda::{Batch, Filter, Period, View};
use gpui::{Div, SharedString};
use std::sync::{
    Arc,
    atomic::{AtomicBool, AtomicU64, Ordering},
};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
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
    calendar_revision: u64,
    vault: String,
    canonical: String,
    launch_name: String,
    registry: Vec<String>,
    zone: String,
    zone_revision: u64,
}
pub(crate) struct AgendaState {
    identity: Identity,
    serial: u64,
    selection: u64,
    view: View,
    requested: Option<Period>,
    cancel: Arc<AtomicBool>,
    result: Option<Result<Batch, String>>,
    page: usize,
    focused: bool,
    wake: Option<gpui::Task<()>>,
}
impl Drop for AgendaState {
    fn drop(&mut self) {
        self.cancel.store(true, Ordering::Release);
    }
}
impl AgendaState {
    fn current(&self, identity: &Identity, serial: u64, selection: u64, view: View) -> bool {
        self.fetch_current(identity, serial, view) && self.selection == selection
    }
    fn fetch_current(&self, identity: &Identity, serial: u64, view: View) -> bool {
        self.identity == *identity
            && self.serial == serial
            && self.view == view
            && !self.cancel.load(Ordering::Acquire)
    }
}
fn retry_period(state: &AgendaState) -> Option<Period> {
    state.requested.clone()
}
fn bounded_label(text: &str, limit: usize) -> String {
    let mut chars = text.chars();
    let mut label: String = chars.by_ref().take(limit).collect();
    if chars.next().is_some() {
        label.push('…');
    }
    label
}
fn now() -> Option<i64> {
    i64::try_from(SystemTime::now().duration_since(UNIX_EPOCH).ok()?.as_secs()).ok()
}
fn current_clock(batch: &Batch, now: i64) -> bool {
    // Matched today's lower bound also needed for clock rollback, not just midnight.
    batch.today_start <= now && now < batch.today_end
}
#[derive(Clone)]
enum Row {
    Heading(String),
    Notice(String),
    Event(usize, usize),
    Task(usize),
}
fn rows(batch: &Batch, filter: Filter) -> Vec<Row> {
    let mut rows = vec![];
    let tasks = batch.tasks.as_ref().ok();
    let projection =
        tasks.and_then(|t| calendar_agenda::task_rows(&t.tasks, &batch.period, &batch.today).ok());
    if filter != Filter::Events {
        if let Some((_, _, invalid)) = &projection {
            if *invalid > 0 {
                rows.push(Row::Notice(format!("{invalid} tasks contain invalid authored dates; those dates cannot be placed. Valid dates are retained.")));
            }
        }
        match &batch.tasks {
            Err(error) => rows.push(Row::Notice(format!("Tasks unavailable: {error}"))),
            Ok(t) if workspace_activity::incomplete(&t.diagnostics) => rows.push(Row::Notice(
                "Tasks are partial/cached; absence and totals are not complete.".into(),
            )),
            _ => {}
        }
        if let Some((_, overdue, _)) = &projection {
            if !overdue.is_empty() {
                rows.push(Row::Heading("Overdue (before display-zone today)".into()));
                for i in overdue {
                    rows.push(Row::Task(*i));
                }
            }
        }
    }
    for (day, date) in batch.period.days.iter().enumerate() {
        rows.push(Row::Heading(format!(
            "{date}{}",
            if date == &batch.today {
                " · Today"
            } else {
                ""
            }
        )));
        if filter != Filter::Tasks {
            match &batch.events[day] {
                Err(e) => rows.push(Row::Notice(format!("Events unknown: {e}"))),
                Ok(response) => {
                    let partial = workspace_activity::incomplete(&response.diagnostics);
                    if partial {
                        rows.push(Row::Notice(
                            "Events are partial/cached; missing rows remain unknown.".into(),
                        ));
                    }
                    if response.events.is_empty() {
                        rows.push(Row::Notice(
                            if partial {
                                "No available events (incomplete)."
                            } else {
                                "No events."
                            }
                            .into(),
                        ));
                    }
                    for (index, event) in response.events.iter().enumerate() {
                        if !event.deleted {
                            rows.push(Row::Event(day, index));
                        }
                    }
                }
            }
        }
        if filter != Filter::Events {
            if let Some((days, _, invalid)) = &projection {
                for i in &days[day] {
                    rows.push(Row::Task(*i));
                }
                if days[day].is_empty() {
                    rows.push(Row::Notice(
                        if *invalid > 0
                            || tasks.is_some_and(|t| workspace_activity::incomplete(&t.diagnostics))
                        {
                            "No available tasks (incomplete)."
                        } else {
                            "No open dated tasks."
                        }
                        .into(),
                    ));
                }
            } else if tasks.is_some() {
                rows.push(Row::Notice("Task date projection unavailable.".into()));
            }
        }
    }
    rows
}
fn page_range(total: usize, page: usize) -> std::ops::Range<usize> {
    let start = page.min(total.div_ceil(100).saturating_sub(1)) * 100;
    start..(start + 100).min(total)
}
impl Workspace {
    fn calendar_period_identity(&self) -> Option<Identity> {
        if !self.calendar_active()
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
        let (zone, zone_revision) = self.shell.calendar_zone.agenda_stamp();
        Some(Identity {
            launch: session.client().launch_generation,
            epoch: storage.generation,
            request: self.request,
            scope_revision: self.shell.scope_revision,
            calendar_revision: self.shell.calendar.revision,
            vault,
            canonical,
            launch_name,
            registry,
            zone,
            zone_revision,
        })
    }
    fn calendar_period_available(&self, cx: &App) -> bool {
        !self.tag_routing_blocked(cx)
            && !self.editor_shortcut_blocked(cx)
            && self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.shell.find.is_composing(cx)
            && !self.shell.calendar_zone.is_composing(cx)
            && !self.reader_selection_dragging()
    }
    fn calendar_period_current(&self, id: &Identity, serial: u64, selection: u64) -> bool {
        self.calendar_period_identity().as_ref() == Some(id)
            && self.shell.calendar.agenda.as_ref().is_some_and(|s| {
                s.current(id, serial, selection, self.shell.calendar.view)
                    && s.result
                        .as_ref()
                        .and_then(|r| r.as_ref().ok())
                        .is_none_or(|b| now().is_some_and(|n| current_clock(b, n)))
            })
    }
    fn fetch_calendar_period(
        &mut self,
        view: View,
        requested: Option<Period>,
        cx: &mut Context<Self>,
    ) {
        let Some(identity) = self.calendar_period_identity() else {
            return;
        };
        if !self.calendar_period_available(cx) {
            return;
        }
        self.shell.calendar.view = view;
        self.shell.calendar.agenda = None;
        if view == View::Month {
            cx.notify();
            return;
        }
        self.shell.activity = None; // retire month/day ownership; never reuse its responses.
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        let cancel = Arc::new(AtomicBool::new(false));
        self.shell.calendar.agenda = Some(AgendaState {
            identity: identity.clone(),
            serial,
            selection: 0,
            view,
            requested: requested.clone(),
            cancel: cancel.clone(),
            result: None,
            page: 0,
            focused: true,
            wake: None,
        });
        let permit = Permit::acquire();
        let shared = workspace_activity::AgendaPermit::acquire();
        let (Some(permit), Some(shared)) = (permit, shared) else {
            self.shell.calendar.agenda.as_mut().unwrap().result = Some(Err(
                "Earlier requests are finishing. Use Reload / Retry.".into(),
            ));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let (_permit, _shared) = (permit, shared);
            let clock = now().ok_or("Clock unavailable")?;
            let start = Instant::now();
            let check = || -> Result<(), String> {
                if cancel.load(Ordering::Acquire) || start.elapsed() > Duration::from_secs(40) {
                    return Err("Cancelled or batch deadline exceeded".into());
                }
                let s = client.storage().map_err(|e| e.to_string())?;
                if s.phase != "ready" || s.generation != captured.epoch {
                    return Err("Storage changed; Reload".into());
                }
                Ok(())
            };
            check()?;
            let result = calendar_agenda::run(
                clock,
                view,
                requested,
                &cancel,
                |date| {
                    check()?;
                    let response = client
                        .calendar_agenda_day(
                            &captured.vault,
                            &captured.launch_name,
                            date,
                            &captured.zone,
                        )
                        .map_err(|e| e.to_string())?;
                    check()?;
                    if response.launch_generation != captured.launch
                        || response.source_vault != captured.vault
                    {
                        return Err("Event source changed".into());
                    }
                    Ok(response)
                },
                || {
                    check()?;
                    let response = client
                        .open_tasks(&captured.vault, &captured.launch_name)
                        .map_err(|e| e.to_string())?;
                    check()?;
                    if response.launch_generation != captured.launch
                        || response.source_vault != captured.vault
                    {
                        return Err("Task source changed".into());
                    }
                    Ok(response)
                },
            )?;
            check()?;
            if !current_clock(&result, now().ok_or("Clock unavailable")?) {
                return Err("Display-zone date changed during loading; Retry".into());
            }
            Ok::<_, String>(result)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                // Local filtering while loading does not revoke the fetch itself.
                // Row/page authority still uses the separate selection revision.
                if this.calendar_period_identity().as_ref() == Some(&identity)
                    && this.shell.calendar.agenda.as_ref().is_some_and(|s| {
                        s.fetch_current(&identity, serial, this.shell.calendar.view)
                    })
                {
                    self::accept_result(this, result, cx);
                }
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn sync_calendar_period(&mut self, window: &Window, cx: &mut Context<Self>) {
        let identity = self.calendar_period_identity();
        if self
            .shell
            .calendar
            .agenda
            .as_ref()
            .is_some_and(|s| Some(&s.identity) != identity.as_ref())
        {
            self.shell.calendar.agenda = None;
            cx.notify();
            return;
        }
        let Some(s) = self.shell.calendar.agenda.as_mut() else {
            return;
        };
        let focused = window.is_window_active();
        let regained = focused && !s.focused;
        s.focused = focused;
        let expired = s
            .result
            .as_ref()
            .and_then(|r| r.as_ref().ok())
            .is_some_and(|b| now().is_none_or(|n| !current_clock(b, n)));
        if expired && focused {
            let view = s.view;
            let requested = s.requested.clone();
            if self.calendar_period_available(cx) {
                self.fetch_calendar_period(view, requested, cx);
            }
        } else if regained {
            cx.notify();
        }
    }
    pub(crate) fn calendar_period_controls(&mut self, dark: bool, cx: &mut Context<Self>) -> Div {
        self.sync_calendar_zone(dark, cx);
        let mut bar = div().flex().flex_col().gap_2().mb_3();
        let Some(id) = self.calendar_period_identity() else {
            return bar.child("Calendar source unavailable.");
        };
        let mut modes = div().flex().flex_wrap().gap_3();
        for (label, view) in [
            ("Month", View::Month),
            ("Today", View::Today),
            ("Tomorrow", View::Tomorrow),
            ("This week", View::Week),
        ] {
            let captured = id.clone();
            let old = self.shell.calendar.view;
            let serial = self.shell.calendar.agenda.as_ref().map(|s| s.serial);
            modes = modes.child(
                div()
                    .id(SharedString::from(format!("period-{label}")))
                    .cursor_pointer()
                    .child(label)
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if window.is_window_active()
                            && this.calendar_period_identity().as_ref() == Some(&captured)
                            && this.shell.calendar.view == old
                            && this.shell.calendar.agenda.as_ref().map(|s| s.serial) == serial
                        {
                            this.fetch_calendar_period(view, None, cx);
                        }
                    })),
            );
        }
        bar = bar.child(modes);
        if self.shell.calendar.view == View::Month {
            return bar;
        }
        let view = self.shell.calendar.view;
        let serial = self.shell.calendar.agenda.as_ref().map(|s| s.serial);
        let requested = self
            .shell
            .calendar
            .agenda
            .as_ref()
            .and_then(|s| s.result.as_ref())
            .and_then(|r| r.as_ref().ok())
            .map(|b| b.period.clone());
        let captured = id.clone();
        let retry = self.shell.calendar.agenda.as_ref().and_then(retry_period);
        bar = bar.child(
            div()
                .flex()
                .flex_wrap()
                .gap_3()
                .children(self.shell.calendar_zone.agenda_input())
                .child(
                    div()
                        .id("agenda-retry")
                        .cursor_pointer()
                        .child("Apply zone / Reload / Retry")
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if window.is_window_active()
                                && this.calendar_period_identity().as_ref() == Some(&captured)
                                && this.shell.calendar.agenda.as_ref().map(|s| s.serial) == serial
                            {
                                this.fetch_calendar_period(view, retry.clone(), cx);
                            }
                        })),
                ),
        );
        let mut navigation = div().flex().flex_wrap().gap_3();
        for (label, forward) in [("Previous period", false), ("Next period", true)] {
            if let Some(period) = requested.as_ref().and_then(|p| p.shifted(forward)) {
                let captured = id.clone();
                navigation = navigation.child(
                    div()
                        .id(SharedString::from(format!("agenda-{forward}")))
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if window.is_window_active()
                                && this.calendar_period_identity().as_ref() == Some(&captured)
                                && this.shell.calendar.agenda.as_ref().map(|s| s.serial) == serial
                            {
                                this.fetch_calendar_period(view, Some(period.clone()), cx);
                            }
                        })),
                );
            }
        }
        for (label, filter) in [
            ("All", Filter::All),
            ("Events", Filter::Events),
            ("Tasks", Filter::Tasks),
        ] {
            let captured = id.clone();
            navigation = navigation.child(
                div()
                    .id(SharedString::from(format!("agenda-filter-{label}")))
                    .cursor_pointer()
                    .child(label)
                    .on_click(cx.listener(move |this, _, window, cx| {
                        if window.is_window_active()
                            && this.calendar_period_identity().as_ref() == Some(&captured)
                            && this.calendar_period_available(cx)
                            && this.shell.calendar.agenda.as_ref().map(|s| s.serial) == serial
                        {
                            this.shell.calendar.filter = filter;
                            if let Some(s) = this.shell.calendar.agenda.as_mut() {
                                s.page = 0;
                                s.selection = s.selection.wrapping_add(1);
                            }
                            cx.notify();
                        }
                    })),
            );
        }
        bar.child(navigation).child(format!(
            "Display zone: {} · Sunday-first week · filter {:?}",
            id.zone, self.shell.calendar.filter
        ))
    }
    pub(crate) fn calendar_period_body(&mut self, cx: &mut Context<Self>) -> Div {
        let mut body = div().flex().flex_col().gap_2().text_size(px(12.));
        let Some(s) = self.shell.calendar.agenda.as_ref() else {
            return body.child("Period is stale or zone changed. Apply / Reload to load it.");
        };
        let Some(result) = &s.result else {
            return body.child("Loading period (one bounded serial batch)…");
        };
        let batch = match result {
            Ok(b) => b,
            Err(e) => return body.child(format!("Period unavailable: {e}")),
        };
        if now().is_none_or(|n| !current_clock(batch, n)) {
            return body.child("Display-zone day changed; Apply / Reload when the current input or dialog is finished.");
        }
        let all = rows(batch, self.shell.calendar.filter);
        let range = page_range(all.len(), s.page);
        let (id, serial, selection) = (s.identity.clone(), s.serial, s.selection);
        body = body.child(format!(
            "{} – {} · {}–{} of {} display rows",
            batch.period.days[0],
            batch.period.days.last().unwrap(),
            range.start + 1,
            range.end,
            all.len()
        ));
        let mut paging = div().flex().gap_3();
        for (label, page) in [
            ("Previous page", s.page.checked_sub(1)),
            (
                "Next page",
                Some(s.page + 1).filter(|p| p * 100 < all.len()),
            ),
        ] {
            if let Some(page) = page {
                let captured = id.clone();
                paging = paging.child(
                    div()
                        .id(SharedString::from(format!("agenda-page-{label}")))
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if window.is_window_active()
                                && this.calendar_period_current(&captured, serial, selection)
                                && this.calendar_period_available(cx)
                            {
                                let s = this.shell.calendar.agenda.as_mut().unwrap();
                                s.page = page;
                                s.selection = s.selection.wrapping_add(1);
                                cx.notify();
                            }
                        })),
                );
            }
        }
        body = body.child(paging);
        for (offset, row) in all[range.clone()].iter().enumerate() {
            let row_number = range.start + offset;
            match row {
                Row::Heading(label) => {
                    body = body.child(
                        div()
                            .mt_3()
                            .font_weight(gpui::FontWeight::BOLD)
                            .child(label.clone()),
                    );
                }
                Row::Notice(label) => {
                    body = body.child(label.clone());
                }
                Row::Event(day, index) => {
                    let event = &batch.events[*day].as_ref().unwrap().events[*index];
                    body = body.child(div().child(workspace_activity::event_label(event)));
                }
                Row::Task(index) => {
                    let task = &batch.tasks.as_ref().unwrap().tasks[*index];
                    let index = *index;
                    let captured = id.clone();
                    body=body.child(div().child(format!("{} · {} · {} · scheduled {} · due {}",bounded_label(&task.text,512),bounded_label(&task.title,256),task.priority,task.scheduled,task.due)).child(div().id(SharedString::from(format!("agenda-task-{row_number}"))).cursor_pointer().child("Open source note").on_click(cx.listener(move |this,_,window,cx|{
                        if !window.is_window_active()||!this.calendar_period_current(&captured,serial,selection)||!this.calendar_period_available(cx){return;}
                        let s=this.shell.calendar.agenda.as_ref().unwrap();let Some(Ok(b))=&s.result else{return;};
                        let all=rows(b,this.shell.calendar.filter);
                        if !page_range(all.len(),s.page).contains(&row_number)||!matches!(all.get(row_number),Some(Row::Task(i)) if *i==index){return;}
                        let Some(task)=b.tasks.as_ref().ok().and_then(|t|t.tasks.get(index)) else{return;};
                        this.open_live_key(live::NoteKey{vault:captured.canonical.clone(),note_id:task.note_id},cx);
                    }))));
                }
            }
        }
        body
    }
}
fn accept_result(
    this: &mut Workspace,
    mut result: Result<Batch, String>,
    cx: &mut Context<Workspace>,
) {
    if result
        .as_ref()
        .is_ok_and(|b| now().is_none_or(|n| !current_clock(b, n)))
    {
        result = Err("Clock crossed the accepted display-zone day; Retry.".into());
    }
    let serial = this.shell.calendar.agenda.as_ref().unwrap().serial;
    let poll = result.is_ok();
    this.shell.calendar.agenda.as_mut().unwrap().result = Some(result);
    if poll {
        let wake = cx.spawn(async move |this, cx| {
            loop {
                // Wake at least every 30s to notice midnight / clock adjustments.
                cx.background_executor()
                    .timer(Duration::from_secs(30))
                    .await;
                let keep = this
                    .update(cx, |this, cx| {
                        let keep = this
                            .shell
                            .calendar
                            .agenda
                            .as_ref()
                            .is_some_and(|s| s.serial == serial);
                        if keep {
                            cx.notify();
                        }
                        keep
                    })
                    .unwrap_or(false);
                if !keep {
                    break;
                }
            }
        });
        this.shell.calendar.agenda.as_mut().unwrap().wake = Some(wake);
    }
    cx.notify();
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
            calendar_revision: 5,
            vault: "foreign".into(),
            canonical: "foreign".into(),
            launch_name: "host".into(),
            registry: vec!["host".into(), "foreign".into()],
            zone: "UTC".into(),
            zone_revision: 6,
        }
    }
    #[test]
    fn owner_close_scope_zone_aba_and_page_revision_reject_stale_actions() {
        let id = identity();
        let cancel = Arc::new(AtomicBool::new(false));
        let mut state = AgendaState {
            identity: id.clone(),
            serial: 10,
            selection: 7,
            view: View::Week,
            requested: None,
            cancel: cancel.clone(),
            result: None,
            page: 0,
            focused: true,
            wake: None,
        };
        assert_eq!(retry_period(&state), None); // Reload keeps relative intent, not accepted period.
        assert!(state.current(&id, 10, 7, View::Week));
        assert!(!state.current(&id, 11, 7, View::Week));
        assert!(!state.current(&id, 10, 8, View::Week));
        assert!(!state.current(&id, 10, 7, View::Today));
        for changed in 0..7 {
            let mut next = id.clone();
            match changed {
                0 => next.launch += 1,
                1 => next.epoch += 1,
                2 => next.request += 1,
                3 => next.scope_revision += 1,
                4 => next.calendar_revision += 1,
                5 => next.zone_revision += 2,
                _ => next.registry.clear(),
            };
            assert!(!state.current(&next, 10, 7, View::Week));
        }
        state.selection += 1; // Changing the local filter while loading retains fetch authority.
        assert!(state.fetch_current(&id, 10, View::Week));
        assert!(!state.current(&id, 10, 7, View::Week));
        drop(state);
        assert!(cancel.load(Ordering::Acquire));
    }
    #[test]
    fn local_paging_and_clock_bounds_are_exact() {
        assert_eq!(page_range(101, 0), 0..100);
        assert_eq!(page_range(101, 1), 100..101);
        assert_eq!(page_range(101, usize::MAX), 100..101);
        let b = Batch {
            today: "2026-11-01".into(),
            today_start: 100,
            today_end: 100 + 25 * 3600,
            period: Period {
                days: vec!["2026-11-01".into()],
            },
            events: vec![Err("unknown".into())],
            tasks: Err("unknown".into()),
        };
        assert!(current_clock(&b, 100));
        assert!(!current_clock(&b, 99));
        assert!(!current_clock(&b, b.today_end));
        let list = rows(&b, Filter::All);
        assert!(
            list.iter()
                .any(|r| matches!(r,Row::Notice(n) if n.contains("Events unknown")))
        );
        assert!(
            list.iter()
                .any(|r| matches!(r,Row::Notice(n) if n.contains("Tasks unavailable")))
        );
    }
    #[test]
    fn relative_reload_reanchors_but_explicit_period_navigation_stays_fixed() {
        let mut state = AgendaState {
            identity: identity(),
            serial: 1,
            selection: 0,
            view: View::Today,
            requested: None,
            cancel: Arc::new(AtomicBool::new(false)),
            result: None,
            page: 0,
            focused: true,
            wake: None,
        };
        let old = Period::current(View::Today, "2026-11-15").unwrap();
        assert_eq!(retry_period(&state), None);
        let reloaded = retry_period(&state)
            .or_else(|| Period::current(state.view, "2026-11-16"))
            .unwrap();
        assert_eq!(reloaded.days[0], "2026-11-16");
        state.requested = old.shifted(false);
        assert_eq!(retry_period(&state).unwrap().days[0], "2026-11-14");
        assert_eq!(
            retry_period(&state)
                .or_else(|| Period::current(state.view, "2026-11-16"))
                .unwrap()
                .days[0],
            "2026-11-14"
        );
    }
}
