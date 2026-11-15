//! Read-only Home activity. The date picker uses the device-local date; selected
//! calendar events use an explicit session-only engine-projected display zone.
use super::*;
use gpui::{Div, SharedString};

static NEXT_AGENDA: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1);
static ACTIVE_AGENDA: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);
pub(crate) struct AgendaPermit;
impl AgendaPermit {
    pub(crate) fn acquire() -> Option<Self> {
        ACTIVE_AGENDA
            .fetch_update(
                std::sync::atomic::Ordering::AcqRel,
                std::sync::atomic::Ordering::Acquire,
                |n| (n < 2).then_some(n + 1),
            )
            .ok()
            .map(|_| Self)
    }
}
impl Drop for AgendaPermit {
    fn drop(&mut self) {
        ACTIVE_AGENDA.fetch_sub(1, std::sync::atomic::Ordering::AcqRel);
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    launch: u64,
    epoch: u64,
    request: u64,
    scope_revision: u64,
    vault: String,
    since: String,
    until: String,
}
impl Identity {
    fn accepts(
        &self,
        launch: Option<u64>,
        epoch: Option<u64>,
        request: u64,
        scope_revision: u64,
        vault: &str,
        available: bool,
    ) -> bool {
        available
            && launch == Some(self.launch)
            && epoch == Some(self.epoch)
            && request == self.request
            && scope_revision == self.scope_revision
            && vault == self.vault
    }
}
pub(crate) struct ActivityState {
    identity: Identity,
    result: Option<Result<live::ActivityResponse, String>>,
    hover: Option<String>,
    agenda: Option<AgendaState>,
}
pub(crate) struct CalendarZone {
    launch: Option<u64>,
    accepted: String,
    draft: String,
    serial: u64,
    input: Option<gpui::Entity<search_input::SearchInput>>,
}
impl Default for CalendarZone {
    fn default() -> Self {
        Self {
            launch: None,
            accepted: "UTC".into(),
            draft: "UTC".into(),
            serial: 0,
            input: None,
        }
    }
}
impl CalendarZone {
    pub(crate) fn is_composing(&self, cx: &App) -> bool {
        self.input
            .as_ref()
            .is_some_and(|input| input.read(cx).is_composing())
    }

