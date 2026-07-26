# 0059. The help site is a vault

Status: Accepted

## Context

ADR 0055 made a vault able to publish what a directory published — pinned
slugs, `journal: false`, `gen: false` — and deprecated directory mode without
removing it. Removal waits on two things: `docs/help` moving into a vault, and
the shared-bundle tests that only run through directory mode being ported. This
is the first.

`docs/help` was nineteen `.md` files plus a `site.yml` holding what each page
said about itself: its icon, tags, cover image, typed props, and its `up`
parent. That map exists because a directory has no sidecars (ADR 0049). A vault
does, so the map has somewhere better to live.

## Decision

`docs/help` is a track vault: bodies under `note/<id>.md`, one sidecar each
under `.track/notes/<id>.yaml`, and a `.track/config.yml`. `site.yml` is gone —
every one of its `pages:` entries became sidecar fields (`icon`, `tags`,
`image`, `props`, and `up` as the link property it always was in a vault), and
its `home:` became the vault config's `web.home`.

Three things the migration needed, none of them specific to this site:

- **`--all`.** A directory published every file in it; vault mode published only
  the ids it was given. `--all` publishes every note, so the build needs no id
  list to keep in step with the vault. It excludes journals: those are day hubs
  indexing creates as a side effect, and the set of them is a record of which
  days their author worked. Handing that over must be something a caller asks
  for by id, never something "all" does quietly.
- **`--root` defaults to `web.home`.** ADR 0049 established that a site's front
  door does not change when the same content is deployed elsewhere, so it
  belongs with the content — and then left vault mode requiring the id on the
  command line anyway. It no longer does when the vault names one. The flag still
  wins when given and is still required for a vault that names none, so this only
  removes a magic number from build configs.
- **Pinned slugs.** Every page carries `slug:` set to the address the directory
  served it at, so no published URL moved.

## Verification

The bundle was built both ways and compared. Identical: the set of published
pages (19), their addresses, the assets, `site.json`, and every page's body,
title, tags, icon, image, props, and backlinks. Three differences, all of them
the vault being the one that is right:

- The link graph loses two **self-edges**. `ReplaceLinks` has always ignored a
  note's link to itself; `BuildDir` built its own edge set and did not, so the
  published graph showed self-loops the live workspace never does. Two help pages
  demo a self-link, and they no longer appear in their own backlinks.
- `resolve.json` loses its **file-base-name keys**. A directory resolved
  `[[links]]` by base name as well as by title; a vault resolves by title, as
  everywhere else in track. `track doctor` reports no unresolved link, so no page
  relied on it.
- Each page gains **`days`** (the created day, via `ActivityDays`' fallback) and
  a **`props.up`** entry. Neither renders: `days` feeds the calendar, which this
  site does not publish, and the `up` relation has a dedicated display — the
  breadcrumb trail and children list — so `NoteProps` already filters it out of
  the generic property list.

Building does not write to the vault. `export-site` calls `Full()`, and activity
days are stamped by `RefreshIfStale`, so a build on a fresh checkout (where git
gives every file the checkout's mtime) leaves all nineteen sidecars byte-identical.

## Consequences

- The repository loses greppable help filenames: `docs/help/tasks.md` is now
  `docs/help/note/1785024015000.md` with `title: Tasks` beside it. Comments that
  pointed at a path now name the page. This is the cost of notes being
  id-addressed and titles being sidecar-owned (ADR 0013), and it is what makes
  the title editable without moving a file or breaking a URL.
- The help site is now edited the way a vault is edited — `track` commands, the
  workspace, the LSP — rather than as loose Markdown. `make site-dev` still
  rebuilds on change.
- `make site` points `TRACK_VAULT` at `docs/help` and keeps that vault's index in
  `.site-cache`, out of the developer's own cache directory.
- Directory mode still works and still warns. What is left before it can be
  removed is the test port: hierarchy pages, tag pages, and query blocks are
  exercised only through `BuildDir` today.
