//! Native one-sheet shell. Only backed actions are interactive.
use super::*;
use gpui::{Div, Focusable, PathBuilder, Stateful, WindowAppearance, canvas, point};

pub struct ShellState {
    pub home: bool,
    pub search_open: bool,
    pub dark: Option<bool>,
    pub diagnostics: bool,
    pub tab_menu: bool,
    reader_focus: Option<gpui::FocusHandle>,
    reader_scrolls: std::collections::HashMap<tabs::TabKey, gpui::ScrollHandle>,
    tab_cycle: Vec<tabs::TabKey>,
    tab_cycle_index: usize,
    heading_anchors:
        std::collections::HashMap<tabs::TabKey, (String, std::rc::Rc<markdown::HeadingAnchors>)>,
}

impl Default for ShellState {
    fn default() -> Self {
        Self {
            home: true,
            search_open: false,
            dark: None,
            diagnostics: false,
            tab_menu: false,
            reader_focus: None,
            reader_scrolls: Default::default(),
            tab_cycle: Vec::new(),
            tab_cycle_index: 0,
            heading_anchors: Default::default(),
        }
    }
}

#[derive(Clone, Copy)]
struct Palette {
    bg: u32,
    paper: u32,
    ink: u32,
    muted: u32,
    line: u32,
    mark: u32,
}
impl Palette {
    fn new(dark: bool) -> Self {
        if dark {
            Self {
                bg: 0x141618,
                paper: 0x191c1e,
                ink: 0xe9e9e4,
                muted: 0xa2a29b,
                line: 0x282c2f,
                mark: 0xf4785e,
            }
        } else {
            Self {
                bg: 0xfbfaf8,
                paper: 0xffffff,
                ink: 0x1a1a18,
                muted: 0x5e5d58,
                line: 0xe6e4de,
                mark: 0xc13a1e,
            }
        }
    }
}
fn rail_button(id: &'static str, glyph: &'static str, p: Palette, active: bool) -> Stateful<Div> {
    let color = if active { p.mark } else { p.muted };
    div()
        .id(id)
        .w(px(40.))
        .h(px(40.))
        .flex()
        .items_center()
        .justify_center()
        .rounded_md()
        .cursor_pointer()
        .child(
            canvas(
                |_, _, _| (),
                move |bounds, _, window, _| {
                    let mut path = PathBuilder::stroke(px(1.5));
                    let at = |x: f32, y: f32| {
                        point(
                            bounds.left() + px(x * 20. / 24.),
                            bounds.top() + px(y * 20. / 24.),
                        )
                    };
                    let segments: Vec<Vec<(f32, f32)>> = match glyph {
                        "home" => vec![
                            vec![(3., 11.), (12., 3.), (21., 11.)],
                            vec![
                                (5., 10.),
                                (5., 21.),
                                (10., 21.),
                                (10., 14.),
                                (14., 14.),
                                (14., 21.),
                                (19., 21.),
                                (19., 10.),
                            ],
                        ],
                        "search" => {
                            let mut ring: Vec<_> = (0..=32)
                                .map(|i| {
                                    let a = i as f32 * std::f32::consts::TAU / 32.;
                                    (10.5 + 6.5 * a.cos(), 10.5 + 6.5 * a.sin())
                                })
                                .collect();
                            ring.push((15.1, 15.1));
                            vec![ring, vec![(15.1, 15.1), (21., 21.)]]
                        }
                        "info" => vec![vec![(12., 5.), (12., 6.)], vec![(12., 10.), (12., 19.)]],
                        "reload" => {
                            let arc = (0..=24)
                                .map(|i| {
                                    let a = 0.6 + i as f32 * 5.0 / 24.;
                                    (12. + 8. * a.cos(), 12. + 8. * a.sin())
                                })
                                .collect();
                            vec![arc, vec![(21., 3.), (21., 9.), (15., 9.)]]
                        }
                        _ => {
                            let ring = (0..=32)
                                .map(|i| {
                                    let a = i as f32 * std::f32::consts::TAU / 32.;
                                    (12. + 8. * a.cos(), 12. + 8. * a.sin())
                                })
                                .collect();
                            vec![ring, vec![(12., 4.), (12., 20.)]]
                        }
                    };
                    for points in segments {
                        for (i, (x, y)) in points.into_iter().enumerate() {
                            if i == 0 {
                                path.move_to(at(x, y));
                            } else {
                                path.line_to(at(x, y));
                            }
                        }
                    }
                    if let Ok(path) = path.build() {
                        window.paint_path(path, rgb(color));
                    }
                },
            )
            .w(px(20.))
            .h(px(20.)),
        )
}

