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
