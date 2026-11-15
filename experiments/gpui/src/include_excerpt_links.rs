//! Explicit internal-note controls for accepted included prose, not general hrefs.
use super::*;
const MAX_LINKS: usize = 16;
#[derive(Clone, Debug, PartialEq, Eq)]
struct LinkTicket {
    owner: Ticket,
    block: usize,
    link: markdown::Link,
}
fn wiki_term(link: &markdown::Link) -> Option<String> {
    let markdown::LinkTarget::Wiki { key, .. } = &link.target else {
        return None;
    };
    if key.is_empty()
        || key.len() > 4096
        || key.chars().any(char::is_control)
        || key.contains("://")
        || ["javascript:", "data:", "file:", "mailto:"]
            .iter()
            .any(|p| key.to_ascii_lowercase().starts_with(p))
    {
        return None;
    }
    markdown::resolve_term(&link.target)
}
/// Deliberately narrower than ordinary Markdown href handling: simple relative
/// note titles only. Strip literal .md before decoding, never choose a basename.
fn markdown_note_term(destination: &str) -> Option<String> {
    let raw = destination.trim();
    if raw.is_empty()
        || raw.len() > 4096
        || raw.chars().any(char::is_control)
        || raw.contains(['/', '\\', ':', '?', '#'])
    {
        return None;
    }
    let stem = if raw
        .get(raw.len().saturating_sub(3)..)
        .is_some_and(|s| s.eq_ignore_ascii_case(".md"))
    {
        &raw[..raw.len() - 3]
    } else {
        raw
    };
    let mut bytes = Vec::with_capacity(stem.len());
    let mut input = stem.as_bytes().iter().copied();
    while let Some(byte) = input.next() {
        if byte == b'%' {
            let hi = (input.next()? as char).to_digit(16)?;
            let lo = (input.next()? as char).to_digit(16)?;
            bytes.push(((hi << 4) | lo) as u8);
        } else {
            bytes.push(byte);
        }
    }
    let decoded = String::from_utf8(bytes).ok()?;
    let term = decoded.trim();
    // An encoded terminal .md is a literal title suffix, not an extension to
    // strip a second time. Other dotted/email-like forms stay inert.
    let encoded_md = stem.contains('%')
        && !stem.contains('.')
        && term
            .get(term.len().saturating_sub(3)..)
            .is_some_and(|s| s.eq_ignore_ascii_case(".md"))
        && !term[..term.len() - 3].contains('.');
    if term.is_empty()
        || term.len() > 4096
        || term.chars().any(char::is_control)
        || term.contains(['/', '\\', ':', '?', '#', '@', '%'])
        || (term.contains('.') && !encoded_md)
    {
        return None;
    }
    Some(term.to_owned())
}
fn link_term(link: &markdown::Link) -> Option<String> {
    match &link.target {
        markdown::LinkTarget::Wiki { .. } => wiki_term(link),
        markdown::LinkTarget::Markdown { destination } => markdown_note_term(destination),
        markdown::LinkTarget::Footnote { .. } => None,
    }
}
fn eligible(blocks: &[markdown::Block]) -> Vec<(usize, markdown::Link)> {
    blocks
        .iter()
        .enumerate()
        .filter(|(_, b)| b.kind == markdown::BlockKind::Paragraph)
        .flat_map(|(i, b)| {
            b.links
                .iter()
                .filter(move |l| link_term(l).is_some() && b.text.get(l.range.clone()).is_some())
                .map(move |l| (i, l.clone()))
        })
        .take(MAX_LINKS)
        .collect()
}
fn excerpt_blocks(state: &IncludesState, item: usize) -> Option<&[markdown::Block]> {
    match state
        .result
        .as_ref()?
        .as_ref()
        .ok()?
        .preview
        .as_ref()
        .ok()?
        .items
        .get(item)?
    {
        markdown::PreviewItem::Include { blocks, .. } => Some(blocks),
        _ => None,
    }
}
fn resolver_source(ticket: &LinkTicket) -> shell::LinkSource {
    let mut source = ticket.owner.identity.source.clone();
    source.note = live::NoteKey {
        vault: ticket.owner.entry.vault.clone(),
        note_id: ticket.owner.entry.note_id.unwrap(),
    };
    source.etag = ticket.owner.entry.etag.clone().unwrap();
    source
}
impl Workspace {
    fn excerpt_link_ticket(
        &self,
        item: usize,
        entry: usize,
        block: usize,
        link: &markdown::Link,
    ) -> Option<LinkTicket> {
        let owner = self.include_source_ticket(item, entry)?;
        let state = self.shell.includes.as_ref()?;
        if !eligible(excerpt_blocks(state, item)?).contains(&(block, link.clone())) {
            return None;
        }
        Some(LinkTicket {
            owner,
            block,
            link: link.clone(),
        })
    }
    fn excerpt_link_current(&self, t: &LinkTicket, cx: &Context<Self>) -> bool {
        self.include_source_current(&t.owner, cx)
            && self
                .excerpt_link_ticket(t.owner.item, t.owner.entry_index, t.block, &t.link)
                .as_ref()
                == Some(t)
    }
    pub(crate) fn excerpt_link_controls(
        &self,
        item: usize,
        start: usize,
        end: usize,
        cx: &Context<Self>,
    ) -> Div {
        let Some(state) = self.shell.includes.as_ref() else {
            return div();
        };
        let Some(Ok(snapshot)) = &state.result else {
            return div();
        };
        let Some(markdown::PreviewItem::Include { index, blocks, .. }) = snapshot
            .preview
            .as_ref()
            .ok()
            .and_then(|p| p.items.get(item))
        else {
            return div();
        };
        let mut controls = div().flex().flex_col().gap_1();
        for (block, link) in eligible(blocks)
            .into_iter()
            .filter(|(b, _)| *b >= start && *b < end)
        {
            let Some(ticket) = self.excerpt_link_ticket(item, *index, block, &link) else {
                continue;
            };
            let label = blocks[block]
                .text
                .get(link.range.clone())
                .unwrap_or("Note link");
            controls = controls.child(
                div()
                    .id(gpui::SharedString::from(format!(
                        "include-wiki-{}-{}-{item}-{block}-{}",
                        ticket.owner.serial, ticket.owner.epoch, link.range.start
                    )))
                    .text_size(px(12.))
                    .cursor_pointer()
                    .child(format!("Open included link: {label}"))
                    .on_click(
                        cx.listener(move |this, event: &gpui::ClickEvent, window, cx| {
                            cx.stop_propagation();
                            if shell::plain_reader_click(event) {
                                this.activate_excerpt_link(ticket.clone(), window, cx);
                            }
                        }),
                    ),
            );
        }
        controls
    }
    fn activate_excerpt_link(
        &mut self,
        ticket: LinkTicket,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active() || !self.excerpt_link_current(&ticket, cx) {
            return;
        }
        let Some(handle) = cx.active_window().and_then(shortcut_workspace) else {
            return;
        };
        let Some(term) = link_term(&ticket.link) else {
            return;
        };
        let state = self.shell.includes.as_mut().unwrap();
        if query_navigation::BUSY
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .is_err()
        {
            state.query_nav.pending = None;
            state.query_nav.notice = Some(
                "Previous navigation canceled; its lookup is still finishing. Try again shortly."
                    .into(),
            );
            cx.notify();
            return;
        }
        let permit = query_navigation::Permit;
        let token = SERIAL.fetch_add(1, Ordering::Relaxed);
        state.query_nav.pending = Some(token);
        state.query_nav.notice = Some("Resolving included note link…".into());
        let source = resolver_source(&ticket);
        let client = self.live.as_ref().unwrap().client();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client
                .storage()
                .map_err(|_| "Include storage unavailable")?;
            if before.phase != "ready" || before.generation != source.storage_generation {
                return Err("Include storage changed".to_owned());
            }
            let response = client
                .resolve(&term, &source.note.vault)
                .map_err(|_| "Included link could not be resolved")?;
            let after = client
                .storage()
                .map_err(|_| "Include storage unavailable")?;
            if after.phase != "ready" || after.generation != source.storage_generation {
                return Err("Include storage changed".to_owned());
            }
            query_navigation::verified_key(&response, &source, &term)
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
                    if !active
                        || !window.is_window_active()
                        || !this.excerpt_link_current(&ticket, cx)
                    {
                        this.shell.includes.as_mut().unwrap().query_nav.notice = Some(
                            "Included link navigation canceled after its source changed.".into(),
                        );
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
                        &ticket.owner.registry.0
                    } else {
                        &key.vault
                    };
                    if !ticket.owner.registry.1.contains(canonical)
                        || !this.session_ui.permits(&key.vault)
                    {
                        this.shell.includes.as_mut().unwrap().query_nav.notice = Some(
                            "Included link vault is unavailable; no fallback was used.".into(),
                        );
                        cx.notify();
                        return;
                    }
                    let intent = shell::InternalLinkIntent {
                        source: ticket.owner.identity.source.note.clone(),
                        destination: key,
                        target: ticket.link.target.clone(),
                        launch_generation: ticket.owner.identity.source.launch_generation,
                        storage_generation: ticket.owner.identity.source.storage_generation,
                        allow_epoch_advance: false,
                    };
                    if this.guard_transition(
                        workspace_edit::Transition::InternalLink(intent.clone()),
                        cx,
                    ) {
                        return;
                    }
                    this.close_includes_preview(
                        &ticket.owner.identity,
                        ticket.owner.serial,
                        window,
                        cx,
                    );
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
    fn owner(vault: Option<&str>) -> Ticket {
        let mut e = serde_json::json!({"line":0,"source_line":0,"note_id":42,"kind":"note","etag":"excerpt-rev","caption":"Alias","title":"Not identity","lines":["[[Same|Alias]]"]});
        if let Some(v) = vault {
            e["vault"] = v.into();
        }
        let entry = crate::included_notes::decode(
            serde_json::json!({"markdown":"![[Alias]]","includes":[e]}),
            "host",
        )
        .unwrap()
        .entries
        .remove(0);
        let source = shell::LinkSource {
            note: live::NoteKey {
                vault: "host".into(),
                note_id: 1,
            },
            etag: "host-rev".into(),
            storage_generation: 2,
            launch_generation: 3,
            request: 4,
        };
        Ticket {
            identity: Identity {
                route: source.note.clone(),
                source,
            },
            serial: 5,
            epoch: 6,
            item: 0,
            entry_index: 0,
            entry,
            scope: "host".into(),
            scope_revision: 7,
            registry: (
                "main".into(),
                vec!["main".into(), "host".into(), "foreign".into()],
            ),
        }
    }
    fn link(text: &str) -> markdown::Link {
        eligible(&markdown::preview_blocks(text).unwrap())
            .remove(0)
            .1
    }
    #[test]
    fn simple_markdown_targets_normalize_without_label_or_path_guessing() {
        for (raw, expected) in [
            ("Design.md", "Design"),
            ("Title.MD", "Title"),
            ("Caf%C3%A9.md", "Café"),
            ("Title%2Emd", "Title.md"),
            ("  Design  ", "Design"),
        ] {
            assert_eq!(markdown_note_term(raw).as_deref(), Some(expected));
        }
        for raw in [
            "",
            "#Heading",
            "Note.md#Heading",
            "Note?x=1",
            "https://example.invalid",
            "id:42",
            "work:Note",
            "mailto:x@y",
            "/Note",
            "../Note",
            "docs/Note.md",
            "docs%2FNote.md",
            "assets/pic.png",
            "apps/thing",
            "foo.com",
            "foo%2Ecom",
            "a%3Ab",
            "a%23b",
            "a%3Fb",
            "a%5Cb",
            "Bad%",
            "Bad%Q1",
            "%FF",
            "%00",
            ".md",
            "..",
            "a@b",
        ] {
            assert!(markdown_note_term(raw).is_none(), "{raw}");
        }
        let parsed = link("[Not identity](Caf%C3%A9.MD)");
        assert_eq!(link_term(&parsed).as_deref(), Some("Café"));
        assert!(matches!(
            parsed.target,
            markdown::LinkTarget::Markdown { .. }
        ));
    }
    #[test]
    fn markdown_links_retain_include_origin_and_exact_ticket_target() {
        for (wire, expected) in [(None, "host"), (Some(""), ""), (Some("foreign"), "foreign")] {
            let t = LinkTicket {
                owner: owner(wire),
                block: 0,
                link: link("[Alias](Same.md)"),
            };
            assert_eq!(resolver_source(&t).note.vault, expected);
            assert_eq!(t.owner.identity.source.note.vault, "host");
            assert_eq!(link_term(&t.link).as_deref(), Some("Same"));
            let mut changed = t.clone();
            changed.link = link("[Alias](Other.md)");
            assert_ne!(t, changed);
            changed = t.clone();
            changed.owner.epoch += 1;
            assert_ne!(t, changed);
        }
    }
    #[test]
    fn prose_note_links_are_bounded_in_original_order() {
        let blocks=markdown::preview_blocks("[[Same|one]] [[Same|two]] [relative](Note.md) [web](https://example.invalid)\n\n```text\n[[Code]]\n```\n\n[[Last]]").unwrap();
        let accepted = eligible(&blocks);
        assert_eq!(accepted.len(), 4);
        assert_ne!(accepted[0].1.range, accepted[1].1.range);
        assert_eq!(wiki_term(&accepted[0].1).as_deref(), Some("Same"));
        assert_eq!(wiki_term(&accepted[3].1).as_deref(), Some("Last"));
        let many = (0..100)
            .map(|i| format!("[[Note {i}]]"))
            .collect::<Vec<_>>()
            .join(" ");
        let bounded = eligible(&markdown::preview_blocks(&many).unwrap());
        assert_eq!(bounded.len(), 16);
        assert_eq!(wiki_term(&bounded[15].1).as_deref(), Some("Note 15"));
    }
    #[test]
    fn resolver_namespace_is_include_origin_but_navigation_owner_is_host() {
        for (vault, expected) in [(None, "host"), (Some(""), ""), (Some("foreign"), "foreign")] {
            let ticket = LinkTicket {
                owner: owner(vault),
                block: 0,
                link: link("[[Same#Details|Alias]]"),
            };
            let source = resolver_source(&ticket);
            assert_eq!(source.note.vault, expected);
            assert_eq!(source.note.note_id, 42);
            assert_eq!(source.etag, "excerpt-rev");
            assert_eq!(ticket.owner.identity.source.note.vault, "host");
            assert_eq!(ticket.owner.identity.source.note.note_id, 1);
            assert_eq!(wiki_term(&ticket.link).as_deref(), Some("Same"));
            assert!(
                matches!(&ticket.link.target,markdown::LinkTarget::Wiki{fragment:Some(f),..} if f=="#Details")
            );
        }
        let block = link("[[Same#^block-id|Alias]]");
        assert!(
            matches!(block.target,markdown::LinkTarget::Wiki{fragment:Some(f),..} if f=="#^block-id")
        );
        assert_eq!(wiki_term(&link("[[C#]]")).as_deref(), Some("C#"));
    }
    #[test]
    fn stale_occurrence_and_link_cannot_reuse_authority() {
        let base = LinkTicket {
            owner: owner(Some("foreign")),
            block: 0,
            link: link("[[Same|Alias]]"),
        };
        for n in 0..9 {
            let mut stale = base.clone();
            match n {
                0 => stale.owner.epoch += 1,
                1 => stale.owner.entry.etag = Some("new".into()),
                2 => stale.owner.entry.vault.clear(),
                3 => stale.owner.item += 1,
                4 => stale.block += 1,
                5 => stale.link.range.start += 1,
                6 => stale.owner.identity.source.request += 1,
                7 => stale.owner.scope_revision += 1,
                _ => stale.owner.registry.1.clear(),
            };
            assert_ne!(base, stale);
        }
    }
    #[test]
    fn response_must_match_included_namespace_without_host_fallback() {
        let ticket = LinkTicket {
            owner: owner(Some("foreign")),
            block: 0,
            link: link("[[Same]]"),
        };
        let source = resolver_source(&ticket);
        let mut response:live::Resolution=serde_json::from_value(serde_json::json!({"found":true,"note":{"note_id":99,"title":"Same","file_kind":"note","vault":"foreign"}})).unwrap();
        response.source_vault = "foreign".into();
        response.launch_generation = 3;
        response.term = "Same".into();
        assert_eq!(
            query_navigation::verified_key(&response, &source, "Same")
                .unwrap()
                .vault,
            "foreign"
        );
        response.source_vault = "host".into();
        assert!(query_navigation::verified_key(&response, &source, "Same").is_err());
        response.source_vault = "foreign".into();
        response.found = false;
        assert!(query_navigation::verified_key(&response, &source, "Same").is_err());
    }
}
