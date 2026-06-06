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
```lua :name hello :results output verbatim :session repl
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

## Proposed Metadata Shape

The current metadata schema is version 1. Babel support should require a future metadata version because result storage adds new durable fields.

```yaml
version: 2
title: Example
blocks:
  hello:
    language: lua
    header_args:
      results: [output, verbatim, replace]
      session: repl
    body_hash: sha256:...
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
| `:cache` | `no` | Execution runs when requested; stored results are restored only when the body hash and metadata still match. |
| `:var` | none | No input variables are supplied. |
| `:session` | `none` | Run without a long-lived interpreter session; effectively one process per block. |
| `:dir` | note directory | Execute relative to the note file's directory unless `:dir` is set. |
| `:exports` | `code` for future export semantics | Parsed as metadata only; track has no exporter yet. |
| `:noweb` | `no` | Do not expand `<<name>>` references. |
| `:tangle` | `no` | Do not write source blocks to output files. |
| `:visible-lines` | none | Track-specific Neovim display hint. Execution and stored block bodies still use the full source. |

These defaults are intentionally close to Org Babel where practical, but track stores results outside the Markdown body and only executes blocks in response to an explicit user command.

`:results` is one header argument whose value is a sequence of tokens, not separate `type` and `handling` arguments. Org Babel commonly combines tokens such as `output replace` or `value silent`; track currently supports the listed `:results` tokens and treats omitted tokens as `output replace`.

## Support Matrix

