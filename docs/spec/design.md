# Web Design Master

The reference for styling web UI (`web/src/styles.css`). When adding or
restyling a control, pick exactly one variant below and follow its recipe. If
none fits, extend this document first — do not invent a one-off treatment.

## Principles

- **One sheet, floating rail.** The page (`--bg`) surrounds the reader sheet
  (`--panel`), while navigation floats over the sheet as a compact dock. The
  dock uses the panel surface, a hairline, and a radius, but no shadow: it is a
  stable orientation aid, not a lifted card. The reader itself stays full width.
- **Color belongs to visualizations, and to one salient.** Inside a chart, a
  diagram, or the graph, color carries meaning (series, zones, event lines). So
  the UI around them gives it up: chrome, links, and body text are ink and
  hairlines. The one exception is `--mark`, a vermilion that says *this one* —
  the keyboard cursor, the active tab, the graph's centre node, the focus ring.
  It is the chrome's whole colour budget: there is no second accent, and nothing
  takes the vermilion merely to look alive.
- **Hierarchy by space and rule, not size.** Three type sizes in the whole
  reader (body, title, meta) plus the small-caps label. Sections are told
  apart by their leading and by a rule above them, never by a fourth size.
- **Two measures by default.** Prose reads at `--measure` (40em ≈ 42–48 Japanese
  characters); visualizations, tables, and code blocks run the full column. The
  Content width setting may widen the prose measure through `--content-measure`,
  while the default difference keeps visualizations reading as content rather than chrome.
- **A box is earned.** A border or fill exists only to separate a control from
  content beneath it (quiet chip) or to lift a genuinely floating layer
  (floating layer). Shadows belong to floating layers alone.
- **Tokens only, defined once.** Every color and corner radius comes from the
  custom properties at the top of `web/src/styles.css`; components never
  hardcode hex values or raw radii. Font sizes in chrome scale via
  `calc(...px * var(--font-scale, 1))`.
- **Hover reveals controls; it does not open surfaces.** A quiet chip fading in
  over media is hover's job. A panel, an expansion, or anything that covers what
  is being read opens from an explicit affordance instead.
  The one exception is the note preview on a resolved `[[wikilink]]`, which is
  the feature rather than an accident: the link *is* the affordance, so the
  preview answers "what is behind this" without spending a navigation. It earns
  the exception by behaving: it waits out a hover-intent delay so a pointer
  crossing a column of links opens nothing, and it opens only for a link that
  resolves — a pending or unresolved one is inert. Nothing else on a reading
  surface may take it as precedent.

## Tokens

Ten carry the whole interface. Light and dark are the same design with the
values swapped; nothing else branches on theme.

| Token | Role | Light | Dark |
| --- | --- | --- | --- |
| `--bg` | Page ground: around the sheet and behind the dock | `#fbfaf8` | `#141618` |
| `--panel` | The sheet, dock, tab strip, and floating layers | `#ffffff` | `#191c1e` |
| `--panel-soft` | Sunk ground: code blocks, inputs, and quiet controls | `#f3f2ee` | `#212528` |
| `--text` | Body ink | `#1a1a18` | `#e9e9e4` |
| `--muted` | Secondary ink: chrome at rest, inline code, table cells | `#5e5d58` | `#a2a29b` |
| `--faint` | Tertiary ink: meta, labels, and captions' sources | `#6f6e68` | `#8b8b83` |
| `--line` | Hairline | `#e6e4de` | `#282c2f` |
| `--line-strong` | Stated rule: link underlines, table headers, and scrollbar thumbs | `#c7c5bd` | `#3e4347` |
| `--line-node` | Graph node outlines | `#8e8c84` | `#6e7478` |
| `--mark` | The salient: logo, active tab, the graph's centre node | `#c13a1e` | `#f4785e` |

`--mark` is the only colored token the chrome carries, and it colors text and
fills as well as rules — the keyboard cursor's bars, a filled `.primary-button`
with a white label — so both values clear AA on every ground they land on
(5.4:1 on the sheet in light, 6.3:1 in dark). It never shares a declaration with
`--text`: re-branding the salient must not reach body ink.

The three inks are a contrast ladder, not a fade: `--faint` is the quietest
step that still clears WCAG AA (4.5:1) on both the sheet and the sunk ground,
because everything it carries — labels, meta, and caption sources —
source — is text someone has to read. A quieter tertiary is not available; if
something needs to recede further than `--faint`, it is a rule or a shape, not
smaller greyer type.

