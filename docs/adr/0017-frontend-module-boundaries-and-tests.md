# ADR 0017: Frontend Module Boundaries and a Test Net

## Status

Accepted

## Context

The web UI grew fast, often by adding to whatever file already touched the
feature being changed. `MarkdownView.tsx` reached ~1270 lines holding a dozen
unrelated concerns (react-markdown wiring, wiki-link previews, draggable/
resizable preview windows, link/embed routing, a syntax-highlight tokenizer,
clipboard, geometry). The Go side had the same shape: `internal/cli/commands.go`
(~1100 lines), `internal/track/webui/webui.go` (~750), and
`internal/track/lsp/link_features.go` (~655) each bundled many handlers.

Two problems followed. First, a change for one feature sat next to unrelated
code, so edits — especially work delegated to another agent — kept breaking
adjacent behavior. Second, the frontend had **no test runner at all**: `npm run
build` only type-checked, so regressions in pure logic (URL parsing, the
tokenizer, preview geometry) and in rendering shipped silently. The Go packages,
by contrast, were well covered, which is why the breakages clustered in the web
UI.

## Decision

Pay down the accumulation in a fixed order — **extract pure logic, pin it with
tests, then split the UI** — and keep that as the standing pattern for future
growth.

1. **A frontend test net is mandatory.** `web` runs `vitest` (jsdom
   environment); pure helpers are unit-tested and interactive components are
   tested with React Testing Library. New non-trivial logic ships with a test.

2. **Pure logic lives apart from React and is unit-tested.** Tokenizing,
   URL/href classification, and preview-window geometry are framework-free
   modules (`markdown/highlight.ts`, `markdown/urls.ts`, `preview/bounds.ts`)
   with colocated `*.test.ts`.

3. **One concern per file.** Components are split by what they render
   (`markdown/CodeBlock.tsx`, `ExternalLink.tsx`, `Embed.tsx`,
   `preview/WikiLink.tsx`, `WikiPreview.tsx`); shared contexts, remark/rehype
   plugins, clipboard, and preview stacking live in their own small modules.
   `MarkdownView.tsx` is reduced to wiring react-markdown to these. The public
   import path (`components/MarkdownView`) is preserved so callers are
   unaffected.

4. **The same per-concern split applies to Go god-files**, following each
   package's existing one-file-per-feature convention: `commands.go` →
   `admin/note/search_commands.go` + `flags.go`; `webui.go` → `handlers_*.go` +
   `follow.go` + `helpers.go`; `link_features.go` → `link_features/completion/
   code_actions.go`. These are pure file moves within a package — no API change.

Refactors of this kind are behaviour-preserving: they are validated by `npm run
build`, `npm test`, `go build/vet/test ./...`, and `nix build .#track-cli`, not
by changing what the code does.

## Consequences

- Regressions in the previously fragile areas now fail loudly (a failed test or
  a type error) instead of reaching the UI, which is the main goal.
- Adding a `vitest`/jsdom (and RTL) toolchain enlarges the npm dependency set;
  `flake.nix`'s `npmDepsHash` must be updated whenever dependencies change, and
  the nix flake only sees git-tracked files, so new files must be staged before
  `nix build` will include them.
- The `WikiPreview` → `MarkdownView` recursion is a runtime-only import cycle,
  which React handles; module boundaries must keep such cycles out of
  top-level (module-initialization) code.
- Remaining mid-size files (`config.go`, `lsp/server.go`, `GraphCanvas.tsx`,
  `NoteReader.tsx`) are acceptable for now; split them only when a concern
  genuinely diverges, not for line count alone.
