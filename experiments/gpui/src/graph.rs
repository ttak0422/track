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
/// Camera intent is independent of layout generation: a layout may settle after a
/// pan or zoom, but it must not replace the user's chosen view with an auto-fit.
#[derive(Clone, Copy, Debug, Default)]
pub struct CameraIntent(u64);
impl CameraIntent {
    pub fn revision(self) -> u64 {
        self.0
    }
    pub fn changed(&mut self) {
        self.0 = self.0.wrapping_add(1);
    }
    pub fn permits_auto_fit(self, started_at: u64) -> bool {
        self.0 == started_at
    }
}

/// Unmodified wheel input belongs to the surrounding scroll surface.
pub fn is_zoom_wheel(control: bool, shift: bool) -> bool {
    control || shift
}

/// Preview intent still follows every pointer event; only visual invalidation is gated.
pub fn pointer_needs_redraw(
    previous: Option<i64>,
    current: Option<i64>,
    moved_geometry: bool,
) -> bool {
    moved_geometry || previous != current
}

/// Screen-space title policy, independent of graph layout and GPU drawing.
pub fn show_label(
    view: Viewport,
    titles: bool,
    center: bool,
    degree: usize,
    highlighted: bool,
) -> bool {
    let spacious = view.width >= 420. || view.height >= 420.;
    titles && (center || highlighted || (spacious && (view.scale >= 0.26 || degree >= 4)))
}

