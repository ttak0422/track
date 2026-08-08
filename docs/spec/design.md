# Web Design Master

The reference for styling web UI (`web/src/styles.css`). When adding or
restyling a control, pick exactly one variant below and follow its recipe. If
none fits, extend this document first — do not invent a one-off treatment.

## Principles

- **Two grounds, one sheet.** The page (`--bg`) carries the rail; the sheet
  (`--panel`) carries the tab strip and the note. One hairline rules them
  apart. Nothing else in the permanent chrome draws a slab, a box, or a fill:
  the reader is one sheet of paper with a margin of tools beside it.
- **Color belongs to figures.** Inside a chart, a diagram, or the graph, color
  carries meaning (series, zones, event lines). So the UI around them gives it
  up: chrome, links, and active states are ink and hairlines. `--mark` is the
  single salient, and today it is ink — the one place a brand color could ever
  land.
- **Hierarchy by space and rule, not size.** Three type sizes in the whole
  reader (body, title, meta) plus the small-caps label. Sections are told
  apart by their leading and by a rule above them, never by a fourth size.
- **Two measures.** Prose reads at `--measure` (40em ≈ 42–48 Japanese
  characters); figures, tables, and code blocks run the full column. The
  difference is what makes a figure read as a figure.
- **A box is earned.** A border or fill exists only to separate a control from
  content beneath it (quiet chip) or to lift a genuinely floating layer
  (floating layer). Shadows belong to floating layers alone.
- **Tokens only, defined once.** Every color and corner radius comes from the
  custom properties at the top of `web/src/styles.css`; components never
  hardcode hex values or raw radii. Font sizes in chrome scale via
  `calc(...px * var(--font-scale, 1))`.
- **No hover-triggered popups on visible content.** Previews and expansions
  open from an explicit affordance (a button, a click), never from merely
  hovering something already readable. Hover may *reveal controls* (quiet
  chips fading in over media) — it must not *open surfaces*.

## Tokens

Ten carry the whole interface. Light and dark are the same design with the
values swapped; nothing else branches on theme.

| Token | Role | Light | Dark |
| --- | --- | --- | --- |
| `--bg` | Page ground: behind the sheet, under the rail | `#fbfaf8` | `#141618` |
| `--panel` | The sheet: tab strip, reader, floating layers | `#ffffff` | `#191c1e` |
| `--panel-soft` | Sunk ground: a figure's bed, code blocks, inputs | `#f3f2ee` | `#212528` |
| `--text` | Body ink | `#1a1a18` | `#e9e9e4` |
| `--muted` | Secondary ink: chrome at rest, inline code, table cells | `#5e5d58` | `#a2a29b` |
| `--faint` | Tertiary ink: meta, labels, captions' sources, figure numbers | `#6f6e68` | `#8b8b83` |
| `--line` | Hairline | `#e6e4de` | `#282c2f` |
| `--line-strong` | Stated rule: a figure's gutter, link underlines, a table's header rule | `#c7c5bd` | `#3e4347` |
| `--line-node` | Graph node outlines | `#8e8c84` | `#6e7478` |
| `--mark` | The salient: logo, active tab, the graph's centre node | `#1a1a18` | `#e9e9e4` |

`--mark` equals `--text` by value and differs by role: it is the one token a
brand color would replace, so it never shares a declaration with body ink.

The three inks are a contrast ladder, not a fade: `--faint` is the quietest
step that still clears WCAG AA (4.5:1) on both the sheet and the sunk ground,
because everything it carries — labels, meta, figure numbers, a caption's
source — is text someone has to read. A quieter tertiary is not available; if
something needs to recede further than `--faint`, it is a rule or a shape, not
smaller greyer type.

Figures keep their own palette, and it is the only colored thing on the page:
`--chart-1..6` and `--chart-ramp-*` (series and heatmap ramp, read by
`echartsTheme.ts`), plus `--danger` for destructive intent and unresolved
links. A control in the chrome never reaches into this palette — and neither
does the graph, whose emphasis is ink against neighbours that dim.

