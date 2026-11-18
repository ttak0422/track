# Rust quality checks

The project-local `.agents/skills/rust-*` documents come from BeautifulRust;
provenance is in `.agents/skills/BeautifulRust.md`. They guide review and changes
without installing an external program or modifying global agent configuration.

Only `unsafe_op_in_unsafe_fn`, `undocumented_unsafe_blocks`, `dbg_macro` and `todo`
are initially enabled as warnings. Applying the full upstream application-adapted
table to the existing crate produced thousands of warnings, obscuring actionable
findings. It is not enabled by default and no lint-clean baseline is claimed.

After preparing the pinned dependencies with `./experiments/gpui/prepare.sh`:

```sh
cargo fmt --manifest-path experiments/gpui/Cargo.toml --check
cargo clippy --locked --manifest-path experiments/gpui/Cargo.toml --all-targets --all-features --no-deps
cargo test --locked --manifest-path experiments/gpui/Cargo.toml
```

For a broader, opt-in idiom review, add `-- -W clippy::pedantic -W clippy::unwrap_used`
to the Clippy command. Prioritize correctness, unsafe invariants and failure handling
ahead of style. Keep GPUI callback conventions, pinned dependencies, admission checks
and deliberate numeric geometry conversions unless a concrete defect is established.
Do not add `-D warnings` to CI until existing findings have been triaged.
The main Go/Swift/Nix CI remains unchanged.
