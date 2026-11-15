//! Dedicated launch-vault tag pages. Page routes are history entries, never tabs.
//! No search interpretation, cached working-vault list, pagination or implicit writes.
use super::*;

pub(crate) struct Page {
    pub tag: String,
    pub scope: String,
    request: u64,
    launch: Option<u64>,
    storage: Option<u64>,
    rows: Vec<live::Note>,
    loading: bool,
    error: Option<String>,
    qualification: String,
    scroll: gpui::UniformListScrollHandle,
}

fn normalize(tag: &str) -> String {
    // str::to_lowercase includes contextual final sigma (unlike char folding).
    tag.trim_end_matches('/').to_lowercase()
}
fn matches_tag(tag: &str, candidate: &str) -> bool {
    let candidate = candidate.to_lowercase();
    candidate == tag
        || candidate
            .strip_prefix(tag)
            .is_some_and(|s| s.starts_with('/'))
}
fn source_matches(
    source: Option<&shell::LinkSource>,
    current: Option<&shell::LinkSource>,
    tag: &str,
    tags: &[String],
) -> bool {
    source.is_some() && source == current && tags.iter().any(|value| value == tag)
}
fn response_current(
    request: u64,
    current_request: u64,
    launch: Option<u64>,
    current_launch: Option<u64>,
    storage: Option<u64>,
    current_storage: Option<u64>,
) -> bool {
    request == current_request && launch == current_launch && storage == current_storage
}
// A stable owned response can establish a newer epoch than the page started
// with. Admission against the original request/current epoch still happens first.
fn stable_listing_generation(before: Option<u64>, after: Option<u64>, failed: bool) -> bool {
    !failed && before.is_some() && before == after
}
fn qualification(fields: &std::collections::BTreeMap<String, serde_json::Value>) -> String {
    let cached = fields
        .get("read_state")
        .and_then(|s| s.get("cache_used"))
        .and_then(|v| v.as_bool())
        .unwrap_or(false);
    if workspace_live::partial(fields) || cached {
        "Partial or cached listing; tag membership may be incomplete.".into()
    } else {
        String::new()
    }
}

fn surface_blocks(
    search: bool,
    tab: bool,
    vault: bool,
    allow_search: bool,
    allow_tab: bool,
    allow_vault: bool,
) -> bool {
    (search && !allow_search) || (tab && !allow_tab) || (vault && !allow_vault)
}

