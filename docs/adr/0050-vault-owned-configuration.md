# 0050. Note semantics are vault config; machines keep paths and commands

Status: Accepted. The split stands; only its inventory has shrunk — `db_path` was removed from the
machine-scope list by [0064](0064-the-index-location-is-always-derived.md), which leaves `cache_dir`
as the one key naming where the index lands.

## Context

Everything configurable lived in one machine file (`~/.config/track/config.yml` or the platform
equivalent). That conflated two owners. `vault_dir` or `cache_dir` describe *this
machine*: where the vault happens to be mounted, where its cache belongs, which local command turns
text into vectors. But `task_states`, the `properties:` schema, saved `queries:`, `icons`, date
formats, default templates, `capture_inbox`, `archive_note`, `web.home`, `gen_keep`, and `extensions`
describe *the notes themselves* — move the vault to another machine and every one of those must come
along, or checkboxes stop parsing, saved queries vanish, and the property schema silently stops
validating.

ADR 0049 already drew this line for published directory sites ("does it change when the same content
is deployed somewhere else? If yes, it is not site config") but only applied it to `export-site --src`.
The multi-vault plan makes the conflation acute: with several vaults on one machine, a single global
`task_states` cannot be right for both a work vault and a public one, and vault-mode exports differed
between CI and a laptop because they read whatever ambient machine config happened to be present.

## Decision

Split configuration into two strictly-decoded files with disjoint key ownership:

- **Machine config** (the platform user config file, `TRACK_CONFIG` to override): at the time of
  this decision, `vault_dir`, `db_path`, `cache_dir`, `web.theme`, `web.colors_path`. Babel
  executors stay env-only (`TRACK_BABEL_<LANG>`), which is machine scope by construction. ADR 0064
  later removed configured `db_path`: the index path is now derived from the cache directory and
  vault path, so `cache_dir` is the remaining machine key for where it lands.
- **Vault config** (`<vault>/.track/config.yml`, optional): at the time of this decision,
  `extensions`, `date_format`, `journal_date_format`, `default_template`, `journal_template`,
  `gen_keep`, `task_states`, `properties`, `queries`, `capture_inbox`, `archive_note`, `icons`,
  `web.home`. ADR 0058 later removed `task_states` and fixed the state set in code; the current
  vault file has no such key.

Both files reject unknown keys. A vault-scope key in the machine file is a hard error naming the new
location; a machine-scope key in the vault file is a hard error naming the offending key. There is no
fallback layer and no merging of the same key from both files: every key has exactly one home.

Environment overrides keep their existing test/one-off role and precedence (env > file > default),
whichever file the key lives in.

## Why keys with paths or commands never move into the vault

A vault is synced and clonable — that is its point. So a vault must not be able to say what runs on
the reader's machine, or where that machine reads and writes.

The first half is currently true by construction: no config file names a command at all. The one key
that did, `embedder`, was removed with the feature that used it (ADR 0056), and the sole remaining
exec path — Babel — is configured by environment variable only and runs on an explicit
`track babel run`. The rule stands for anything added later: a command-valued key belongs to the
machine, never to a vault.

The second half is what the split enforces today. At the time of this decision, `db_path` and
`cache_dir` were machine scope for a plainer reason than danger: they describe *this machine*, not
these notes. ADR 0064 later removed `db_path` as a configured key; the derived index path still
keeps that ownership boundary, while `cache_dir` remains the setting a machine may choose. The same
vault opened on two machines needs two cache locations, and one machine's path may not exist on the
other. Letting a synced vault set either location would point this machine's writes somewhere it
never chose.

Both are enforced structurally by the strict schemas — a key simply has no field in the other file —
rather than by validating individual values.

## Consequences

- A vault now behaves identically on every machine that opens it, and vault-mode `export-site` no
  longer differs between CI and a laptop (the gap ADR 0049 closed for directory mode only).
- Long-lived processes (LSP, `track web`) read config at startup, so editing a vault config requires
  a restart, same as the machine config before.
- `gen` snapshots note trees only; the vault config is deliberately not snapshotted — undoing note
  edits must not revert configuration.
- Existing machine configs carrying vault-scope keys fail loudly on upgrade and must move those keys
  into `<vault>/.track/config.yml`. This is a deliberate breaking change; a silent fallback would
  leave the ownership split fictional.
- The Neovim plugin's flat YAML reader is unaffected: the only machine-config key it still reads
  (`vault_dir`) is machine scope and top-level. (ADR 0050's sibling change hands cache resolution
  entirely to the CLI, so the plugin no longer reads `cache_dir` at all.)
