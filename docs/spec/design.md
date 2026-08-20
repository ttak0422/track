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
- **A pointer that cannot hover gets none of it.** Both halves of the rule above
  are a cursor's idiom. On a touch screen there is no resting on anything — the
  tap that would open a preview is the tap that follows the link, and it focuses
  the link besides — and what opens is a window to be dragged, resized, and
  dismissed by pointing somewhere else. So under `(hover: none)` no preview opens
  at all (wiki link, graph node, media), the tab's popup is absent, and the
  controls hover would have revealed stand from the start instead: a reveal that
  never fires leaves a control nobody can find. The published site is where this
  is felt — a phone reads it, while the live workspace is desk-bound.
- **A tap is not a hover.** The rail's flyouts open on hover and toggle on click,
  and a tap fires `pointerenter` on its way down all the same — so the tap opened
  the panel and its own click, finding it open, shut it again. Every rail flyout
  therefore takes its hover handlers from `hoverOpen`, which ignores a touch
  pointer and leaves the click the whole job; a button that also opens on focus
  asks for `:focus-visible`, since the focus a tap gives it has a click on its
  way. A panel a thumb can open is one that opens on a press, not on an arrival.

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
- A block that bleeds takes the whole reading surface — the reader's left edge to
  its scrollbar — not a window's width struck from the middle of the prose. Once
  the aside docks the column is no longer centred, so the second reading leaves a
  band of surface unused on the right and spills the same amount off the left,
  where it is only ever clipped. The bleed is measured from the reader's left
  padding edge instead. Nothing is held back to protect the aside: the aside's
  padded ground (see Sidebar) is what keeps the block from taking its words.

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

Diagram controls do not rest on content at all: an inline diagram gathers every control it has —
the fold chip at the left, the rest at the right — into one strip on the frame's own ground above the
drawing (`.mermaid-bar`). The strip uses the sunk `--panel-soft` ground, `--radius`, and a small
padding inset; its glyphs take variant 7, whose hover/focus surface is `--panel` so it remains legible
against the strip. A chip floating in the drawing's corner lands on whatever the diagram put there,
and a control a reader has to look *past* is worse than one they have to look for. Off the drawing
there is nothing to obscure, so the strip waits for no hover either — which is also the only
way a touch pointer ever reaches it. A popup keeps its floating cluster:
its drawing is fitted inside the window's own padding, so the corner is already clear.

Media and deck controls remain quiet chips when their underlying content needs a separating surface.

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
- **A lightbox carries its own way out.** Esc and a click past the modal close
  it, and a modal sized to fill the window leaves neither: the backdrop is a few
  pixels wide, and a phone has no Esc. So each one takes a `.lightbox-close` in
  its top-right corner — placement only, the button keeping its own surface's
  icon-button treatment (`.mermaid-control`, `.graph-reset`).

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
BACKLINKS, a code block's language, an OGP card's site name, the vault a tab's
note came from).

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

### 9. Ink disc — a thumb's target floating over prose

A round control that sits on the reading surface with nothing behind it to
rest on: the phone's floating mark and the buttons it fans. Variant 3's panel
surface is a hair from the page's own, which left a muted glyph on a disc you
had to find among the words behind it, so this one inverts — the fill is ink
and the glyph is the page. The pair swaps itself between themes: a black disc
with a white glyph in light, white with a black glyph in dark.

- `background: var(--text)`, `color: var(--bg)`, no border,
  `border-radius: 50%`, soft shadow. At least 44px: it is aimed at with a thumb.
- Hover / `:focus-visible`: `outline: 2px solid var(--mark)` with a 2px
  offset. The glyph cannot ink further — it is already the page's colour on
  ink — so the aimed-at state rings the disc rather than recolouring it.
- A disc carrying the brand mark shows the letter alone. The mark is a tile —
  it draws its own ground, near-black in light and white in dark — and a square
  inside the circle is a second shape, not a mark, so the tile is blended into
  the disc: `mix-blend-mode: lighten` resolves the light tile's darker ground
  to the disc it sits on, `darken` does the same for the dark tile's lighter
  one, and the letter survives both. A configured site icon is left alone; a
  supplied image keeps its own colors, tile and all.
- Canonical: `.mobile-dock-fab`, `.mobile-dock-fan-btn`.

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
  frame with an ellipsis. **The active tab takes twice the frame (336px)** — the
  title being read is the one that gets room — and the invariant still holds:
  closing the active tab hands the wide frame to the next one, so the close
  button keeps landing where the last one was. A phone gives the active tab
  nothing extra, because every tab already fills the strip there. The strip
  never scrolls sideways — a title you cannot see is in the menu, not off the
  edge. **A tab sent to the `+N` menu can be closed in the menu itself**: each
  row pairs its open action with a close button (variant 7), so dismissing an
  overflowed tab costs no page switch — which on a phone, where the strip holds
  one tab, would be the whole trip.
