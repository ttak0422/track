# Canonical-read-only process sentinels

Build the candidate Go CLI and run the standard-library-only harness explicitly:

```sh
go build -o /tmp/track-readonly-test ./cmd/track
python3 tests/canonical_read_only/process.py --binary /tmp/track-readonly-test
```

The harness creates and removes only synthetic temporary fixtures. It supplies
explicit machine configuration, active and foreign vaults, ordinary and private
cache roots, HOME, and supervisor identity. It never launches default writable
mode, invokes `web stop`, uses a registered personal vault, or opens a browser.
Each server is terminated and reaped even on assertion failure. The caller is
responsible for building the exact candidate being evaluated.

Assertions cover canonical directory inventories, file bytes, modes, sizes and
nanosecond mtimes across startup, active/foreign reads and indexed search, render/View Spec/follow,
healthy active/foreign calendar events/activity GETs,
denied endpoints, read-after-external-edit, storage retry and graceful shutdown.
The external edits update only the two expected body tuples, calculated from
the explicit write bytes and exact nanosecond timestamp. No live canonical
resnapshot is accepted after startup; all other sentinels remain unchanged.
Capability mode is asserted before waiting for readiness and again with deliberately
invalid configuration while storage is unavailable (without cache creation). The
supervised child listener readiness token is checked. Private cache database and PID existence are asserted while running; PID removal
is asserted after shutdown. Ordinary cache and machine configuration are protected
by the same sentinels. Partial launch flags and invalid root inputs must fail
without creating cache entries. Atime is excluded because reads can change it.

Limits: this is an application-contract test, not an OS sandbox. Reading updated
note bodies does not alone prove OS watcher delivery or exclusive watcher-driven indexed-content freshness;
package-level sentinel tests cover the production watch reconciliation callback.
It does not prove arbitrary hostile concurrent rename/mount resistance, every
HTTP route, every interrupted bootstrap schedule, or macOS behavior. It does not
run an old binary or launch default mode: partial required options failing
closed is the checked no-fallback behavior, not a native client handshake test.

Calendar ranges are empty fixture ranges: HTTP 200 verifies healthy dispatch, not
calendar event-content correctness. The first capability request can race already
completed startup and therefore does not establish observation of the initializing
phase. The separate invalid-configuration/unavailable case establishes that the
capability remains available independently of verified storage.
