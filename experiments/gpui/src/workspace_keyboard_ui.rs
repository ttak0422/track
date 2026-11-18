//! Real Workspace rendering and event dispatch in GPUI's virtual TestPlatform.
//! The owned v2 engine reads only a newly generated disposable fixture.
use super::*;
use gpui::{AppContext, TestAppContext, VisualTestContext};

fn workspace(session: live::Session, cx: &mut Context<Workspace>) -> Workspace {
    let live_notes = session.client().notes("").unwrap().notes;
    let snapshot = Snapshot {
        notes: live_notes
            .iter()
            .map(|note| Note {
                note_id: note.note_id,
                title: note.title.clone(),
                tags: note.tags.clone(),
                path: note.path.clone().into(),
                file_kind: note.file_kind.clone(),
            })
            .collect(),
        read_state: None,
    };
    let mut workspace = Workspace {
        admission: Default::default(),
        shell: ShellState::default(),
        editing_ui: workspace_edit::EditingUi::new(None),
        tabs: Default::default(),
        history: Default::default(),
        tag_page: None,
        session_ui: workspace_session::SessionUi::new(None),
        event_awareness: Default::default(),
        refresh: Default::default(),
        follow: Default::default(),
        reader_images: Default::default(),
        reader_pdf: None,
        reader_math: None,
        math_reference_cache: Default::default(),
        pdf_enlargement: None,
        snapshot,
        selected: None,
        rendered_rows: 0,
        source: "isolated keyboard fixture".into(),
        backend: None,
        content: Vec::new(),
        current_note: None,
        current_note_request: None,
        read_error: None,
        partial_read: false,
        current_related: Default::default(),
        status: String::new(),
        list_status: String::new(),
        request: 0,
        live_reader_pending: None,
        graph_request: 0,
        pending_local_graph: None,
        graph_view: cx.new(|_| graph_view::GraphView::new(graph::Graph::default())),
        show_graph: false,
        full_graph: Default::default(),
        previews: Default::default(),
        subscriptions: Vec::new(),
        accepted_storage_generation: session.initial_storage.as_ref().map(|s| s.generation),
        live: Some(session),
        live_requested: true,
        live_notes,
        search_results: Vec::new(),
        search: cx.new(search_input::SearchInput::new),
        query_epoch: Default::default(),
        quit_session: Default::default(),
    };
    workspace.subscribe_search(cx);
    workspace.shell.home = false;
    // Select through production acceptance and tab logic, rather than fabricating
    // reader ownership, scroll geometry, a body, or a navigation result.
    let index = workspace
        .live_notes
        .iter()
        .position(|n| n.note_id == 42)
        .unwrap();
    workspace.select(index, cx);
    workspace
}

fn click(cx: &mut VisualTestContext, selector: &'static str) {
    let bounds = cx.debug_bounds(selector).expect(selector);
    cx.simulate_click(bounds.center(), gpui::Modifiers::default());
}

fn key_event(cx: &mut VisualTestContext, key: &str) {
    // Dispatch a native key event without fabricating platform text/IME output.
    cx.simulate_event(gpui::KeyDownEvent {
        keystroke: gpui::Keystroke::parse(key).unwrap(),
        is_held: false,
    });
}

fn reader_offset(view: &gpui::Entity<Workspace>, cx: &VisualTestContext) -> f32 {
    view.read_with(cx, |workspace, _| {
        let tab = &workspace.tabs.active().unwrap().key;
        f32::from(workspace.shell.reader_scrolls[tab].offset().y)
    })
}

fn search_roundtrip(cx: &mut TestAppContext, outside_click: bool) {
    live::with_keyboard_fixture(|session| {
        let (view, cx) = cx.add_window_view(|_, cx| workspace(session, cx));
        cx.run_until_parked();
        view.read_with(cx, |workspace, _| {
            assert!(
                workspace.current_note.is_some(),
                "reader must finish actual engine acceptance"
            );
            assert!(!workspace.partial_read);
            assert!(workspace.read_error.is_none());
        });
        click(cx, "keyboard-reader-row-0");
        cx.simulate_keystrokes("j");
        let before_search = reader_offset(&view, cx);
        assert!(
            before_search < 0.,
            "the actual plain-reader click must enable scrolling"
        );
        click(cx, "keyboard-search");
        cx.simulate_keystrokes("j k");
        view.read_with(cx, |workspace, app| {
            assert!(workspace.shell.search_open, "typing must keep Search open");
            assert_eq!(workspace.search.read(app).query(), "jk");
        });
        assert_eq!(
            reader_offset(&view, cx),
            before_search,
            "search input must not scroll the reader"
        );
        if outside_click {
            let bounds = cx.debug_bounds("keyboard-search-backdrop").unwrap();
            cx.simulate_click(
                point(bounds.left() + px(8.), bounds.bottom() - px(8.)),
                Default::default(),
            );
        } else {
            cx.simulate_keystrokes("escape");
        }
        cx.update(|window, app| {
            view.read_with(app, |workspace, app| {
                assert!(!workspace.shell.search_open);
                assert!(
                    workspace
                        .shell
                        .reader_focus
                        .as_ref()
                        .unwrap()
                        .is_focused(window)
                );
                assert!(!workspace.search.focus_handle(app).is_focused(window));
            })
        });
        cx.simulate_keystrokes("j");
        let after = reader_offset(&view, cx);
        assert!(
            after < before_search,
            "j must scroll after dismissal without another click"
        );
        cx.simulate_keystrokes("k");
        assert!((reader_offset(&view, cx) - before_search).abs() < 0.01);
    });
}

