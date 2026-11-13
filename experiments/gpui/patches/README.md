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
