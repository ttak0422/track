//! Inert native drawing. Local scroll never uses the host reader's anchor handle.
use crate::mindmap::{self, Tree};
use gpui::{
    Bounds, Div, PathBuilder, SharedString, TextRun, canvas, div, fill, point, prelude::*, px, rgb,
    size,
};
use std::{cell::RefCell, collections::BTreeMap, rc::Rc, sync::Arc};

#[derive(Default)]
pub struct Cache(RefCell<BTreeMap<String, (String, bool, Rc<Prepared>)>>);
struct Prepared {
    geometry: Result<mindmap::Layout, String>,
    lines: Vec<gpui::ShapedLine>,
}
impl Cache {
    pub fn clear(&self) {
        self.0.borrow_mut().clear();
    }
}
pub fn render(
    tree: Arc<Tree>,
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
                }),
            )
        });
        if entry.0 != font_key || entry.1 != dark {
            let lines: Vec<_> = tree
                .nodes
                .iter()
                .map(|node| {
                    let text: SharedString = node.label.clone().into();
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
            *entry = (
                font_key,
                dark,
                Rc::new(Prepared {
                    geometry: mindmap::layout(&tree, &widths),
                    lines,
                }),
            );
        }
        entry.2.clone()
    };
    let mut view = div().flex().flex_col().gap_2().min_w_0();
    if tree.nodes.is_empty() {
        return view.child("Mindmap: nothing to map · source below");
    }
    view = view.child("Mindmap · static native diagram; links inactive · original source below");
    if tree.omitted {
        view = view.child("Non-outline content omitted from diagram; full source retained below.");
    }
    let geometry = match &prepared.geometry {
        Ok(geometry) => geometry,
        Err(reason) => return view.child(format!("Mindmap unavailable: {reason}; source below")),
    };
    let width = geometry.width;
    let height = geometry.height;
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
                        for (index, node) in tree.nodes.iter().enumerate() {
                            if let Some(parent) = node.parent {
                                let a = &geometry.nodes[parent];
                                let b = &geometry.nodes[index];
                                let x1 = a.x + a.width;
                                let y1 = a.y + a.height / 2.;
                                let x2 = b.x;
                                let y2 = b.y + b.height / 2.;
                                let middle = (x1 + x2) / 2.;
                                let mut path = PathBuilder::stroke(px(1.5));
                                path.move_to(at(x1, y1));
                                path.line_to(at(middle, y1));
                                path.line_to(at(middle, y2));
                                path.line_to(at(x2, y2));
                                if let Ok(path) = path.build() {
                                    window.paint_path(path, rgb(rule));
                                }
                            }
                        }
                        for (index, node) in tree.nodes.iter().enumerate() {
                            let rect = &geometry.nodes[index];
                            let mut outer = fill(
                                Bounds::new(
                                    at(rect.x, rect.y),
                                    size(px(rect.width), px(rect.height)),
                                ),
                                rgb(if node.parent.is_none() { ink } else { rule }),
                            );
                            outer.corner_radii = px(if node.implicit { 4. } else { 6. }).into();
                            window.paint_quad(outer);
                            if node.implicit {
                                continue;
                            }
                            let border = if node.parent.is_none() { 2. } else { 1. };
                            let mut inner = fill(
                                Bounds::new(
                                    at(rect.x + border, rect.y + border),
                                    size(
                                        px(rect.width - border * 2.),
                                        px(rect.height - border * 2.),
                                    ),
                                ),
                                rgb(panel),
                            );
                            inner.corner_radii = px(5.).into();
                            window.paint_quad(inner);
                            let _ = prepared.lines[index].paint(
                                at(rect.x + 12., rect.y + 8.),
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
    fn close_releases_prepared_geometry_across_repeated_epochs() {
        let cache = Cache::default();
        for epoch in 0..100 {
            for block in 0..8 {
                cache.0.borrow_mut().insert(
                    format!("map-{epoch}-{block}"),
                    (
                        "font".into(),
                        false,
                        Rc::new(Prepared {
                            geometry: Err("test".into()),
                            lines: vec![],
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