impl Workspace {
    // Unlike the editor gate, this protects competing surfaces and composition owners.
    // Pending dirty decisions are deliberately left to guard_transition, which owns
    // Save/Discard/Cancel and freezes the first admitted intent.
    pub(crate) fn tag_routing_blocked(&self, cx: &App) -> bool {
        self.tag_routing_blocked_with_owner(false, false, false, false, cx)
    }
    pub(crate) fn reader_preview_routing_blocked(&self, cx: &App) -> bool {
        self.tag_routing_blocked_with_owner(false, false, false, true, cx)
    }
    pub(crate) fn tag_transition_blocked(
        &self,
        intent: &workspace_edit::Transition,
        cx: &App,
    ) -> bool {
        use workspace_edit::Transition;
        let involves_page = self.tag_page.is_some()
            || matches!(intent, Transition::Tag { .. })
            || matches!(intent, Transition::History(ticket) if matches!(ticket.route, Some(tabs::TabKey::Tag { .. })));
        involves_page
            && !matches!(intent, Transition::Quit)
            && self.tag_routing_blocked_with_owner(
                matches!(intent, Transition::ActivateTab(_)),
                matches!(intent, Transition::WorkingVault(_)),
                false,
                false,
                cx,
            )
    }
    pub(crate) fn tag_search_result_blocked(&self, cx: &App) -> bool {
        self.tag_routing_blocked_with_owner(false, false, true, false, cx)
    }
    fn tag_routing_blocked_with_owner(
        &self,
        allow_tab_menu: bool,
        allow_vault_menu: bool,
        allow_search: bool,
        own_reader_preview: bool,
        cx: &App,
    ) -> bool {
        self.recent_open()
            || self.enlarged_image_open()
            || self.reader_hierarchy_open()
            || self.search.read(cx).is_composing()
            || self.shell.calendar_zone.is_composing(cx)
            || surface_blocks(
                self.shell.search_open,
                self.shell.tab_menu,
                self.shell.vault_menu,
                allow_search,
                allow_tab_menu,
                allow_vault_menu,
            )
            || self.editing_ui.tag_owner_busy()
            || self.shell.diagnostics
            || self.shell.find.open
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.handoff_active()
            || self.includes_preview_active()
            || self.previews.model.geometry_active()
            || (!own_reader_preview && self.previews.model.transient.is_some())
    }
    pub(crate) fn activate_reader_tag(
        &mut self,
        source: Option<&shell::LinkSource>,
        tag: &str,
        cx: &mut Context<Self>,
    ) {
        let tags = self
            .current_note
            .as_ref()
            .map(|n| n.note.tags.as_slice())
            .unwrap_or_default();
        if self.tag_routing_blocked(cx)
            || self.read_error.is_some()
            || !source_matches(source, self.current_link_source().as_ref(), tag, tags)
        {
            return;
        }
        self.open_tag_page(normalize(tag), String::new(), cx);
    }
    pub(crate) fn open_tag_page(&mut self, tag: String, scope: String, cx: &mut Context<Self>) {
        if self.guard_transition(
            workspace_edit::Transition::Tag {
                tag: tag.clone(),
                scope: scope.clone(),
            },
            cx,
        ) {
            return;
        }
        let valid = tabs::TabKey::valid_tag_route(&tag, &scope);
        // Invalid literals get a visible error but never enter persisted history or I/O.
        if valid {
            self.history.record(Some(tabs::TabKey::Tag {
                tag: tag.clone(),
                scope: scope.clone(),
            }));
        }
        self.suspend_host_reader_actions();
        self.request += 1;
        self.graph_request += 1;
        self.query_epoch
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        self.selected = None;
        self.content.clear();
        self.current_note = None;
        self.current_note_request = None;
        self.current_related.clear();
        self.read_error = None;
        self.partial_read = false;
        self.show_graph = false;
        self.shell.home = false;
        let request = self.request;
        let launch = self.live.as_ref().map(|s| s.client().launch_generation);
        let storage = self.accepted_storage_generation;
        self.tag_page = Some(Page {
            tag: tag.clone(),
            scope,
            request,
            launch,
            storage,
            rows: Vec::new(),
            loading: valid,
            error: (!valid).then(|| "Invalid tag".into()),
            qualification: String::new(),
            scroll: Default::default(),
        });
        self.session_ui
            .sync_with_scope(&self.tabs, &self.history, &self.shell.working_scope);
        cx.notify();
        if !valid {
            return;
        }
        let Some(session) = self.live.clone() else {
            let page = self.tag_page.as_mut().unwrap();
            page.loading = false;
            page.error = Some(
                "Tag listing requires the owned server; no fallback vault read was sent.".into(),
            );
            return;
        };
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let before = client.storage();
            let result = client.notes(""); // Empty scope is explicitly the launch listing, NOT search.
            let after = client.storage();
            if !stable_listing_generation(
                before.as_ref().ok().map(|s| s.generation),
                after.as_ref().ok().map(|s| s.generation),
                session.failure().is_some(),
            ) {
                return (Err(live::Error::Stale), after);
            }
            (result, after)
        });
        cx.spawn(async move |this, cx| {
            let (result, observed_storage) = job.await;
            let _ = this.update(cx, |this, cx| {
                let current_launch = this.live.as_ref().map(|s| s.client().launch_generation);
                if !response_current(
                    request,
                    this.request,
                    launch,
                    current_launch,
                    storage,
                    this.accepted_storage_generation,
                ) {
                    return;
                }
                if !this
                    .tag_page
                    .as_ref()
                    .is_some_and(|p| p.request == request && p.tag == tag)
                {
                    return;
                }
                // Check the original response ticket before adopting an epoch. The
                // shared observer invalidates all other note/search/graph work.
                // Rebind this page only after that invalidation, so row callbacks
                // from its preceding generation cannot open a current row.
                if let Ok(state) = &observed_storage {
                    this.observe_storage(state, cx);
                }
                let page = this.tag_page.as_mut().unwrap();
                page.request = this.request;
                page.storage = this.accepted_storage_generation;
                page.loading = false;
                match result {
                    Ok(response) => {
                        page.qualification = qualification(&response.diagnostics);
                        page.rows = response
                            .notes
                            .into_iter()
                            .filter(|note| {
                                note.tags
                                    .iter()
                                    .any(|candidate| matches_tag(&tag, candidate))
                            })
                            .collect();
                    }
                    Err(error) => page.error = Some(error.to_string()),
                }
                cx.notify();
            });
        })
        .detach();
    }
    fn tag_rows_current(&self, page: &Page) -> bool {
        response_current(
            page.request,
            self.request,
            page.launch,
            self.live.as_ref().map(|s| s.client().launch_generation),
            page.storage,
            self.accepted_storage_generation,
        ) && self.quit_session.failure().is_none()
            && !page.loading
            && page.error.is_none()
    }
    fn activate_tag_result(
        &mut self,
        request: u64,
        index: usize,
        key: &live::NoteKey,
        cx: &mut Context<Self>,
    ) {
        if self.tag_routing_blocked(cx) {
            return;
        }
        let Some(page) = &self.tag_page else {
            return;
        };
        if page.request != request
            || !self.tag_rows_current(page)
            || !page.rows.get(index).is_some_and(|n| &n.key() == key)
        {
            return;
        }
        self.open_live_key(key.clone(), cx);
    }
    pub(crate) fn render_tag_page(
        &self,
        ink: u32,
        muted: u32,
        line: u32,
        cx: &mut Context<Self>,
    ) -> gpui::AnyElement {
        let page = self.tag_page.as_ref().unwrap();
        let current = response_current(
            page.request,
            self.request,
            page.launch,
            self.live.as_ref().map(|s| s.client().launch_generation),
            page.storage,
            self.accepted_storage_generation,
        ) && self.quit_session.failure().is_none();
        let status = if !current {
            "Listing is no longer current. Reload to read this tag again.".into()
        } else if let Some(error) = &page.error {
            format!("Tag listing failed: {error}")
        } else if page.loading {
            "Loading tag notes…".into()
        } else if page.rows.is_empty() && page.qualification.is_empty() {
            "No notes carry this tag".into()
        } else if page.rows.is_empty() {
            "No matches in the available partial/cached listing.".into()
        } else {
            format!("{} matching notes", page.rows.len())
        };
        let count = if self.tag_rows_current(page) {
            page.rows.len()
        } else {
            0
        };
        let request = page.request;
        div()
            .id("tag-page")
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .pl(px(80.))
            .pr_6()
            .py_6()
            .text_color(rgb(ink))
            .child(div().text_size(px(28.)).child(format!("#{}", page.tag)))
            .child(
                div()
                    .text_color(rgb(muted))
                    .child("Launch vault · read-only tag listing"),
            )
            .child(div().py_2().child(status))
            .child(
                div()
                    .text_color(rgb(muted))
                    .child(page.qualification.clone()),
            )
            .child(
                uniform_list(
                    "tag-results",
                    count,
                    cx.processor(move |this, range: std::ops::Range<usize>, _, cx| {
                        range
                            .filter_map(|index| {
                                let page =
                                    this.tag_page.as_ref().filter(|p| p.request == request)?;
                                if !this.tag_rows_current(page) {
                                    return None;
                                }
                                let row = page.rows.get(index)?;
                                let key = row.key();
                                let title = format!(
                                    "{} {}",
                                    row.title,
                                    row.flags
                                        .iter()
                                        .map(|s| format!("[{s}]"))
                                        .collect::<Vec<_>>()
                                        .join(" ")
                                );
                                let identity = format!(
                                    "{} · note {}",
                                    if row.vault.is_empty() {
                                        "Launch vault"
                                    } else {
                                        &row.vault
                                    },
                                    row.note_id
                                );
                                Some(
                                    div()
                                        .id(("tag-result", index))
                                        .h(px(72.))
                                        .py_2()
                                        .border_b_1()
                                        .border_color(rgb(line))
                                        .cursor_pointer()
                                        .child(div().child(title))
                                        .child(
                                            div()
                                                .text_size(px(11.))
                                                .text_color(rgb(muted))
                                                .child(identity),
                                        )
                                        .on_click(cx.listener(move |this, _, _, cx| {
                                            this.activate_tag_result(request, index, &key, cx)
                                        })),
                                )
                            })
                            .collect()
                    }),
                )
                .track_scroll(page.scroll.clone())
                .flex_1(),
            )
            .into_any_element()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn owning_surface_navigation_is_distinct_from_obscured_page_clicks() {
        assert!(surface_blocks(true, false, false, false, false, false));
        assert!(!surface_blocks(true, false, false, true, false, false));
        assert!(surface_blocks(false, true, false, false, false, false));
        assert!(!surface_blocks(false, true, false, false, true, false));
        assert!(surface_blocks(false, false, true, false, false, false));
        assert!(!surface_blocks(false, false, true, false, false, true));
        assert!(surface_blocks(true, true, false, true, false, false));
    }
    #[test]
    fn literal_hierarchy_unicode_and_contextual_lowercase() {
        assert_eq!(normalize("ΟΣ/"), "ος");
        assert_eq!(normalize("İ/A///"), "i\u{307}/a");
        assert_eq!(normalize(" a /"), " a ");
        assert_eq!(normalize("///"), "");
        for value in ["a", "a/b", "A/C"] {
            assert!(matches_tag("a", value));
        }
        for value in ["ab", "ba", "b/a", " a"] {
            assert!(!matches_tag("a", value));
        }
        assert!(matches_tag(&normalize("ΟΣ"), "ΟΣ"));
        assert!(matches_tag("a ?#", "A ?#/child"));
    }
    #[test]
    fn full_order_is_preserved_without_search_limit() {
        let rows: Vec<_> = (0..200)
            .filter(|i| matches_tag("a", if i % 2 == 0 { "a/b" } else { "ab" }))
            .collect();
        assert_eq!(rows, (0..200).step_by(2).collect::<Vec<_>>());
        assert_eq!(rows.len(), 100);
    }
    #[test]
    fn stable_new_epoch_refresh_is_admitted_but_straddling_reads_are_not() {
        // Started at epoch 3; no intervening navigation or observer changed it.
        assert!(response_current(1, 1, Some(2), Some(2), Some(3), Some(3)));
        assert!(stable_listing_generation(Some(4), Some(4), false));
        // After shared observation the page is rebound, invalidating old rows.
        assert!(!response_current(1, 2, Some(2), Some(2), Some(3), Some(4)));
        assert!(response_current(2, 2, Some(2), Some(2), Some(4), Some(4)));
        assert!(!stable_listing_generation(Some(3), Some(4), false));
        assert!(!stable_listing_generation(None, None, false));
        assert!(!stable_listing_generation(Some(4), Some(4), true));
        // A different observer accepting epoch 5 before this response wins.
        assert!(!response_current(1, 1, Some(2), Some(2), Some(3), Some(5)));
    }
    #[test]
    fn stale_response_identity_never_admits_rows() {
        assert!(response_current(1, 1, Some(2), Some(2), Some(3), Some(3)));
        assert!(!response_current(1, 2, Some(2), Some(2), Some(3), Some(3)));
        assert!(!response_current(1, 1, Some(2), Some(4), Some(3), Some(3)));
        assert!(!response_current(1, 1, Some(2), Some(2), Some(3), Some(4)));
    }
    #[test]
    fn cached_results_are_not_exhaustive() {
        let fields =
            serde_json::from_value(serde_json::json!({"read_state":{"cache_used":true}})).unwrap();
        assert!(!qualification(&fields).is_empty());
        assert!(qualification(&Default::default()).is_empty());
    }
    #[test]
    fn stale_source_and_equal_numeric_foreign_id_are_rejected() {
        let source = shell::LinkSource {
            note: live::NoteKey {
                vault: "a".into(),
                note_id: 7,
            },
            etag: "e".into(),
            storage_generation: 1,
            launch_generation: 1,
            request: 1,
        };
        let mut other = source.clone();
        other.note.vault = "b".into();
        let tags = vec!["A/B".into()];
        assert!(source_matches(Some(&source), Some(&source), "A/B", &tags));
        assert!(!source_matches(Some(&source), Some(&other), "A/B", &tags));
        assert!(!source_matches(Some(&source), None, "A/B", &tags));
        assert!(!source_matches(Some(&source), Some(&source), "A", &tags));
    }
}
