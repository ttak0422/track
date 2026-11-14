//! Static native paint only. Models belong to the accepted rendered-preview snapshot.
use crate::numeric_chart::{self, Chart};
use gpui::{
    Bounds, Div, PathBuilder, SharedString, TextRun, canvas, div, fill, point, prelude::*, px, rgb,
    size,
};
use std::sync::Arc;

pub fn render(chart: Arc<Chart>, dark: bool) -> Div {
    let ink = if dark { 0xe9e9e4 } else { 0x1a1a18 };
    let rule = if dark { 0x3e4347 } else { 0xc7c5bd };
    let mut view = div().flex().flex_col().gap_2().overflow_hidden();
    if let Some(title) = &chart.title {
        view = view.child(title.clone());
    }
    view = view.child("Static native chart · zoom and hover details unavailable");
    let mut legend = div().flex().flex_wrap().gap_3();
    for series in &chart.series {
        legend = legend.child(
            div()
                .flex()
                .gap_1()
                .items_center()
                .child(div().w(px(12.)).h(px(3.)).bg(rgb(series.color)))
                .child(series.name.clone()),
        );
    }
    view = view.child(legend);
    if let Some(name) = &chart.y_name {
        view = view.child(format!("Y: {name}"));
    }
    // Full values remain readable even when axis labels are dense or long.
    view = view.child(format!(
        "Y domain: {} to {}",
        chart.domain.0, chart.domain.1
    ));
    if let Some(name) = &chart.x_name {
        view = view.child(format!("X: {name}"));
    }
    view.child(
        canvas(
            |_, _, _| (),
            move |bounds, _, window, cx| {
                let width = f32::from(bounds.size.width) as f64;
                let left = 170.;
                let top = 12.;
                let height = 240.;
                let label = |text: String,
                             x: f64,
                             y: f64,
                             window: &mut gpui::Window,
                             cx: &mut gpui::App| {
                    let text: SharedString = text.into();
                    let run = TextRun {
                        len: text.len(),
                        font: window.text_style().font(),
                        color: rgb(ink).into(),
                        background_color: None,
                        underline: None,
                        strikethrough: None,
                    };
                    let shaped = window.text_system().shape_line(text, px(11.), &[run], None);
                    let _ = shaped.paint(
                        point(bounds.left() + px(x as f32), bounds.top() + px(y as f32)),
                        px(16.),
                        window,
                        cx,
                    );
                };
                let geometry = match numeric_chart::geometry(&chart, width - left - 24., height) {
                    Ok(g) if width >= 340. => g,
                    _ => {
                        label(
                            "Chart unavailable at this width; source below".into(),
                            0.,
                            12.,
                            window,
                            cx,
                        );
                        return;
                    }
                };
                let at = |x: f64, y: f64| {
                    point(
                        bounds.left() + px((left + x) as f32),
                        bounds.top() + px((top + y) as f32),
                    )
                };
                let stroke =
                    |points: &[(f64, f64)], color: u32, width: f32, window: &mut gpui::Window| {
                        let mut path = PathBuilder::stroke(px(width));
                        if let Some(&(x, y)) = points.first() {
                            path.move_to(at(x, y));
                        }
                        for &(x, y) in points.iter().skip(1) {
                            path.line_to(at(x, y));
                        }
                        if let Ok(path) = path.build() {
                            window.paint_path(path, rgb(color));
                        }
                    };
                for (value, y) in [(chart.domain.1, 0.), (chart.domain.0, height)] {
                    stroke(&[(0., y), (geometry.width, y)], rule, 1., window);
                    label(format!("{value:e}"), 0., top + y - 8., window, cx);
                }
                stroke(&[(0., 0.), (0., height)], rule, 1., window);
                if let Some(y) = geometry.zero_y {
                    stroke(&[(0., y), (geometry.width, y)], ink, 1., window);
                    if y > 15. && y < height - 15. {
                        label("0".into(), 145., top + y - 8., window, cx);
                    }
                }
                for line in geometry.lines {
                    let color = chart.series[line.series].color;
                    for segment in line.segments {
                        if segment.len() > 1 {
                            stroke(
                                &segment.iter().map(|p| (p.x, p.y)).collect::<Vec<_>>(),
                                color,
                                2.,
                                window,
                            );
                        }
                    }
                    for p in line.symbols {
                        let mut dot = fill(
                            Bounds::new(at(p.x, p.y) - point(px(2.), px(2.)), size(px(4.), px(4.))),
                            rgb(color),
                        );
                        dot.corner_radii = px(2.).into();
                        window.paint_quad(dot);
                    }
                }
                for bar in geometry.bars {
                    // Actual zero remains a zero-height mark, never inflated into a value.
                    if bar.height > 0. {
                        window.paint_quad(fill(
                            Bounds::new(
                                at(bar.x, bar.y),
                                size(px(bar.width as f32), px(bar.height as f32)),
                            ),
                            rgb(chart.series[bar.series].color),
                        ));
                    }
                }
                let stride = chart
                    .categories
                    .len()
                    .div_ceil((geometry.width / 90.).floor().max(1.) as usize)
                    .max(1);
                for i in (0..chart.categories.len()).step_by(stride) {
                    let source = &chart.categories[i];
                    let mut text: String = source.chars().take(10).collect();
                    if source.chars().count() > 10 {
                        text.push('…');
                    }
                    let x = (left + geometry.category_x[i] - 12.)
                        .max(left)
                        .min(width - 85.);
                    label(text, x, top + height + 8., window, cx);
                }
            },
        )
        .w_full()
        .h(px(300.)),
    )
}
