//! Explicit host-prose note links. Display labels never grant destination authority.
use super::*;
use include_navigation::include_excerpt_links::{link_term, resolve_note_link};
use std::rc::Rc;
const MAX_HOST_LINKS: usize = 16;
const MAX_SCANNED_LINKS: usize = 256;
const MAX_HOST_BLOCKS: usize = 8192;
#[derive(Clone, Debug, PartialEq, Eq)]
struct Candidate {
    item: usize,
    block: usize,
    link: markdown::Link,
    label: String,
    fragment: bool,
}
#[derive(Default)]
pub(super) struct Cache {
    stamp: Option<(u64, u64)>,
    links: Rc<Vec<Candidate>>,
}
fn has_fragment(link: &markdown::Link) -> bool {
    match &link.target {
        markdown::LinkTarget::Wiki {
            key,
            fragment: Some(fragment),
            ..
        } => key.len().saturating_add(fragment.len()) <= 4096,
        markdown::LinkTarget::Markdown { destination } => {
            destination.len() <= 4096 && destination.contains('#')
        }
        _ => false,
    }
}
fn host_term(link: &markdown::Link) -> Option<String> {
    match &link.target {
        markdown::LinkTarget::Wiki {
            key,
            fragment: Some(fragment),
            ..
        } => {
            if key.len().saturating_add(fragment.len()) > 4096
                || fragment.chars().any(char::is_control)
                // Validate syntax with the Source reader, without fetching a target.
                || !matches!(
                    markdown::fragment_anchor(&[], &link.target),
                    Err(markdown::FragmentError::MissingHeading(_)
                        | markdown::FragmentError::MissingBlock(_))
                )
            {
                return None;
            }
            link_term(link)
        }
        markdown::LinkTarget::Markdown { .. } if has_fragment(link) => None,
        _ => link_term(link),
    }
}
fn candidates(preview: &markdown::IncludePreview) -> Vec<Candidate> {
    let mut out = Vec::new();
    let mut blocks_seen = 0;
    let mut links_seen = 0;
    'items: for (item, entry) in preview.items.iter().enumerate() {
        let markdown::PreviewItem::Host(blocks) = entry else {
            continue;
        };
        for (block, b) in blocks.iter().enumerate() {
            if blocks_seen == MAX_HOST_BLOCKS {
                break 'items;
            }
            blocks_seen += 1;
            if b.kind != markdown::BlockKind::Paragraph || b.image.is_some() {
                continue;
            }
            for link in &b.links {
                if links_seen == MAX_SCANNED_LINKS {
                    break 'items;
                }
                links_seen += 1;
                let Some(label) = b.text.get(link.range.clone()) else {
                    continue;
                };
                let fragment = has_fragment(link);
                if !fragment && host_term(link).is_none() {
                    continue;
                }
                out.push(Candidate {
                    item,
                    block,
                    link: link.clone(),
                    label: label.chars().take(80).collect(),
                    fragment,
                });
                if out.len() == MAX_HOST_LINKS {
                    break 'items;
                }
            }
        }
    }
    out
}
fn cached(state: &IncludesState) -> Rc<Vec<Candidate>> {
    let stamp = (state.serial, state.preview_epoch);
    let mut cache = state.host_links.borrow_mut();
    if cache.stamp != Some(stamp) {
        cache.stamp = Some(stamp);
        cache.links = Rc::new(
            state
                .result
                .as_ref()
                .and_then(|r| r.as_ref().ok())
                .and_then(|s| s.preview.as_ref().ok())
                .map(candidates)
                .unwrap_or_default(),
        );
    }
    cache.links.clone()
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Ticket {
    identity: Identity,
    serial: u64,
    epoch: u64,
    mode: u64,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
    candidate: Candidate,
}
impl Ticket {
    fn intent(&self, destination: live::NoteKey) -> shell::InternalLinkIntent {
        shell::InternalLinkIntent {
            source: self.identity.source.note.clone(),
            destination,
            target: self.candidate.link.target.clone(),
            launch_generation: self.identity.source.launch_generation,
            storage_generation: self.identity.source.storage_generation,
            allow_epoch_advance: false,
        }
    }
}
fn current_ticket(expected: &Ticket, current: Option<&Ticket>) -> bool {
    current == Some(expected)
}
impl Workspace {
    fn host_link_ticket(&self, candidate: &Candidate) -> Option<Ticket> {
        if host_term(&candidate.link).is_none() {
            return None;
        }
        let state = self.shell.includes.as_ref()?;
        let identity = self.includes_identity()?;
        if !state.preview_open
            || state.identity != identity
            || self.shell.accepted_title_request != Some(self.request)
            || !self.includes_body_current(state)
            || state.accepted_body.is_none()
        {
            return None;
        }
        if !cached(state).contains(candidate) {
            return None;
        }
        let registry = self.follow_registry()?;
        let vault = &identity.source.note.vault;
        let canonical = if vault.is_empty() { &registry.0 } else { vault };
        if !registry.1.contains(canonical) || !self.session_ui.permits(vault) {
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
            mode: self.shell.rendered_reading.generation(),
            scope: self.working_vault_scope().to_owned(),
            scope_revision: self.shell.scope_revision,
            registry,
            candidate: candidate.clone(),
        })
    }
    fn host_link_current(&self, t: &Ticket, cx: &Context<Self>) -> bool {
        current_ticket(t, self.host_link_ticket(&t.candidate).as_ref())
            && self.query_control_gate(&t.identity, cx).allowed()
            && !self.recent_open()
            && !self.reader_hierarchy_open()
            && !self.enlarged_image_open()
            && !self.creation_open()
            && !self.reader_selection_dragging()
            && !self.shell.find.is_composing(cx)
            && !self.shell.calendar_zone.is_composing(cx)
    }
    pub(super) fn host_link_controls(
        &self,
        item: usize,
        start: usize,
        end: usize,
        cx: &mut Context<Self>,
    ) -> Div {
        let Some(state) = self.shell.includes.as_ref() else {
            return div();
        };
        let mut controls = div().flex().flex_col().gap_1();
        for candidate in cached(state)
            .iter()
            .filter(|c| c.item == item && c.block >= start && c.block < end)
        {
            if candidate.fragment {
                if let Some(control) = self.local_host_heading_control(
                    item,
                    candidate.block,
                    &candidate.link,
                    &candidate.label,
                    cx,
                ) {
                    controls = controls.child(control);
                    continue;
                }
                if host_term(&candidate.link).is_none() {
                    controls = controls.child(format!(
                        "Host link fragment unavailable: {}",
                        candidate.label
                    ));
                    continue;
                }
            }
            let Some(ticket) = self.host_link_ticket(candidate) else {
                continue;
            };
            controls = controls.child(
                div()
                    .id(gpui::SharedString::from(format!(
                        "host-link-{}-{}-{item}-{}-{}",
                        ticket.serial, ticket.epoch, candidate.block, candidate.link.range.start
                    )))
                    .text_size(px(12.))
                    .cursor_pointer()
                    .child(if candidate.fragment {
                        format!("Open Source at fragment: {}", candidate.label)
                    } else {
                        format!("Open host link: {}", candidate.label)
                    })
                    .on_click(
                        cx.listener(move |this, event: &gpui::ClickEvent, window, cx| {
                            cx.stop_propagation();
                            if shell::plain_reader_click(event) {
                                this.activate_host_link(ticket.clone(), window, cx);
                            }
                        }),
                    ),
            );
        }
        controls
    }
    fn activate_host_link(&mut self, ticket: Ticket, window: &mut Window, cx: &mut Context<Self>) {
        if !window.is_window_active() || !self.host_link_current(&ticket, cx) {
            return;
        }
        let Some(handle) = cx.active_window().and_then(shortcut_workspace) else {
            return;
        };
        let Some(term) = host_term(&ticket.candidate.link) else {
            return;
        };
        let state = self.shell.includes.as_mut().unwrap();
        let token = SERIAL.fetch_add(1, Ordering::Relaxed);
        if !query_navigation::claim_link_lookup(
            &query_navigation::BUSY,
            &mut state.query_nav.pending,
            &mut state.query_nav.notice,
            token,
        ) {
            cx.notify();
            return;
        }
        let permit = query_navigation::Permit;
        state.query_nav.notice = Some("Resolving host note link…".into());
        let source = ticket.identity.source.clone();
        let client = self.live.as_ref().unwrap().client();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            resolve_note_link(&client, &source, &term)
        });
        cx.spawn(async move |_, cx| {
            let result = job.await;
            let _ = cx.update(|app| {
                let active = app.active_window() == Some(handle.into());
                let _ = handle.update(app, |this, window, cx| {
                    if !this
                        .shell
                        .includes
                        .as_ref()
                        .is_some_and(|s| s.query_nav.pending == Some(token))
                    {
                        return;
                    }
                    this.shell.includes.as_mut().unwrap().query_nav.pending = None;
                    if !active || !window.is_window_active() || !this.host_link_current(&ticket, cx)
                    {
                        this.shell.includes.as_mut().unwrap().query_nav.notice =
                            Some("Host link navigation canceled after its source changed.".into());
                        cx.notify();
                        return;
                    }
                    let key = match result {
                        Ok(key) => key,
                        Err(error) => {
                            this.shell.includes.as_mut().unwrap().query_nav.notice = Some(error);
                            cx.notify();
                            return;
                        }
                    };
                    let canonical = if key.vault.is_empty() {
                        &ticket.registry.0
                    } else {
                        &key.vault
                    };
                    if !ticket.registry.1.contains(canonical)
                        || !this.session_ui.permits(&key.vault)
                    {
                        this.shell.includes.as_mut().unwrap().query_nav.notice =
                            Some("Host link vault is unavailable; no fallback was used.".into());
                        cx.notify();
                        return;
                    }
                    let intent = ticket.intent(key);
                    if this.guard_transition(
                        workspace_edit::Transition::InternalLink(intent.clone()),
                        cx,
                    ) {
                        return;
                    }
                    // Only an authorized activation may retire automatic Rendered mode.
                    // Keep Source visible while its existing fragment landing completes.
                    if ticket.candidate.fragment {
                        this.shell.rendered_reading.disable();
                    }
                    this.close_includes_preview(&ticket.identity, ticket.serial, window, cx);
                    this.open_internal_link(intent, cx);
                });
            });
        })
        .detach();
        cx.notify();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn link(text: &str) -> markdown::Link {
        markdown::parse(text)
            .into_iter()
            .flat_map(|b| b.links)
            .next()
            .unwrap()
    }
    #[test]
    fn host_targets_ignore_alias_and_never_drop_fragments() {
        assert_eq!(
            host_term(&link("[[Destination|Alias]]")).as_deref(),
            Some("Destination")
        );
        assert_eq!(host_term(&link("[[C#|Language]]")).as_deref(), Some("C#"));
        for input in ["[Alias](Note.md#heading)", "[Alias](Note%23heading.md)"] {
            assert!(host_term(&link(input)).is_none(), "{input}");
        }
        let mut oversized = link("[[A#heading]]");
        if let markdown::LinkTarget::Wiki { fragment, .. } = &mut oversized.target {
            *fragment = Some("#".repeat(5000));
        }
        assert!(host_term(&oversized).is_none());
        if let markdown::LinkTarget::Wiki { fragment, .. } = &mut oversized.target {
            *fragment = Some(String::new());
        }
        assert!(host_term(&oversized).is_none());
        assert!(link_term(&link("[[Destination#Heading|Alias]]")).is_some()); // Existing include route unchanged.
    }
    #[test]
    fn wiki_fragment_admission_reuses_source_syntax_and_preserves_exact_payload() {
        for (input, term, fragment) in [
            ("[[Target##Section|Go]]", "Target", "##Section"),
            (
                "[[id:200###日本語 Hello!|Old title]]",
                "id:200",
                "###日本語 Hello!",
            ),
            (
                "[[remote:id:200#^block-a|Details]]",
                "remote:id:200",
                "#^block-a",
            ),
        ] {
            let parsed = link(input);
            assert_eq!(host_term(&parsed).as_deref(), Some(term), "{input}");
            let mut ticket = ticket();
            ticket.candidate.link = parsed.clone();
            ticket.candidate.fragment = true;
            let destination = live::NoteKey {
                vault: "remote".into(),
                note_id: 200,
            };
            let intent = ticket.intent(destination.clone());
            assert_eq!(intent.target, parsed.target);
            assert_eq!(intent.source, ticket.identity.source.note);
            assert_eq!(intent.destination, destination);
            assert!(!intent.allow_epoch_advance);
            assert!(
                matches!(&intent.target, markdown::LinkTarget::Wiki { fragment: Some(f), .. } if f == fragment)
            );
        }
        for fragment in [
            "",
            "#",
            "###",
            "#%53ection",
            "#h-section",
            "#^",
            "#^bad id",
            "#line\nbreak",
            "#nul\0byte",
        ] {
            let mut parsed = link("[[Target#Section]]");
            if let markdown::LinkTarget::Wiki {
                fragment: value, ..
            } = &mut parsed.target
            {
                *value = Some(fragment.into());
            }
            assert!(host_term(&parsed).is_none(), "{fragment:?}");
        }
    }
    #[test]
    fn alias_does_not_change_key_fragment_or_resolved_namespace() {
        let mut first = ticket();
        first.candidate.link = link("[[remote:id:200##Section|Old title]]");
        let mut renamed = first.clone();
        renamed.candidate.link = link("[[remote:id:200##Section|New label]]");
        let destination = live::NoteKey {
            vault: "remote".into(),
            note_id: 200,
        };
        assert_eq!(
            host_term(&first.candidate.link).as_deref(),
            Some("remote:id:200")
        );
        assert_eq!(
            first.intent(destination.clone()),
            renamed.intent(destination)
        );
    }
    #[test]
    fn fragment_terms_keep_existing_key_safety_and_combined_size_bound() {
        for key in [
            "",
            "javascript:run",
            "file:secret",
            "data:text",
            "https://example.invalid",
            "bad\nkey",
        ] {
            let parsed = markdown::Link {
                range: 0..1,
                target: markdown::LinkTarget::Wiki {
                    key: key.into(),
                    fragment: Some("#Section".into()),
                    authored_label: false,
                },
            };
            assert!(host_term(&parsed).is_none(), "{key:?}");
        }
        let mut parsed = link("[[A#Section]]");
        if let markdown::LinkTarget::Wiki { key, .. } = &mut parsed.target {
            *key = "a".repeat(4096 - "#Section".len());
        }
        assert!(host_term(&parsed).is_some());
        if let markdown::LinkTarget::Wiki { key, .. } = &mut parsed.target {
            key.push('a');
        }
        assert!(host_term(&parsed).is_none());
    }
    #[test]
    fn fragment_resolution_requires_exact_source_term_and_authoritative_found_response() {
        let mut ticket = ticket();
        ticket.candidate.link = link("[[foreign:id:200#^block-a|Alias]]");
        let term = host_term(&ticket.candidate.link).unwrap();
        let mut response: live::Resolution = serde_json::from_value(serde_json::json!({
            "found": true, "note": {"note_id":200,"title":"Current title","file_kind":"note","vault":"foreign"}
        })).unwrap();
        response.source_vault = ticket.identity.source.note.vault.clone();
        response.launch_generation = ticket.identity.source.launch_generation;
        response.term = term.clone();
        let key =
            query_navigation::verified_key(&response, &ticket.identity.source, &term).unwrap();
        assert_eq!(key.vault, "foreign");
        assert_eq!(key.note_id, 200);
        assert_eq!(ticket.intent(key).target, ticket.candidate.link.target);
        for change in 0..4 {
            let mut rejected = response.clone();
            match change {
                0 => rejected.found = false,
                1 => rejected.source_vault = "foreign".into(),
                2 => rejected.term = "Alias".into(),
                _ => rejected.launch_generation += 1,
            }
            assert!(
                query_navigation::verified_key(&rejected, &ticket.identity.source, &term).is_err()
            );
        }
    }
    #[test]
    fn accepted_wiki_fragments_use_existing_source_landings_and_missing_diagnostics() {
        let blocks = markdown::parse_reader(
            "# Section\n\n## Section\n\nfirst ^block-a\n\nsecond ^block-a\n\n```text\ncode ^block-a\n```\n\n^detached",
        );
        let heading = link("[[Target##Section|Go]]");
        assert_eq!(
            markdown::fragment_anchor(&blocks, &heading.target),
            Ok(Some(markdown::outline(&blocks)[0].anchor.clone()))
        );
        let block = link("[[Target#^block-a|Details]]");
        assert_eq!(
            markdown::fragment_anchor(&blocks, &block.target),
            Ok(Some("^block-a".into()))
        );
        assert_eq!(
            markdown::authored_block_targets(&blocks).get("^block-a"),
            Some(&2)
        );
        for (input, error) in [
            (
                "[[Target#Missing]]",
                markdown::FragmentError::MissingHeading("Missing".into()),
            ),
            (
                "[[Target#^detached]]",
                markdown::FragmentError::MissingBlock("detached".into()),
            ),
        ] {
            let parsed = link(input);
            assert_eq!(host_term(&parsed).as_deref(), Some("Target"));
            assert_eq!(
                markdown::fragment_anchor(&blocks, &parsed.target),
                Err(error)
            );
        }
    }
    #[test]
    fn conservative_markdown_conversion_and_unsafe_targets() {
        assert_eq!(
            host_term(&link("[Alias](Caf%C3%A9.MD)")).as_deref(),
            Some("Café")
        );
        assert_eq!(
            host_term(&link("[Alias](Title%2Emd)")).as_deref(),
            Some("Title.md")
        );
        for href in [
            "https://example.com",
            "assets/a.png",
            "../Other.md",
            "/Other.md",
            "Other.md?q=x",
            "bad%ZZ.md",
            "A%2fB.md",
            "A%3aB.md",
            "A%00B.md",
            "data:text/plain,hi",
        ] {
            let value = markdown::Link {
                range: 0..1,
                target: markdown::LinkTarget::Markdown {
                    destination: href.into(),
                },
            };
            assert!(host_term(&value).is_none(), "{href}");
        }
    }
    #[test]
    fn global_limit_and_host_occurrence_identity() {
        let blocks = markdown::parse("[[A|same]] [[B|same]]");
        let preview = markdown::IncludePreview {
            items: vec![
                markdown::PreviewItem::Host(blocks.clone()),
                markdown::PreviewItem::Include {
                    index: 0,
                    directive: "![[X]]".into(),
                    blocks: blocks.clone(),
                },
                markdown::PreviewItem::Host(blocks),
            ],
            unsupported: vec![],
        };
        let result = candidates(&preview);
        assert_eq!(result.len(), 4);
        assert_eq!((result[0].item, result[2].item), (0, 2));
        assert_ne!(result[0].link.target, result[1].link.target);
        let preview = markdown::IncludePreview {
            items: (0..40)
                .map(|_| markdown::PreviewItem::Host(markdown::parse("[[A]]")))
                .collect(),
            unsupported: vec![],
        };
        assert_eq!(candidates(&preview).len(), MAX_HOST_LINKS);
    }
    #[test]
    fn host_prose_fragments_do_not_authorize_code_includes_or_markdown_fragments() {
        let preview = markdown::IncludePreview {
            items: vec![
                markdown::PreviewItem::Host(markdown::parse(
                    "[[Target##Section|Go]] `[[Code#Section]]` [local](#section) [other](Other.md#section) [[Target#%53ection]]\n\n```text\n[[Fence#Section]]\n```",
                )),
                markdown::PreviewItem::Include {
                    index: 0,
                    directive: "![[Included]]".into(),
                    blocks: markdown::parse("[[Included#Section]]"),
                },
            ],
            unsupported: vec![],
        };
        let controls = candidates(&preview);
        assert_eq!(controls.len(), 4);
        assert_eq!(controls[0].label, "Go");
        assert!(
            controls
                .iter()
                .all(|candidate| candidate.item == 0 && candidate.fragment)
        );
        assert_eq!(
            controls
                .iter()
                .filter(|candidate| host_term(&candidate.link).is_some())
                .count(),
            1
        );
        assert!(markdown::is_local_fragment(&controls[1].link.target));
    }
    fn ticket() -> Ticket {
        let note = live::NoteKey {
            vault: "host".into(),
            note_id: 42,
        };
        Ticket {
            identity: Identity {
                source: LinkSource {
                    note: note.clone(),
                    etag: "body".into(),
                    launch_generation: 1,
                    storage_generation: 2,
                    request: 3,
                },
                route: note,
            },
            serial: 4,
            epoch: 5,
            mode: 6,
            scope: "host".into(),
            scope_revision: 7,
            registry: ("host".into(), vec!["host".into(), "foreign".into()]),
            candidate: Candidate {
                item: 0,
                block: 0,
                link: link("[[Target|Alias]]"),
                label: "Alias".into(),
                fragment: false,
            },
        }
    }
    #[test]
    fn ticket_rejects_each_owner_change_and_duplicate_occurrence() {
        let expected = ticket();
        assert!(current_ticket(&expected, Some(&ticket())));
        assert!(!current_ticket(&expected, None));
        for i in 0..17 {
            let mut changed = ticket();
            match i {
                0 => changed.identity.source.request += 1,
                1 => changed.serial += 1,
                2 => changed.epoch += 1,
                3 => changed.mode += 1,
                4 => changed.scope_revision += 1,
                5 => changed.registry.1.clear(),
                6 => changed.identity.source.storage_generation += 1,
                7 => changed.identity.source.note.vault = "foreign".into(),
                8 => changed.candidate.item += 1,
                9 => changed.candidate.block += 1,
                10 => changed.candidate.link = link("[[Another|Alias]]"),
                11 => changed.identity.source.etag = "new body".into(),
                12 => changed.identity.source.launch_generation += 1,
                13 => changed.scope = "foreign".into(),
                14 => changed.registry.0 = "foreign".into(),
                15 => changed.candidate.link = link("[[Target#Section|Alias]]"),
                _ => changed.identity.route.note_id += 1,
            };
            assert!(!current_ticket(&expected, Some(&changed)));
        }
        let mut launch = ticket();
        launch.identity.source.note.vault.clear();
        assert!(current_ticket(&launch, Some(&launch)));
        assert_ne!(
            launch.identity.source.note.vault,
            expected.identity.source.note.vault
        );
    }
}