fn label(text: &'static str, p: Palette) -> Div {
    div()
        .font_family("monospace")
        .text_size(px(10.))
        .text_color(rgb(p.muted))
        .mb_3()
        .child(text)
}
fn display_date(seconds: i64) -> Option<String> {
    let seconds: libc::time_t = seconds.try_into().ok()?;
    let mut date: libc::tm = unsafe { std::mem::zeroed() };
    if unsafe { libc::localtime_r(&seconds, &mut date) }.is_null() {
        return None;
    }
    Some(format!(
        "{:04}-{:02}-{:02}",
        date.tm_year + 1900,
        date.tm_mon + 1,
        date.tm_mday
    ))
}
// A relation belongs to its source note's vault unless the server explicitly
// provides another vault. Null/non-string vaults and non-positive IDs are inert.
fn reference_key(value: &serde_json::Value, inherited_vault: &str) -> Option<live::NoteKey> {
    let note_id = value.get("note_id")?.as_i64().filter(|id| *id > 0)?;
    let vault = match value.get("vault") {
        None => inherited_vault,
        Some(value) => value.as_str()?,
    };
    Some(live::NoteKey {
        vault: vault.to_owned(),
        note_id,
    })
}
fn reference_row(
    value: &serde_json::Value,
    inherited_vault: Option<&str>,
    id: (&'static str, usize),
    cx: &mut Context<Workspace>,
) -> Option<Stateful<Div>> {
    let title = value.get("title")?.as_str()?.to_owned();
    let key = inherited_vault.and_then(|scope| reference_key(value, scope));
    Some(div().id(id).py_1().child(title).when_some(key, |row, key| {
        row.cursor_pointer()
            .on_click(cx.listener(move |this, _, _, cx| {
                this.shell.home = false;
                this.shell.search_open = false;
                this.shell.tab_menu = false;
                this.open_live_key(key.clone(), cx);
            }))
    }))
}
fn cycle_index(index: usize, len: usize, backwards: bool) -> usize {
    if len == 0 {
        return 0;
    }
    if backwards {
        (index + len - 1) % len
    } else {
        (index + 1) % len
    }
}
impl Workspace {
    fn activate_tab(&mut self, key: tabs::TabKey, cx: &mut Context<Self>) {
        self.shell.tab_cycle.clear();
        if !self.tabs.activate(&key) {
            return;
        }
        self.shell.tab_menu = false;
        self.shell.search_open = false;
        match key {
            tabs::TabKey::Home => self.open_home(cx),
            tabs::TabKey::Note { vault, note_id } => {
                self.shell.home = false;
                self.selected = None;
                self.content.clear();
                self.current_note = None;
                self.current_related.clear();
                if self.live.is_some() {
                    self.open_live_key(live::NoteKey { vault, note_id }, cx);
                } else if let Some(index) = self
                    .snapshot
                    .notes
                    .iter()
                    .position(|note| note.note_id == note_id)
                {
                    self.select(index, cx);
                }
            }
        }
        cx.notify();
    }
    fn close_tab(&mut self, key: &tabs::TabKey, cx: &mut Context<Self>) {
        self.shell.tab_cycle.clear();
        self.shell.reader_scrolls.remove(key);
        self.shell.heading_anchors.remove(key);
        let was_active = self.tabs.active().is_some_and(|tab| &tab.key == key);
        if !self.tabs.close(key) {
            return;
        }
        self.shell.tab_menu = false;
        if was_active {
            if let Some(next) = self.tabs.active().map(|tab| tab.key.clone()) {
                self.activate_tab(next, cx);
            } else {
                // Do not reopen a configured home or accept an older async response.
                self.show_empty_home();
            }
        }
        cx.notify();
    }
    pub fn configured_home_index(&self) -> Option<usize> {
        let id = self
            .live
            .as_ref()?
            .initial_storage
            .as_ref()?
            .home_note_id
            .as_ref()?
            .parse::<i64>()
            .ok()?;
        let active_vault = self
            .live
            .as_ref()?
            .initial_vaults
            .get("active")
            .and_then(|v| v.get("name"))
            .and_then(|v| v.as_str())
            .unwrap_or("");
        self.snapshot
            .notes
            .iter()
            .enumerate()
            .find(|(index, note)| {
                note.note_id == id
                    && self.live_notes.get(*index).is_some_and(|live_note| {
                        live_note.vault.is_empty() || live_note.vault == active_vault
                    })
            })
            .map(|(index, _)| index)
    }
    fn show_empty_home(&mut self) {
        self.request += 1;
        self.graph_request += 1;
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.shell.home = true;
        self.selected = None;
        self.content.clear();
        self.current_note = None;
        self.current_related.clear();
        self.show_graph = false;
    }
    fn open_home(&mut self, cx: &mut Context<Self>) {
        self.shell.tab_menu = false;
        self.shell.search_open = false;
        if let Some(index) = self.configured_home_index() {
            self.shell.home = false;
            self.select(index, cx);
        } else if self
            .live
            .as_ref()
            .and_then(|session| session.initial_storage.as_ref())
            .is_some_and(|storage| storage.home_note_id.is_some())
        {
            self.show_empty_home();
            self.load_live_notes(cx);
        } else {
            self.tabs.open_home();
            self.show_empty_home();
        }
        cx.notify();
    }
}
impl Render for Workspace {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let dark = self.shell.dark.unwrap_or(matches!(
            window.appearance(),
            WindowAppearance::Dark | WindowAppearance::VibrantDark
        ));
        let p = Palette::new(dark);
        let reference_vault = self
            .current_note
            .as_ref()
            .map(|body| body.note.vault.clone());
        let wide = window.viewport_size().width >= px(1100.);
        let height = window.viewport_size().height;
        let note = self
            .selected
            .and_then(|i| self.snapshot.notes.get(i))
            .cloned();
        let home = self.shell.home;
        if let Some(body) = &self.current_note {
            self.tabs.update_title(
                &tabs::TabKey::Note {
                    vault: body.note.vault.clone(),
                    note_id: body.note.note_id,
                },
                body.note.title.clone(),
            );
        }
        self.search.update(cx, |search, _| search.set_dark(dark));
        self.graph_view.update(cx, |graph, cx| {
            graph.set_dark(dark, cx);
            graph.set_compact(true, cx);
        });
        let title = self
            .current_note
            .as_ref()
            .map(|body| body.note.title.clone())
            .or_else(|| note.as_ref().map(|n| n.title.clone()))
            .or_else(|| self.tabs.active().map(|tab| tab.title.clone()))
            .unwrap_or_default();
        let reader_focus = self
            .shell
            .reader_focus
            .get_or_insert_with(|| cx.focus_handle())
            .clone();
        let reader_key = self
            .tabs
            .active()
            .map(|tab| tab.key.clone())
            .unwrap_or(tabs::TabKey::Home);
        let reader_scroll = self
            .shell
            .reader_scrolls
            .entry(reader_key.clone())
            .or_default()
            .clone();
        let content_ready = self.current_note.is_some() || !self.content.is_empty();
        let headings = markdown::outline(&self.content);
        let outline_signature = format!("{headings:?}");
        let anchor_entry = self
            .shell
            .heading_anchors
            .entry(reader_key.clone())
            .or_insert_with(|| {
                (
                    outline_signature.clone(),
                    std::rc::Rc::new(markdown::HeadingAnchors::new(&self.content, &reader_scroll)),
                )
            });
        if anchor_entry.0 != outline_signature {
            *anchor_entry = (
                outline_signature,
                std::rc::Rc::new(markdown::HeadingAnchors::new(&self.content, &reader_scroll)),
            );
        }
        let heading_anchors = anchor_entry.1.clone();
        let minimum_heading_level = headings.iter().map(|entry| entry.level).min().unwrap_or(1);
        let tab_layout = self
            .tabs
            .layout(f32::from(window.viewport_size().width).max(0.) as u32);
        let tab = div()
            .h(px(38.))
            .flex_shrink_0()
            .flex()
            .items_center()
            .border_b_1()
            .border_color(rgb(p.line))
            .children(
                tab_layout
                    .visible
                    .iter()
                    .enumerate()
                    .filter_map(|(index, key)| {
                        let entry = self.tabs.get(key)?;
                        let title = entry.title.clone();
                        let activate = key.clone();
                        let close = key.clone();
                        let middle_close = key.clone();
                        Some(
                            div()
                                .id(("tab", index))
                                .w(px(if index == 0 {
                                    tabs::ACTIVE_WIDTH
                                } else {
                                    tabs::INACTIVE_WIDTH
                                } as f32))
                                .min_w_0()
                                .flex_shrink_0()
                                .max_w(
                                    (window.viewport_size().width
                                        - px(if tab_layout.overflow_count() > 0 {
                                            tabs::OVERFLOW_WIDTH as f32
                                        } else {
                                            0.
                                        }))
                                    .max(px(0.)),
                                )
                                .h_full()
                                .px_3()
                                .flex()
                                .items_center()
                                .border_b_2()
                                .border_color(rgb(if index == 0 { p.mark } else { p.paper }))
                                .text_size(px(14.))
                                .font_weight(if index == 0 {
                                    gpui::FontWeight::MEDIUM
                                } else {
                                    gpui::FontWeight::NORMAL
                                })
                                .text_color(rgb(if index == 0 { p.ink } else { p.muted }))
                                .cursor_pointer()
                                .child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .overflow_hidden()
                                        .text_ellipsis()
                                        .child(title),
                                )
                                .child(
                                    div()
                                        .id(("close-tab", index))
                                        .w(px(24.))
                                        .h(px(24.))
                                        .flex_shrink_0()
                                        .flex()
                                        .items_center()
                                        .justify_center()
                                        .text_color(rgb(p.muted))
                                        .child("×")
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            cx.stop_propagation();
                                            this.close_tab(&close, cx);
                                        })),
                                )
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    this.activate_tab(activate.clone(), cx)
                                }))
                                .on_mouse_down(
                                    gpui::MouseButton::Middle,
                                    cx.listener(move |this, _, _, cx| {
                                        cx.stop_propagation();
                                        this.close_tab(&middle_close, cx);
                                    }),
                                ),
                        )
                    }),
            )
            .when(tab_layout.overflow_count() > 0, |bar| {
                bar.child(
                    div()
                        .id("tab-overflow")
                        .w(px(tabs::OVERFLOW_WIDTH as f32))
                        .h_full()
                        .flex_shrink_0()
                        .flex()
                        .items_center()
                        .justify_center()
                        .cursor_pointer()
                        .text_size(px(13.))
                        .child(format!("+{}", tab_layout.overflow_count()))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.tab_menu = !this.shell.tab_menu;
                            cx.notify();
                        })),
                )
            });
        let tab_menu = div()
            .id("tab-overflow-menu")
            .absolute()
            .right(px(8.))
            .top(px(40.))
            .w(px(300.))
            .max_h(px(400.))
            .overflow_y_scroll()
            .bg(rgb(p.paper))
            .border_1()
            .border_color(rgb(p.line))
            .rounded_lg()
            .py_2()
            .children(
                tab_layout
                    .overflow
                    .iter()
                    .enumerate()
                    .filter_map(|(index, key)| {
                        let title = self.tabs.get(key)?.title.clone();
                        let activate = key.clone();
                        Some(
                            div()
                                .id(("overflow-tab", index))
                                .px_4()
                                .py_2()
                                .text_size(px(14.))
                                .cursor_pointer()
                                .child(title)
                                .on_click(cx.listener(move |this, _, _, cx| {
                                    this.activate_tab(activate.clone(), cx)
                                })),
                        )
                    }),
            );
        let rail = div()
            .absolute()
            .left(px(8.))
            .top((height - px(250.)) / 2.)
            .w(px(50.))
            .py_2()
            .flex()
            .flex_col()
            .items_center()
            .gap_1()
            .bg(rgb(p.paper))
            .border_1()
            .border_color(rgb(p.line))
            .rounded_lg()
            .child(
                rail_button("home", "home", p, home).on_click(cx.listener(|this, _, _, cx| {
                    this.open_home(cx);
                })),
            )
            .child(
                rail_button("search", "search", p, self.shell.search_open).on_click(cx.listener(
                    |this, _, window, cx| {
                        this.shell.search_open = !this.shell.search_open;
                        if this.shell.search_open {
                            window.focus(&this.search.focus_handle(cx));
                        }
                        cx.notify();
                    },
                )),
            )
            .child(
                rail_button("reload", "reload", p, false)
                    .on_click(cx.listener(|this, _, _, cx| this.load_notes(cx))),
            )
            .child(div().w(px(24.)).h(px(1.)).my_1().bg(rgb(p.line)))
            .child(
                rail_button("theme", "theme", p, false).on_click(cx.listener(
                    move |this, _, _, cx| {
                        this.shell.dark = Some(!dark);
                        cx.notify();
                    },
                )),
            )
            .child(
                rail_button("diagnostics", "info", p, self.shell.diagnostics).on_click(
                    cx.listener(|this, _, _, cx| {
                        this.shell.diagnostics = !this.shell.diagnostics;
                        cx.notify();
                    }),
                ),
            );
        let aside =
            div()
                .id("context-rail")
                .overflow_y_scroll()
                .w(px(
                    (f32::from(window.viewport_size().width) * 0.24).clamp(240., 380.)
                ))
                .flex_shrink_0()
                .when(wide, |aside| aside.h_full())
                .pt(px(48.))
                .pr_6()
                .text_size(px(14.))
                .text_color(rgb(p.muted))
                .when(note.as_ref().is_some_and(|n| !n.tags.is_empty()), |aside| {
                    aside.child(div().mb_8().child(label("TAGS", p)).children(
                        note.as_ref().unwrap().tags.iter().map(|tag| {
                            div().mb_2().text_color(rgb(p.ink)).child(format!("#{tag}"))
                        }),
                    ))
                })
                .when(headings.len() > 1, |aside| {
                    aside.child(div().mb_8().child(label("CONTENTS", p)).children(
                        headings.into_iter().enumerate().map(|(index, heading)| {
                            let anchors = heading_anchors.clone();
                            let anchor = heading.anchor;
                            div()
                                .id(("outline", index))
                                .mb_2()
                                .pl(px((heading.level.saturating_sub(minimum_heading_level)
                                    as f32)
                                    * 28.))
                                .cursor_pointer()
                                .child(heading.title)
                                .on_click(cx.listener(move |this, _, window, cx| {
                                    if anchors.scroll_to(&anchor, window, cx) {
                                        cx.notify();
                                    }
                                    if let Some(focus) = &this.shell.reader_focus {
                                        window.focus(focus);
                                    }
                                }))
                        }),
                    ))
                });
        let aside = aside.when(
            self.current_related
                .get("children")
                .and_then(|v| v.as_array())
                .is_some_and(|children| !children.is_empty()),
            |aside| {
                aside.child(
                    div().mt_6().child(label("CHILDREN", p)).children(
                        self.current_related["children"]
                            .as_array()
                            .unwrap()
                            .iter()
                            .enumerate()
                            .filter_map(|(index, entry)| {
                                reference_row(
                                    entry,
                                    reference_vault.as_deref(),
                                    ("child-reference", index),
                                    cx,
                                )
                            }),
                    ),
                )
            },
        );
        let aside = aside.when(self.current_related.contains_key("backlinks"), |aside| {
            let links = self
                .current_related
                .get("backlinks")
                .and_then(|v| v.as_array())
                .cloned()
                .unwrap_or_default();
            aside.child(
                div()
                    .mt_6()
                    .child(label("BACKLINKS", p))
                    .when(links.is_empty(), |section| section.child("No backlinks."))
                    .children(links.iter().enumerate().filter_map(|(index, link)| {
                        reference_row(
                            link,
                            reference_vault.as_deref(),
                            ("backlink-reference", index),
                            cx,
                        )
                    })),
            )
        });
        let aside = aside.when(self.live.is_some() || self.backend.is_some(), |aside| {
            aside.child(
                div()
                    .mb_4()
                    .border_t_1()
                    .border_color(rgb(p.line))
                    .mt_6()
                    .pt_4()
                    .child(label("GRAPH", p))
                    .when(!self.show_graph, |graph| {
                        graph.child(
                            div()
                                .id("load-local-graph")
                                .cursor_pointer()
                                .text_color(rgb(p.mark))
                                .child("Show connections")
                                .on_click(cx.listener(|this, _, _, cx| this.load_graph(cx))),
                        )
                    })
                    .when(self.show_graph, |graph| {
                        graph.child(div().h(px(280.)).child(self.graph_view.clone()))
                    }),
            )
        });
        let mut aside = Some(aside);
        let metadata: Vec<(String, String)> = self
            .current_note
            .as_ref()
            .map(|body| {
                let mut fields: Vec<_> = body
                    .props
                    .as_array()
                    .into_iter()
                    .flatten()
                    .filter_map(|prop| {
                        let key = prop.get("key")?.as_str()?;
                        if key == "up" {
                            return None;
                        }
                        Some((key.to_uppercase(), prop.get("value")?.as_str()?.to_owned()))
                    })
                    .collect();
                if let Some(created) = body.note.additional.get("created").and_then(|v| v.as_str())
                {
                    fields.push(("CREATED".into(), created.into()));
                }
                if let Some(updated) = body
                    .note
                    .additional
                    .get("updated")
                    .and_then(|v| v.as_i64())
                    .and_then(display_date)
                {
                    fields.push(("UPDATED".into(), updated));
                }
                fields
            })
            .unwrap_or_default();
        let trail = self
            .current_related
            .get("trail")
            .and_then(|v| v.as_array())
            .cloned()
            .unwrap_or_default();
        let reader = div()
            .flex_1()
            .min_w_0()
            .h_full()
            .flex()
            .justify_center()
            .pl(px(64.))
            .pr(px(32.))
            .pt(px(32.))
            .child(
                div()
                    .id(gpui::SharedString::from(format!(
                        "note-reader:{reader_key:?}"
                    )))
                    .track_focus(&reader_focus)
                    .when(content_ready, |reader| reader.track_scroll(&reader_scroll))
                    .flex_1()
                    .max_w(px(880.))
                    .min_w_0()
                    .h_full()
                    .overflow_y_scroll()
                    .pb_12()
                    .when(!trail.is_empty(), |reader| {
                        reader.child(
                            div()
                                .text_size(px(13.))
                                .mb_4()
                                .flex()
                                .flex_wrap()
                                .gap_1()
                                .children(trail.iter().enumerate().filter_map(|(index, entry)| {
                                    reference_row(
                                        entry,
                                        reference_vault.as_deref(),
                                        ("breadcrumb-reference", index),
                                        cx,
                                    )
                                    .map(|row| {
                                        div().flex().items_center().gap_1().child(row).child("›")
                                    })
                                })),
                        )
                    })
                    .when(!metadata.is_empty(), |reader| {
                        reader.child(div().flex().gap(px(28.)).mb(px(36.)).children(
                            metadata.into_iter().map(|(key, value)| {
                                div()
                                    .child(
                                        div()
                                            .font_family("monospace")
                                            .text_size(px(11.))
                                            .text_color(rgb(p.muted))
                                            .child(key),
                                    )
                                    .child(
                                        div()
                                            .text_size(px(13.))
                                            .text_color(rgb(p.muted))
                                            .child(value),
                                    )
                            }),
                        ))
                    })
                    .when(
                        self.current_note.is_some()
                            && self
                                .current_note
                                .as_ref()
                                .is_some_and(|note| note.props.is_null())
                            && trail.is_empty(),
                        |reader| {
                            reader.child(
                                div()
                                    .text_size(px(11.))
                                    .text_color(rgb(p.muted))
                                    .mb_6()
                                    .child("Additional metadata is unavailable in this response."),
                            )
                        },
                    )
                    .child(
                        div()
                            .max_w(px(656.))
                            .pl(px(16.))
                            .text_size(px(16.))
                            .line_height(px(29.6))
                            .child(
                                div()
                                    .text_size(px(26.))
                                    .line_height(px(36.4))
                                    .font_weight(gpui::FontWeight::MEDIUM)
                                    .mb(px(6.))
                                    .child(title),
                            )
                            .child(markdown::render_themed_with_anchors(
                                &self.content,
                                dark,
                                &heading_anchors,
                            ))
                            .on_mouse_down(
                                gpui::MouseButton::Left,
                                cx.listener(|this, _, window, _| {
                                    if let Some(focus) = &this.shell.reader_focus {
                                        window.focus(focus);
                                    }
                                }),
                            ),
                    )
                    .when(!wide, |body| body.child(aside.take().unwrap())),
            );
        let home_view = div()
            .flex_1()
            .flex()
            .flex_col()
            .items_center()
            .justify_center()
            .pl(px(70.))
            .pb(px(100.))
            .child(
                div()
                    .w(px(520.))
                    .max_w_full()
                    .text_size(px(11.))
                    .text_color(rgb(p.muted))
                    .mb_8()
                    .child("Activity history is not available in this preview."),
            )
            .when(!self.shell.search_open, |home| {
                home.child(
                    div()
                        .id("home-search")
                        .w(px(520.))
                        .max_w_full()
                        .child(self.search.clone())
                        .on_mouse_down(
                            gpui::MouseButton::Left,
                            cx.listener(|this, _, _, cx| {
                                this.shell.search_open = true;
                                cx.notify();
                            }),
                        ),
                )
            });
        let query = self.search.read(cx).query().to_lowercase();
        let result_indices: Vec<usize> = if query.trim().is_empty() {
            vec![]
        } else if self.live.is_some() {
            (0..self.search_results.len()).collect()
        } else {
            self.snapshot
                .notes
                .iter()
                .enumerate()
                .filter(|(_, note)| {
                    note.title.to_lowercase().contains(&query)
                        || note
                            .tags
                            .iter()
                            .any(|tag| tag.to_lowercase().contains(&query))
                })
                .map(|(i, _)| i)
                .collect()
        };
        let search_panel = div()
            .absolute()
            .left(
                (window.viewport_size().width
                    - px(640.).min(window.viewport_size().width - px(48.)))
                    / 2.,
            )
            .top(height * 0.12)
            .h(if query.trim().is_empty() {
                px(72.)
            } else {
                (px(74.) + px(result_indices.len().max(1) as f32 * 64.)).min(height * 0.70)
            })
            .w(px(640.).min(window.viewport_size().width - px(48.)))
            .bg(rgb(p.paper))
            .border_1()
            .border_color(rgb(p.line))
            .rounded_lg()
            .flex()
            .flex_col()
            .overflow_hidden()
            .child(div().px_4().py_3().child(self.search.clone()))
            .child(
                uniform_list(
                    "notes",
                    result_indices.len(),
                    cx.processor(move |this, range: std::ops::Range<usize>, _, cx| {
                        this.rendered_rows = range.len();
                        range
                            .filter_map(|row| {
                                let i = *result_indices.get(row)?;
                                let (title, tags) = if this.live.is_some() {
                                    let note = this.search_results.get(i)?;
                                    (note.title.clone(), note.tags.join(" · "))
                                } else {
                                    let note = this.snapshot.notes.get(i)?;
                                    (note.title.clone(), note.tags.join(" · "))
                                };
                                Some(
                                    div()
                                        .id(i)
                                        .h(px(64.))
                                        .px_5()
                                        .py_3()
                                        .overflow_hidden()
                                        .cursor_pointer()
                                        .border_b_1()
                                        .border_color(rgb(p.line))
                                        .text_color(rgb(p.ink))
                                        .child(div().text_size(px(13.)).child(title))
                                        .child(
                                            div()
                                                .text_size(px(10.))
                                                .text_color(rgb(p.muted))
                                                .child(tags),
                                        )
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            this.shell.home = false;
                                            this.shell.search_open = false;
                                            this.search.update(cx, |search, cx| search.clear(cx));
                                            if this.live.is_some() {
                                                this.select_search_result(i, cx);
                                            } else {
                                                this.select(i, cx);
                                            }
                                        })),
                                )
                            })
                            .collect()
                    }),
                )
                .flex_1()
                .min_h_0(),
            );
        div()
            .id("workspace-shell")
            .on_mouse_down(
                gpui::MouseButton::Left,
                cx.listener(|this, _, _, _| this.shell.tab_cycle.clear()),
            )
            .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                if event.keystroke.key == "escape" {
                    if this.shell.search_open {
                        this.shell.search_open = false;
                        if let Some(focus) = &this.shell.reader_focus {
                            window.focus(focus);
                        }
                        cx.notify();
                        cx.stop_propagation();
                        return;
                    }
                    if this.shell.tab_menu {
                        this.shell.tab_menu = false;
                        cx.notify();
                        cx.stop_propagation();
                        return;
                    }
                    if this
                        .shell
                        .reader_focus
                        .as_ref()
                        .is_some_and(|focus| focus.is_focused(window))
                    {
                        window.blur();
                        cx.stop_propagation();
                    }
                    return;
                }
                if this.shell.search_open
                    || this.shell.tab_menu
                    || !this
                        .shell
                        .reader_focus
                        .as_ref()
                        .is_some_and(|focus| focus.is_focused(window))
                {
                    return;
                }
                let modifiers = event.keystroke.modifiers;
                if event.keystroke.key == "w"
                    && modifiers.platform
                    && !modifiers.control
                    && !modifiers.alt
                    && !modifiers.shift
                {
                    if let Some(key) = this.tabs.active().map(|tab| tab.key.clone()) {
                        this.close_tab(&key, cx);
                        cx.stop_propagation();
                    }
                } else if event.keystroke.key == "tab"
                    && !modifiers.platform
                    && !modifiers.control
                    && !modifiers.alt
                {
                    if this.tabs.entries().len() > 1 {
                        let mut order = std::mem::take(&mut this.shell.tab_cycle);
                        if order.len() != this.tabs.entries().len()
                            || !order.iter().all(|key| this.tabs.get(key).is_some())
                        {
                            order = this
                                .tabs
                                .entries()
                                .iter()
                                .map(|tab| tab.key.clone())
                                .collect();
                            this.shell.tab_cycle_index = 0;
                        }
                        let index =
                            cycle_index(this.shell.tab_cycle_index, order.len(), modifiers.shift);
                        this.activate_tab(order[index].clone(), cx);
                        this.shell.tab_cycle = order;
                        this.shell.tab_cycle_index = index;
                        cx.stop_propagation();
                    }
                }
            }))
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .bg(rgb(p.paper))
            .text_color(rgb(p.ink))
            .font_family(".SystemUIFont")
            .child(tab)
            .when(
                self.partial_read
                    || self
                        .snapshot
                        .read_state
                        .as_ref()
                        .is_some_and(|state| state.partial),
                |root| {
                    root.child(
                        div()
                            .pl(px(64.))
                            .py_1()
                            .text_size(px(11.))
                            .text_color(rgb(p.mark))
                            .child("Some notes could not be read. Open diagnostics for details."),
                    )
                },
            )
            .when(self.read_error.is_some(), |root| {
                root.child(
                    div()
                        .pl(px(64.))
                        .py_1()
                        .text_size(px(11.))
                        .text_color(rgb(p.mark))
                        .child("A read failed. Open diagnostics for details."),
                )
            })
            .when(home, |root| root.child(home_view))
            .when(!home, |root| {
                root.child(
                    div()
                        .flex()
                        .flex_1()
                        .min_h_0()
                        .child(reader)
                        .when(wide, |row| row.child(aside.take().unwrap())),
                )
            })
            .child(rail)
            .when(self.shell.tab_menu, |root| {
                root.child(
                    div()
                        .id("tab-menu-backdrop")
                        .absolute()
                        .size_full()
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.tab_menu = false;
                            cx.notify();
                        })),
                )
                .child(tab_menu)
            })
            .when(self.shell.diagnostics, |root| {
                root.child(
                    div()
                        .id("diagnostics-drawer")
                        .absolute()
                        .right(px(16.))
                        .top(px(48.))
                        .w(px(420.))
                        .max_w_full()
                        .h(px(240.))
                        .overflow_y_scroll()
                        .p_4()
                        .bg(rgb(p.paper))
                        .border_1()
                        .border_color(rgb(p.line))
                        .rounded_lg()
                        .text_size(px(11.))
                        .text_color(rgb(p.muted))
                        .child(format!(
                            "{}\n{}\n{}\n{}",
                            self.source,
                            self.list_status,
                            self.status,
                            self.read_error.as_deref().unwrap_or("")
                        )),
                )
            })
            .when(self.shell.search_open, |root| {
                root.child(
                    div()
                        .id("search-backdrop")
                        .absolute()
                        .size_full()
                        .bg(gpui::rgba(0x00000047))
                        .on_click(cx.listener(|this, _, _, cx| {
                            this.shell.search_open = false;
                            cx.notify();
                        })),
                )
                .child(search_panel)
            })
    }
}

