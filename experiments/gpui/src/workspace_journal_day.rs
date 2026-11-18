//! Source-owned journal activity, independent of Home's day and display zone.
use super::*;
use gpui::Div;
use shell::LinkSource;

static NEXT_READ: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1);
#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    source: LinkSource,
    tab: tabs::TabKey,
    date: String,
}
pub(crate) struct JournalDayState {
    identity: Identity,
    serial: u64,
    result: Option<Result<live::AgendaResponse, String>>,
}
impl JournalDayState {
    fn accepts(&self, identity: &Identity, serial: u64) -> bool {
        self.identity == *identity && self.serial == serial
    }
}
fn journal_date(note: &live::Note) -> Option<String> {
    if note.file_kind != "journal" {
        return None;
    }
    let digits = note.note_id.to_string();
    if digits.len() != 8 || !digits.bytes().all(|c| c.is_ascii_digit()) {
        return None;
    }
    let date = format!("{}-{}-{}", &digits[..4], &digits[4..6], &digits[6..]);
    live::calendar_date_valid(&date).then_some(date)
}
fn response_matches(identity: &Identity, response: &live::AgendaResponse) -> bool {
    response.date == identity.date
        && response.source_vault == identity.source.note.vault
        && response.launch_generation == identity.source.launch_generation
}
fn visible_notes<'a>(
    identity: &Identity,
    response: &'a live::AgendaResponse,
) -> Vec<&'a live::Note> {
    response
        .notes
        .iter()
        .filter(|note| note.key() != identity.source.note)
        .collect()
}
impl Workspace {
    fn journal_day_identity(&self, cx: &App) -> Option<Identity> {
        if !self.footnote_reader_available(cx) {
            return None;
        }
        let note = self.current_note.as_ref()?;
        let date = journal_date(&note.note)?;
        let route = self.current_note_request.as_ref()?;
        let tab = self.tabs.active()?.key.clone();
        if !matches!(&tab, tabs::TabKey::Note { vault, note_id } if vault == &route.vault && *note_id == route.note_id)
            || route.note_id != note.note.note_id
            || (!route.vault.is_empty() && route.vault != note.note.vault)
            || note.etag.is_empty()
        {
            return None;
        }
        Some(Identity {
            source: self.current_link_source()?,
            tab,
            date,
        })
    }
    pub(crate) fn sync_journal_day(&mut self, cx: &mut Context<Self>) {
        let Some(identity) = self.journal_day_identity(cx) else {
            self.shell.journal_day = None;
            return;
        };
        if self
            .shell
            .journal_day
            .as_ref()
            .is_some_and(|state| state.identity == identity)
        {
            return;
        }
        self.load_journal_day(identity, cx);
    }
    fn load_journal_day(&mut self, identity: Identity, cx: &mut Context<Self>) {
        if self.journal_day_identity(cx).as_ref() != Some(&identity) {
            return;
        }
        let serial = NEXT_READ.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        self.shell.journal_day = Some(JournalDayState {
            identity: identity.clone(),
            serial,
            result: None,
        });
        let Some(permit) = workspace_activity::AgendaPermit::acquire() else {
            self.shell.journal_day.as_mut().unwrap().result = Some(Err(
                "Earlier daily-view reads are still finishing. Retry shortly.".into(),
            ));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let expected = identity.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != expected.source.storage_generation {
                return Err("Storage changed; journal activity is unavailable.".to_owned());
            }
            let response = client
                .agenda(&expected.date, &expected.source.note.vault)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready"
                || after.generation != before.generation
                || !response_matches(&expected, &response)
            {
                return Err("Journal activity identity changed. Reload or retry.".into());
            }
            Ok(response)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.retire_invalid_read(cx) {
                    cx.notify();
                    return;
                }
                if this.journal_day_identity(cx).as_ref() != Some(&identity) {
                    return;
                }
                if let Some(state) = this
                    .shell
                    .journal_day
                    .as_mut()
                    .filter(|s| s.accepts(&identity, serial))
                {
                    state.result = Some(result);
                    cx.notify();
                }
            });
        })
        .detach();
    }
    pub(crate) fn journal_day_section(&self, cx: &mut Context<Self>) -> Div {
        let mut view = div().mt_6().min_w_0().max_w_full().text_size(px(12.));
        let Some(state) = self
            .shell
            .journal_day
            .as_ref()
            .filter(|state| self.journal_day_identity(cx).as_ref() == Some(&state.identity))
        else {
            return view;
        };
        view = view.child(
            div()
                .mb_3()
                .font_family("monospace")
                .text_size(px(10.))
                .child("ON THIS DAY"),
        );
        let Some(result) = &state.result else {
            return view.child("Loading notes worked on this day…");
        };
        let response = match result {
            Ok(response) => response,
            Err(_) => {
                let identity = state.identity.clone();
                let serial = state.serial;
                return view
                    .child("Notes worked on this day are unavailable.")
                    .child(
                        div()
                            .id("journal-day-retry")
                            .py_1()
                            .cursor_pointer()
                            .child("Retry")
                            .on_click(cx.listener(move |this, _, _, cx| {
                                if this
                                    .shell
                                    .journal_day
                                    .as_ref()
                                    .is_some_and(|s| s.accepts(&identity, serial))
                                {
                                    this.load_journal_day(identity.clone(), cx);
                                    cx.notify();
                                }
                            })),
                    );
            }
        };
        let incomplete = workspace_activity::incomplete(&response.diagnostics);
        if incomplete {
            view = view.child("Incomplete or cached results; missing notes are unknown.");
        }
        let notes = visible_notes(&state.identity, response);
        if notes.is_empty() && !incomplete {
            return view.child("No notes were worked on this day.");
        }
        for (index, note) in notes.iter().take(100).enumerate() {
            let identity = state.identity.clone();
            let serial = state.serial;
            let key = note.key();
            let flags = note
                .flags
                .iter()
                .filter(|flag| !flag.is_empty())
                .cloned()
                .collect::<Vec<_>>();
            view = view.child(
                div()
                    .id(("journal-day-note", index))
                    .min_w_0()
                    .max_w_full()
                    .py_1()
                    .cursor_pointer()
                    .child(note.title.clone())
                    .when(!flags.is_empty(), |row| {
                        row.child(div().text_size(px(10.)).child(flags.join(" · ")))
                    })
                    .on_click(cx.listener(move |this, _, _, cx| {
                        if this.journal_day_identity(cx).as_ref() != Some(&identity) {
                            return;
                        }
                        let current = this
                            .shell
                            .journal_day
                            .as_ref()
                            .filter(|s| s.accepts(&identity, serial))
                            .and_then(|s| s.result.as_ref())
                            .and_then(|r| r.as_ref().ok());
                        if current.is_some_and(|response| {
                            visible_notes(&identity, response)
                                .get(index)
                                .is_some_and(|note| note.key() == key)
                        }) {
                            this.open_live_key(key.clone(), cx);
                        }
                    })),
            );
        }
        if notes.len() > 100 {
            view = view.child(format!(
                "Showing the first 100 of {} notes; no further pages are fetched.",
                notes.len()
            ));
        }
        view
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn note(id: i64, vault: &str, kind: &str) -> live::Note {
        serde_json::from_value(json!({"note_id":id,"vault":vault,"file_kind":kind,"path":"unused","title":"日本語 long title"})).unwrap()
    }
    fn identity() -> Identity {
        Identity {
            source: LinkSource {
                note: live::NoteKey {
                    vault: "journal-vault".into(),
                    note_id: 20261115,
                },
                etag: "v1".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            tab: tabs::TabKey::Note {
                vault: "journal-vault".into(),
                note_id: 20261115,
            },
            date: "2026-11-15".into(),
        }
    }
    fn response() -> live::AgendaResponse {
        let mut result: live::AgendaResponse =
            serde_json::from_value(json!({"date":"2026-11-15","notes":[]})).unwrap();
        result.source_vault = "journal-vault".into();
        result.launch_generation = 3;
        result
    }
    #[test]
    fn only_exact_daily_journals_supply_a_civil_date() {
        assert_eq!(
            journal_date(&note(20240229, "named", "journal")).as_deref(),
            Some("2024-02-29")
        );
        for (id, kind) in [
            (20230229, "journal"),
            (2026, "journal"),
            (202611, "journal"),
            (20261301, "journal"),
            (20261115, "note"),
            (20261115, "Journal"),
            (-20261115, "journal"),
            (202611150, "journal"),
        ] {
            assert!(journal_date(&note(id, "named", kind)).is_none());
        }
        let mut n = note(20261115, "named", "journal");
        n.title = "2020-01-01".into();
        n.additional.insert("created".into(), json!(0));
        assert_eq!(journal_date(&n).as_deref(), Some("2026-11-15"));
    }
    #[test]
    fn response_is_bound_to_journal_vault_date_and_launch_not_home() {
        let source = identity();
        let mut result = response();
        assert!(response_matches(&source, &result));
        result.source_vault = "home-scope".into();
        assert!(!response_matches(&source, &result));
        result.source_vault.clear();
        assert!(!response_matches(&source, &result));
        result.source_vault = source.source.note.vault.clone();
        result.date = "2026-11-14".into();
        assert!(!response_matches(&source, &result));
        result.date = source.date.clone();
        result.launch_generation += 1;
        assert!(!response_matches(&source, &result));
    }
    #[test]
    fn engine_order_and_qualified_self_exclusion_precede_visible_bound() {
        let identity = identity();
        let mut result = response();
        result.notes = vec![
            note(9, "journal-vault", "note"),
            note(20261115, "journal-vault", "journal"),
            note(7, "journal-vault", "note"),
            note(20261115, "other", "journal"),
        ];
        let visible = visible_notes(&identity, &result);
        assert_eq!(
            visible.iter().map(|n| n.note_id).collect::<Vec<_>>(),
            [9, 7, 20261115]
        );
        result.notes = (1..=102)
            .map(|id| note(id, "journal-vault", "note"))
            .collect();
        let visible = visible_notes(&identity, &result);
        assert_eq!(visible.len(), 102); // UI reports the full filtered count
        assert_eq!(visible.iter().take(100).last().unwrap().note_id, 100);
        assert_eq!(visible[0].title, "日本語 long title");
    }
    #[test]
    fn source_changes_and_same_source_retry_reject_old_results_and_controls() {
        let identity = identity();
        let state = JournalDayState {
            identity: identity.clone(),
            serial: 9,
            result: None,
        };
        assert!(state.accepts(&identity, 9));
        assert!(!state.accepts(&identity, 8));
        for field in 0..8 {
            let mut changed = identity.clone();
            match field {
                0 => changed.source.note.vault = "other".into(),
                1 => changed.source.note.note_id += 1,
                2 => changed.source.etag.push('x'),
                3 => changed.source.storage_generation += 1,
                4 => changed.source.launch_generation += 1,
                5 => changed.source.request += 1,
                6 => changed.tab = tabs::TabKey::Home,
                _ => changed.date = "2026-11-14".into(),
            }
            assert!(!state.accepts(&changed, 9));
        }
        let retry = JournalDayState {
            identity: identity.clone(),
            serial: 10,
            result: Some(Err("failed".into())),
        };
        assert!(!retry.accepts(&identity, 9));
        assert!(retry.accepts(&identity, 10));
    }
    #[test]
    fn empty_partial_failure_and_malformed_null_are_not_conflated() {
        let mut result = response();
        assert!(visible_notes(&identity(), &result).is_empty());
        assert!(!workspace_activity::incomplete(&result.diagnostics));
        result
            .diagnostics
            .insert("read_state".into(), json!({"partial":true}));
        assert!(workspace_activity::incomplete(&result.diagnostics));
        for notes in [json!(null), json!({}), json!([{}])] {
            assert!(
                serde_json::from_value::<live::AgendaResponse>(
                    json!({"date":"2026-11-15","notes":notes})
                )
                .is_err()
            );
        }
        let failed = JournalDayState {
            identity: identity(),
            serial: 1,
            result: Some(Err("unavailable".into())),
        };
        assert!(failed.result.unwrap().is_err());
    }
}
