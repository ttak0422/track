# Ordinary read admission: proposed implementation boundary

Status: ordinary native admission remains closed. The supervised engine CLI
transport is implemented below; it is not native admission or release approval.
The ordinary-vault gate stays closed. This document does not supersede the
fixture restrictions or establish UI equivalence with the shipping Track app.

## Initial stopping point (before the slices below)

The real-Go synthetic bridge exercises a manifest-bound owned process, finite
responses, sticky drift rejection and canonical-file sentinels. Linux builds
also type-check and link the real GPUI application. That is a useful completed
integration milestone, not ordinary-workspace admission.

The missing production path is structural:

- `live.rs` admits `Fixture` in production. `SyntheticCanonical`,
  `SyntheticBound` and the bound finite-only event policy are test-only.
- `live_read_inputs.rs` prepares inputs but cannot grant launch authority.
- `config.ResolveReadOnlyLaunchManifest` and
  `webui.newManifestBoundBootstrap` exist, but the bound production CLI protocol
  is not connected. The native test bridge is not that protocol.
- The bridge proves an exact generated two-vault environment. A real configured
  workspace can have unavailable peers and different HOME/XDG/cwd semantics.
  Its authority must not be inferred from a disposable marker or a manifest.

Further empty/nonempty endpoint fixtures do not solve these missing pieces.

## Required authority and context

A production admission proposal must explicitly bind the selected workspace,
allowed registered peers, executable identity, configuration provenance and
runtime-cache location before spawn. The manifest is a bounded observation,
not user approval, authentication, readiness or permission to edit.

Go remains the authority for configuration precedence, path/tilde resolution,
registered aliases and ordinary-cache separation. The native side must preserve
and bind the actual resolution context; it must not reuse the synthetic bridge's
rewritten HOME, XDG_CONFIG_HOME, cwd or exact two-vault registry. In particular,
using the private runtime directory as XDG_CONFIG_HOME is fixture isolation,
not a proposed production configuration-discovery rule.

A private runtime root must be separately owned and private, proven disjoint
from canonical vault/configuration and ordinary derived-cache namespaces. Its
creation and cleanup must not change existing vaults, normal cache, permissions
or unrelated runtime roots. Cleanup owns only this launch's resources.

Unavailable registered peers are an availability condition, not permission to
silently select another vault. Resolve their identity separately from readiness;
the test bridge's all-peers-ready requirement is not a production policy.

## Proposed implementation order

1. **Inactive contract and independent review.** Specify the exact production
   selector/context/peer-authority representation, failure states and private
   root ownership. Keep it impossible to call from ordinary CLI or UI. Resolve
   how explicit workspace selection grants peer-read scope; do not infer it
   from the existence of a registry entry alone.
2. **Production-shaped engine protocol, still gated.** Introduce a bounded
   preflight/serve contract over the existing manifest and bound bootstrap,
   without fixture markers. Bind the expected revision and per-launch nonce,
   retain the actual parent/token ownership handshake, and reject drift before
   lease/store opening and before finite-response publication. No unbound or
   version-1 fallback, automatic restart or timeout relaxation is permitted.
   A nonce is correlation only. Test this through disposable generated inputs.
3. **Native finite read lifecycle, still gated.** Replace synthetic proof with
   the reviewed authority type, not an unchecked manifest conversion. Integrate
   the production finite-only session policy: no SSE/Follow attempt or hidden
   polling fallback. Refuse Save locally. Invalidation retires queued callbacks
   and future reads, presents a persistent unavailable state and retains source
   data as stale rather than silently promoting a new process/storage identity.
4. **Release review.** Review canonical-write exclusion end to end, private
   resource ownership, crash/timeout/drift cleanup, peer unavailability and
   public-v1 behavior against the exact candidate. Only a separate deliberate
   change may open ordinary read admission. Editing remains a separate gate.

Do not promote all of `live_bound_fixture.rs` into production: its fixture walk,
fixed limits, generated-registry assumptions and test-binary command protocol
are intentionally not a real-workspace authority model.

## Acceptance evidence and exclusions

