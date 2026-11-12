//! CLI graph boundary and deterministic initial geometry, independently implemented from
//! the public behavior contract. Circular placement is not force-directed layout parity.
use serde::Deserialize;
use std::collections::{BTreeMap, BTreeSet};

#[derive(Clone, Debug, Deserialize, Default)]
pub struct GraphResponse {
    pub graph: Graph,
}
#[derive(Clone, Debug, Deserialize, Default)]
pub struct Graph {
    #[serde(default)]
    pub center_id: i64,
    #[serde(default)]
    pub nodes: Vec<GraphNode>,
    #[serde(default)]
    pub edges: Vec<GraphEdge>,
}
#[derive(Clone, Debug, Deserialize, Default)]
pub struct GraphNode {
    pub note_id: i64,
    pub title: String,
    #[serde(default)]
    pub file_kind: Option<String>,
    #[serde(default)]
    pub path: Option<String>,
    #[serde(default)]
    pub vault: Option<String>,
    #[serde(default)]
    pub center: bool,
    #[serde(default)]
    pub size: Option<i64>,
    #[serde(default)]
    pub area_score: Option<f64>,
}
#[derive(Clone, Debug, Deserialize, PartialEq, Eq)]
pub struct GraphEdge {
    pub source_id: i64,
    pub target_id: i64,
}
#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub struct Point {
    pub x: f64,
    pub y: f64,
}
impl Point {
    fn finite(self) -> bool {
        self.x.is_finite() && self.y.is_finite()
    }
}
#[derive(Clone, Copy, Debug)]
pub struct Viewport {
    pub width: f64,
    pub height: f64,
    pub offset: Point,
    pub scale: f64,
}
impl Viewport {
    pub fn new(width: f64, height: f64) -> Self {
        Self {
            width,
            height,
            offset: Point::default(),
            scale: 1.,
        }
    }
    pub fn world_to_screen(&self, p: Point) -> Point {
        Point {
            x: self.width / 2. + self.offset.x + p.x * self.scale,
            y: self.height / 2. + self.offset.y + p.y * self.scale,
        }
    }
    pub fn screen_to_world(&self, p: Point) -> Point {
        Point {
            x: (p.x - self.width / 2. - self.offset.x) / self.scale,
            y: (p.y - self.height / 2. - self.offset.y) / self.scale,
        }
    }
    pub fn pan(&mut self, delta: Point) {
        if delta.finite() {
            self.offset.x += delta.x;
            self.offset.y += delta.y;
        }
    }
    pub fn zoom_at(&mut self, cursor: Point, factor: f64) {
        if !factor.is_finite() || factor <= 0. || !cursor.finite() {
            return;
        }
        let anchor = self.screen_to_world(cursor);
        self.scale = (self.scale * factor).clamp(0.015, 4.);
        self.offset = Point {
            x: cursor.x - self.width / 2. - anchor.x * self.scale,
            y: cursor.y - self.height / 2. - anchor.y * self.scale,
        };
    }
}
#[derive(Clone, Debug)]
pub struct LayoutNode {
    pub node: GraphNode,
    pub position: Point,
    pub degree: usize,
}
fn adjacency(graph: &Graph) -> BTreeMap<i64, BTreeSet<i64>> {
    let mut result: BTreeMap<_, BTreeSet<_>> = graph
        .nodes
        .iter()
        .map(|n| (n.note_id, BTreeSet::new()))
        .collect();
    for e in &graph.edges {
        if e.source_id != e.target_id
            && result.contains_key(&e.source_id)
            && result.contains_key(&e.target_id)
        {
            result.get_mut(&e.source_id).unwrap().insert(e.target_id);
            result.get_mut(&e.target_id).unwrap().insert(e.source_id);
        }
    }
    result
}
/// Ascending numeric ID around a circle starting on the positive x axis. A singleton
/// stays at the origin. This stable INITIAL placement deliberately has no physics.
pub fn circular_layout(graph: &Graph) -> Vec<LayoutNode> {
    let adjacency = adjacency(graph);
    let mut nodes = graph.nodes.clone();
    nodes.sort_by_key(|n| n.note_id);
    let count = nodes.len();
    let radius = if count <= 1 {
        0.
    } else {
        ((count as f64).sqrt() * 8.).max(120.)
    };
    nodes
        .into_iter()
        .enumerate()
        .map(|(i, node)| {
            let angle = i as f64 / count as f64 * std::f64::consts::TAU;
            let degree = adjacency.get(&node.note_id).map_or(0, BTreeSet::len);
            LayoutNode {
                node,
                degree,
                position: Point {
                    x: radius * angle.cos(),
                    y: radius * angle.sin(),
                },
            }
        })
        .collect()
}
pub fn node_radius(node: &GraphNode, center_id: i64, degree: usize) -> f64 {
    let center = node.center || node.note_id == center_id;
    let scored = node
        .area_score
        .filter(|s| s.is_finite() && (1. ..=5.).contains(s))
        .map(|s| (16. + (s - 1.) / 4. * (289. - 16.)).sqrt())
        .or_else(|| {
            node.size
                .filter(|s| (1..=5).contains(s))
                .map(|s| [4., 6., 8.5, 12., 17.][s as usize - 1])
        });
    let radius = scored.unwrap_or_else(|| {
        if center {
            10.
        } else {
            6. + 8. * (degree.min(16) as f64 / 16.).sqrt()
        }
    });
    if center { radius.max(10.) } else { radius }
}
pub fn hit_test(
    nodes: &[LayoutNode],
    center_id: i64,
    viewport: &Viewport,
    screen: Point,
) -> Option<i64> {
    if !screen.finite() {
        return None;
    }
    nodes
        .iter()
        .filter_map(|n| {
            let p = viewport.world_to_screen(n.position);
            let distance = (p.x - screen.x).hypot(p.y - screen.y);
            (distance <= node_radius(&n.node, center_id, n.degree) + 5.)
                .then_some((distance, n.node.note_id))
        })
        .min_by(|a, b| a.0.total_cmp(&b.0).then(a.1.cmp(&b.1)))
        .map(|(_, id)| id)
}
/// Move one node by screen pixels without changing the camera or other nodes.
/// This direct manipulation has no elastic/force simulation and is reset by new graph data.
pub fn drag_node(nodes: &mut [LayoutNode], id: i64, viewport: &Viewport, delta: Point) -> bool {
    if !delta.finite() || !viewport.scale.is_finite() || viewport.scale <= 0. {
        return false;
    }
    let Some(node) = nodes.iter_mut().find(|node| node.node.note_id == id) else {
        return false;
    };
    node.position.x += delta.x / viewport.scale;
    node.position.y += delta.y / viewport.scale;
    true
}

