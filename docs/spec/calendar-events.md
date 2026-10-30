# Single calendar events

Calendar events are independent records, not dated task lines or note activity. Existing notes,
task states, journals, and their indexes are not converted. This first version handles one occurrence
per record. It has no recurrence, calendar synchronization, invitations, notification delivery, or
automatic actions.

## Canonical storage

Each event is a version-1 JSON file at `calendar/events/<UUID>.json` inside its vault. Include this
directory in vault backups and synchronization. No SQLite schema change or migration is required;
the note index can be rebuilt without losing events. Event reads do not create directories. The
existing static-site exporter does not publish these records, including with `--calendar`.

The public record carries `version`, `id`, `revision`, `title`, `all_day`, `start`, `end`, `time_zone`,
`description`, `location`, `url`, `note_id`, `status`, `deleted`, and `updated_at`. IDs are canonical
lowercase UUID strings supplied by clients before their first save. An optional `note_id` is a
positive numeric local note ID encoded as a string; the link can become dangling without changing
event identity. No note is created or edited as a consequence of saving an event.

`status` is `confirmed` or `cancelled`. Cancellation preserves a visible event. Soft deletion sets
`deleted` while retaining every field; ordinary lists omit it and trash lists include it. Deletion
and restoration must preserve all other fields. Restore first, then edit. There is no purge operation.

Writes take a cooperating-process exclusive file lock and compare the expected revision while
holding it. A successful change increments the revision, writes and syncs a temporary file, renames
it atomically, and syncs the directory. Readers see one complete revision. The engine rejects
symlinks in the event directories, record files, and lock file; filesystem access stays anchored to
the selected vault. Malformed, unsupported-version, or noncanonical records cause an explicit error,
including in lists, rather than disappearing as an empty calendar. A corrupt record is never overwritten.
Record opens are nonblocking before checking the opened descriptor's regular-file type, so a
named pipe cannot stall readers or a save while holding its exclusive lock.

Accepted zone identifiers come from the pinned Go toolchain's bundled IANA data, including
legacy aliases, with the `Factory` placeholder excluded. Regenerate `zones.txt` with
`go generate ./internal/track/calendarevent` when upgrading Go. Host-only names (`Local`,
`localtime`, `posixrules`, `posix/…`, `right/…`) are not event identities. The browser also checks
its own `Intl.DateTimeFormat` support when authoring time changes; older browsers may lack newer
IANA names. Unsupported stored zones show their original RFC3339 timestamps and a warning;
unrelated edits preserve those instants rather than silently converting them to another zone.

The lock serializes cooperating processes on one filesystem. It does not provide a distributed lock
between machines independently writing copies of a synchronized vault; concurrent sync conflicts
require user reconciliation. Keep the lock file in place while a process may hold it.

## Dates and time zones

All-day `start` and `end` are civil `YYYY-MM-DD` dates with exclusive end and no `time_zone`. At least
one day is required. They must not be converted through UTC midnight. Timed events require start
before end and an explicit IANA time zone. The Go bundle embeds standard-library time-zone data.

Timed writes accept a local `YYYY-MM-DDTHH:mm` (or seconds) interpreted in that zone, or RFC3339 with
an explicit offset matching the zone at that instant. Nonexistent local times during a forward clock
change are rejected. Repeated local times during a backward change are rejected as ambiguous unless
the caller includes the intended offset. Stored timestamps are normalized RFC3339 values in the
event's zone. The zone identifier remains alongside them. Historical offsets containing seconds
cannot be represented by RFC3339 and are rejected. A missing or unknown time must not be turned into
a fabricated midnight event.

## Revision and retry protocol

A write sends the complete editable fields plus `expected_revision` and `operation_id`. Revision 0
creates the supplied event ID; positive values must exactly match the saved record. Each request has
a UUID operation ID scoped to the event. The file also stores its last operation ID and a SHA-256
fingerprint of the normalized request, including expected revision. Those internal fields are not
returned through the API.

An unchanged retry using the same operation ID returns the same revision, including after an
uncertain network response. Reusing that ID with different input conflicts. Once another operation
has changed the event, an old request is stale and conflicts. Clients retain their inputs on errors,
refuse duplicate in-flight submits, and reload before retrying a genuine revision conflict. A failed
directory sync can report an error after the rename; retrying that same operation resolves whether
the record was already saved without creating another event.

Title is required and at most 500 UTF-8 bytes; description is at most 32 KiB, location 2 KiB, URL
4 KiB, and time-zone identifier 128 bytes. Control characters are rejected except description line
breaks and tabs. URLs are HTTP(S) without embedded credentials. The request limit is 64 KiB and
stored records are bounded to 128 KiB. Unknown request fields are rejected.

## HTTP API

The existing workspace Host/Origin and vault-selection guards apply to every route.

## Editor draft protection

