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

## Render and preview contracts

The real-Go render scenario covers block includes within a vault, a cross-include
into the active vault (explicit empty origin), a named foreign origin, and scoped
query output/resolution. Successful inherited origins are admitted for preview
assets only when they equal the current verified host. Error/ambiguous include
metadata remains ineligible; explicit origins still pass existing registry and
session checks. ViewSpec and gallery consumers use this shared scope candidate
without falling back to the working vault.

The same fixture reproduced a separate chart rejection: Go's generated ECharts
option includes `_trackTheme:true`. The bounded numeric decoder now accepts only
that exact presentation marker while retaining all other field/type/size limits.
It preserves category/value/null semantics and the supplied palette. This does
not add browser theme, zoom or interactive chart parity. The integration checks
real inline metric bar/line output; full GPUI and gallery image painting remain
unverified. The source is retained for unsupported chart options.

## Raster assets and orientation

The bound asset scenario serves the same filename from two generated vaults with
different pixels. Full images and thumbnails preserve the requested scope; an
empty scope selects the launch vault. Missing/non-image/traversal requests and
an asset found only in another vault do not trigger fallback. Protected files
remain unchanged through owned shutdown.

The native raster decoder previously read raw JPEG pixels without applying EXIF
orientation. It now reads orientation from the bounded header decoder, retains
`ImageReader::decode` and its allocation reservation for pixel decoding, then
applies orientation before thumbnail sizing and BGRA conversion. Tests map all
eight JPEG orientations independently against decoded baseline pixels, cover
missing/invalid metadata and verify portrait thumbnail limits. The real Go
transport also checks an oriented JPEG. Alpha/channel policy is unchanged.

These are pixel/transport tests, not native painting, colour-management or GUI
acceptance. PDF rasterization and macOS are not exercised. Asset containment is
still the existing controlled-fixture/engine contract, not an OS sandbox or a
promise against hostile filesystem replacement; the ordinary-vault gate stays
closed.

## Resolver and note relationships

The bound navigation scenario uses duplicate titles and IDs across two vaults,
resolves local and explicit cross-vault targets, and opens the returned keys
without substituting the request scope. Resolver responses with an omitted vault
mean the launch vault; this differs from source-inherited note relation fields.
The test checks exact backlink/child identities and namespaces, missing-target
non-creation, another-vault-only target rejection and protected inventories.
It does not exercise wiki/Markdown syntax parsing, external backlink/trail
presentation or actual GUI activation. No production resolver behavior changed.

## Linux build and navigation validation

The real (non-stub) GPUI 0.2.2 and xattr 0.2.3 dependencies were materialized from
hash-verified official archives with the tracked patches and guard markers.
On Linux x86_64 with Rust 1.99.0, `cargo check --locked`,
`cargo check --locked --tests` and `cargo test --locked --no-run` against
`experiments/gpui/Cargo.toml` succeeded. Both native test executables linked.
This was direct Cargo validation, not execution of the Nix derivation. Existing
compiler warnings remain; no native executable or GUI was run. Earlier mentions
of full GPUI being unverified describe the earlier fixture-only runs, not this
subsequent compile/link check. macOS, painting, input and IME remain unverified.

The source adapter passed 329 tests (19 ignored), and the isolated real-Go live
matrix passed 146 tests. An earlier run concurrent with native compilation had
two failures: a process-capture fixture race and a save-race readiness timeout.
The capture fixture now signals after the background shell has established its
redirection before its leader exits; ten focused runs and the final suites
passed. Capture behavior, error assertions and time limits were not relaxed.
The save timeout was not reproduced in the isolated rerun; its cause is not
established and no save behavior or timeout was changed. These results do not
open the ordinary-vault gate or establish complete UI compatibility.

## Populated calendar projections

A further real-Go scenario seeds fixed canonical event records before startup.
The engine validates their normalized fields and saved revision fingerprints;
no event-write endpoint or new fixture authority is introduced. Both generated
vaults use the same event IDs with distinct titles. Explicit main/work and the
empty launch scope preserve event and linked-note identity.

The scenario covers a two-day all-day event with an exclusive end and a timed
event spanning the New York autumn offset change. Engine day boundaries span
25 hours in New York and 24 hours in UTC; range counts and preview identity are
checked against the event list. Canonical inventories remain unchanged after
owned shutdown. Cancelled/deleted records, preview truncation, spring clock
changes and GUI presentation are not covered by this scenario. Production code
and the ordinary-vault gate are unchanged, so no user-facing help change applies.

With this scenario included, the source adapter passed 329 tests (20 ignored)
and the isolated real-Go live matrix passed 147 tests. The real GPUI Linux
`cargo check --locked --tests` and `cargo test --locked --no-run` also succeeded;
this remains compile/link evidence, not GUI execution.
