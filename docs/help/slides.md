# Slides

Any note doubles as a slide deck: `---` thematic breaks split the body into slides, and the note
page can present them one at a time, full screen — in the live workspace and on a published static
site alike.

This page contains `---` separators, so it is itself a deck. Press **Slides** in the top-right
corner of this page to start presenting, then flip through with the arrow keys.

---

## Writing a deck

There is no special file type — a deck is an ordinary note. Put `---` on its own line, with a blank
line above it, wherever one slide should end and the next begin:

```markdown
# One

First slide.

---

# Two

Second slide.
```

Everything a note page renders works inside a slide: `[[Title]]` links, tables, code,
[[Diagrams]], [[Charts]], [[Embeds]], includes, and math like $e^{i\pi} + 1 = 0$.

---

## Navigation

| Key                            | Action               |
| ------------------------------ | -------------------- |
| `→` `↓` `Space` `PageDown`     | Next slide           |
| `←` `↑` `Shift+Space` `PageUp` | Previous slide       |
| `Home` / `End`                 | First / last slide   |
| `Esc`                          | Leave the deck       |

The on-screen arrows below the slide do the same, and the browser's Back button also leaves the
deck.

---

## Deep links

The slide you are on lives in the URL hash — this one is `#slide=4`. Copy the address while
presenting to link straight to a slide; opening that link starts the deck on it. This works on the
static export too, so a published note can be shared mid-deck.

---

## What counts as a separator

Only a hyphen break splits slides, and the split follows how the page itself reads:

- `---` needs a blank line (or a heading) directly above it. Directly under a line of text it is
  Markdown's setext-heading underline — the text becomes a heading, and no split happens.
- A `---` inside a fenced code block is code, never a separator (the example on slide 2 did not
  split this page).
- `***` and `___` render as horizontal rules but do not split slides.

A note without separators has no deck: the **Slides** button only appears once there is a second
slide.
