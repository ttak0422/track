//! Read-only ordering of one verified, top-level rich task group.
//! Original block indices are values, never replaced by the display positions.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Field {
    Scheduled,
    Due,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Direction {
    Source,
    Ascending,
    Descending,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Order {
    pub field: Field,
    pub direction: Direction,
}
impl Order {
    pub fn next(previous: Option<Self>, field: Field) -> Self {
        let direction = match previous.filter(|p| p.field == field).map(|p| p.direction) {
            Some(Direction::Ascending) => Direction::Descending,
            Some(Direction::Descending) => Direction::Source,
            _ => Direction::Ascending,
        };
        Self { field, direction }
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Row {
    pub block: usize,
    pub line: u64,
    pub depth: usize,
    pub scheduled: String,
    pub due: String,
}
fn date(value: &str) -> bool {
    if value.is_empty() {
        return true;
    }
    let b = value.as_bytes();
    if b.len() != 10
        || b[4] != b'-'
        || b[7] != b'-'
        || b.iter()
            .enumerate()
            .any(|(i, c)| i != 4 && i != 7 && !c.is_ascii_digit())
    {
        return false;
    }
    let year = value[..4].parse::<u32>().unwrap();
    let month = value[5..7].parse::<u32>().unwrap();
    let day = value[8..].parse::<u32>().unwrap();
    let limit = match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            if year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) {
                29
            } else {
                28
            }
        }
        _ => 0,
    };
    year > 0 && day > 0 && day <= limit
}
/// None means unavailable, not permission to guess or reorder part of a list.
pub fn permutation(rows: &[Row], order: Order) -> Option<Vec<usize>> {
    if rows.is_empty()
        || rows.len() > 256
        || rows
            .iter()
            .any(|r| r.depth != 0 || r.line == 0 || !date(&r.scheduled) || !date(&r.due))
        || rows
            .windows(2)
            .any(|w| w[0].block.checked_add(1) != Some(w[1].block) || w[0].line >= w[1].line)
    {
        return None;
    }
    let mut indices: Vec<_> = (0..rows.len()).collect();
    if order.direction != Direction::Source {
        indices.sort_by(|&a, &b| {
            let value = |i: usize| match order.field {
                Field::Scheduled => rows[i].scheduled.as_str(),
                Field::Due => rows[i].due.as_str(),
            };
            let (a, b) = (value(a), value(b));
            match (a.is_empty(), b.is_empty()) {
                (true, false) => std::cmp::Ordering::Greater,
                (false, true) => std::cmp::Ordering::Less,
                (true, true) => std::cmp::Ordering::Equal,
                _ => {
                    if order.direction == Direction::Ascending {
                        a.cmp(b)
                    } else {
                        b.cmp(a)
                    }
                }
            }
        });
    }
    Some(indices.into_iter().map(|i| rows[i].block).collect())
}
#[cfg(test)]
mod tests {
    use super::*;
    fn rows() -> Vec<Row> {
        ["2026-03-02", "", "2026-03-01", "2026-03-02"]
            .iter()
            .enumerate()
            .map(|(i, d)| Row {
                block: 10 + i,
                line: 20 + i as u64,
                depth: 0,
                scheduled: d.to_string(),
                due: String::new(),
            })
            .collect()
    }
    #[test]
    fn stable_direction_cycle_and_original_indices() {
        let rows = rows();
        let a = Order::next(None, Field::Scheduled);
        let d = Order::next(Some(a), Field::Scheduled);
        let s = Order::next(Some(d), Field::Scheduled);
        assert_eq!(permutation(&rows, a), Some(vec![12, 10, 13, 11]));
        assert_eq!(permutation(&rows, d), Some(vec![10, 13, 12, 11]));
        assert_eq!(permutation(&rows, s), Some(vec![10, 11, 12, 13]));
        assert_eq!(
            Order::next(Some(d), Field::Due).direction,
            Direction::Ascending
        );
        assert_eq!(
            permutation(
                &rows,
                Order {
                    field: Field::Due,
                    direction: Direction::Descending
                }
            ),
            Some(vec![10, 11, 12, 13])
        );
    }
    #[test]
    fn nested_or_ambiguous_groups_are_never_partially_sorted() {
        let a = Order::next(None, Field::Due);
        let mut r = rows();
        r[1].depth = 1;
        assert!(permutation(&r, a).is_none());
        r[1].depth = 0;
        r[1].block += 1;
        assert!(permutation(&r, a).is_none());
        let mut r = rows();
        r[2].due = "2026-02-30".into();
        assert!(permutation(&r, a).is_none());
        assert!(date("2024-02-29"));
        assert!(!date("2025-02-29"));
    }
}
