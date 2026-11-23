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

## Synthetic admission-shape matrix

```sh
python3 tests/canonical_read_only/admission.py --binary /tmp/track-readonly-test
```

This additional engine-only matrix does **not** open GPUI's ordinary-vault gate.
It uses the same explicit canonical flag pair and temporary fixture harness:

- Machine `default_vault` and registered active/foreign paths, plus vault-local
  link format, property schema and saved query configuration.
- Explicit synthetic `HOME`, with `~/active`, `~/peer` and `~/normal` resolution.
- Explicit synthetic environment selector, an additional environment-registered
  peer, and an ordinary-cache override. Every canonical/config/ordinary cache
  inventory stays unchanged through cold startup, foreign reads, retry and exit.
- Missing registered root, namespace overlapping an active vault or registered
  peer, namespace containing machine config, and symlink/hardlink cache entries
  fail closed without database or PID creation. Rejection may precede listeners
  or leave the capability endpoint available with unavailable storage.

The engine policy accumulates active/known peer roots and vault-local config paths
in `readPolicy.withConfig`; `cacheConfig` validates their separation before the
lease/open callbacks. In particular, a registered peer whose root cannot be
resolved is not permission to skip its protection: the missing-peer case expects
no stores or leases, including for the otherwise valid active vault.

### Remaining admission decisions and gaps

These engine-only cases use disposable fixture inputs; they do not exercise
the GPUI review dialog. Experimental ordinary GPUI reads require a separate
explicit `--read-only` review, manifest-bound v2 admission and verified
capability before reads. Unreviewed ordinary launches and ordinary mutations
remain blocked. See [the isolated native acceptance procedure](../../experiments/gpui/qa/ORDINARY-READ-APPKIT.md)
for the foreground AppKit checks that this suite does not cover.

This matrix does not validate newly discovered peer/config replacements during an
in-flight generation, unreadable/network-mounted roots, permissions under a
non-owner account, arbitrary concurrent path replacement, mount aliases, or Mac
filesystem behavior. It does not run a native GUI, use personal data, test event
content, or claim an OS sandbox. The existing Go internal-policy tests cover
additional provisional/retry/late-open schedules; neither suite exhausts them.

The HOME scenario preserves configured `cache_dir: ~/normal` without setting
`TRACK_CACHE_DIR`. A separate no-`cache_dir` scenario supplies a synthetic
`XDG_CACHE_HOME` and protects that entire directory, including absence of its
normal `track/` cache. The environment-override case is separate coverage and is
not evidence for unoverridden machine configuration.

Storage can report ready just before its bootstrap generation finishes. A retry
POST during that interval is intentionally a no-op. The matrix makes bounded
explicit retry attempts until the generation advances, asserts the unchanged
canonical inventory after every attempt, and prints attempt counts/generations.
This is retry lifecycle coverage, not exclusive watcher-delivery evidence.

Every matrix case also snapshots the entire synthetic HOME, not just its known
vault/cache children, so unexpected `.config` or other home writes fail. TMPDIR
is explicitly the disposable private cache root; no real home or normal system
temporary location is supplied to the engine child.

The matrix checks exact registry paths and per-vault note body/path/identity
(including the engine's empty active-vault label), not just a shared note ID.
Missing peer roots must remain absent; a peer overlapping the private namespace
is itself included in the canonical inventory. Rejection assertions require the
expected validation error class, not just an arbitrary nonzero exit/unavailable
response. Owned process groups must disappear within a bounded wait after leader
exit; remaining descendants are killed and the test fails. The shared launcher
preflights both adjacent ports and excludes port 65535; it does not retry a failed
server launch or hide a post-preflight port race.

Vault-local configuration coverage is load acceptance and preservation, not
independent verification of property, saved-query or link-format semantics.
XDG_CONFIG_HOME discovery is intentionally untested: every matrix launch supplies
an explicit TRACK_CONFIG, matching the inactive native admission design.

The changed-peer retry case starts a verified active/foreign pair, then explicitly
replaces only the machine config's `work` registry path with a missing synthetic
path. Its expected config tuple is computed from the written bytes and exact
nanosecond timestamp; no other sentinel is refreshed. After an accepted new retry
generation, the expected outcome is whole-bootstrap `unavailable` with the
specific missing protected-vault error, not merely a named-peer warning. The
missing directory must remain absent, existing private DB paths stay unchanged,
private-root entries remain in `canonical-read-only-v1`, canonical capability stays
true, and mutation requests remain denied. Existing private SQLite cleanup/writes
are allowed; their bytes/mtimes are not treated as canonical. This does not cover
recovery, hostile concurrent swaps or every accumulated-peer history.

## Manifest-bound internal child transport

```sh
go build -o /tmp/track-bound-read ./cmd/track
python3 -B tests/canonical_read_only/bound_launch.py --binary /tmp/track-bound-read
```

This marker-free disposable process matrix invokes `__native-read` through the
actual CLI, before ordinary selector/config rewriting. It checks observation-only
preflight, v2 revision/nonce capability before reads, active/peer reads, sticky drift,
18 rejection cases, both listener bind failures, SIGTERM and actual parent-death
cleanup. Every server and supervisor is test-owned. Canonical/config/ordinary-cache
sentinels cover bytes, modes, sizes and nanosecond mtimes; preflight also protects
the private root. No real vault, default writable launch or native UI is used.

Sync conflict filenames and invalid metadata are isolated while valid HTTP reads
continue. A read-only connection to the **synthetic private index only** verifies
individual issues and retention of a subsequently corrupted indexed note. The
HTTP checks also verify that bounded per-file diagnostics survive partial reads.
A corrupt private database instead reports unavailable storage. These checks
do not establish a complete native partial/unavailable-peer UX or arbitrary
hostile filesystem safety. This matrix does not drive GPUI admission;
unreviewed ordinary launches remain blocked.
