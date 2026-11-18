//! Read-only Home/calendar protocol coverage, not rendered UI validation.
use super::*;

#[test]
#[ignore = "requires an explicitly supplied synthetic Go bridge binary"]
fn bound_home_calendar_reads_preserve_scope_and_canonical_files() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    // Grammar from webui_test.go, store/activity_test.go and
    // handlers_journal_lookup_test.go. Never mutate configuration/authority.
    for vault in ["main", "work"] {
        let base = fixture.0.join("home").join(vault);
        std::fs::write(
            base.join("note/43.md"),
            format!(
                "- [ ] {vault} pending [due:2026-07-18]\n- [x] {vault} finished [due:2026-07-18]\n"
            ),
        )
        .unwrap();
        std::fs::write(
            base.join(".track/notes/43.yaml"),
            format!("version: 1\ntitle: {vault} calendar note\ndays:\n  - 2026-07-18\n"),
        )
        .unwrap();
        std::fs::create_dir_all(base.join("journal")).unwrap();
        std::fs::write(
            base.join("journal/901.md"),
            format!("{vault} journal body\n"),
        )
        .unwrap();
        std::fs::write(
            base.join(".track/notes/901.yaml"),
            "version: 1\ntitle: '20260718'\n",
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
    for vault in ["main", "work"] {
        let agenda = client.agenda("2026-07-18", vault).unwrap();
        assert_eq!(agenda.notes.len(), 1);
        assert_eq!(agenda.notes[0].note_id, 43);
        assert_eq!(agenda.notes[0].vault, vault);
        assert_eq!(agenda.notes[0].title, format!("{vault} calendar note"));
        let dated = client.dated_tasks(vault).unwrap();
        assert_eq!(dated.tasks.len(), 2);
        assert!(dated.tasks.iter().all(|task| task.note_id == 43
            && task.due == "2026-07-18"
            && task.text.starts_with(vault)));
        let open = client.open_tasks(vault, "work").unwrap();
        assert_eq!(open.tasks.len(), 1);
        assert!(!open.tasks[0].done);
        assert!(open.tasks[0].text.contains("pending"));
        let content = client
            .calendar_content(vault, "work", "2026-07-18", "2026-07-18", "UTC")
            .unwrap();
        assert_eq!(content.days.len(), 1);
        assert_eq!(content.days[0].note_count, 1);
        assert_eq!(
            content.days[0].notes[0].title,
            format!("{vault} calendar note")
        );
        assert_eq!(content.days[0].task_count, 2);
        let day = client
            .calendar_agenda_day(vault, "work", "2026-07-18", "UTC")
            .unwrap();
        assert!(day.events.is_empty());
        assert_eq!(day.date, "2026-07-18");
        let range = client
            .calendar_range(vault, "work", "2026-07-18", "2026-07-19", "UTC")
            .unwrap();
        assert_eq!(range.days.len(), 2);
        assert!(
            range
                .days
                .iter()
                .all(|day| day.count == 0 && day.previews.is_empty())
        );
        let journal = client.journal_lookup("20260718", vault, "work").unwrap();
        assert!(journal.found);
        assert_eq!(journal.source_vault, vault);
        assert_eq!(journal.vault, if vault == "work" { "" } else { vault });
        let reference = journal.note.unwrap();
        assert_eq!(reference.note_id, 901);
        assert_eq!(reference.file_kind, "journal");
        let opened = client
            .note(&NoteKey {
                vault: vault.into(),
                note_id: reference.note_id,
            })
            .unwrap();
        assert_eq!(opened.note.body, format!("{vault} journal body"));
        assert!(
            !client
                .journal_lookup("20260719", vault, "work")
                .unwrap()
                .found
        );
        assert!(!fixture.0.join("home").join(vault).join("calendar").exists());
    }
    assert!(session.events().unavailable);
    session.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}

#[test]
#[ignore = "requires an explicitly supplied synthetic Go bridge binary"]
fn bound_populated_calendar_preserves_scope_and_dst_projection() {
    assert_eq!(
        std::env::var("TRACK_NATIVE_TEST_SYNTHETIC_ONLY").as_deref(),
        Ok("1")
    );
    let binary = PathBuf::from(
        std::env::var_os("TRACK_NATIVE_BOUND_TEST_BINARY").expect("explicit test bridge"),
    );
    assert!(binary.is_absolute());
    let (fixture, proof) = admission_process_tests::fixture(&binary);
    // Canonical record shape: calendarevent/{event,store}.go. Fingerprints are
    // SHA256 of Go's declaration-ordered WriteRequest JSON (including empty
    // fields, expected_revision=0 and operation_id). These are fixed fixtures,
    // not an alternative writer: the real Go read checks Normalize equality,
    // identity and the request fingerprint before serving any event.
    let hashes = [
        [
            "d67102b4e174050c65871d7ea31bc5d0a8a0d4a2c83059bb8202be0aed327114",
            "22161042f5950767b7cea450004b38da5160543ad7d5da325bdf60c1d8173e56",
        ],
        [
            "31266df6a8002fd9ef29fd620879d14f69c02e433a203e37f1aff5d1d8dddb35",
            "ae67cd4bb77f9a747807de111bb792fc5bc492e32b49af9aeda6ac7c037bf022",
        ],
    ];
    for (v, vault) in ["main", "work"].iter().enumerate() {
        let events = fixture.0.join("home").join(vault).join("calendar/events");
        std::fs::create_dir_all(&events).unwrap();
        for index in 0..2 {
            let all_day = index == 0;
            let id = format!("20000000-0000-4000-8000-{:012}", index + 1);
            let record = serde_json::json!({
                "version":1,"id":id,"revision":1,
                "title":format!("{vault} {}", if all_day {"all-day"} else {"fold"}),
                "all_day":all_day,
                "start":if all_day {"2026-10-31"} else {"2026-11-01T01:30:00-04:00"},
                "end":if all_day {"2026-11-02"} else {"2026-11-01T02:30:00-05:00"},
                "time_zone":if all_day {""} else {"America/New_York"},
                "description":"","location":"","url":"","note_id":"42",
                "status":"confirmed","deleted":false,"updated_at":"2026-10-01T00:00:00Z",
                "last_operation_id":format!("30000000-0000-4000-8000-{:012}", index + 1),
                "last_request_hash":hashes[v][index]
            });
            std::fs::write(
                events.join(format!("{id}.json")),
                serde_json::to_vec(&record).unwrap(),
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
    for (scope, vault, body) in [
        ("main", "main", "Active sentinel body"),
        ("work", "work", "Foreign sentinel body"),
        ("", "work", "Foreign sentinel body"),
    ] {
        let events = client.calendar_events(scope).unwrap();
        assert_eq!(events.events.len(), 2);
        for event in &events.events {
            assert_eq!(event.version, 1);
            assert_eq!(event.revision, 1);
            assert_eq!(event.note_id, "42");
            assert_eq!(event.status, "confirmed");
            assert!(!event.deleted);
            assert!(event.title.starts_with(&format!("{vault} ")));
            assert_eq!(
                client
                    .note(&NoteKey {
                        vault: scope.into(),
                        note_id: event.note_id.parse().unwrap()
                    })
                    .unwrap()
                    .note
                    .body,
                body
            );
        }
        let fold = events.events.iter().find(|event| !event.all_day).unwrap();
        assert_eq!(fold.start, "2026-11-01T01:30:00-04:00");
        assert_eq!(fold.end, "2026-11-01T02:30:00-05:00");
        assert_eq!(fold.time_zone, "America/New_York");
        let day = client
            .calendar_agenda_day(scope, "work", "2026-11-01", "America/New_York")
            .unwrap();
        assert_eq!(day.source_vault, scope);
        assert_eq!(day.time_zone, "America/New_York");
        assert_eq!(day.day_start, "2026-11-01T04:00:00Z");
        assert_eq!(day.day_end, "2026-11-02T05:00:00Z");
        assert_eq!(
            day.day_end_unix.unwrap() - day.day_start_unix.unwrap(),
            25 * 3600
        );
        assert_eq!(day.events.len(), 2);
        assert!(
            day.events
                .iter()
                .all(|event| event.title.starts_with(&format!("{vault} ")))
        );
        let utc = client
            .calendar_agenda_day(scope, "work", "2026-11-01", "UTC")
            .unwrap();
        assert_eq!(utc.day_start, "2026-11-01T00:00:00Z");
        assert_eq!(utc.day_end, "2026-11-02T00:00:00Z");
        assert_eq!(utc.events.len(), 2);
        let range = client
            .calendar_range(
                scope,
                "work",
                "2026-10-31",
                "2026-11-02",
                "America/New_York",
            )
            .unwrap();
        assert_eq!(range.source_vault, scope);
        assert_eq!(
            range.days.iter().map(|day| day.count).collect::<Vec<_>>(),
            vec![1, 2, 0]
        );
        for day in &range.days {
            assert_eq!(day.previews.len() as u64, day.count);
            for preview in &day.previews {
                let event = events
                    .events
                    .iter()
                    .find(|event| event.id == preview.id)
                    .unwrap();
                assert_eq!(
                    (
                        preview.revision,
                        preview.title.as_str(),
                        preview.status.as_str()
                    ),
                    (event.revision, event.title.as_str(), event.status.as_str())
                );
            }
        }
        assert!(
            client
                .calendar_agenda_day(scope, "work", "2026-11-02", "America/New_York")
                .unwrap()
                .events
                .is_empty()
        );
    }
    session.shutdown().unwrap();
    assert!(!client.is_alive());
    assert_eq!(
        before,
        admission_process_tests::protected_inventory(&fixture.0)
    );
}