pub fn neighbors(graph: &Graph, id: i64) -> BTreeSet<i64> {
    let mut adjacency = adjacency(graph);
    match adjacency.remove(&id) {
        Some(mut set) => {
            set.insert(id);
            set
        }
        None => BTreeSet::new(),
    }
}
#[derive(Clone, Debug)]
pub struct Overview {
    pub graph: Graph,
    pub hidden: usize,
}
pub fn overview(graph: &Graph) -> Overview {
    overview_with_cap(graph, 1000)
}
pub fn overview_with_cap(graph: &Graph, cap: usize) -> Overview {
    let adjacency = adjacency(graph);
    let mut ranked: Vec<_> = adjacency.iter().filter(|(_, v)| !v.is_empty()).collect();
    ranked.sort_by(|(a, av), (b, bv)| bv.len().cmp(&av.len()).then(a.cmp(b)));
    let retained: BTreeSet<_> = ranked.into_iter().take(cap).map(|(id, _)| *id).collect();
    let edges: Vec<_> = graph
        .edges
        .iter()
        .filter(|e| {
            e.source_id != e.target_id
                && retained.contains(&e.source_id)
                && retained.contains(&e.target_id)
        })
        .cloned()
        .collect();
    let connected: BTreeSet<_> = edges
        .iter()
        .flat_map(|e| [e.source_id, e.target_id])
        .collect();
    let nodes: Vec<_> = graph
        .nodes
        .iter()
        .filter(|n| connected.contains(&n.note_id))
        .cloned()
        .collect();
    Overview {
        hidden: graph.nodes.len() - nodes.len(),
        graph: Graph {
            center_id: graph.center_id,
            nodes,
            edges,
        },
    }
}
/// Synthetic metadata only. No vault or filesystem access.
pub fn fixture(count: usize) -> Graph {
    Graph {
        center_id: 1,
        nodes: (1..=count)
            .map(|i| GraphNode {
                note_id: i as i64,
                title: format!("Note {i}"),
                area_score: Some(1. + (i % 5) as f64),
                ..Default::default()
            })
            .collect(),
        edges: (2..=count)
            .map(|i| GraphEdge {
                source_id: 1,
                target_id: i as i64,
            })
            .collect(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn close(a: Point, b: Point) {
        assert!(
            (a.x - b.x).abs() < 1e-8 && (a.y - b.y).abs() < 1e-8,
            "{a:?} != {b:?}"
        );
    }
    #[test]
    fn decode_envelope() {
        let r: GraphResponse=serde_json::from_str(r#"{"graph":{"center_id":42,"nodes":[{"note_id":42,"title":"日本語","area_score":3.5,"center":true,"size":2,"path":"/vault/42.md","vault":"work"},{"note_id":43,"title":"Other"}],"edges":[{"source_id":42,"target_id":43}]}}"#).unwrap();
        assert_eq!(r.graph.nodes[0].title, "日本語");
        assert_eq!(r.graph.nodes[0].area_score, Some(3.5));
        assert_eq!(r.graph.nodes[1].size, None);
        assert_eq!(overview(&r.graph).graph.nodes[0].area_score, Some(3.5));
    }
    #[test]
    fn empty_and_singleton() {
        let g = Graph::default();
        assert!(circular_layout(&g).is_empty());
        assert_eq!(overview(&g).hidden, 0);
        assert_eq!(circular_layout(&fixture(1))[0].position, Point::default());
        assert_eq!(fixture(1).nodes.len(), 1);
    }
    #[test]
    fn camera_roundtrip() {
        let mut v = Viewport::new(800., 600.);
        close(
            v.world_to_screen(Point::default()),
            Point { x: 400., y: 300. },
        );
        v.pan(Point { x: 32., y: -76. });
        v.zoom_at(Point { x: 12., y: 25. }, 1.7);
        let p = Point { x: 123., y: -987. };
        close(v.screen_to_world(v.world_to_screen(p)), p);
    }
    #[test]
    fn clamps_keep_anchor_and_invalid_zoom_does_nothing() {
        let mut v = Viewport::new(800., 600.);
        let cursor = Point { x: 72., y: 513. };
        let anchor = v.screen_to_world(cursor);
        v.zoom_at(cursor, 1000.);
        assert_eq!(v.scale, 4.);
        close(v.screen_to_world(cursor), anchor);
        v.zoom_at(cursor, 0.000001);
        assert_eq!(v.scale, 0.015);
        close(v.screen_to_world(cursor), anchor);
        for factor in [f64::NAN, -1., 0., f64::INFINITY] {
            v.zoom_at(cursor, factor);
            assert_eq!(v.scale, 0.015);
            close(v.screen_to_world(cursor), anchor);
        }
    }
    #[test]
    fn fixed_pixel_picking() {
        let n = LayoutNode {
            node: GraphNode {
                note_id: 2,
                title: "x".into(),
                size: Some(2),
                ..Default::default()
            },
            position: Point::default(),
            degree: 0,
        };
        for scale in [0.015, 1., 4.] {
            let mut v = Viewport::new(200., 200.);
            v.scale = scale;
            assert_eq!(
                hit_test(std::slice::from_ref(&n), 1, &v, Point { x: 111., y: 100. }),
                Some(2)
            );
            assert_eq!(
                hit_test(
                    std::slice::from_ref(&n),
                    1,
                    &v,
                    Point { x: 111.01, y: 100. }
                ),
                None
            );
            assert_eq!(
                hit_test(
                    std::slice::from_ref(&n),
                    1,
                    &v,
                    Point {
                        x: f64::NAN,
                        y: 100.
                    }
                ),
                None
            );
        }
    }
    #[test]
    fn scoring_precedence() {
        let mut n = GraphNode {
            note_id: 2,
            title: String::new(),
            area_score: Some(3.),
            size: Some(1),
            ..Default::default()
        };
        assert!((node_radius(&n, 1, 0).powi(2) - 152.5).abs() < 1e-8);
        n.area_score = Some(f64::NAN);
        assert_eq!(node_radius(&n, 1, 0), 4.);
        n.size = None;
        assert_eq!(node_radius(&n, 1, 0), 6.);
        assert_eq!(node_radius(&n, 1, 16), 14.);
        assert_eq!(node_radius(&n, 1, 100), 14.);
        n.center = true;
        assert_eq!(node_radius(&n, 1, 16), 10.);
        n.area_score = Some(5.);
        assert_eq!(node_radius(&n, 1, 0), 17.);
    }
    fn sample() -> Graph {
        let mut g = fixture(6);
        g.edges = [(1, 2), (1, 3), (4, 5), (6, 6), (6, 99)]
            .map(|(source_id, target_id)| GraphEdge {
                source_id,
                target_id,
            })
            .to_vec();
        g
    }
    #[test]
    fn adjacency_selection() {
        let g = sample();
        assert_eq!(neighbors(&g, 2), BTreeSet::from([1, 2]));
        assert_eq!(neighbors(&g, 1), BTreeSet::from([1, 2, 3]));
        assert_eq!(neighbors(&g, 6), BTreeSet::from([6]));
        assert!(neighbors(&g, 99).is_empty());
    }
    #[test]
    fn overview_caps_and_hidden() {
        let g = sample();
        let o = overview_with_cap(&g, 3);
        assert_eq!(
            o.graph.nodes.iter().map(|n| n.note_id).collect::<Vec<_>>(),
            vec![1, 2, 3]
        );
        assert_eq!(o.graph.edges.len(), 2);
        assert_eq!(o.hidden, 3);
        for cap in [0, 1] {
            let o = overview_with_cap(&g, cap);
            assert!(o.graph.nodes.is_empty() && o.graph.edges.is_empty());
            assert_eq!(o.hidden, 6);
        }
        let o = overview(&fixture(1005));
        assert_eq!(o.graph.nodes.len(), 1000);
        assert_eq!(o.hidden, 5);
    }
    #[test]
    fn layout_stable_by_numeric_id() {
        let g = fixture(10);
        let first = circular_layout(&g);
        let mut reversed = g.clone();
        reversed.nodes.reverse();
        let second = circular_layout(&reversed);
        for (a, b) in first.iter().zip(&second) {
            assert_eq!(a.node.note_id, b.node.note_id);
            assert_eq!(a.position, b.position);
        }
    }
}

#[cfg(test)]
mod interaction_measurements {
    use super::*;
    #[test]
    fn drag_uses_screen_delta_at_each_zoom_without_moving_neighbors() {
        for scale in [0.015, 1., 4.] {
            let mut nodes = circular_layout(&fixture(2));
            let mut viewport = Viewport::new(800., 600.);
            viewport.scale = scale;
            let before = viewport.world_to_screen(nodes[0].position);
            let neighbor = nodes[1].position;
            assert!(drag_node(
                &mut nodes,
                1,
                &viewport,
                Point { x: 30., y: -20. }
            ));
            let after = viewport.world_to_screen(nodes[0].position);
            assert!((after.x - before.x - 30.).abs() < 1e-8);
            assert!((after.y - before.y + 20.).abs() < 1e-8);
            assert_eq!(nodes[1].position, neighbor);
            assert!(!drag_node(&mut nodes, 999, &viewport, Point::default()));
            assert!(!drag_node(
                &mut nodes,
                1,
                &viewport,
                Point { x: f64::NAN, y: 0. }
            ));
        }
    }
    /// CPU geometry only: no canvas paint, GPU presentation, or input-to-photon claim.
    /// Run: cargo test --release --lib geometry_benchmark -- --ignored --nocapture
    #[test]
    #[ignore = "manual release timing; not a CI performance threshold"]
    fn geometry_benchmark() {
        use std::{hint::black_box, time::Instant};
        for count in [100, 1000] {
            let nodes = circular_layout(&fixture(count));
            let mut viewport = Viewport::new(800., 600.);
            let iterations = 10_000;
            let start = Instant::now();
            for frame in 0..iterations {
                let cursor = Point {
                    x: (frame % 800) as f64,
                    y: (frame % 600) as f64,
                };
                viewport.pan(Point { x: 0.25, y: -0.125 });
                viewport.zoom_at(cursor, if frame % 2 == 0 { 1.001 } else { 1. / 1.001 });
                black_box(hit_test(black_box(&nodes), 1, black_box(&viewport), cursor));
            }
            let elapsed = start.elapsed();
            eprintln!(
                "geometry benchmark: {count} nodes, {iterations} camera+pick iterations, {:.3} ms total, {:.3} us/iteration",
                elapsed.as_secs_f64() * 1000.,
                elapsed.as_secs_f64() * 1_000_000. / iterations as f64
            );
        }
    }
}
