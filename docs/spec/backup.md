# Local vault backup

`track backup` writes a portable gzip-compressed tar archive of canonical vault
files, with their bytes unchanged. It does not open or rebuild SQLite, render
notes, save a generation, modify the source, or upload anything. The existing
`export` command produces presentation Markdown; `gen` omits assets and data.
Neither is a substitute for this backup.

```sh
track backup --out /independent-backup/track-2026-11-01.tar.gz
track backup --vault work --out /independent-backup/work.tar.gz
track backup --all-vaults --out /independent-backup/all.tar.gz
track backup --out /independent-backup/latest.tar.gz --overwrite --timeout 10m
```

The destination's parent must already exist and be writable. Without
`--overwrite`, an existing destination is an error, including one created by
another process while the backup runs. `--overwrite` replaces a regular file only
after the complete archive has been written, flushed, and checked. Symlinks and
directories are refused as destinations. A same-directory temporary file is
removed on normal failure or deadline expiry. An interruption such as killing
the parent process or power loss can leave a `.track-backup-*.partial` file;
this is not a completed backup and can be removed when no backup is running.

The default deadline is five minutes for the worker, including machine-config
resolution, directory enumeration, reads, compression, and file flushing. All
source I/O runs in a separate, disposable process. The parent requests termination
at the deadline without waiting indefinitely for a blocked source filesystem
read. Only the parent can commit the final destination. Output-directory access
and the final filesystem rename are outside the worker deadline; an unavailable
backup destination can itself stall. This feature does not change other Track
commands' handling of blocked I/O.

Success prints one JSON object with `path`, `complete: true`, `format_version`,
`files`, and `vaults`. Failure prints `error` JSON and exits nonzero; there is no
partial-success mode and no final artifact from a failed attempt. An existing
complete archive remains untouched until a successful commit. macOS and Linux use
an atomic no-replace rename by default; unsupported destination filesystem
semantics fail closed. Atomic visibility and power-loss durability still depend
on the filesystem and storage hardware.

## Selection and archive contents

Without `--all-vaults`, only the active vault is exported, using the normal machine
configuration / `TRACK_VAULT` selection. `--vault NAME` selects a registered vault
and may appear before or after `backup`. `--all-vaults` includes every registered
vault and the active vault if it is unregistered; it cannot be combined with
`--vault`. Every selected vault must be available or the entire operation fails.

A single vault lives under `vault/`. Multiple vaults live under `vaults/0001/`,
`vaults/0002/`, etc., with an additional `vaults/active/` for an unregistered active
vault. The manifest maps each archive prefix to its registry name. Files with the
same note ID or asset name in different vaults remain separate. Original absolute
source paths are not stored.

The following top-level directories, recursively, are included:

| Directory | Canonical state preserved |
| --- | --- |
| `note/`, `journal/` | Original bodies, including legacy embedded metadata and sync conflict copies |
| `template/` | Template bodies and directives |
| `assets/` | Images, PDFs, attachments and other assets |
| `data/` | Canonical observations / JSONL data |
| `calendar/` | Event records, revisions and deleted content |
| `apps/` | Vault-local static app files; browser localStorage is outside the vault |
| `.track/` | All vault-owned state, including `config.yml`, authoritative `notes/` sidecars, `gen/` snapshots and cursor, `trash/`, `sources/` evidence and retained `requests/` history |

The initialized directories `note/`, `journal/`, `template/`, `assets/`, `data/`,
and `.track/notes/` must exist as real directories. `calendar/`, `apps/`, vault
configuration and optional history directories need not exist. Unsupported
entries directly under the vault root cause an error instead of being silently
omitted or swept into the archive. This strict first version does not support
custom root layouts; it has no option to bypass that check. Root `.git` entries
and `.DS_Store` files are explicitly excluded. The check also prevents accidentally
backing up an arbitrary personal directory with a partial Track layout.