Visualizations keep their own palette — the only place on the page where several
colors meet: `--chart-1..6` and `--chart-ramp-*` (series and heatmap ramp, read
by `echartsTheme.ts`), plus `--danger` for destructive intent and unresolved
links. A control in the chrome never reaches into this palette; it has `--mark`
and nothing else. `--danger` is the near neighbour of that vermilion, so the two
are told apart by shape rather than hue: danger is a dotted underline or a
filled destructive button, the mark is a solid rule, ring, or node.

Non-color tokens: `--font-sans` (IBM Plex Sans JP for the reading surface),
`--font-mono` (the one mono stack: code, the editor, section labels),
`--measure` (the prose column), `--radius-sm` (4px, badges and inline chips),
`--radius` (6px, controls and inputs), `--radius-lg` (8px, panels and
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
  Visualizations, tables, and code blocks fill it.
- Prose — paragraphs, lists, headings, the title, the meta strip — defaults to
  `--measure` inside that column and follows the setting through
  `--content-measure`. The cap lands on `.markdown-view > *`, and the block-level
  elements that bleed opt out by name.
- Body copy carries no color and no background. Links are ink with a
  `--line-strong` underline (see variant 8); inline code is mono and `--muted`
  with no chip, because a filled chip in a Japanese line makes the line ripple.

### Scrollbars

Visible overflow uses the same quiet scrollbar everywhere: an 8px thumb,
transparent track, and a rounded `--line-strong` thumb with a transparent
inset. Hover moves the thumb to `--muted`. Scrollers whose chrome would compete
with the content — code blocks, the rail, tab overflow, and preview panes — may
keep their bars hidden.

The ordinary read-only note uses the reader's visible scrollbar; its thumb
belongs to the reading surface rather than to the surrounding workspace chrome.

### Search matches

Search results use a semantic `<mark>` around the matching text only. The mark
inherits the surrounding ink, uses `--panel-soft` with `--radius-sm`, and takes
`font-weight: 500`; the result row itself stays flat, with the keyboard cursor
remaining an inked edge.

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
- Canonical: `.media-control`, `.pdf-deck-nav`.

Diagram controls are the icon-button exception: `.mermaid-control` uses variant 7 because the diagram
already supplies the visual surface and its glyphs need no resting slab. Media and deck controls remain
quiet chips when their underlying content needs a separating surface.

### 3. Floating layer — the only layer that floats

Menus, previews, the search popup, and modal dialogs. These legitimately sit
above the page, so they alone carry shadows.

- `background: var(--panel)`, `border: 1px solid var(--line)`, soft
  `box-shadow`. Items inside are text controls (muted rows that ink on hover).
- Canonical: `.menu-panel`, `.note-menu-panel`, `.modal-card`,
  `.tab-overflow-panel`, `.tab-tools`.
- A selection action popover is this same floating-layer variant: it is anchored just
  outside an explicit text selection, never opened by hover, and its press preserves
  the selection it acts on.
- Every member is transient. The rail is the exception: it is a permanent
  floating dock with the panel surface, a hairline, and a radius, but no shadow.

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
  layer, see Tab strip), `.wiki-preview-toggle`, `.mermaid-control`.
- A glyph button resting on media or a deck remains a quiet chip when that
  content needs separation; diagram chrome is the documented exception above.

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

## Visualization blocks

Charts and diagrams are content blocks, not numbered figures or cards. They
are separated from surrounding prose by whitespace only; no generated figure
number, left gutter rule, gray bed, or outer decoration is added by the
frontend. Their own nodes, series, and annotations carry the visual hierarchy.

Interaction controls remain quiet chips on top of the content and appear on
hover or focus where possible. A control may still be permanently visible
when it is the only way to discover a collapsed or pannable visualization.

## Table

Rules run horizontally only, and only where they separate something.

- `th`: body sans, `font-weight: 500`, the author's casing and normal tracking,
  `text-align: left`, no fill, one `--line-strong` rule beneath the header row.
- `td`: `--muted`, `vertical-align: top`, a `--line` rule beneath each row.
  The first column stays `--text` — it is what you scan.
- No column rules, no striping, no header fill. A table bleeds to the column
  like a visualization.

## Tab strip

The strip is a line of titles on the sheet, not a bar of chrome.

- **Most recent first**, so the note being read is the leftmost tab and the
  strip behind it is the order you visited things in. The tab you are on is
  therefore always in the same place, and never in the overflow.
