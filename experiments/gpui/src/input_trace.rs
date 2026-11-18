//! Opt-in disposable-fixture diagnostics. Never records text buffers or printable
//! typing. Platform input may swallow events before GPUI; absence from this log
//! is not proof that the operating system delivered no key.
use std::sync::atomic::{AtomicBool, Ordering};
static ENABLED: AtomicBool = AtomicBool::new(false);

fn trace_allowed(fixture: bool, value: Option<&str>) -> bool {
    fixture && value == Some("1")
}
pub fn initialize(fixture: bool) -> bool {
    let value = std::env::var("TRACK_NATIVE_INPUT_TRACE").ok();
    let enabled = trace_allowed(fixture, value.as_deref());
    ENABLED.store(enabled, Ordering::Relaxed);
    if enabled {
        eprintln!("track-input trace enabled for developer fixture; printable typing redacted");
    }
    enabled
}
fn safe_key_name(key: &gpui::Keystroke) -> &'static str {
    match key.key.as_str() {
        "backspace" => "backspace",
        "delete" => "delete",
        "left" => "left",
        "right" => "right",
        "up" => "up",
        "down" => "down",
        "home" => "home",
        "end" => "end",
        "escape" => "escape",
        "enter" => "enter",
        "tab" => "tab",
        "pageup" => "pageup",
        "pagedown" => "pagedown",
        name if key.modifiers.platform || key.modifiers.control => match name {
            "a" => "a",
            "c" => "c",
            "v" => "v",
            "x" => "x",
            "z" => "z",
            "q" => "q",
            "w" => "w",
            "s" => "s",
            "m" => "m",
            "f" => "f",
            _ => "other-shortcut",
        },
        _ => "printable-or-other-redacted",
    }
}
pub fn key(event: &gpui::KeystrokeEvent) {
    if !ENABLED.load(Ordering::Relaxed) {
        return;
    }
    let stroke = &event.keystroke;
    eprintln!(
        "track-input key={} cmd={} ctrl={} alt={} shift={} fn={} action_matched={}",
        safe_key_name(stroke),
        stroke.modifiers.platform,
        stroke.modifiers.control,
        stroke.modifiers.alt,
        stroke.modifiers.shift,
        stroke.modifiers.function,
        event.action.is_some()
    );
}
fn unusual_codepoints(text: &str) -> Vec<u32> {
    // Restrict output to control/private-use/suspect-symbol codepoints. Never
    // emit surrounding characters, length, cursor offset, source, or text snippets.
    text.chars().filter(|c| matches!(*c,
        '\u{0}'..='\u{8}' | '\u{b}'..='\u{1f}' | '\u{7f}'..='\u{9f}' |
        '\u{e000}'..='\u{f8ff}' | '\u{f0000}'..='\u{ffffd}' | '\u{100000}'..='\u{10fffd}' | '\u{25bd}'
    )).map(|c|c as u32).take(16).collect()
}
pub fn platform_text(text: &str, marked: bool) {
    if !ENABLED.load(Ordering::Relaxed) {
        return;
    }
    let unusual = unusual_codepoints(text);
    if !unusual.is_empty() {
        let codes: Vec<_> = unusual.iter().map(|c| format!("U+{c:04X}")).collect();
        eprintln!(
            "track-input platform_text marked={marked} unusual={}",
            codes.join(",")
        );
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn trace_needs_both_fixture_and_exact_environment_opt_in() {
        assert!(trace_allowed(true, Some("1")));
        assert!(!trace_allowed(false, Some("1")));
        for value in [None, Some("true"), Some("0"), Some("")] {
            assert!(!trace_allowed(true, value));
        }
    }
    #[test]
    fn trace_never_includes_printable_note_text() {
        assert!(unusual_codepoints("private 日本語 😀\n\t").is_empty());
        assert_eq!(
            unusual_codepoints("private\u{7f}secret▽\u{f728}"),
            vec![0x7f, 0x25bd, 0xf728]
        );
        assert_eq!(
            safe_key_name(&gpui::Keystroke::parse("a").unwrap()),
            "printable-or-other-redacted"
        );
        assert_eq!(
            safe_key_name(&gpui::Keystroke::parse("日本語").unwrap()),
            "printable-or-other-redacted"
        );
        assert_eq!(
            safe_key_name(&gpui::Keystroke::parse("cmd-a").unwrap()),
            "a"
        );
        assert_eq!(
            safe_key_name(&gpui::Keystroke::parse("cmd-q").unwrap()),
            "q"
        );
        assert_eq!(
            safe_key_name(&gpui::Keystroke::parse("backspace").unwrap()),
            "backspace"
        );
    }
}
