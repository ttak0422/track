//! Bounded, inert outline model for native mindmap previews.
const MAX_NODES: usize = 128;
const MAX_DEPTH: usize = 16;
const MAX_AXIS: f32 = 16_384.0;

#[derive(Debug, Clone)]
pub struct Tree {
    pub nodes: Vec<Node>,
    pub omitted: bool,
}
#[derive(Debug, Clone)]
pub struct Node {
    pub label: String,
    pub parent: Option<usize>,
    pub implicit: bool,
}
#[derive(Debug, Clone, Copy)]
pub struct Rect {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
}
#[derive(Debug, Clone)]
pub struct Layout {
    pub nodes: Vec<Rect>,
    pub width: f32,
    pub height: f32,
}

fn heading(line: &str) -> Option<(usize, &str)> {
    let level = line.bytes().take_while(|b| *b == b'#').count();
    if !(1..=6).contains(&level) || !line[level..].starts_with(char::is_whitespace) {
        return None;
    }
    let mut label = line[level..].trim();
    let without_hashes = label.trim_end_matches('#');
    if without_hashes.len() < label.len() && without_hashes.ends_with(char::is_whitespace) {
        label = without_hashes.trim_end();
    }
    (!label.is_empty()).then_some((level, label))
}

fn label(text: &str) -> String {
    if let Some(inner) = text.strip_prefix("[[").and_then(|s| s.strip_suffix("]]")) {
        if !inner.contains(['[', ']', '\\']) {
            let (target, alias) = inner.split_once('|').unwrap_or((inner, inner));
            if !target.is_empty() && !alias.contains('|') {
                return if alias.is_empty() { target } else { alias }.to_string();
            }
        }
    }
    if let Some(inner) = text.strip_prefix('[').and_then(|s| s.strip_suffix(')')) {
        if let Some((display, target)) = inner.split_once("](") {
            if !display.is_empty()
                && !display.contains(['[', ']', '\\'])
                && !target.is_empty()
                && !target.contains(['(', ')', '\\'])
                && !target.chars().any(char::is_whitespace)
            {
                return display.to_string();
            }
        }
    }
    text.to_string()
}

fn push(tree: &mut Tree, text: &str, parent: Option<usize>) -> Result<usize, String> {
    if text.len() > 1024 {
        return Err("Mindmap: label exceeds 1024 bytes".into());
    }
    if tree.nodes.len() >= MAX_NODES {
        return Err("Mindmap: exceeds 128 nodes".into());
    }
    let index = tree.nodes.len();
    tree.nodes.push(Node {
        label: label(text),
        parent,
        implicit: false,
    });
    Ok(index)
}