Each implementation slice needs actual-source tests plus independent review.
The production-shaped protocol must cover configuration precedence and absent
files, path aliases, unavailable peers, private-root collisions, drift during
startup/in-flight reads and owned-process cleanup without canonical changes.
Linux compile/link and isolated read matrices are available cloud checks.

None of this is an OS sandbox, exclusive filesystem snapshot or defense against
hostile path replacement. Asset symlinks and filesystem scope must be described
according to the engine's actual behavior, not advertised as physical containment.
Already-admitted private-cache or lease work may finish after invalidation;
finite-response publication must still fail closed and owned cleanup remains
permitted.

Native layout, input/IME, clipboard, focus, termination and visual equivalence
are separate acceptance work. No current macOS or GUI result is supplied by
these cloud checks. The shipping app is not replaced; the broad gaps in
[PARITY.md](PARITY.md) remain. Ordinary read admission would not itself complete
Markdown, graph, calendar editing, persistence or full interaction parity.

## Decision checkpoint

The next substantive unit is the authority/context contract in step 1, followed
by the gated engine protocol in step 2, not additional calendar edge-case tests.
Before choosing a production peer-scope or unavailable-peer UX policy, review it
against the existing app's workspace behavior. No such policy is silently fixed
by this checkpoint, and no executable behavior changes with this document.

## First inactive context slice

`live_launch_context.rs` records a supplied resolution-context request and
compares later supplied observations without reading the process environment,
filesystem or vaults. It preserves HOME and all TRACK_/XDG_ entries exactly,
including empty values for XDG_/TRACK_, unknown names and TRACK_CACHE_DIR.
Missing or empty HOME and
unsupported encoding/control characters fail closed; Rust does not expand tilde,
resolve relative paths or invent platform defaults. This is intentionally a
conservative request representation, not an implementation of Go's loader.

PWD is recorded separately because Go's `os.Getwd` can return its lexical value
when it names the actual current directory. It is not part of Go's environment
revision. The supplied cwd is a requested path, not proof of that runtime result.
PATH and TMPDIR are also outside the manifest revision and need a separate
execution policy before a child can be configured.

Ownership variables (`TRACK_WEB_DESKTOP_PARENT_PID` and
`TRACK_WEB_DESKTOP_READY_TOKEN`) are inside the TRACK_ namespace. Future
preflight and serve must observe the same final owned-child values, not compare
a pre-injection request with a post-injection manifest. The record does not
calculate a manifest hash or grant executable, peer, cache or startup authority;
there is no conversion to `StartupAdmission`, no command builder and no launch
caller. Debug/error output must not reveal environment values.

The existing independent registry comparator is not replaced. Explicit peer-read
authority, filesystem identity/provenance and the production protocol remain the
next implementation boundary. Merely matching this context is not admission.

### Context-slice verification

Four focused context tests cover exact values/presence/order, rejected ambiguous
inputs, independent PWD handling and entry/byte/path bounds. The complete source
adapter passed 333 tests (20 ignored); the live-module run with the explicitly
supplied Go binaries passed 151 tests. Existing ordinary-launch rejection and
canonical-file sentinels remain in those suites. Independent review found no
launch path or authority conversion. No user-visible behavior changes, so this
inactive slice needs no user-facing help update.
Real-GPUI Linux `cargo check --locked --tests` and
`cargo test --locked --no-run` succeeded with this slice. Native GUI, macOS and
ordinary-workspace launch remain untested and disabled as applicable.

## Inactive workspace scope and provenance slice

`WorkspaceScopeRequest` separates an explicit selector and an independently
supplied requested peer-display-path set from `LaunchManifestObservation`.
Unresolved scope never matches; a populated manifest does not fill in missing
intent. Literal path selectors, including relative paths and tilde spelling, are
preserved rather than resolved by Rust; resolved display/canonical paths are
separate observation fields. The observed context cwd may retain an absolute
lexical PWD alias from Go; it is preserved, not cleaned by the decoder.
Matching the requested set is only a comparison, not consent or startup
authority. No selector, manifest or response can mint a peer-read grant.