The event editor uses the workspace router blocker for navigation to a different pathname.
Changed live form controls, input-method composition, pending writes, and failed writes need
discard confirmation. Cancelled navigation retains the mounted form. Accepted navigation closes
it even when a day-route parameter change reuses the panel; late write results cannot close a
new editor. Same-path navigation keeps its form. Successful saving and intentional editor
Cancel/Escape release the guard. In-flight saves cannot be cancelled through the editor buttons.

A separate beforeunload listener reads the same current state. This keeps reload/tab-close
protection after a cancelled history Back, for which the history library suppresses its own next
unload check. It never opens an extra JavaScript confirm for beforeunload. Native page replacement,
window Close and Quit retain the conservative open-dialog confirmation described in the desktop
termination contract. No browser draft is persisted by this guard.

| Request | Result |
| --- | --- |
| `GET /api/calendar-events` | `{vault, events}` excluding trash; `?deleted=1` includes deleted records. |
| `GET /api/calendar-event?id=UUID` | `{vault, event}`, including a deleted record when explicitly addressed. |
| `PUT /api/calendar-event?id=UUID` | Complete write request; returns `{vault, event}`. |

Responses use 400 for validation errors, 404 for missing events, 409 for revision or operation
conflicts, and 500 for filesystem or corruption failures. Unknown vaults do not fall back to the
launch vault. Successful writes broadcast the existing workspace change notification without
reindexing notes. CLI/external changes can be picked up by event-list refetching.

## CLI

`track event list [--deleted]` and `track event get --id UUID` print JSON. To write, send only editable
fields as one JSON object on stdin:

```sh
printf '%s\n' '{"title":"Review","all_day":true,"start":"2026-10-29","end":"2026-10-30"}' \
  | track event save --id 12bd0310-5f3f-49f6-a277-c2c8f5a12618 --expect 0 \
      --operation c6a7a51c-dcb8-4097-b8c3-3b19ebd6e5dd
```

Keep the event and operation IDs stable for an unchanged retry. A later edit supplies the returned
revision and a new operation ID. Use the same save command with the full saved fields and
`"deleted": true` to move to trash; set it back to false with the latest revision to restore. Global
`--vault NAME` selects a registered vault exactly as it does for notes.

## Day/week time grid

Live day/week event panels can switch between their existing agenda and a time grid. The panel
keeps its editor mounted across layout, period and content-filter changes. Month and static
exports retain their existing behavior; no storage, API or native navigation contract changes.

All-day ranges retain civil dates and exclusive ends. Timed intervals intersect display-zone
midnight boundaries, constructed independently so a normalized 01:00 start on a midnight-gap
day cannot extend the preceding day into the next date. Each column uses elapsed minutes so DST gaps/folds do not lose or reverse
intervals. Transition-day ticks include offsets; columns need not align by wall clock on these
days. Skipped local dates have no timed interval. A deterministic greedy interval partition
assigns columns within each connected overlap group, including minimum 24-minute visual targets
for short events. Dense days widen the horizontally scrollable surface rather than occlude
buttons. Every segment opens the original stable event in the existing editor. Tasks stay in
their date-only agenda. Drag/resize is separate work.

## Calendar display preferences

The Shell provides Sunday/Monday week-start and an optional named IANA display zone. Empty zone
follows the device and refreshes on focus. `track.calendar-display.v1` stores version 1,
`weekStartsOn` (0/1) and `timeZone` in browser-origin/profile localStorage, shared across vaults.
The accepted zone must be in the same generated Go IANA list as event authoring and supported by
the browser. `go generate ./internal/track/calendarevent` updates both lists; a parity test prevents
drift. Host-only placeholders and numeric-offset strings cannot become new-event defaults.
Reading never writes or repairs storage implicitly. Invalid/unreadable stored values fall back
to defaults with a warning in the settings disclosure. Apply validates and persists first; a
failed write retains the active view and form input. Defaults remove the key. Storage events
update other tabs. Static exports ignore these private preferences and offer no settings form.
The native app's normal fixed origin and persistent WKWebsiteDataStore use the same scope;
other ports/profiles are independent. No user config, vault data or OS setting is modified.

Civil UTC arithmetic aligns month/weekday columns and Sunday/Monday weeks without dropping a
zone's skipped date. Display-zone day intervals are found independently by Intl civil-date
boundaries, cached by zone/date with bounded maps; 23/25-hour days, half-hour transitions,
midnight gaps and skipped dates retain real instants. Empty intervals have no timed membership.
Today/Tomorrow, midnight/focus refresh and overdue reminders use the display zone; dated tasks,
all-day entries and activity-history dates remain civil dates. A browsed month stays selected
when the zone changes; Current month uses the new zone.

The original event editor retains authored zones and stored timestamps. New timed-event defaults
use the display zone, captured at editor mount so later preference changes cannot reset a draft.
Existing browser/native unsaved-input protection stays intact.