#[cfg(test)]
mod reference_tests {
    use super::reference_key;
    use serde_json::json;

    #[test]
    fn missing_vault_inherits_source_not_search_scope() {
        let key = reference_key(&json!({"note_id":42}), "source-vault").unwrap();
        assert_eq!(key.vault, "source-vault");
        assert_eq!(key.note_id, 42);
    }
    #[test]
    fn explicit_vault_preserves_same_id_distinction() {
        let a = reference_key(&json!({"note_id":42,"vault":"one"}), "source").unwrap();
        let b = reference_key(&json!({"note_id":42,"vault":"two"}), "source").unwrap();
        assert_ne!(a, b);
        assert_eq!(a.note_id, b.note_id);
    }
    #[test]
    fn explicit_empty_vault_means_launch_vault() {
        assert_eq!(
            reference_key(&json!({"note_id":42,"vault":""}), "source")
                .unwrap()
                .vault,
            ""
        );
    }
    #[test]
    fn malformed_and_nonpositive_references_are_inert() {
        for value in [
            json!({}),
            json!({"note_id":0}),
            json!({"note_id":-1}),
            json!({"note_id":"42"}),
            json!({"note_id":42.5}),
            json!({"note_id":42,"vault":null}),
            json!({"note_id":42,"vault":7}),
            json!({"note_id":18446744073709551615u64}),
        ] {
            assert!(reference_key(&value, "source").is_none(), "{value}");
        }
    }
}

