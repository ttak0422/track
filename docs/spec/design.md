# Web Design Master

The reference for styling web UI (`web/src/styles.css`). When adding or
restyling a control, pick exactly one variant below and follow its recipe. If
none fits, extend this document first — do not invent a one-off treatment.

## Principles

- **One surface.** The sidebar, tab strip, and note body share the same panel.
  Permanent chrome draws no slabs, boxes, or fills; the app reads as one sheet
  of paper. The only persistent line is the hairline under the active tab.
- **Hierarchy by ink, not boxes.** State and emphasis are expressed with color
  (`--muted` → `--text` → `--accent`) and `font-weight`, never by adding
  borders or background fills to a control at rest.
- **A box is earned.** A border or fill exists only to separate a control from
  content beneath it (quiet chip) or to lift a genuinely floating layer
  (floating layer). Shadows belong to floating layers alone.
- **Tokens only.** Every color and corner radius comes from the `:root` custom
  properties in `web/src/styles.css`; components never hardcode hex values or
  raw radii. Font sizes in chrome scale via `calc(...px * var(--font-scale, 1))`.
- **No hover-triggered popups on visible content.** Previews and expansions
  open from an explicit affordance (a button, a click), never from merely
  hovering something already readable. Hover may *reveal controls* (quiet
  chips fading in over media) — it must not *open surfaces*.

## Tokens

| Token | Role |
| --- | --- |
| `--bg` | App background behind the panel |
| `--panel` | The main surface (sidebar, notes, tabs, floating layers) |
| `--panel-soft` | Slightly shifted surface: quiet chips, code blocks, inputs |
| `--text` | Primary ink |
| `--muted` | Resting ink for chrome and secondary text |
| `--line` | Hairlines and chip borders |
| `--accent` / `--accent-strong` | The single salient: active state, links |
| `--danger` | Destructive intent |
| `--graph-active`(`-strong`) | Graph highlight |
| `--chart-1..6`, `--chart-ramp-*` | Chart series and heatmap ramp |
| `--font-mono` | The one mono stack: code, the editor, section labels |
| `--radius-sm` (4px) | Badges and inline chips |
| `--radius` (6px) | Controls: quiet chips, inputs, list rows |
| `--radius-lg` (8px) | Panels, cards, embeds, floating layers |

Light and dark values are defined together at the top of `styles.css`; using
tokens makes a component theme-correct with no extra work.

Those three are the whole radius scale — a control that wants a corner takes
one of them, not a new number. Drawn shapes are not on the scale and keep
their own geometry: icon glyphs, heatmap cells, pills (`999px`), circles
(`50%`).

## Variants

### 1. Text control — the default for chrome

Anything sitting directly on the app surface: toggles, segmented modes, nav,
tags. Plain text, no border, no fill, no radius.

- Rest: `color: var(--muted)`.
- Hover / `:focus-visible`: `color: var(--text)`.
- Active/selected: `color: var(--text)` (or `--accent` when it marks a mode
  being *on*, e.g. follow) plus `font-weight: 600`.
- Canonical: `.rail-button`, `.graph-reset`, `.note-tags button`.
- In the icon rail the same states are carried by the glyph rather than a
  label: `.rail-button.active` takes `--accent` for a mode that is *on* (the
  note's display mode, follow), and never for navigation, which goes somewhere
  rather than turns something on.

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

Menus, previews, the search popup, modal dialogs. These legitimately sit above
the page, so they alone carry shadows.

- `background: var(--panel)`, `border: 1px solid var(--line)`, soft
  `box-shadow`. Items inside are text controls (muted rows that ink on hover).
- Canonical: `.menu-panel`, `.note-menu-panel`, `.modal-card`.

### 4. Filled action — modal decisions only

Bordered/filled buttons are reserved for a modal's action row, where a
destructive choice needs weight the flat idiom cannot give.

- Neutral: hairline border, transparent fill. Destructive: `.danger-button`
  filled.
- Canonical: `.modal-actions button`, `.modal-actions .danger-button`.

### 5. Underline input — text entry

Single-line fields carry editability with a bottom hairline, not a box.

- `border: 0; border-bottom: 1px solid var(--line);` transparent background;
  focus moves the line to `--accent`.
- Canonical: `.home-hero .searchbox input`, `input.modal-input`.
- Exception: the multi-line editor textarea keeps a boxed `--panel-soft` field.

### 6. Section label — the caption naming a region

Small caps that title a chrome region or annotate content (ACTIVITY,
BACKLINKS, a code block's language, an OGP card's site name).

- `font-family: var(--font-mono)`, `calc(11px * var(--font-scale, 1))`,
  `font-weight: 500`, `letter-spacing: 0.06em`, `text-transform: uppercase`,
  `color: var(--muted)`.
- One shared rule near the top of `styles.css` carries the typography; each
  site keeps only its own margins. Add new labels to that rule rather than
  restating the recipe.

## Adding new UI

- Reuse the canonical class when the markup allows; otherwise copy its recipe
  and name the variant in a comment (`/* quiet chip */`) so the choice is
  greppable.
- Hit targets for icon-sized controls are at least 24px (WCAG 2.2), even when
  the glyph is smaller.
- Scrollers hide their scrollbars (`scrollbar-width: none` plus the WebKit
  rule), matching the rest of the app.