/// Keep authored Unicode intact; the hover title is never shortened.
pub fn label_title(title: &str, id: i64, hovered: bool) -> String {
    let title = if title.is_empty() {
        format!("#{id}")
    } else {
        title.to_owned()
    };
    if hovered || title.chars().count() <= 24 {
        title
    } else {
        format!("{}...", title.chars().take(23).collect::<String>())
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

/// Return one delivered neighborhood without allocating adjacency sets for every node.
/// This runs during hover rendering; full adjacency is reserved for layout/overview.
pub fn neighbors(graph: &Graph, id: i64) -> BTreeSet<i64> {
    let available: BTreeSet<_> = graph.nodes.iter().map(|node| node.note_id).collect();
    if !available.contains(&id) {
        return BTreeSet::new();
    }
    let mut result = BTreeSet::from([id]);
    for edge in &graph.edges {
        let other = if edge.source_id == id {
            edge.target_id
        } else if edge.target_id == id {
            edge.source_id
        } else {
            continue;
        };
        if available.contains(&other) {
            result.insert(other);
        }
    }
    result
}

#[derive(Clone, Debug)]
pub struct Overview {
    pub graph: Graph,
    pub hidden: usize,
}
pub fn overview(graph: &Graph) -> Overview {
    overview_with_cap(graph, 1000)
}
/// Display-only whole-vault projection. The wire payload is not capped by this
/// operation. Count hidden nodes against the original payload, including isolates.
/// Duplicate IDs (if supplied) use the first node's metadata and draw only once.
pub fn overview_with_cap(input: &Graph, cap: usize) -> Overview {
    let mut graph = input.clone();
    let total = graph.nodes.len();
    let mut nodes = BTreeMap::new();
    for node in graph.nodes.drain(..) {
        nodes.entry(node.note_id).or_insert(node);
    }
    graph.edges.retain(|edge| {
        edge.source_id != edge.target_id
            && nodes.contains_key(&edge.source_id)
            && nodes.contains_key(&edge.target_id)
    });
    let mut degree: BTreeMap<i64, usize> = BTreeMap::new();
    for edge in &graph.edges {
        *degree.entry(edge.source_id).or_default() += 1;
        *degree.entry(edge.target_id).or_default() += 1;
    }
    let mut ranked: Vec<_> = degree.into_iter().collect();
    ranked.sort_unstable_by(|(a_id, a_degree), (b_id, b_degree)| {
        b_degree.cmp(a_degree).then_with(|| a_id.cmp(b_id))
    });
    let retained: BTreeSet<_> = ranked.iter().take(cap).map(|(id, _)| *id).collect();
    graph
        .edges
        .retain(|edge| retained.contains(&edge.source_id) && retained.contains(&edge.target_id));
    let linked: BTreeSet<_> = graph
        .edges
        .iter()
        .flat_map(|edge| [edge.source_id, edge.target_id])
        .collect();
    graph.nodes = nodes
        .into_values()
        .filter(|node| linked.contains(&node.note_id))
        .collect();
    let hidden = total - graph.nodes.len();
    Overview { graph, hidden }
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
    #[test]
    fn pointer_redraw_tracks_visible_changes_not_repeated_hover() {
        for target in [None, Some(1), Some(2)] {
            assert!(!super::pointer_needs_redraw(target, target, false));
            assert!(super::pointer_needs_redraw(target, target, true));
        }
        for (previous, current) in [(None, Some(1)), (Some(1), None), (Some(1), Some(2))] {
            assert!(super::pointer_needs_redraw(previous, current, false));
        }
    }

    #[test]
    fn labels_follow_screen_space_density_and_explicit_intent() {
        let small = super::Viewport::new(300., 280.);
        assert!(!super::show_label(small, true, false, 100, false));
        assert!(super::show_label(small, true, true, 0, false));
        assert!(super::show_label(small, true, false, 0, true));
        assert!(!super::show_label(small, false, true, 100, true));
        for (width, height) in [(420., 280.), (390., 420.)] {
            let mut view = super::Viewport::new(width, height);
            view.scale = 0.259;
            assert!(!super::show_label(view, true, false, 3, false));
            assert!(super::show_label(view, true, false, 4, false));
            view.scale = 0.26;
            assert!(super::show_label(view, true, false, 0, false));
        }
    }

    #[test]
    fn label_titles_preserve_unicode_and_expand_on_hover() {
        assert_eq!(super::label_title("", 42, false), "#42");
        let exact = "界".repeat(24);
        assert_eq!(super::label_title(&exact, 42, false), exact);
        let long = "界".repeat(25);
        assert_eq!(
            super::label_title(&long, 42, false),
            format!("{}...", "界".repeat(23))
        );
        assert_eq!(super::label_title(&long, 42, true), long);
        assert_eq!(super::label_title("short", 42, false), "short");
    }

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

/// Bounded initial relaxation, intended for a background worker, never pointer events.
/// This independent generic force model does not claim compatibility with other renderers.
#[derive(Clone, Copy, Debug)]
pub struct ForceOptions {
    pub iterations: usize,
    pub seed: u64,
    pub anchor_center: bool,
}
impl Default for ForceOptions {
    fn default() -> Self {
        Self {
            iterations: 80,
            seed: 42,
            anchor_center: true,
        }
    }
}

/// Return an owned layout snapshot. `fixed` positions support preserving manually dragged
/// nodes across a new settle. Explicit fixed positions override the automatic center anchor.
/// Work is capped at 200 iterations; callers should use overview's 1000-node cap.
/// The caller owns cancellation/stale-generation rejection when publishing the snapshot.
pub fn force_layout(
    graph: &Graph,
    options: ForceOptions,
    fixed: &BTreeMap<i64, Point>,
) -> Vec<LayoutNode> {
    let mut nodes = circular_layout(graph);
    let mut state = options.seed.max(1);
    let mut random = || {
        state ^= state << 13;
        state ^= state >> 7;
        state ^= state << 17;
        (state >> 11) as f64 / ((1u64 << 53) as f64)
    };
    // Seeded two-dimensional cloud avoids the symmetry of a single circle. Sorted node
    // order makes this stable even when the CLI changes its JSON node ordering.
    let spread = (nodes.len() as f64).sqrt().max(1.) * 18.;
    let pins: Vec<_> = nodes
        .iter()
        .map(|n| {
            fixed
                .get(&n.node.note_id)
                .copied()
                .filter(|p| p.finite())
                .or_else(|| {
                    (options.anchor_center && n.node.note_id == graph.center_id)
                        .then_some(Point::default())
                })
        })
        .collect();
    for (i, node) in nodes.iter_mut().enumerate() {
        node.position = pins[i].unwrap_or_else(|| Point {
            x: (random() - 0.5) * spread,
            y: (random() - 0.5) * spread,
        });
    }
    let index: BTreeMap<_, _> = nodes
        .iter()
        .enumerate()
        .map(|(i, n)| (n.node.note_id, i))
        .collect();
    // Undirected unique springs: duplicate/reversed edges must not multiply strength.
    let springs: BTreeSet<_> = graph
        .edges
        .iter()
        .filter_map(|e| {
            let (&a, &b) = (index.get(&e.source_id)?, index.get(&e.target_id)?);
            (a != b).then_some((a.min(b), a.max(b)))
        })
        .collect();
    let iterations = options.iterations.min(200);
    let mut forces = vec![Point::default(); nodes.len()];
    for step in 0..iterations {
        for (force, node) in forces.iter_mut().zip(&nodes) {
            *force = Point {
                x: -node.position.x * 0.015,
                y: -node.position.y * 0.015,
            };
        }
        for a in 0..nodes.len() {
            for b in a + 1..nodes.len() {
                let dx = nodes[a].position.x - nodes[b].position.x;
                let dy = nodes[a].position.y - nodes[b].position.y;
                let squared = (dx * dx + dy * dy).max(4.);
                let magnitude = 1800. / (squared * squared.sqrt());
                let (fx, fy) = (dx * magnitude, dy * magnitude);
                forces[a].x += fx;
                forces[a].y += fy;
                forces[b].x -= fx;
                forces[b].y -= fy;
            }
        }
        for &(a, b) in &springs {
            let dx = nodes[b].position.x - nodes[a].position.x;
            let dy = nodes[b].position.y - nodes[a].position.y;
            let distance = dx.hypot(dy).max(0.001);
            let magnitude = (distance - 70.) * 0.035 / distance;
            let (fx, fy) = (dx * magnitude, dy * magnitude);
            forces[a].x += fx;
            forces[a].y += fy;
            forces[b].x -= fx;
            forces[b].y -= fy;
        }
        let max_step = 8. * (1. - step as f64 / iterations.max(1) as f64) + 0.5;
        for (i, node) in nodes.iter_mut().enumerate() {
            if let Some(pin) = pins[i] {
                node.position = pin;
                continue;
            }
            let length = forces[i].x.hypot(forces[i].y);
            let scale = if length > max_step {
                max_step / length
            } else {
                1.
            };
            let next = Point {
                x: node.position.x + forces[i].x * scale,
                y: node.position.y + forces[i].y * scale,
            };
            if next.finite() {
                node.position = next;
            }
        }
    }
    nodes
}

#[cfg(test)]
mod force_tests {
    use super::*;
    #[test]
    fn deterministic_finite_disconnected_and_anchored() {
        let mut graph = fixture(40);
        graph.edges.truncate(10);
        let first = force_layout(&graph, ForceOptions::default(), &BTreeMap::new());
        graph.nodes.reverse();
        graph.edges.reverse();
        let second = force_layout(&graph, ForceOptions::default(), &BTreeMap::new());
        for (a, b) in first.iter().zip(&second) {
            assert_eq!(a.position, b.position);
            assert!(a.position.finite());
        }
        assert_eq!(first[0].position, Point::default());
        assert_ne!(first[30].position, first[31].position);
        assert_eq!(first.len(), 40);
        assert!(
            force_layout(&Graph::default(), ForceOptions::default(), &BTreeMap::new()).is_empty()
        );
    }
    #[test]
    fn pinned_drag_position_survives_relaxation() {
        let fixed = BTreeMap::from([
            (2, Point { x: 120., y: -95. }),
            (1, Point { x: 20., y: 30. }),
        ]);
        let layout = force_layout(&fixture(5), ForceOptions::default(), &fixed);
        assert_eq!(layout[1].position, fixed[&2]);
        assert_eq!(layout[0].position, fixed[&1]);
    }
    #[test]
    #[ignore = "manual release timing; background initial settle only"]
    fn force_benchmark() {
        for count in [100, 1000] {
            let graph = fixture(count);
            let start = std::time::Instant::now();
            let layout = force_layout(&graph, ForceOptions::default(), &BTreeMap::new());
            std::hint::black_box(&layout);
            eprintln!(
                "force benchmark: {count} nodes, 80 iterations, {:.3} ms",
                start.elapsed().as_secs_f64() * 1000.
            );
            assert!(layout.iter().all(|n| n.position.finite()));
        }
    }
}

#[cfg(test)]
mod neighborhood_tests {
    use super::*;

    // Previous implementation retained only as the benchmark/semantic baseline.
    fn full_adjacency_neighbors(graph: &Graph, id: i64) -> BTreeSet<i64> {
        match adjacency(graph).remove(&id) {
            Some(mut set) => {
                set.insert(id);
                set
            }
            None => BTreeSet::new(),
        }
    }

    #[test]
    fn direct_scan_matches_all_available_neighborhoods() {
        let mut graph = fixture(40);
        graph.edges.clear();
        for a in 1..=43 {
            for b in 1..=43 {
                if (a * 17 + b * 11) % 7 == 0 {
                    graph.edges.push(GraphEdge {
                        source_id: a,
                        target_id: b,
                    });
                    // Include duplicate, reversed, self and unavailable endpoints.
                    graph.edges.push(GraphEdge {
                        source_id: b,
                        target_id: a,
                    });
                    graph.edges.push(GraphEdge {
                        source_id: a,
                        target_id: b,
                    });
                }
            }
        }
        for id in 0..=43 {
            assert_eq!(
                neighbors(&graph, id),
                full_adjacency_neighbors(&graph, id),
                "ID {id}"
            );
        }
        assert!(neighbors(&Graph::default(), 1).is_empty());
        assert_eq!(neighbors(&fixture(1), 1), BTreeSet::from([1]));
    }

    /// CPU neighborhood lookup only; excludes GPUI paint and GPU presentation.
    #[test]
    #[ignore = "manual release comparison; not a CI speed threshold"]
    fn neighborhood_benchmark() {
        use std::{hint::black_box, time::Instant};
        for count in [100, 1000] {
            for dense in [false, true] {
                let mut graph = fixture(count);
                if dense {
                    graph.edges = (1..=count)
                        .flat_map(|a| {
                            ((a + 1)..=count)
                                .filter(move |b| (a + b) % 10 == 0)
                                .map(move |b| GraphEdge {
                                    source_id: a as i64,
                                    target_id: b as i64,
                                })
                        })
                        .collect();
                }
                let iterations = 200;
                for (name, lookup) in [
                    (
                        "full adjacency",
                        full_adjacency_neighbors as fn(&Graph, i64) -> BTreeSet<i64>,
                    ),
                    ("direct scan", neighbors),
                ] {
                    let start = Instant::now();
                    for iteration in 0..iterations {
                        let id = (iteration % count + 1) as i64;
                        black_box(lookup(black_box(&graph), black_box(id)));
                    }
                    eprintln!(
                        "neighborhood benchmark: {count} nodes, {} edges, {name}, {iterations} lookups, {:.3} us/lookup",
                        graph.edges.len(),
                        start.elapsed().as_secs_f64() * 1_000_000. / iterations as f64
                    );
                }
            }
        }
    }
}

#[cfg(test)]
mod overview_tests {
    use super::*;

    fn payload(count: i64, edges: &[(i64, i64)]) -> Graph {
        Graph {
            nodes: (1..=count)
                .map(|note_id| GraphNode {
                    note_id,
                    title: format!("Note {note_id}"),
                    ..Default::default()
                })
                .collect(),
            edges: edges
                .iter()
                .map(|&(source_id, target_id)| GraphEdge {
                    source_id,
                    target_id,
                })
                .collect(),
            ..Default::default()
        }
    }

    #[test]
    fn overview_empty_isolated_invalid_and_self_links() {
        let Overview {
            graph: empty,
            hidden,
        } = overview(&Graph::default());
        assert!(empty.nodes.is_empty());
        assert_eq!(hidden, 0);
        let Overview {
            graph: isolated,
            hidden,
        } = overview(&payload(3, &[(1, 1), (1, 99), (99, 2)]));
        assert!(isolated.nodes.is_empty());
        assert!(isolated.edges.is_empty());
        assert_eq!(hidden, 3);
        let Overview { graph, hidden } = overview(&payload(4, &[(1, 2), (2, 1), (3, 3), (3, 99)]));
        assert_eq!(
            graph.nodes.iter().map(|n| n.note_id).collect::<Vec<_>>(),
            [1, 2]
        );
        assert_eq!(graph.edges.len(), 2);
        assert_eq!(hidden, 2);
    }

    #[test]
    fn overview_cap_counts_both_directions_and_stable_numeric_ties() {
        let edges: Vec<_> = (1..=1001)
            .map(|id| if id % 2 == 0 { (id, 1002) } else { (1002, id) })
            .collect();
        let mut input = payload(1003, &edges);
        input.nodes.reverse();
        let Overview { graph, hidden } = overview(&input);
        assert_eq!(graph.nodes.len(), 1000);
        assert_eq!(graph.edges.len(), 999);
        assert_eq!(hidden, 3);
        let ids: BTreeSet<_> = graph.nodes.iter().map(|n| n.note_id).collect();
        assert!(ids.contains(&1002));
        assert!(ids.contains(&999));
        assert!(!ids.contains(&1000));
        assert!(!ids.contains(&1001));
    }

    #[test]
    fn overview_removes_nodes_isolated_by_cut_without_backfilling() {
        let mut edges: Vec<_> = (1..=998).map(|id| (id, id % 998 + 1)).collect();
        edges.extend([(999, 1001), (1000, 1002)]);
        let Overview { graph, hidden } = overview(&payload(1002, &edges));
        assert_eq!(graph.nodes.len(), 998);
        assert_eq!(graph.edges.len(), 998);
        assert_eq!(hidden, 4);
    }

    #[test]
    fn overview_ranks_incident_edges_not_distinct_neighbors() {
        let input = payload(6, &[(1, 2), (2, 1), (1, 2), (2, 1), (3, 4), (3, 5), (3, 6)]);
        let Overview { graph, hidden } = overview_with_cap(&input, 2);
        assert_eq!(
            graph.nodes.iter().map(|n| n.note_id).collect::<Vec<_>>(),
            [1, 2]
        );
        assert_eq!(graph.edges.len(), 4);
        assert_eq!(hidden, 4);
    }

    #[test]
    fn overview_preserves_scoped_metadata_and_area() {
        let mut input = payload(2, &[(1, 2)]);
        input.nodes[0].vault = Some("work".into());
        input.nodes[0].area_score = Some(1.5);
        input.nodes[0].size = Some(2);
        let Overview { graph, hidden } = overview(&input);
        assert_eq!(hidden, 0);
        assert_eq!(graph.nodes[0].vault.as_deref(), Some("work"));
        assert_eq!(graph.nodes[0].area_score, Some(1.5));
        assert_eq!(graph.nodes[0].size, Some(2));
    }
}

#[cfg(test)]
mod camera_intent_tests {
    use super::*;

    #[test]
    fn untouched_camera_accepts_initial_fit() {
        let intent = CameraIntent::default();
        assert!(intent.permits_auto_fit(intent.revision()));
    }

    #[test]
    fn newer_camera_intent_rejects_late_fit_without_canceling_layout() {
        let mut intent = CameraIntent::default();
        let layout_started = intent.revision();
        for _ in 0..3 {
            intent.changed();
            assert!(!intent.permits_auto_fit(layout_started));
        }
        assert!(intent.permits_auto_fit(intent.revision()));
    }

    #[test]
    fn wheel_requires_explicit_zoom_modifier() {
        assert!(!is_zoom_wheel(false, false));
        assert!(is_zoom_wheel(true, false));
        assert!(is_zoom_wheel(false, true));
        assert!(is_zoom_wheel(true, true));
    }
}