Machine configuration is not included (a copy stored or hard-linked inside an
otherwise included directory causes an error): it can contain retired agent credentials,
local paths, cache locations, and display settings. The vault's config schema has
no credential fields, so `.track/config.yml` is preserved byte for byte without
parsing or rewriting it. No external paths, environment values, machine-owned
palette, SQLite databases, WAL/SHM files or caches are followed. Renamed titles
and current identities live in bodies and sidecars; Track has no separate vault
UUID or rename ledger. Registry names are machine-owned identity and are retained
in the manifest so cross-vault links can be restored with the same names.

Tar entries are only regular files and directories with relative paths. Symlinks,
special files and paths containing backslashes are refused, including links within
the vault. Reads are anchored at `os.Root`; final file opens use no-follow and
nonblocking flags. Output archives are created with mode `0600`; archive members
use owner-only file/directory permissions, preserving owner executable bits.
Ownership, ACLs, extended attributes, hard-link identity and provider metadata are
not preserved. File bytes and modification times are preserved; this is a logical
vault backup, not a filesystem image.

## Completeness and concurrent writes

Every file has an inventory entry with its archive path, byte size and SHA-256.
Empty directories are included. Files are checked before and after copying, with
inode identity, size, mode, modification time and change time. A second directory
inventory checks for changes, additions, removals and replacements after copying.
A missing, unreadable, unsupported or observably changing entry fails the backup
and discards its temporary output.

On macOS, known `UF_DATALESS` placeholders and legacy `.icloud` stubs are refused
before opening their contents. Other providers can represent placeholders as
ordinary files: availability cannot be determined universally. The worker deadline
bounds an unavailable read, but it cannot guarantee that every provider avoids
hydration. The command never explicitly requests a provider to download files.

The manifest's `complete` means all supported canonical files observed locally
were copied and checked. `completeness_scope` states that it does **not** verify
remote sync completion or detect data already absent before the backup started.
It preserves malformed or legacy data as-is rather than "repairing" it. It does
not diagnose missing note/sidecar pairs; use read-only `track doctor --vault NAME`
separately when investigating divergence. A locally empty but initialized vault
can be backed up; the archive cannot reconstruct data that was already lost.

This is **not an atomic filesystem snapshot**. A writer can change files after
its last check, and multi-file edits that were already partly complete when the
scan began may look stable. Finish synchronization and pause editing before
running a backup. The manifest records the copy interval and consistency model.
No source files are deleted or moved, and no locks are acquired that could make
other Track operations wait for this feature.

## Inspect and restore into a new directory

There is intentionally no command to restore over a live vault. First inspect and
extract a trusted archive into a new, isolated directory:

```sh
tar -tzf /independent-backup/track-2026-11-01.tar.gz
mkdir /separate-recovery/inspection
tar -xzf /independent-backup/track-2026-11-01.tar.gz -C /separate-recovery/inspection
```

Read `manifest.json`: `format` must be `track-backup`, `version` must be supported
(currently `1`), and `complete` must be true. Check each file's byte size and SHA-256
against `entries` before use. The tests exercise both Go's tar reader and the
installed `tar` tool, then compare all restored body, sidecar, asset, config,
history, event and data bytes/checksums.

The extracted `vault/` directory (or an individual `vaults/<prefix>/`) is the new
vault root: do not flatten the `.track/` directory or merge different vaults.
Keep existing vaults untouched. Manually register recovered vault roots under the
manifest's original names in a new machine configuration when cross-vault links
are needed. Reapply required machine-local environment overrides separately;
they were intentionally not archived. After verification, Track can build fresh
indexes against the recovered directory; caches are not restored. Generation
history and retained trash/evidence remain available in that new vault.

Place or copy backups to independent storage. An archive beside the original data
on the same failing disk or sync account does not protect against loss of that
storage. Destinations inside any selected source vault, including symlink aliases,
are always refused to prevent self-inclusion. The command cannot determine whether
two different paths share a disk or synchronization account. Tar.gz is compression,
not encryption; use the protection of your chosen backup destination for sensitive
content, including old generations and deleted notes.