Non-color tokens: `--font-mono` (the one mono stack: code, the editor, section
labels), `--measure` (the prose column), `--radius-sm` (4px, badges and inline
chips), `--radius` (6px, controls and inputs), `--radius-lg` (8px, panels and
floating layers). Those three are the whole radius scale — a control that
wants a corner takes one of them, not a new number. Drawn shapes are not on
the scale and keep their own geometry: icon glyphs, heatmap cells, pills
(`999px`), circles (`50%`).

### One definition per value

Each token is declared **once**, as `light-dark(<light>, <dark>)` on `:root`.
The manual override works through `color-scheme` (`:root[data-theme="dark"]`
sets it to `dark`, `[data-theme="light"]` to `light`), so there is no second
copy of the dark palette to drift out of step with the first.

Every color token is registered with `@property … syntax: "<color>"`. That is
not decoration: `getComputedStyle().getPropertyValue()` on an *unregistered*
property hands back the literal text `light-dark(#fbfaf8, #141618)`, and the
ECharts theme, the Mermaid config, and the graph canvas all read their colors
that way. Registered, the same call returns the resolved `rgb(...)` for
whichever mode is live. A new color token is registered with the rest or it
breaks those three surfaces silently.

## Type

Three sizes, and no fourth.

| Role | Size | Weight |
| --- | --- | --- |
| Body, `h2`, `h3` | 16px / 1.85 | 400, headings 700 |
| Note title (`h1`) | 26px / 1.4 | 500 |
| Meta, captions, tables, the aside, tabs | 13–14px | 400 |
| `.label` (section label) | 11px, `.12em`, uppercase, `--faint` | 500 |

`h1` stays at 1.6× the body: note titles run long (`20260805 表現力ベンチ
ゴールデータでの再現`), and a larger one simply folds to two lines.

`h2` and `h3` are the same size as body text. Their hierarchy is space and a
rule: `h2` takes 44px of lead and a hairline above it, `h3` takes 26px and
nothing else. Paragraphs and lists lead with 13px, list items with 7px.

## The reading surface

- The reader column is `--content-width` (880px by default, and a setting).
  Figures, tables, and code blocks fill it.
- Prose — paragraphs, lists, headings, the title, the meta strip — is capped
  at `--measure` inside that column. The cap lands on `.markdown-view > *`,
  and the block-level elements that bleed opt out by name.
- Body copy carries no color and no background. Links are ink with a
  `--line-strong` underline (see variant 8); inline code is mono and `--muted`
  with no chip, because a filled chip in a Japanese line makes the line ripple.

## Variants

### 1. Text control — the default for chrome

Anything sitting directly on the app surface: toggles, segmented modes, nav,
tags. Plain text, no border, no fill, no radius.

