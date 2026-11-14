//! Read-only Home calendar. Dates use the local calendar shared with the owned
//! child server; no journal endpoint or note mutation is used here.
use super::*;
use gpui::Div;

static NEXT_AGENDA: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1);
static ACTIVE_AGENDA: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);
struct AgendaPermit;
impl AgendaPermit {
    fn acquire() -> Option<Self> {
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
        vault: &str,
        available: bool,
    ) -> bool {
        available
            && launch == Some(self.launch)
            && epoch == Some(self.epoch)
            && request == self.request
            && vault == self.vault
    }
}
pub(crate) struct ActivityState {
    identity: Identity,
    result: Option<Result<live::ActivityResponse, String>>,
    hover: Option<String>,
    agenda: Option<AgendaState>,
}
struct AgendaState {
    date: String,
    serial: u64,
    result: Option<Result<live::AgendaResponse, String>>,
}
impl AgendaState {
    fn accepts(&self, serial: u64, date: &str) -> bool {
        self.serial == serial && self.date == date
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
fn calendar(today: Day, weekday: usize, weeks: usize) -> Vec<Day> {
    let count = (weeks.clamp(1, 40) - 1) * 7 + weekday.min(6) + 1;
    let mut dates = vec![today];
    for _ in 1..count {
        dates.push(dates.last().unwrap().previous());
    }
    dates.reverse();
    dates
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
fn incomplete(fields: &std::collections::BTreeMap<String, serde_json::Value>) -> bool {
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
            vault: "work".into(),
            since: "2026-11-01".into(),
            until: "2026-11-14".into(),
        };
        assert!(id.accepts(Some(2), Some(8), 4, "work", true));
        assert!(!id.accepts(Some(3), Some(8), 4, "work", true));
        assert!(!id.accepts(Some(2), Some(9), 4, "work", true));
        assert!(!id.accepts(Some(2), Some(8), 5, "work", true));
        assert!(!id.accepts(Some(2), Some(8), 4, "", true));
        assert!(!id.accepts(Some(2), Some(8), 4, "work", false));
        assert!(!id.accepts(None, Some(8), 4, "work", true));
        assert!(!id.accepts(Some(2), None, 4, "work", true));
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

    fn select_activity_day(&mut self, identity: Identity, date: String, cx: &mut Context<Self>) {
        if !self.activity_identity_current(&identity)
            || date < identity.since
            || date > identity.until
        {
            return;
        }
        let Some(session) = self.live.as_ref() else {
            return;
        };
        let client = session.client();
        let state = self.shell.activity.as_mut().unwrap();
        let serial = NEXT_AGENDA.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        state.agenda = Some(AgendaState {
            date: date.clone(),
            serial,
            result: None,
        });
        let Some(permit) = AgendaPermit::acquire() else {
            state.agenda.as_mut().unwrap().result = Some(Err(
                "Earlier requests are still finishing. Retry shortly.".into(),
            ));
            cx.notify();
            return;
        };
        let captured = identity.clone();
        let selected = date.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.epoch {
                return Err("Storage changed. Reload the workspace.".into());
            }
            let response = client
                .agenda(&selected, &captured.vault)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready"
                || after.generation != before.generation
                || response.launch_generation != captured.launch
                || response.source_vault != captured.vault
                || response.date != selected
            {
                return Err("Agenda response became stale. Reload the workspace.".into());
            }
            Ok(response)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if !this.activity_identity_current(&identity) {
                    return;
                }
                if let Some(agenda) = this
                    .shell
                    .activity
                    .as_mut()
                    .and_then(|s| s.agenda.as_mut())
                    .filter(|a| a.accepts(serial, &date))
                {
                    agenda.result = Some(result);
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
    fn day_selection_rejects_out_of_order_and_same_day_retry_results() {
        let mut state = AgendaState {
            date: "2026-11-14".into(),
            serial: 1,
            result: None,
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
