//! Focused plain-reader scroll arithmetic. No input, navigation or persistence.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Direction {
    Down,
    Up,
}
pub fn direction(
    key: &str,
    shift: bool,
    control: bool,
    alt: bool,
    platform: bool,
    function: bool,
) -> Option<Direction> {
    if shift || control || alt || platform || function {
        return None;
    }
    match key {
        "j" => Some(Direction::Down),
        "k" => Some(Direction::Up),
        _ => None,
    }
}
pub fn offset(
    current: f32,
    maximum: f32,
    line_height: Option<f32>,
    direction: Direction,
) -> Option<f32> {
    if !current.is_finite() || !maximum.is_finite() || maximum < 0. {
        return None;
    }
    let line = line_height
        .filter(|v| v.is_finite() && *v > 0. && *v <= 1024.)
        .unwrap_or(24.);
    let step = line * 3.;
    Some(
        (current
            + if direction == Direction::Down {
                -step
            } else {
                step
            })
        .clamp(-maximum, 0.),
    )
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn only_unmodified_lowercase_commands_are_owned() {
        assert_eq!(
            direction("j", false, false, false, false, false),
            Some(Direction::Down)
        );
        assert_eq!(
            direction("k", false, false, false, false, false),
            Some(Direction::Up)
        );
        for key in ["J", "K", "x", "escape", "down"] {
            assert!(direction(key, false, false, false, false, false).is_none());
        }
        for index in 0..5 {
            let mut m = [false; 5];
            m[index] = true;
            assert!(direction("j", m[0], m[1], m[2], m[3], m[4]).is_none());
        }
    }
    #[test]
    fn three_body_lines_repeat_synchronously_and_clamp_at_both_edges() {
        let first = offset(0., 200., Some(29.6), Direction::Down).unwrap();
        assert!((first + 88.8).abs() < 0.001);
        let second = offset(first, 200., Some(29.6), Direction::Down).unwrap();
        assert!((second + 177.6).abs() < 0.001);
        assert_eq!(
            offset(second, 200., Some(29.6), Direction::Down),
            Some(-200.)
        );
        assert_eq!(offset(-10., 200., Some(29.6), Direction::Up), Some(0.));
        assert_eq!(offset(0., 0., None, Direction::Down), Some(0.));
    }
    #[test]
    fn unknown_line_height_uses_72px_and_bad_geometry_is_rejected() {
        for line in [
            None,
            Some(0.),
            Some(f32::NAN),
            Some(f32::INFINITY),
            Some(-1.),
        ] {
            assert_eq!(offset(0., 500., line, Direction::Down), Some(-72.));
        }
        assert!(offset(f32::NAN, 100., None, Direction::Down).is_none());
        assert!(offset(0., f32::INFINITY, None, Direction::Down).is_none());
        assert!(offset(0., -1., None, Direction::Down).is_none());
    }
}