| Org Babel syntax or feature | Markdown-compatible syntax | Initial support | Notes |
| --- | --- | --- | --- |
| Source block language | <code>```lua</code> | Yes | Treat the first info-string token as the language. |
| Language activation | Track execution adapter configuration | Yes later | Org supports many language identifiers, but evaluation requires per-language support. track should make language executors explicit. |
| Source block header arguments | <code>```lua :results output</code> | Yes | Parse `:<key> <value>` pairs after the language. Boolean flags in `:results` are handled as tokens. |
| Source block switches | Same info string if needed | No initially | Org switches are mostly export/line-number behavior; defer until track has export/render support. |
| `#+NAME: <name>` | `:name <name>` in fence info string | Yes | Needed for stable result lookup, calls, and noweb. |
| `#+HEADER:` multi-line headers | None initially | No | Markdown has no common multi-line fence metadata. Prefer single-line fence args. |
| Inline source `src_lang{body}` | Markdown inline code with an optional future extension | No | Inline evaluation complicates parsing and display. Defer. |
| `#+CALL:` named block calls | Future command/UI action against `:name` | Partial later | Keep parser model ready, but do not add new Markdown block syntax initially. |
| Inline `call_name(...)` | None initially | No | Defer with inline source. |
| Org property defaults `#+PROPERTY: header-args...` | Note-level sidecar defaults or future Markdown comments | No initially | Avoid adding Org-only syntax to Markdown body. |
| Global defaults | Track config | Yes later | Defaults such as `:results replace` can live in track config, not in each note. |
| Language-specific defaults | Track config keyed by language | Yes later | Equivalent to Org's `org-babel-default-header-args:<LANG>`. |
| Evaluation security prompt | CLI/editor confirmation policy | Yes | Default should refuse execution unless explicitly requested by user command. |
| `:eval yes` | `:eval yes` | Yes | Allows execution subject to track security policy. |
| `:eval no` / `never` | `:eval no` | Yes | Block is never executed. |
| `:eval query` | `:eval query` | Yes | Frontend asks before execution. CLI should require an explicit confirmation flag. |
| `:eval no-export` / `never-export` | Parsed, no effect initially | Later | Export is not currently a track feature. |
| `:eval query-export` | Parsed, no effect initially | Later | Export is not currently a track feature. |
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
| `:cache yes/no` | `:cache yes` | Yes | Use body hash plus normalized header args and variable refs. |
| `:var name=value` literals | `:var x=1` | Yes | Support strings, numbers, booleans as normalized metadata inputs. |
| `:var name=table` Org table refs | Same token | No initially | track Markdown does not define named tables yet. |
| `:var name=block(args)` | Same token | Later | Requires named block dependency execution. |
| `:colnames yes/no/nil` | Same token | Later | Only meaningful once table variables/results are supported. |
| `:rownames yes/no` | Same token | Later | Only meaningful once table variables/results are supported. |
| `:hlines yes/no` | Same token | Later | Only meaningful once table variables/results are supported. |
| `:session none` | `:session none` | Yes | Default: one process per block. |
| `:session <name>` | `:session repl` | Later | Requires long-lived interpreter lifecycle per language/session. |
| `:dir <path>` | `:dir ./scripts` | Yes with restrictions | Resolve relative to note directory or vault; deny paths outside allowed roots unless explicitly configured. |
| `:mkdirp yes/no` | Same token | Later | Useful for `:dir`, `:tangle`, and file results. |
| `:prologue` / `:epilogue` | Same token | Later | Requires careful quoting in fence info strings. |
| `:post block(...)` | Same token | Later | Requires named block calls and result piping. |
| `:exports code/results/both/none` | Same token | Parsed only | track has no exporter yet; keep for future compatibility. |
| `:noweb no` | `:noweb no` | Yes | Default: do not expand `<<...>>`. |
| `:noweb yes` | `:noweb yes` | Later | Requires named block registry and expansion before execution. |
| `:noweb tangle` / `eval` / export variants | Same token | Later | Implement after separate evaluate/tangle/export phases exist. |
| `:noweb-ref <name>` | Same token | Later | Allows multiple blocks to share one noweb reference. |
| `:tangle no` | `:tangle no` | Yes | Default: no file output. |
| `:tangle yes` | `:tangle yes` | Later | Requires safe output naming and write policy. |
| `:tangle <filename>` | Same token | Later | Filename must be constrained to vault or configured output roots. |
| `:comments no/link/org/both/noweb` | Same token | Later | Only meaningful with tangling. |
| `:padline yes/no` | Same token | Later | Only meaningful with tangling. |
| `:shebang <string>` | Same token | Later | Only meaningful with tangling. |
| `:tangle-mode <mode>` | Same token | Later | Only meaningful with tangling; needs permission validation. |
| `:no-expand` | Same token | Later | Only meaningful when tangling/noweb expansion exists. |
| `:file`, `:output-dir`, `:file-ext`, `:file-desc`, `:file-mode`, `:sep` | Same tokens | Later | File artifact handling should be designed as a separate storage policy. |
| Track source display | `:visible-lines 4-5,8` | Yes | Track-specific editor display hint. Org Babel has no generic header for showing only selected source lines; Obsidian has similar behavior through plugins such as Codeblock Customizer rather than a Markdown standard. |

## Initial Implementation Set

Start with execution of ordinary fenced code blocks:

- Parse fenced code blocks in Markdown notes.
- Read language from the first info-string token.
- Read block args from Org-style `:<key> <value>` tokens in the rest of the info string.
- Support `:name`, `:results output`, `:results verbatim`, `:results replace`, `:results silent`, `:results none`, `:results discard`, `:eval yes/no/query`, `:cache yes/no`, `:var` literal values, `:session none`, `:dir`, `:exports` as parsed metadata, `:noweb no`, `:tangle no`, and `:visible-lines` as an editor-only display hint.
- Complete configured languages, supported header keys, and fixed header values in fence info strings through LSP completion; `:` starts header-key completion, and accepted header keys insert one trailing space before value completion.
- Store execution result metadata outside the Markdown body.
- Keep stdout, stderr, exit code, wall-clock timestamps, status, body hash, and normalized header args.
- Do not mutate the note body with `#+RESULTS:`-style blocks.
- Editor integrations should execute current buffer contents, including unsaved edits, matching Emacs Org Babel. Plain CLI execution reads the saved file unless the caller explicitly supplies the body.

## Deferred Work

Defer features that require a richer document model or file artifact policy:

- Inline source and inline calls.
- Named block calls and dependency graphs.
- Table/list typed variables and result coercion.
- Sessions.
- Noweb expansion.
- Tangling.
- File/graphics results.
- Export behavior.
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