- **Every tab that fits is shown.** The count is measured, not fixed: a wide
  window with short titles keeps them all, and only what genuinely has no room
  goes to the `+N` button at the right end, which lists the rest in a floating
  layer. Every tab is the same 168px, so no title crowds the others out and the
  close button lands in the same place on each; a longer title clips to the
  frame with an ellipsis. The strip never scrolls sideways — a title you cannot
  see is in the menu, not off the edge.
- The active tab is marked by `border-bottom: 2px solid var(--mark)` plus
  `font-weight: 500` — never a fill or a box. Inactive tabs are `--muted` with
  no border, and a hairline under the whole strip separates it from the note.
- **A tab's popup carries what the tab has no room for.** Hovering (or focusing)
  a tab opens a floating layer (variant 3) under the strip: one row holding its
  full title — wrapped rather than clipped — with float beside it. The full title
  used to come from the browser's own `title` tooltip, which opens at the pointer
  and landed on top of that button; a control that has a panel of its own does
  not also carry a native tooltip.
- **Close is the exception, and stands in the tab.** Closing several tabs is one
  gesture repeated, and from the popup each repeat cost a trip down into it and
  back. It sits at the tab's right end, revealed by the tab's own hover. It is
  absolutely positioned and reserves no width: a slot kept clear on every tab
  padded the strip out, and the title it overlays is already clipped there. It
  brings its own ground when it appears — the icon button's `--panel-soft` fill,
  shown with the glyph rather than on the button's own hover, because a glyph
  drawn straight onto the letters underneath leaves both unreadable. The
  unsaved-changes dot holds that same corner until the tab is hovered — it is
  state, not a control, so it says what the corner says while nothing is aimed
  at it.
- The reveal keys off `:hover` and `:has(:focus-visible)` — **not**
  `:focus-within`, which variant 2's hover-revealed chips can afford and this
  cannot: the container here also holds the tab's own title button, so a mouse
  click on the tab left the panel pinned open with the pointer long gone.

## Sidebar and rail

The note's aside is a quiet column; the rail is a floating dock over the sheet.

- Aside: a viewport-sized column — `clamp(240px, 24vw, 380px)` — 60px from
  the note column. The fixed 186px stub truncated every other title; the rail
  instead drinks the screen's spare width, and the note column gives up width
  before the rail does on a narrow laptop. Section headings take the label
  recipe; a count sits at the right end of the heading row in mono 11px
  `--faint`. Rows are text controls — no pills.
- The aside's graph draws its centre node filled with `--mark`, its other
  nodes filled with `--bg` and outlined 1px in `--line-node`, and its edges in
  `--line-strong`. Hover and search highlighting are ink (`--text`), not the
  salient: the match and its edges strengthen while everything else dims in
  place. Only the centre node stays `--mark`, highlighted or not — a frame
  frozen mid-hover must still say which note you are on, and it cannot if
  "where I am" and "what I am pointing at" wear the same colour. The graph
  therefore has no palette of its own: emphasis is contrast, and the one colour
  means one thing.
- Rail: a compact floating dock 8px from the left edge, centred a quarter of
  the way down the viewport so its menus open into empty screen rather than
  against the bottom edge, on `--panel` with a hairline and radius but no
  shadow. Its height stops below the tab strip even in a short window. The
  Settings control stays pinned at the foot while the workspace views and
  open-note controls above it scroll, so appearance controls never disappear
  into an undiscoverable overflow. Glyphs are text controls; workspace views
  and open-note controls share the dock in order. A menu hung off the dock sits
  on the same centre line (`.rail-menu-panel`) or is anchored to its own
  button's rect (`.mode-menu-panel`, `.hierarchy-panel`).
- **A rail panel names itself.** A flyout opens away from the glyph that
  summoned it, and a panel of rows says nothing about which glyph that was, so
  each carries a `.rail-panel-title` — the section label recipe again (variant
  6). It sits above the panel's body, outside any scroller and outside any
  `role="menu"`: a panel that scrolls must not carry its own name off the top,
  and a heading is not a menu item.
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
- Window resize handles are frame interaction zones, not icon-sized controls;
  their hit area follows the window edge geometry and may be narrower than
  24px.
- Visible scrollers use the shared quiet scrollbar. A local `scrollbar-width:
  none` plus the WebKit rule is reserved for compact chrome whose bar would
  compete with the content.

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
