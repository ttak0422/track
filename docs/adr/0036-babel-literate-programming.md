# 0036. Babel literate programming: noweb, tangle, and environment variables

Status: Accepted

## Context

Babel (docs/spec/babel.md) executed one fenced block at a time. Composing a program out of blocks —
Org's literate-programming half — needed three deferred pieces: noweb `<<name>>` references, tangling
blocks out to files, and feeding one block's output into another. Org solves variable passing by
generating language-specific assignment code, but track's executors are arbitrary configured commands
(`TRACK_BABEL_<LANG>`), so per-language codegen would need an adapter per language and contradict the
"explicit executors" design.

## Decision

- **Variables are environment entries.** `:var k=v` headers and `--var k=v` CLI overrides are
  appended to the block process's environment (sorted key order). Environment is the one mechanism
  every executor already shares, so any language reads inputs the same way (`$k`, `os.getenv`, …).
  Consequences accepted: values are strings only, keys must be valid environment names, and a fence
  info-string value cannot contain whitespace (the info string splits on fields) — spacey values come
  from `--var`.
- **A `:var` value naming another named block resolves to that block's stored sidecar result** (value,
  else stdout, trailing newline trimmed). Dependencies are never executed automatically: a missing
  stored result is an error naming the block to `exec` first. This keeps runs explicit and
  policy-checked (`:eval` gates stay per-invocation) and defers dependency-graph execution.
- **Noweb expansion is a pure engine function** (`babel.ExpandNoweb`), opt-in per block via `:noweb`
  (`yes`, or phase-limited `eval` / `tangle`), recursive with cycle detection that reports the chain.
  A whole-line reference re-applies its indentation to every expanded line. Expansion never changes a
  block's stored identity: the sidecar keeps the body hash of the block as written.
- **Tangling is planned in the engine, written by the CLI.** `babel.TanglePlan` groups `:tangle
  <file>` blocks by target in note order (blank-line separated, noweb-expanded per phase);
  `babel.ResolveTanglePath` confines every target to the vault by lexical path check, mirroring the
  existing `:dir` policy. `:tangle yes` is rejected rather than inventing derived file naming.
- **`track babel run` is `exec` under a second name**, so the calls surface (`run --name X --var
  k=v`) adds no second code path.

## Consequences

- Any configured language gains variables, composition, and tangling with zero per-language code.
- Typed variables (tables, lists) and automatic dependency execution remain deferred; the
  environment-string contract is documented in the spec and help.
- A note can only ever write files inside its own vault, including through `..` targets.
