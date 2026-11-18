//! Explicit source-note navigation from a successful owned include header.
#[path = "include_excerpt_links.rs"]
pub(super) mod include_excerpt_links;
use super::*;
#[derive(Clone, Debug, PartialEq, Eq)]
struct Ticket {
    identity: Identity,
    serial: u64,
    epoch: u64,
    item: usize,
    entry_index: usize,
    entry: crate::included_notes::Include,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
}
fn destination(entry: &crate::included_notes::Include) -> Option<live::NoteKey> {
    if entry.error.as_ref().is_some_and(|e| !e.is_empty())
        || !entry.bad_options.is_empty()
        || !matches!(entry.kind.as_deref(), Some("note" | "journal"))
        || entry.etag.as_ref().is_none_or(String::is_empty)
    {
        return None;
    }
    Some(live::NoteKey {
        vault: entry.vault.clone(),
        note_id: entry.note_id.filter(|id| *id > 0)?,
    })
}
fn entry_at(
    state: &IncludesState,
    item: usize,
    entry_index: usize,
) -> Option<&crate::included_notes::Include> {
    let snapshot = state.result.as_ref()?.as_ref().ok()?;
    if !matches!(snapshot.preview.as_ref().ok()?.items.get(item), Some(markdown::PreviewItem::Include { index, .. }) if *index == entry_index)
    {
        return None;
    }
    snapshot.response.entries.get(entry_index)
}
impl Workspace {
    fn include_source_ticket(&self, item: usize, entry_index: usize) -> Option<Ticket> {
        let state = self.shell.includes.as_ref()?;
        let identity = self.includes_identity()?;
        if !state.preview_open
            || state.identity != identity
            || !self.includes_body_current(state)
            || self.shell.accepted_title_request != Some(self.request)
        {
            return None;
        }
        let entry = entry_at(state, item, entry_index)?.clone();
        let key = destination(&entry)?;
        let registry = self.follow_registry()?;
        let canonical = if key.vault.is_empty() {
            &registry.0
        } else {
            &key.vault
        };
        if !registry.1.contains(canonical) || !self.session_ui.permits(&key.vault) {
            return None;
        }
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        if storage.phase != "ready"
            || storage.generation != identity.source.storage_generation
            || self.accepted_storage_generation != Some(storage.generation)
        {
            return None;
        }
        Some(Ticket {
            identity,
            serial: state.serial,
            epoch: state.preview_epoch,
            item,
            entry_index,
            entry,
            scope: self.working_vault_scope().to_owned(),
            scope_revision: self.shell.scope_revision,
            registry,
        })
    }
    fn include_source_current(&self, ticket: &Ticket, cx: &Context<Self>) -> bool {
        self.query_control_gate(&ticket.identity, cx).allowed()
            && self
                .include_source_ticket(ticket.item, ticket.entry_index)
                .as_ref()
                == Some(ticket)
            && !self.recent_open()
            && !self.reader_hierarchy_open()
            && !self.enlarged_image_open()
            && !self.creation_open()
            && !self.reader_selection_dragging()
            && !self.shell.find.is_composing(cx)
            && !self.shell.calendar_zone.is_composing(cx)
    }
    pub(super) fn include_source_control(
        &self,
        item: usize,
        entry_index: usize,
        cx: &Context<Self>,
    ) -> Div {
        let Some(ticket) = self.include_source_ticket(item, entry_index) else {
            return div();
        };
        div().child(
            div()
                .id(gpui::SharedString::from(format!(
                    "include-source-{}-{}-{item}-{entry_index}",
                    ticket.serial, ticket.epoch
                )))
                .cursor_pointer()
                .child("Open source note")
                .on_click(
                    cx.listener(move |this, event: &gpui::ClickEvent, window, cx| {
                        cx.stop_propagation();
                        if !shell::plain_reader_click(event)
                            || !window.is_window_active()
                            || !this.include_source_current(&ticket, cx)
                        {
                            return;
                        }
                        let Some(key) = destination(&ticket.entry) else {
                            return;
                        };
                        if this.guard_transition(workspace_edit::Transition::Note(key.clone()), cx)
                        {
                            return;
                        }
                        this.close_includes_preview(&ticket.identity, ticket.serial, window, cx);
                        this.open_live_key(key, cx);
                    }),
                ),
        )
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn response(vault: Option<&str>) -> crate::included_notes::Response {
        let mut entry = serde_json::json!({"line":0,"source_line":99,"note_id":42,"kind":"note","etag":"revision","caption":"Alias id:999","title":"Not identity","lines":["body"]});
        if let Some(v) = vault {
            entry["vault"] = v.into();
        }
        crate::included_notes::decode(
            serde_json::json!({"markdown":"![[Alias]]","includes":[entry]}),
            "host",
        )
        .unwrap()
    }
    #[test]
    fn exact_identity_ignores_alias_and_preserves_explicit_empty_vault() {
        for (wire, expected) in [(None, "host"), (Some(""), ""), (Some("foreign"), "foreign")] {
            let mut e = response(wire).entries.remove(0);
            let key = destination(&e).unwrap();
            assert_eq!(key.note_id, 42);
            assert_eq!(key.vault, expected);
            e.caption = "other alias".into();
            e.title = Some("999".into());
            e.source_line = -1;
            assert_eq!(destination(&e), Some(key));
        }
    }
    #[test]
    fn incomplete_failed_or_unsupported_entry_has_no_action() {
        let valid = response(None).entries.remove(0);
        for n in 0..6 {
            let mut e = valid.clone();
            match n {
                0 => e.error = Some("missing".into()),
                1 => e.bad_options.push("invalid".into()),
                2 => e.note_id = None,
                3 => e.kind = Some("asset".into()),
                4 => e.etag = None,
                _ => e.note_id = Some(0),
            };
            assert!(destination(&e).is_none());
        }
        let mut journal = valid;
        journal.kind = Some("journal".into());
        assert!(destination(&journal).is_some());
    }
    #[test]
    fn duplicate_source_notes_keep_distinct_accepted_header_occurrences() {
        let mut response = response(None);
        response.markdown = "![[First]]\n\n![[Second]]".into();
        let mut duplicate = response.entries[0].clone();
        duplicate.line = 2;
        duplicate.caption = "Second".into();
        response.entries.push(duplicate);
        let preview = markdown::included_preview(&response);
        let source = shell::LinkSource {
            note: live::NoteKey {
                vault: "host".into(),
                note_id: 1,
            },
            etag: "r".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        let mut state = IncludesState::new(Identity {
            route: source.note.clone(),
            source,
        });
        state.result = Some(Ok(Snapshot {
            query_positions: Ok(Default::default()),
            inline: Ok(Default::default()),
            boards: Default::default(),
            queries: Default::default(),
            response,
            preview,
        }));
        assert_eq!(
            destination(entry_at(&state, 0, 0).unwrap()),
            destination(entry_at(&state, 1, 1).unwrap())
        );
        assert_ne!(
            entry_at(&state, 0, 0).unwrap(),
            entry_at(&state, 1, 1).unwrap()
        );
        assert!(entry_at(&state, 0, 1).is_none());
        assert!(entry_at(&state, 1, 0).is_none());
        assert!(entry_at(&state, 2, 1).is_none());
        state.result = None;
        assert!(entry_at(&state, 0, 0).is_none());
    }
    #[test]
    fn tickets_retain_occurrence_and_source_epoch_not_just_destination() {
        let source = shell::LinkSource {
            note: live::NoteKey {
                vault: "host".into(),
                note_id: 1,
            },
            etag: "r".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        let base = Ticket {
            identity: Identity {
                route: source.note.clone(),
                source,
            },
            serial: 5,
            epoch: 6,
            item: 1,
            entry_index: 0,
            entry: response(None).entries.remove(0),
            scope: "host".into(),
            scope_revision: 7,
            registry: ("main".into(), vec!["main".into(), "host".into()]),
        };
        for n in 0..10 {
            let mut stale = base.clone();
            match n {
                0 => stale.serial += 1,
                1 => stale.epoch += 1,
                2 => stale.item += 1,
                3 => stale.entry_index += 1,
                4 => stale.identity.source.request += 1,
                5 => stale.identity.source.storage_generation += 1,
                6 => stale.scope_revision += 1,
                7 => stale.registry.1.clear(),
                8 => stale.entry.vault.clear(),
                _ => stale.identity.route.note_id += 1,
            };
            assert_ne!(base, stale);
        }
    }
}
