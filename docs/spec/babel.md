# Babel Support Matrix

This document maps Org Babel features to track's Markdown-first note format.
The goal is to keep ordinary Markdown fenced code blocks as the authoring syntax while supporting the useful execution, dependency, and literate-programming behavior from Org Babel.

Org Babel reference points:

- Source blocks are normally written as `#+NAME:` plus `#+BEGIN_SRC <language> <switches> <header arguments>` and `#+END_SRC`.
- Inline source blocks use `src_<language>{<body>}` or `src_<language>[<header arguments>]{<body>}`.
- Header arguments can be set globally, through Org properties, on a block, or in function calls; more local settings win.
- Evaluation results are normally inserted after the block under `#+RESULTS:`.
- Named blocks can be called through `#+CALL:` or inline `call_<name>(...)`.
- Noweb references use `<<name>>` and can expand during evaluation, export, or tangling depending on `:noweb`.
- Tangling writes source blocks to files when enabled by `:tangle`.

## Markdown Syntax

The baseline source block is a normal fenced code block:

````markdown
```lua
print(1)
```
````

Block-level Babel options should be carried in the fence info string after the language, preserving a valid Markdown fenced code block:

````markdown
```lua :name hello :results output verbatim :session none
print(1)
```
````

Execution results should not be inserted into the Markdown body. They belong in the note sidecar metadata under `.track/notes/<id>.yaml`, keyed by a stable block identity.

In Neovim, results render as virtual lines just below the block's closing fence, so the buffer text is unchanged and multi-line output is shown without editing the note. The stored result persists in the sidecar across sessions.

Source display can also be narrowed without changing execution. `:visible-lines` is a track-specific, editor-only header argument that hides source block body lines outside the listed 1-based line ranges:

````markdown
```c :visible-lines 4-5
#include <stdio.h>

int main(void) {
    printf("hello\n");
    return 0;
}
```
````

The block above still executes with the full C source. The Neovim frontend only conceals body lines not listed by `:visible-lines`; fence lines remain visible, and the cursor row is revealed for editing. Supported range syntax is a comma-separated list such as `4`, `4-5`, or `4-5,8`.

## Stored Metadata Shape

Babel result storage was introduced in sidecar v2. New runs carrying input and execution hashes use v12; older results remain readable as metadata but must be rerun before restoration or reuse.

```yaml
version: 12
title: Example
blocks:
  hello:
    language: lua
    header_args:
      results: [output, verbatim, replace]
      session: none
    body_hash: sha256:...
    input_hash: sha256:...
    execution_key: sha256:...
    last_run:
      started_at: "2026-05-30T12:00:00Z"
      finished_at: "2026-05-30T12:00:00Z"
      status: success
      exit_code: 0
      stdout: "1\n"
      stderr: ""
      value: null
      files: []
```

Unnamed blocks need generated stable ids derived from note id, block ordinal, language, and body hash.
Named blocks should use `:name` as the result key and should fail validation if names are duplicated in one note, matching Org's requirement that source block names be unique.

## Header Argument Defaults

When a source block omits a Babel header argument, track uses these defaults:

| Header | Default when omitted | Current behavior |
| --- | --- | --- |
| `:name` | none | Unnamed blocks get a generated result id from note id, ordinal, language, and body hash. |
| `:eval` | `yes` | A user-invoked execution command may run the block. `:eval query` requires confirmation and `:eval no` refuses execution. |
| `:results` | `output replace` | `:results` accepts multiple tokens. The current default captures stdout, stderr, and exit status, then stores only the latest result for the block in the sidecar metadata. |
| `:cache` | `no` | `yes` reuses only a successful result with matching document inputs, executor configuration, and resolved working directory. |
| `:var` | none | No input variables are supplied. Declared variables reach the block as process environment entries; a value naming another block in the note resolves to that block's stored result. |
| `:session` | `none` | Run without a long-lived interpreter session; effectively one process per block. |
| `:dir` | note directory | Execute relative to the note file's directory unless `:dir` is set. |
| `:exports` | `code` | Markdown export includes code/results/both/none without execution. Web/static currently preserve source and do not apply this setting. |
| `:noweb` | `no` | Do not expand `<<name>>` references. |
| `:tangle` | `no` | Do not write source blocks to output files. |
| `:visible-lines` | none | Track-specific Neovim display hint. Execution and stored block bodies still use the full source. |

These defaults are intentionally close to Org Babel where practical, but track stores results outside the Markdown body and only executes blocks in response to an explicit user command.