    fn advance(&mut self) -> u64 {
        self.serial = NEXT_AGENDA.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        self.serial
    }
    fn edit(&mut self, text: String) {
        self.draft = text;
        self.advance();
    }
    fn accepts(&self, serial: u64, zone: &str) -> bool {
        self.serial == serial && self.draft == zone
    }
}
struct AgendaState {
    date: String,
    serial: u64,
    result: Option<Result<live::AgendaResponse, String>>,
    tasks: Option<Result<live::DatedTasksResponse, String>>,
    events: Option<Result<live::CalendarDayEventsResponse, String>>,
    events_page: usize,
    events_pending: Option<String>,
    events_error: Option<String>,
}
impl AgendaState {
    fn loading(date: String, serial: u64) -> Self {
        Self {
            date,
            serial,
            result: None,
            tasks: None,
            events: None,
            events_page: 0,
            events_pending: None,
            events_error: None,
        }
    }
    fn busy(&mut self) {
        let error = "Earlier requests are still finishing. Retry shortly.".to_owned();
        self.result = Some(Err(error.clone()));
        self.tasks = Some(Err(error.clone()));
        self.events = Some(Err(error));
    }
    fn accepts(&self, serial: u64, date: &str) -> bool {
        self.serial == serial && self.date == date
    }
    fn finish_zone(
        &mut self,
        selection: &mut CalendarZone,
        zone: String,
        result: Result<live::CalendarDayEventsResponse, String>,
    ) {
        self.events_pending = None;
        match result {
            Ok(response) => {
                selection.accepted = zone;
                self.events = Some(Ok(response));
                self.events_error = None;
                self.events_page = 0;
            }
            Err(error) => {
                self.events_error = Some(format!("Display zone {zone:?} not applied: {error}"));
            }
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Day {
    year: i32,
    month: i32,
    day: i32,
}
impl Day {
    fn text(self) -> String {
        format!("{:04}-{:02}-{:02}", self.year, self.month, self.day)
    }
    fn previous(mut self) -> Self {
        self.day -= 1;
        if self.day == 0 {
            self.month -= 1;
            if self.month == 0 {
                self.year -= 1;
                self.month = 12;
            }
            self.day = match self.month {
                4 | 6 | 9 | 11 => 30,
                2 => {
                    if self.year % 4 == 0 && (self.year % 100 != 0 || self.year % 400 == 0) {
                        29
                    } else {
                        28
                    }
                }
                _ => 31,
            };
        }
        self
    }
}
/// Civil arithmetic only: never add seconds or reinterpret a display-zone date.
pub(crate) fn adjacent_day(date: &str, forward: bool) -> Option<String> {
    if !live::calendar_date_valid(date) {
        return None;
    }
    let mut day = Day {
        year: date[..4].parse().ok()?,
        month: date[5..7].parse().ok()?,
        day: date[8..].parse().ok()?,
    };
    if forward {
        day.day += 1;
        if !live::calendar_date_valid(&day.text()) {
            day.day = 1;
            day.month += 1;
            if day.month == 13 {
                day.month = 1;
                day.year += 1;
            }
        }
    } else {
        day = day.previous();
    }
    let result = day.text();
    live::calendar_date_valid(&result).then_some(result)
}
fn adjacent_in_range(identity: &Identity, date: &str, forward: bool) -> Option<String> {
    if date < identity.since.as_str() || date > identity.until.as_str() {
        return None;
    }
    adjacent_day(date, forward).filter(|next| next >= &identity.since && next <= &identity.until)
}

fn calendar(today: Day, weekday: usize, weeks: usize) -> Vec<Day> {
    let count = (weeks.clamp(1, 40) - 1) * 7 + weekday.min(6) + 1;
    let mut dates = vec![today];
    for _ in 1..count {
        dates.push(dates.last().unwrap().previous());
    }
    dates.reverse();
    dates
}
pub(crate) fn device_today() -> Option<(String, usize)> {
    today().map(|(day, weekday)| (day.text(), weekday))
}

fn today() -> Option<(Day, usize)> {
    // localtime_r writes only the supplied stack value; no global TZ changes.
    unsafe {
        let now = libc::time(std::ptr::null_mut());
        let mut tm: libc::tm = std::mem::zeroed();
        if libc::localtime_r(&now, &mut tm).is_null() {
            return None;
        }
        Some((
            Day {
                year: tm.tm_year + 1900,
                month: tm.tm_mon + 1,
                day: tm.tm_mday,
            },
            tm.tm_wday as usize,
        ))
    }
}
fn band(count: u64) -> usize {
    match count {
        0 => 0,
        1 => 1,
        2..=3 => 2,
        4..=6 => 3,
        _ => 4,
    }
}
pub(crate) fn incomplete(fields: &std::collections::BTreeMap<String, serde_json::Value>) -> bool {
    fn check(v: &serde_json::Value) -> bool {
        v.as_object().is_some_and(|o| {
            o.iter().any(|(k, v)| match k.as_str() {
                "partial" | "cache_used" => {
                    v.as_bool() == Some(true) || v.as_array().is_some_and(|a| !a.is_empty())
                }
                "issues" | "unavailable" => v.as_array().is_some_and(|a| !a.is_empty()),
                "omitted" => v.as_u64().is_some_and(|n| n > 0),
                "storage_state" => v.as_str().is_some_and(|s| s != "ready"),
                "read_state" => check(v),
                _ => false,
            })
        })
    }
    check(&serde_json::to_value(fields).unwrap_or_default())
}
impl Workspace {
    pub(crate) fn home_activity(&mut self, width: f32, dark: bool, cx: &mut Context<Self>) -> Div {
        let muted = if dark { 0x969a96 } else { 0x77776f };
        let mut view = div()
            .w(px(width))
            .max_w_full()
            .mb_8()
            .text_size(px(11.))
            .text_color(rgb(muted));
        let Some((today, weekday)) = today() else {
            return view.child("Activity unavailable: local date could not be read.");
        };
        let dates = calendar(
            today,
            weekday,
            ((width - 36.) / 12.).floor().max(1.) as usize,
        );
        self.sync_calendar_zone(dark, cx);
        let Some(session) = &self.live else {
            return view.child(if self.live_requested {
                "Activity is waiting for the owned server."
            } else {
                "Activity requires a live workspace."
            });
        };
        let Some(epoch) = self.accepted_storage_generation else {
            return view.child("Activity unavailable while storage is not ready.");
        };
        if self.quit_session.is_closing() || self.quit_session.failure().is_some() {
            return view.child("Activity unavailable: owned server stopped.");
        }
        let identity = Identity {
            launch: session.client().launch_generation,
            epoch,
            request: self.request,
            scope_revision: self.shell.scope_revision,
            vault: self.working_vault_scope().to_owned(),
            since: dates[0].text(),
            until: today.text(),
        };
        if self
            .shell
            .activity
            .as_ref()
            .is_none_or(|state| state.identity != identity)
        {
            let client = session.client();
            self.shell.activity = Some(ActivityState {
                identity: identity.clone(),
                result: None,
                hover: None,
                agenda: None,
            });
            let task_identity = identity.clone();
            let job = cx.background_executor().spawn(async move {
                let before = client.storage().map_err(|e| e.to_string())?;
                if before.phase != "ready" || before.generation != task_identity.epoch {
                    return Err("Storage changed or is incomplete. Reload the workspace.".into());
                }
                let response = client
                    .activity(
                        &task_identity.since,
                        &task_identity.until,
                        &task_identity.vault,
                    )
                    .map_err(|e| e.to_string())?;
                let after = client.storage().map_err(|e| e.to_string())?;
                if after.phase != "ready"
                    || after.generation != before.generation
                    || response.launch_generation != task_identity.launch
                    || response.source_vault != task_identity.vault
                    || response.activity.since != task_identity.since
                    || response.activity.until != task_identity.until
                {
                    return Err("Activity response became stale. Reload the workspace.".into());
                }
                Ok(response)
            });
            let response_identity = identity.clone();
            cx.spawn(async move |this, cx| {
                let result = job.await;
                let _ = this.update(cx, |this, cx| {
                    if !response_identity.accepts(
                        this.live.as_ref().map(|s| s.client().launch_generation),
                        this.accepted_storage_generation,
                        this.request,
                        this.shell.scope_revision,
                        this.working_vault_scope(),
                        this.shell.home
                            && !this.quit_session.is_closing()
                            && this.quit_session.failure().is_none(),
                    ) {
                        return;
                    }
                    if let Some(state) = this
                        .shell
                        .activity
                        .as_mut()
                        .filter(|state| state.identity == response_identity)
                    {
                        state.result = Some(result);
                        cx.notify();
                    }
                });
            })
            .detach();
        }
        let state = self.shell.activity.as_ref().unwrap();
        view = view.child(div().mb_2().child("Activity"));
        let Some(result) = &state.result else {
            return view.child("Loading activity…");
        };
        let response = match result {
            Ok(response) => response,
            Err(error) => {
                return view.child(format!("Activity unavailable: {error}")).child(
                    div()
                        .id("retry-activity")
                        .mt_2()
                        .cursor_pointer()
                        .child("Reload workspace")
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.load_notes(cx);
                        })),
                );
            }
        };
        let partial = incomplete(&response.diagnostics);
        let counts: std::collections::HashMap<_, _> = response
            .activity
            .counts
            .iter()
            .map(|c| (c.date.as_str(), c.count))
            .collect();
        // Missing buckets are zero only for complete responses, never partial ones.
        let colors = if dark {
            [0x303638, 0x623b33, 0x945040, 0xc66850, 0xf4785e]
        } else {
            [0xe9e7e2, 0xf0d4cb, 0xe5a18c, 0xd66949, 0xc13a1e]
        };
        let mut grid = div().flex().gap(px(3.));
        let mut labels = div().flex().flex_col().gap(px(3.)).w(px(30.)).pt(px(15.));
        for row in 0..7 {
            labels = labels.child(div().h(px(9.)).line_height(px(9.)).child(match row {
                1 => "Mon",
                3 => "Wed",
                5 => "Fri",
                _ => "",
            }));
        }
        grid = grid.child(labels);
        for (week, days) in dates.chunks(7).enumerate() {
            let caption = if week == 0 || days.iter().any(|d| d.day == 1) {
                format!(
                    "{:02}",
                    days.iter().find(|d| d.day == 1).unwrap_or(&days[0]).month
                )
            } else {
                String::new()
            };
            let mut column = div()
                .flex()
                .flex_col()
                .gap(px(3.))
                .child(div().h(px(12.)).text_size(px(9.)).child(caption));
            for day in days {
                let date = day.text();
                let count = counts.get(date.as_str()).copied();
                let label = match count {
                    Some(n) => format!("{date}: {n} contribution(s)"),
                    None if !partial => format!("{date}: 0 contributions"),
                    None => format!("{date}: count unavailable"),
                };
                let color = colors[band(count.unwrap_or(0))];
                let click_identity = identity.clone();
                let click_date = date.clone();
                column = column.child(
                    div()
                        .id(gpui::SharedString::from(format!("activity-{date}")))
                        .size(px(9.))
                        .rounded(px(2.))
                        .bg(rgb(color))
                        .cursor_pointer()
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.select_activity_day(
                                click_identity.clone(),
                                click_date.clone(),
                                cx,
                            );
                        }))
                        .on_hover(cx.listener(move |this, hover, _, cx| {
                            if let Some(state) = &mut this.shell.activity {
                                state.hover = if *hover { Some(label.clone()) } else { None };
                                cx.notify();
                            }
                        })),
                );
            }
            grid = grid.child(column);
        }
        view.child(grid)
            .when(partial, |view| {
                view.child(
                    div()
                        .mt_2()
                        .child("Incomplete or cached activity; missing counts are unknown."),
                )
            })
            .child(div().mt_2().child(state.hover.clone().unwrap_or_else(|| {
                if partial {
                    "Activity is incomplete or cached; missing counts are unknown.".into()
                } else if response.activity.total == 0 {
                    "No activity in this date range.".into()
                } else {
                    format!(
                        "{} contributions · {} to {}",
                        response.activity.total, response.activity.since, response.activity.until
                    )
                }
            })))
            .child(
                div()
                    .mt_1()
                    .child("Select a day for active notes · journal creation is disabled."),
            )
            .child(self.activity_agenda(cx))
            .child(self.activity_tasks(cx))
            .child(self.activity_events(cx))
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn callback_rejects_each_changed_identity_dimension() {
        let id = Identity {
            launch: 2,
            epoch: 8,
            request: 4,
            scope_revision: 3,
            vault: "work".into(),
            since: "2026-11-01".into(),
            until: "2026-11-14".into(),
        };
        assert!(id.accepts(Some(2), Some(8), 4, 3, "work", true));
        assert!(!id.accepts(Some(3), Some(8), 4, 3, "work", true));
        assert!(!id.accepts(Some(2), Some(9), 4, 3, "work", true));
        assert!(!id.accepts(Some(2), Some(8), 5, 3, "work", true));
        assert!(!id.accepts(Some(2), Some(8), 4, 3, "", true));
        assert!(!id.accepts(Some(2), Some(8), 4, 3, "work", false));
        assert!(!id.accepts(None, Some(8), 4, 3, "work", true));
        assert!(!id.accepts(Some(2), None, 4, 3, "work", true));
        assert!(!id.accepts(Some(2), Some(8), 4, 5, "work", true)); // scope ABA
        let mut changed = id.clone();
        changed.until = "2026-11-15".into();
        assert_ne!(id, changed);
        changed = id.clone();
        changed.since = "2026-10-25".into();
        assert_ne!(id, changed);
    }
    #[test]
    fn sunday_alignment_and_partial_last_week() {
        let d = calendar(
            Day {
                year: 2026,
                month: 11,
                day: 13,
            },
            5,
            4,
        );
        assert_eq!(d.len(), 27);
        assert_eq!(d[0].text(), "2026-10-18");
        assert_eq!(d.last().unwrap().text(), "2026-11-13");
    }
    #[test]
    fn calendar_arithmetic_handles_leap_year_and_dst_without_seconds() {
        assert_eq!(
            Day {
                year: 2024,
                month: 3,
                day: 1
            }
            .previous()
            .text(),
            "2024-02-29"
        );
        assert_eq!(
            Day {
                year: 2100,
                month: 3,
                day: 1
            }
            .previous()
            .text(),
            "2100-02-28"
        );
        assert_eq!(
            Day {
                year: 2026,
                month: 1,
                day: 1
            }
            .previous()
            .text(),
            "2025-12-31"
        );
        assert_eq!(
            calendar(
                Day {
                    year: 2026,
                    month: 3,
                    day: 9
                },
                1,
                2
            )
            .len(),
            9
        );
    }
    #[test]
    fn all_intensity_thresholds() {
        assert_eq!(
            (0..9).map(band).collect::<Vec<_>>(),
            vec![0, 1, 2, 2, 3, 3, 3, 4, 4]
        );
    }
    #[test]
    fn partial_and_cache_are_not_empty() {
        for value in [
            serde_json::json!({"partial":true}),
            serde_json::json!({"cache_used":true}),
            serde_json::json!({"omitted":1}),
        ] {
            let mut d = std::collections::BTreeMap::new();
            d.insert("read_state".into(), value);
            assert!(incomplete(&d));
        }
        assert!(!incomplete(&Default::default()));
    }
}