#[cfg(test)]
mod reader_navigation_tests {
    use super::*;
    #[test]
    fn tab_cycle_wraps_both_directions() {
        assert_eq!(cycle_index(0, 3, false), 1);
        assert_eq!(cycle_index(2, 3, false), 0);
        assert_eq!(cycle_index(0, 3, true), 2);
        assert_eq!(cycle_index(0, 0, true), 0);
    }
    #[test]
    fn reader_offsets_are_isolated_by_vault_and_survive_unbound_loading() {
        let mut shell = ShellState::default();
        let a = tabs::TabKey::Note {
            vault: "a".into(),
            note_id: 42,
        };
        let b = tabs::TabKey::Note {
            vault: "b".into(),
            note_id: 42,
        };
        shell
            .reader_scrolls
            .entry(a.clone())
            .or_default()
            .set_offset(point(px(0.), px(-200.)));
        shell
            .reader_scrolls
            .entry(b.clone())
            .or_default()
            .set_offset(point(px(0.), px(-80.)));
        assert_eq!(shell.reader_scrolls[&a].offset().y, px(-200.));
        assert_eq!(shell.reader_scrolls[&b].offset().y, px(-80.));
        shell.reader_scrolls.remove(&a);
        assert_eq!(shell.reader_scrolls[&b].offset().y, px(-80.));
    }
}