- The active tab is marked by `border-bottom: 2px solid var(--mark)` plus
  `font-weight: 500` — never a fill or a box. Inactive tabs are `--faint` — a
  step further back than chrome usually stands, because the strip is read by
  finding the one title that is not faded — with no border, and a hairline
  under the whole strip separates it from the note. The strip carries only one
  dot, and it belongs to the unsaved-changes state: a second one marking the
  active tab read as "this tab is being edited", which is the one thing the
  tab you are on cannot afford to be ambiguous about.
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
- **At phone width the strip is one tab.** The frame fills the strip rather than
  taking its 168px, so the same measuring pass settles on the leftmost tab — which
  is always the note being read — and everything behind it waits in the `+N` menu.
  Nothing else about the strip changes: same measure, same overflow, and the tab
  you are on in the same place. Close stands in that tab from the start there,
  since no hover will reveal it, and the popup carrying the full title is gone —
  a tab with the whole strip to itself has room to say what it is.

## Task table

The notation table's cells are their own controls, wearing the text they
show: the state cell is a stripped select, a date cell a stripped button.

- A date cell opens the workspace's own calendar (`TaskDatePicker`), not the
  browser's native one: the native picker shows era years and a foreign
  scheme, and it cannot be restyled. The picker is a floating layer (variant
  3) anchored under the cell, with its own month navigation (text controls),
  a day grid, and a footer of two text controls — `DELETE` and `SAVE`, muted
  and inking, SAVE in full ink. Today wears the mark's ring; the working
  choice is a filled mark.

## Sidebar and rail

The note's aside is a quiet column; the rail is a floating dock over the sheet.

- Aside: a viewport-sized column — `clamp(240px, 24vw, 380px)` — 60px from
  the note column. The fixed 186px stub truncated every other title; the rail
  instead drinks the screen's spare width, and the note column gives up width
  before the rail does on a narrow laptop. Section headings take the label
  recipe; a count sits at the right end of the heading row in mono 11px
  `--faint`. Rows are text controls — no pills.
- Docked, the aside takes the sheet's own ground (`--panel`) — no rule, no
  radius, nothing that reads as a box. It is not decoration: a full-bleed block
  runs the width of the reading surface and passes beneath this column, and
  strokes crossing the contents and backlink lists made them unreadable. The
  words win, and the ground is what lets them.
- That ground reaches past the words by the page's usual 16px, because ground
  flush with a glyph is not ground — a stroke arriving from under the column
  stops touching the letter it stopped at. It grows outward only: the padding is
  added to the column's width and taken back off its margins, so neither the
  words nor the row move.
- The aside's graph draws its centre node filled with `--mark`, its other
  nodes filled with `--bg` and outlined 1px in `--line-node`, and its edges in
  `--line-strong`. A node's radius carries its precomputed five-level grade
  (1–5, from the note's own outgoing-link count, graded absolutely over the
  whole vault) — the same size in every view, so the engine computes it once
  and the canvas only draws it. The five radii are 4 / 6 / 8.5 / 12 / 17px:
  each level about 1.4× the last, because a grade has to be legible from the
  shape of the field rather than by comparing two dots side by side. The
  centre keeps its focal 10px, or its own grade where that is larger. Hover
  and search highlighting are ink
  (`--text`), not the salient: the match and its edges strengthen while
  everything else dims in
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
- **A screen with no cursor, or a window under 540px, trades the rail for a
  floating mark.** Both ask one question — is there reach and room for a rail
  down the side? — and a phone answers no whichever way it is turned, which
  width alone could not say: rotating one is 390px becoming 844px, and the
  dock jumped back to the left edge halfway through the turn. A 64px lane is a
  quarter of a phone besides, and the reader wants that width more than the
  dock wants a margin. A foot rail spanning the window bought the reach with
  a strip of the reading surface, so it is gone: a round track logo floats
  over the reading surface instead (`MobileDock`), draggable anywhere, and a
  tap fans its controls — search, history, the views, settings, and the open
  note's own group — out in the arc facing away from the edges it rests
  against: a half-circle against one edge, the quadrant between them in a
  corner. The arc's radius grows with the number of buttons rather than the
  buttons crowding along a fixed one — an arc too short for them ends with the
  off-screen clamp stacking half the fan on a single point. Each of the
  fan's popups is a rail panel unchanged (variant 3 + `.rail-panel-title`),
  and the note group is the rail's own four controls (follow, display mode,
  Meta, Delete) as one panel; the copy actions stay behind, since a phone's
  own selection copies text. The mark and its fan buttons are ink discs
  (variant 9) — inverted, unbordered, soft shadow — because they sit on the
  prose itself rather than on chrome; the panel surface a rail button rests on
  is a hair from the page's own, and a disc wearing it disappeared into the
  words behind it. The brand tile is blended into the mark's disc so the letter
  alone stands on it. The flyouts those buttons open are ordinary rail panels
  (variant 3 + `.rail-panel-title`), but pinned to the window rather than to
  the button: the mark is dragged wherever the reader likes, and a panel placed
  from its rect opened somewhere new every time. They take the window the way
  the full-page graph takes the reader, and a list too short to fill that box
  leaves the rest of it empty — where a panel opens matters more than how much
  of it is used. The mark floats above them and closes them.
  The rail stays mounted behind it (the `/` search chord and the popups still
  work) but takes no strip of the window: `--foot-dock` stays `0px`, and
  nothing below needs a breakpoint of its own. (What is *only* about width
  stays behind the 540px query: the tab frame filling the strip, the reader's
  tighter margins. A landscape phone and a tablet have the width for several
  tabs, whatever their dock is doing.)
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
