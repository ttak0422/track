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