The observation retains the complete typed Go manifest, including canonical
paths, opaque root/cache identities, configuration provenance and absent config
states. Its bounded decoder checks wire structure without reading the filesystem,
recomputing hashes or certifying the identities. A later exact observation
comparison detects changed reported provenance; it does not establish authenticity,
filesystem freshness or atomicity. The request and context records still have no
conversion into `StartupAdmission`.

The selected active display path can be an alias distinct from the same peer's
registered display path, while canonical identity/cache fields must agree.
Absent configuration is not an error by itself, and nonpositive configuration
mtime is not rejected merely because generated fixtures usually use recent files.
Go's existing manifest requires every declared root to be verifiable; a missing
root fails preflight. This differs from a verified peer whose runtime storage is
not ready. Existing `ExpectedRegistry` remains the later readiness comparator;
its available/unavailable logic is not duplicated in the provenance decoder.

This slice is not the production CLI protocol, private-root lifecycle or a
workspace selection UI. Those must supply and validate actual context and
independent authority before the ordinary gate can be reconsidered.

Six pure provenance tests cover independent/unresolved scope, literal selector
comparison, aliases, absent and historical configuration metadata, malformed
wire rejection and registry bounds. The synthetic bridge additionally decodes
actual Go manifests and compares against its independently generated scope before
running the original strict fixture checks. The source adapter passed 339 tests
(20 ignored); the live-module matrix with the explicit Go binaries passed 157.
The prior fixture authority and canonical-file sentinels remain intact.
The final candidate also passed real-GPUI Linux `cargo check --locked --tests`
and `cargo test --locked --no-run`. These are compile/link checks, not GUI
acceptance. The gate is unchanged; this internal-only slice adds no user-facing
workflow and therefore no help-vault change.

## Internal preflight/serve protocol slice

An unexported Go dispatcher accepts a bounded versioned request containing an
explicit selector, private root, nonce and parent-supplied final child context.
Preflight requires an empty revision and returns the existing Go manifest;
serve requires the exact expected revision and returns the existing lazy bound
bootstrap. Neither branch listens or adds a public CLI route. The generated
fixture bridge remains the only caller and retains its exact fixture proof,
parent watcher, ready-token notification, listener ownership and cleanup.

Before and after dispatch, Go compares the complete HOME/TRACK_/XDG_ entry set,
PWD presence/value and literal cwd with the supplied expectation. The actual
parent PID and ready-token shape are checked at that moment; this does not
replace ongoing owner monitoring or authenticate an arbitrary caller. Failed
post-checks close a created bootstrap. Cancellation, mismatch or malformed input
never falls back to an unbound protocol. A nonce remains correlation, not consent.
The existing manifest revalidation and canonical-write exclusion remain intact.

Requests reject unknown, duplicate, case-aliased and non-ASCII object field
names, trailing JSON, excessive depth and byte/entry/path limits. Required fields
and mode-specific revision rules are enforced. Error/Debug output does not expose
the supplied environment. Rust encodes both phases from the final env-cleared
fixture command after ownership values are installed, not from an expectation
manufactured by the child. The context record now stores literal cwd as a string
so aliases cannot compare equal through Path component normalization.

The request format allows up to 256 KiB. The initial bridge used one environment
variable, which could exceed OS per-string exec limits. The bounded pipe slice
below replaces that mechanism completely. A future production caller still
needs authority/lifecycle review rather than adopting the fixture wrapper.
No ordinary launch, actual workspace grant, private-root creation or native GUI
acceptance is introduced by this internal slice.

Protocol verification passed the webui/config Go packages and focused race tests
three times. Fresh subprocess cases cover malformed/duplicate/case-folded fields,
mode/revision bounds, cancellation, context/owner mismatches and no premature
lease or canonical-file changes. The Rust source adapter passed 343 tests
(20 ignored); its live-module matrix passed 161 with the new Go protocol bridge.
The wrong-revision fault now changes the structured request revision rather than
the retired loose environment field, retaining the original no-write assertions.
Real-GPUI Linux tests check and no-run link also passed. Independent review
confirmed the ASCII-field-name fix and lifecycle boundaries. Mac/GUI and public
production transport remain unverified; the ordinary gate stays closed.

