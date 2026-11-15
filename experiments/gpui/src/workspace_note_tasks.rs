//! Read-only projection of task lines shipped with the accepted reader source.
use super::*;
use crate::shell::LinkSource;
use gpui::Div;

pub(crate) struct NoteTasksState {
    source: LinkSource,
    body: String,
    invalidated: bool,
    metadata: Option<serde_json::Value>,
    open: bool,
    page: usize,
    scope_revision: u64,
    epoch: u64,
    sort: Option<crate::task_date_sort::Order>,
    sort_rows: Option<Vec<crate::task_date_sort::Row>>,
    indices: Vec<usize>,
    items: Result<std::sync::Arc<Vec<live::NoteTask>>, String>,
}
fn next_epoch() -> u64 {
    static EPOCH: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1);
    EPOCH.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
}
fn sort_snapshot_current(
    epoch: u64,
    captured_epoch: u64,
    scope: u64,
    captured_scope: u64,
    body: &str,
    current_body: &str,
    metadata: Option<&serde_json::Value>,
    current_metadata: Option<&serde_json::Value>,
) -> bool {
    epoch == captured_epoch
        && scope == captured_scope
        && body == current_body
        && metadata == current_metadata
}
fn source_matches(
    source: &LinkSource,
    request: Option<&live::NoteKey>,
    active: Option<&tabs::TabKey>,
) -> bool {
    request.is_some_and(|request| request.note_id == source.note.note_id
        && (request.vault.is_empty() || request.vault == source.note.vault)
        && matches!(active, Some(tabs::TabKey::Note {vault,note_id}) if vault == &request.vault && *note_id == request.note_id))
}
fn page_range(total: usize, requested: usize) -> (usize, usize, std::ops::Range<usize>) {
    let pages = total.div_ceil(100);
    let page = requested.min(pages.saturating_sub(1));
    let start = page * 100;
    (page, pages, start..(start + 100).min(total))
}