/// Parse explicit heading/bullet source, or headings from the exact host snapshot.
pub fn parse(source: &str, host: Option<&str>) -> Result<Tree, String> {
    if source.len() > 32 * 1024 || source.lines().count() > 512 {
        return Err("Mindmap: source exceeds 32 KiB or 512 lines".into());
    }
    let empty = source.trim().is_empty();
    let input = if empty {
        host.ok_or("Mindmap: surrounding source unavailable")?
    } else {
        source
    };
    let mut tree = Tree {
        nodes: Vec::new(),
        omitted: false,
    };
    let mut headings: Vec<(usize, usize)> = Vec::new();
    let mut bullets: Vec<(usize, usize)> = Vec::new();
    let mut fence: Option<(u8, usize)> = None;
    for line in input.lines() {
        if empty {
            // Markdown fences may be indented at most three spaces. Only a
            // matching delimiter of sufficient length closes the current fence.
            let indent = line.bytes().take_while(|b| *b == b' ').count();
            let trimmed = &line[indent..];
            if indent <= 3 {
                let ch = trimmed.as_bytes().first().copied().unwrap_or(0);
                let count = trimmed.bytes().take_while(|b| *b == ch).count();
                if let Some((open, length)) = fence {
                    if ch == open && count >= length && trimmed[count..].trim().is_empty() {
                        fence = None;
                    }
                    continue;
                }
                if (ch == b'`' || ch == b'~')
                    && count >= 3
                    && (ch != b'`' || !trimmed[count..].contains('`'))
                {
                    fence = Some((ch, count));
                    continue;
                }
            }
            if fence.is_some() {
                continue;
            }
        }
        if let Some((level, text)) = heading(line) {
            while headings
                .last()
                .is_some_and(|(previous, _)| *previous >= level)
            {
                headings.pop();
            }
            let index = push(&mut tree, text, headings.last().map(|(_, index)| *index))?;
            headings.push((level, index));
            bullets.clear();
        } else if !empty && !line.trim().is_empty() {
            let prefix = line
                .bytes()
                .take_while(|b| *b == b' ' || *b == b'\t')
                .count();
            let tail = &line[prefix..];
            let is_bullet =
                tail.starts_with(['-', '*', '+']) && tail[1..].starts_with(char::is_whitespace);
            if is_bullet {
                let indent: usize = line[..prefix]
                    .bytes()
                    .map(|b| if b == b'\t' { 2 } else { 1 })
                    .sum();
                if indent > 8 || indent % 2 != 0 {
                    return Err("Mindmap: list indentation must be 0–8 spaces in steps of 2".into());
                }
                if let Some((_, heading)) = headings.last() {
                    let text = tail[1..].trim();
                    if !text.is_empty() {
                        while bullets
                            .last()
                            .is_some_and(|(previous, _)| *previous >= indent)
                        {
                            bullets.pop();
                        }
                        let parent = bullets.last().map(|(_, index)| *index).unwrap_or(*heading);
                        let index = push(&mut tree, text, Some(parent))?;
                        bullets.push((indent, index));
                        continue;
                    }
                }
            }
            tree.omitted = true;
        }
    }
    if tree.nodes.is_empty() {
        return Err("Mindmap: nothing to map".into());
    }
    if tree
        .nodes
        .iter()
        .filter(|node| node.parent.is_none())
        .count()
        > 1
    {
        if tree.nodes.len() >= MAX_NODES {
            return Err("Mindmap: exceeds 128 nodes including connector".into());
        }
        for node in &mut tree.nodes {
            node.parent = Some(node.parent.map_or(0, |index| index + 1));
        }
        tree.nodes.insert(
            0,
            Node {
                label: String::new(),
                parent: None,
                implicit: true,
            },
        );
    }
    depths(&tree)?;
    Ok(tree)
}

fn depths(tree: &Tree) -> Result<Vec<usize>, String> {
    if tree.nodes.is_empty() || tree.nodes.len() > MAX_NODES {
        return Err("Mindmap: invalid node count".into());
    }
    let mut depths = Vec::with_capacity(tree.nodes.len());
    for (index, node) in tree.nodes.iter().enumerate() {
        let depth = match node.parent {
            Some(parent) if parent < index => depths[parent] + 1,
            Some(_) => return Err("Mindmap: invalid parent".into()),
            None => 0,
        };
        if depth >= MAX_DEPTH {
            return Err("Mindmap: exceeds depth 16".into());
        }
        depths.push(depth);
    }
    Ok(depths)
}

