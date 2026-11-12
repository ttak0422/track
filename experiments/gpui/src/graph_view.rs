//! Native GPUI canvas. Circular fallback followed by a bounded background force snapshot.
//! This is independent initial relaxation, not continuous physics or compatibility parity.
//! Dragging directly repositions a node and its attached edges; neighbors do not elastically
//! rearrange. Positions last only until graph data is replaced. Leaving the canvas ends a drag.
use crate::graph::{self, Graph, LayoutNode, Point, Viewport};
use gpui::{
    Bounds, Context, MouseButton, PathBuilder, Pixels, Window, canvas, div, fill, point,
    prelude::*, px, rgb, size,
};
use std::collections::BTreeMap;

#[derive(Clone, Copy, Debug)]
pub struct OpenNote(pub i64);

pub struct GraphView {
    graph: Graph,
    nodes: Vec<LayoutNode>,
    viewport: Viewport,
    origin: Point,
    selected: Option<i64>,
    hovered: Option<i64>,
    pan_from: Option<Point>,
    node_drag: Option<(i64, Point)>,
    compact: bool,
    dark: bool,
    layout_generation: u64,
    layout_status: &'static str,
}
impl gpui::EventEmitter<OpenNote> for GraphView {}

impl GraphView {
    pub fn new(graph: Graph) -> Self {
        let nodes = graph::circular_layout(&graph);
        Self {
            graph,
            nodes,
            viewport: Viewport::new(0., 0.),
            origin: Point::default(),
            selected: None,
            hovered: None,
            pan_from: None,
            node_drag: None,
            compact: false,
            dark: false,
            layout_generation: 0,
            layout_status: "circular initial layout",
        }
    }
    pub fn set_graph(&mut self, graph: Graph, cx: &mut Context<Self>) {
        let generation = self.layout_generation.wrapping_add(1);
        let dark = self.dark;
        let compact = self.compact;
        *self = Self::new(graph);
        self.dark = dark;
        self.compact = compact;
        self.layout_generation = generation;
        // Keep oversized local graphs intact; do not silently apply overview filtering.
        if self.nodes.len() > 1000 {
            self.layout_status = "circular fallback · force limit 1,000 nodes";
            cx.notify();
            return;
        }
        self.layout_status = "settling in background";
        let graph = self.graph.clone();
        let job = cx.background_executor().spawn(async move {
            graph::force_layout(&graph, graph::ForceOptions::default(), &BTreeMap::new())
        });
        cx.spawn(async move |this, cx| {
            let nodes = job.await;
            let _ = this.update(cx, |this, cx| {
                // Graph replacement or manual node movement makes an old result stale.
                if this.layout_generation != generation {
                    return;
                }
                this.nodes = nodes;
                this.layout_status = "force snapshot · drag has no elastic physics";
                this.hovered = None;
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    pub fn set_compact(&mut self, compact: bool, cx: &mut Context<Self>) {
        if self.compact != compact {
            self.compact = compact;
            cx.notify();
        }
    }
    pub fn set_dark(&mut self, dark: bool, cx: &mut Context<Self>) {
        if self.dark != dark {
            self.dark = dark;
            cx.notify();
        }
    }
    pub fn selected(&self) -> Option<i64> {
        self.selected
    }
    fn local(&self, position: gpui::Point<Pixels>) -> Point {
        Point {
            x: f32::from(position.x) as f64 - self.origin.x,
            y: f32::from(position.y) as f64 - self.origin.y,
        }
    }
}
impl Render for GraphView {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let (panel, ink, muted, line, outline, mark) = if self.dark {
            (0x191c1e, 0xe9e9e4, 0xa2a29b, 0x282c2f, 0x6e7478, 0xf4785e)
        } else {
            (0xffffff, 0x1a1a18, 0x5e5d58, 0xe6e4de, 0x8e8c84, 0xc13a1e)
        };
        let compact = self.compact;
        let labels: Vec<_> = if compact {
            self.nodes
                .iter()
                .filter(|n| {
                    n.node.note_id == self.graph.center_id
                        || n.node.center
                        || Some(n.node.note_id) == self.hovered
                        || Some(n.node.note_id) == self.selected
                })
                .map(|n| {
                    (
                        n.node.note_id,
                        n.node.title.clone(),
                        self.viewport.world_to_screen(n.position),
                        graph::node_radius(&n.node, self.graph.center_id, n.degree),
                    )
                })
                .collect()
        } else {
            Vec::new()
        };
        let entity = cx.entity();
        let nodes = self.nodes.clone();
        let edges = self.graph.edges.clone();
        let center_id = self.graph.center_id;
        let highlighted = self
            .hovered
            .or(self.selected)
            .map(|id| graph::neighbors(&self.graph, id));
        let selected = self.selected;
        let hovered = self.hovered;
        let mut viewport = self.viewport;
        let detail = self
            .hovered
            .or(self.selected)
            .and_then(|id| self.graph.nodes.iter().find(|node| node.note_id == id))
            .cloned();
        div()
            .id("native-graph")
            .relative()
            .size_full()
            .overflow_hidden()
            .bg(rgb(panel))
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|this, event: &gpui::MouseDownEvent, _, cx| {
                    let p = this.local(event.position);
                    if let Some(id) =
                        graph::hit_test(&this.nodes, this.graph.center_id, &this.viewport, p)
                    {
                        this.selected = Some(id);
                        this.pan_from = None;
                        this.node_drag = Some((id, p));
                        if event.click_count == 2 {
                            this.node_drag = None;
                            cx.emit(OpenNote(id));
                        }
                    } else {
                        this.selected = None;
                        this.node_drag = None;
                        this.pan_from = Some(p);
                    }
                    cx.notify();
                }),
            )
            .on_mouse_move(cx.listener(|this, event: &gpui::MouseMoveEvent, _, cx| {
                let p = this.local(event.position);
                if event.dragging() {
                    if let Some((id, previous)) = this.node_drag {
                        this.layout_generation = this.layout_generation.wrapping_add(1);
                        this.layout_status = "manual positions · no elastic physics";
                        graph::drag_node(
                            &mut this.nodes,
                            id,
                            &this.viewport,
                            Point {
                                x: p.x - previous.x,
                                y: p.y - previous.y,
                            },
                        );
                        this.node_drag = Some((id, p));
                    }
                    if let Some(previous) = this.pan_from {
                        this.viewport.pan(Point {
                            x: p.x - previous.x,
                            y: p.y - previous.y,
                        });
                        this.pan_from = Some(p);
                    }
                } else {
                    this.node_drag = None;
                    this.pan_from = None;
                }
                this.hovered =
                    graph::hit_test(&this.nodes, this.graph.center_id, &this.viewport, p);
                cx.notify();
            }))
            .on_mouse_up(
                MouseButton::Left,
                cx.listener(|this, _, _, _| {
                    this.pan_from = None;
                    this.node_drag = None;
                }),
            )
            .on_mouse_up_out(
                MouseButton::Left,
                cx.listener(|this, _, _, _| {
                    this.pan_from = None;
                    this.node_drag = None;
                }),
            )
            .on_hover(cx.listener(|this, inside, _, cx| {
                if !inside {
                    this.hovered = None;
                    this.node_drag = None;
                    this.pan_from = None;
                    cx.notify();
                }
            }))
            .on_scroll_wheel(cx.listener(|this, event: &gpui::ScrollWheelEvent, _, cx| {
                let delta = event.delta.pixel_delta(px(20.));
                if event.modifiers.control || event.modifiers.shift {
                    this.viewport.zoom_at(
                        this.local(event.position),
                        (f32::from(delta.y) as f64 * 0.005).exp(),
                    );
                } else {
                    this.viewport.pan(Point {
                        x: f32::from(delta.x) as f64,
                        y: f32::from(delta.y) as f64,
                    });
                }
                cx.stop_propagation();
                cx.notify();
            }))
            .child(
                canvas(
                    move |bounds, _, cx| {
                        viewport.width = f32::from(bounds.size.width) as f64;
                        viewport.height = f32::from(bounds.size.height) as f64;
                        entity.update(cx, |this, _| {
                            this.viewport.width = viewport.width;
                            this.viewport.height = viewport.height;
                            this.origin = Point {
                                x: f32::from(bounds.origin.x) as f64,
                                y: f32::from(bounds.origin.y) as f64,
                            };
                        });
                        viewport
                    },
                    move |bounds, viewport, window, _| {
                        let positions: BTreeMap<_, _> = nodes
                            .iter()
                            .map(|n| (n.node.note_id, viewport.world_to_screen(n.position)))
                            .collect();
                        let screen = |p: Point| {
                            point(
                                bounds.origin.x + px(p.x as f32),
                                bounds.origin.y + px(p.y as f32),
                            )
                        };
                        for edge in &edges {
                            let (Some(a), Some(b)) = (
                                positions.get(&edge.source_id),
                                positions.get(&edge.target_id),
                            ) else {
                                continue;
                            };
                            let active = highlighted.as_ref().is_some_and(|ids| {
                                ids.contains(&edge.source_id) && ids.contains(&edge.target_id)
                            });
                            let mut path = PathBuilder::stroke(px(if active { 1.5 } else { 1. }));
                            path.move_to(screen(*a));
                            path.line_to(screen(*b));
                            if let Ok(path) = path.build() {
                                window.paint_path(
                                    path,
                                    rgb(if active { outline } else { line }),
                                );
                            }
                        }
                        for n in &nodes {
                            let p = positions[&n.node.note_id];
                            let r = graph::node_radius(&n.node, center_id, n.degree) as f32;
                            let active = highlighted
                                .as_ref()
                                .is_none_or(|ids| ids.contains(&n.node.note_id));
                            let color = if selected == Some(n.node.note_id) || n.node.note_id == center_id || n.node.center {
                                mark
                            } else if hovered == Some(n.node.note_id) {
                                ink
                            } else if active {
                                outline
                            } else {
                                line
                            };
                            let mut dot = fill(
                                Bounds::new(
                                    screen(p) - point(px(r), px(r)),
                                    size(px(r * 2.), px(r * 2.)),
                                ),
                                rgb(color),
                            );
                            if compact && color != mark {
                                dot.background = rgb(panel).into();
                                dot.border_color = rgb(color).into();
                                dot.border_widths = px(0.75).into();
                            }
                            dot.corner_radii = px(r).into();
                            window.paint_quad(dot);
                        }
                    },
                )
                .size_full(),
            )
            .children(labels.into_iter().map(|(id,title,p,r)| {
                div().id(("graph-label", id as usize)).absolute()
                    .left(px((p.x+r+5.) as f32)).top(px((p.y-8.) as f32))
                    .max_w(px(170.)).overflow_hidden().text_xs().text_color(rgb(ink))
                    .cursor_pointer().child(title)
                    .on_mouse_down(MouseButton::Left, |_,_,cx|cx.stop_propagation())
                    .on_click(cx.listener(move |_,_,_,cx| { cx.emit(OpenNote(id)); cx.stop_propagation(); }))
            }))
            .when(compact, |el| el.child(
                div().id("graph-reset").absolute().right_1().top_1()
                    .text_xs().text_color(rgb(muted)).cursor_pointer().child("↺")
                    .on_mouse_down(MouseButton::Left, |_,_,cx|cx.stop_propagation())
                    .on_click(cx.listener(|this,_,_,cx| {
                        this.viewport.offset=Point::default(); this.viewport.scale=1.;
                        this.pan_from=None; this.node_drag=None; cx.stop_propagation(); cx.notify();
                    }))
            ))
            .when(!compact, |el| el.child(
                div().absolute().bottom_3().left_3().right_3().p_3()
                    .bg(rgb(panel)).border_1().border_color(rgb(line))
                    .text_color(rgb(ink)).text_sm()
                    .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .child(div().text_xs().text_color(rgb(muted)).child(self.layout_status))
                    .children(detail.map(|node| {
                        let id = node.note_id;
                        div().flex().flex_col().gap_1()
                            .child(div().child(node.title))
                            .child(div().text_xs().text_color(rgb(muted)).child(format!(
                                "ID {} · {} · vault {}", id,
                                node.file_kind.as_deref().unwrap_or("note"),
                                node.vault.as_deref().unwrap_or("unspecified"))))
                            .child(div().text_xs().overflow_hidden().child(node.path.unwrap_or_default()))
                            .child(div().id("graph-open-note").cursor_pointer()
                                .text_color(rgb(ink)).child("Open note →")
                                .on_click(cx.listener(move |_, _, _, cx| {
                                    cx.emit(OpenNote(id)); cx.stop_propagation();
                                })))
                    }))
                    .when(self.hovered.is_none() && self.selected.is_none(), |el| {
                        el.child("Hover to identify · click to select · double-click to open · drag node or background · Ctrl/Shift-wheel to zoom")
                    })
            ))
    }
}