## Bounded owned-pipe transport

The fixture bridge now receives requests only through its owned stdin pipe,
not an environment payload. Rust advances bounded nonblocking partial writes;
preflight alternates those writes with stdout draining under one capture deadline
so neither full pipe can deadlock the other. Completion closes stdin to deliver
EOF. A partial send, early exit, overflow, cancellation or timeout does not publish
a successful preflight. Existing owned-group cleanup remains specific to capture.

For serve, the existing exact-child owner and output drains are established
before transmission. Sending and readiness share one deadline. Failure retains
the existing kill/reap cleanup; no process-group authority is invented for serve.

Go requires a pipe and a deadline, captures its FD once, switches that descriptor
to nonblocking raw reads and checks cancellation between reads/waits. It consumes
at most the byte limit plus one, requires EOF and closes the owned input on every
return. There is no potentially stranded background read goroutine, regular-file
fallback or old environment-request fallback. The test bridge applies a bounded
five-second request-read phase; its existing owner watcher and outer startup
bounds remain. Decode/context checks still precede bootstrap publication.

This closes the environment-size transport gap for controlled owned pipes. It
does not create a private runtime directory, authorize workspace peers, make a
filesystem sandbox, introduce a public CLI or open ordinary-vault admission.
These bounds apply to the exchange and admission checks, not a strict wall-clock
promise for kernel I/O or process reaping. Existing cleanup/reaper limitations
remain; the transport does not strengthen them into an OS-level guarantee.

Pipe verification passed webui/config Go package tests and pipe/protocol race
checks three times. Rust tests include payloads larger than pipe capacity, an
unread pipe, early child exit, EOF closure and a 192 KiB request with 64 KiB of
child output emitted before input consumption. The complete source adapter
passed 348 tests (20 ignored); the real-Go live-module matrix passed 166.
Real-GPUI Linux tests check and no-run link passed, with independent lifecycle
review. No user-facing workflow changed; Mac/GUI acceptance remains separate.

## Inactive private-runtime ownership primitive

The native ownership helper creates a new unpredictable child directory only
beneath an explicitly supplied, current-user-owned parent that is not writable
by group or other users. It does not choose a system runtime location, read
HOME/XDG defaults, adopt an existing root or repair permissions. Parent and child directory descriptors retain observed identities;
creation uses exclusive relative directory operations and never follows an
existing child symlink. Tests supply generated fixture parents only. There is no
production caller or new admission conversion.

Ownership is not workspace authorization or a separation proof. Before any
future launch, Go's manifest and `ReadOnlyCachePath` must still validate the root
against every protected vault, configuration file and ordinary cache namespace.
The caller must separately authorize the parent location before creation; this
helper cannot establish that creating a directory there is appropriate.

Cleanup is deliberately empty-only and explicit. Unexpected contents, changed
identity or an unverifiable path are retained rather than recursively deleted.
Dropping the handle closes descriptors without filesystem deletion. A served
cache normally leaves a nonempty namespace, so this is not completed cache
reclamation or crash recovery. Tracking owned descendants and coordinating
cleanup with lease/store/process termination remain separate work. Identity
checks and relative directory operations do not eliminate malicious same-user
replacement races between a check and a syscall; no filesystem sandbox or
atomic pathname ownership guarantee is claimed.

Seven focused tests cover exclusive creation, existing-name collisions,
parent/child directory and symlink replacement, permission drift, nonempty
retention, explicit empty cleanup and descriptor-only Drop. A fresh exact-test
subprocess applies a restrictive umask and verifies that the successfully created
but invalid-mode directory is reported and retained, not repaired or removed.
The same seven tests passed in the actual Linux GPUI-linked test executable
without starting the GUI. The complete source adapter passed 355 tests (20
ignored); the real-Go live-module matrix passed 173. Real-GPUI Linux tests check
and no-run link passed. Independent review found no blocking issue. Go code is
unchanged in this slice. These checks do not validate macOS, GUI equivalence,
workspace authorization or populated-runtime reclamation. The ordinary gate
remains closed; this internal-only primitive adds no user workflow or help change.