/// Widths are actual shaped text advances, not character-count estimates.
pub fn layout(tree: &Tree, widths: &[f32]) -> Result<Layout, String> {
    let depths = depths(tree)?;
    if widths.len() != tree.nodes.len() || widths.iter().any(|v| !v.is_finite() || *v < 0.0) {
        return Err("Mindmap: invalid measured widths".into());
    }
    let mut columns = vec![0.0_f32; depths.iter().max().copied().unwrap_or(0) + 1];
    let mut children = vec![Vec::new(); tree.nodes.len()];
    let mut rects = Vec::with_capacity(tree.nodes.len());
    for (index, node) in tree.nodes.iter().enumerate() {
        let width = if node.implicit {
            8.0
        } else {
            (widths[index] + 24.0).max(40.0)
        };
        if !width.is_finite() || width > MAX_AXIS {
            return Err("Mindmap: geometry exceeds bounds".into());
        }
        columns[depths[index]] = columns[depths[index]].max(width);
        if let Some(parent) = node.parent {
            children[parent].push(index);
        }
        rects.push(Rect {
            x: 0.0,
            y: 0.0,
            width,
            height: if node.implicit { 8.0 } else { 36.0 },
        });
    }
    let mut positions = vec![0.0; columns.len()];
    for depth in 1..columns.len() {
        positions[depth] = positions[depth - 1] + columns[depth - 1] + 40.0;
    }
    let width = positions.last().unwrap() + columns.last().unwrap();
    // Leaves reserve distinct rows; a parent sits at its children's midpoint.
    let mut next_row = 0.0;
    for index in 0..rects.len() {
        rects[index].x = positions[depths[index]];
        if children[index].is_empty() {
            rects[index].y = next_row + (36.0 - rects[index].height) / 2.0;
            next_row += 54.0;
        }
    }
    for index in (0..rects.len()).rev() {
        if let (Some(first), Some(last)) = (children[index].first(), children[index].last()) {
            let center = (rects[*first].y
                + rects[*first].height / 2.0
                + rects[*last].y
                + rects[*last].height / 2.0)
                / 2.0;
            rects[index].y = center - rects[index].height / 2.0;
        }
    }
    let height = next_row - 18.0;
    if !width.is_finite()
        || width > MAX_AXIS
        || height <= 0.0
        || height > MAX_AXIS
        || rects
            .iter()
            .any(|r| !r.y.is_finite() || r.y < 0.0 || r.y + r.height > height)
    {
        return Err("Mindmap: geometry exceeds bounds".into());
    }
    Ok(Layout {
        nodes: rects,
        width,
        height,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn canonical_tree_and_inert_labels() {
        let tree = parse("# Vault\n## Notes\n- [[Syntax]]\n- [[Diagrams|Visuals]]\n## Resources\n- [Graphviz](https://graphviz.org/)", None).unwrap();
        assert_eq!(
            tree.nodes
                .iter()
                .map(|n| n.label.as_str())
                .collect::<Vec<_>>(),
            [
                "Vault",
                "Notes",
                "Syntax",
                "Visuals",
                "Resources",
                "Graphviz"
            ]
        );
        assert_eq!(
            tree.nodes.iter().map(|n| n.parent).collect::<Vec<_>>(),
            [None, Some(0), Some(1), Some(1), Some(0), Some(4)]
        );
        assert_eq!(label("[[a|]]"), "a");
        for text in [
            "<script>alert(1)</script>",
            "mixed [[x]]",
            "[a](javascript:alert(1))",
            "日本語 👩‍💻 e\u{301}",
        ] {
            assert_eq!(label(text), text);
        }
    }
    #[test]
    fn hierarchy_forest_duplicates_and_omissions() {
        let tree = parse(
            "ignored\n- orphan\n### First ###\n##### Deep\n- x\n\t- y\n  - y\n# Last\nprose",
            None,
        )
        .unwrap();
        assert!(tree.omitted && tree.nodes[0].implicit);
        assert_eq!(tree.nodes[1].label, "First");
        assert_eq!(
            tree.nodes.iter().map(|n| n.parent).collect::<Vec<_>>(),
            [None, Some(0), Some(1), Some(2), Some(3), Some(3), Some(0)]
        );
        assert!(parse("text\n- orphan", None).is_err());
    }
    #[test]
    fn exact_host_and_fence_exclusion() {
        let host = "# Host\n```mindmap\n# excluded\n````\n~~~rust\n# excluded too\n```\n~~~~\n## Child\n    # not heading\n> # quote\n";
        let tree = parse(" \n", Some(host)).unwrap();
        assert_eq!(tree.nodes.len(), 2);
        assert_eq!(tree.nodes[1].label, "Child");
        assert!(parse("", None).unwrap_err().contains("unavailable"));
        assert!(
            parse("", Some("no headings"))
                .unwrap_err()
                .contains("nothing")
        );
        assert_eq!(parse("# Explicit", None).unwrap().nodes.len(), 1);
    }
    #[test]
    fn source_and_node_limits_fail_whole_map() {
        let at = format!("# Root\n{}", "- x\n".repeat(127));
        assert_eq!(parse(&at, None).unwrap().nodes.len(), 128);
        assert!(parse(&(at + "- x"), None).is_err());
        assert!(parse(&"# root\n".repeat(128), None).is_err());
        assert!(parse(&format!("# {}", "x".repeat(1025)), None).is_err());
        assert!(parse(&" ".repeat(32769), None).is_err());
        assert!(parse(&format!("# Root\n{}", "\n".repeat(512)), None).is_err());
        assert!(parse("# Root\n - odd", None).is_err());
        assert!(parse("# Root\n          - deep", None).is_err());
    }
    #[test]
    fn layout_geometry_and_width_changes() {
        let tree = parse("# Root\n## One\n- a\n- b\n## Two\n- c", None).unwrap();
        let layout = layout(&tree, &vec![80.0; tree.nodes.len()]).unwrap();
        for (index, node) in tree.nodes.iter().enumerate() {
            if let Some(parent) = node.parent {
                assert!(
                    layout.nodes[index].x > layout.nodes[parent].x + layout.nodes[parent].width
                );
            }
        }
        assert!(layout.nodes[2].y + 36.0 <= layout.nodes[3].y);
        assert!(layout.nodes[3].y + 36.0 <= layout.nodes[5].y);
        assert!(
            super::layout(&tree, &vec![160.0; tree.nodes.len()])
                .unwrap()
                .width
                > layout.width
        );
        assert!(super::layout(&tree, &vec![f32::NAN; tree.nodes.len()]).is_err());
        assert!(super::layout(&tree, &vec![16_384.0; tree.nodes.len()]).is_err());
        let one = parse("# Alone", None).unwrap();
        assert_eq!(super::layout(&one, &[0.0]).unwrap().height, 36.0);
    }
    #[test]
    fn exact_limits_and_forest_layout() {
        assert!(parse(&format!("# {}", "x".repeat(1024)), None).is_ok());
        let at_bytes = format!("# Root\n{}", " ".repeat(32768 - 7));
        assert!(parse(&at_bytes, None).is_ok());
        let at_lines = format!("# Root\n{}", "\n".repeat(511));
        assert_eq!(at_lines.lines().count(), 512);
        assert!(parse(&at_lines, None).is_ok());
        assert!(parse("# Root\n        - allowed", None).is_ok());
        let host = format!("# Root\n{}", "## Child\n".repeat(128));
        assert!(parse("", Some(&host)).is_err());
        let tree = parse("# Same\n# Same", None).unwrap();
        let diagram = layout(&tree, &[0.0, 100.0, 100.0]).unwrap();
        assert_eq!(diagram.nodes[0].width, 8.0);
        assert_eq!(diagram.nodes[0].height, 8.0);
        assert!(diagram.nodes[1].y + diagram.nodes[1].height <= diagram.nodes[2].y);
        assert!(diagram.width > 0.0 && diagram.height > 0.0);
    }

    #[test]
    fn depth_and_parent_validation() {
        let mut tree = Tree {
            nodes: (0_usize..16)
                .map(|i| Node {
                    label: "x".into(),
                    parent: i.checked_sub(1),
                    implicit: false,
                })
                .collect(),
            omitted: false,
        };
        assert!(layout(&tree, &[20.0; 16]).is_ok());
        tree.nodes.push(Node {
            label: "x".into(),
            parent: Some(15),
            implicit: false,
        });
        assert!(layout(&tree, &[20.0; 17]).is_err());
        tree.nodes[0].parent = Some(0);
        assert!(layout(&tree, &[20.0; 17]).is_err());
    }
}
