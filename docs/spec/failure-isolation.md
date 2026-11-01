# Failure isolation and continued use

Track separates prerequisites from optional work. A recoverable failure must not discard verified
content or stop independent requests. Degraded operation is explicit; it is not a successful empty
read, a confirmed deletion, or permission to write uncertain data.

| Phase | Required dependency | Recoverable boundary and continuing behavior |
| --- | --- | --- |
| Configuration | Valid machine/vault configuration and a trustworthy root | Invalid configuration is an actionable failure, never default writable settings. A malformed/unreadable individual title sidecar is skipped with diagnostics, including on first startup. |
| Identity | A verified namespace for explicit-ID resolution and title-changing mutations | Startup/read availability does not certify unknown titles. Explicit target ambiguity checks and creation/rename preflights stay strict. |
| Index initialization/refresh | A usable database | Readable files publish together with links and partial state. Failed files and uncertain missing entries retain existing cache data. Empty-cache reads report partial results without claiming cached data exists. |
| Individual note | Verified body and identity | Backlinks, properties and hierarchy are optional sections. Their failure leaves the body readable, marks the response partial/read-only, and logs diagnostics. Failure to persist diagnostics cannot discard that verified body. |
| Preview preparation | Successful sanitization before active Markdown/task controls | Initial failure shows escaped source with an explicit retry. A retained render cannot authorize task or diagram writes unless its body, note identity and current query match. |
| Graph/math/chart | The corresponding optional renderer | Local boundaries and handled loader promises preserve sibling content and navigation. Retry is explicit; rejected shared loaders are not permanent session failures. |
| Browser startup | The application bundle for interactive features | Failed preparation promises are handled independently. A boot-chunk failure preserves first-paint content and offers a reload action rather than an unhandled blank page. |
| API transport | Valid JSON and essential endpoint fields | Malformed success is a local query/mutation failure. A missing save acknowledgement is uncertain, not successful, and is never automatically retried. |

## State, diagnostics and retry

`read_state` identifies partial results, cached entries and the last verification attempt. A check
timestamp is not a fresh complete snapshot. Missing sidecars that were previously indexed cannot
be offered as blank editable metadata. Read-triggered scans never confirm deletions; explicit
`reindex --prune-missing` validates its scanned baseline and expected directories before pruning.

Unchanged file failures retry at most once per 30 seconds during normal refresh, while observed
changes trigger an immediate attempt. Explicit reindex retries now. Client reads permit at most
one automatic retry and do not retry deterministic HTTP 4xx errors. Preview preparation and
mutations have no automatic retry. A failed first note load does not record a seen/read milestone.

File warnings retain at most 1,024 diagnostic identities and emit at most 128 warnings per minute,
with a suppression summary for larger bursts. Client optional-feature diagnostics retain at most
128 distinct identities. Repeated failures remain visible locally without repeated intrusive
notifications; logs retain the diagnostic cause.

## Deliberate limits

Nonblocking admission rejects pipes/devices and malformed text before parsing. It does not promise
that every regular-file filesystem operation, cloud-provider request, or initialization lock can
be cancelled by a context or deadline. Hard containment of those operations needs an owned worker
boundary and a separate design; cancelling an uncertain mutation must never lead to automatic
repetition. File enumeration is not an atomic snapshot against arbitrary external writers.

Critical configuration, root and database failures remain honest failures. The native startup
failure view and browser's last-good/read-only state must not imply that the underlying storage
is healthy. Recoverable sidecar or optional renderer failures are not evidence of such corruption.
