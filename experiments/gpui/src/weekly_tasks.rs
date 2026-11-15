//! A bounded projection of one vault's dated-task snapshot onto civil weeks.
use crate::{live, workspace_activity::adjacent_day};
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Week {
    pub days: Vec<String>,
}
impl Week {
    pub fn containing(today: &str, weekday: usize) -> Option<Self> {
        if weekday > 6 || !live::calendar_date_valid(today) {
            return None;
        }
        let mut start = today.to_owned();
        for _ in 0..weekday {
            start = adjacent_day(&start, false)?;
        }
        Self::starting(start)
    }
    fn starting(start: String) -> Option<Self> {
        let mut days = vec![start];
        for _ in 1..7 {
            days.push(adjacent_day(days.last()?, true)?);
        }
        Some(Self { days })
    }
    pub fn shifted(&self, forward: bool) -> Option<Self> {
        let mut start = self.days.first()?.clone();
        for _ in 0..7 {
            start = adjacent_day(&start, forward)?;
        }
        Self::starting(start)
    }
}
pub(crate) struct Projection {
    pub days: Vec<Vec<usize>>,
    pub overdue: Vec<usize>,
    pub invalid_dates: usize,
}
fn open(task: &live::DatedTask) -> bool {
    !task.done && !matches!(task.state.as_str(), "DONE" | "CANCELLED")
}
fn dated(date: &str) -> bool {
    !date.is_empty() && live::calendar_date_valid(date)
}
pub(crate) fn project(
    tasks: &[live::DatedTask],
    week: &Week,
    today: &str,
) -> Result<Projection, String> {
    if tasks.len() > 4096
        || week.days.len() != 7
        || !live::calendar_date_valid(today)
        || Week::starting(week.days.first().cloned().unwrap_or_default()).as_ref() != Some(week)
    {
        return Err("Weekly task bounds or date unavailable.".into());
    }
    let mut indices: Vec<_> = tasks
        .iter()
        .enumerate()
        .filter(|(_, task)| open(task))
        .map(|(i, _)| i)
        .collect();
    indices.sort_by(|a, b| {
        let a = &tasks[*a];
        let b = &tasks[*b];
        (
            a.due.is_empty(),
            &a.due,
            a.priority.is_empty(),
            &a.priority,
            a.note_id.to_string(),
            a.line,
        )
            .cmp(&(
                b.due.is_empty(),
                &b.due,
                b.priority.is_empty(),
                &b.priority,
                b.note_id.to_string(),
                b.line,
            ))
    });
    let invalid_dates = indices
        .iter()
        .filter(|i| {
            [&tasks[**i].scheduled, &tasks[**i].due]
                .iter()
                .any(|date| !date.is_empty() && !dated(date))
        })
        .count();
    let days = week
        .days
        .iter()
        .map(|day| {
            indices
                .iter()
                .copied()
                .filter(|i| {
                    let task = &tasks[*i];
                    (dated(&task.scheduled) && task.scheduled == *day)
                        || (dated(&task.due) && task.due == *day)
                })
                .collect()
        })
        .collect();
    let overdue = if week.days.iter().any(|day| day == today) {
        indices
            .into_iter()
            .filter(|i| dated(&tasks[*i].due) && tasks[*i].due.as_str() < today)
            .collect()
    } else {
        vec![]
    };
    Ok(Projection {
        days,
        overdue,
        invalid_dates,
    })
}
#[cfg(test)]
mod tests {
    use super::*;
    fn task(id: i64, scheduled: &str, due: &str) -> live::DatedTask {
        serde_json::from_value(serde_json::json!({"note_id":id,"file_kind":"note","title":"Note","line":1,"state":"TODO","done":false,"text":"Task","scheduled":scheduled,"due":due})).unwrap()
    }
    #[test]
    fn civil_weeks_cross_year_leap_day_and_dst_without_seconds() {
        let week = Week::containing("2024-02-29", 4).unwrap();
        assert_eq!(week.days[0], "2024-02-25");
        assert_eq!(week.days[6], "2024-03-02");
        assert_eq!(week.shifted(true).unwrap().days[0], "2024-03-03");
        assert_eq!(week.shifted(true).unwrap().shifted(false).unwrap(), week);
        assert_eq!(
            Week::containing("2025-01-01", 3).unwrap().days[0],
            "2024-12-29"
        );
        assert_eq!(
            Week::containing("2024-03-10", 0).unwrap().days[6],
            "2024-03-16"
        );
        assert!(Week::containing("2025-02-29", 6).is_none());
        assert!(Week::containing("2025-01-01", 7).is_none());
    }
    #[test]
    fn scheduled_due_deduplicate_and_closed_undated_are_not_placed() {
        let week = Week::containing("2024-03-13", 3).unwrap();
        let mut tasks = vec![
            task(1, "2024-03-13", ""),
            task(2, "", "2024-03-13"),
            task(3, "2024-03-12", "2024-03-13"),
            task(4, "2024-03-13", "2024-03-13"),
            task(5, "", ""),
            task(6, "2024-03-13", ""),
        ];
        tasks[5].state = "CANCELLED".into();
        let result = project(&tasks, &week, "2024-03-13").unwrap();
        assert_eq!(result.days[3].len(), 4);
        assert_eq!(result.days[2], vec![2]);
        tasks[0].done = true;
        tasks[1].state = "DONE".into();
        tasks[3].state = "CUSTOM".into();
        assert_eq!(
            project(&tasks, &week, "2024-03-13").unwrap().days[3].len(),
            2
        );
    }
    #[test]
    fn overdue_is_only_current_week_and_may_also_occur_on_a_day() {
        let week = Week::containing("2024-03-13", 3).unwrap();
        let tasks = vec![task(1, "", "2024-03-11"), task(2, "", "2024-02-01")];
        let result = project(&tasks, &week, "2024-03-13").unwrap();
        assert_eq!(result.overdue, vec![1, 0]);
        assert_eq!(result.days[1], vec![0]);
        assert!(
            project(&tasks, &week.shifted(false).unwrap(), "2024-03-13")
                .unwrap()
                .overdue
                .is_empty()
        );
        assert!(
            project(&tasks, &week.shifted(true).unwrap(), "2024-03-13")
                .unwrap()
                .overdue
                .is_empty()
        );
    }
    #[test]
    fn stable_order_uses_due_priority_lexical_identity_and_line() {
        let week = Week::containing("2024-03-13", 3).unwrap();
        let mut tasks = vec![
            task(2, "2024-03-13", ""),
            task(2, "", "2024-03-13"),
            task(10, "", "2024-03-13"),
            task(3, "", "2024-03-13"),
        ];
        tasks[3].priority = "A".into();
        assert_eq!(
            project(&tasks, &week, "2024-03-13").unwrap().days[3],
            vec![3, 2, 1, 0]
        );
        tasks.push(task(20, "2024-02-31", ""));
        assert_eq!(
            project(&tasks, &week, "2024-03-13").unwrap().invalid_dates,
            1
        );
    }
}