impl Workspace {
    fn activity_identity_current(&self, identity: &Identity) -> bool {
        identity.accepts(
            self.live.as_ref().map(|s| s.client().launch_generation),
            self.accepted_storage_generation,
            self.request,
            self.shell.scope_revision,
            self.working_vault_scope(),
            self.shell.home
                && !self.quit_session.is_closing()
                && self.quit_session.failure().is_none(),
        ) && self
            .shell
            .activity
            .as_ref()
            .is_some_and(|s| &s.identity == identity)
    }

    fn activity_selection_available(&self, cx: &App) -> bool {
        self.editing_ui.follow_safe()
            && !self.creation_open()
            && !self.handoff_active()
            && !self.includes_preview_active()
            && !self.shell.search_open
            && !self.shell.find.open
            && !self.shell.vault_menu
            && !self.shell.tab_menu
            && !self.shell.diagnostics
            && !self.previews.model.geometry_active()
            && self.previews.model.transient.is_none()
            && !self.search.read(cx).is_composing()
            && !self.shell.calendar_zone.is_composing(cx)
    }
    fn step_activity_day(
        &mut self,
        identity: Identity,
        serial: u64,
        date: &str,
        forward: bool,
        cx: &mut Context<Self>,
    ) {
        if !self.activity_identity_current(&identity)
            || !self.activity_selection_available(cx)
            || !self
                .shell
                .activity
                .as_ref()
                .and_then(|s| s.agenda.as_ref())
                .is_some_and(|a| a.accepts(serial, date))
        {
            return;
        }
        if let Some(next) = adjacent_in_range(&identity, date, forward) {
            self.select_activity_day(identity, next, cx);
        }
    }

