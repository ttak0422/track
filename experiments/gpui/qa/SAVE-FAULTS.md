# Disposable native Save fault harness

This is test-only scaffolding, not a production feature. It is an executable
`--track-bin` adapter that owns a real Track child and forwards loopback requests.
It does not replace validation, etags, storage checks, or editing opt-in in GPUI.
It rejects unmarked/out-of-root fixture paths, symlinks in the fixture, external
proxy URLs, and PUT destinations other than `/api/note`. Other mutation methods
are unsupported. No retries. It does not attach to an existing server.

## Prepare a NEW public fixture (do not reuse an active QA fixture)

From the repository root, with an already-built Go binary and GPUI binary:

```sh
export TRACK_QA_REAL_BINARY=/tmp/track-ui-reference
export TRACK_QA_ROOT="$(python3 - <<'PY'
from pathlib import Path
import shutil, tempfile
root = Path(tempfile.mkdtemp(prefix='track-save-fault-'))
source = Path('docs/help').resolve(strict=True)
assert not any(p.is_symlink() for p in source.rglob('*'))
shutil.copytree(source, root / 'vault')
(root / 'vault/.track-native-edit-fixture').write_text('disposable-fixture-only\n')
(root / 'config.yml').write_text('{}\n')
(root / 'cache').mkdir()
print(root)
PY
)"
# Launch only when ready for GUI QA. Choose a fresh adjacent public port pair.
# 24082/24083 are examples, not reserved: collision fails rather than adopting.
./experiments/gpui/target/debug/track-gpui-poc \
  --vault "$TRACK_QA_ROOT/vault" \
  --track-bin "$PWD/experiments/gpui/qa/save_fault_proxy.py" \
  --config "$TRACK_QA_ROOT/config.yml" --cache "$TRACK_QA_ROOT/cache" \
  --server-port 24082 --developer-edit-fixture "$TRACK_QA_ROOT/vault"
```

Confirm the actual CLI flag spellings against `main.rs` before using another
revision. Normal launches without the explicit fixture flag remain read-only.
The adapter additionally requires its fixture/config/cache to be under the
platform temporary directory. It reserves a separate upstream pair in
26000–29999; neither public nor upstream listener adopts a foreign process.
Public ports 19022/19062/19082 are explicitly refused to avoid known QA sessions.
The upstream starts with the same fixture and explicit config/cache, and watches
the adapter as its parent. Adapter relays only its child's exact readiness token.

## Arm one save, then operate the app

Only arm while no save is pending. These files contain test controls, never note
bodies, revisions or tokens. `events.jsonl` contains counts/status/PIDs/ports only.

Pending acknowledgement:

```sh
printf 'hold\n' > "$TRACK_QA_ROOT/arm"
tail -f "$TRACK_QA_ROOT/events.jsonl"
# In GUI: change a note, Save; immediately invoke Quit while save is pending.
# After the "held" event, acknowledge successful upstream save:
touch "$TRACK_QA_ROOT/release"
```

**Timing limit:** the real client request timeout is 3 seconds. Release before
that deadline for the acknowledged pending-save/quit case. A held ack beyond 3 s
correctly produces Uncertain, even though the upstream already committed.
For reliable GUI automation, schedule Quit and release immediately after the
`held` event, not after waiting for another screenshot round-trip. A slow manual
run does not prove the pending-success path. The harness does not lengthen the
production timeout. Its own hold stops after 30 s or shutdown.

Uncertain save, committed upstream but response lost:

```sh
printf 'drop\n' > "$TRACK_QA_ROOT/arm"
# In GUI: change a note and Save (or Save and continue in Quit prompt).
```

The adapter forwards exactly one original PUT unchanged, waits for its real
response, and disconnects only if upstream returned 200. Check UI retains draft,
shows uncertainty, cancels pending Quit/navigation and does not automatically
retry. `put_ack` then `dropped_ack` records establish one committed request.
Use the app's explicit read-only inspection to compare the saved body/etag.
Inspecting must not itself write, clear uncertain draft, or replay Quit. A
conflict/non-200 passes through unchanged rather than being disguised as a drop.
The arm is consumed on the next PUT, not GET preflights; an unsuccessful
preflight leaves it armed. Remove `arm` manually before an unrelated save.

## Shutdown and evidence

Use the app's normal guarded close/quit. Adapter SIGTERM triggers bounded
terminate/kill/wait of its own child (0.2s graceful plus 0.1s forced reap,
inside GPUI's 0.7s outer grace); `reaped` is recorded immediately after wait,
before listener cleanup. Termination handlers are installed before forwarding
child readiness.
Parent death is also detected; Go retains its parent watcher for forced adapter
termination. The second public port is held only to prevent collisions, not a
WebSocket proxy. WebSocket parity is outside this Save-only harness.

Check `events.jsonl` for exactly the expected PUT count and final `reaped`, then
check both recorded public/upstream adjacent port pairs are free. Verify only
this new fixture changed. Do not delete a fixture while its app is still live.

## Automated tests (no GUI or real vault)

```sh
python3 -m unittest discover -s experiments/gpui/qa -p 'test_save_fault_proxy.py' -v
```

Six controlled-loopback tests cover Unicode/raw request forwarding, held ack
and release, dropped ack with real readback/conflict, forbidden mutation/URL
routes, fixture rejection, and adapter readiness plus owned fake-child
termination/reap/public-port release, including a SIGTERM-resistant child
requiring SIGKILL and adapter exit within the 0.7s outer deadline. These are scaffolding tests, not actual
Go+GPUI pending/uncertain save validation. The real app still needs GUI evidence.

## Real-GUI evidence limit

The first real-GUI uncertain-save run used the older adapter whose inner 0.7s
grace competed with GPUI's outer 0.7s grace. It produced no `reaped` event.
Released ports alone do not prove child reaping; that run must not be cited as
confirmed real-child cleanup. A subsequent clean GUI launch/quit also had no `reaped` event; its cause is
unresolved. A possible inspection-triggered app relaunch is not established
as the cause. The harness is not yet confirmed reliable for GUI cleanup.

Corrected lifecycle isolation, 2026-11-14: a new public docs/help copy, real
Go binary, adapter-owned ports 24182/24183 and 26000/26001, no mutations or GUI.
Explicit adapter SIGTERM recorded `cleanup_enter` (signal 15), child termination,
and `reaped` with child status 0 in 13 ms; adapter exited 0. All four ports had no
listeners afterward. Six controlled tests also pass, including resistant-child
forced reaping within 0.7 s. This demonstrates standalone cleanup, not GUI cleanup.
Additional safe lifecycle events now record parent changes, received signals at
cleanup entry, term/kill attempts, elapsed reap time, and exception type only.
A future isolated GUI run must retain these events and avoid any inspection
that might launch the app again after exit.
Pending-success remains unverified in GUI; see the 3s timing limit above.