`:results` is one header argument whose value is a sequence of tokens, not separate `type` and `handling` arguments. Org Babel commonly combines tokens such as `output replace` or `value silent`; track currently supports the listed `:results` tokens and treats omitted tokens as `output replace`.

## Support Matrix

| Org Babel syntax or feature | Markdown-compatible syntax | Current support | Notes |
| --- | --- | --- | --- |
| Source block language | <code>```lua</code> | Yes | Treat the first info-string token as the language. |
| Language activation | `TRACK_BABEL_<LANG>` environment configuration | Yes | Execution requires an explicitly configured command; tangling and preview need no executor. |
| Source block header arguments | <code>```lua :results output</code> | Yes | Parse `:<key> <value>` pairs after the language. Boolean flags in `:results` are handled as tokens. |
| Source block switches | Same info string if needed | No initially | Org switches are mostly export/line-number behavior; switch syntax and rendering remain deferred. |
| `#+NAME: <name>` | `:name <name>` in fence info string | Yes | Needed for stable result lookup, calls, and noweb. |
| `#+HEADER:` multi-line headers | None initially | No | Markdown has no common multi-line fence metadata. Prefer single-line fence args. |
| Inline source `src_lang{body}` | Markdown inline code with an optional future extension | No | Inline evaluation complicates parsing and display. Defer. |
| `#+CALL:` named block calls | `track babel run --name <n> --var k=v` | Yes (CLI) | Calls a named block with parameters through the CLI; no Markdown call syntax is added. |
| Inline `call_name(...)` | None initially | No | Defer with inline source. |
| Org property defaults `#+PROPERTY: header-args...` | Note-level sidecar defaults or future Markdown comments | No initially | Avoid adding Org-only syntax to Markdown body. |
| Global defaults | Track config | Yes later | Defaults such as `:results replace` can live in track config, not in each note. |
| Language-specific defaults | Track config keyed by language | Yes later | Equivalent to Org's `org-babel-default-header-args:<LANG>`. |
| Evaluation security prompt | CLI/editor confirmation policy | Yes | Default should refuse execution unless explicitly requested by user command. |
| `:eval yes` | `:eval yes` | Yes | Allows execution subject to track security policy. |
| `:eval no` / `never` | `:eval no` | Yes | Block is never executed. |
| `:eval query` | `:eval query` | Yes | Frontend asks before execution. CLI should require an explicit confirmation flag. |
| `:eval no-export` / `never-export` | Parsed, no effect initially | Later | Export never executes blocks; these values have no extra effect during explicit execution. |
| `:eval query-export` | Parsed, no effect initially | Later | Export never executes blocks; these values have no extra effect during explicit execution. |
| `:results value` | `:results value` | Later | Requires language-specific value capture. Start with stdout-oriented `output`. |
| `:results output` | `:results output` | Yes | Capture stdout/stderr/exit status in metadata. |
| `:results table` / `vector` | `:results table` | Later | Requires table serialization and type conversion. |
| `:results list` | `:results list` | Later | Requires list serialization. |
| `:results scalar` / `verbatim` | `:results verbatim` | Yes | Store raw text; display can choose not to coerce. |
| `:results file` | `:results file :file out.png` | Later | Needs safe artifact path policy under `.track/` or vault attachments. |
| `:results raw` | `:results raw` | Metadata only | Store raw result, but do not inject into Markdown body. |
| `:results code` | `:results code` | Metadata only | Store result plus desired render format. |
| `:results drawer` | `:results drawer` | Not applicable | Org drawer insertion is replaced by sidecar metadata. |
| `:results html` / `latex` / `org` | Same tokens | Metadata only | Store format marker; export/render support can come later. |
| `:results link` / `graphics` | Same tokens | Later | Depends on file result support. |
| `:results pp` | `:results pp` | Later | Requires language-specific pretty-printing. |
| `:results replace` | `:results replace` | Yes | Replace last stored result for the block in metadata. |
| `:results silent` | `:results silent` | Yes | Execute but do not update stored result; still return transient command output if requested. |
| `:results none` | `:results none` | Yes | Execute without storing or displaying result. |
| `:results discard` | `:results discard` | Yes | Execute and ignore result completely. |
| `:results append` / `prepend` | Same tokens | Later | Metadata can keep result history, but initial support should store only the latest result. |
| `:cache yes/no` | `:cache yes` | Yes | Match normalized headers, expanded source, resolved variables, executor command/args, and resolved working directory. Only successful runs are reused, after checking `:eval`. |
| `:var name=value` literals | `:var x=1` | Yes | Injected into the block's process environment as `x=1`; every value is a string (numbers arrive as decimal text). Keys must be valid environment names, and fence-info values cannot contain whitespace — use `--var` for those. |
| `:var name=table` Org table refs | Same token | No initially | track Markdown does not define named tables yet. |
| `:var name=block(args)` | `:var x=<block-name>` (no arguments) | Partial | A value naming another named block feeds that block's stored result (value, else stdout). The dependency is never executed automatically; a missing stored result is an error naming the block to `exec` first. |
| `:colnames yes/no/nil` | Same token | Later | Only meaningful once table variables/results are supported. |
| `:rownames yes/no` | Same token | Later | Only meaningful once table variables/results are supported. |
| `:hlines yes/no` | Same token | Later | Only meaningful once table variables/results are supported. |
| `:session none` | `:session none` | Yes | Default: one process per block. |
| `:session <name>` | `:session repl` | Later | Requires long-lived interpreter lifecycle per language/session. |
| `:dir <path>` | `:dir ./scripts` | Yes with restrictions | Resolve relative to note directory or vault; deny paths outside allowed roots unless explicitly configured. |
| `:mkdirp yes/no` | Same token | Later | Tangle always creates missing parent directories inside the vault, so a toggle is not needed yet; `:dir` and file results may still want it. |
| `:prologue` / `:epilogue` | Same token | Later | Requires careful quoting in fence info strings. |
| `:post block(...)` | Same token | Later | Requires named block calls and result piping. |
| `:exports code/results/both/none` | Same token | Partial | Implemented by Markdown export. Web/static preserve source without filtering or stored results. |
| `:noweb no` | `:noweb no` | Yes | Default: do not expand `<<...>>`. |
| `:noweb yes` | `:noweb yes` | Yes | Expands `<<name>>` recursively against the note's named blocks before execution and before tangling. A whole-line reference keeps its indentation; unresolved references and cycles are errors naming the chain. |
| `:noweb tangle` / `eval` | Same tokens | Yes | Expand only in that phase (tangling or evaluation). |
| `:noweb` export variants | Same tokens | Later | Export-phase noweb variants remain unimplemented; Markdown export currently preserves the source body. |
| `:noweb-ref <name>` | Same token | Later | Allows multiple blocks to share one noweb reference. |
| `:tangle no` | `:tangle no` | Yes | Default: no file output. |
| `:tangle yes` | `:tangle yes` | No | Rejected with an error: track has no derived output naming, so a tangled block must name its file. |
| `:tangle <filename>` | Same token | Yes | `track babel tangle` resolves the target against the note's directory, refuses paths outside the vault, creates missing parent directories, and concatenates same-target blocks in note order separated by a blank line. `--dry-run` prints the plan without writing. |
| `:comments no/link/org/both/noweb` | Same token | Later | Only meaningful with tangling. |
| `:padline yes/no` | Same token | Later | Only meaningful with tangling. |
| `:shebang <string>` | Same token | Later | Only meaningful with tangling. |
| `:tangle-mode <mode>` | Same token | Later | Only meaningful with tangling; needs permission validation. |
| `:no-expand` | Same token | Later | Only meaningful when tangling/noweb expansion exists. |
| `:file`, `:output-dir`, `:file-ext`, `:file-desc`, `:file-mode`, `:sep` | Same tokens | Later | File artifact handling should be designed as a separate storage policy. |
| Track source display | `:visible-lines 4-5,8` | Yes | Track-specific editor display hint. Org Babel has no generic header for showing only selected source lines; Obsidian has similar behavior through plugins such as Codeblock Customizer rather than a Markdown standard. |