    fn select_activity_day(&mut self, identity: Identity, date: String, cx: &mut Context<Self>) {
        if !self.activity_identity_current(&identity)
            || !self.activity_selection_available(cx)
            || !live::calendar_date_valid(&date)
            || date < identity.since
            || date > identity.until
        {
            return;
        }
        let Some(session) = self.live.as_ref() else {
            return;
        };
        let client = session.client();
        let zone_serial = self.shell.calendar_zone.advance();
        let zone = self.shell.calendar_zone.accepted.clone();
        let state = self.shell.activity.as_mut().unwrap();
        let serial = NEXT_AGENDA.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        state.agenda = Some(AgendaState::loading(date.clone(), serial));
        let Some(permit) = AgendaPermit::acquire() else {
            state.agenda.as_mut().unwrap().busy();
            cx.notify();
            return;
        };
        let captured = identity.clone();
        let selected = date.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let stale =
                || "Storage or daily-view identity changed. Reload the workspace.".to_owned();
            let before = client.storage();
            if !before
                .as_ref()
                .is_ok_and(|s| s.phase == "ready" && s.generation == captured.epoch)
            {
                return (Err(stale()), Err(stale()), Err(stale()));
            }
            let response = client
                .agenda(&selected, &captured.vault)
                .map_err(|e| e.to_string());
            let tasks = client
                .dated_tasks(&captured.vault)
                .map_err(|e| e.to_string());
            let events = client
                .calendar_day_events_in_zone(&captured.vault, &selected, &zone)
                .map_err(|e| e.to_string());
            let after = client.storage();
            if !after
                .as_ref()
                .is_ok_and(|s| s.phase == "ready" && s.generation == captured.epoch)
            {
                return (Err(stale()), Err(stale()), Err(stale()));
            }
            let response = response.and_then(|r| {
                if r.launch_generation == captured.launch
                    && r.source_vault == captured.vault
                    && r.date == selected
                {
                    Ok(r)
                } else {
                    Err(stale())
                }
            });
            let tasks = tasks.and_then(|r| {
                if r.launch_generation == captured.launch && r.source_vault == captured.vault {
                    Ok(r)
                } else {
                    Err(stale())
                }
            });
            let events = events.and_then(|r| {
                if r.launch_generation == captured.launch
                    && r.source_vault == captured.vault
                    && r.date == selected
                {
                    Ok(r)
                } else {
                    Err(stale())
                }
            });
            (response, tasks, events)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if !this.activity_identity_current(&identity)
                    || this.editing_ui.active()
                    || this.editing_ui.has_pending()
                {
                    return;
                }
                if let Some(agenda) = this
                    .shell
                    .activity
                    .as_mut()
                    .and_then(|s| s.agenda.as_mut())
                    .filter(|a| a.accepts(serial, &date))
                {
                    agenda.result = Some(result.0);
                    agenda.tasks = Some(result.1);
                    if this.shell.calendar_zone.serial == zone_serial {
                        agenda.events = Some(result.2);
                    } else if agenda.events.is_none() {
                        agenda.events_error =
                            Some("Display zone edited. Apply to load calendar events.".into());
                    }
                    agenda.events_page = 0;
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }

    fn activity_agenda(&self, cx: &mut Context<Self>) -> Div {
        let mut view = div().mt_3().flex().flex_col().gap_1();
        let Some(state) = self.shell.activity.as_ref() else {
            return view;
        };
        let Some(agenda) = state.agenda.as_ref() else {
            return view;
        };
        let available = self.activity_identity_current(&state.identity)
            && self.activity_selection_available(cx);
        let mut controls = div().flex().gap_3();
        for (forward, id, label) in [
            (false, "agenda-previous-day", "Previous day"),
            (true, "agenda-next-day", "Next day"),
        ] {
            let enabled =
                available && adjacent_in_range(&state.identity, &agenda.date, forward).is_some();
            let identity = state.identity.clone();
            let date = agenda.date.clone();
            let serial = agenda.serial;
            controls = controls.child(
                div()
                    .id(id)
                    .child(label)
                    .when(enabled, |button| {
                        button
                            .cursor_pointer()
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.step_activity_day(
                                    identity.clone(),
                                    serial,
                                    &date,
                                    forward,
                                    cx,
                                );
                            }))
                    })
                    .when(!enabled, |button| button.opacity(0.45)),
            );
        }
        view = view.child(controls).child(format!("Day navigation is limited to the displayed activity range: {} to {}. Boundary controls are disabled.", state.identity.since, state.identity.until));
        view = view.child(format!("Active notes · {}", agenda.date));
        let Some(result) = &agenda.result else {
            return view.child("Loading active notes…");
        };
        let response = match result {
            Ok(value) => value,
            Err(_) => {
                let identity = state.identity.clone();
                let date = agenda.date.clone();
                let serial = agenda.serial;
                return view
                    .child(
                        "Active notes unavailable; the request failed, is busy, or became stale.",
                    )
                    .child(
                        div()
                            .id("agenda-retry")
                            .cursor_pointer()
                            .child("Retry selected day")
                            .on_click(cx.listener(move |this, _, _, cx| {
                                if this
                                    .shell
                                    .activity
                                    .as_ref()
                                    .and_then(|s| s.agenda.as_ref())
                                    .is_some_and(|a| a.accepts(serial, &date))
                                {
                                    this.select_activity_day(identity.clone(), date.clone(), cx);
                                }
                            })),
                    );
            }
        };
        if incomplete(&response.diagnostics) {
            view = view.child("Incomplete or cached results; missing notes are unknown.");
        } else if response.notes.is_empty() {
            view = view.child("No active notes on this day.");
        }
        for (index, note) in response.notes.iter().take(100).enumerate() {
            let key = note.key();
            let label = format!(
                "{} · {}",
                note.title,
                if key.vault.is_empty() {
                    "launch vault"
                } else {
                    &key.vault
                }
            );
            let identity = state.identity.clone();
            let date = agenda.date.clone();
            let serial = agenda.serial;
            view = view.child(
                div()
                    .id(("agenda-note", index))
                    .cursor_pointer()
                    .child(label)
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if this.activity_identity_current(&identity)
                            && this
                                .shell
                                .activity
                                .as_ref()
                                .and_then(|s| s.agenda.as_ref())
                                .is_some_and(|a| {
                                    a.accepts(serial, &date)
                                        && a.result.as_ref().is_some_and(|r| r.is_ok())
                                })
                        {
                            this.open_live_key(key.clone(), cx);
                        }
                    })),
            );
        }
        if response.notes.len() > 100 {
            view = view.child(format!(
                "Showing the first 100 of {} notes; no further pages are fetched.",
                response.notes.len()
            ));
        }
        view
    }
}

