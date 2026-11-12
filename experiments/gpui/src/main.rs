use gpui::{
    App, Application, Bounds, Context, KeyBinding, Menu, MenuItem, Window, WindowBounds,
    WindowOptions, actions, div, prelude::*, px, rgb, size, uniform_list,
};
mod backend;
mod markdown;
use backend::{Backend, Note, Snapshot};
actions!(native_workspace, [Quit]);

fn fixture(count: usize) -> Snapshot {
    Snapshot {
        notes: (0..count)
            .map(|i| Note {
                note_id: i as i64,
                title: format!("Field note {:05} · Native workspace", i + 1),
                tags: vec![format!("topic-{}", i % 12)],
                path: Default::default(),
                file_kind: "note".into(),
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
    backend: Option<Backend>,
    content: Vec<markdown::Block>,
    status: String,
    list_status: String,
    request: u64,
}

impl Workspace {
    fn load_notes(&mut self, cx: &mut Context<Self>) {
        let Some(backend) = self.backend.clone() else {
            return;
        };
        self.request += 1;
        let request = self.request;
        self.status = "Loading notes…".into();
        let job = cx
            .background_executor()
            .spawn(async move { backend.notes() });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                match result {
                    Ok(response) => {
                        this.snapshot = response.value;
                        this.list_status = response.warnings;
                        if let Some(state) = &this.snapshot.read_state {
                            this.list_status.push_str(&format!("\nRead state: partial={}, cached={}, checked={}, omitted={}, issues={:?}, extra={:?}", state.partial, state.cache_used, state.checked_at, state.omitted, state.issues, state.additional));
                        }
                        this.selected = None;
                        this.content.clear();
                        if !this.snapshot.notes.is_empty() {
                            this.select(0, cx);
                        }
                    }
                    Err(error) => this.status = error.to_string(),
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }

    fn select(&mut self, index: usize, cx: &mut Context<Self>) {
        self.selected = Some(index);
        self.request += 1;
        self.content.clear();
        let Some(backend) = self.backend.clone() else {
            cx.notify();
            return;
        };
        let id = self.snapshot.notes[index].note_id;
        let request = self.request;
        self.status = "Loading portable Markdown…".into();
        let job = cx
            .background_executor()
            .spawn(async move { backend.export_markdown(id) });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.request != request {
                    return;
                }
                match result {
                    Ok(response) => {
                        this.content = markdown::parse(&response.value);
                        this.status = response.warnings;
                    }
                    Err(error) => this.status = error.to_string(),
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
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
                    if partial { "WARNING: partial snapshot" } else if self.backend.is_some() { "read-only / manual refresh" } else { "read-only snapshot" }))))
            .child(div().flex().p_3().gap_3()
                .child(div().id("reload").cursor_pointer().text_color(rgb(0x7ec9b5)).child("Reload vault")
                    .on_click(cx.listener(|this, _, _, cx| this.load_notes(cx))))
                .child(div().id("status").max_h(px(80.)).overflow_y_scroll().text_sm().child(format!("{} {}", self.list_status, self.status))))
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
                                .on_click(cx.listener(move |this, _, _, cx| { this.select(i, cx); }))
                        }).collect()
                    })).h_full()))
                .child(div().flex_1().min_w_0().p_8().flex().flex_col().gap_4()
                    .child(div().text_sm().text_color(rgb(0x7ec9b5)).child("NOTE INSPECTOR"))
                    .child(div().text_2xl().child(note.as_ref().map(|n| n.title.clone()).unwrap_or("No notes in this snapshot".into())))
                    .child(div().text_color(rgb(0x9daebe)).child(note.as_ref().map(|n| format!("ID {}  ·  {}  ·  {}\n{}", n.note_id, n.file_kind, n.tags.join(" / "), n.path.display())).unwrap_or_default()))
                    .child(div().text_sm().text_color(rgb(0x9daebe)).child(
                        if self.backend.is_some() { "Portable Markdown · read only · not editable source" } else { "Snapshot mode includes metadata only. Use --vault and --track-bin to read note content." }))
                    .child(div().id(("content", self.request as usize)).flex_1().min_h_0().overflow_y_scroll().text_sm()
                        .child(markdown::render(&self.content)))))
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
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.first().is_some_and(|a| a == "--help") {
        println!(
            "track-gpui-poc [notes.json]\ntrack-gpui-poc --vault /absolute/vault --track-bin /absolute/track [--config /absolute/config] [--cache /absolute/cache]\nNo arguments: synthetic fixture. Vault mode is read-only; CLI may refresh derived caches."
        );
        return Ok(());
    }
    let (snapshot, source, backend) = if args.first().is_some_and(|a| a.starts_with("--")) {
        let mut vault = None;
        let mut executable = None;
        let mut config = None;
        let mut cache = None;
        let mut iter = args.iter();
        while let Some(flag) = iter.next() {
            let value = iter.next().ok_or("missing option value")?;
            match flag.as_str() {
                "--vault" => vault = Some(value.into()),
                "--track-bin" => executable = Some(value.into()),
                "--config" => config = Some(value.into()),
                "--cache" => cache = Some(value.into()),
                _ => return Err(format!("unknown option: {flag}").into()),
            }
        }
        let mut backend = Backend::new(
            executable.ok_or("--track-bin is required")?,
            vault.ok_or("--vault is required")?,
        )?;
        if let Some(config) = config {
            backend = backend.with_config(config)?;
        }
        if let Some(cache) = cache {
            backend = backend.with_cache(cache)?;
        }
        (fixture(0), "explicit vault / CLI".into(), Some(backend))
    } else if args.len() == 1 {
        (
            serde_json::from_reader(std::fs::File::open(&args[0])?)?,
            "imported JSON".into(),
            None,
        )
    } else if args.is_empty() {
        (fixture(10_000), "synthetic fixture".into(), None)
    } else {
        return Err("expected a single JSON path or explicit vault options".into());
    };
    Application::new().run(move |cx: &mut App| {
        cx.on_action(|_: &Quit, cx| cx.quit());
        cx.bind_keys([KeyBinding::new("cmd-q", Quit, None)]);
        cx.set_menus(vec![Menu {
            name: "Track Native".into(),
            items: vec![MenuItem::action("Quit", Quit)],
        }]);
        let bounds = Bounds::centered(None, size(px(1120.), px(720.)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                ..Default::default()
            },
            |_, cx| {
                cx.new(|cx| {
                    let mut workspace = Workspace {
                        selected: (!snapshot.notes.is_empty()).then_some(0),
                        snapshot,
                        rendered_rows: 0,
                        source,
                        backend,
                        content: Vec::new(),
                        status: String::new(),
                        list_status: String::new(),
                        request: 0,
                    };
                    workspace.load_notes(cx);
                    workspace
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