impl Workspace {
    fn tasks_source(&self) -> Option<LinkSource> {
        if self.shell.accepted_title_request != Some(self.request)
            || self.editing_ui.active()
            || self.editing_ui.has_pending()
            || self.shell.home
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.read_error.is_some()
        {
            return None;
        }
        let source = self.current_link_source()?;
        if !source_matches(
            &source,
            self.current_note_request.as_ref(),
            self.tabs.active().map(|t| &t.key),
        ) {
            return None;
        }
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if storage.phase != "ready" || storage.generation != source.storage_generation {
            return None;
        }
        Some(source)
    }
    fn tasks_control_available(
        &self,
        source: &LinkSource,
        epoch: u64,
        scope: u64,
        window: &Window,
        cx: &App,
        require_open: bool,
    ) -> bool {
        window.is_window_active()
            && self.tasks_source().as_ref() == Some(source)
            && self.footnote_reader_available(cx)
            && !self.reader_preview_routing_blocked(cx)
            && !self.reader_selection_dragging()
            && !self.shell.find.is_composing(cx)
            && self.shell.scope_revision == scope
            && self.shell.note_tasks.as_ref().is_some_and(|s| {
                (!require_open || s.open)
                    && self.current_note.as_ref().is_some_and(|n| {
                        sort_snapshot_current(
                            s.epoch,
                            epoch,
                            s.scope_revision,
                            scope,
                            &s.body,
                            &n.body,
                            s.metadata.as_ref(),
                            n.note.additional.get("tasks"),
                        )
                    })
            })
    }
    pub(crate) fn accepted_tasks(&mut self) -> Result<std::sync::Arc<Vec<live::NoteTask>>, String> {
        let source = self.tasks_source().ok_or("Task source unavailable")?;
        let note = self.current_note.as_ref().unwrap();
        let invalidated = note
            .note
            .additional
            .get("tasks")
            .is_some_and(serde_json::Value::is_null);
        if self.shell.note_tasks.as_ref().is_none_or(|s| {
            s.source != source
                || s.body != note.body
                || s.invalidated != invalidated
                || s.scope_revision != self.shell.scope_revision
                || s.metadata.as_ref() != note.note.additional.get("tasks")
        }) {
            let items = live::note_tasks(
                self.current_note
                    .as_ref()
                    .unwrap()
                    .note
                    .additional
                    .get("tasks"),
            )
            .map(std::sync::Arc::new)
            .map_err(|e| e.to_string());
            let sort_rows = items
                .as_ref()
                .ok()
                .and_then(|tasks| markdown::sortable_task_rows(&note.body, &self.content, tasks));
            let indices = (0..items.as_ref().map_or(0, |v| v.len())).collect();
            self.shell.note_tasks = Some(NoteTasksState {
                source: source.clone(),
                body: note.body.clone(),
                invalidated,
                metadata: note.note.additional.get("tasks").cloned(),
                open: false,
                page: 0,
                scope_revision: self.shell.scope_revision,
                epoch: next_epoch(),
                sort: None,
                sort_rows,
                indices,
                items,
            });
        }
        self.shell.note_tasks.as_ref().unwrap().items.clone()
    }
    pub(crate) fn note_tasks_inspector(&mut self, cx: &mut Context<Self>) -> Div {
        let mut view = div().mb_3().text_size(px(12.));
        let Some(source) = self.tasks_source() else {
            self.shell.note_tasks = None;
            return view;
        };
        let _ = self.accepted_tasks();
        let state = self.shell.note_tasks.as_ref().unwrap();
        let open = state.open;
        let captured = source.clone();
        let epoch = state.epoch;
        let scope = state.scope_revision;
        view = view.child(
            div()
                .id("reader-tasks-toggle")
                .cursor_pointer()
                .child(if open {
                    "▾ Note tasks (read-only)"
                } else {
                    "▸ Note tasks (read-only)"
                })
                .on_click(cx.listener(move |this, _, window, cx| {
                    if !this.tasks_control_available(&captured, epoch, scope, window, cx, false) {
                        return;
                    }
                    if let Some(state) = this
                        .shell
                        .note_tasks
                        .as_mut()
                        .filter(|s| s.source == captured)
                    {
                        state.open = !state.open;
                        state.page = 0;
                        state.epoch = next_epoch();
                    }
                    cx.notify();
                })),
        );
        if !open {
            return view;
        }
        let state = self.shell.note_tasks.as_ref().unwrap();
        let items = match &state.items {
            Ok(items) => items,
            Err(error) => return view.child(format!("Task metadata unavailable: {error}")),
        };
        if items.is_empty() {
            return view.child("No parsed task lines in this accepted source.");
        }
        if state.sort_rows.is_some() {
            let mut sort_controls = div()
                .flex()
                .flex_wrap()
                .gap_2()
                .child("Inspector order (body unchanged):");
            for (label, field) in [
                ("SCHED", crate::task_date_sort::Field::Scheduled),
                ("DUE", crate::task_date_sort::Field::Due),
            ] {
                let captured = source.clone();
                let epoch = state.epoch;
                let scope = state.scope_revision;
                let previous = state.sort;
                let direction = previous.filter(|o| o.field == field).map(|o| o.direction);
                let suffix = match direction {
                    Some(crate::task_date_sort::Direction::Ascending) => " ↑",
                    Some(crate::task_date_sort::Direction::Descending) => " ↓",
                    _ => " · source",
                };
                sort_controls = sort_controls.child(
                    div()
                        .id(gpui::SharedString::from(format!("note-task-sort-{label}")))
                        .cursor_pointer()
                        .child(format!("{label}{suffix}"))
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if !this
                                .tasks_control_available(&captured, epoch, scope, window, cx, true)
                            {
                                return;
                            }
                            let state = this.shell.note_tasks.as_mut().unwrap();
                            if state.sort != previous {
                                return;
                            }
                            let Some(rows) = &state.sort_rows else {
                                return;
                            };
                            let order = crate::task_date_sort::Order::next(previous, field);
                            let Some(blocks) = crate::task_date_sort::permutation(rows, order)
                            else {
                                return;
                            };
                            let Ok(items) = &state.items else {
                                return;
                            };
                            let indices: Option<Vec<_>> = blocks
                                .iter()
                                .map(|block| {
                                    let line = rows.iter().find(|r| r.block == *block)?.line;
                                    items.iter().position(|t| t.line == line)
                                })
                                .collect();
                            let Some(indices) = indices else {
                                return;
                            };
                            state.epoch = next_epoch();
                            state.sort = Some(order);
                            state.indices = indices;
                            state.page = 0;
                            cx.notify();
                        })),
                );
            }
            view = view.child(sort_controls);
        } else {
            view = view.child("Date sorting unavailable: requires one fully matched top-level rich task list with valid dates.");
        }
        let (page, pages, range) = page_range(items.len(), state.page);
        let mut controls = div().flex().gap_3().child(format!(
            "{} tasks · page {}/{} · source lines, not navigation",
            items.len(),
            page + 1,
            pages
        ));
        for (label, target) in [
            ("Previous", page.checked_sub(1)),
            ("Next", (page + 1 < pages).then_some(page + 1)),
        ] {
            if let Some(target) = target {
                let captured = source.clone();
                let epoch = state.epoch;
                let scope = state.scope_revision;
                controls = controls.child(
                    div()
                        .id(if label == "Previous" {
                            "tasks-prev"
                        } else {
                            "tasks-next"
                        })
                        .cursor_pointer()
                        .child(label)
                        .on_click(cx.listener(move |this, _, window, cx| {
                            if !this
                                .tasks_control_available(&captured, epoch, scope, window, cx, true)
                            {
                                return;
                            }
                            if let Some(state) = this
                                .shell
                                .note_tasks
                                .as_mut()
                                .filter(|s| s.source == captured && s.open)
                            {
                                state.page = target;
                                state.epoch = next_epoch();
                            }
                            cx.notify();
                        })),
                );
            }
        }
        let mut rows = div()
            .id("reader-tasks-scroll")
            .max_h(px(240.))
            .overflow_y_scroll();
        for index in &state.indices[range] {
            let task = &items[*index];
            let mut metadata = format!("L{} · {}", task.line, task.state);
            for (label, value) in [
                ("priority", &task.priority),
                ("scheduled", &task.scheduled),
                ("due", &task.due),
                ("completed", &task.completed),
            ] {
                if !value.is_empty() {
                    metadata.push_str(&format!(" · {label}: {value}"));
                }
            }
            rows = rows.child(
                div()
                    .mb_2()
                    .child(metadata)
                    .child(div().child(task.text.clone())),
            );
        }
        view.child(controls).child(rows)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn sort_snapshot_rejects_old_scope_epoch_body_and_metadata() {
        let metadata = serde_json::json!({"items":[]});
        assert!(sort_snapshot_current(
            3,
            3,
            7,
            7,
            "body",
            "body",
            Some(&metadata),
            Some(&metadata)
        ));
        assert!(!sort_snapshot_current(
            4,
            3,
            7,
            7,
            "body",
            "body",
            Some(&metadata),
            Some(&metadata)
        ));
        assert!(!sort_snapshot_current(
            3,
            3,
            8,
            7,
            "body",
            "body",
            Some(&metadata),
            Some(&metadata)
        ));
        assert!(!sort_snapshot_current(
            3,
            3,
            7,
            7,
            "old",
            "new",
            Some(&metadata),
            Some(&metadata)
        ));
        assert!(!sort_snapshot_current(
            3,
            3,
            7,
            7,
            "body",
            "body",
            Some(&metadata),
            None
        ));
        assert!(!sort_snapshot_current(
            3,
            3,
            7,
            7,
            "body",
            "body",
            Some(&metadata),
            Some(&serde_json::Value::Null)
        ));
    }
    #[test]
    fn note_tasks_local_source_replacement_invalidates_until_fresh_projection() {
        for initial in [
            None,
            Some(
                serde_json::json!({"items":[{"line":1,"state":"TODO","done":false,"text":"old"}]}),
            ),
        ] {
            let mut fields = std::collections::BTreeMap::new();
            if let Some(value) = initial {
                fields.insert("tasks".into(), value);
            }
            live::invalidate_note_tasks(&mut fields);
            assert!(live::note_tasks(fields.get("tasks")).is_err());
            // A later accepted GET replaces the source, with missing metadata
            // legitimately meaning no parsed task lines.
            fields.clear();
            assert!(live::note_tasks(fields.get("tasks")).unwrap().is_empty());
            fields.insert(
                "tasks".into(),
                serde_json::json!({"items":[{"line":2,"state":"DONE","done":true,"text":"fresh"}]}),
            );
            assert_eq!(
                live::note_tasks(fields.get("tasks")).unwrap()[0].text,
                "fresh"
            );
        }
    }
    #[test]
    fn note_tasks_pages_cover_all_rows_and_clamp_or_reset() {
        for total in [0usize, 1, 100, 101, 4096] {
            let pages = total.div_ceil(100);
            let mut visited = Vec::new();
            for page in 0..pages {
                let (actual, count, range) = page_range(total, page);
                assert_eq!((actual, count), (page, pages));
                assert!(range.len() <= 100);
                visited.extend(range);
            }
            assert_eq!(visited, (0..total).collect::<Vec<_>>());
            let (clamped, _, last) = page_range(total, usize::MAX);
            assert_eq!(clamped, pages.saturating_sub(1));
            assert_eq!(last.end, total);
            let (reset, _, first) = page_range(total, 0);
            assert_eq!(reset, 0);
            assert_eq!(first, 0..total.min(100));
        }
    }
    #[test]
    fn note_tasks_identity_requires_accepted_route_and_all_callback_dimensions() {
        let source = LinkSource {
            note: live::NoteKey {
                vault: "work".into(),
                note_id: 7,
            },
            etag: "v1".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        let alias = live::NoteKey {
            vault: String::new(),
            note_id: 7,
        };
        assert!(source_matches(
            &source,
            Some(&alias),
            Some(&tabs::TabKey::Note {
                vault: String::new(),
                note_id: 7
            })
        ));
        assert!(!source_matches(&source, None, Some(&tabs::TabKey::Home)));
        assert!(!source_matches(
            &source,
            Some(&alias),
            Some(&tabs::TabKey::Note {
                vault: "foreign".into(),
                note_id: 7
            })
        ));
        let wrong = live::NoteKey {
            vault: "foreign".into(),
            note_id: 7,
        };
        assert!(!source_matches(
            &source,
            Some(&wrong),
            Some(&tabs::TabKey::Note {
                vault: "foreign".into(),
                note_id: 7
            })
        ));
        for change in 0..6 {
            let mut other = source.clone();
            match change {
                0 => other.note.vault = "foreign".into(),
                1 => other.note.note_id += 1,
                2 => other.etag.push('2'),
                3 => other.storage_generation += 1,
                4 => other.launch_generation += 1,
                _ => other.request += 1,
            }
            assert_ne!(source, other);
        }
    }
    #[test]
    fn note_tasks_projection_preserves_undated_states_dates_and_text() {
        let items = serde_json::json!({"items":[
            {"line":1,"state":"TODO","done":false,"text":"日本語 <raw>"},
            {"line":3,"state":"DOING","done":false,"text":"b","priority":"A","due":"2026-02-30"},
            {"line":4,"state":"WAITING","done":false,"text":"c"},
            {"line":9,"state":"DONE","done":true,"text":"d","completed":"2026-99-99"},
            {"line":10,"state":"CANCELLED","done":true,"text":"e"}]});
        let result = live::note_tasks(Some(&items)).unwrap();
        assert_eq!(result.len(), 5);
        assert_eq!(result[0].text, "日本語 <raw>");
        assert_eq!(result[1].due, "2026-02-30");
        assert!(live::note_tasks(None).unwrap().is_empty());
        assert!(
            live::note_tasks(Some(&serde_json::json!({"items":[]})))
                .unwrap()
                .is_empty()
        );
    }
    #[test]
    fn note_tasks_projection_rejects_malformed_without_source_mutation() {
        for value in [
            serde_json::Value::Null,
            serde_json::json!({}),
            serde_json::json!({"items":null}),
        ] {
            assert!(live::note_tasks(Some(&value)).is_err());
        }
        let row = serde_json::json!({"line":1,"state":"TODO","done":false,"text":"ok"});
        for (key, value) in [
            ("line", serde_json::json!(0)),
            ("state", serde_json::json!("UNKNOWN")),
            ("done", serde_json::json!(true)),
            ("priority", serde_json::json!("aa")),
            ("due", serde_json::json!("tomorrow")),
            ("text", serde_json::json!("x".repeat(65537))),
        ] {
            let mut item = row.clone();
            item[key] = value;
            assert!(live::note_tasks(Some(&serde_json::json!({"items":[item]}))).is_err());
        }
        assert!(
            live::note_tasks(Some(
                &serde_json::json!({"items":[row.clone(),row.clone()]})
            ))
            .is_err()
        );
        assert!(live::note_tasks(Some(&serde_json::json!({"items":vec![row;4097]}))).is_err());
    }
}