#[cfg(test)]
mod agenda_tests {
    use super::*;
    #[test]
    fn outstanding_agenda_requests_are_bounded_and_release_on_drop() {
        let one = AgendaPermit::acquire().unwrap();
        let two = AgendaPermit::acquire().unwrap();
        assert!(AgendaPermit::acquire().is_none());
        drop(one);
        let replacement = AgendaPermit::acquire().unwrap();
        drop(two);
        drop(replacement);
        assert_eq!(ACTIVE_AGENDA.load(std::sync::atomic::Ordering::Acquire), 0);
    }
    #[test]
    fn calendar_failure_does_not_hide_agenda_or_tasks_and_success_keeps_diagnostics() {
        let mut state = AgendaState {
            date: "2026-11-14".into(),
            serial: 7,
            result: Some(Ok(serde_json::from_value(
                serde_json::json!({"date":"2026-11-14","notes":[]}),
            )
            .unwrap())),
            tasks: Some(Ok(serde_json::from_value(
                serde_json::json!({"vault":"work","tasks":[]}),
            )
            .unwrap())),
            events: Some(Err("unavailable".into())),
            events_page: 0,
            events_pending: None,
            events_error: None,
        };
        assert!(state.result.as_ref().unwrap().is_ok());
        assert!(state.tasks.as_ref().unwrap().is_ok());
        assert!(state.events.as_ref().unwrap().is_err());
        state.events = Some(Ok(serde_json::from_value(
            serde_json::json!({"vault":"work","date":"2026-11-14","time_zone":"UTC","day_start":"2026-11-14T00:00:00Z","day_end":"2026-11-15T00:00:00Z","events":[],"read_state":{"partial":true}}),
        )
        .unwrap()));
        state.result = Some(Err("unavailable".into()));
        state.tasks = Some(Err("unavailable".into()));
        assert!(incomplete(
            &state.events.as_ref().unwrap().as_ref().unwrap().diagnostics
        ));
        assert!(state.accepts(7, "2026-11-14"));
        assert!(!state.accepts(6, "2026-11-14"));
        assert!(!state.accepts(7, "2026-11-13"));
    }
    #[test]
    fn day_selection_rejects_out_of_order_and_same_day_retry_results() {
        let mut state = AgendaState {
            date: "2026-11-14".into(),
            serial: 1,
            result: None,
            tasks: None,
            events: None,
            events_page: 0,
            events_pending: None,
            events_error: None,
        };
        assert!(state.accepts(1, "2026-11-14"));
        assert!(!state.accepts(1, "2026-11-13"));
        state.serial = 2;
        assert!(!state.accepts(1, "2026-11-14"));
        assert!(state.accepts(2, "2026-11-14"));
        state.date = "2026-11-13".into();
        assert!(!state.accepts(2, "2026-11-14"));
    }
}

