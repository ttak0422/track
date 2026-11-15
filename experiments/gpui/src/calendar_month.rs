//! Civil month layout only; no timezone conversion or persistence.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Month {
    pub year: i32,
    pub month: u32,
}
impl Month {
    pub fn containing(date: &str) -> Option<Self> {
        if !crate::live::calendar_date_valid(date) {
            return None;
        }
        let value = Self {
            year: date[..4].parse().ok()?,
            month: date[5..7].parse().ok()?,
        };
        (value.year >= 1 && value.year <= 9998).then_some(value)
    }
    pub fn first(&self) -> String {
        format!("{:04}-{:02}-01", self.year, self.month)
    }
    pub fn days(&self) -> Vec<String> {
        let prefix = format!("{:04}-{:02}-", self.year, self.month);
        (1..=31)
            .map(|day| format!("{prefix}{day:02}"))
            .filter(|date| crate::live::calendar_date_valid(date))
            .collect()
    }
    pub fn last(&self) -> String {
        self.days().last().unwrap().clone()
    }
    pub fn shifted(&self, forward: bool) -> Option<Self> {
        let index = self.year * 12 + self.month as i32 - 1 + if forward { 1 } else { -1 };
        let value = Self {
            year: index.div_euclid(12),
            month: index.rem_euclid(12) as u32 + 1,
        };
        (value.year >= 1 && value.year <= 9998).then_some(value)
    }
    pub fn leading(&self) -> usize {
        // Gregorian weekday; Sunday = 0. No duration/epoch arithmetic.
        let offsets = [0, 3, 2, 5, 0, 3, 5, 1, 4, 6, 2, 4];
        let year = self.year - if self.month < 3 { 1 } else { 0 };
        ((year + year / 4 - year / 100 + year / 400 + offsets[self.month as usize - 1] + 1) % 7)
            as usize
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn civil_month_bounds_alignment_and_year_steps() {
        let leap = Month::containing("2024-02-20").unwrap();
        assert_eq!(leap.days().len(), 29);
        assert_eq!(leap.leading(), 4);
        assert_eq!(leap.first(), "2024-02-01");
        assert_eq!(leap.last(), "2024-02-29");
        assert_eq!(Month::containing("2100-02-20").unwrap().days().len(), 28);
        assert_eq!(Month::containing("2026-11-15").unwrap().leading(), 0);
        assert_eq!(
            Month::containing("2024-12-01")
                .unwrap()
                .shifted(true)
                .unwrap()
                .first(),
            "2025-01-01"
        );
        assert_eq!(
            Month::containing("2025-01-01")
                .unwrap()
                .shifted(false)
                .unwrap()
                .first(),
            "2024-12-01"
        );
        assert!(
            Month::containing("0001-01-01")
                .unwrap()
                .shifted(false)
                .is_none()
        );
        assert!(
            Month::containing("9998-12-01")
                .unwrap()
                .shifted(true)
                .is_none()
        );
        assert!(Month::containing("2024-02-30").is_none());
    }
}