## Explicit workspace intent and owned-root preflight

The intended native read-request scope is the selected active vault plus the
complete registered peer set, made explicit at the request boundary. This keeps
shipping web workspace semantics: active selection is not a request to narrow
peer access. Unknown or unavailable peers still must not fall back to the active
vault. This scope decision does not authorize a real workspace. A production
intent/consent UI and its deliberate enablement remain separate gates; no request,
registry, manifest or successful comparison may generate a grant by itself.

The current integration is limited to the existing generated-fixture authority.
It passes a newly owned runtime child under that fixture's private parent to
Go's actual manifest resolver and separation validation, preserving the fixture's
original HOME/XDG/configuration context. The test bridge permits an independently
specified alternative root only for preflight capture, only as a directly owned
private child with the generated name shape. This bridge check is not proof of
new creation: Rust retains the actual creator handle and checks its identity.
Serve refuses the override. No
public CLI route, production grant constructor or new admission variant exists.

The native side compares the returned observation with the independently known
active selector and complete fixture peer set, while retaining the runtime owner.
The observation is evidence of Go validation, not authorization or a substitute
for subsequent drift checks. Preflight must leave the private runtime empty and
canonical fixture inventories unchanged. Explicit empty-only cleanup remains
separate; unknown contents are retained rather than deleted. This integration
does not solve populated-cache reclamation, crash recovery, or prove safe ordinary
workspace launch.

The first actual GPUI-linked integration run failed the bridge's exact fixture
configuration-byte check. The actual dependency graph enables
`serde_json/preserve_order`, while the source adapter did not; a `json!` fixture
object therefore had a different key order. The fixture generator now serializes
an explicit sorted map (including the already-sorted peer registry), with an
exact-byte regression. The Go check remains strict. This is a test-fixture encoding
fix, not relaxation of manifest validation or a change to production config.

After that fixture fix, both new integration cases and the encoding regression
passed in the actual GPUI-linked executable. Its full `live::`-filtered isolated
run (including the matching `workspace_live` unit tests, excluding the separate
public writable-fixture save test) passed 187 tests using
the explicitly supplied real Go binaries, without starting a GUI. The source
adapter passed 356 tests (22 integration tests ignored in that ordinary run).
Go webui/config package tests passed; override-boundary race tests passed three
times. Final real-GPUI Linux tests check and no-run link passed. Independent
review found no blocking issue. These are fixture/headless checks, not macOS,
GUI parity, actual user consent or ordinary-workspace release evidence. No
user-facing workflow changed, so no help-vault update was needed.

## Read-request presentation preview boundary

The next presentation prototype is compiled only for tests, with supplied fixture
labels and paths. It must display the active vault and the complete peer set,
read-only limits, private-cache retention and the closed ordinary-launch gate.
It does not inspect a real registry, read a vault, start preflight, create runtime
directories or publish a grant. A positive preview interaction records only a
local presentation decision, never an admission capability.

A real consent UI still requires a separate reviewed connection between the
exact displayed request, its independently obtained scope and current context.
Replacing a request must invalidate any earlier decision and stale input events.
Dismissal, cancellation, an absent decision or a changed request cannot be
interpreted as permission. Unknown/nonempty runtime contents must not be deleted
as a side effect of accepting, cancelling or closing the presentation.

`workspace_read_request.rs` models that local preview decision; the GPUI view is
also registered only under `cfg(test)`. Active display paths are independent of
registered peer display paths, so an alias is not silently substituted and an
unregistered active path can be shown. The model does not resolve aliases or
certify that the supplied peer list is complete. Bounded text is presented
without ellipsis, with scrolling for paths and the peer list; directional control
characters are rejected and Debug output is redacted.

Initial focus targets Cancel. Enter never confirms; a deliberate confirm click
or an unmodified, non-repeated Space on its focused control records only
`PreviewConfirmed`. A recorded choice remains dismissible. Denied is terminal;
replacing even identical request content requires a new decision and rejects old
input generations. Confirmation is hidden after recording a choice, and denied
state hides the action controls. Dismissal changes the preview state rather than
claiming that an enclosing window was closed.