impl Workspace {
    fn activity_tasks(&self, cx: &mut Context<Self>) -> Div {
        let mut view = div().mt_3().flex().flex_col().gap_1();
        let Some(state) = self.shell.activity.as_ref() else {
            return view;
        };
        let Some(agenda) = state.agenda.as_ref() else {
            return view;
        };
        view = view.child(format!("Scheduled / due tasks · {}", agenda.date));
        let Some(result) = &agenda.tasks else {
            return view.child("Loading dated tasks…");
        };
        let response = match result {
            Ok(value) => value,
            Err(_) => {
                let identity = state.identity.clone();
                let date = agenda.date.clone();
                let serial = agenda.serial;
                return view.child("Dated tasks unavailable.").child(
                    div()
                        .id("daily-tasks-retry")
                        .cursor_pointer()
                        .child("Retry selected day")
                        .on_click(cx.listener(move |this, _, _, cx| {
                            if this
                                .shell
                                .activity
                                .as_ref()
                                .and_then(|s| s.agenda.as_ref())
                                .is_some_and(|a| a.accepts(serial, &date))
                            {
                                this.select_activity_day(identity.clone(), date.clone(), cx);
                            }
                        })),
                );
            }
        };
        let matching: Vec<_> = response
            .tasks
            .iter()
            .filter(|t| task_on_day(&t.scheduled, &t.due, &agenda.date))
            .collect();
        if incomplete(&response.diagnostics) {
            view = view.child("Incomplete or cached tasks; missing tasks are unknown.");
        } else if matching.is_empty() {
            view = view.child("No scheduled or due tasks on this day.");
        }
        for (index, task) in matching.iter().take(100).enumerate() {
            let key = live::NoteKey {
                vault: response.vault.clone(),
                note_id: task.note_id,
            };
            let identity = state.identity.clone();
            let date = agenda.date.clone();
            let serial = agenda.serial;
            let label = format!(
                "{} · {} · {} (line {}) · scheduled {} · due {}",
                task.state,
                task.text,
                task.title,
                task.line,
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
            view = view.child(
                div()
                    .id(("agenda-task", index))
                    .cursor_pointer()
                    .child(label)
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if this.activity_identity_current(&identity)
                            && this
                                .shell
                                .activity
                                .as_ref()
                                .and_then(|s| s.agenda.as_ref())
                                .is_some_and(|a| {
                                    a.accepts(serial, &date)
                                        && a.tasks.as_ref().is_some_and(|r| r.is_ok())
                                })
                        {
                            this.open_live_key(key.clone(), cx);
                        }
                    })),
            );
        }
        if matching.len() > 100 {
            view = view.child(format!(
                "Showing the first 100 of {} matching tasks.",
                matching.len()
            ));
        }
        view.child("Read-only states · opens the note, not the task line. No overdue or recurrence expansion.")
    }
}
fn task_on_day(scheduled: &str, due: &str, date: &str) -> bool {
    scheduled == date || due == date
}
#[cfg(test)]
mod daily_task_tests {
    use super::*;
    #[test]
    fn exact_calendar_day_union_is_not_overdue_or_double_counted() {
        assert!(task_on_day("2026-11-14", "", "2026-11-14"));
        assert!(task_on_day("", "2026-11-14", "2026-11-14"));
        assert!(task_on_day("2026-11-14", "2026-11-14", "2026-11-14"));
        assert!(!task_on_day("2026-11-13", "2026-11-13", "2026-11-14"));
        assert!(!task_on_day("", "", "2026-11-14"));
    }
}

fn events_page_range(total: usize, requested: usize) -> (usize, usize, std::ops::Range<usize>) {
    let pages = total.div_ceil(100);
    let page = requested.min(pages.saturating_sub(1));
    let start = page * 100;
    (page, pages, start..(start + 100).min(total))
}
fn events_heading(date: &str, zone: &str) -> String {
    format!("Calendar events · {date} ({zone})")
}
fn event_label(event: &live::CalendarEvent) -> String {
    format!(
        "{} · {} · {} to {} · {}{}",
        event.status,
        event.title,
        event.start,
        event.end,
        if event.all_day {
            "all-day civil dates (end exclusive)".to_owned()
        } else {
            format!("event zone: {} (original timestamps)", event.time_zone)
        },
        if event.location.is_empty() {
            String::new()
        } else {
            format!(" · {}", event.location)
        }
    )
}
impl Workspace {
    fn activity_events(&self, cx: &mut Context<Self>) -> Div {
        let mut view = div().mt_3().flex().flex_col().gap_1();
        let Some(state) = self.shell.activity.as_ref() else {
            return view;
        };
        let Some(agenda) = state.agenda.as_ref() else {
            return view;
        };
        view = view
            .child("Display zone · session only (default UTC)")
            .children(self.shell.calendar_zone.input.clone())
            .child(
                div()
                    .id("calendar-zone-apply")
                    .cursor_pointer()
                    .child("Apply display zone")
                    .on_click(cx.listener(|this, _, _, cx| this.apply_calendar_zone(cx))),
            );
        if let Some(zone) = &agenda.events_pending {
            view = view.child(format!("Loading {} ({zone})…", agenda.date));
        }
        if let Some(error) = &agenda.events_error {
            view = view.child(error.clone());
        }
        if agenda.events.is_none()
            && (agenda.events_error.is_some() || agenda.events_pending.is_some())
        {
            return view;
        }
        let Some(result) = &agenda.events else {
            return view.child("Loading calendar events…");
        };
        let response = match result {
            Ok(response) => response,
            Err(error) => return view.child(format!("Calendar events unavailable for {} ({}): {error}. Select the day again to retry; notes and tasks are independent.", agenda.date, self.shell.calendar_zone.accepted)),
        };
        view = view.child(events_heading(&response.date, &response.time_zone)).child(
            "Timed events match this display-zone date; all-day events keep civil-date membership.",
        );
        if incomplete(&response.diagnostics) {
            view = view.child("Incomplete or cached calendar events; missing events are unknown.");
        } else if response.events.is_empty() {
            view = view.child("No calendar events on this display-zone date.");
        }
        let (page, pages, range) = events_page_range(response.events.len(), agenda.events_page);
        let mut controls = div().flex().gap_3();
        if pages > 0 {
            controls = controls.child(format!(
                "{} events · page {}/{} · snapshot",
                response.events.len(),
                page + 1,
                pages
            ));
        }
        for (label, target) in [
            ("Previous", page.checked_sub(1)),
            ("Next", (page + 1 < pages).then_some(page + 1)),
        ] {
            if let Some(target) = target {
                let identity = state.identity.clone();
                let serial = agenda.serial;
                let zone_serial = self.shell.calendar_zone.serial;
                let date = agenda.date.clone();
                controls = controls.child(
                    div()
                        .id(if label == "Previous" {
                            "calendar-events-prev"
                        } else {
                            "calendar-events-next"
                        })
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, _, cx| {
                            if !this.activity_identity_current(&identity)
                                || this.shell.calendar_zone.serial != zone_serial
                            {
                                return;
                            }
                            if let Some(agenda) = this
                                .shell
                                .activity
                                .as_mut()
                                .and_then(|s| s.agenda.as_mut())
                                .filter(|a| {
                                    a.accepts(serial, &date)
                                        && a.events.as_ref().is_some_and(|r| r.is_ok())
                                })
                            {
                                agenda.events_page = target;
                                cx.notify();
                            }
                        })),
                );
            }
        }
        // Serial is globally unique across selected-day requests, including ABA.
        let mut rows = div()
            .id(SharedString::from(format!(
                "calendar-events-{}-{}-{page}",
                agenda.serial, response.time_zone
            )))
            .max_h(px(300.))
            .overflow_y_scroll();
        for event in &response.events[range] {
            rows = rows.child(div().mb_2().child(event_label(event)));
        }
        view.child(controls)
            .child(rows)
            .child("Read-only calendar metadata · no event editing, links or recurrence expansion.")
    }
}

