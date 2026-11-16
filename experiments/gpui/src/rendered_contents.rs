//! Host-only chapter identities. Includes never contribute anchors.
use super::*;
use crate::reader_find::{Hit, Matches, Target};
use std::{cell::RefCell, rc::Rc};
const MAX_HEADINGS: usize = 256;
const MAX_BLOCKS: usize = 8192;
const MAX_TEXT: usize = 2 * 1024 * 1024;
#[derive(Clone, Debug, PartialEq, Eq)]
struct Entry {
    item: usize,
    block: usize,
    anchor: String,
    title: String,
    level: u8,
}
fn entries(preview: &markdown::IncludePreview) -> Result<Vec<Entry>, String> {
    let mut headings = Vec::new();
    let mut keys = Vec::new();
    let mut count = 0usize;
    let mut bytes = 0usize;
    for (item, p) in preview.items.iter().enumerate() {
        let markdown::PreviewItem::Host(blocks) = p else {
            continue;
        };
        for (block, b) in blocks.iter().enumerate() {
            count += 1;
            if count > MAX_BLOCKS {
                return Err("Contents unavailable: host block limit exceeded".into());
            }
            if !matches!(b.kind, markdown::BlockKind::Heading(_)) {
                continue;
            }
            bytes = bytes.saturating_add(b.text.len());
            if headings.len() == MAX_HEADINGS || bytes > MAX_TEXT {
                return Err("Contents unavailable: heading limit exceeded".into());
            }
            headings.push(b.clone());
            keys.push((item, block));
        }
    }
    Ok(markdown::outline(&headings)
        .into_iter()
        .map(|e| Entry {
            item: keys[e.block_index].0,
            block: keys[e.block_index].1,
            anchor: e.anchor,
            title: e.title,
            level: e.level,
        })
        .collect())
}
fn local_heading(link: &markdown::Link) -> Option<String> {
    let markdown::LinkTarget::Markdown { destination } = &link.target else {
        return None;
    };
    if !destination.starts_with('#') {
        return None;
    }
    crate::snapshot_links::location("", destination)?.heading
}
#[derive(Clone, PartialEq, Eq)]
struct Owner {
    identity: Identity,
    serial: u64,
    epoch: u64,
    mode: u64,
    scope: String,
    scope_revision: u64,
    registry: (String, Vec<String>),
    pages: std::collections::BTreeMap<(usize, usize, Option<usize>), usize>,
    sources: std::collections::BTreeSet<(usize, usize)>,
}
#[derive(Clone)]
struct Action {
    owner: Owner,
    revision: u64,
    entry: Entry,
}
pub(super) struct Ui {
    owner: Option<Owner>,
    entries: Result<Vec<Entry>, String>,
    revision: u64,
    pending: Option<Action>,
    retries: u8,
    geometry: RefCell<Option<(Rc<markdown::FindAnchors>, Hit)>>,
    anchors: RefCell<Option<((usize, usize, usize), Rc<markdown::FindAnchors>)>>,
    notice: Option<String>,
    fragments: bool,
    queued: bool,
    expanded: bool,
}

