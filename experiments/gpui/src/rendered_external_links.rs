//! Explicit browser dispatch from accepted, actually displayed preview prose.
use super::*;
use std::rc::Rc;
const MAX_CONTROLS: usize = 16;
const MAX_LINKS: usize = 256;
const MAX_BLOCKS: usize = 8192;
const MAX_URL_BYTES: usize = 4096;
#[derive(Clone, Debug, PartialEq, Eq)]
struct Candidate {
    item: usize,
    block: usize,
    link: markdown::Link,
    label: String,
    url: String,
}
fn candidates(snapshot: &Snapshot) -> Vec<Candidate> {
    let Ok(preview) = &snapshot.preview else {
        return Vec::new();
    };
    let mut result = Vec::new();
    let mut blocks_seen = 0;
    let mut links_seen = 0;
    'items: for (item, entry) in preview.items.iter().enumerate() {
        let blocks = match entry {
            markdown::PreviewItem::Host(blocks) => blocks,
            markdown::PreviewItem::Include { index, blocks, .. } => {
                let Some(e) = snapshot.response.entries.get(*index) else {
                    continue;
                };
                if e.error.as_ref().is_some_and(|e| !e.is_empty())
                    || !e.bad_options.is_empty()
                    || e.excerpt.is_empty()
                    || e.note_id.is_none_or(|id| id <= 0)
                    || e.etag.as_ref().is_none_or(String::is_empty)
                    || !matches!(e.kind.as_deref(), Some("note" | "journal"))
                {
                    continue;
                }
                blocks
            }
        };
        for (block, b) in blocks.iter().enumerate() {
            if blocks_seen == MAX_BLOCKS {
                break 'items;
            }
            blocks_seen += 1;
            if b.kind != markdown::BlockKind::Paragraph || b.image.is_some() {
                continue;
            }
            for link in &b.links {
                if links_seen == MAX_LINKS {
                    break 'items;
                }
                links_seen += 1;
                let markdown::LinkTarget::Markdown { destination } = &link.target else {
                    continue;
                };
                if destination.len() > MAX_URL_BYTES {
                    continue;
                }
                let Some(url) = crate::external_links::destination(&link.target)
                    .filter(|u| u.len() <= MAX_URL_BYTES)
                else {
                    continue;
                };
                let Some(label) = b.text.get(link.range.clone()) else {
                    continue;
                };
                // The reader preserves nested image markup literally inside link labels.
                // Do not turn linked/inline image fallbacks into browser controls.
                if label.contains("![") || label.contains('<') || label.contains('>') {
                    continue;
                }
                result.push(Candidate {
                    item,
                    block,
                    link: link.clone(),
                    label: label.chars().take(80).collect(),
                    url,
                });
                if result.len() == MAX_CONTROLS {
                    break 'items;
                }
            }
        }
    }
    result
}
#[derive(Default)]
pub(super) struct Cache {
    stamp: Option<(u64, u64)>,
    candidates: Rc<Vec<Candidate>>,
}
fn cached(state: &IncludesState) -> Rc<Vec<Candidate>> {
    let stamp = (state.serial, state.preview_epoch);
    let mut cache = state.external_links.borrow_mut();
    if cache.stamp != Some(stamp) {
        cache.stamp = Some(stamp);
        cache.candidates = Rc::new(
            state
                .result
                .as_ref()
                .and_then(|r| r.as_ref().ok())
                .map(candidates)
                .unwrap_or_default(),
        );
    }
    cache.candidates.clone()
}
fn blocks_at(state: &IncludesState, item: usize) -> Option<&[markdown::Block]> {
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
        markdown::PreviewItem::Host(b) | markdown::PreviewItem::Include { blocks: b, .. } => {
            Some(b)
        }
    }
}
fn slice_matches(accepted: &[markdown::Block], start: usize, shown: &[markdown::Block]) -> bool {
    accepted.get(start..start.saturating_add(shown.len())) == Some(shown)
}
#[derive(Clone, PartialEq, Eq)]
struct Ticket {
    identity: Identity,
    serial: u64,
    epoch: u64,
    mode: u64,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
    pages: std::collections::BTreeMap<(usize, usize, Option<usize>), usize>,
    sources: std::collections::BTreeSet<(usize, usize)>,
    find: u64,
    selection: u64,
    candidate: Candidate,
}
fn dispatch<T: PartialEq>(
    expected: &T,
    current: Option<&T>,
    allowed: bool,
    candidate: &Candidate,
    block: &markdown::Block,
    open: impl FnOnce(&str),
) -> bool {
    if !allowed
        || current != Some(expected)
        || crate::external_links::destination(&candidate.link.target).as_ref()
            != Some(&candidate.url)
    {
        return false;
    }
    crate::external_links::dispatch(
        expected,
        current,
        std::slice::from_ref(block),
        std::slice::from_ref(block),
        &candidate.link,
        open,
    )
}
impl Workspace {
    fn external_preview_ticket(&self, candidate: &Candidate) -> Option<Ticket> {
        let s = self.shell.includes.as_ref()?;
        let identity = self.includes_identity()?;
        if !s.preview_open
            || s.fetching
            || s.identity != identity
            || !self.includes_body_current(s)
            || self.shell.accepted_title_request != Some(self.request)
            || !cached(s).contains(candidate)
        {
            return None;
        }
        let registry = self.follow_registry()?;
        let vault = match s
            .result
            .as_ref()?
            .as_ref()
            .ok()?
            .preview
            .as_ref()
            .ok()?
            .items
            .get(candidate.item)?
        {
            markdown::PreviewItem::Host(_) => &identity.source.note.vault,
            markdown::PreviewItem::Include { index, .. } => {
                &s.result
                    .as_ref()?
                    .as_ref()
                    .ok()?
                    .response
                    .entries
                    .get(*index)?
                    .vault
            }
        };
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
            serial: s.serial,
            epoch: s.preview_epoch,
            mode: self.shell.rendered_reading.generation(),
            scope: self.working_vault_scope().to_owned(),
            scope_revision: self.shell.scope_revision,
            registry,
            pages: s.query_pages.clone(),
            sources: s.query_sources.clone(),
            find: s.find.interaction_revision(),
            selection: s.selection.borrow().interaction_revision(),
            candidate: candidate.clone(),
        })
    }
    fn external_preview_allowed(&self, t: &Ticket, cx: &Context<Self>) -> bool {
        self.query_control_gate(&t.identity, cx).allowed()
            && self.shell.includes.as_ref().is_some_and(|s| {
                !s.find.is_open()
                    && !s.contents.is_pending()
                    && s.selection.borrow().is_empty()
                    && s.query_nav.pending.is_none()
            })
            && self.editing_ui.plain_reader_mode()
            && self.editing_ui.follow_safe()
            && !self.recent_open()
            && !self.reader_hierarchy_open()
            && !self.enlarged_image_open()
            && !self.creation_open()
            && !self.reader_selection_dragging()
            && !self.shell.find.is_composing(cx)
            && !self.shell.calendar_zone.is_composing(cx)
            && self.quit_session.failure().is_none()
            && (!self.session_ui.recovery_enabled()
                || self.session_ui.recovery_status()
                    == workspace_session::RecoveryStatus::Ready(draft_recovery::LocalStatus::Empty))
    }
    pub(super) fn external_preview_controls(
        &self,
        item: usize,
        start: usize,
        shown: &[markdown::Block],
        cx: &mut Context<Self>,
    ) -> Div {
        let Some(state) = self.shell.includes.as_ref() else {
            return div();
        };
        // Generated query/source/diagram disclosures are not the accepted prose slice.
        if !blocks_at(state, item).is_some_and(|b| slice_matches(b, start, shown)) {
            return div();
        }
        let mut controls = div().flex().flex_col().gap_1();
        for c in cached(state)
            .iter()
            .filter(|c| c.item == item && c.block >= start && c.block < start + shown.len())
        {
            let Some(ticket) = self.external_preview_ticket(c) else {
                continue;
            };
            controls = controls.child(
                div()
                    .id(gpui::SharedString::from(format!(
                        "preview-browser-{}-{}-{}-{}-{}",
                        ticket.serial, ticket.epoch, item, c.block, c.link.range.start
                    )))
                    .text_size(px(12.))
                    .cursor_pointer()
                    .child(format!("Open in browser: {} · {}", c.label, c.url))
                    .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .on_click(
                        cx.listener(move |this, event: &gpui::ClickEvent, window, cx| {
                            cx.stop_propagation();
                            let allowed = shell::plain_reader_click(event)
                                && window.is_window_active()
                                && this.external_preview_allowed(&ticket, cx);
                            let current = this.external_preview_ticket(&ticket.candidate);
                            let Some(block) = this
                                .shell
                                .includes
                                .as_ref()
                                .and_then(|s| blocks_at(s, ticket.candidate.item))
                                .and_then(|b| b.get(ticket.candidate.block))
                            else {
                                return;
                            };
                            dispatch(
                                &ticket,
                                current.as_ref(),
                                allowed,
                                &ticket.candidate,
                                block,
                                |url| cx.open_url(url),
                            );
                        }),
                    ),
            );
        }
        controls
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn snapshot(text: &str) -> Snapshot {
        Snapshot {
            query_positions: Err("test".into()),
            inline: Err("test".into()),
            boards: Default::default(),
            response: crate::included_notes::Response {
                markdown: text.into(),
                entries: vec![],
            },
            preview: Ok(markdown::IncludePreview {
                items: vec![markdown::PreviewItem::Host(markdown::parse(text))],
                unsupported: vec![],
            }),
            queries: Default::default(),
        }
    }
    fn prose(s: &Snapshot) -> &markdown::Block {
        let markdown::PreviewItem::Host(b) = &s.preview.as_ref().unwrap().items[0] else {
            panic!()
        };
        &b[0]
    }
    #[test]
    fn destination_is_not_label_and_dispatch_is_explicit_exact_and_once() {
        let s = snapshot("[different.example](HTTP://EXAMPLE.COM:80/a%2Fb?q=%2520#part)");
        let c = candidates(&s).remove(0);
        assert_eq!(c.url, "http://example.com/a%2Fb?q=%2520#part");
        assert_eq!(c.label, "different.example");
        let mut calls = vec![];
        assert!(calls.is_empty());
        assert!(dispatch(&1, Some(&1), true, &c, prose(&s), |u| calls
            .push(u.to_owned())));
        assert_eq!(calls, vec![c.url.clone()]);
        assert!(!dispatch(&1, Some(&2), true, &c, prose(&s), |_| panic!()));
        assert!(!dispatch(&1, Some(&1), false, &c, prose(&s), |_| panic!()));
        let other = snapshot("[different.example](https://other.example)");
        assert!(!dispatch(
            &1,
            Some(&1),
            true,
            &c,
            prose(&other),
            |_| panic!()
        ));
    }
    #[test]
    fn unsupported_destinations_and_nonprose_remain_inert() {
        for raw in [
            "//example.com",
            "../Other.md",
            "file:///x",
            "data:text/plain,x",
            "javascript:alert(1)",
            "https://a:b@example.com",
            "https:///example.com",
            "https://example.com\\evil",
        ] {
            let mut s = snapshot("[x](https://example.com)");
            let markdown::PreviewItem::Host(b) = &mut s.preview.as_mut().unwrap().items[0] else {
                panic!()
            };
            b[0].links[0].target = markdown::LinkTarget::Markdown {
                destination: raw.into(),
            };
            assert!(candidates(&s).is_empty(), "{raw}");
        }
        for text in [
            "```\n[x](https://example.com)\n```",
            "![x](https://example.com/a.png)",
            "[![x](assets/a.png)](https://example.com)",
            "<div>https://example.com</div>",
        ] {
            assert!(candidates(&snapshot(text)).is_empty());
        }
    }
    #[test]
    fn occurrence_and_global_prefix_limits() {
        let mut s = snapshot("[x](https://example.com)");
        let host = s.preview.as_ref().unwrap().items[0].clone();
        s.preview.as_mut().unwrap().items = vec![host; 20];
        let cs = candidates(&s);
        assert_eq!(cs.len(), 16);
        assert_eq!(cs[0].item, 0);
        assert_eq!(cs[15].item, 15);
        let text = format!("{} [x](https://example.com)", "[x](local.md) ".repeat(256));
        assert!(candidates(&snapshot(&text)).is_empty());
        let mut s = snapshot("[x](https://example.com)");
        let mut empty = markdown::parse("ordinary");
        let mut blocks = vec![empty.remove(0); 8192];
        blocks.push(prose(&s).clone());
        s.preview.as_mut().unwrap().items = vec![markdown::PreviewItem::Host(blocks)];
        assert!(candidates(&s).is_empty());
    }
    #[test]
    fn url_byte_limits_before_and_after_normalization() {
        let mut s = snapshot("[x](https://example.com)");
        let markdown::PreviewItem::Host(b) = &mut s.preview.as_mut().unwrap().items[0] else {
            panic!()
        };
        let base = "https://example.com/";
        b[0].links[0].target = markdown::LinkTarget::Markdown {
            destination: format!("{base}{}", "a".repeat(4096 - base.len())),
        };
        assert_eq!(candidates(&s).len(), 1);
        let markdown::PreviewItem::Host(b) = &mut s.preview.as_mut().unwrap().items[0] else {
            panic!()
        };
        b[0].links[0].target = markdown::LinkTarget::Markdown {
            destination: format!("{base}{}", "a".repeat(4097 - base.len())),
        };
        assert!(candidates(&s).is_empty());
        let markdown::PreviewItem::Host(b) = &mut s.preview.as_mut().unwrap().items[0] else {
            panic!()
        };
        b[0].links[0].target = markdown::LinkTarget::Markdown {
            destination: format!("{base}{}", "日".repeat(500)),
        };
        assert!(candidates(&s).is_empty()); // Percent-encoded normalized URL exceeds limit.
    }
    #[test]
    fn bad_include_and_disclosure_are_not_eligible() {
        let mut s = snapshot("[x](https://example.com)");
        let blocks = vec![prose(&s).clone()];
        let e = crate::included_notes::Include {
            line: 0,
            source_line: 0,
            note_id: Some(7),
            kind: Some("note".into()),
            etag: Some("e".into()),
            title: Some("alias".into()),
            caption: "alias".into(),
            excerpt: "[x](https://example.com)".into(),
            bad_options: vec![],
            error: None,
            vault: "foreign".into(),
            explicit_origin: true,
        };
        s.response.entries.push(e);
        s.preview.as_mut().unwrap().items = vec![
            markdown::PreviewItem::Include {
                index: 0,
                directive: "include".into(),
                blocks: blocks.clone(),
            },
            markdown::PreviewItem::Include {
                index: 0,
                directive: "include".into(),
                blocks: blocks.clone(),
            },
        ];
        let c = candidates(&s);
        assert_eq!(c.len(), 2);
        assert_ne!(c[0].item, c[1].item);
        s.response.entries[0].bad_options.push("invalid".into());
        assert!(candidates(&s).is_empty());
        s.response.entries[0].bad_options.clear();
        s.response.entries[0].error = Some("unavailable".into());
        assert!(candidates(&s).is_empty());
        assert!(slice_matches(&blocks, 0, &blocks));
        let mut disclosure = blocks.clone();
        disclosure[0].text = "different generated text".into();
        assert!(!slice_matches(&blocks, 0, &disclosure));
        assert!(!slice_matches(&blocks, 1, &blocks));
    }
    #[test]
    fn exact_owner_and_occurrence_changes_never_dispatch() {
        let s = snapshot("[x](https://example.com)");
        let c = candidates(&s).remove(0);
        let note = live::NoteKey {
            vault: "host".into(),
            note_id: 7,
        };
        let t = Ticket {
            identity: Identity {
                source: LinkSource {
                    note: note.clone(),
                    etag: "e".into(),
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
            pages: Default::default(),
            sources: Default::default(),
            find: 8,
            selection: 9,
            candidate: c.clone(),
        };
        assert!(dispatch(&t, Some(&t), true, &c, prose(&s), |_| {}));
        assert!(!dispatch(&t, None, true, &c, prose(&s), |_| panic!()));
        for i in 0..14 {
            let mut changed = t.clone();
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
                10 => {
                    changed.pages.insert((0, 0, None), 1);
                }
                11 => {
                    changed.sources.insert((0, 0));
                }
                12 => changed.find += 1,
                _ => changed.selection += 1,
            };
            assert!(
                !dispatch(&t, Some(&changed), true, &c, prose(&s), |_| panic!()),
                "{i}"
            );
        }
    }
}
