//! Bounded civil period and engine-day batch. No local timezone interpretation.
use crate::{calendar_month::Month, live, weekly_tasks, workspace_activity::adjacent_day};
use std::sync::atomic::{AtomicBool, Ordering};
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) enum View {
    #[default]
    Month,
    Today,
    Tomorrow,
    Week,
}
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) enum Filter {
    #[default]
    All,
    Events,
    Tasks,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Period {
    pub days: Vec<String>,
}
impl Period {
    pub fn current(view: View, today: &str) -> Option<Self> {
        let month = Month::containing(today)?;
        let day: usize = today[8..].parse().ok()?;
        let weekday = (month.leading() + day - 1) % 7;
        Some(Self {
            days: match view {
                View::Month => return None,
                View::Today => vec![today.into()],
                View::Tomorrow => vec![adjacent_day(today, true)?],
                View::Week => weekly_tasks::Week::containing(today, weekday)?.days,
            },
        })
    }
    pub fn shifted(&self, forward: bool) -> Option<Self> {
        if !matches!(self.days.len(), 1 | 7) {
            return None;
        }
        let mut days = self.days.clone();
        for day in &mut days {
            for _ in 0..self.days.len() {
                *day = adjacent_day(day, forward)?;
            }
        }
        days.iter()
            .all(|d| Month::containing(d).is_some())
            .then_some(Self { days })
    }
}
pub(crate) fn utc_date(now: i64) -> Option<String> {
    // Gregorian civil date from UTC epoch days (not display-zone arithmetic).
    let z = now.div_euclid(86400).checked_add(719468)?;
    let era = z.div_euclid(146097);
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let mut year = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = mp + if mp < 10 { 3 } else { -9 };
    year += i64::from(month <= 2);
    let date = format!("{year:04}-{month:02}-{day:02}");
    Month::containing(&date).map(|_| date)
}
pub(crate) fn contains_now(day: &live::CalendarDayEventsResponse, now: i64) -> bool {
    matches!((day.day_start_unix, day.day_end_unix), (Some(a), Some(b)) if a <= now && now < b)
}
pub(crate) struct Batch {
    pub today: String,
    pub today_start: i64,
    pub today_end: i64,
    pub period: Period,
    pub events: Vec<Result<live::CalendarDayEventsResponse, String>>,
    pub tasks: Result<live::DatedTasksResponse, String>,
}
fn event_bytes(day: &live::CalendarDayEventsResponse) -> usize {
    day.events.iter().fold(0usize, |total, e| {
        [
            &e.title,
            &e.description,
            &e.location,
            &e.url,
            &e.start,
            &e.end,
            &e.time_zone,
            &e.note_id,
            &e.id,
            &e.status,
            &e.updated_at,
        ]
        .iter()
        .fold(total.saturating_add(128), |n, v| n.saturating_add(v.len()))
    })
}
fn valid_period(period: &Period) -> bool {
    matches!(period.days.len(), 1 | 7)
        && period.days.iter().all(|d| Month::containing(d).is_some())
        && period
            .days
            .windows(2)
            .all(|w| adjacent_day(&w[0], true).as_ref() == Some(&w[1]))
}
fn task_bytes(tasks: &live::DatedTasksResponse) -> usize {
    tasks.tasks.iter().fold(0usize, |n, t| {
        [
            &t.text,
            &t.title,
            &t.path,
            &t.priority,
            &t.scheduled,
            &t.due,
            &t.completed,
            &t.state,
            &t.file_kind,
        ]
        .iter()
        .fold(n.saturating_add(128), |n, v| n.saturating_add(v.len()))
    })
}
/// At most 3 anchor probes + 7 displayed days, serial; matching today is reused.
/// Each transport already has BODY_LIMIT and timeout. Accepted event aggregate:
/// 4096 records / 4 MiB textual metadata; never silently truncate a day.
pub(crate) fn run(
    now: i64,
    view: View,
    requested: Option<Period>,
    cancel: &AtomicBool,
    mut day_read: impl FnMut(&str) -> Result<live::CalendarDayEventsResponse, String>,
    task_read: impl FnOnce() -> Result<live::DatedTasksResponse, String>,
) -> Result<Batch, String> {
    if requested.as_ref().is_some_and(|p| !valid_period(p)) {
        return Err("Invalid period".into());
    }
    let utc = utc_date(now).ok_or("Clock outside supported civil range")?;
    let candidates = [
        Some(utc.clone()),
        adjacent_day(&utc, false),
        adjacent_day(&utc, true),
    ];
    let mut matched = None;
    for date in candidates.into_iter().flatten() {
        if cancel.load(Ordering::Acquire) {
            return Err("Cancelled".into());
        }
        if let Ok(day) = day_read(&date) {
            if day.date == date && contains_now(&day, now) {
                matched = Some(day);
                break;
            }
        }
    }
    let today_response =
        matched.ok_or("Display-zone today unavailable; Retry. No local-date fallback.")?;
    let today = today_response.date.clone();
    let today_start = today_response.day_start_unix.unwrap();
    let today_end = today_response.day_end_unix.unwrap();
    let period = requested
        .or_else(|| Period::current(view, &today))
        .ok_or("Period unavailable")?;
    if !valid_period(&period) {
        return Err("Invalid period".into());
    }
    let mut events = Vec::new();
    let mut cached = Some(today_response);
    let (mut count, mut bytes) = (0usize, 0usize);
    for date in &period.days {
        if cancel.load(Ordering::Acquire) {
            return Err("Cancelled".into());
        }
        let result = if *date == today {
            Ok(cached.take().unwrap())
        } else {
            day_read(date)
        };
        let result = result.and_then(|day| {
            if day.date != *date {
                return Err("Day response mismatch".into());
            }
            count = count.saturating_add(day.events.len());
            bytes = bytes.saturating_add(event_bytes(&day));
            if count > 4096 || bytes > 4 * 1024 * 1024 {
                Err("Event aggregate limit exceeded; this day is unknown.".into())
            } else {
                Ok(day)
            }
        });
        events.push(result);
    }
    if cancel.load(Ordering::Acquire) {
        return Err("Cancelled".into());
    }
    let tasks = task_read().and_then(|tasks| {
        if tasks.tasks.len() > 4096 || task_bytes(&tasks) > 4 * 1024 * 1024 {
            Err("Task aggregate limit exceeded; tasks unknown.".into())
        } else {
            Ok(tasks)
        }
    });
    Ok(Batch {
        today,
        today_start,
        today_end,
        period,
        events,
        tasks,
    })
}
pub(crate) fn task_rows(
    tasks: &[live::DatedTask],
    period: &Period,
    today: &str,
) -> Result<(Vec<Vec<usize>>, Vec<usize>, usize), String> {
    // Reuse weekly ordering/open/invalid-date contracts, then select visible dates.
    let start = period.days.first().ok_or("Empty period")?;
    let mut days = vec![start.clone()];
    for _ in 1..7 {
        days.push(adjacent_day(days.last().unwrap(), true).ok_or("Date boundary")?);
    }
    let projection = weekly_tasks::project(tasks, &weekly_tasks::Week { days }, today)?;
    let visible = projection
        .days
        .into_iter()
        .take(period.days.len())
        .collect();
    let overdue = if period.days.iter().any(|d| d == today) {
        projection.overdue
    } else {
        vec![]
    };
    Ok((visible, overdue, projection.invalid_dates))
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn civil_periods_and_utc_anchor() {
        assert_eq!(utc_date(0).as_deref(), Some("1970-01-01"));
        assert_eq!(utc_date(-1).as_deref(), Some("1969-12-31"));
        let week = Period::current(View::Week, "2024-02-29").unwrap();
        assert_eq!(week.days[0], "2024-02-25");
        assert_eq!(week.days[6], "2024-03-02");
        assert_eq!(week.shifted(true).unwrap().days[0], "2024-03-03");
        assert_eq!(
            Period::current(View::Tomorrow, "2024-12-31").unwrap().days[0],
            "2025-01-01"
        );
    }
    fn day(date: &str, start: i64, end: i64) -> live::CalendarDayEventsResponse {
        live::CalendarDayEventsResponse {
            vault: "".into(),
            date: date.into(),
            time_zone: "UTC".into(),
            day_start: "".into(),
            day_end: "".into(),
            day_start_unix: Some(start),
            day_end_unix: Some(end),
            events: vec![],
            source_vault: "".into(),
            launch_generation: 1,
            diagnostics: Default::default(),
        }
    }
    #[test]
    fn probes_reuse_matching_day_and_keep_failures_independent() {
        let mut calls = vec![];
        let batch = run(
            0,
            View::Today,
            None,
            &AtomicBool::new(false),
            |date| {
                calls.push(date.to_owned());
                Ok(day(date, if date == "1969-12-31" { -1 } else { 1 }, 100))
            },
            || Err("tasks failed".into()),
        )
        .unwrap();
        assert_eq!(calls, vec!["1970-01-01", "1969-12-31"]);
        assert_eq!(batch.today, "1969-12-31");
        assert!(batch.events[0].is_ok());
        assert!(batch.tasks.is_err());
        assert!(!contains_now(&day("1970-01-01", 0, 10), 10));
    }
    #[test]
    fn failed_anchor_and_cancellation_never_fetch_tasks() {
        let mut reads = 0;
        assert!(
            run(
                0,
                View::Week,
                None,
                &AtomicBool::new(false),
                |_| {
                    reads += 1;
                    Err("offline".into())
                },
                || panic!("no tasks before anchor")
            )
            .is_err()
        );
        assert_eq!(reads, 3);
        assert!(
            run(
                0,
                View::Week,
                None,
                &AtomicBool::new(true),
                |_| panic!("cancelled"),
                || panic!("cancelled")
            )
            .is_err()
        );
    }
    #[test]
    fn request_budget_partial_days_and_preflight_validation() {
        let cancel = AtomicBool::new(false);
        let mut calls = 0;
        let period = Period {
            days: (1..=7).map(|d| format!("2024-03-{d:02}")).collect(),
        };
        let b = run(
            0,
            View::Week,
            Some(period),
            &cancel,
            |date| {
                calls += 1;
                if calls < 3 {
                    return Err("probe unavailable".into());
                }
                if date == "2024-03-03" {
                    return Err("one day failed".into());
                }
                Ok(day(date, -1, 86400))
            },
            || Err("task failure independent".into()),
        )
        .unwrap();
        assert_eq!(calls, 10);
        assert_eq!(b.events.len(), 7);
        assert!(b.events[2].is_err());
        assert!(b.events[1].is_ok());
        assert!(b.tasks.is_err());
        assert!(
            run(
                0,
                View::Week,
                Some(Period { days: vec![] }),
                &cancel,
                |_| panic!("invalid period must not read"),
                || panic!()
            )
            .is_err()
        );
    }
    #[test]
    fn cancelling_after_probe_prevents_next_request() {
        let cancel = AtomicBool::new(false);
        let mut reads = 0;
        assert!(
            run(
                0,
                View::Week,
                None,
                &cancel,
                |date| {
                    reads += 1;
                    cancel.store(true, Ordering::Release);
                    Ok(day(date, -1, 86400))
                },
                || panic!("cancelled tasks")
            )
            .is_err()
        );
        assert_eq!(reads, 1);
    }
    #[test]
    fn authoritative_bounds_accept_dst_and_exclude_end() {
        for hours in [23, 25] {
            let d = day("2026-11-01", 100, 100 + hours * 3600);
            assert!(contains_now(&d, 100));
            assert!(contains_now(&d, 100 + hours * 3600 - 1));
            assert!(!contains_now(&d, 99));
            assert!(!contains_now(&d, 100 + hours * 3600));
        }
        assert!(utc_date(i64::MAX).is_none());
    }
    fn task(scheduled: &str, due: &str) -> live::DatedTask {
        live::DatedTask {
            note_id: 1,
            file_kind: "note".into(),
            title: "Source".into(),
            path: "".into(),
            line: 1,
            state: "TODO".into(),
            done: false,
            priority: "".into(),
            scheduled: scheduled.into(),
            due: due.into(),
            completed: "".into(),
            text: "Task".into(),
        }
    }
    #[test]
    fn task_union_overdue_and_invalid_dates_remain_explicit() {
        let p = Period::current(View::Week, "2024-02-29").unwrap();
        let tasks = vec![
            task("2024-02-29", "2024-02-29"),
            task("2024-02-28", "2024-03-01"),
            task("2024-02-29", "2024-02-30"),
            task("", "2024-02-20"),
        ];
        let (days, overdue, invalid) = task_rows(&tasks, &p, "2024-02-29").unwrap();
        assert_eq!(days[4].iter().filter(|i| **i == 0).count(), 1);
        assert!(days[3].contains(&1));
        assert!(days[5].contains(&1));
        assert!(days[4].contains(&2));
        assert_eq!(invalid, 1);
        assert!(overdue.contains(&3));
        let next = p.shifted(true).unwrap();
        assert!(task_rows(&tasks, &next, "2024-02-29").unwrap().1.is_empty());
    }
    #[test]
    fn task_and_event_aggregate_budgets_fail_without_truncation() {
        let mut huge = task("1970-01-01", "");
        huge.text = "x".repeat(4 * 1024 * 1024);
        let b = run(
            0,
            View::Today,
            None,
            &AtomicBool::new(false),
            |date| Ok(day(date, 0, 86400)),
            || {
                Ok(live::DatedTasksResponse {
                    vault: "".into(),
                    tasks: vec![huge],
                    source_vault: "".into(),
                    launch_generation: 1,
                    diagnostics: Default::default(),
                })
            },
        )
        .unwrap();
        assert!(b.tasks.is_err());
        assert!(b.events[0].is_ok());
    }
}
