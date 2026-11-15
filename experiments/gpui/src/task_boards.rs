//! Read-only task lanes from the immutable accepted host response, never Markdown tasks.
use super::*;
use std::{collections::BTreeMap, sync::Arc};
const STATES: [&str; 5] = ["TODO", "DOING", "WAITING", "DONE", "CANCELLED"];
const LANES: usize = 3;
const CARDS: usize = 50;
type Key = (usize, usize);
type Queries = BTreeMap<Key, Result<crate::query_view::View, String>>;
#[derive(Default)]
pub(super) struct Boards {
    pub blocks: BTreeMap<Key, Result<(), String>>,
    pub viewspecs: BTreeMap<Key, Result<(), String>>,
    pub mindmaps: BTreeMap<Key, Result<std::sync::Arc<crate::mindmap::Tree>, String>>,
    pub charts: BTreeMap<Key, Result<std::sync::Arc<crate::numeric_chart::Chart>, String>>,
    tasks: Arc<Vec<live::NoteTask>>,
    lanes: [Vec<usize>; 5],
}
pub(super) fn project(
    preview: &Result<markdown::IncludePreview, String>,
    tasks: Result<Arc<Vec<live::NoteTask>>, String>,
) -> (Queries, Boards) {
    project_with_source(preview, tasks, None)
}
pub(super) fn project_with_source(
    preview: &Result<markdown::IncludePreview, String>,
    tasks: Result<Arc<Vec<live::NoteTask>>, String>,
    host_source: Option<&str>,
) -> (Queries, Boards) {
    let mut queries = BTreeMap::new();
    let mut boards = Boards::default();
    if let Ok(tasks) = &tasks {
        boards.tasks = tasks.clone();
        for (index, task) in tasks.iter().enumerate() {
            if let Some(lane) = STATES.iter().position(|state| *state == task.state) {
                boards.lanes[lane].push(index);
            }
        }
    }
    let Ok(preview) = preview else {
        return (queries, boards);
    };
    let mut visuals = 0;
    for (item, content) in preview.items.iter().enumerate() {
        let (blocks, host) = match content {
            markdown::PreviewItem::Host(blocks) => (blocks, true),
            markdown::PreviewItem::Include { blocks, .. } => (blocks, false),
        };
        for (block, content) in blocks.iter().enumerate() {
            if content.kind != markdown::BlockKind::Code {
                continue;
            }
            let key = (item, block);
            let info = content.fence_info.as_deref();
            if content.language.as_deref() == Some("mindmap") {
                let result = if info != Some("mindmap") {
                    Err("Unsupported mindmap options; source retained.".into())
                } else if visuals >= 8 {
                    Err("Combined visual resource limit (8 blocks); source retained.".into())
                } else {
                    crate::mindmap::parse(&content.text, if host { host_source } else { None })
                        .map(std::sync::Arc::new)
                };
                if result.is_ok() {
                    visuals += 1;
                }
                boards.mindmaps.insert(key, result);
            } else if info == Some("viewspec") {
                let result = if visuals >= 8 {
                    Err("Combined visual resource limit (8 blocks); source retained.".into())
                } else {
                    crate::inline_viewspec::admit(&content.text)
                };
                if result.is_ok() {
                    visuals += 1;
                }
                boards.viewspecs.insert(key, result);
            } else if info == Some("echarts") {
                let chart = if visuals >= 8 {
                    Err("Combined visual resource limit (8 blocks); source retained.".into())
                } else {
                    crate::numeric_chart::decode(&content.text).map(std::sync::Arc::new)
                };
                if chart.is_ok() {
                    visuals += 1;
                }
                boards.charts.insert(key, chart);
            } else if info == Some("track-view") {
                let view = if visuals >= 8 {
                    Err("Combined visual resource limit (8 blocks); source retained.".into())
                } else {
                    crate::query_view::decode(&content.text)
                };
                if view.is_ok() {
                    visuals += 1;
                }
                queries.insert(key, view);
            } else if content.language.as_deref() == Some("taskboard") {
                let result = if info != Some("taskboard") || !content.text.trim().is_empty() {
                    Err(
                        "Unsupported taskboard content or options; use an empty taskboard fence."
                            .into(),
                    )
                } else if !host {
                    Err("Task board unavailable for this included source".into())
                } else if let Err(error) = &tasks {
                    Err(format!("Task board unavailable: {error}"))
                } else if visuals >= 8 {
                    Err("Combined visual resource limit (8 blocks); source retained.".into())
                } else {
                    visuals += 1;
                    Ok(())
                };
                boards.blocks.insert(key, result);
            }
        }
    }
    (queries, boards)
}
/// Called only after the shared identity, serial, epoch and interaction gates.
pub(super) fn set_page(
    state: &mut IncludesState,
    key: Key,
    lane: Option<usize>,
    page: usize,
) -> bool {
    let Some(Ok(snapshot)) = &state.result else {
        return false;
    };
    let board = &snapshot.boards;
    if !matches!(board.blocks.get(&key), Some(Ok(()))) || board.tasks.is_empty() {
        return false;
    }
    let (count, size) = match lane {
        None => (5, LANES),
        Some(lane) => {
            let visible = state
                .query_pages
                .get(&(key.0, key.1, None))
                .copied()
                .unwrap_or(0);
            if lane >= 5 || lane / LANES != visible {
                return false;
            }
            (board.lanes[lane].len(), CARDS)
        }
    };
    if page >= count.div_ceil(size).max(1) {
        return false;
    }
    state.query_pages.insert((key.0, key.1, lane), page);
    true
}
impl Workspace {
    pub(super) fn render_task_board(
        &self,
        key: Key,
        block: &markdown::Block,
        dark: bool,
        line: u32,
        cx: &mut Context<Self>,
    ) -> Div {
        let state = self.shell.includes.as_ref().unwrap();
        let Some(Ok(snapshot)) = &state.result else {
            return div();
        };
        let board = &snapshot.boards;
        let mut view = div()
            .flex()
            .flex_col()
            .gap_2()
            .ml(px(block.indent as f32 * 16.));
        if block.quoted {
            view = view.border_l_2().border_color(rgb(line)).pl_3();
        }
        if let Some(marker) = &block.marker {
            view = view.child(marker.clone());
        }
        view = view
            .child("Task board (read-only)")
            .child("State changes, dragging and source-line navigation are unavailable.");
        // Always retain the exact parsed fence alongside the visual. The host's
        // existing raw Copy remains authoritative, including its delimiters.
        view = view.child(self.render_preview_copy(
            key.0,
            key.1,
            std::slice::from_ref(block),
            dark,
            cx,
        ));
        if let Some(Err(reason)) = board.blocks.get(&key) {
            return view.child(reason.clone());
        }
        if board.tasks.is_empty() {
            return view.child("No tasks in this note snapshot");
        }
        view = view.child(self.query_pager(key, None, 5, LANES, cx));
        let page = state
            .query_pages
            .get(&(key.0, key.1, None))
            .copied()
            .unwrap_or(0);
        let mut lanes = div().flex().gap_2().w_full().min_w_0();
        for lane in page * LANES..((page + 1) * LANES).min(5) {
            let indices = &board.lanes[lane];
            let mut column = div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .gap_2()
                .child(format!("{} · {} tasks", STATES[lane], indices.len()))
                .child(self.query_pager(key, Some(lane), indices.len(), CARDS, cx));
            let card_page = state
                .query_pages
                .get(&(key.0, key.1, Some(lane)))
                .copied()
                .unwrap_or(0);
            for index in indices.iter().skip(card_page * CARDS).take(CARDS) {
                let task = &board.tasks[*index];
                let mut card = div()
                    .id(gpui::SharedString::from(format!(
                        "task-{}-{}-{}-{}-{lane}-{}",
                        state.serial, state.preview_epoch, key.0, key.1, task.line
                    )))
                    .p_2()
                    .border_1()
                    .border_color(rgb(line))
                    .child(if task.text.is_empty() {
                        "(untitled task)".to_owned()
                    } else {
                        task.text.clone()
                    });
                if task.done {
                    card = card.border_l_2().child("Completed state");
                }
                let mut chips = div().flex().flex_wrap().gap_2().text_size(px(11.));
                if !task.priority.is_empty() {
                    chips = chips.child(format!("#{}", task.priority));
                }
                for (label, value) in [
                    ("Scheduled", &task.scheduled),
                    ("Due", &task.due),
                    ("Completed", &task.completed),
                ] {
                    if !value.is_empty() {
                        chips = chips.child(format!("{label}: {value}"));
                    }
                }
                column = column.child(
                    card.child(chips).child(
                        div()
                            .text_size(px(10.))
                            .child(format!("Source line {}", task.line)),
                    ),
                );
            }
            lanes = lanes.child(column);
        }
        view.child(lanes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn preview(body: &str) -> Result<markdown::IncludePreview, String> {
        markdown::included_preview(
            &crate::included_notes::decode(json!({"markdown":body,"includes":[]}), "work").unwrap(),
        )
    }
    fn tasks(total: usize) -> Arc<Vec<live::NoteTask>> {
        Arc::new(live::note_tasks(Some(&json!({"items":(0..total).map(|i| json!({"line":i+1,"state":"TODO","done":false,"text":"same"})).collect::<Vec<_>>()}))).unwrap())
    }
    #[test]
    fn exact_fences_placement_and_source_isolation() {
        for body in [
            "`taskboard`",
            "````text\n```taskboard\n```\n````",
            "<pre>\n```taskboard\n```\n</pre>",
            "\\`\\`\\`taskboard",
        ] {
            assert!(project(&preview(body), Ok(tasks(1))).1.blocks.is_empty());
        }
        for body in ["```taskboard extra\n```", "```taskboard\nkeep me\n```"] {
            assert!(project(&preview(body), Ok(tasks(1))).1.blocks[&(0, 0)].is_err());
        }
        let source = "```taskboard\n```\n\n```taskboard\n \t\n```\n\n# Following";
        let p = preview(source);
        assert_eq!(project(&p, Ok(tasks(1))).1.blocks.len(), 2);
        let markdown::PreviewItem::Host(blocks) = &p.unwrap().items[0] else {
            panic!()
        };
        assert_eq!(*blocks, markdown::parse(source));
        assert_eq!(blocks.last().unwrap().text, "Following");
        let response = crate::included_notes::decode(json!({"markdown":"![[Target]]\n","includes":[{"line":0,"source_line":0,"caption":"Target","lines":["```taskboard","```"],"note_id":42,"kind":"note","etag":"rev"}]}),"work").unwrap();
        let p = markdown::included_preview(&response);
        assert_eq!(
            project(&p, Ok(tasks(1))).1.blocks[&(0, 0)]
                .as_ref()
                .unwrap_err(),
            "Task board unavailable for this included source"
        );
        for body in ["> ```taskboard\n> ```", "- item\n\n  ```taskboard\n  ```"] {
            let p = preview(body);
            let b = project(&p, Ok(tasks(1))).1;
            assert_eq!(b.blocks.values().filter(|v| v.is_ok()).count(), 1);
        }
    }
    #[test]
    fn combined_budget_and_shared_immutable_source() {
        let query = "```track-view\n{\"layout\":\"list\",\"columns\":[],\"groups\":[]}\n```\n\n";
        for body in [
            format!("{}{}", query.repeat(5), "```taskboard\n```\n\n".repeat(5)),
            format!("{}{}", "```taskboard\n```\n\n".repeat(5), query.repeat(5)),
        ] {
            let original = tasks(4096);
            let (queries, boards) = project(&preview(&body), Ok(original.clone()));
            assert_eq!(
                queries.values().filter(|v| v.is_ok()).count()
                    + boards.blocks.values().filter(|v| v.is_ok()).count(),
                8
            );
            assert!(Arc::ptr_eq(&original, &boards.tasks));
        }
        let p = preview("```taskboard\n```");
        assert!(project(&p, Ok(tasks(0))).1.blocks[&(0, 0)].is_ok());
        assert!(project(&p, Err("invalidated".into())).1.blocks[&(0, 0)].is_err());
    }
    #[test]
    fn lanes_preserve_five_states_literal_metadata_and_engine_order() {
        let value = json!({"items":STATES.iter().enumerate().map(|(i,state)|json!({"line":i+1,"state":state,"done":i>=3,"text":"","priority":"A","scheduled":"2026-11-16","due":"2026-02-30","completed":"2026-11-15"})).collect::<Vec<_>>()});
        let data = Arc::new(live::note_tasks(Some(&value)).unwrap());
        let boards = project(&preview("```taskboard\n```"), Ok(data)).1;
        for lane in 0..5 {
            assert_eq!(boards.lanes[lane], vec![lane]);
        }
        assert_eq!(boards.tasks[0].due, "2026-02-30");
        assert_eq!(boards.tasks[0].text, "");
        for total in [0, 1, 50, 51, 4096] {
            let boards = project(&preview("```taskboard\n```"), Ok(tasks(total))).1;
            assert_eq!(boards.lanes[0], (0..total).collect::<Vec<_>>());
            let visited: Vec<_> = boards.lanes[0].chunks(CARDS).flatten().copied().collect();
            assert_eq!(visited, (0..total).collect::<Vec<_>>());
        }
    }
    #[test]
    fn board_callbacks_validate_membership_pages_source_epoch_and_all_gates() {
        let id = Identity {
            source: LinkSource {
                note: live::NoteKey {
                    vault: "work".into(),
                    note_id: 1,
                },
                etag: "rev".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            route: live::NoteKey {
                vault: "work".into(),
                note_id: 1,
            },
        };
        let response = crate::included_notes::decode(
            json!({"markdown":"```taskboard\n```\n\n```taskboard\n```","includes":[]}),
            "work",
        )
        .unwrap();
        let preview = markdown::included_preview(&response);
        let (queries, boards) = project(&preview, Ok(tasks(51)));
        let mut s = IncludesState::new(id.clone());
        s.open = true;
        s.preview_open = true;
        s.serial = 10;
        s.preview_epoch = 11;
        s.result = Some(Ok(Snapshot {
            query_positions: Ok(Default::default()),
            inline: Ok(Default::default()),
            response,
            preview,
            queries,
            boards,
        }));
        let ready = || QueryPageGate {
            preview_active: true,
            source_matches: true,
            ..Default::default()
        };
        assert!(s.query_page_action(ready(), &id, 10, 11, (0, 0), Some(0), 1));
        assert_eq!(s.query_pages.get(&(0, 1, Some(0))), None);
        assert!(!s.query_page_action(ready(), &id, 10, 11, (0, 0), Some(0), 2));
        assert!(!s.query_page_action(ready(), &id, 10, 11, (0, 9), None, 0));
        assert!(s.query_page_action(ready(), &id, 10, 11, (0, 0), None, 1));
        assert!(!s.query_page_action(ready(), &id, 10, 11, (0, 0), Some(0), 0));
        assert!(s.query_page_action(ready(), &id, 10, 11, (0, 0), Some(4), 0));
        assert!(!s.query_page_action(ready(), &id, 10, 11, (0, 0), None, 2));
        assert!(!s.query_page_action(ready(), &id, 9, 11, (0, 0), None, 0));
        assert!(!s.query_page_action(ready(), &id, 10, 9, (0, 0), None, 0));
        for gate in [
            QueryPageGate {
                pending: true,
                ..ready()
            },
            QueryPageGate {
                editing: true,
                ..ready()
            },
            QueryPageGate {
                closing: true,
                ..ready()
            },
            QueryPageGate {
                source_matches: false,
                ..ready()
            },
        ] {
            assert!(!s.query_page_action(gate, &id, 10, 11, (0, 0), None, 0));
        }
        let mut old = id.clone();
        old.source.request += 1;
        assert!(!s.query_page_action(ready(), &old, 10, 11, (0, 0), None, 0));
        s.preview_open = false;
        assert!(!s.query_page_action(ready(), &id, 10, 11, (0, 0), None, 0));
    }
}

#[cfg(test)]
mod chart_projection_tests {
    use super::*;
    use serde_json::json;
    const OPTION: &str = r#"{"xAxis":{"type":"category","data":["A","B","C"]},"yAxis":{"type":"value"},"series":[{"type":"line","data":[0,null,4]}]}"#;
    fn fence() -> String {
        format!("```echarts\n{OPTION}\n```\n")
    }
    fn project_body(body: &str) -> (markdown::IncludePreview, Boards) {
        let response =
            crate::included_notes::decode(json!({"markdown":body,"includes":[]}), "host").unwrap();
        let preview = markdown::included_preview(&response);
        let (_, boards) = project(&preview, Ok(Arc::new(vec![])));
        (preview.unwrap(), boards)
    }
    #[test]
    fn charts_only_activate_exact_parsed_fences_and_keep_bytes() {
        for body in [
            format!("````text\n{}\n````", fence()),
            format!("<pre>\n{}\n</pre>", fence()),
            format!("`echarts {OPTION}`"),
            fence().replace("```echarts", "```echarts extra"),
            format!("\\`\\`\\`echarts\n{OPTION}"),
        ] {
            assert!(project_body(&body).1.charts.is_empty(), "{body}");
        }
        let (preview, boards) = project_body(&format!("# Before\n{}\n# After", fence()));
        let (&(item, block), chart) = boards.charts.first_key_value().unwrap();
        assert!(chart.is_ok());
        let markdown::PreviewItem::Host(blocks) = &preview.items[item] else {
            panic!()
        };
        assert_eq!(blocks[block].text, format!("{OPTION}\n"));
        assert!(blocks[block + 1].text.contains("After"));
    }
    #[test]
    fn chart_nested_origin_and_literal_labels_stay_in_preview() {
        let quoted = fence()
            .lines()
            .map(|line| format!("> {line}\n"))
            .collect::<String>();
        let (preview, boards) = project_body(&quoted);
        assert!(boards.charts[&(0, 0)].is_ok());
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert!(blocks[0].quoted);
        let response = crate::included_notes::decode(json!({"markdown":"![[Target]]\n","includes":[{"line":0,"source_line":0,"caption":"Target","lines":[fence()],"note_id":42,"kind":"note","etag":"rev"}]}), "host").unwrap();
        let preview = markdown::included_preview(&response);
        let (_, boards) = project(&preview, Ok(Arc::new(vec![])));
        assert_eq!(boards.charts.len(), 1);
        assert!(matches!(
            preview.unwrap().items[0],
            markdown::PreviewItem::Include { index: 0, .. }
        ));
    }
    #[test]
    fn charts_share_ordered_budget_with_taskboards_and_queries() {
        let mut body = "```taskboard\n```\n".repeat(2);
        body.push_str("```track-view\n{\"layout\":\"list\",\"columns\":[],\"groups\":[]}\n```\n");
        body.push_str(&fence().repeat(6));
        let (_, boards) = project_body(&body);
        assert_eq!(boards.blocks.values().filter(|v| v.is_ok()).count(), 2);
        assert_eq!(boards.charts.values().filter(|v| v.is_ok()).count(), 5);
        assert!(
            boards
                .charts
                .last_key_value()
                .unwrap()
                .1
                .as_ref()
                .unwrap_err()
                .contains("8 blocks")
        );
    }
    #[test]
    fn endpoint_bar_domain_falls_back_with_entire_source() {
        let source = OPTION
            .replace("\"type\":\"line\"", "\"type\":\"bar\"")
            .replace(
                "\"type\":\"category\"",
                "\"type\":\"category\",\"boundaryGap\":false",
            );
        let (preview, boards) = project_body(&format!("```echarts\n{source}\n```\n"));
        assert!(
            boards.charts[&(0, 0)]
                .as_ref()
                .unwrap_err()
                .contains("boundaryGap=true")
        );
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert_eq!(blocks[0].text, format!("{source}\n"));
    }
    #[test]
    fn invalid_whole_chart_is_retained_without_spending_budget() {
        let body = format!(
            "```echarts\n{{\"dataset\":{{}}}}\n```\n{}",
            fence().repeat(8)
        );
        let (_, boards) = project_body(&body);
        assert_eq!(boards.charts.len(), 9);
        assert_eq!(boards.charts.values().filter(|v| v.is_ok()).count(), 8);
        assert!(boards.charts.first_key_value().unwrap().1.is_err());
    }
}

#[cfg(test)]
mod mindmap_tests {
    use super::*;
    use serde_json::json;
    fn project_body(body: &str, source: Option<&str>) -> (markdown::IncludePreview, Boards) {
        let response =
            crate::included_notes::decode(json!({"markdown":body,"includes":[]}), "host").unwrap();
        let preview = markdown::included_preview(&response);
        let (_, boards) = project_with_source(&preview, Ok(Arc::new(vec![])), source);
        (preview.unwrap(), boards)
    }
    const MAP: &str = "```mindmap\n# Vault\n## Notes\n- [[Syntax]]\n- [[Diagrams|Visuals]]\n## Resources\n- [Graphviz](https://graphviz.org/)\n```\n";
    #[test]
    fn parser_activation_retains_source_and_placement() {
        for body in [
            format!("````text\n{MAP}\n````"),
            format!("<pre>\n{MAP}\n</pre>"),
            "`mindmap`".into(),
        ] {
            assert!(project_body(&body, None).1.mindmaps.is_empty());
        }
        assert!(
            project_body(&MAP.replace("```mindmap", "```mindmap extra"), None)
                .1
                .mindmaps
                .values()
                .all(Result::is_err)
        );
        let quoted = MAP
            .lines()
            .map(|line| format!("> {line}\n"))
            .collect::<String>();
        let (preview, boards) = project_body(&quoted, None);
        let markdown::PreviewItem::Host(blocks) = &preview.items[0] else {
            panic!()
        };
        assert!(blocks[0].quoted);
        assert!(blocks[0].text.contains("[[Diagrams|Visuals]]"));
        assert_eq!(
            boards.mindmaps[&(0, 0)]
                .as_ref()
                .unwrap()
                .nodes
                .iter()
                .map(|n| n.label.as_str())
                .collect::<Vec<_>>(),
            vec![
                "Vault",
                "Notes",
                "Syntax",
                "Visuals",
                "Resources",
                "Graphviz"
            ]
        );
    }
    #[test]
    fn empty_host_uses_captured_source_never_display_or_include_host() {
        let (_, board) = project_body(
            "# Render projection\n```mindmap\n \n```",
            Some("# Captured source\n## Child"),
        );
        assert_eq!(
            board
                .mindmaps
                .values()
                .next()
                .unwrap()
                .as_ref()
                .unwrap()
                .nodes[0]
                .label,
            "Captured source"
        );
        let response=crate::included_notes::decode(json!({"markdown":"![[Target]]\n","includes":[{"line":0,"source_line":0,"caption":"Target","lines":["```mindmap\n```\n", MAP],"note_id":42,"kind":"note","etag":"rev"}]}),"host").unwrap();
        let preview = markdown::included_preview(&response);
        let (_, board) =
            project_with_source(&preview, Ok(Arc::new(vec![])), Some("# Never borrow"));
        assert_eq!(board.mindmaps.len(), 2);
        assert!(
            board
                .mindmaps
                .values()
                .next()
                .unwrap()
                .as_ref()
                .unwrap_err()
                .contains("surrounding source unavailable")
        );
        assert!(board.mindmaps.values().nth(1).unwrap().is_ok());
    }
    #[test]
    fn mindmaps_share_the_eight_visual_budget() {
        let mut body = "```taskboard\n```\n".repeat(2);
        body.push_str("```track-view\n{\"layout\":\"list\",\"columns\":[],\"groups\":[]}\n```\n");
        body.push_str(&MAP.repeat(6));
        let (_, board) = project_body(&body, None);
        assert_eq!(board.mindmaps.values().filter(|v| v.is_ok()).count(), 5);
        assert!(
            board
                .mindmaps
                .values()
                .last()
                .unwrap()
                .as_ref()
                .unwrap_err()
                .contains("8 blocks")
        );
    }
}
