# 0068: Design candidates are token sets behind a dev-only playground

## Status

Accepted (2026-08-04)

## Context

Restyling this app has so far meant committing one finished treatment and judging it after the
fact — a take-it-or-leave-it change with no way to hold two candidates next to each other before
choosing. What the workflow lacked was not iteration speed (`make site-dev` already reloads a CSS
edit in under a second) but a comparison surface: somewhere several candidate designs render side
by side, cheap enough that producing three candidates is not three branches.

Two properties of the codebase make that surface almost free. Every color and radius flows through
the `:root` custom properties in `web/src/styles.css` (docs/spec/design.md: "Tokens only"), and the
non-DOM renderers — ECharts, the graph canvas, Mermaid, D2 — resolve those same tokens through
`getComputedStyle`, so swapping token values restyles charts and diagrams along with the chrome. A
candidate design is therefore not a branch or a stylesheet fork; it is a token set.

The obvious tooling was considered and rejected. Storybook (or Ladle) is a dependency and an
upgrade treadmill for what is here a single page of canonical markup; visual-regression services
(Chromatic, Argos) are cloud products solving the *drift* problem, not the *choosing* problem; a
token pipeline (Style Dictionary) buys nothing when the only output is one CSS file. ADR 0029 (app
surfaces bundle everything) and the npm/nix coupling (ADR 0017: every devDependency moves
`npmDepsHash`) both push the same way: no new dependencies for an experimentation surface.

## Decision

- **A candidate is a `[data-theme-variant="…"]` block** in `web/src/dev/candidates.css`, overriding
  whatever tokens it changes; light/dark combos follow the same attribute cascade the base theme
  already uses. Set on `<html>` it themes the whole app; set on any element it themes one subtree
  (custom properties inherit), which is what makes side-by-side cards possible on a single page.
- **The playground is `/gallery`, registered only under `import.meta.env.DEV`** in `App.tsx`. It
  renders design.md's six control variants with their canonical classes, a token board, and a
  compare grid of every candidate at once; its switcher writes `data-theme-variant` /
  `data-theme` on the document element. Neither production build ships the route, its chunk, or
  the candidates file, and the prerender never sees the path.
- **Any dev URL selects a preview**: `?theme=dark&variant=candidate-a` is applied before first
  paint by a dev-guarded hook in `main.tsx` (`web/src/dev/preview.ts`).
- **`make design-shots` turns the comparison into files**: `scripts/design-shots.mjs` screenshots
  pages × light/dark × candidates against the running dev server via `npx playwright screenshot`
  (exact-pinned in the script, not a dependency — the lighthouse pattern) and writes
  `_design-shots/index.html`, one row per page/theme, one column per candidate. This is also the
  self-verification loop for an agent restyling anything: shoot, look, fix, re-shoot.
- **Adoption order is spec first.** Picking a candidate means editing docs/spec/design.md (tokens,
  principles) and then making `styles.css` follow, the direction design.md itself mandates for new
  variants. `candidates.css` afterwards returns to being sketches; candidates are disposable and
  never merge as-is.

## Consequences

- Producing N candidates is N CSS blocks in one working tree — no worktrees, no branches — and the
  owner picks from one page (or one screenshot sheet) instead of imagining diffs.
- The playground only proves what tokens reach. The ~25 hardcoded hex values outside the token
  blocks (danger fallbacks, lightbox, syntax highlighting) and any shape/spacing changes beyond
  the radius scale are invisible to a candidate block; a candidate that needs them grows plain CSS
  in its block, and adoption folds those back properly.
- Candidates ignore the "system" theme: the base `prefers-color-scheme` block outranks a
  candidate's light values, so the playground forces an explicit light/dark. Known, documented in
  candidates.css, acceptable for a preview surface.
- Playwright's browser download is a one-time local cost (`npx playwright install chromium`),
  taken instead of a permanent devDependency. Screenshots stay local artifacts (`_design-shots/`,
  ignored); pixel-diff regression testing remains out of scope until after a refresh lands, and
  would arrive as Playwright `toHaveScreenshot` with CI-owned baselines if ever needed.