## Implemented Block Workflow

Ordinary fenced code blocks support:

- Parse fenced code blocks in Markdown notes.
- Read language from the first info-string token.
- Read block args from Org-style `:<key> <value>` tokens in the rest of the info string.
- Support `:name`, `:results output`, `:results verbatim`, `:results replace`, `:results silent`, `:results none`, `:results discard`, `:eval yes/no/query`, `:cache yes/no`, `:var` literal values, `:session none`, `:dir`, `:exports` in Markdown export, noweb expansion, explicit-file tangling, and `:visible-lines` as an editor-only display hint.
- Complete configured languages, supported header keys, and fixed header values in fence info strings through LSP completion; `:` starts header-key completion, and accepted header keys insert one trailing space before value completion.
- Store execution result metadata outside the Markdown body.
- Keep stdout, stderr, exit code, wall-clock timestamps, status, body hash, and normalized header args.
- Do not mutate the note body with `#+RESULTS:`-style blocks.
- Editor integrations should execute current buffer contents, including unsaved edits, matching Emacs Org Babel. Plain CLI execution reads the saved file unless the caller explicitly supplies the body.

## Literate Programming Commands

Beyond per-block execution, the CLI supports the noweb/tangle/call trio:

- `track babel run --name <n> (--id N | --path P) [--var k=v ...]` calls a named block with
  parameters. `run` and `exec` are one command under two names; `--var` overrides a block `:var` of
  the same key. Resolved variables are appended to the process environment in sorted key order.
