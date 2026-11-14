//! Full-vault view, separate from the note-local aside. No writes or note visits.
use super::*;
#[derive(Default)]
pub struct FullGraph {
    scope: Option<String>,
    nodes: std::collections::BTreeSet<i64>,
    caption: String,
    status: String,
    loading: bool,
}
impl Workspace {
    pub(crate) fn invalidate_full_graph(&mut self, cx: &mut Context<Self>) {
        self.full_graph = FullGraph {
            status: "Graph invalidated by storage/server change. Reload graph.".into(),
            ..Default::default()
        };
        self.graph_view
            .update(cx, |v, cx| v.set_graph(graph::Graph::default(), cx));
    }
    pub(crate) fn full_graph_active(&self) -> bool {
        self.tabs
            .active()
            .is_some_and(|t| t.key == tabs::TabKey::Graph)
    }
    pub(crate) fn open_full_graph(&mut self, cx: &mut Context<Self>) {
        if self.guard_transition(workspace_edit::Transition::FullGraph, cx) {
            return;
        }
        self.tabs.open_graph();
        self.shell.home = false;
        self.shell.search_open = false;
        self.shell.tab_menu = false;
        self.selected = None;
        self.read_error = None;
        self.partial_read = false;
        self.current_note = None;
        self.content.clear();
        self.current_related.clear();
        self.shell.find.open = false;
        self.request += 1; // invalidate pending note reads
        self.graph_request += 1;
        let request = self.graph_request;
        let scope = self.working_vault_scope().to_owned();
        self.full_graph = FullGraph {
            loading: true,
            status: "Loading graph…".into(),
            ..Default::default()
        };
        self.graph_view
            .update(cx, |view, cx| view.set_graph(graph::Graph::default(), cx));
        let Some(session) = self.live.clone() else {
            self.full_graph.loading = false;
            self.full_graph.status = "Full graph requires the owned server (--server-port); the local aside is not a full graph.".into();
            cx.notify();
            return;
        };
        let launch = session.client().launch_generation;
        let epoch = self.accepted_storage_generation;
        let captured_scope = scope.clone();
        let job = cx.background_executor().spawn(async move {
            let client = session.client();
            let before = client.storage();
            let result = client.full_graph(&captured_scope);
            let after = client.storage();
            if !matches!((&before, &after), (Ok(a), Ok(b)) if a.generation == b.generation && Some(b.generation) == epoch) {
                return Err(live::Error::Stale);
            }
            result
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if !accept_completion(
                    this.full_graph_active(),
                    request,
                    this.graph_request,
                    &scope,
                    this.working_vault_scope(),
                    epoch,
                    this.accepted_storage_generation,
                    launch,
                    this.live.as_ref().map(|s| s.client().launch_generation),
                ) {
                    return;
                }
                this.full_graph.loading = false;
                match result {
                    Ok((graph, fields)) => {
                        let graph::Overview { graph, hidden } = graph::overview(&graph);
                        let count = graph.nodes.len();
                        this.full_graph.nodes = graph.nodes.iter().map(|n| n.note_id).collect();
                        this.full_graph.scope = Some(scope);
                        this.full_graph.caption = caption(count, hidden);
                        this.full_graph.status = fields
                            .iter()
                            .filter(|(k, _)| k.as_str() != "vault")
                            .map(|(k, v)| format!("{k}: {v}"))
                            .collect::<Vec<_>>()
                            .join(" · ");
                        this.graph_view
                            .update(cx, |view, cx| view.set_graph(graph, cx));
                    }
                    Err(error) => {
                        this.full_graph.status = format!("Graph: {error}");
                    }
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn open_full_graph_node(&mut self, id: i64, cx: &mut Context<Self>) {
        if !self.full_graph_active() || !self.full_graph.nodes.contains(&id) {
            return;
        }
        if let Some(vault) = self.full_graph.scope.clone() {
            // Captured response scope, never the current note's vault or a node path.
            self.open_live_key(live::NoteKey { vault, note_id: id }, cx);
        }
    }
    pub(crate) fn render_full_graph(&self, cx: &mut Context<Self>) -> gpui::Div {
        div()
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .pl(px(64.))
            .child(div().text_xs().child(self.full_graph.status.clone()))
            .child(div().flex_1().min_h_0().child(self.graph_view.clone()))
            .child(
                div()
                    .flex()
                    .justify_between()
                    .p_2()
                    .text_xs()
                    .child(self.full_graph.caption.clone())
                    .child(
                        div()
                            .flex()
                            .gap_3()
                            .child(
                                div()
                                    .id("graph-titles")
                                    .cursor_pointer()
                                    .child(if self.graph_view.read(cx).titles_visible() {
                                        "Hide note titles"
                                    } else {
                                        "Show note titles"
                                    })
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.graph_view.update(cx, |v, cx| v.toggle_titles(cx))
                                    })),
                            )
                            .child(
                                div()
                                    .id("full-graph-reset")
                                    .cursor_pointer()
                                    .child("Reset graph view")
                                    .on_click(cx.listener(|this, _, _, cx| {
                                        this.graph_view.update(cx, |v, cx| v.reset_view(cx))
                                    })),
                            )
                            .child(
                                div()
                                    .id("full-graph-retry")
                                    .cursor_pointer()
                                    .child("Reload graph")
                                    .on_click(
                                        cx.listener(|this, _, _, cx| this.open_full_graph(cx)),
                                    ),
                            ),
                    ),
            )
    }
}
fn number(n: usize) -> String {
    let s = n.to_string();
    s.chars()
        .enumerate()
        .fold(String::new(), |mut out, (i, c)| {
            if i > 0 && (s.len() - i) % 3 == 0 {
                out.push(',');
            }
            out.push(c);
            out
        })
}
fn caption(visible: usize, hidden: usize) -> String {
    let mut text = match visible {
        0 => "No linked notes".into(),
        1 => "1 note".into(),
        n => format!("{} notes", number(n)),
    };
    if hidden > 0 {
        text.push_str(&format!(" · {} not drawn", number(hidden)));
    }
    text
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn counts() {
        assert_eq!(caption(0, 1001), "No linked notes · 1,001 not drawn");
        assert_eq!(caption(1, 0), "1 note");
    }
}

#[allow(clippy::too_many_arguments)]
fn accept_completion(
    active: bool,
    request: u64,
    current_request: u64,
    scope: &str,
    current_scope: &str,
    epoch: Option<u64>,
    current_epoch: Option<u64>,
    launch: u64,
    current_launch: Option<u64>,
) -> bool {
    active
        && request == current_request
        && scope == current_scope
        && epoch == current_epoch
        && current_launch == Some(launch)
}
#[cfg(test)]
mod lifetime_tests {
    use super::*;
    #[test]
    fn exit_scope_retry_storage_and_launch_reject_late_completion() {
        assert!(accept_completion(
            true,
            1,
            1,
            "a",
            "a",
            Some(2),
            Some(2),
            3,
            Some(3)
        ));
        assert!(!accept_completion(
            false,
            1,
            1,
            "a",
            "a",
            Some(2),
            Some(2),
            3,
            Some(3)
        ));
        assert!(!accept_completion(
            true,
            1,
            1,
            "a",
            "b",
            Some(2),
            Some(2),
            3,
            Some(3)
        ));
        assert!(!accept_completion(
            true,
            1,
            2,
            "a",
            "a",
            Some(2),
            Some(2),
            3,
            Some(3)
        ));
        assert!(!accept_completion(
            true,
            1,
            1,
            "a",
            "a",
            Some(2),
            Some(4),
            3,
            Some(3)
        ));
        assert!(!accept_completion(
            true,
            1,
            1,
            "a",
            "a",
            Some(2),
            Some(2),
            3,
            Some(4)
        ));
    }
}