#[gpui::test]
#[ignore = "requires an explicit synthetic-only Go bridge binary"]
fn search_escape_restores_reader_keys_without_click(cx: &mut TestAppContext) {
    search_roundtrip(cx, false);
}

#[gpui::test]
#[ignore = "requires an explicit synthetic-only Go bridge binary"]
fn search_outside_click_restores_reader_keys_without_click(cx: &mut TestAppContext) {
    search_roundtrip(cx, true);
}

#[gpui::test]
#[ignore = "requires an explicit synthetic-only Go bridge binary"]
fn search_input_composition_and_tab_routing(cx: &mut TestAppContext) {
    use gpui::EntityInputHandler;
    live::with_keyboard_fixture(|session| {
        let (view, cx) = cx.add_window_view(|_, cx| workspace(session, cx));
        cx.update(|_, app| {
            view.update(app, |workspace, cx| {
                for id in [43, 42] {
                    let index = workspace
                        .live_notes
                        .iter()
                        .position(|n| n.note_id == id)
                        .unwrap();
                    workspace.select_from_ui(index, cx);
                }
            })
        });
        cx.run_until_parked();
        click(cx, "keyboard-reader-row-0");
        let active = |cx: &VisualTestContext| {
            view.read_with(cx, |w, _| w.tabs.active().unwrap().key.clone())
        };
        let first = active(cx);
        cx.simulate_keystrokes("tab");
        let second = active(cx);
        assert_ne!(first, second, "reader Tab must switch notes");
        cx.simulate_keystrokes("shift-tab");
        assert_eq!(
            active(cx),
            first,
            "Shift+Tab must reverse without another click"
        );
        click(cx, "keyboard-reader-row-0");
        click(cx, "keyboard-search");
        cx.simulate_input("normaljk");
        key_event(cx, "tab");
        key_event(cx, "shift-tab");
        assert_eq!(active(cx), first, "Search must not switch tabs");
        let input = view.read_with(cx, |w, _| w.search.clone());
        assert_eq!(input.read_with(cx, |s, _| s.query().to_owned()), "normaljk");
        // Inject the real platform preedit callback; this is not a physical IME test.
        cx.update(|window, app| {
            input.update(app, |s, cx| {
                s.replace_and_mark_text_in_range(None, "候補", Some(0..2), window, cx);
            })
        });
        key_event(cx, "escape");
        key_event(cx, "tab");
        key_event(cx, "shift-tab");
        view.read_with(cx, |w, app| {
            assert!(
                w.shell.search_open,
                "Escape must leave composing Search to the platform"
            );
            assert!(w.search.read(app).is_composing());
        });
        assert_eq!(active(cx), first);
        cx.update(|window, app| input.update(app, |s, cx| s.unmark_text(window, cx)));
        cx.simulate_keystrokes("escape tab");
        assert_eq!(
            active(cx),
            second,
            "Tab must resume on the restored reader focus"
        );
    });
}

#[gpui::test]
#[ignore = "requires an explicit synthetic-only Go bridge binary"]
fn search_dismissal_refuses_stale_reader_geometry(cx: &mut TestAppContext) {
    live::with_keyboard_fixture(|session| {
        let (view, cx) = cx.add_window_view(|_, cx| workspace(session, cx));
        click(cx, "keyboard-reader-row-0");
        click(cx, "keyboard-search");
        cx.simulate_resize(size(px(900.), px(600.)));
        cx.simulate_keystrokes("escape");
        let before = reader_offset(&view, cx);
        cx.simulate_keystrokes("j");
        assert_eq!(
            reader_offset(&view, cx),
            before,
            "old geometry must not grant reader keys"
        );
        view.read_with(cx, |w, _| assert!(w.shell.reader_keys.is_none()));
        click(cx, "keyboard-reader-row-0");
        cx.simulate_keystrokes("j");
        assert!(
            reader_offset(&view, cx) < before,
            "a new actual reader click may claim fresh geometry"
        );
    });
}