The floating-layer presentation uses supplied theme tokens and text controls.
No production menu, shell, CLI, consent callback, grant conversion or launch path
is connected. This is a reviewable UI implementation boundary, not an enabled
ordinary-workspace feature. Actual focus, keyboard/IME, scrolling, accessibility
and visual behavior still need GUI acceptance before any production connection.

Six model tests passed in both the source adapter and actual GPUI-linked test
executable, covering pending/Enter, cancellation and revocation, replacement and
stale input, explicit scope, aliases/unregistered active paths, and misleading
text rejection. The complete source adapter passed 362 tests (22 ignored).
Real-GPUI Linux tests check and linked tests succeeded; the renderer was compiled
but not displayed. Independent review found no blocking issue. Go and live
transport were unchanged and not revalidated in this presentation-only slice.
No real vault, Mac or GUI was used. Because there is no user-accessible workflow,
this test-only prototype has no help-vault update.

## Fixture review binding, not production consent

A preview interaction cannot supply the independent authority missing from the
production path. The next binding experiment therefore retains the existing
`SyntheticCanonical` fixture proof as its only authority. It pairs that proof
with the creator-owned runtime, an independently supplied complete fixture scope,
the exact displayed request and the final env-cleared child command. No arbitrary
external command or child-reported scope can substitute for those inputs.

Dialog identity is separate from request generation: two fresh dialogs both
starting at generation one must not accept each other's events. The reviewed
combination must remain unchanged through preflight publication. Cancellation,
stale selection, changed context, changed executable/input bindings or replaced
runtime, when observed by the checks, fail closed and invalidate that review,
even if the old values are later restored. Changes entirely between checks are
not continuously detected. Successful preflight is one-shot, not a reusable
launch permit.

These are point-in-time fixture checks, not atomic authorization or continuous
filesystem protection. The transport's cancellation and owned-process cleanup
remain necessary. A real GUI-to-consent connection, independent real-workspace
authority source and deliberate production admission review remain unimplemented.
The ordinary gate stays closed; neither a preview decision nor a preflight
manifest enables serve or canonical editing.

The binding is a synchronous test helper, not a connected asynchronous GUI
consent flow. It owns its env-cleared command and compares program, arguments,
all explicit environment entries and cwd, in addition to the resolution-context
record. The independently retained runtime descriptor identity must match the
Go response. A shared cancellation flag is forwarded to the existing bounded
capture and checked again before result publication. GUI cancellation is not
wired by this experiment. Unknown runtime contents are retained; no cleanup is
triggered by a preview decision or a failed check.

Five binding tests passed in the source adapter and actual GPUI-linked tests,
including real-Go preflight, zero spawn attempts on denied/stale/mismatched
inputs, sticky rejection after restoration, one-shot completion and nonempty
runtime retention. Seven preview-model tests passed in both forms, including
cross-dialog event rejection. The complete adapter passed 367 tests (23 ignored).
The actual GPUI-linked `live::`-filtered run passed 192 tests with explicit Go
binaries, including matching `workspace_live` tests and excluding the separate
public writable-fixture save test. Linux tests check and linked build passed,
with independent review. Go source was unchanged. GUI/macOS, real consent and
ordinary admission remain untested or disabled as applicable. This test-only
binding adds no user workflow and needs no help-vault change.

## Bounded asynchronous fixture handoff

This handoff is limited to one fixture preflight worker for a test view's
lifetime. Confirmation remains a preview choice. Independent fixture authority
moves into the worker; no real-workspace authority is manufactured from it.
The UI-side handoff checks only immutable correlation data and performs no
filesystem validation or blocking process wait. The background worker retains
proof, runtime and process ownership and performs the existing checks.

Cancellation, request replacement and view destruction signal the same shared
cancellation flag. Replacement never creates a new review automatically. Result
acceptance must additionally match the exact flight identity, current dialog
instance/generation, displayed request and decision; a late result is not proof
that a cancelled request should be restored. Dropping a view must not join the
worker on the GUI thread or abandon its owned-child cleanup. Runtime directories
are not recursively removed on completion, discard or view destruction.

