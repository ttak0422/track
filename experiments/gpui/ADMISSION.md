# Ordinary read admission: proposed implementation boundary

Status: design checkpoint, not an implemented launch mode or release approval.
The ordinary-vault gate stays closed. This document does not supersede the
fixture restrictions or establish UI equivalence with the shipping Track app.

## Current stopping point

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

The request format allows up to 256 KiB; the test bridge conveys small fixture
requests through one environment variable outside the manifest-hashed namespace.
That is not a production transport guarantee: OS per-string exec limits may be
smaller. A future production caller needs an explicitly bounded/cancelable
transport and authority/lifecycle review rather than adopting the test wrapper.
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