#[gpui::test]
#[ignore = "requires an explicit synthetic-only Go bridge binary"]
fn search_result_retires_previous_reader_owner(cx: &mut TestAppContext) {
    live::with_keyboard_fixture(|session| {
        let (view, cx) = cx.add_window_view(|_, cx| workspace(session, cx));
        click(cx, "keyboard-reader-row-0");
        cx.simulate_keystrokes("j");
        click(cx, "keyboard-search");
        cx.simulate_input("DestinationOnly");
        view.read_with(cx, |w, _| assert_eq!(w.search_results.len(), 1));
        cx.simulate_keystrokes("enter");
        view.read_with(cx, |w, _| {
            assert!(!w.shell.search_open);
            assert_eq!(w.current_note.as_ref().unwrap().note.note_id, 43);
            assert!(w.shell.search_return_reader.is_none());
            assert!(w.shell.reader_keys.is_none());
        });
        let before = reader_offset(&view, cx);
        cx.simulate_keystrokes("j");
        assert_eq!(
            reader_offset(&view, cx),
            before,
            "Search result must not inherit the old note's scrolling owner"
        );
        click(cx, "keyboard-reader-row-0");
        cx.simulate_keystrokes("j");
        assert!(reader_offset(&view, cx) < before);
    });
}

#[gpui::test]
#[ignore = "requires an explicit synthetic-only Go bridge binary"]
fn tab_sequence_keeps_initial_order_across_mru_moves(cx: &mut TestAppContext) {
    live::with_keyboard_fixture(|session| {
        let (view, cx) = cx.add_window_view(|_, cx| workspace(session, cx));
        cx.update(|_, app| {
            view.update(app, |workspace, cx| {
                for id in [43, 44, 42] {
                    let index = workspace
                        .live_notes
                        .iter()
                        .position(|n| n.note_id == id)
                        .unwrap();
                    workspace.select_from_ui(index, cx);
                }
            })
        });
        cx.run_until_parked();
        click(cx, "keyboard-reader-row-0");
        let order = view.read_with(cx, |w, _| {
            w.tabs
                .entries()
                .iter()
                .map(|t| t.key.clone())
                .collect::<Vec<_>>()
        });
        assert_eq!(order.len(), 3);
        for (key, expected) in [
            ("tab", 1),
            ("tab", 2),
            ("tab", 0),
            ("shift-tab", 2),
            ("shift-tab", 1),
        ] {
            cx.simulate_keystrokes(key);
            view.read_with(cx, |w, _| {
                assert_eq!(w.tabs.active().unwrap().key, order[expected])
            });
        }
    });
}

#[gpui::test]
#[ignore = "requires an explicit synthetic-only Go bridge binary"]
fn tab_refusal_retries_same_neighbor_without_another_click(cx: &mut TestAppContext) {
    live::with_keyboard_fixture(|session| {
        let (view, cx) = cx.add_window_view(|_, cx| workspace(session, cx));
        cx.update(|_, app| {
            view.update(app, |workspace, cx| {
                for id in [43, 44, 42] {
                    let index = workspace
                        .live_notes
                        .iter()
                        .position(|n| n.note_id == id)
                        .unwrap();
                    workspace.select_from_ui(index, cx);
                }
            })
        });
        cx.run_until_parked();
        click(cx, "keyboard-reader-row-0");
        let order = view.read_with(cx, |w, _| {
            w.tabs
                .entries()
                .iter()
                .map(|t| t.key.clone())
                .collect::<Vec<_>>()
        });
        cx.simulate_keystrokes("tab");
        cx.update(|_, app| view.update(app, |w, _| w.editing_ui.test_hold_recovery_cleanup(true)));
        cx.simulate_keystrokes("tab");
        view.read_with(cx, |w, _| {
            assert_eq!(
                w.tabs.active().unwrap().key,
                order[1],
                "existing cleanup gate must refuse departure"
            )
        });
        cx.update(|_, app| view.update(app, |w, _| w.editing_ui.test_hold_recovery_cleanup(false)));
        cx.simulate_keystrokes("tab");
        view.read_with(cx, |w, _| {
            assert_eq!(
                w.tabs.active().unwrap().key,
                order[2],
                "refusal must not consume the requested neighbor"
            )
        });
    });
}