impl Default for Ui {
    fn default() -> Self {
        Self {
            owner: None,
            entries: Ok(vec![]),
            revision: 0,
            pending: None,
            retries: 0,
            geometry: RefCell::default(),
            anchors: RefCell::default(),
            notice: None,
            fragments: false,
            queued: false,
            expanded: false,
        }
    }
}
impl Ui {
    pub(super) fn interaction_revision(&self) -> u64 {
        self.revision
    }
    pub(super) fn is_pending(&self) -> bool {
        self.pending.is_some()
    }
    fn cancel(&mut self) {
        self.revision = self.revision.wrapping_add(1);
        if self.pending.take().is_some() {
            self.notice = Some("Chapter navigation canceled".into());
        }
        self.queued = false;
        self.geometry.borrow_mut().take();
        self.anchors.borrow_mut().take();
    }
    fn owns_pending(&self, action: &Action) -> bool {
        self.owner.as_ref() == Some(&action.owner)
            && self.revision == action.revision
            && self.pending.as_ref().is_some_and(|p| {
                p.owner == action.owner && p.revision == action.revision && p.entry == action.entry
            })
    }
    fn cancel_action(&mut self, action: &Action, notice: &str) -> bool {
        if !self.owns_pending(action) {
            return false;
        }
        self.cancel();
        self.notice = Some(notice.into());
        true
    }
    fn accepts(&self, action: &Action, current: Option<&Owner>) -> bool {
        self.owns_pending(action)
            && current == Some(&action.owner)
            && self
                .entries
                .as_ref()
                .is_ok_and(|e| e.contains(&action.entry))
    }
    pub(super) fn presentation(
        &self,
        state: &IncludesState,
        item: usize,
        start: usize,
        blocks: &[markdown::Block],
    ) -> Option<(Matches, Option<usize>, Rc<markdown::FindAnchors>)> {
        let action = self.pending.as_ref()?;
        let e = &action.entry;
        if e.item != item || e.block < start || e.block >= start + blocks.len() {
            return None;
        }
        let snapshot = state.result.as_ref()?.as_ref().ok()?;
        let markdown::PreviewItem::Host(original) =
            snapshot.preview.as_ref().ok()?.items.get(item)?
        else {
            return None;
        };
        let local = e.block - start;
        let b = blocks.get(local)?;
        if original.get(e.block) != Some(b) || !matches!(b.kind, markdown::BlockKind::Heading(_)) {
            return None;
        }
        let range = 0..b.text.chars().next()?.len_utf8();
        let hit = Hit {
            target: Target::Block(local),
            range,
        };
        let slice = (item, start, blocks.len());
        let mut cache = self.anchors.borrow_mut();
        if cache.as_ref().is_none_or(|(key, _)| *key != slice) {
            *cache = Some((
                slice,
                Rc::new(markdown::FindAnchors::new(blocks, &state.preview_scroll)),
            ));
        }
        let anchors = cache.as_ref()?.1.clone();
        *self.geometry.borrow_mut() = Some((anchors.clone(), hit.clone()));
        Some((
            Matches {
                hits: vec![hit],
                ..Default::default()
            },
            Some(0),
            anchors,
        ))
    }
}
fn source_headings_match(entries: &[Entry], source: &[markdown::Block]) -> bool {
    let outline = markdown::outline(source);
    outline.len() == entries.len()
        && outline
            .iter()
            .zip(entries)
            .all(|(a, b)| a.anchor == b.anchor && a.title == b.title && a.level == b.level)
}
impl Workspace {
    fn contents_owner(&self, cx: &Context<Self>) -> Option<Owner> {
        let s = self.shell.includes.as_ref()?;
        if !s.preview_open
            || s.fetching
            || s.find.is_open()
            || s.query_nav.pending.is_some()
            || !self.query_control_gate(&s.identity, cx).allowed()
            || !self.includes_body_current(s)
            || self.shell.accepted_title_request != Some(self.request)
            || self.recent_open()
            || self.reader_hierarchy_open()
            || self.enlarged_image_open()
            || self.creation_open()
            || self.shell.find.is_composing(cx)
            || self.shell.calendar_zone.is_composing(cx)
        {
            return None;
        }
        let registry = self.follow_registry()?;
        let storage = self.live.as_ref()?.initial_storage.as_ref()?;
        let vault = &s.identity.source.note.vault;
        let canonical = if vault.is_empty() { &registry.0 } else { vault };
        if !registry.1.contains(canonical)
            || !self.session_ui.permits(vault)
            || storage.phase != "ready"
            || storage.generation != s.identity.source.storage_generation
            || self.accepted_storage_generation != Some(storage.generation)
        {
            return None;
        }
        Some(Owner {
            identity: s.identity.clone(),
            serial: s.serial,
            epoch: s.preview_epoch,
            mode: self.shell.rendered_reading.generation(),
            scope: self.working_vault_scope().into(),
            scope_revision: self.shell.scope_revision,
            registry,
            pages: s.query_pages.clone(),
            sources: s.query_sources.clone(),
        })
    }
    pub(super) fn cancel_preview_contents(&mut self) {
        if let Some(s) = self.shell.includes.as_mut() {
            s.contents.cancel();
        }
    }
    pub(super) fn begin_preview_contents(&mut self, window: &Window, cx: &Context<Self>) {
        let owner = self
            .contents_owner(cx)
            .filter(|_| window.is_window_active());
        let s = self.shell.includes.as_mut().unwrap();
        s.contents.geometry.borrow_mut().take();
        if s.contents.owner != owner {
            s.contents.cancel();
            s.contents.expanded = false;
            s.contents.owner = owner;
            s.contents.notice = None;
            s.contents.fragments = false;
            s.contents.entries = if s.contents.owner.is_some() {
                s.result
                    .as_ref()
                    .and_then(|r| r.as_ref().ok())
                    .and_then(|s| s.preview.as_ref().ok())
                    .ok_or_else(|| "Contents unavailable: no accepted host projection".into())
                    .and_then(entries)
            } else {
                Ok(vec![])
            };
            if let (Ok(entries), Some(body)) = (&s.contents.entries, &s.accepted_body) {
                s.contents.fragments =
                    source_headings_match(entries, &markdown::parse_reader(body));
            }
        }
    }
    fn activate_preview_heading(
        &mut self,
        owner: &Owner,
        revision: u64,
        entry: &Entry,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !window.is_window_active() || self.contents_owner(cx).as_ref() != Some(owner) {
            return;
        }
        let s = self.shell.includes.as_mut().unwrap();
        if s.contents.owner.as_ref() != Some(owner)
            || s.contents.revision != revision
            || !s.contents.entries.as_ref().is_ok_and(|e| e.contains(entry))
        {
            return;
        }
        s.contents.cancel();
        s.contents.retries = 0;
        s.contents.notice = Some("Locating host heading…".into());
        s.contents.pending = Some(Action {
            owner: owner.clone(),
            revision: s.contents.revision,
            entry: entry.clone(),
        });
        self.clear_preview_selection();
        cx.notify();
    }
    pub(super) fn preview_contents_controls(&self, cx: &mut Context<Self>) -> Div {
        let Some(s) = self.shell.includes.as_ref() else {
            return div();
        };
        let mut ui = div()
            .flex()
            .flex_col()
            .child("Host Contents (rendered view only)");
        ui = ui.child(
            div()
                .h(px(24.))
                .overflow_hidden()
                .whitespace_nowrap()
                .child(s.contents.notice.clone().unwrap_or_default()),
        );
        let Some(owner) = self.contents_owner(cx) else {
            return ui.child("Contents unavailable while another preview action owns the view.");
        };
        let toggle_owner = owner.clone();
        let toggle_revision = s.contents.revision;
        ui = ui.child(
            div()
                .id("toggle-rendered-contents")
                .h(px(24.))
                .cursor_pointer()
                .child(if s.contents.expanded {
                    "Hide Contents"
                } else {
                    "Show Contents"
                })
                .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_click(cx.listener(move |this, _, window, cx| {
                    if !window.is_window_active()
                        || this.contents_owner(cx).as_ref() != Some(&toggle_owner)
                    {
                        return;
                    }
                    let state = this.shell.includes.as_mut().unwrap();
                    if state.contents.revision != toggle_revision {
                        return;
                    }
                    state.contents.cancel();
                    state.contents.expanded = !state.contents.expanded;
                    this.clear_preview_selection();
                    cx.notify();
                })),
        );
        if !s.contents.expanded {
            return ui;
        }
        let entries = match &s.contents.entries {
            Ok(e) => e,
            Err(e) => return ui.child(e.clone()),
        };
        if entries.is_empty() {
            return ui.child("No host headings.");
        }
        let revision = s.contents.revision;
        let rows = entries
            .iter()
            .enumerate()
            .map(|(i, e)| {
                let owner = owner.clone();
                let entry = e.clone();
                div()
                    .id(gpui::SharedString::from(format!("rendered-contents-{i}")))
                    .pl(px(f32::from(e.level.saturating_sub(1)) * 12.))
                    .cursor_pointer()
                    .child(format!(
                        "{} · #{}",
                        e.title.chars().take(80).collect::<String>(),
                        e.anchor.chars().take(80).collect::<String>()
                    ))
                    .on_mouse_down(gpui::MouseButton::Left, |_, _, cx| cx.stop_propagation())
                    .on_click(cx.listener(move |this, _, window, cx| {
                        this.activate_preview_heading(&owner, revision, &entry, window, cx)
                    }))
            })
            .collect::<Vec<_>>();
        ui.child(
            div()
                .id("rendered-contents-list")
                .max_h(px(144.))
                .overflow_y_scroll()
                .children(rows),
        )
    }
    pub(super) fn local_host_heading_control(
        &self,
        item: usize,
        block: usize,
        link: &markdown::Link,
        label: &str,
        cx: &mut Context<Self>,
    ) -> Option<gpui::AnyElement> {
        let markdown::LinkTarget::Markdown { destination } = &link.target else {
            return None;
        };
        if !destination.starts_with('#') {
            return None;
        }
        let s = self.shell.includes.as_ref()?;
        let heading = local_heading(link);
        let entry = heading
            .as_ref()
            .and_then(|h| {
                s.contents
                    .entries
                    .as_ref()
                    .ok()?
                    .iter()
                    .find(|e| &e.anchor == h)
            })
            .cloned();
        let Some(owner) = self.contents_owner(cx) else {
            return Some(
                div()
                    .child("Host heading unavailable while another preview action owns the view")
                    .into_any_element(),
            );
        };
        if !s.contents.fragments || entry.is_none() {
            return Some(div().child(format!("Host heading not reached: {label} (missing, invalid, or source/projection headings differ)")).into_any_element());
        }
        let revision = s.contents.revision;
        let entry = entry.unwrap();
        let link = link.clone();
        Some(div().id(gpui::SharedString::from(format!("host-heading-{item}-{block}-{}",link.range.start))).cursor_pointer().child(format!("Go to host heading: {label}"))
            .on_mouse_down(gpui::MouseButton::Left,|_,_,cx|cx.stop_propagation())
            .on_click(cx.listener(move|this,_,window,cx|{
                let valid=this.shell.includes.as_ref().and_then(|s|s.result.as_ref()).and_then(|r|r.as_ref().ok()).and_then(|s|s.preview.as_ref().ok()).and_then(|p|p.items.get(item)).is_some_and(|p|matches!(p,markdown::PreviewItem::Host(b) if b.get(block).is_some_and(|b|b.links.contains(&link))));
                if valid{this.activate_preview_heading(&owner,revision,&entry,window,cx);}
            })).into_any_element())
    }
    pub(super) fn finish_preview_contents(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let s = self.shell.includes.as_mut().unwrap();
        if s.contents.queued {
            return;
        }
        let Some(action) = s.contents.pending.clone() else {
            return;
        };
        let Some((anchors, hit)) = s.contents.geometry.borrow().clone() else {
            s.contents.pending = None;
            s.contents.notice =
                Some("Host heading not reached: no displayed heading geometry".into());
            cx.notify();
            return;
        };
        s.contents.queued = true;
        let offset = s.preview_scroll.offset();
        let bounds = s.preview_scroll.bounds();
        let extent = s.preview_scroll.max_offset();
        let weak = cx.entity().downgrade();
        window.on_next_frame(move |window, cx| {
            let _ = weak.update(cx, |this, cx| {
                let current = this.contents_owner(cx);
                let Some(s) = this.shell.includes.as_mut() else {
                    return;
                };
                if !s.contents.owns_pending(&action) {
                    return;
                }
                let geometry_current = s
                    .contents
                    .geometry
                    .borrow()
                    .as_ref()
                    .is_some_and(|(a, h)| Rc::ptr_eq(a, &anchors) && h == &hit);
                if !window.is_window_active()
                    || !s.contents.accepts(&action, current.as_ref())
                    || !geometry_current
                {
                    if s.contents.cancel_action(
                        &action,
                        "Host heading not reached: focus, owner or layout changed",
                    ) {
                        cx.notify();
                    }
                    return;
                }
                s.contents.queued = false;
                if s.preview_scroll.offset() != offset
                    || s.preview_scroll.bounds() != bounds
                    || s.preview_scroll.max_offset() != extent
                {
                    s.contents.cancel();
                    s.contents.notice =
                        Some("Host heading not reached: scroll/layout changed".into());
                    cx.notify();
                    return;
                }
                if anchors.scroll_to_hit(&hit, window.viewport_size()) {
                    s.contents.pending = None;
                    s.contents.notice = Some("Host heading reached".into());
                } else if s.contents.retries == 0 {
                    s.contents.retries = 1;
                } else {
                    s.contents.pending = None;
                    s.contents.notice =
                        Some("Host heading not reached: current layout unavailable".into());
                }
                cx.notify();
            });
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn preview(source: &str) -> markdown::IncludePreview {
        markdown::IncludePreview {
            items: vec![markdown::PreviewItem::Host(markdown::parse(source))],
            unsupported: vec![],
        }
    }
    fn link(destination: &str) -> markdown::Link {
        markdown::Link {
            range: 0..1,
            target: markdown::LinkTarget::Markdown {
                destination: destination.into(),
            },
        }
    }
    fn owner() -> Owner {
        let note = live::NoteKey {
            vault: "host".into(),
            note_id: 1,
        };
        Owner {
            identity: Identity {
                source: LinkSource {
                    note: note.clone(),
                    etag: "e".into(),
                    request: 1,
                    launch_generation: 2,
                    storage_generation: 3,
                },
                route: note,
            },
            serial: 4,
            epoch: 5,
            mode: 6,
            scope: "host".into(),
            scope_revision: 7,
            registry: ("host".into(), vec!["host".into()]),
            pages: Default::default(),
            sources: Default::default(),
        }
    }
    #[test]
    fn host_global_slug_collisions_exclude_include_headings() {
        let p = markdown::IncludePreview {
            items: vec![
                markdown::PreviewItem::Host(markdown::parse("# A-2\n\n# A\n")),
                markdown::PreviewItem::Include {
                    index: 0,
                    directive: "![[Other]]".into(),
                    blocks: markdown::parse("# A\n"),
                },
                markdown::PreviewItem::Host(markdown::parse("# A\n")),
            ],
            unsupported: vec![],
        };
        let e = entries(&p).unwrap();
        assert_eq!(
            e.iter().map(|e| e.anchor.as_str()).collect::<Vec<_>>(),
            vec!["a-2", "a", "a-3"]
        );
        assert_eq!(e.iter().map(|e| e.item).collect::<Vec<_>>(), vec![0, 0, 2]);
        let e = entries(&preview("# A\n# A\n# A-2\n")).unwrap();
        assert_eq!(
            e.iter().map(|e| e.anchor.as_str()).collect::<Vec<_>>(),
            vec!["a", "a-2", "a-2-2"]
        );
    }
    #[test]
    fn parser_headings_and_local_fragments_are_exact_and_decode_once() {
        let e=entries(&preview("# **日本語**\n\nSetext\n---\n\n```\n# not a heading\n```\n\n<div>\n# HTML not heading\n</div>\n\n#\n")).unwrap();
        assert_eq!(
            e.iter().map(|e| e.anchor.as_str()).collect::<Vec<_>>(),
            vec!["日本語", "setext", "section"]
        );
        assert_eq!(
            local_heading(&link("#%E6%97%A5%E6%9C%AC%E8%AA%9E")).as_deref(),
            Some("日本語")
        );
        assert_eq!(local_heading(&link("#%2520")).as_deref(), Some("%20"));
        for bad in ["#", "#%FF", "#%", "#a#b", "Other.md#a", "https://host/#a"] {
            assert!(local_heading(&link(bad)).is_none(), "{bad}");
        }
        assert!(
            local_heading(&markdown::Link {
                range: 0..2,
                target: markdown::LinkTarget::Wiki {
                    key: "C#".into(),
                    fragment: None,
                    authored_label: false
                }
            })
            .is_none()
        );
    }
    #[test]
    fn limits_fail_whole_and_source_mismatch_does_not_certify_links() {
        assert!(entries(&preview(&"# Heading\n".repeat(MAX_HEADINGS + 1))).is_err());
        let mut blocks = preview("# A");
        let markdown::PreviewItem::Host(b) = &mut blocks.items[0] else {
            unreachable!()
        };
        b[0].kind = markdown::BlockKind::Paragraph;
        b.resize(MAX_BLOCKS + 1, b[0].clone());
        assert!(entries(&blocks).is_err());
        let mut p = preview("# A");
        let markdown::PreviewItem::Host(b) = &mut p.items[0] else {
            unreachable!()
        };
        b[0].text = "x".repeat(MAX_TEXT + 1);
        assert!(entries(&p).is_err());
        let e = entries(&preview("# A\n# B")).unwrap();
        assert!(source_headings_match(
            &e,
            &markdown::parse("# A\n\ntext\n\n# B")
        ));
        assert!(!source_headings_match(
            &e,
            &markdown::parse("# A\n# A\n# B")
        ));
    }
    #[test]
    fn repeated_action_cancel_and_owner_changes_reject_old_reveal() {
        let owner = owner();
        let entry = entries(&preview("# A")).unwrap().remove(0);
        let mut ui = Ui::default();
        ui.owner = Some(owner.clone());
        ui.entries = Ok(vec![entry.clone()]);
        let first = Action {
            owner: owner.clone(),
            revision: ui.revision,
            entry: entry.clone(),
        };
        ui.pending = Some(first.clone());
        assert!(ui.accepts(&first, Some(&owner)));
        ui.queued = true; // manual scroll, selection/Find, or fold toggle retires the queued frame
        ui.cancel();
        assert!(!ui.queued);
        assert!(!ui.accepts(&first, Some(&owner)));
        let second = Action {
            owner: owner.clone(),
            revision: ui.revision,
            entry,
        };
        ui.pending = Some(second.clone());
        assert!(ui.accepts(&second, Some(&owner)));
        assert!(!ui.accepts(&first, Some(&owner)));
        let mut other = owner.clone();
        other.epoch += 1;
        assert!(!ui.accepts(&second, Some(&other)));
        other = owner.clone();
        other.mode += 1;
        assert!(!ui.accepts(&second, Some(&other)));
        other = owner.clone();
        other.scope_revision += 1;
        assert!(!ui.accepts(&second, Some(&other)));
        other = owner.clone();
        other.identity.source.request += 1;
        assert!(!ui.accepts(&second, Some(&other)));
        assert!(!ui.accepts(&second, None));
    }
    #[test]
    fn inactive_frame_cancels_only_its_pending_action_and_can_retry() {
        let owner = owner();
        let entry = entries(&preview("# A")).unwrap().remove(0);
        let mut ui = Ui::default();
        ui.owner = Some(owner.clone());
        ui.entries = Ok(vec![entry.clone()]);
        let old = Action {
            owner: owner.clone(),
            revision: ui.revision,
            entry: entry.clone(),
        };
        ui.pending = Some(old.clone());
        ui.queued = true;
        assert!(ui.cancel_action(&old, "inactive"));
        assert!(!ui.queued);
        assert!(ui.pending.is_none());
        assert!(!ui.accepts(&old, Some(&owner)));
        let new = Action {
            owner: owner.clone(),
            revision: ui.revision,
            entry,
        };
        ui.pending = Some(new.clone());
        ui.queued = true;
        assert!(!ui.cancel_action(&old, "stale callback"));
        assert!(ui.queued);
        assert!(ui.accepts(&new, Some(&owner)));
        assert!(ui.cancel_action(&new, "geometry unavailable"));
        assert!(!ui.queued);
    }
}
