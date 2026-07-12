# 0049. A published site's config travels with its content

Status: Accepted

## Context

`track export-site` has two input modes. In **vault mode** it publishes notes from the user's vault and
already holds that user's ambient config (`~/.config/track/config.yml`), so it can resolve icons
(`config.NoteIcon`) and everything else from it. In **directory mode**
(`site.BuildDir(srcDir, rootName, baseURL, frontendDir, outDir)`) it publishes a plain Markdown directory
that belongs to no vault — this repository's `docs/help` is published exactly that way — and it
deliberately takes no config at all: no vault, no sidecars, nothing from the machine it happens to run on.

That left a published directory site unable to state anything about *itself*. Two names for one concept
had already appeared: the live workspace's landing note is the config key `web.home` (ADR 0046), while
the published site's entry page is the `--root` flag; the help text says outright that they "play the same
role". And `--root` is mode-overloaded ("a note id (vault mode) or file base name (with `--src`)").
Directory pages had no tags and no icon maps either, so icons could only come from a page's own `icon::`
inline field.

The obvious fix — read the ambient user config in directory mode — is wrong: `docs/help` must publish
identically from a contributor's laptop and from CI, and CI has no `~/.config/track/config.yml` at all.

## Decision

- **A per-site config file, opt-in, living with the content.** Directory mode auto-discovers
  `<srcDir>/site.yml`. **Absent file = the previous behaviour, byte for byte** — a plain Markdown
  directory still publishes with no config, which is what makes the mode safe to point at any directory.
  (`BuildDir` only ever scanned `*.md`, so a config file sits harmlessly among the pages it configures.)
- **An ownership split.** The **ambient user config owns the machine and the user**: `vault_dir`,
  `cache_dir`, templates, babel, embedder, capture inbox, web theme, `web.home`. It is unchanged, and
  directory mode still never reads it. The **site config owns the published site**: what its entry page is
  and what its icons are. The test for which side a value falls on: *does it change when the same content
  is deployed somewhere else?* If yes, it is not site config.
- **`--base-url`, `--out` and `--frontend` stay CLI flags.** They fail that test. This repo publishes the
  one `docs/help` directory to two places with different bases: `.github/workflows/pages.yml` builds with
  `SITE_BASE: ${{ steps.pages.outputs.base_path }}`, and `.github/workflows/preview.yml` builds the same
  content with `SITE_BASE: /track-previews/pr-<n>/`. A `base_url` key in `site.yml` would be wrong for at
  least one of them on every PR.
- **Keys: `home` and `icons`.** `home` names the entry page by file base name or page title — the same two
  keys a `[[wiki link]]` resolves by, so it is named the way everything else in a directory site is named.
  `icons: {tags: {...}, kinds: {...}}` is the same shape and meaning as the ambient config's `icons:`;
  knowledge carries over from a vault unchanged. No `title`, no `base_url`, nothing speculative.
- **Entry-page precedence: `--root` > `site.yml` `home` > the `index` convention.** This unifies the
  concept — the site's home is now a config value, like the workspace's — while keeping `--root` as an
  override for a one-off build. Resolving to nothing stays a loud error; a site that silently publishes a
  different front door is worse than one that fails to build.
- **Icon precedence is `config.NoteIcon`'s, literally.** `BuildDir` calls the single resolver with the
  site's maps, so a page's own `icon::` field beats the `tags` map, which beats the `kinds` map (a
  directory page is always kind `note`) — the same order a vault note's sidecar override, tags and kind
  resolve in. One resolver, no second precedence rule to drift.
- **A page's tags come from its own `tags::` inline field**, like its props and its icon: a directory page
  has no sidecar, so its body is the only place a tag can come from. This is what gives the `icons.tags`
  map something to match.
- **Strict decoding.** `site.yml` is decoded with `yaml.Decoder` + `KnownFields(true)` (the idiom
  `note.ParseMetaDoc` already uses), and an unknown key is an error naming the file and the key. A config
  exercised only at publish time is exactly the place a silently-dropped typo ships a wrong site.

## Consequences

- `docs/help/site.yml` ships with this ADR and exercises the mechanism (an explicit `home: index` plus both
  icon maps). It changes nothing in the published output: every help page already states its own `icon::`,
  which outranks both maps, and its entry page is still `index`. That is the intended state — the maps are
  the fallback for pages that do not state an icon.
- Until help pages carry `tags::` fields, the help site's icons still come only from `icon::` overrides and
  the `icons.tags` map matches nothing there. That is correct, not a gap: the mechanism is in place and the
  tags arrive with the pages that want them.
- `make site` and both site workflows keep working with no change: they pass `--root index`, which still
  wins over the config, and everything else they pass is a build flag.
- Vault mode is untouched. A vault note's icons and the workspace's home still come from the ambient
  config; a vault export has no `site.yml` to read (its content is not a directory).
- The cost is a second config surface. It is bounded by the ownership rule above: a key belongs in
  `site.yml` only if it describes the published site itself, and in the CLI only if it describes one
  deployment of it.
