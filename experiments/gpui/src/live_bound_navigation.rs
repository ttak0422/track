//! Link-target resolution and relationship data roundtrips, not GUI activation.
use super::*;

#[test]
#[ignore = "requires an explicitly supplied synthetic Go bridge binary"]
fn bound_navigation_resolves_targets_without_cross_vault_fallback() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    // id-v1 spelling follows link_resolution_test.go. Resolution receives the
    // parsed target, never wiki brackets, Markdown labels or anchor suffixes.
    for (vault, other) in [("main", "work"), ("work", "main")] {
        let base = fixture.0.join("home").join(vault);
        std::fs::write(base.join("note/43.md"), format!("{vault} shared body\n")).unwrap();
        std::fs::write(
            base.join(".track/notes/43.yaml"),
            "version: 1\ntitle: Shared target\n",
        )
        .unwrap();
        std::fs::write(
            base.join("note/44.md"),
            format!("{vault} child\n\nup:: [[id:43]]\n\n[[{other}:id:43]]\n"),
        )
        .unwrap();
        std::fs::write(
            base.join(".track/notes/44.yaml"),
            format!("version: 1\ntitle: {vault} child\n"),
        )
        .unwrap();
        if vault == "main" {
            std::fs::write(base.join("note/45.md"), "main exclusive body\n").unwrap();
            std::fs::write(
                base.join(".track/notes/45.yaml"),
                "version: 1\ntitle: Main exclusive\n",
            )
            .unwrap();
        }
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
    let client = session.client();
    for (source, other) in [("main", "work"), ("work", "main")] {
        for (term, target) in [
            ("id:43".to_owned(), source),
            ("Shared target".to_owned(), source),
            (format!("{other}:id:43"), other),
            (format!("{other}:Shared target"), other),
        ] {
            let resolved = client.resolve(&term, source).unwrap();
            assert_eq!(resolved.source_vault, source);
            assert_eq!(resolved.term, term);
            assert!(resolved.found);
            assert_eq!(resolved.note.title, "Shared target");
            let key = resolved.key().unwrap();
            assert_eq!(key.note_id, 43);
            assert_eq!(key.vault, if target == "work" { "" } else { target });
            let opened = client.note(&key).unwrap();
            assert_eq!(opened.note.body, format!("{target} shared body"));
            for field in ["backlinks", "children"] {
                let refs = opened
                    .related_and_diagnostics
                    .get(field)
                    .unwrap()
                    .as_array()
                    .unwrap();
                assert_eq!(refs.len(), 1, "{field}: {refs:?}");
                assert_eq!(refs[0]["note_id"], 44);
                assert_eq!(refs[0]["title"], format!("{target} child"));
                assert_eq!(
                    refs[0].get("vault").and_then(Value::as_str).unwrap_or(""),
                    if target == "work" { "" } else { target }
                );
            }
        }
        for term in ["id:99999", "Missing target", "missing-vault:id:43"] {
            let result = client.resolve(term, source);
            match result {
                Ok(resolved) => {
                    assert!(!resolved.found);
                    assert!(resolved.key().is_none());
                }
                Err(Error::Http { .. }) if term.starts_with("missing-vault:") => {}
                other => panic!("unexpected missing-target result: {other:?}"),
            }
        }
    }
    for source in ["work", ""] {
        for term in ["id:45", "Main exclusive"] {
            let missing = client.resolve(term, source).unwrap();
            assert!(!missing.found);
            assert!(missing.key().is_none());
        }
    }
    session.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}
