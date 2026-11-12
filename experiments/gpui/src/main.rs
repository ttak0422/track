use gpui::{
    App, Application, Bounds, Context, Window, WindowBounds, WindowOptions, div, prelude::*, px,
    rgb, size, uniform_list,
};
use serde::Deserialize;

#[derive(Clone, Deserialize)]
struct Note {
    note_id: i64,
    title: String,
    #[serde(default)]
    tags: Vec<String>,
}

#[derive(Deserialize)]
struct Snapshot {
    notes: Vec<Note>,
    #[serde(default)]
    read_state: Option<ReadState>,
}

#[derive(Deserialize)]
struct ReadState {
    #[serde(default)]
    partial: bool,
}

fn fixture(count: usize) -> Snapshot {
    Snapshot {
        notes: (0..count)
            .map(|i| Note {
                note_id: i as i64,
                title: format!("Field note {:05} · Native workspace", i + 1),
                tags: vec![format!("topic-{}", i % 12)],
            })
            .collect(),
        read_state: None,
    }
}

struct Workspace {
    snapshot: Snapshot,
    selected: Option<usize>,
    rendered_rows: usize,
    source: String,
}

impl Render for Workspace {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let note = self
            .selected
            .and_then(|i| self.snapshot.notes.get(i))
            .cloned();
        let partial = self.snapshot.read_state.as_ref().is_some_and(|s| s.partial);
        div().size_full().flex().flex_col().bg(rgb(0x111820)).text_color(rgb(0xe7edf4))
            .child(div().p_5().border_b_1().border_color(rgb(0x30404e))
                .child(div().text_xl().child("TRACK / Native canvas"))
                .child(div().text_sm().text_color(rgb(0x9daebe)).child(format!(
                    "GPUI experiment · {} notes · {} · {}", self.snapshot.notes.len(), self.source,
                    if partial { "WARNING: partial snapshot" } else { "read-only snapshot" }))))
            .child(div().flex().flex_1().min_h_0()
                .child(div().w(px(360.)).h_full().border_r_1().border_color(rgb(0x30404e))
                    .child(uniform_list("notes", self.snapshot.notes.len(), cx.processor(|this, range: std::ops::Range<usize>, _, cx| {
                        this.rendered_rows = range.len();
                        range.map(|i| {
                            let note = &this.snapshot.notes[i];
                            div().id(i).h(px(64.)).overflow_hidden().p_3().cursor_pointer()
                                .bg(rgb(if this.selected == Some(i) { 0x254853 } else { 0x18222c }))
                                .border_b_1().border_color(rgb(0x27343f))
                                .child(div().text_sm().overflow_hidden().child(note.title.clone()))
                                .child(div().text_xs().text_color(rgb(0x7ec9b5)).child(note.tags.join(" / ")))
                                .on_click(cx.listener(move |this, _, _, cx| { this.selected = Some(i); cx.notify(); }))
                        }).collect()
                    })).h_full()))
                .child(div().flex_1().min_w_0().p_8().flex().flex_col().gap_4()
                    .child(div().text_sm().text_color(rgb(0x7ec9b5)).child("NOTE INSPECTOR"))
                    .child(div().text_2xl().child(note.as_ref().map(|n| n.title.clone()).unwrap_or("No notes in this snapshot".into())))
                    .child(div().text_color(rgb(0x9daebe)).child(note.as_ref().map(|n| format!("ID {}  ·  {}", n.note_id, n.tags.join(" / "))).unwrap_or_default()))
                    .child(div().mt_8().p_5().rounded_lg().bg(rgb(0x1b2b36))
                        .child("Native rendering, without a web view")
                        .child(div().mt_3().text_sm().text_color(rgb(0x9daebe)).child("Scroll the note list and select a row. GPUI creates only visible rows; the inspector updates without HTML, a JavaScript bridge, or a preview server.")))
                    .child(div().text_sm().text_color(rgb(0x9daebe)).child("This prototype deliberately does not edit notes or render Markdown. The existing Go CLI remains responsible for vault semantics."))))
            .child(div().p_3().text_xs().border_t_1().border_color(rgb(0x30404e))
                .child(format!("Last list layout: {} visible rows / {} total · Select to refresh this counter · Close window to quit", self.rendered_rows, self.snapshot.notes.len())))
    }
}

struct StderrLog;
impl log::Log for StderrLog {
    fn enabled(&self, metadata: &log::Metadata) -> bool {
        metadata.level() <= log::Level::Warn
    }
    fn log(&self, record: &log::Record) {
        if self.enabled(record.metadata()) {
            eprintln!("{}: {}", record.level(), record.args());
        }
    }
    fn flush(&self) {}
}
static LOGGER: StderrLog = StderrLog;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let _ = log::set_logger(&LOGGER);
    log::set_max_level(log::LevelFilter::Warn);
    let arg = std::env::args().nth(1);
    if arg.as_deref() == Some("--help") {
        println!(
            "track-gpui-poc [notes.json]\nNo argument: synthetic 10,000-note fixture. JSON: output of track notes. No vault writes."
        );
        return Ok(());
    }
    let (snapshot, source) = match arg {
        Some(path) => (
            serde_json::from_reader(std::fs::File::open(path)?)?,
            "imported JSON".to_string(),
        ),
        None => (fixture(10_000), "synthetic fixture".to_string()),
    };
    Application::new().run(move |cx: &mut App| {
        let bounds = Bounds::centered(None, size(px(1120.), px(720.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                ..Default::default()
            },
            |_, cx| {
                cx.new(|_| Workspace {
                    selected: (!snapshot.notes.is_empty()).then_some(0),
                    snapshot,
                    rendered_rows: 0,
                    source,
                })
            },
        )
        .expect("open GPUI window");
        cx.on_window_closed(|cx| {
            cx.quit();
        })
        .detach();
        cx.activate(true);
    });
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn accepts_cli_snapshot_and_partial_read() {
        let s: Snapshot = serde_json::from_str(r#"{"notes":[{"note_id":1785024017000,"title":"日本語","path":"/ignored","file_kind":"note"}],"read_state":{"partial":true,"issues":[]}}"#).unwrap();
        assert_eq!(s.notes[0].title, "日本語");
        assert!(s.notes[0].tags.is_empty());
        assert!(s.read_state.unwrap().partial);
    }
    #[test]
    fn empty_snapshot_is_valid() {
        assert!(
            serde_json::from_str::<Snapshot>(r#"{"notes":[]}"#)
                .unwrap()
                .notes
                .is_empty()
        );
    }
    #[test]
    fn invalid_snapshot_is_rejected() {
        assert!(serde_json::from_str::<Snapshot>(r#"{"notes":[{"title":"missing id"}]}"#).is_err());
    }
    #[test]
    fn fixture_has_unique_ids() {
        let s = fixture(10_000);
        assert_eq!(s.notes.len(), 10_000);
        assert_eq!(s.notes[9999].note_id, 9999);
    }
}
