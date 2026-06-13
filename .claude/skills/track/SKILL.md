---
name: track
description: Use the track CLI when the user asks to create, record, search, rename, or maintain notes, journal entries, Zettelkasten items, or linked Markdown knowledge in this repository's track vault.
---

# track Skill

Use the `track` CLI and parse its JSON output. `$TRACK_VAULT` must point at the vault; set `TRACK_CACHE_DIR` to a cache directory when running in an isolated environment.

When creating AI-generated drafts, pass `--ai`. Complete Markdown from stdin is preserved verbatim, including a leading `#` heading:

```sh
cat article.md | track new --title "Article Title" --ai
```

Titles live in `.track/notes/<id>.yaml`; body H1 headings are content. Rename notes with `track rename (--id N | --title S | --path P) --to S` so backlinks and rename history are updated.

For the full tool-neutral workflow contract, read `docs/spec/agent-workflows.md`.
