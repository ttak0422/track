//! Opt-in finite consumers against the real Go canonical-read-only server.
//! These are protocol/identity tests, not rendered UI-equivalence evidence.
use super::*;

#[test]
#[ignore = "requires an explicitly supplied synthetic Go bridge binary"]
fn bound_finite_workflows_keep_same_id_vaults_distinct() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    // Only synthetic content is added, before the Go manifest is captured. The
    // machine config, registry, peer configs and admission authority stay intact.
    for vault in ["main", "work"] {
        let base = fixture.0.join("home").join(vault);
        std::fs::write(
            base.join("note/43.md"),
            format!("{vault} workflowprobe child\n\nup:: [[id:42]]\n"),
        )
        .unwrap();
        std::fs::write(base.join(".track/notes/43.yaml"), format!("version: 1\ntitle: {vault} workflowprobe\ntags:\n  - workflow/{vault}\ncreated: 2026-07-18T12:00:00Z\n")).unwrap();
    }
    let before = admission_process_tests::protected_inventory(&fixture.0);
    let bound = SyntheticBound::new(proof, fixture.0.clone()).unwrap();
    let owner = OwnedServer::start_admitted(
        bound.proof.launch(),
        &AtomicBool::new(false),
        StartupAdmission::SyntheticBound(bound),
    )
    .unwrap();
    let session = Session::from_owned(owner);
    assert!(session.events().unavailable);
    let client = session.client();
    // Same numeric IDs must never collapse the default registry main into the
    // explicitly selected launch work, including nested graph identities.
    for (vault, sentinel) in [("main", "Active"), ("work", "Foreign")] {
        let notes = client.notes(vault).unwrap().notes;
        assert_eq!(notes.len(), 2);
        assert!(notes.iter().all(|note| note.vault == vault));
        let child = notes.iter().find(|note| note.note_id == 43).unwrap();
        assert_eq!(child.title, format!("{vault} workflowprobe"));
        assert_eq!(child.tags, vec![format!("workflow/{vault}")]);
        let key = NoteKey {
            vault: vault.into(),
            note_id: 42,
        };
        let note = client.note(&key).unwrap().note;
        assert_eq!(note.note.vault, vault);
        assert_eq!(note.body, format!("{sentinel} sentinel body"));
        let found = client.search("workflowprobe", vault).unwrap().results;
        assert_eq!(found.len(), 1);
        assert_eq!((found[0].note_id, found[0].vault.as_str()), (43, vault));
        assert_eq!(found[0].title, child.title);
        let recent = client.recently_created(vault, "work").unwrap().notes;
        assert_eq!(recent.len(), 2);
        assert_eq!(recent[0].note_id, 43);
        assert!(recent.iter().all(|note| note.vault == vault));
        let (graph, _) = client.full_graph(vault, "work").unwrap();
        assert_eq!(graph.nodes.len(), 2);
        assert!(
            graph
                .nodes
                .iter()
                .all(|node| node.vault.as_deref() == Some(vault))
        );
        assert!(
            graph
                .nodes
                .iter()
                .any(|node| node.note_id == 43 && node.title == child.title)
        );
        assert!(
            graph
                .edges
                .iter()
                .any(|edge| edge.source_id == 43 && edge.target_id == 42)
        );
        // The same key construction used by graph activation must open the
        // scoped child rather than the same numeric ID in the other vault.
        let graph_child = graph.nodes.iter().find(|node| node.note_id == 43).unwrap();
        let activated = client
            .note(&NoteKey {
                vault: graph_child.vault.clone().expect("normalized node identity"),
                note_id: graph_child.note_id,
            })
            .unwrap()
            .note;
        assert_eq!(activated.note.vault, vault);
        assert_eq!(
            activated.body,
            format!("{vault} workflowprobe child\n\nup:: [[id:42]]")
        );
        let (local, _) = client.local_graph(&key, "work").unwrap();
        assert_eq!(local.center_id, 42);
        assert!(
            local
                .nodes
                .iter()
                .all(|node| node.vault.as_deref() == Some(vault))
        );
        assert!(
            local
                .nodes
                .iter()
                .any(|node| node.note_id == 43 && node.title == child.title)
        );
    }
    // Workspace search is federated, unlike no-selector notes/graphs. Opening
    // each returned key must preserve identity even though both IDs are 43.
    let results = client.search("workflowprobe", "").unwrap().results;
    assert_eq!(results.len(), 2);
    let mut identities = std::collections::BTreeSet::new();
    for result in results {
        let key = result.key();
        let canonical_vault = if key.vault.is_empty() {
            "work"
        } else {
            &key.vault
        };
        assert_eq!(key.note_id, 43);
        assert!(identities.insert(canonical_vault.to_owned()));
        let opened = client.note(&key).unwrap().note;
        assert_eq!(
            opened.note.title,
            format!("{canonical_vault} workflowprobe")
        );
        assert_eq!(
            opened.body,
            format!("{canonical_vault} workflowprobe child\n\nup:: [[id:42]]")
        );
    }
    assert_eq!(
        identities,
        ["main".to_owned(), "work".to_owned()].into_iter().collect()
    );
    // No-selector notes and graphs are launch-scoped, not workspace unions.
    let launch = client.notes("").unwrap().notes;
    assert_eq!(launch.len(), 2);
    assert!(launch.iter().any(|note| note.title == "work workflowprobe"));
    assert!(!launch.iter().any(|note| note.title == "main workflowprobe"));
    let (launch_graph, _) = client.full_graph("", "work").unwrap();
    assert!(
        launch_graph
            .nodes
            .iter()
            .any(|node| node.title == "work workflowprobe")
    );
    assert!(
        !launch_graph
            .nodes
            .iter()
            .any(|node| node.title == "main workflowprobe")
    );
    assert!(session.failure().is_none());
    session.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}
