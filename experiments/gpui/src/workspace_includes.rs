//! Explicit, inert engine-resolved snapshots bound to the accepted host source.
use super::*;
use crate::shell::LinkSource;
use gpui::Div;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
static ACTIVE: AtomicBool = AtomicBool::new(false);
static SERIAL: AtomicU64 = AtomicU64::new(1);
struct Permit;
impl Permit {
    fn acquire() -> Option<Self> {
        ACTIVE
            .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
            .ok()
            .map(|_| Self)
    }
}
impl Drop for Permit {
    fn drop(&mut self) {
        ACTIVE.store(false, Ordering::Release);
    }
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Identity {
    source: LinkSource,
    route: live::NoteKey,
}
pub(crate) struct IncludesState {
    identity: Identity,
    open: bool,
    serial: u64,
    result: Option<Result<Vec<crate::included_notes::Include>, String>>,
}
impl IncludesState {
    fn new(identity: Identity) -> Self {
        Self {
            identity,
            open: false,
            serial: 0,
            result: None,
        }
    }
    fn accepts(&self, identity: &Identity, serial: u64) -> bool {
        self.open && &self.identity == identity && self.serial == serial
    }
}
fn source_position(line: i64) -> String {
    if line < 0 {
        "noncontiguous".into()
    } else {
        format!("line {}", line as u64 + 1)
    }
}
impl Workspace {
    fn includes_identity(&self) -> Option<Identity> {
        if self.full_graph_active()
            || self.shell.home
            || self.editing_ui.active()
            || self.editing_ui.has_pending()
            || self.quit_session.is_closing()
            || self.quit_session.failure().is_some()
            || self.read_error.is_some()
        {
            return None;
        }
        let source = self.current_link_source()?;
        let route = self.current_note_request.as_ref()?;
        if route.note_id != source.note.note_id
            || (!route.vault.is_empty() && route.vault != source.note.vault)
            || !matches!(self.tabs.active().map(|t| &t.key),Some(tabs::TabKey::Note {vault,note_id}) if vault == &route.vault && *note_id == route.note_id)
        {
            return None;
        }
        Some(Identity {
            source,
            route: route.clone(),
        })
    }
    pub(crate) fn reset_stale_includes(&mut self) {
        if self
            .shell
            .includes
            .as_ref()
            .is_some_and(|s| self.includes_identity().as_ref() != Some(&s.identity))
        {
            self.shell.includes = None;
        }
    }
    fn includes_fetch(&mut self, identity: Identity, cx: &mut Context<Self>) {
        if self.includes_identity().as_ref() != Some(&identity)
            || !self.pin_keyboard_available(cx)
            || !self.find_available()
        {
            return;
        }
        let Some(state) = self
            .shell
            .includes
            .as_mut()
            .filter(|s| s.identity == identity && s.open)
        else {
            return;
        };
        let serial = SERIAL.fetch_add(1, Ordering::Relaxed);
        state.serial = serial;
        state.result = None;
        let Some(permit) = Permit::acquire() else {
            state.result = Some(Err(
                "An earlier include request is still finishing. Refresh shortly.".into(),
            ));
            cx.notify();
            return;
        };
        let client = self.live.as_ref().unwrap().client();
        let body = self.current_note.as_ref().unwrap().body.clone();
        let captured = identity.clone();
        let accepted_body = body.clone();
        let job = cx.background_executor().spawn(async move {
            let _permit = permit;
            let before = client.storage().map_err(|e| e.to_string())?;
            if before.phase != "ready" || before.generation != captured.source.storage_generation {
                return Err("Storage changed. Reload the workspace.".into());
            }
            let response = client
                .included_notes(&captured.source.note.vault, &body)
                .map_err(|e| e.to_string())?;
            let after = client.storage().map_err(|e| e.to_string())?;
            if after.phase != "ready" || after.generation != captured.source.storage_generation {
                return Err("Storage changed. Reload the workspace.".into());
            }
            Ok(response)
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |this, cx| {
                if this.includes_identity().as_ref() != Some(&identity)
                    || this
                        .current_note
                        .as_ref()
                        .is_none_or(|n| n.body != accepted_body)
                {
                    return;
                }
                if let Some(state) = this
                    .shell
                    .includes
                    .as_mut()
                    .filter(|s| s.accepts(&identity, serial))
                {
                    state.result = Some(result);
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }
    pub(crate) fn includes_inspector(&mut self, cx: &mut Context<Self>) -> Div {
        let mut view = div().mb_3().text_size(px(12.));
        let Some(identity) = self.includes_identity() else {
            self.shell.includes = None;
            return view;
        };
        if self
            .shell
            .includes
            .as_ref()
            .is_none_or(|s| s.identity != identity)
        {
            self.shell.includes = Some(IncludesState::new(identity.clone()));
        }
        let open = self.shell.includes.as_ref().unwrap().open;
        let captured = identity.clone();
        view = view.child(
            div()
                .id("includes-toggle")
                .cursor_pointer()
                .child(if open {
                    "▾ Included notes (read-only)"
                } else {
                    "▸ Included notes (read-only)"
                })
                .on_click(cx.listener(move |this, _, _, cx| {
                    if this.includes_identity().as_ref() != Some(&captured)
                        || !this.pin_keyboard_available(cx)
                        || !this.find_available()
                    {
                        return;
                    }
                    let Some(state) = this
                        .shell
                        .includes
                        .as_mut()
                        .filter(|s| s.identity == captured)
                    else {
                        return;
                    };
                    state.open = !state.open;
                    state.serial = SERIAL.fetch_add(1, Ordering::Relaxed);
                    state.result = None;
                    if state.open {
                        this.includes_fetch(captured.clone(), cx);
                    }
                    cx.notify();
                })),
        );
        if !open {
            return view;
        }
        let captured = identity.clone();
        view=view.child("Engine-resolved snapshots; refresh to request updated excerpts. Completeness and currentness are not guaranteed. Host Find does not search excerpts.")
            .child(div().id("includes-refresh").cursor_pointer().child("Refresh included notes").on_click(cx.listener(move |this,_,_,cx|this.includes_fetch(captured.clone(),cx))));
        let Some(result) = &self.shell.includes.as_ref().unwrap().result else {
            return view.child("Loading included notes…");
        };
        let entries = match result {
            Ok(entries) => entries,
            Err(error) => return view.child(format!("Included notes unavailable: {error}")),
        };
        if entries.is_empty() {
            return view.child("No included notes in this response.");
        }
        let mut rows = div()
            .id("includes-scroll")
            .max_h(px(320.))
            .overflow_y_scroll();
        for entry in entries {
            let origin = if entry.vault.is_empty() {
                "launch vault"
            } else {
                &entry.vault
            };
            let target = match entry.note_id {
                Some(id) => format!("{} #{id}", entry.kind.as_deref().unwrap_or("target")),
                None => "unknown target".into(),
            };
            let mut row = div()
                .mb_3()
                .child(format!(
                    "{} · {}",
                    entry.caption,
                    entry.title.as_deref().unwrap_or("Untitled include")
                ))
                .child(format!(
                    "{origin} · {target} · rendered host line {} · source {}",
                    entry.line + 1,
                    source_position(entry.source_line)
                ));
            if let Some(etag) = &entry.etag {
                row = row.child(format!("Snapshot revision: {etag}"));
            }
            if let Some(error) = &entry.error {
                if !error.is_empty() {
                    row = row.child(format!("Include error: {error}"));
                }
            }
            for option in &entry.bad_options {
                row = row.child(format!("Invalid option: {option}"));
            }
            rows = rows.child(row.child(div().w_full().child(entry.excerpt.clone())));
        }
        view.child(rows)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn identity() -> Identity {
        Identity {
            source: LinkSource {
                note: live::NoteKey {
                    vault: "work".into(),
                    note_id: 1,
                },
                etag: "rev".into(),
                storage_generation: 2,
                launch_generation: 3,
                request: 4,
            },
            route: live::NoteKey {
                vault: "work".into(),
                note_id: 1,
            },
        }
    }
    #[test]
    fn includes_stale_source_route_and_serial_are_rejected() {
        let id = identity();
        let mut s = IncludesState::new(id.clone());
        s.serial = 10;
        assert!(!s.accepts(&id, 10));
        s.open = true;
        assert!(s.accepts(&id, 10));
        s.open = false;
        assert!(!s.accepts(&id, 10));
        s.open = true;
        s.serial = 11;
        assert!(!s.accepts(&id, 10));
        for dimension in 0..7 {
            let mut changed = id.clone();
            match dimension {
                0 => changed.source.request += 1,
                1 => changed.source.launch_generation += 1,
                2 => changed.source.storage_generation += 1,
                3 => changed.source.etag.push('x'),
                4 => changed.source.note.note_id += 1,
                5 => changed.source.note.vault.push('x'),
                _ => changed.route.vault.clear(),
            };
            assert!(!s.accepts(&changed, 11));
        }
    }
    #[test]
    fn includes_source_position_never_overflows_or_offers_a_jump() {
        assert_eq!(source_position(-1), "noncontiguous");
        assert_eq!(source_position(0), "line 1");
        assert_eq!(source_position(i64::MAX), "line 9223372036854775808");
    }
    #[test]
    fn includes_permit_is_bounded_and_released() {
        let p = Permit::acquire().unwrap();
        assert!(Permit::acquire().is_none());
        drop(p);
        assert!(Permit::acquire().is_some());
    }
}
