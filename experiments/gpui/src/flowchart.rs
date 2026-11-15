//! Conservative, whole-document flowchart subset. No I/O or partial rendering.
use std::collections::{HashMap, HashSet, VecDeque};
pub const MAX_SOURCE: usize = 16 * 1024;
pub const MAX_NODES: usize = 64;
pub const MAX_EDGES: usize = 128;
pub const MAX_LABEL: usize = 256;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Direction {
    TD,
    TB,
    LR,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Shape {
    Rectangle,
    Decision,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Node {
    pub id: String,
    pub text: String,
    pub shape: Shape,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Edge {
    pub from: usize,
    pub to: usize,
    pub label: Option<String>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Graph {
    pub direction: Direction,
    pub nodes: Vec<Node>,
    pub edges: Vec<Edge>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Error(pub &'static str);
type Result<T> = std::result::Result<T, Error>;
fn err<T>(message: &'static str) -> Result<T> {
    Err(Error(message))
}
fn label(s: &str) -> Result<()> {
    if s.is_empty()
        || s.len() > MAX_LABEL
        || s.chars().any(|c| {
            c.is_control() || matches!(c, '<' | '>' | '*' | '_' | '~' | '$' | '`' | '\\' | '"')
        })
    {
        return err("Unsupported label");
    }
    if s.contains("fa:") || s.contains("icon:") || s.contains("#") {
        return err("Decorated labels are unsupported");
    }
    Ok(())
}
fn statements(source: &str) -> Result<Vec<&str>> {
    let mut result = Vec::new();
    let mut start = 0;
    let mut close = None;
    for (i, c) in source.char_indices() {
        if let Some(expected) = close {
            if c == expected {
                close = None;
            } else if matches!(c, '[' | ']' | '{' | '}' | '|' | '\n' | '\r') {
                return err("Nested or multiline label");
            }
        } else {
            match c {
                '[' => close = Some(']'),
                '{' => close = Some('}'),
                '|' => close = Some('|'),
                ']' | '}' => return err("Unmatched delimiter"),
                ';' | '\n' => {
                    let s = source[start..i].trim();
                    if !s.is_empty() {
                        result.push(s);
                    }
                    start = i + c.len_utf8();
                }
                _ => {}
            }
        }
    }
    if close.is_some() {
        return err("Unclosed label");
    }
    let last = source[start..].trim();
    if !last.is_empty() {
        result.push(last);
    }
    Ok(result)
}
fn endpoint(input: &str) -> Result<(Node, bool, &str)> {
    let input = input.trim_start();
    let end = input
        .bytes()
        .take_while(|b| b.is_ascii_alphanumeric() || *b == b'_')
        .count();
    if end == 0 || !input.as_bytes()[0].is_ascii_alphabetic() && input.as_bytes()[0] != b'_' {
        return err("Invalid node ID");
    }
    if end > MAX_LABEL {
        return err("Node ID exceeds label limit");
    }
    let id = &input[..end];
    if [
        "click",
        "style",
        "class",
        "classDef",
        "subgraph",
        "end",
        "direction",
        "linkStyle",
        "graph",
        "flowchart",
    ]
    .contains(&id)
    {
        return err("Reserved node ID");
    }
    let tail = input[end..].trim_start();
    let (shape, closing) = match tail.chars().next() {
        Some('[') => (Shape::Rectangle, ']'),
        Some('{') => (Shape::Decision, '}'),
        _ => {
            return Ok((
                Node {
                    id: id.into(),
                    text: id.into(),
                    shape: Shape::Rectangle,
                },
                false,
                tail,
            ));
        }
    };
    let pos = tail.find(closing).ok_or(Error("Unclosed node"))?;
    let text = &tail[1..pos];
    if shape == Shape::Rectangle
        && (text.starts_with(['(', '/', '\\']) || text.ends_with([')', '/', '\\']))
    {
        return err("Unsupported node shape");
    }
    label(text)?;
    // Quoted/Mermaid-interpreted labels are outside this plain-text subset.
    if text.contains(['"', '`', '\\']) {
        return err("Unsupported label syntax");
    }
    Ok((
        Node {
            id: id.into(),
            text: text.into(),
            shape,
        },
        true,
        tail[pos + 1..].trim_start(),
    ))
}
pub fn parse(source: &str) -> Result<Graph> {
    if source.len() > MAX_SOURCE
        || source
            .chars()
            .any(|c| c.is_control() && !matches!(c, '\n' | '\r' | '\t'))
    {
        return err("Source limit or control character");
    }
    let parts = statements(source)?;
    let Some(header) = parts.first() else {
        return err("Missing header");
    };
    let tokens: Vec<_> = header.split_whitespace().collect();
    if tokens.len() != 2 || !matches!(tokens[0], "flowchart" | "graph") {
        return err("Unsupported header");
    }
    let direction = match tokens[1] {
        "TD" => Direction::TD,
        "TB" => Direction::TB,
        "LR" => Direction::LR,
        _ => return err("Unsupported direction"),
    };
    let mut graph = Graph {
        direction,
        nodes: vec![],
        edges: vec![],
    };
    let mut ids = HashMap::new();
    let mut explicit = HashSet::new();
    fn intern(
        node: Node,
        defined: bool,
        graph: &mut Graph,
        ids: &mut HashMap<String, usize>,
        explicit: &mut HashSet<usize>,
    ) -> Result<usize> {
        if let Some(&i) = ids.get(&node.id) {
            if defined {
                if explicit.contains(&i) && graph.nodes[i] != node {
                    return err("Conflicting node definition");
                }
                graph.nodes[i] = node;
                explicit.insert(i);
            }
            return Ok(i);
        }
        if graph.nodes.len() == MAX_NODES {
            return err("Node limit");
        }
        let i = graph.nodes.len();
        ids.insert(node.id.clone(), i);
        graph.nodes.push(node);
        if defined {
            explicit.insert(i);
        }
        Ok(i)
    }
    for statement in &parts[1..] {
        let (node, defined, tail) = endpoint(statement)?;
        let from = intern(node, defined, &mut graph, &mut ids, &mut explicit)?;
        if tail.is_empty() {
            continue;
        }
        let Some(mut tail) = tail.strip_prefix("-->") else {
            return err("Unsupported edge or statement");
        };
        tail = tail.trim_start();
        let edge_label = if let Some(rest) = tail.strip_prefix('|') {
            let end = rest.find('|').ok_or(Error("Unclosed edge label"))?;
            let text = &rest[..end];
            label(text)?;
            if text.contains(['"', '`', '\\']) {
                return err("Unsupported edge label");
            }
            tail = rest[end + 1..].trim_start();
            Some(text.to_owned())
        } else {
            None
        };
        let (node, defined, rest) = endpoint(tail)?;
        if !rest.is_empty() {
            return err("Trailing syntax");
        }
        let to = intern(node, defined, &mut graph, &mut ids, &mut explicit)?;
        if graph.edges.len() == MAX_EDGES {
            return err("Edge limit");
        }
        graph.edges.push(Edge {
            from,
            to,
            label: edge_label,
        });
    }
    if graph.nodes.is_empty() {
        return err("Empty graph");
    }
    layers(&graph)?;
    Ok(graph)
}
fn layers(graph: &Graph) -> Result<Vec<usize>> {
    let n = graph.nodes.len();
    if n == 0 || n > MAX_NODES || graph.edges.len() > MAX_EDGES {
        return err("Graph limits");
    }
    let mut indegree = vec![0; n];
    let mut outgoing = vec![vec![]; n];
    for e in &graph.edges {
        if e.from >= n || e.to >= n {
            return err("Invalid edge index");
        }
        indegree[e.to] += 1;
        outgoing[e.from].push(e.to);
    }
    let mut queue: VecDeque<_> = (0..n).filter(|&i| indegree[i] == 0).collect();
    let mut rank = vec![0; n];
    let mut seen = 0;
    while let Some(i) = queue.pop_front() {
        seen += 1;
        for &j in &outgoing[i] {
            rank[j] = rank[j].max(rank[i] + 1);
            indegree[j] -= 1;
            if indegree[j] == 0 {
                queue.push_back(j);
            }
        }
    }
    if seen != n {
        return err("Cycles are unsupported");
    }
    Ok(rank)
}
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Bounds {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
}
#[derive(Clone, Debug, PartialEq)]
pub struct Layout {
    /// Same order as Graph::nodes; coordinates are top-left.
    pub nodes: Vec<Bounds>,
    pub width: f32,
    pub height: f32,
}
/// Widths must come from the UI's actual single-line text measurement, in node order.
/// No font guesses or truncation. The caller draws edges using graph indices.
pub fn layout(graph: &Graph, measured_widths: &[f32]) -> Result<Layout> {
    let rank = layers(graph)?;
    if measured_widths.len() != graph.nodes.len()
        || measured_widths
            .iter()
            .any(|w| !w.is_finite() || *w < 0. || *w > 2048.)
    {
        return err("Invalid measured widths");
    }
    let mut nodes: Vec<_> = graph
        .nodes
        .iter()
        .zip(measured_widths)
        .map(|(n, w)| Bounds {
            x: 0.,
            y: 0.,
            width: if n.shape == Shape::Decision {
                (w + 48.).max(100.) * 1.5
            } else {
                (w + 32.).max(80.)
            },
            height: if n.shape == Shape::Decision { 96. } else { 56. },
        })
        .collect();
    let mut major = 24.;
    let mut total_cross: f32 = 0.;
    for layer in 0..=*rank.iter().max().unwrap() {
        let members: Vec<_> = (0..nodes.len()).filter(|&i| rank[i] == layer).collect();
        let mut cross = 24.;
        let mut span: f32 = 0.;
        for i in members {
            let b = &mut nodes[i];
            if graph.direction == Direction::LR {
                b.x = major;
                b.y = cross;
                cross += b.height + 32.;
                span = span.max(b.width);
            } else {
                b.x = cross;
                b.y = major;
                cross += b.width + 32.;
                span = span.max(b.height);
            }
        }
        total_cross = total_cross.max(cross - 8.);
        major += span + 64.;
    }
    let (width, height) = if graph.direction == Direction::LR {
        (major - 40., total_cross)
    } else {
        (total_cross, major - 40.)
    };
    if !width.is_finite() || !height.is_finite() || width > 32768. || height > 32768. {
        return err("Layout exceeds bounds");
    }
    Ok(Layout {
        nodes,
        width,
        height,
    })
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn accepted_and_stable() {
        let g = parse("flowchart LR; A[開始] -->|yes| B{決定}; B --> C; A[開始]").unwrap();
        assert_eq!(
            g.nodes.iter().map(|n| n.id.as_str()).collect::<Vec<_>>(),
            vec!["A", "B", "C"]
        );
        assert_eq!(g.edges[0].label.as_deref(), Some("yes"));
        assert_eq!(g.nodes[1].shape, Shape::Decision);
        let l = layout(&g, &[40., 40., 12.]).unwrap();
        assert!(l.nodes[0].x < l.nodes[1].x);
        assert_eq!(l, layout(&g, &[40., 40., 12.]).unwrap());
    }
    #[test]
    fn reject_whole() {
        for s in [
            "graph BT; A --> B",
            "graph TD; A --> B --> C",
            "graph TD; A --> A",
            "graph TD; A-->B;B-->A",
            "graph TD; A[x]; A[y]",
            "graph TD; click A url",
            "graph TD; A[<b>x</b>]",
            "graph TD; %% comment\nA",
            "graph TD; A((x))",
            "graph TD;A[(Database)]",
            "graph TD;A[/Input/]",
            "graph TD;A[hello #9829;]",
            "graph TD;A-->|yes #9829;|B",
            "graph TD;A[**bold**]",
            "graph TD;A[$x$]",
            "graph TD;A[fa:home]",
            "graph TD; A[a\nb]",
            "graph TD; subgraph S;A;end",
        ] {
            assert!(parse(s).is_err(), "{s}");
        }
    }
    #[test]
    fn labels_and_implicit() {
        let g = parse("graph TB\nA --> B\nB[hello; 世界]\nA{ok}").unwrap();
        assert_eq!(g.nodes[1].text, "hello; 世界");
        assert_eq!(g.nodes[0].shape, Shape::Decision);
        assert_eq!(g.direction, Direction::TB);
    }
    #[test]
    fn bounds() {
        assert!(parse(&"x".repeat(MAX_SOURCE + 1)).is_err());
        assert!(parse(&format!("graph TD;A[{}]", "a".repeat(257))).is_err());
        let max_id = "a".repeat(MAX_LABEL);
        let over_id = "a".repeat(MAX_LABEL + 1);
        assert!(parse(&format!("graph TD;{max_id}")).is_ok());
        assert!(parse(&format!("graph TD;A --> {max_id}")).is_ok());
        assert!(parse(&format!("graph TD;{over_id}")).is_err());
        assert!(parse(&format!("graph TD;A --> {over_id}")).is_err());
        assert!(parse(&format!("graph TD;{over_id}[short]")).is_err());
        let g = parse("graph TD;A-->B").unwrap();
        assert!(layout(&g, &[f32::NAN, 2.]).is_err());
        assert!(layout(&g, &[1.]).is_err());
        let l = layout(&g, &[200., 400.]).unwrap();
        assert!(l.nodes[0].y < l.nodes[1].y);
        assert_eq!(l.nodes[1].width, 432.);
    }
    #[test]
    fn disconnected_branch_join_and_finite_boxes() {
        let g = parse("graph TD; Isolated; A-->B; A-->C; B-->D; C-->D").unwrap();
        let l = layout(&g, &[20., 20., 300., 10., 100.]).unwrap();
        assert_eq!(g.nodes.len(), 5);
        assert_eq!(g.edges.len(), 4);
        assert_eq!(l.nodes[2].y, l.nodes[3].y);
        assert!(l.nodes[4].y > l.nodes[2].y);
        for b in &l.nodes {
            assert!(
                b.x >= 0. && b.y >= 0. && b.x + b.width <= l.width && b.y + b.height <= l.height
            );
        }
        for (i, a) in l.nodes.iter().enumerate() {
            for b in &l.nodes[i + 1..] {
                assert!(
                    a.x + a.width <= b.x
                        || b.x + b.width <= a.x
                        || a.y + a.height <= b.y
                        || b.y + b.height <= a.y
                );
            }
        }
    }
    #[test]
    fn node_edge_limits() {
        let source = format!(
            "graph TD;{}",
            (0..65)
                .map(|i| format!("N{i}"))
                .collect::<Vec<_>>()
                .join(";")
        );
        assert!(parse(&source).is_err());
        let source = format!("graph TD;{}", vec!["A-->B"; 129].join(";"));
        assert!(parse(&source).is_err());
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Route {
    pub points: Vec<(f32, f32)>,
    pub label: (f32, f32),
}
#[derive(Clone, Debug, PartialEq)]
pub struct Diagram {
    pub layout: Layout,
    pub routes: Vec<Route>,
}
/// Orthogonal routing with separate tracks in rank gaps; skip-rank edges use
/// outer lanes, never intermediate nodes. Measured edge labels stay untruncated.
pub fn diagram(graph: &Graph, widths: &[f32], edge_widths: &[f32]) -> Result<Diagram> {
    if edge_widths.len() != graph.edges.len()
        || edge_widths
            .iter()
            .any(|w| !w.is_finite() || *w < 0. || *w > 2048.)
    {
        return err("Invalid edge label measurements");
    }
    let rank = layers(graph)?;
    let mut base = layout(graph, widths)?;
    let max_label = edge_widths.iter().copied().fold(0., f32::max);
    let gap = 96. + graph.edges.len() as f32 * 28. + max_label;
    // Re-place layers with room for every edge track and measured labels.
    let mut major = 24.;
    let mut cross_extent: f32 = 0.;
    for r in 0..=*rank.iter().max().unwrap() {
        let mut cross = 24. + max_label / 2.;
        let mut extent: f32 = 0.;
        for (i, b) in base
            .nodes
            .iter_mut()
            .enumerate()
            .filter(|(i, _)| rank[*i] == r)
        {
            let _ = i;
            if graph.direction == Direction::LR {
                b.x = major;
                b.y = cross;
                cross += b.height + 32.;
                extent = extent.max(b.width);
            } else {
                b.x = cross;
                b.y = major;
                cross += b.width + 32.;
                extent = extent.max(b.height);
            }
        }
        cross_extent = cross_extent.max(cross + max_label / 2.);
        major += extent + gap;
    }
    let mut routes = vec![];
    let mut outer = cross_extent + 24.;
    for (i, e) in graph.edges.iter().enumerate() {
        let a = base.nodes[e.from];
        let b = base.nodes[e.to];
        let skip = rank[e.to] > rank[e.from] + 1;
        let source_end = base
            .nodes
            .iter()
            .enumerate()
            .filter(|(j, _)| rank[*j] == rank[e.from])
            .map(|(_, n)| {
                if graph.direction == Direction::LR {
                    n.x + n.width
                } else {
                    n.y + n.height
                }
            })
            .fold(0., f32::max);
        let exit = source_end + 32. + i as f32 * 28.;
        let (points, label) = if graph.direction == Direction::LR {
            let start = (a.x + a.width, a.y + a.height / 2.);
            let end = (b.x, b.y + b.height / 2.);
            if skip {
                let entry = b.x - 24.;
                let lane = outer;
                outer += 32.;
                (
                    vec![
                        start,
                        (exit, start.1),
                        (exit, lane),
                        (entry, lane),
                        (entry, end.1),
                        end,
                    ],
                    ((exit + entry - edge_widths[i]) / 2., lane - 22.),
                )
            } else {
                (
                    vec![start, (exit, start.1), (exit, end.1), end],
                    (exit + 6., (start.1 + end.1) / 2. - 22.),
                )
            }
        } else {
            let start = (a.x + a.width / 2., a.y + a.height);
            let end = (b.x + b.width / 2., b.y);
            if skip {
                let entry = b.y - 24.;
                let lane = outer;
                outer += edge_widths[i] + 32.;
                (
                    vec![
                        start,
                        (start.0, exit),
                        (lane, exit),
                        (lane, entry),
                        (end.0, entry),
                        end,
                    ],
                    (lane + 6., (exit + entry) / 2. - 10.),
                )
            } else {
                (
                    vec![start, (start.0, exit), (end.0, exit), end],
                    ((start.0 + end.0 - edge_widths[i]) / 2., exit - 22.),
                )
            }
        };
        routes.push(Route { points, label });
    }
    let mut width = base
        .nodes
        .iter()
        .map(|n| n.x + n.width + 24.)
        .fold(0., f32::max);
    let mut height = base
        .nodes
        .iter()
        .map(|n| n.y + n.height + 24.)
        .fold(0., f32::max);
    for (i, r) in routes.iter().enumerate() {
        for &(x, y) in &r.points {
            width = width.max(x + 24.);
            height = height.max(y + 24.);
        }
        width = width.max(r.label.0 + edge_widths[i] + 24.);
        height = height.max(r.label.1 + 44.);
    }
    if !width.is_finite()
        || !height.is_finite()
        || width > 32768.
        || height > 32768.
        || routes.iter().any(|r| r.label.0 < 0. || r.label.1 < 0.)
    {
        return err("Routed layout exceeds bounds");
    }
    base.width = width;
    base.height = height;
    Ok(Diagram {
        layout: base,
        routes,
    })
}
#[cfg(test)]
mod routing_tests {
    use super::*;
    #[test]
    fn skips_do_not_cross_nodes() {
        for dir in ["TD", "LR"] {
            let g = parse(&format!("graph {dir}; A-->B;B-->C;C-->D;A-->|skip|D;A-->C")).unwrap();
            let d = diagram(&g, &[30., 60., 100., 40.], &[0., 0., 0., 28., 0.]).unwrap();
            for route in &d.routes {
                for pair in route.points.windows(2) {
                    let (a, b) = (pair[0], pair[1]);
                    assert!(a.0 == b.0 || a.1 == b.1);
                    for n in &d.layout.nodes {
                        let hits = if a.0 == b.0 {
                            a.0 > n.x
                                && a.0 < n.x + n.width
                                && a.1.min(b.1) < n.y + n.height
                                && a.1.max(b.1) > n.y
                        } else {
                            a.1 > n.y
                                && a.1 < n.y + n.height
                                && a.0.min(b.0) < n.x + n.width
                                && a.0.max(b.0) > n.x
                        };
                        assert!(!hits, "{dir} {a:?}->{b:?} hits {n:?}");
                    }
                }
            }
        }
    }
}
