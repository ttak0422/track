# 0050. Note semantics are vault config; machines keep paths and commands

Status: Accepted

## Context

Everything configurable lived in one machine file (`~/.config/track/config.yml` or the platform
equivalent). That conflated two owners. `vault_dir`, `cache_dir`, or `embedder` describe *this
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

- **Machine config** (the platform user config file, `TRACK_CONFIG` to override): `vault_dir`,
  `db_path`, `cache_dir`, `embedder`, `web.theme`, `web.colors_path`. Babel executors stay env-only
  (`TRACK_BABEL_<LANG>`), which is machine scope by construction.
- **Vault config** (`<vault>/.track/config.yml`, optional): `extensions`, `date_format`,
  `journal_date_format`, `default_template`, `journal_template`, `gen_keep`, `task_states`,
  `properties`, `queries`, `capture_inbox`, `archive_note`, `icons`, `web.home`.

Both files reject unknown keys. A vault-scope key in the machine file is a hard error naming the new
location; a machine-scope key in the vault file is a hard error naming the offending key. There is no
fallback layer and no merging of the same key from both files: every key has exactly one home.

Environment overrides keep their existing test/one-off role and precedence (env > file > default),
whichever file the key lives in.

## Why keys with paths or commands never move into the vault

A vault is synced and clonable — that is its point. If a vault could carry `embedder` (a command line
track executes) or redirect `db_path`/`cache_dir`, then cloning a repository that happens to be a
track vault would hand that repository the ability to run arbitrary commands on the reader's machine
or write outside its cache. The ownership split is therefore also the trust boundary: the vault says
what its notes *mean*, the machine says what gets *executed* and *where files land*. This is enforced
structurally by the strict schemas, not by validation of individual values.

## Consequences

- A vault now behaves identically on every machine that opens it, and vault-mode `export-site` no
  longer differs between CI and a laptop (the gap ADR 0049 closed for directory mode only).
- Long-lived processes (LSP, `track web`) read config at startup, so editing a vault config requires
  a restart, same as the machine config before.
- `gen` snapshots note trees and `renames.yaml` only; the vault config is deliberately not
  snapshotted — undoing note edits must not revert configuration.
- Existing machine configs carrying vault-scope keys fail loudly on upgrade and must move those keys
  into `<vault>/.track/config.yml`. This is a deliberate breaking change; a silent fallback would
  leave the ownership split fictional.
- The Neovim plugin's flat YAML reader is unaffected: the only keys it reads (`vault_dir`,
  `cache_dir`) remain machine scope and top-level.
