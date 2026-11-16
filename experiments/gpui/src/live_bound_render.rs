//! Real-engine render/preview contracts; no renderer or UI-equivalence claim.
use super::*;

#[test]
#[ignore = "requires an explicitly supplied synthetic Go bridge binary"]
fn bound_render_keeps_include_sources_and_decodes_inline_chart() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    // Stable-ID block include syntax follows link_resolution_test.go. Query and
    // metric chart examples follow read_routes_sentinel_test.go; metric version
    // is explicit because native preview admission requires versioned records.
    for vault in ["main", "work"] {
        let base = fixture.0.join("home").join(vault);
        std::fs::write(
            base.join("note/43.md"),
            format!("# Section\n\n{vault} excerpt ^block\n"),
        )
        .unwrap();
        std::fs::write(
            base.join(".track/notes/43.yaml"),
            format!("version: 1\ntitle: {vault} render source\n"),
        )
        .unwrap();
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
    let mut preview_origins = Vec::new();
    for (vault, other) in [("main", "work"), ("work", "main")] {
        let body = format!(
            "![[id:43#^block|Local]]\n![[{other}:id:43#^block|Remote]]\n\n```track-query\nTABLE title\n```\n"
        );
        let rendered = client.included_notes(vault, &body).unwrap();
        assert_eq!(rendered.entries.len(), 2);
        for (entry, expected, caption) in [
            (&rendered.entries[0], vault, "Local"),
            (&rendered.entries[1], other, "Remote"),
        ] {
            assert_eq!(entry.note_id, Some(43));
            assert_eq!(entry.kind.as_deref(), Some("note"));
            assert_eq!(entry.caption, caption);
            assert_eq!(
                entry.title.as_deref(),
                Some(
                    if caption == "Local" {
                        "id:43".to_owned()
                    } else {
                        format!("{other}:id:43")
                    }
                    .as_str()
                )
            );
            assert_eq!(entry.excerpt, format!("{expected} excerpt"));
            let expected_wire = if caption == "Remote" && expected == "work" {
                ""
            } else {
                expected
            };
            assert_eq!(entry.vault, expected_wire);
            assert_eq!(entry.explicit_origin, caption == "Remote");
            assert_eq!(entry.asset_origin(vault), Some(expected_wire));
            preview_origins.push(entry.asset_origin(vault).unwrap().to_owned());
            assert!(entry.etag.as_ref().is_some_and(|etag| !etag.is_empty()));
            assert!(entry.error.is_none());
        }
        assert!(!rendered.markdown.contains("```track-query"));
        assert!(rendered.markdown.contains("| title |\n| --- |"));
        assert!(rendered.markdown.contains("| [[id:43]] |"));
        // ID-format query tables intentionally contain ID links, not titles.
        let linked = client.resolve("id:43", vault).unwrap();
        assert!(linked.found);
        assert_eq!(linked.note.title, format!("{vault} render source"));
    }
    for vault in preview_origins {
        // Pure inline records carry no file-backed authority. The real Go
        // ECharts option still must pass the native bounded chart decoder.
        let spec = r#"{"version":2,"mark":"bar","title":"Demo","data":{"kind":"metric","records":[{"version":1,"name":"A","time":"t1","value":3}]},"encoding":{"x":{"field":"name","type":"nominal"},"y":[{"field":"value"}]}}"#;
        for (mark, kind) in [
            ("bar", crate::numeric_chart::Kind::Bar),
            ("line", crate::numeric_chart::Kind::Line),
        ] {
            let spec = spec.replace("\"mark\":\"bar\"", &format!("\"mark\":\"{mark}\""));
            let chart = client.inline_viewspec(&vault, &spec).unwrap();
            assert_eq!(chart.title.as_deref(), Some("Demo"));
            assert_eq!(chart.kind, kind);
            assert_eq!(chart.categories, vec!["A"]);
            assert_eq!(chart.series.len(), 1);
            assert_eq!(chart.series[0].values, vec![Some(3.0)]);
        }
    }
    assert!(session.events().unavailable);
    session.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}
