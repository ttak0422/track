//! Inert native drawing. Local scroll never uses the host reader's anchor handle.
use crate::flowchart::{self, Graph};
use gpui::{
    Bounds, Div, PathBuilder, SharedString, TextRun, canvas, div, fill, point, prelude::*, px, rgb,
    size,
};
use std::{cell::RefCell, collections::BTreeMap, rc::Rc, sync::Arc};

#[derive(Default)]
pub struct Cache(RefCell<BTreeMap<String, (String, bool, Rc<Prepared>)>>);
struct Prepared {
    geometry: Result<flowchart::Diagram, String>,
    lines: Vec<gpui::ShapedLine>,
    edge_lines: Vec<gpui::ShapedLine>,
}
impl Cache {
    pub fn clear(&self) {
        self.0.borrow_mut().clear();
    }
}
pub fn render(
    tree: Arc<Graph>,
    dark: bool,
    identity: String,
    cache: &Cache,
    window: &mut gpui::Window,
) -> Div {
    let ink = if dark { 0xe9e9e4 } else { 0x1a1a18 };
    let panel = if dark { 0x212528 } else { 0xf3f2ee };
    let rule = if dark { 0x6e7478 } else { 0x8e8c84 };
    let font = window.text_style().font();
    let font_key = format!("{font:?}");
    let prepared = {
        let mut cached = cache.0.borrow_mut();
        let entry = cached.entry(identity.clone()).or_insert_with(|| {
            (
                String::new(),
                !dark,
                Rc::new(Prepared {
                    geometry: Err(String::new()),
                    lines: vec![],
                    edge_lines: vec![],
                }),
            )
        });
        if entry.0 != font_key || entry.1 != dark {
            let lines: Vec<_> = tree
                .nodes
                .iter()
                .map(|node| {
                    let text: SharedString = node.text.clone().into();
                    let run = TextRun {
                        len: text.len(),
                        font: font.clone(),
                        color: rgb(ink).into(),
                        background_color: None,
                        underline: None,
                        strikethrough: None,
                    };
                    window.text_system().shape_line(text, px(14.), &[run], None)
                })
                .collect();
            let widths: Vec<_> = lines.iter().map(|line| f32::from(line.width)).collect();
            let edge_lines: Vec<_> = tree
                .edges
                .iter()
                .map(|edge| {
                    let text: SharedString = edge.label.clone().unwrap_or_default().into();
                    let run = TextRun {
                        len: text.len(),
                        font: font.clone(),
                        color: rgb(ink).into(),
                        background_color: None,
                        underline: None,
                        strikethrough: None,
                    };
                    window.text_system().shape_line(text, px(12.), &[run], None)
                })
                .collect();
            let edge_widths: Vec<_> = edge_lines.iter().map(|l| f32::from(l.width)).collect();
            *entry = (
                font_key,
                dark,
                Rc::new(Prepared {
                    geometry: flowchart::diagram(&tree, &widths, &edge_widths)
                        .map_err(|e| e.0.to_owned()),
                    lines,
                    edge_lines,
                }),
            );
        }
        entry.2.clone()
    };
    let mut view = div().flex().flex_col().gap_2().min_w_0();
    if tree.nodes.is_empty() {
        return view.child("Flowchart: nothing to map · source below");
    }
    view = view.child("Flowchart · static native diagram; links inactive · original source below");
    let geometry = match &prepared.geometry {
        Ok(geometry) => geometry,
        Err(reason) => return view.child(format!("Flowchart unavailable: {reason}; source below")),
    };
    let width = geometry.layout.width;
    let height = geometry.layout.height;
    view.child(
        div()
            .id(SharedString::from(identity))
            .w_full()
            .min_w_0()
            .h(px(height.min(540.)))
            .overflow_x_scroll()
            .overflow_y_scroll()
            .child(
                canvas(
                    |_, _, _| (),
                    move |bounds, _, window, cx| {
                        let Ok(geometry) = &prepared.geometry else {
                            return;
                        };
                        let at = |x, y| point(bounds.left() + px(x), bounds.top() + px(y));
                        for (i, route) in geometry.routes.iter().enumerate() {
                            let mut path = PathBuilder::stroke(px(1.5));
                            if let Some(&(x, y)) = route.points.first() {
                                path.move_to(at(x, y));
                            }
                            for &(x, y) in route.points.iter().skip(1) {
                                path.line_to(at(x, y));
                            }
                            if let Ok(path) = path.build() {
                                window.paint_path(path, rgb(rule));
                            }
                            if let [.., p, q] = route.points.as_slice() {
                                let (dx, dy) = (q.0 - p.0, q.1 - p.1);
                                let norm = (dx * dx + dy * dy).sqrt();
                                if norm > 0. {
                                    let (ux, uy) = (dx / norm, dy / norm);
                                    let mut arrow = PathBuilder::stroke(px(1.5));
                                    arrow.move_to(at(
                                        q.0 - ux * 8. - uy * 4.,
                                        q.1 - uy * 8. + ux * 4.,
                                    ));
                                    arrow.line_to(at(q.0, q.1));
                                    arrow.line_to(at(
                                        q.0 - ux * 8. + uy * 4.,
                                        q.1 - uy * 8. - ux * 4.,
                                    ));
                                    if let Ok(path) = arrow.build() {
                                        window.paint_path(path, rgb(rule));
                                    }
                                }
                            }
                            if tree.edges[i].label.is_some() {
                                let w = f32::from(prepared.edge_lines[i].width);
                                window.paint_quad(fill(
                                    Bounds::new(
                                        at(route.label.0 - 2., route.label.1),
                                        size(px(w + 4.), px(20.)),
                                    ),
                                    rgb(panel),
                                ));
                                let _ = prepared.edge_lines[i].paint(
                                    at(route.label.0, route.label.1),
                                    px(20.),
                                    window,
                                    cx,
                                );
                            }
                        }
                        for (index, node) in tree.nodes.iter().enumerate() {
                            let b = geometry.layout.nodes[index];
                            if node.shape == flowchart::Shape::Decision {
                                for (inset, color) in [(0., rule), (1.5, panel)] {
                                    let mut path = PathBuilder::fill();
                                    path.move_to(at(b.x + b.width / 2., b.y + inset));
                                    path.line_to(at(b.x + b.width - inset, b.y + b.height / 2.));
                                    path.line_to(at(b.x + b.width / 2., b.y + b.height - inset));
                                    path.line_to(at(b.x + inset, b.y + b.height / 2.));
                                    path.close();
                                    if let Ok(path) = path.build() {
                                        window.paint_path(path, rgb(color));
                                    }
                                }
                            } else {
                                window.paint_quad(fill(
                                    Bounds::new(at(b.x, b.y), size(px(b.width), px(b.height))),
                                    rgb(rule),
                                ));
                                window.paint_quad(fill(
                                    Bounds::new(
                                        at(b.x + 1., b.y + 1.),
                                        size(px(b.width - 2.), px(b.height - 2.)),
                                    ),
                                    rgb(panel),
                                ));
                            }
                            let text_width = f32::from(prepared.lines[index].width);
                            let _ = prepared.lines[index].paint(
                                at(
                                    b.x + (b.width - text_width) / 2.,
                                    b.y + (b.height - 20.) / 2.,
                                ),
                                px(20.),
                                window,
                                cx,
                            );
                        }
                    },
                )
                .w(px(width))
                .h(px(height))
                .flex_shrink_0(),
            ),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn close_releases_all_prepared_frames() {
        let cache = Cache::default();
        for epoch in 0..32 {
            for i in 0..8 {
                cache.0.borrow_mut().insert(
                    format!("{epoch}-{i}"),
                    (
                        String::new(),
                        false,
                        Rc::new(Prepared {
                            geometry: Err("test".into()),
                            lines: vec![],
                            edge_lines: vec![],
                        }),
                    ),
                );
            }
            assert_eq!(cache.0.borrow().len(), 8);
            cache.clear();
            assert!(cache.0.borrow().is_empty());
        }
    }
}
