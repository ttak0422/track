# Disposable manifest-bound startup integration

This **test-only** bridge connects the native owned-process adapter to Go's
internal manifest-bound v2 bootstrap. It does not enable ordinary-vault launch,
editing, SSE, or a production v2 CLI. The generated fixture, not a manifest
returned by the process, remains the admission authority.

The test captures the manifest with the same explicit environment and cwd as the
serving process, then checks the exact revision and correlation nonce after the
existing owned-process readiness token. The nonce is not authentication. Registry
identity and stable ready storage are checked separately. Finite note reads,
local Save refusal, sticky configuration drift, wrong-revision startup rejection,
protected-file inventories and owned cleanup are covered. No real vault is used.

## Run

From the repository root with Go and the experiment's Rust version installed:

```sh
go test -c -o /tmp/track-bound-bridge.test ./internal/track/webui
go build -o /tmp/track-fixture-cli ./cmd/track
python3 experiments/gpui/qa/prepare-bound-harness.py /tmp/track-bound-harness-new
TRACK_NATIVE_TEST_SYNTHETIC_ONLY=1 \
TRACK_NATIVE_BOUND_TEST_BINARY=/tmp/track-bound-bridge.test \
TRACK_NATIVE_TEST_BINARY=/tmp/track-fixture-cli \
cargo test --manifest-path /tmp/track-bound-harness-new/Cargo.toml live:: \
  -- --include-ignored --test-threads=1 \
  --skip owned_public_fixture_save_read_conflict_and_reap
```

The skipped legacy test requires a separate explicit public-help fixture copy and is
not part of this synthetic-only run. Use a new harness directory. The generator reads dependency versions from the
experiment lockfile, includes the actual transport/policy modules, and extracts
three unchanged UI-independent helpers. It replaces only GPUI's image carrier
with a simple in-memory test type. These tests **do not compile or validate the
full GPUI application**, native layout, IME, clipboard, GPU behavior or macOS
runtime. Serial execution is deliberate and is not evidence that historical
parallel recovery-store locking issues are fixed.

Capture output is bounded and nonblocking. The capture group leader is observed
without reaping, remaining group processes are terminated, and only then is the
leader reaped. Descendant-held and closed-pipe regressions verify cleanup. These
point-in-time checks are not a hostile-filesystem/process sandbox or a hard
kernel-I/O latency guarantee. The Go bridge is absent from production binaries.

## Finite-only session behavior

The test-bound owner now passes its admitted event policy into the ordinary
session lifecycle. A bound session has an inert observer: it starts no SSE
request, retry loop or event worker, and does not manufacture change/gap counters.
Its explicit unavailable state is distinct from an interrupted connection.
Follow refuses that state; automatic refresh controls are unavailable, while
finite reads and explicit Reload remain the supported path. No production
admission route selects this policy yet.

The adapter harness also includes the actual event-awareness module. Tests cover
unavailable notices, stale dismissal and Follow tickets, inert observer cleanup,
and the actual Go-bound session's finite read and owned-process shutdown.
Workspace control wiring requires full GPUI/native validation separately.

Local Linux verification for this finite-only unit: the complete adapter suite
passed 318 tests (14 opt-in tests ignored), and the live matrix with explicit
synthetic Go binaries passed 135 tests. The two-module event/notice suite passed
16 tests, and the Follow suite passed five tests. An earlier overlapping cargo
invocation failed two subprocess-spawn tests with ENOENT; rebuilding the same
test executable while another suite was running is the suspected harness cause.
The final complete run was serialized and passed. This does not resolve the
separately documented historical recovery-store concurrency issue. Full GPUI
compilation and macOS runtime were not performed for this unit.

## Scoped browsing compatibility

The real Go bridge reproduced a native graph mismatch: an explicitly requested
launch vault still has an empty wire label, which the earlier native adapter
rejected. Full and local graph reads now use the caller's current verified launch
identity to recognize that alias. They preserve canonical node namespaces and
reject explicit foreign labels, malformed labels and wrong graph centers. Missing
legacy optional labels remain scoped to the request. This is not a broader
ordinary-vault admission grant.

The synthetic browsing scenario uses two vaults with the same note IDs and checks
lists, tags, scoped/workspace search, recent-created order, full/local graph
identities, finite note navigation and protected inventories. The help correction
is in `docs/help/note/1785024017000.md`. `nix develop --command make site` could not
run in this container because `nix` is not installed; rendered help and full
GPUI/native interaction remain unverified.

## Home and calendar reads

A second real-Go scenario exercises two same-ID vaults with populated dated notes,
open/completed tasks and existing journals. Agenda, task listings, calendar
note/task summaries and journal-to-note navigation preserve scope. Missing
journals and empty event day/range queries do not create canonical files or a
calendar directory. The final protected inventory remains unchanged. This
scenario does not cover populated calendar-event files, DST or native rendering.

Range preflight now matches the Go projection domain of years 0001 through 9998;
year 9999 is rejected before HTTP for both event and note/activity ranges. This
leaves the 42-day cap, ordinary-vault gate and explicit named-zone semantics
unchanged. The help date-domain wording was updated; the previously recorded
Nix/site-rendering limitation still applies.
