# CLI

The `track` command is the source of truth for the vault. Every subcommand that changes content
reindexes immediately, so search and links stay current without a separate build step. Commands that
return data print single-line JSON.

Back to [[track]].

## Creating and editing notes

| Command | Purpose |
| --- | --- |
| `track new --title <t>` | Create a note (fails if the title exists). |
| `track open --title <t>` | Open the note with this title, creating it if absent. |
| `track append (--id N \| --title S) --body <s>` | Append body text and/or merge tags. |
| `track update (--id N \| --title S) --body <s>` | Replace body text and/or update tags. |
| `track journal [--offset <n>]` | Open or create a daily note. |

`--body` is read from piped stdin when the flag is omitted:

```sh
printf '本文 [[他ノート]]\n' | track open --title "メモ"
```

## Finding notes

| Command | Purpose |
| --- | --- |
| `track search --query <s>` | Search notes; `--query '#tag'` filters by tag. |
| `track notes` | List every note, newest first. |
| `track notes --untagged` | List only the notes that still carry no tags. |
| `track resolve --term <s>` | Resolve a keyword to a note. |
| `track backlinks --id N` | List notes that link to a note. |
| `track graph --id N` | Show a local link graph. |
| `track graph --orphans` | List notes with no inbound link and notes whose title names a missing parent scope. |

## Curating tags

`track notes --untagged` is the pull side of a tagging pass: it returns exactly the notes that still
need tags, so a person — or an automated helper — can work through them one at a time. Add tags to an
existing note with `track append`, which merges them into the sidecar without touching the body:

```sh
track notes --untagged                       # {"notes":[{"note_id":…,"title":"Draft","tags":null}, …]}
track append --title "Draft" --tag zettel    # merge a tag; the note drops off the untagged list
```

Journals are date-titled and have their own [[Web workspace]] surfaces, so they never appear in the
note listing.

## Maintenance

`track refresh-all` runs the whole maintenance pipeline in one idempotent pass: a full reindex followed
by a read-only `doctor` health report. It changes no notes, so it is safe to run unattended on a
schedule (cron or launchd) to keep the index in step with a vault that a cloud sync edits behind
track's back.

```sh
track refresh-all
# {"reindex":{"indexed":42,"deleted":0,"links":17},"doctor":{"scanned":42,"issues":[],"ok":true},"took_ms":12}
```

A crontab line that refreshes every 15 minutes:

```cron
*/15 * * * * /usr/local/bin/track refresh-all >/dev/null 2>&1
```

## Publishing

`track export` writes a single note out as Markdown. `track export-site` builds a whole static HTML
site — see [[Web workspace]] for the rendered reading experience it mirrors.

```sh
track export-site --src docs/help --root index --out ./site
```

`track render` turns a declarative View Spec into a chart or article — see [[Visualization]].

```sh
track render --spec chart.json --out chart.svg --renderer svg
```
