//! Explicit plain-text transfer from an immutable accepted preview, never the raw reader.
use super::*;
use std::{collections::BTreeMap, sync::Arc};
type Key = (usize, usize);
const MAX_BYTES: usize = 256 * 1024;

/// Applied exactly once at accepted preview ingestion, before rich projections.
/// Raw parsing, raw-reader Copy and engine responses are deliberately unchanged.
pub(super) fn normalize(preview: &mut Result<markdown::IncludePreview, String>) {
    let Ok(preview) = preview else { return };
    for item in &mut preview.items {
        let blocks = match item {
            markdown::PreviewItem::Host(blocks) | markdown::PreviewItem::Include { blocks, .. } => {
                blocks
            }
        };
        for block in blocks {
            if block.kind == markdown::BlockKind::Code && block.text.ends_with('\n') {
                block.text.pop();
                let len = block.text.len();
                for span in &mut block.spans {
                    span.range.end = span.range.end.min(len);
                }
                block.spans.retain(|span| span.range.start < span.range.end);
                block.syntax = Arc::from(
                    crate::code_highlight::classify(&block.text, block.language.as_deref())
                        .as_ref(),
                );
            }
        }
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Origin {
    entry: usize,
    vault: String,
    note: Option<i64>,
    line: u64,
    source_line: i64,
    etag: Option<String>,
}
fn origin(snapshot: &Snapshot, item: usize) -> Option<Option<Origin>> {
    match snapshot.preview.as_ref().ok()?.items.get(item)? {
        markdown::PreviewItem::Host(_) => Some(None),
        markdown::PreviewItem::Include { index, .. } => {
            let e = snapshot.response.entries.get(*index)?;
            if e.note_id.is_none() || e.error.as_ref().is_some_and(|e| !e.is_empty()) {
                return None;
            }
            Some(Some(Origin {
                entry: *index,
                vault: e.vault.clone(),
                note: e.note_id,
                line: e.line,
                source_line: e.source_line,
                etag: e.etag.clone(),
            }))
        }
    }
}
fn block(snapshot: &Snapshot, key: Key) -> Option<&markdown::Block> {
    let blocks = match snapshot.preview.as_ref().ok()?.items.get(key.0)? {
        markdown::PreviewItem::Host(blocks) | markdown::PreviewItem::Include { blocks, .. } => {
            blocks
        }
    };
    blocks.get(key.1)
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Ticket {
    key: Key,
    origin: Option<Origin>,
    payload: Arc<str>,
    language: Option<String>,
    fence_info: Option<String>,
}
fn tickets(snapshot: &Snapshot) -> BTreeMap<Key, Arc<Ticket>> {
    let mut result = BTreeMap::new();
    if let Ok(preview) = &snapshot.preview {
        for (item, section) in preview.items.iter().enumerate() {
            let Some(origin) = origin(snapshot, item) else {
                continue;
            };
            let blocks = match section {
                markdown::PreviewItem::Host(blocks)
                | markdown::PreviewItem::Include { blocks, .. } => blocks,
            };
            for (index, block) in blocks.iter().enumerate() {
                if block.kind == markdown::BlockKind::Code
                    && block.language.is_some()
                    && block.text.len() <= MAX_BYTES
                {
                    let key = (item, index);
                    result.insert(
                        key,
                        Arc::new(Ticket {
                            key,
                            origin: origin.clone(),
                            payload: Arc::from(block.text.as_str()),
                            language: block.language.clone(),
                            fence_info: block.fence_info.clone(),
                        }),
                    );
                }
            }
        }
    }
    result
}
#[derive(Default)]
pub(super) struct CopyUi {
    tickets: BTreeMap<Key, Arc<Ticket>>,
    focus: BTreeMap<Key, gpui::FocusHandle>,
    feedback: BTreeMap<Key, &'static str>,
    feedback_serial: u64,
    stamps: BTreeMap<Key, u64>,
    // At most one cancelable reset per eligible block. Never spawned by render.
    resets: BTreeMap<Key, gpui::Task<()>>,
}
impl CopyUi {
    pub(super) fn prepare(snapshot: Option<&Snapshot>, cx: &mut Context<Workspace>) -> Self {
        let tickets = snapshot.map(tickets).unwrap_or_default();
        let focus = tickets
            .keys()
            .map(|&key| (key, cx.focus_handle()))
            .collect();
        Self {
            tickets,
            focus,
            ..Default::default()
        }
    }
}
fn activate(
    state: &mut IncludesState,
    gate: QueryPageGate,
    identity: &Identity,
    serial: u64,
    epoch: u64,
    ticket: &Ticket,
    sink: impl FnOnce(&str) -> Result<(), ()>,
) -> bool {
    if !gate.allowed()
        || !state.accepts(identity, serial)
        || !state.preview_open
        || state.preview_epoch != epoch
        || state.copy.tickets.get(&ticket.key).map(Arc::as_ref) != Some(ticket)
    {
        return false;
    }
    let Some(Ok(snapshot)) = &state.result else {
        return false;
    };
    let Some(block) = block(snapshot, ticket.key) else {
        return false;
    };
    if origin(snapshot, ticket.key.0) != Some(ticket.origin.clone())
        || block.kind != markdown::BlockKind::Code
        || block.language != ticket.language
        || block.fence_info != ticket.fence_info
        || block.text.as_str() != ticket.payload.as_ref()
        || ticket.payload.len() > MAX_BYTES
    {
        return false;
    }
    // Calendar metadata has a local disclosure. Its hidden control is not authorized.
    if snapshot
        .queries
        .get(&ticket.key)
        .is_some_and(|v| matches!(v, Ok(v) if v.layout == crate::query_view::Layout::Calendar))
        && !state.query_sources.contains(&ticket.key)
    {
        return false;
    }
    let feedback = if sink(&ticket.payload).is_ok() {
        "Copy requested"
    } else {
        "Copy failed"
    };
    state.copy.feedback_serial = state.copy.feedback_serial.wrapping_add(1);
    state
        .copy
        .stamps
        .insert(ticket.key, state.copy.feedback_serial);
    state.copy.feedback.insert(ticket.key, feedback);
    true
}
fn expire(
    state: &mut IncludesState,
    identity: &Identity,
    serial: u64,
    epoch: u64,
    key: Key,
    stamp: u64,
) -> bool {
    if !state.accepts(identity, serial)
        || !state.preview_open
        || state.preview_epoch != epoch
        || state.copy.stamps.get(&key) != Some(&stamp)
    {
        return false;
    }
    state.copy.feedback.remove(&key);
    state.copy.stamps.remove(&key);
    true
}
impl Workspace {
    fn copy_preview(
        &mut self,
        identity: &Identity,
        serial: u64,
        epoch: u64,
        ticket: &Ticket,
        cx: &mut Context<Self>,
    ) {
        let gate = self.query_control_gate(identity, cx);
        let Some(state) = self.shell.includes.as_mut() else {
            return;
        };
        if !activate(state, gate, identity, serial, epoch, ticket, |text| {
            cx.write_to_clipboard(gpui::ClipboardItem::new_string(text.to_owned()));
            Ok(()) // GPUI reports submission only, not OS clipboard completion.
        }) {
            return;
        }
        let identity = identity.clone();
        let key = ticket.key;
        let stamp = state.copy.feedback_serial;
        let task = cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(std::time::Duration::from_millis(1500))
                .await;
            let _ = this.update(cx, |this, cx| {
                let Some(state) = this.shell.includes.as_mut() else {
                    return;
                };
                if expire(state, &identity, serial, epoch, key, stamp) {
                    state.copy.resets.remove(&key);
                    cx.notify();
                }
            });
        });
        self.shell
            .includes
            .as_mut()
            .unwrap()
            .copy
            .resets
            .insert(key, task);
        cx.notify();
    }
    pub(super) fn render_preview_copy(
        &self,
        item: usize,
        start: usize,
        blocks: &[markdown::Block],
        dark: bool,
        cx: &mut Context<Self>,
    ) -> gpui::Stateful<Div> {
        let state = self.shell.includes.as_ref().unwrap();
        let mut controls = BTreeMap::new();
        let mut labels = BTreeMap::new();
        let mut captured = BTreeMap::new();
        for (local, block) in blocks.iter().enumerate() {
            let key = (item, start + local);
            if let (Some(ticket), Some(focus)) =
                (state.copy.tickets.get(&key), state.copy.focus.get(&key))
            {
                controls.insert(
                    local,
                    (focus.clone(), state.copy.feedback.contains_key(&key)),
                );
                let label = state.copy.feedback.get(&key).copied().unwrap_or_else(|| {
                    match block.fence_info.as_deref() {
                        Some("viewspec" | "echarts" | "mindmap" | "taskboard" | "mermaid") => {
                            "Copy source"
                        }
                        _ => "Copy code",
                    }
                });
                labels.insert(local, label.to_owned());
                captured.insert(local, ticket.clone());
            } else if block.language.is_some() && block.text.len() > MAX_BYTES {
                labels.insert(local, "Copy unavailable: exceeds 256 KiB".into());
            }
        }
        let identity = state.identity.clone();
        let serial = state.serial;
        let epoch = state.preview_epoch;
        let weak = cx.entity().downgrade();
        let callbacks = markdown::CodeCallbacks {
            anchors: None,
            controls,
            labels: Some(labels),
            activate: std::rc::Rc::new(move |index, _, cx| {
                if let Some(ticket) = captured.get(&index) {
                    let _ = weak.update(cx, |this, cx| {
                        this.copy_preview(&identity, serial, epoch, ticket, cx)
                    });
                }
            }),
        };
        let generated = blocks.iter().any(|b| {
            matches!(
                b.fence_info.as_deref(),
                Some("track-view" | "track-query-view")
            )
        });
        let find = state.find.presentation(state, item, start, blocks);
        let selection = self.preview_selection_presentation(item, start, blocks, cx);
        div()
            .id(gpui::SharedString::from(format!(
                "preview-code-{serial}-{epoch}-{item}-{start}"
            )))
            .when(generated, |d| {
                d.child("Generated snapshot data (not original query source)")
            })
            .child(markdown::render_preview_selection(
                blocks,
                dark,
                &callbacks,
                find.as_ref()
                    .map(|(matches, active, anchors)| markdown::FindPresentation {
                        matches,
                        active: *active,
                        anchors,
                    }),
                selection.as_ref(),
            ))
            .child(self.excerpt_link_controls(item, start, start + blocks.len(), cx))
            .child(self.host_link_controls(item, start, start + blocks.len(), cx))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn fixture(body: &str, includes: serde_json::Value) -> IncludesState {
        let identity = Identity {
            source: LinkSource {
                note: live::NoteKey {
                    vault: "host".into(),
                    note_id: 1,
                },
                etag: "rev".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            route: live::NoteKey {
                vault: "host".into(),
                note_id: 1,
            },
        };
        let response =
            crate::included_notes::decode(json!({"markdown":body,"includes":includes}), "host")
                .unwrap();
        let mut preview = markdown::included_preview(&response);
        normalize(&mut preview);
        let queries = project_queries(&preview);
        let snapshot = Snapshot {
            query_positions: Ok(Default::default()),
            inline: Ok(Default::default()),
            response,
            preview,
            queries,
            boards: Default::default(),
        };
        let mut state = IncludesState::new(identity);
        state.copy.tickets = tickets(&snapshot);
        state.result = Some(Ok(snapshot));
        state.open = true;
        state.preview_open = true;
        state.serial = 10;
        state.preview_epoch = 20;
        state
    }
    fn gate() -> QueryPageGate {
        QueryPageGate {
            preview_active: true,
            source_matches: true,
            ..Default::default()
        }
    }
    fn send(state: &mut IncludesState, ticket: &Ticket, writes: &mut Vec<String>) -> bool {
        activate(
            state,
            gate(),
            &state.identity.clone(),
            10,
            20,
            ticket,
            |text| {
                writes.push(text.into());
                Ok(())
            },
        )
    }
    #[test]
    fn exact_parser_payloads_and_unchanged_raw_reader() {
        for (source, expected) in [
            ("```lua\nprint(\"hello\")\n```", "print(\"hello\")"),
            ("```lua options\n  a\n\n```", "  a\n"),
            ("```lua\n```", ""),
            ("```lua\n\n```", ""),
            ("```lua\r\n\t日本語 🦀  \r\n```", "\t日本語 🦀  "),
            ("> ```lua\n>  a\n> \n> ```", " a\n"),
            ("- list\n\n  ```lua\n  \t日本語\n  ```", "\t日本語"),
            ("```text\na\rb\n```", "a\rb"),
            (
                "```text\n$(echo no) <script> https://example.test\n```",
                "$(echo no) <script> https://example.test",
            ),
        ] {
            let mut state = fixture(source, json!([]));
            let ticket = state.copy.tickets.values().next().unwrap().clone();
            let mut writes = Vec::new();
            assert!(state.copy.feedback.is_empty());
            assert!(writes.is_empty());
            assert!(send(&mut state, &ticket, &mut writes));
            assert_eq!(writes, [expected], "{source}");
            assert_eq!(state.copy.feedback[&ticket.key], "Copy requested");
            assert!(send(&mut state, &ticket, &mut writes));
            assert_eq!(writes.len(), 2);
        }
        let raw = markdown::parse("```lua\na\n```");
        assert_eq!(raw[0].text, "a\n");
        assert!(
            fixture("```\nplain\n```\n\n    indented\n", json!([]))
                .copy
                .tickets
                .is_empty()
        );
    }
    #[test]
    fn included_origin_is_distinct_and_reorder_or_missing_mapping_rejects() {
        let mut state = fixture(
            "![[A]]\n\n![[B]]",
            json!([
                {"line":0,"source_line":2,"note_id":2,"kind":"note","etag":"a","vault":"one","caption":"A","lines":["```lua\nsame\n```"]},
                {"line":2,"source_line":3,"note_id":3,"kind":"note","etag":"b","vault":"two","caption":"B","lines":["```lua\nsame\n```"]}
            ]),
        );
        let values: Vec<_> = state.copy.tickets.values().cloned().collect();
        assert_eq!(values.len(), 2);
        assert_ne!(values[0].origin, values[1].origin);
        assert_eq!(values[0].payload, values[1].payload);
        let mut writes = Vec::new();
        for ticket in &values {
            assert!(send(&mut state, ticket, &mut writes));
        }
        state
            .result
            .as_mut()
            .unwrap()
            .as_mut()
            .unwrap()
            .response
            .entries
            .swap(0, 1);
        assert!(!send(&mut state, &values[0], &mut writes));
        state
            .result
            .as_mut()
            .unwrap()
            .as_mut()
            .unwrap()
            .response
            .entries
            .clear();
        assert!(!send(&mut state, &values[1], &mut writes));
        assert_eq!(writes, ["same", "same"]);
    }
    #[test]
    fn inclusive_utf8_limit_no_truncation_and_failure_is_not_success() {
        for (body, count) in [
            ("é".repeat(MAX_BYTES / 2), 1),
            (format!("{}x", "é".repeat(MAX_BYTES / 2)), 0),
        ] {
            let state = fixture(&format!("```text\n{body}\n```"), json!([]));
            assert_eq!(state.copy.tickets.len(), count);
            assert_eq!(
                block(state.result.as_ref().unwrap().as_ref().unwrap(), (0, 0))
                    .unwrap()
                    .text,
                body
            );
        }
        let mut state = fixture("```text\ninput\n```", json!([]));
        let ticket = state.copy.tickets[&(0, 0)].clone();
        let id = state.identity.clone();
        assert!(activate(&mut state, gate(), &id, 10, 20, &ticket, |_| Err(
            ()
        )));
        assert_eq!(state.copy.feedback[&(0, 0)], "Copy failed");
        assert_eq!(ticket.payload.as_ref(), "input");
    }
    #[test]
    fn every_identity_and_lifecycle_dimension_rejects_without_writes() {
        for dimension in 0..22 {
            let mut state = fixture("```lua\nsame\n```", json!([]));
            let mut ticket = state.copy.tickets[&(0, 0)].as_ref().clone();
            let mut id = state.identity.clone();
            let mut g = gate();
            match dimension {
                0 => id.source.request += 1,
                1 => id.source.launch_generation += 1,
                2 => id.source.storage_generation += 1,
                3 => id.source.etag.push('x'),
                4 => id.source.note.note_id += 1,
                5 => id.source.note.vault.push('x'),
                6 => id.route.vault.push('x'),
                7 => state.serial += 1,
                8 => state.preview_epoch += 1,
                9 => state.open = false,
                10 => state.preview_open = false,
                11 => g.pending = true,
                12 => g.editing = true,
                13 => g.closing = true,
                14 => g.source_matches = false,
                15 => ticket.key.0 += 1,
                16 => ticket.key.1 += 1,
                17 => ticket.payload = Arc::from("changed"),
                18 => state.result = None,
                19 => g.handoff = true,
                20 => g.preview_active = false,
                _ => g.composing = true,
            }
            let mut writes = 0;
            assert!(
                !activate(&mut state, g, &id, 10, 20, &ticket, |_| {
                    writes += 1;
                    Ok(())
                }),
                "dimension {dimension}"
            );
            assert_eq!(writes, 0);
            assert!(state.copy.feedback.is_empty());
        }
    }
    #[test]
    fn rich_source_retains_input_and_calendar_hidden_source_cannot_copy() {
        let source = r#"{"layout":"calendar","columns":[],"groups":[]}"#;
        let mut state = fixture(&format!("```track-view\n{source}\n```"), json!([]));
        let ticket = state.copy.tickets[&(0, 0)].clone();
        let mut writes = Vec::new();
        assert!(!send(&mut state, &ticket, &mut writes));
        state.query_sources.insert((0, 0));
        assert!(send(&mut state, &ticket, &mut writes));
        assert_eq!(writes, [source]);
        for language in ["viewspec", "echarts", "mindmap", "mermaid"] {
            let mut state = fixture(&format!("```{language}\ninvalid input\n```"), json!([]));
            let ticket = state.copy.tickets[&(0, 0)].clone();
            let mut writes = Vec::new();
            assert!(send(&mut state, &ticket, &mut writes));
            assert_eq!(writes, ["invalid input"]);
        }
    }
    #[test]
    fn feedback_is_per_block_generation_and_latest_activation() {
        let mut state = fixture("```lua\na\n```\n\n```lua\nb\n```", json!([]));
        let tickets: Vec<_> = state.copy.tickets.values().cloned().collect();
        let mut writes = Vec::new();
        let id = state.identity.clone();
        assert!(send(&mut state, &tickets[0], &mut writes));
        let first = state.copy.feedback_serial;
        assert!(send(&mut state, &tickets[1], &mut writes));
        let second = state.copy.feedback_serial;
        assert!(send(&mut state, &tickets[0], &mut writes));
        let repeated = state.copy.feedback_serial;
        assert!(!expire(&mut state, &id, 10, 20, tickets[0].key, first));
        assert!(!expire(&mut state, &id, 10, 19, tickets[0].key, repeated));
        assert!(expire(&mut state, &id, 10, 20, tickets[1].key, second));
        assert_eq!(state.copy.feedback.len(), 1);
        assert!(expire(&mut state, &id, 10, 20, tickets[0].key, repeated));
        assert!(state.copy.feedback.is_empty());
        assert!(send(&mut state, &tickets[0], &mut writes));
        state.preview_open = false;
        let stamp = state.copy.feedback_serial;
        assert!(!expire(&mut state, &id, 10, 20, tickets[0].key, stamp));
        state.copy = Default::default();
        state.preview_open = true;
        state.preview_epoch += 1;
        assert!(!send(&mut state, &tickets[0], &mut writes));
    }
}