- Rest: `color: var(--muted)`.
- Hover / `:focus-visible`: `color: var(--text)`.
- Active/selected: `color: var(--text)` plus `font-weight: 500`, or `--mark`
  where the state is a mode being *on* (the note's display mode, follow).
- Canonical: `.rail-button`, `.graph-reset`, `.note-tags button`.
- In the icon rail the same states are carried by the glyph rather than a
  label. Navigation never takes the active treatment: it goes somewhere rather
  than turns something on.

### 2. Quiet chip — a control resting on content

Controls that sit on top of media, diagrams, or canvases and need separation
from what is beneath them.

- `background: var(--panel-soft)` (or a translucent `--panel` mix over
  images/video), `border: 1px solid var(--line)`, `border-radius: var(--radius)`.
  No shadow.
- Usually hover-revealed: `opacity: 0`, switched to `1` on the container's
  `:hover` / `:focus-within`.
- Canonical: `.mermaid-control`, `.media-control`, `.pdf-deck-nav`.

### 3. Floating layer — the only layer that floats

Menus, previews, the search popup, and modal dialogs. These legitimately sit
above the page, so they alone carry shadows.

- `background: var(--panel)`, `border: 1px solid var(--line)`, soft
  `box-shadow`. Items inside are text controls (muted rows that ink on hover).
- Canonical: `.menu-panel`, `.note-menu-panel`, `.modal-card`,
  `.tab-overflow-panel`, `.tab-tools`.
- Every member is transient. The rail is not one: it is a column of the page,
  ruled off by a hairline, and it paints no surface of its own.

### 4. Filled action — modal decisions only

Bordered/filled buttons are reserved for a modal's action row, where a
destructive choice needs weight the flat idiom cannot give.

- Neutral: hairline border, transparent fill. Destructive: `.danger-button`
  filled.
- Canonical: `.modal-actions button`, `.modal-actions .danger-button`.

### 5. Underline input — text entry

Single-line fields carry editability with a bottom hairline, not a box.

- `border: 0; border-bottom: 1px solid var(--line);` transparent background;
  focus moves the line to `--mark`.
- Canonical: `.home-hero .searchbox input`, `input.modal-input`.
- Exception: the multi-line editor textarea keeps a boxed `--panel-soft` field.

### 6. Section label — the caption naming a region

Small caps that title a chrome region or annotate content (CONTENTS,
BACKLINKS, a code block's language, an OGP card's site name).

- `font-family: var(--font-mono)`, `calc(11px * var(--font-scale, 1))`,
  `font-weight: 500`, `letter-spacing: 0.12em`, `text-transform: uppercase`,
  `color: var(--faint)`.
- One shared rule near the top of `styles.css` carries the typography; each
  site keeps only its own margins. Add new labels to that rule rather than
  restating the recipe.

### 7. Icon button — a glyph whose target is invisible

A chrome control with no label: a tab's close and float buttons, the wiki
preview's expander. It is a text control (variant 1) at rest, but its glyph is
smaller than the area that responds to a click, so it shows that area while
being aimed at.

- Rest: `color: var(--muted)`, transparent background, no border.
- Hover / `:focus-visible`: `color: var(--text)` plus
  `background: var(--panel-soft)` and `border-radius: var(--radius-sm)`. The
  fill is `--panel-soft` — a surface — never `--line`, which is a hairline.
- The hit target stays at least 24px even when the glyph is half that; the fill
  is what makes the target legible.
- Canonical: `.tab-close`, `.tab-float` (both inside the tab's own floating
  layer, see Tab strip), `.wiki-preview-toggle`.
- A glyph button resting *on content* (a preview body, a diagram, media) is a
  quiet chip (variant 2) instead: it needs separation from what is beneath it,
  not just an aiming cue.

### 8. Link — ink and an underline

Body links, wiki links, backlinks, breadcrumbs. No color: a paragraph with six
links in it turns mottled the moment they carry one.

- `color: var(--text)`, `text-decoration: underline`,
  `text-decoration-color: var(--line-strong)`, `text-underline-offset: 3px`,
  `text-decoration-thickness: 1px`. Hover moves the underline to `--muted`.
- In the aside and other chrome lists the underline goes away and the link is
  a text control (variant 1): `--muted` at rest, `--text` for the active or
  hovered row.
- Unresolved wiki links keep `--danger` with a dotted underline — that is a
  warning, not decoration. A pending one is `--muted`.

## Figure

A figure is one container: number, bed, caption, source, and reading note.
Nothing about it is a card.

```
[34px gutter] [ figure body                        ]
 │ 図 1        ┌────────────────────────────────┐
 │             │  --panel-soft bed               │
 │             └────────────────────────────────┘
 │             caption                   ADR 0068
 │             How to read: …
```

- No outer border, no rounded card. A `2px solid var(--line-strong)` rule
  stands in the left gutter and the figure number sits at its top, mono
  10.5px, `.1em`, `--faint`, `white-space: nowrap`.
- Numbers come from a CSS counter on the reader, so they are references the
  body can point at ("図 1"), not decoration.
- The body sits on `--panel-soft` with `border-radius: 3px` and 22px 20px of
  padding.
- Caption: 12px `--muted`, 11px above. Its source sits at the right end of the
  same line, mono 11px `--faint`.
- Reading note: 12.5px `--muted`, `max-width: 44em`, 9px above.
- Members: charts (`.viewspec-chart-wrap`), Mermaid, Graphviz, D2, and draw.io
  diagrams. When the Figure envelope (headline / subtitle / chart / caption /
  sources / interaction hint) is specified, it inherits this look as its
  default rather than defining a second one.

## Table

Rules run horizontally only, and only where they separate something.

- `th`: the section label recipe, `text-align: left`, no fill, one
  `--line-strong` rule beneath the header row.
- `td`: `--muted`, `vertical-align: top`, a `--line` rule beneath each row.
  The first column stays `--text` — it is what you scan.
- No column rules, no striping, no header fill. A table bleeds to the column
  like a figure.

## Tab strip

The strip is a line of titles on the sheet, not a bar of chrome.

- **Most recent first**, so the note being read is the leftmost tab and the
  strip behind it is the order you visited things in. The tab you are on is
  therefore always in the same place, and never in the overflow.
- **Every tab that fits is shown.** The count is measured, not fixed: a wide
  window with short titles keeps them all, and only what genuinely has no room
  goes to the `+N` button at the right end, which lists the rest in a floating
  layer. Each tab truncates at 210px so no single title can crowd the others
  out. The strip never scrolls sideways — a title you cannot see is in the
  menu, not off the edge.
- The active tab is marked by `border-bottom: 2px solid var(--mark)` plus
  `font-weight: 500` — never a fill or a box. Inactive tabs are `--muted` with
  no border, and a hairline under the whole strip separates it from the note.
- **A tab's popup carries what the tab has no room for.** Hovering (or focusing)
  a tab opens a floating layer (variant 3) under the strip holding its full
  title, wrapped rather than clipped, and its controls — float and close. Both
  used to happen in the tab: two 24px buttons covered the title they belong to,
  and the full title came from the browser's own `title` tooltip, which opens at
  the pointer and landed on top of those buttons. A control that has a panel of
  its own does not also carry a native tooltip. Only the unsaved-changes dot
  stays inline: it is state, not a control, and it belongs next to the title it
  describes.
- The reveal keys off `:hover` and `:has(:focus-visible)` — **not**
  `:focus-within`, which variant 2's hover-revealed chips can afford and this
  cannot: the container here also holds the tab's own title button, so a mouse
  click on the tab left the panel pinned open with the pointer long gone.

## Sidebar

The note's aside (right) and the rail (left) are both quiet columns.

- Aside: 186px wide, 60px from the note column. Section headings take the
  label recipe; a count sits at the right end of the heading row in mono 11px
  `--faint`. Rows are text controls — no pills.
- The aside's graph draws its centre node filled with `--mark`, its other
  nodes filled with `--bg` and outlined 1px in `--line-node`, and its edges in
  `--line-strong`. Hover and search highlighting are ink too: the match fills
  with `--mark` and its edges follow, while everything else dims in place.
  Contrast is what marks a match — no graph gets a colour the rest of the UI
  gave up.
- Rail: a 56px column on `--bg`, ruled off the sheet by a hairline on its
  right edge. Glyphs are text controls; views sit at the top, settings at the
  bottom. It paints no surface, casts no shadow, and takes no radius.
- **One icon family.** Every rail glyph is drawn the same way: a 24-unit
  viewBox at 20px, `fill: none`, `stroke: currentColor`, `stroke-width: 1.5`,
  round caps and joins, and **no filled shapes** — a dot is a small stroked
  circle, not a blob. They are outlines so that the one solid shape in the
  column reads as what it is.
- **The mark is a filled square** in `--mark`, at the head of the rail. It is
  the app's mark wherever a mark is drawn (the rail, the empty state's
  watermark), and it carries no color of its own: a published site that
  configures its own icon replaces it, and that image keeps its own colors.

## Adding new UI

- Reuse the canonical class when the markup allows; otherwise copy its recipe
  and name the variant in a comment (`/* quiet chip */`) so the choice is
  greppable.
- Hit targets for icon-sized controls are at least 24px (WCAG 2.2), even when
  the glyph is smaller.
- Scrollers hide their scrollbars (`scrollbar-width: none` plus the WebKit
  rule), matching the rest of the app.

## Trying a restyle

A candidate design is a token set, not a branch (ADR 0068):

- Write each candidate as a `[data-theme-variant="…"]` block in
  `web/src/dev/candidates.css` — dev-only, never bundled. Because everything
  reads tokens, charts and diagrams follow automatically.
- Preview on the dev server (`make site-dev`) at `/gallery`: the variants above
  rendered under any candidate, and all candidates side by side. Any dev URL
  accepts `?variant=…&theme=…`.
- `make design-shots` screenshots candidates × light/dark into
  `_design-shots/index.html` for a one-page comparison.
- Adopting a candidate edits **this document first** (tokens, principles), then
  `styles.css` follows. Candidates never merge as-is.
