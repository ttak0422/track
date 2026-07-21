# Hierarchy

Links make a graph; sometimes you also want a tree. track builds hierarchy navigation from one
conventional relation property: `up`. A note that declares `up:: [[Parent]]` sits under that
parent, and the engine derives everything else — no separate outline file to maintain. If you know
the `up`/`parent` convention from Obsidian vaults or org-mode's file hierarchies, this is the same
idea.

## Declaring a parent

`up` is an ordinary [[Properties|property]] whose value is a wiki link. Write it wherever
properties live:

```markdown
up:: [[Projects]]
```

or set it on the sidecar from the CLI:

```sh
track meta --title "My note" --set "up=[[Projects]]"
```

Only a link-typed value counts (`up:: draft` is just a string property), and the link resolves the
same way a body `[[link]]` does. A note may declare several parents; the breadcrumb trail follows
the first one, while every parent still lists the note among its children.

On a **published directory site** like this one, a page's parent is note-level metadata — like its
icon and tags — so it lives in the site's own config, never in the body: an `up:` entry in the
`pages` map of `site.yml` names the parent page by file base name or title. An inline `up::` field
in a directory page stays a plain prose [[Properties|property]] with no special lifting.

## What you get

- **Breadcrumbs** — the [[Web workspace]] note view (and this published site) shows the chain of
  `up` links above the note, root first. The trail at the top of this page comes from this page's
  `up:` entry in the site config; [[Block links]] shows a two-level trail.
- **Children** — next to the backlinks, each note lists the notes whose `up` points at it. Open
  [[track]] and you'll see the help topics gathered there.
- **CLI/API** — `track nav --id N` (or `--path P`) prints the same data as JSON:

```json
{"trail": [{"note_id": 1, "title": "Projects", "...": "..."}], "children": []}
```

The live server serves it with each note response, and `track export-site` bakes it into the
published bundle, so all three surfaces agree.

## Notes

- A cycle (`A → B → A`) is harmless: the trail stops where it would repeat a note.
- In a vault the relation is data in your notes — `track props` and queries see it like any
  property — but the note view keeps it out of the property strip: the breadcrumbs and children
  *are* its display, and one relation should not render twice.