The bounded acceptance target is deterministic headless ordering tests, a real-Go
fixture completion and compilation of the GPUI asynchronous wiring. This cloud
shell has neither DISPLAY nor WAYLAND_DISPLAY configured, and Xvfb is not
installed. This slice does not change the environment to add a display. Actual
GUI event delivery, focus, IME and
real-Go cancellation initiated through that GUI remain separate acceptance work.
No serve, real consent, ordinary-vault admission or broader test expansion is
part of this handoff.

The prepared-fixture view now schedules the owned worker on GPUI's background
executor and uses a detached weak-view awaiter. Display-only previews remain
inert. The prepared action is labelled "Run fixture preflight", and status never
claims an access grant or automatic runtime cleanup. A completed result retains
its runtime owner; ignoring or dropping it closes descriptors without deleting
paths. Cancellation/replace/drop never resets a flight for another worker.

Four headless flight tests cover duplicate/wrong completion, cancellation before
start, barrier-controlled running cancellation/replacement/drop, and cancellation
after worker completion but before UI acceptance. Seven gate tests include the
actual Go background-worker success and a canceled-before-run worker with zero
spawn attempts. Both groups passed in the adapter and actual GPUI-linked tests.
The complete adapter passed 372 tests (24 ignored); actual GPUI-linked `live::`
checks passed 194 with explicit Go binaries and the existing public writable-save
exclusion. Final Linux tests check/link and independent review passed. No GUI
was displayed. Real-Go in-flight cancellation initiated by GUI events is not
claimed: only the headless ordering tests, forwarding and prior transport
cancellation checks support that boundary. Go source is unchanged.

This finite implementation checkpoint is complete. Actual GUI acceptance and
real-workspace consent/admission are the next separate decisions, not reasons to
keep extending this fixture suite. The ordinary gate stays closed. This test-only
wiring adds no public workflow and requires no help-vault update.

## Supervised production CLI transport, native gate still closed

`track __native-read` now routes before ordinary CLI selector/path rewriting.
It is an internal owned-child protocol, not a supported user launcher or a GPUI
admission grant. An actual parent PID and a bounded hex readiness token are
required before stdin is consumed. The existing parent watcher and signal
cancellation remain active for both phases. A bounded pipe request is the sole
input: there is no environment-payload, regular-file, unbound or writable fallback.

Preflight has no address and returns one JSON manifest plus LF, at most 128 KiB
in total. It creates no bootstrap, listeners, stores or leases. Serve requires
an explicit IPv4 loopback address with a valid adjacent port and the exact
preflight revision in the same final HOME/TRACK_/XDG_/cwd/PWD context. The existing
manifest-bound bootstrap remains responsible for cache separation, sticky drift
rejection and finite-response publication. Readiness is emitted only after both
listeners bind; version-2 capabilities correlate the revision and nonce. Errors
at the CLI boundary are generic rather than printing supplied context values.

The native caller is still test-only: `SyntheticBound::InternalCli` exercises
this actual CLI through the original generated-fixture authority and owned-process
adapter. It verifies v2 before storage reads and retains finite-only event policy,
local Save refusal and shutdown. Production `StartupAdmission` and the ordinary
GPUI launch gate are unchanged. Production intent binding, ordinary native finite
lifecycle, unavailable-peer presentation and populated private-cache reclamation
remain separate work; no test response or manifest may create that authority.

The marker-free CLI process matrix preserves supplied configuration semantics
and checks healthy active/peer reads, preflight private-root inventories, sticky
drift, invalid ownership/input/context/revision/cache separation, listener failures,
SIGTERM and parent-death cleanup. It also exercises sync conflict filenames,
cold malformed metadata and a subsequently corrupted indexed note. Healthy reads
continue and the private derived index records per-file issues without deleting
the cached note. The current HTTP bootstrap can replace those detailed issues
with a generic confirmed-files partial notice; full per-file native presentation
is not established. A corrupt private database instead reports unavailable
storage. None of these generated-fixture checks accesses personal data or proves
native focus, IME, accessibility, renderer equivalence or a filesystem sandbox.
