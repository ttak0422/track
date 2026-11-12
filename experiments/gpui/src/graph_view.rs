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
            layout_generation: 0,
            layout_status: "circular initial layout",
        }
    }
    pub fn set_graph(&mut self, graph: Graph, cx: &mut Context<Self>) {
        let generation = self.layout_generation.wrapping_add(1);
        *self = Self::new(graph);
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
            .bg(rgb(0x111820))
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
                                    rgb(if active { 0x7ec9b5 } else { 0x30404e }),
                                );
                            }
                        }
                        for n in &nodes {
                            let p = positions[&n.node.note_id];
                            let r = graph::node_radius(&n.node, center_id, n.degree) as f32;
                            let active = highlighted
                                .as_ref()
                                .is_none_or(|ids| ids.contains(&n.node.note_id));
                            let color = if selected == Some(n.node.note_id) {
                                0xf2b66d
                            } else if hovered == Some(n.node.note_id) {
                                0xffffff
                            } else if active {
                                0x7ec9b5
                            } else {
                                0x384b57
                            };
                            let mut dot = fill(
                                Bounds::new(
                                    screen(p) - point(px(r), px(r)),
                                    size(px(r * 2.), px(r * 2.)),
                                ),
                                rgb(color),
                            );
                            dot.corner_radii = px(r).into();
                            window.paint_quad(dot);
                        }
                    },
                )
                .size_full(),
            )
            .child(
                div().absolute().bottom_3().left_3().right_3().p_3()
                    .bg(rgb(0x18222c)).border_1().border_color(rgb(0x30404e))
                    .text_color(rgb(0xe7edf4)).text_sm()
                    .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .child(div().text_xs().text_color(rgb(0x9daebe)).child(self.layout_status))
                    .children(detail.map(|node| {
                        let id = node.note_id;
                        div().flex().flex_col().gap_1()
                            .child(div().child(node.title))
                            .child(div().text_xs().text_color(rgb(0x9daebe)).child(format!(
                                "ID {} · {} · vault {}", id,
                                node.file_kind.as_deref().unwrap_or("note"),
                                node.vault.as_deref().unwrap_or("unspecified"))))
                            .child(div().text_xs().overflow_hidden().child(node.path.unwrap_or_default()))
                            .child(div().id("graph-open-note").cursor_pointer()
                                .text_color(rgb(0x7ec9b5)).child("Open note →")
                                .on_click(cx.listener(move |_, _, _, cx| {
                                    cx.emit(OpenNote(id)); cx.stop_propagation();
                                })))
                    }))
                    .when(self.hovered.is_none() && self.selected.is_none(), |el| {
                        el.child("Hover to identify · click to select · double-click to open · drag node or background · Ctrl/Shift-wheel to zoom")
                    })
            )
    }
}
