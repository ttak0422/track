//! Scoped graph wire compatibility against the real native HTTP decoder.
use super::tests::mock;
use super::*;
use serde_json::json;

fn wire(center: i64) -> Value {
    json!({"vault":"work", "graph":{"center_id":center,"nodes":[{"note_id":42,"title":"Scoped","vault":"work"}],"edges":[]},"read_state":{"partial":true}})
}
fn full(value: Value, scope: &str, launch: &str) -> Result<(crate::graph::Graph, Fields), Error> {
    let (client, request) = mock(200, value.to_string());
    let result = client.full_graph(scope, launch);
    request.join().unwrap();
    result
}

#[test]
fn active_alias_and_legacy_missing_graph_metadata_normalize_without_losing_diagnostics() {
    for (scope, launch, label, expected) in [
        ("work", "work", "", "work"),
        ("", "work", "work", "work"),
        ("", "work", "", "work"),
        ("", "", "", ""),
        ("main", "work", "main", "main"),
    ] {
        let mut value = wire(0);
        value["vault"] = label.into();
        value["graph"]["nodes"][0]["vault"] = label.into();
        let (graph, fields) = full(value, scope, launch).unwrap();
        assert_eq!(graph.nodes[0].vault.as_deref(), Some(expected));
        assert_eq!(fields["vault"], expected);
        assert_eq!(fields["read_state"]["partial"], true);
    }
    let mut value = wire(0);
    value.as_object_mut().unwrap().remove("vault");
    value["graph"]["nodes"][0]
        .as_object_mut()
        .unwrap()
        .remove("vault");
    let (graph, fields) = full(value, "main", "work").unwrap();
    assert_eq!(graph.nodes[0].vault.as_deref(), Some("main"));
    assert_eq!(fields["vault"], "main");
}

#[test]
fn foreign_graph_aliases_and_mixed_node_namespaces_are_rejected() {
    for (scope, launch, top, node) in [
        ("main", "work", "", "main"),
        ("main", "work", "main", ""),
        ("work", "work", "other", "work"),
        ("work", "work", "work", "other"),
        ("", "work", "main", "main"),
        ("named", "", "", "named"),
    ] {
        let mut value = wire(0);
        value["vault"] = top.into();
        value["graph"]["nodes"][0]["vault"] = node.into();
        assert!(matches!(full(value, scope, launch), Err(Error::Shape(_))));
    }
    let mut value = wire(0);
    value["graph"]["nodes"]
        .as_array_mut()
        .unwrap()
        .push(json!({"note_id":43,"title":"Foreign","vault":"other"}));
    assert!(matches!(full(value, "work", "work"), Err(Error::Shape(_))));
}

#[test]
fn explicit_malformed_graph_metadata_is_not_legacy_absence() {
    for bad in [Value::Null, json!(7), json!(true), json!([]), json!({})] {
        for node in [false, true] {
            let mut value = wire(0);
            if node {
                value["graph"]["nodes"][0]["vault"] = bad.clone();
            } else {
                value["vault"] = bad.clone();
            }
            assert!(matches!(full(value, "work", "work"), Err(Error::Shape(_))));
        }
    }
}

#[test]
fn graph_center_checks_survive_scope_normalization() {
    assert!(matches!(
        full(wire(42), "work", "work"),
        Err(Error::Shape(_))
    ));
    let key = NoteKey {
        vault: "work".into(),
        note_id: 42,
    };
    for center in [0, 43] {
        let (client, request) = mock(200, wire(center).to_string());
        assert!(matches!(
            client.local_graph(&key, "work"),
            Err(Error::Shape(_))
        ));
        request.join().unwrap();
    }
    let mut value = wire(42);
    value["vault"] = "".into();
    value["graph"]["nodes"][0]["vault"] = "".into();
    let (client, request) = mock(200, value.to_string());
    let (graph, fields) = client.local_graph(&key, "work").unwrap();
    assert_eq!(graph.center_id, 42);
    assert_eq!(graph.nodes[0].vault.as_deref(), Some("work"));
    assert_eq!(fields["vault"], "work");
    assert!(
        request
            .join()
            .unwrap()
            .contains("/api/graph/local?vault=work&id=42")
    );
}

#[test]
fn invalid_graph_inputs_fail_before_http_and_leave_valid_request_available() {
    let (client, request) = mock(200, wire(0).to_string());
    for bad in [
        "x".repeat(1025),
        "bad\0scope".into(),
        "bad\nscope".into(),
        "bad\u{7f}scope".into(),
    ] {
        for (scope, launch) in [(bad.as_str(), "work"), ("work", bad.as_str())] {
            assert!(matches!(
                client.full_graph(scope, launch),
                Err(Error::Config(_))
            ));
            assert!(matches!(
                client.local_graph(
                    &NoteKey {
                        vault: scope.into(),
                        note_id: 42
                    },
                    launch
                ),
                Err(Error::Config(_))
            ));
        }
    }
    assert!(client.full_graph("work", "work").is_ok());
    assert!(request.join().unwrap().contains("/api/graph?vault=work"));
}
