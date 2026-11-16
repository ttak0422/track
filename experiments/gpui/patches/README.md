# Native termination hook

The pinned Apache-2.0 GPUI 0.2.2 crate is fetched from its official registry archive:

- Source: https://static.crates.io/crates/gpui/gpui-0.2.2.crate
- Archive SHA-256: `979b45cfa6ec723b6f42330915a1b3769b930d02b2d505f9697f8ca602bee707`
- Local patch SHA-256: `f57380052291410dfdf593a9065375ccf370b6bd00b9d0b5928d61878de3c719`
- Upstream license: [Apache-2.0](LICENSE-APACHE), retained in generated source.

The patch adds an opt-in `App::on_native_quit_requested` callback on macOS.
External termination is canceled, then the callback runs on the normal foreground
executor. Explicit `App::quit()` grants one-shot approval inside its queued native
call. Existing applications without registration keep their original behavior.
The original delegate and shutdown callbacks remain intact; no runtime method
replacement or global OS settings are involved.

The controller must deduplicate pending prompts, review every dirty draft and save,
and call `quit()` only after successful cleanup/approval. Direct `quit()` calls
bypass the gate and must be audited. Cancellation of system termination cancels
logout/shutdown; it does not automatically resume that system operation.

Compile checks and three pure gate tests cover opt-in, repeated cancellation,
one-shot approval and temporary callback absence. These do not prove native event
routing. Re-run the behavioral cases in `docs/testing/desktop-clean-termination.md`
against GPUI: menu/Cmd-Q, Dock Quit, window close, repeated requests, dirty Cancel,
failed/pending Save, multiple tabs, IME and owned-child reaping. The existing
WKWebView harness is not GPUI runtime evidence. Force Quit, crashes and power loss
remain separate recovery requirements.

# xattr Linux/Android compatibility

GPUI 0.2.2 requires gpui_http_client → zed-async-tar 0.5.0-zed → xattr 0.2.3.
The latter still names libc::ENOATTR on Linux/Android; libc 0.2.190 removed that
long-deprecated alias. We retain libc 0.2.191, including its removal of unsound
Linux/Android af_alg_iv trait implementations, rather than downgrade libc.

- Official source: https://static.crates.io/crates/xattr/xattr-0.2.3.crate
- Archive SHA-256: `6d1526bbe5aaeb5eb06885f4d987bcdfa5e23187055de9b83fe00156a821fabc`
- Local patch SHA-256: `702e61c877b38075793de4911eed11658d3848d54804dd7b8ab2245da05e77d0`
- Upstream license: MIT OR Apache-2.0, retained in generated source.

`xattr-enodata.patch` changes only the Linux/Android missing-attribute constant to
ENODATA, the exact value of the former libc alias. It retains the original
platform cfgs, module declarations, macOS/FreeBSD/NetBSD ENOATTR, and unsupported
fallback. It does not disable tar's xattr feature or alter extraction behavior.

`patched-source.nix` fetches the hash-pinned official archive and applies this
tracked patch. `prepare.sh` links that generated source under `.build-deps/xattr`;
Cargo's explicit `[patch.crates-io]` selects it with the real 0.2.3 package version.
The path override is visible in Cargo.lock; the archive hash lives in Nix because
Cargo path packages do not carry registry checksums. No registry cache is edited.
A build guard rejects missing/stale patch markers for both generated dependencies.

Remove this patch, its path override, preparation/guard entry, and compatibility
fixture dependencies when the pinned GPUI dependency chain adopts an upstream
xattr release that uses ENODATA on Linux/Android. Confirm libc stays current and
rerun the real filesystem and tar tests before removing the workaround.

Verification commands after preparation:

```sh
python3 experiments/gpui/test-prepare.py # stubbed nix-build; link policy only
python3 experiments/gpui/test-termination-gate.py # both patch markers + quit gate
cargo tree --manifest-path experiments/gpui/Cargo.toml --locked -i xattr
cargo test --manifest-path experiments/gpui/Cargo.toml --locked --test xattr_compat
```

The Linux-only integration fixture uses a unique OS-temp directory, never a vault.
It checks path/fd get/set/list/remove, missing attributes versus other errors, and
PAX extended-attribute extraction through the actual zed-async-tar dependency.
It deliberately fails if the test filesystem lacks xattr support. These tests do
not establish macOS/Android runtime behavior or native GUI correctness.
