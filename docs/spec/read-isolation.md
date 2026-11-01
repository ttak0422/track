# Read isolation and progressive startup

`track web` binds its owned listeners before configuration, cache open, title audit, or indexing.
Listener readiness means the UI and ownership endpoints are reachable, not that storage is healthy.
The loopback Markdown capability inbox is independent of vault readiness and persists across
backend handoffs, preserving one-use tokens without granting disk access.
Verified configuration selects the root; provisional configuration never authorizes writes. A root
change during a retry is rejected. The shell, independent panels and healthy peer reads continue
while unavailable storage is reported explicitly. First-run confirmed files are usable read-only. Process ownership is claimed after configuration
verification and before cache opening, then retained across cache failures/retries until final
cleanup. Markdown protocol readiness does not assert PID-lease or storage readiness.

Pure input operations run in the same executable's hidden read-worker mode, before CLI/config
routing. Workers may read, enumerate, inspect, canonicalize, parse and verify versions. They cannot
open an index, run Babel, migrate notes, record reading/activity dates, or perform deletions. Parsing
returns bounded, alias-free YAML nodes so unknown extension fields and integer/timestamp types
survive parent reconstruction. File bytes are bound to descriptor and path identities before/after
reading; this detects ordinary concurrent replacement, not an atomic whole-filesystem snapshot.

There are four filesystem slots and a 32-entry work queue. Admission allows at most 36 callers
overall, with at most two worker reservations and sixteen waiting callers per vault. Ordinary
concurrency waits for a release notification under its original deadline; a blocked-owner burst
cannot consume every admission ticket while other workers are idle. A single retiring child does
not reject admitted reads while another owner slot can still make progress; a fully quarantined
owner fails explicitly. Readiness polls observe the writer gate without acquiring it, so a poll
cannot be mistaken for a superseding mutation. Status is advisory; writes still acquire the gate
and validate the source and cache.
Caller deadlines cover admission, spawn, exchange and parse. A child not reaped after cancellation
continues to consume its slot and owner reservation. Configuration has a separate bounded pool.
Frames are limited to 24 MiB, text files to 16 MiB, metadata to 4 MiB, directories to 100,000 entries.
Stage snapshots admit at most 64 MiB/10,000 changed notes; progress retains at most 32 MiB/1,024 notes.
A subsequent pass verifies prior fingerprints and prioritizes unread notes, without retaining their
bodies indefinitely. Budget exhaustion always produces a partial state. Diagnostics are bounded.

Staging never mutates files and holds no shared writer lock over input I/O. Parent publication checks
the cache's semantic revision, verifies item versions, and transactionally publishes confirmed notes
and links. A mutation invalidates the generation before touching files, including a subsequently
failed mutation. Provisional/partial stages cannot update activity sidecars. Optional activity writes
use temporary files and an expected-metadata check before replacement; noncooperating external
writers are not covered by a global atomic compare-and-swap. Unverified rows/links are retained;
explicit `reindex --prune-missing` is the existing intentional-deletion reconciliation contract.

HTTP read handlers have a one-second deadline, 24 MiB response cap, per-vault admission and an overall
16-handler ceiling. Stuck handlers keep admission until return. Federated queries collect healthy
pages concurrently under a shorter deadline; a blocked peer becomes a named gap. Last-good GET
responses have a 128-entry/32 MiB bound and generation keys. POST previews never reuse another
draft's response. Late results cannot seed a newer generation. Cache opens and peer opens retain
admission until actual return; retries cannot multiply blocked opens. Shutdown bounds its wait and
never represents a pending authoritative write as successfully cancelled.

A read-only initial note response may arrive after a concurrent storage-ready invalidation. The
editor retries that read only after a successful ready poll newer than the response, at most twice
per note/generation/continuous read-only episode. Errors stop automatic recovery; manual retry
remains available. Draft adoption and write guards are unchanged. Retiring a backend ends its SSE
streams, so clients reconnect to the new generation instead of silently losing later updates.

Authoritative mutations invalidate the bootstrap progress snapshot before writing; subsequent reads
use current input/progress or the derived cache. Saves cancel earlier reads before the request and
again before publishing the acknowledgement to the client cache. An older read-only snapshot cannot
replace a verified/saved editor body, and fresh reads cannot overwrite an unsaved draft. Recovery
never converts an explicitly read-only note into writable data merely because storage reports ready.