#[cfg(test)]
mod utc_event_display_tests {
    use super::*;
    #[test]
    fn pages_reach_every_record_in_engine_order() {
        for total in [0usize, 1, 100, 101, 4096] {
            let mut visited = Vec::new();
            let pages = total.div_ceil(100);
            for requested in 0..pages {
                let (page, count, range) = events_page_range(total, requested);
                assert_eq!(page, requested);
                assert_eq!(count, pages);
                assert!(range.len() <= 100);
                visited.extend(range);
            }
            assert_eq!(visited, (0..total).collect::<Vec<_>>());
            let (_, _, last) = events_page_range(total, usize::MAX);
            assert_eq!(last.end, total);
            assert_eq!(events_page_range(total, 0).2.start, 0);
        }
    }
    #[test]
    fn explicit_utc_heading_and_original_event_labels() {
        assert_eq!(
            events_heading("2026-11-01", "UTC"),
            "Calendar events · 2026-11-01 (UTC)"
        );
        let mut event: live::CalendarEvent = serde_json::from_value(serde_json::json!({
            "version":1,"id":"00000000-0000-4000-8000-000000000001","revision":1,
            "title":"Original","all_day":false,"start":"2026-11-01T01:30:00.123-04:00",
            "end":"2026-11-01T01:30:00.456-05:00","time_zone":"America/New_York",
            "status":"cancelled","description":"","location":"room","url":"","note_id":"",
            "deleted":false,"updated_at":"2026-11-01T00:00:00Z"
        }))
        .unwrap();
        let label = event_label(&event);
        for original in [&event.start, &event.end, &event.time_zone, &event.status] {
            assert!(label.contains(original));
        }
        event.all_day = true;
        assert!(event_label(&event).contains("all-day civil dates (end exclusive)"));
    }
}

impl Workspace {
    fn sync_calendar_zone(&mut self, dark: bool, cx: &mut Context<Self>) {
        let launch = self.live.as_ref().map(|s| s.client().launch_generation);
        if self.shell.calendar_zone.launch != launch {
            self.shell.calendar_zone = CalendarZone {
                launch,
                ..Default::default()
            };
        }
        if self.shell.calendar_zone.input.is_none() {
            let input = cx.new(|cx| {
                search_input::SearchInput::with_purpose(
                    search_input::InputPurpose::CalendarZone,
                    cx,
                )
            });
            self.subscriptions.push(cx.subscribe(
                &input,
                |this, entity, event: &search_input::SearchChanged, cx| {
                    if this.shell.calendar_zone.input.as_ref() != Some(&entity) {
                        return;
                    }
                    this.shell.calendar_zone.edit(event.0.clone());
                    if let Some(agenda) =
                        this.shell.activity.as_mut().and_then(|s| s.agenda.as_mut())
                    {
                        agenda.events_pending = None;
                        agenda.events_error = Some(
                            "Display zone edited; select Apply display zone to load it.".into(),
                        );
                    }
                    cx.notify();
                },
            ));
            self.shell.calendar_zone.input = Some(input);
        }
        if let Some(input) = &self.shell.calendar_zone.input {
            input.update(cx, |input, _| input.set_dark(dark));
        }
    }