- `track babel tangle (--id N | --path P) [--dry-run]` writes every block carrying `:tangle <file>`
  out to disk and prints the plan as JSON (`targets` with `path`, `blocks`, `bytes`). Dry-run plans
  without writing.
- Noweb expansion happens inside the engine (`babel.ExpandNoweb`) and is applied before execution and
  before tangling according to each block's `:noweb` header. Expansion never changes a block's stored
  identity: the sidecar keeps the body hash of the block as written, so `babel restore` still matches
  the file on disk.

## Result Validity and Preview

`exec` and `run` accept `--dry-run` to return the expanded body, resolved variables, working directory,
and evaluation policy without running a process or storing a result. Preview works for `:eval no`
and `:eval query` without confirmation and without a configured language executor. References still
need valid stored results. `restore --body-stdin` validates and locates results against an editor's
current buffer, including unsaved changes.

Every stored run has an `input_hash` covering its language, all parsed headers, noweb-expanded body,
and resolved string variables. Restore, Markdown export, and variable references share this check;
changed dependencies, header edits, and CLI overrides that differ from the written defaults prevent
stale results from being presented as current. Failed runs may be displayed, but cannot supply a
variable reference or a cache hit. Legacy records without hashes require a rerun.

An `execution_key` additionally covers the configured executor command/arguments and resolved working
directory. `:cache yes` reuses only successful matching runs. Cache is opt-in: ambient environment,
external files, and interpreter binary contents are not tracked, so blocks depending on them should
keep `:cache no`. The note body and raw block identity are unchanged by expansion.

Tangle validates every destination before any output is written. Paths stay inside the resolved vault,
including through symlinks; `.track/` and files directly managed under `note/`, `journal/`, and
`template/` are protected. Different path spellings resolving to the same output are rejected instead
of overwriting one another. Equal literal targets still concatenate in document order. Validation is
not a multi-file filesystem transaction: an I/O failure during writing can leave earlier outputs updated.

For a complete note-to-files and note-to-website example, see
[`examples/literate-dotfiles`](../../examples/literate-dotfiles/README.md). Babel remains generic:
package realization, home-directory deployment, and Nix activation belong to a separate integration.

## Deferred Work

Rows marked Later/No/Metadata only are not execution capabilities. The parser retains unknown headers, but the runner does not implement them; do not rely on named sessions, value capture, or append/prepend being honored. Remaining design groups are:

- Global/language/note defaults and quoted header values, including prologue/epilogue.
- Tangle controls: noweb-ref, padline, shebang, tangle-mode, comments, no-expand, and export variants.
- Result history and format-specific rendering.
- Richer document/result models and artifact policy:

- Inline source and inline calls.
- Automatic dependency-graph execution (a `:var` block reference reads the stored result and never
  runs the dependency).
- Table/list typed variables and result coercion (variables are environment strings).
- Sessions.
- File/graphics results.
- Web/static `:exports` and stored-result integration; export-phase noweb expansion.
- Org property drawer compatibility.

## Source References

- Org Manual: Structure of Code Blocks, https://orgmode.org/manual/Structure-of-Code-Blocks.html
- Org Manual: Using Header Arguments, https://orgmode.org/manual/Using-Header-Arguments.html
- Org Manual: Environment of a Code Block, https://orgmode.org/manual/Environment-of-a-Code-Block.html
- Org Manual: Evaluating Code Blocks, https://orgmode.org/manual/Evaluating-Code-Blocks.html
- Org Manual: Results of Evaluation, https://orgmode.org/manual/Results-of-Evaluation.html
- Org Manual: Exporting Code Blocks, https://orgmode.org/manual/Exporting-Code-Blocks.html
- Org Manual: Extracting Source Code, https://orgmode.org/manual/Extracting-Source-Code.html
- Org Manual: Noweb Reference Syntax, https://orgmode.org/manual/Noweb-Reference-Syntax.html
- Org Worg: Babel Languages, https://orgmode.org/worg/org-contrib/babel/languages/index.html