    fn apply_calendar_zone(&mut self, cx: &mut Context<Self>) {
        let Some(state) = self.shell.activity.as_ref() else {
            return;
        };
        let identity = state.identity.clone();
        let Some(agenda) = state.agenda.as_ref() else {
            return;
        };
        let date = agenda.date.clone();
        let agenda_serial = agenda.serial;
        if !self.activity_identity_current(&identity)
            || self.editing_ui.active()
            || self.editing_ui.has_pending()
            || self
                .shell
                .calendar_zone
                .input
                .as_ref()
                .is_some_and(|input| input.read(cx).is_composing())
        {
            return;
        }
        let zone = self.shell.calendar_zone.draft.clone();
        let serial = self.shell.calendar_zone.advance();
        let agenda = self
            .shell
            .activity
            .as_mut()
            .unwrap()
            .agenda
            .as_mut()
            .unwrap();
        agenda.events_pending = Some(zone.clone());
        agenda.events_error = None;
        let Some(permit) = AgendaPermit::acquire() else {
            agenda.events_pending = None;
            agenda.events_error = Some(format!(
                "Display zone {zone:?} not applied: earlier requests are still finishing. Retry shortly."
            ));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let captured = identity.clone();
        let selected = date.clone();
        let requested = zone.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let stale =
                || "Storage or daily-view identity changed. Reload the workspace.".to_owned();
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.epoch {
                return Err(stale());
            }
            let response = client
                .calendar_day_events_in_zone(&captured.vault, &selected, &requested)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready"
                || after.generation != captured.epoch
                || response.launch_generation != captured.launch
                || response.source_vault != captured.vault
                || response.date != selected
                || response.time_zone != requested
            {
                return Err(stale());
            }
            Ok(response)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if !this.activity_identity_current(&identity)
                    || !this.shell.calendar_zone.accepts(serial, &zone)
                    || this.editing_ui.active()
                    || this.editing_ui.has_pending()
                {
                    return;
                }
                if let Some(agenda) = this
                    .shell
                    .activity
                    .as_mut()
                    .and_then(|s| s.agenda.as_mut())
                    .filter(|a| a.accepts(agenda_serial, &date))
                {
                    agenda.finish_zone(&mut this.shell.calendar_zone, zone, result);
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }
}

#[cfg(test)]
mod zone_selection_tests {
    use super::*;
    fn response(zone: &str) -> live::CalendarDayEventsResponse {
        serde_json::from_value(
            serde_json::json!({"vault":"work","date":"2026-11-14","time_zone":zone,
            "day_start":"2026-11-14T00:00:00Z","day_end":"2026-11-15T00:00:00Z","events":[]}),
        )
        .unwrap()
    }
    fn agenda() -> AgendaState {
        AgendaState {
            date: "2026-11-14".into(),
            serial: 7,
            result: None,
            tasks: None,
            events: Some(Ok(response("UTC"))),
            events_page: 2,
            events_pending: None,
            events_error: None,
        }
    }
    #[test]
    fn edit_is_local_and_invalidates_aba_without_relabeling_snapshot() {
        let mut selection = CalendarZone::default();
        let original = selection.advance();
        let state = agenda();
        selection.edit("Asia/Tokyo".into());
        assert_eq!(selection.accepted, "UTC");
        assert_eq!(
            state.events.as_ref().unwrap().as_ref().unwrap().time_zone,
            "UTC"
        );
        selection.edit("UTC".into());
        assert!(!selection.accepts(original, "UTC"));
        let applied = selection.advance();
        assert!(selection.accepts(applied, "UTC"));
        selection.advance(); // date selection also invalidates the previous Apply
        assert!(!selection.accepts(applied, "UTC"));
    }
    #[test]
    fn apply_success_commits_but_failure_preserves_accepted_label_and_rows() {
        let mut selection = CalendarZone::default();
        let mut state = agenda();
        state.events_pending = Some("Asia/Tokyo".into());
        state.finish_zone(
            &mut selection,
            "Asia/Tokyo".into(),
            Ok(response("Asia/Tokyo")),
        );
        assert_eq!(selection.accepted, "Asia/Tokyo");
        assert_eq!(state.events_page, 0);
        assert!(state.events_pending.is_none());
        state.events_pending = Some("Local".into());
        state.finish_zone(&mut selection, "Local".into(), Err("unknown zone".into()));
        assert_eq!(selection.accepted, "Asia/Tokyo");
        let accepted = state.events.as_ref().unwrap().as_ref().unwrap();
        assert_eq!(
            events_heading(&accepted.date, &accepted.time_zone),
            "Calendar events · 2026-11-14 (Asia/Tokyo)"
        );
        assert!(state.events_error.as_ref().unwrap().contains("Local"));
        assert!(state.events_pending.is_none());
        assert_eq!(CalendarZone::default().accepted, "UTC");
    }
}

#[cfg(test)]
mod adjacent_day_tests {
    use super::*;
    fn identity() -> Identity {
        Identity {
            launch: 1,
            epoch: 2,
            request: 3,
            scope_revision: 4,
            vault: "work".into(),
            since: "2024-02-28".into(),
            until: "2024-03-02".into(),
        }
    }
    #[test]
    fn civil_steps_cover_month_year_leap_century_and_dst_dates() {
        for (from, to) in [
            ("2024-02-28", "2024-02-29"),
            ("2024-02-29", "2024-03-01"),
            ("2023-02-28", "2023-03-01"),
            ("1900-02-28", "1900-03-01"),
            ("2000-02-28", "2000-02-29"),
            ("2026-04-30", "2026-05-01"),
            ("2026-12-31", "2027-01-01"),
            ("2026-03-08", "2026-03-09"),
            ("2026-11-01", "2026-11-02"),
        ] {
            assert_eq!(adjacent_day(from, true).as_deref(), Some(to));
            assert_eq!(adjacent_day(to, false).as_deref(), Some(from));
        }
        for invalid in [
            "",
            "2024-2-28",
            "2023-02-29",
            "2026-13-01",
            "2026-00-00",
            "日本語",
        ] {
            assert!(adjacent_day(invalid, true).is_none());
            assert!(adjacent_day(invalid, false).is_none());
        }
        assert!(adjacent_day("9999-12-31", true).is_none());
        assert!(adjacent_day("0000-01-01", false).is_none());
    }
    #[test]
    fn range_boundaries_do_not_skip_empty_dates_or_expand_requests() {
        let identity = identity();
        assert!(adjacent_in_range(&identity, &identity.since, false).is_none());
        assert!(adjacent_in_range(&identity, &identity.until, true).is_none());
        assert!(adjacent_in_range(&identity, "2024-02-27", true).is_none());
        assert_eq!(
            adjacent_in_range(&identity, "2024-02-28", true).as_deref(),
            Some("2024-02-29")
        );
        assert_eq!(
            adjacent_in_range(&identity, "2024-03-01", false).as_deref(),
            Some("2024-02-29")
        );
        assert_eq!(identity.vault, "work");
    }
    #[test]
    fn consecutive_steps_and_aba_reject_late_results_and_stale_controls() {
        let identity = identity();
        let mut current = AgendaState::loading("2024-02-28".into(), 11);
        let first = (current.serial, current.date.clone());
        let next = adjacent_in_range(&identity, &current.date, true).unwrap();
        current = AgendaState::loading(next, 12);
        assert!(!current.accepts(first.0, &first.1));
        let second = (current.serial, current.date.clone());
        current = AgendaState::loading(
            adjacent_in_range(&identity, &current.date, false).unwrap(),
            13,
        );
        assert_eq!(current.date, first.1);
        assert!(!current.accepts(first.0, &first.1));
        assert!(!current.accepts(second.0, &second.1));
        assert!(current.accepts(13, "2024-02-28"));
    }
    #[test]
    fn new_day_resets_all_sections_and_busy_failure_keeps_new_date() {
        let mut state = AgendaState::loading("2024-02-28".into(), 1);
        state.result = Some(Err("old notes".into()));
        state.tasks = Some(Err("old tasks".into()));
        state.events = Some(Err("old events".into()));
        state.events_page = 4;
        state.events_pending = Some("Asia/Tokyo".into());
        state.events_error = Some("old error".into());
        assert!(state.result.is_some());
        state = AgendaState::loading("2024-02-29".into(), 2);
        assert!(state.result.is_none() && state.tasks.is_none() && state.events.is_none());
        assert_eq!(state.events_page, 0);
        assert!(state.events_pending.is_none() && state.events_error.is_none());
        state.busy();
        assert!(state.result.as_ref().unwrap().is_err());
        assert!(state.tasks.as_ref().unwrap().is_err());
        assert!(state.events.as_ref().unwrap().is_err());
        assert_eq!(state.date, "2024-02-29");
        assert!(!state.accepts(1, "2024-02-28"));
        let retry = AgendaState::loading(state.date.clone(), 3);
        assert!(!retry.accepts(state.serial, &state.date));
    }
}
