# 0049. A published site's config travels with its content

Status: Accepted

## Context

`track export-site` has two input modes. In **vault mode** it publishes notes from the user's vault and
already holds that user's ambient config (`~/.config/track/config.yml`), so it can resolve icons
(`config.NoteIcon`) and everything else from it. In **directory mode** (`site.BuildDir`) it publishes a
plain Markdown directory that belongs to no vault — this repository's `docs/help` is published exactly that way — and it
deliberately takes no config at all: no vault, no sidecars, nothing from the machine it happens to run on.

That left a published directory site unable to state anything about *itself*. Two names for one concept
had already appeared: the live workspace's landing note is the config key `web.home` (ADR 0046), while
the published site's entry page is the `--root` flag; the help text says outright that they "play the same
role". And `--root` is mode-overloaded ("a note id (vault mode) or file base name (with `--src`)").

Directory *pages* could say nothing about themselves either — no tags, no icon. The first attempt gave
them an `icon::` **inline field**, which was a mistake: it put note-level metadata back inside the body
file, the very thing ADR 0002 and ADR 0032 exist to prevent, and then forced the web render to blank
every whole-line inline field to hide it — which blanked users' real prose (`weight:: 68.2` in a journal)
in the live workspace. ADR 0032 now records the correction and its rule. What a page needs is what a
vault note has: a sidecar.

The obvious fix — read the ambient user config in directory mode — is wrong: `docs/help` must publish
identically from a contributor's laptop and from CI, and CI has no `~/.config/track/config.yml` at all.

## Decision

- **A per-site config file, opt-in, living with the content.** Directory mode auto-discovers
  `<srcDir>/site.yml` (or `site.yaml` — both spellings are read, and finding both is an error, because a
  filename typo that silently publishes a different site is the failure this config's strictness exists to
  prevent). **Absent file = no site-level config at all** — the `index` convention, no icon maps — so a
  plain Markdown directory still publishes with no config, which is what makes the mode safe to point at
  any directory. (`BuildDir` only ever scanned top-level `*.md` and skipped directories, so both the
  config file and the `.track/` sidecars sit harmlessly among the pages they describe.)
- **Site-level and page-level are separated by location, not by name.** What a *page* says about itself
  is not site config: it lives in that page's own sidecar at `<srcDir>/.track/<name>.yml` — the same
  body/metadata split a vault note has, keyed by file base name (ADR 0032 carries the decision). Each is
  read whether or not the other exists. `site.yml` stays at the directory root and does not move under
  `.track/`; there is no name to collide over.
- **An ownership split.** The **ambient user config owns the machine and the user**: `vault_dir`,
  `cache_dir`, templates, babel, embedder, capture inbox, web theme, `web.home`. It is unchanged, and
  directory mode still never reads it. The **site config owns the published site**: what its entry page is
  and what its icons are. The test for which side a value falls on: *does it change when the same content
  is deployed somewhere else?* If yes, it is not site config.
- **`--base-url`, `--out` and `--frontend` stay CLI flags.** They fail that test: none of them says
  anything about the content, each describes one *build* of it. `--out` is a directory on the machine
  running the build, `--frontend` a path into that machine's frontend build tree, and `--base-url` the
  absolute origin one deployment is served from. This repo publishes the one `docs/help` directory twice —
  to GitHub Pages and, on every pull request, to a preview URL — so an origin baked into `site.yml` would
  be wrong for at least one of them. (The deploy base path those two workflows *do* vary, `SITE_BASE`, is a
  Vite build variable; it is baked into the frontend bundle and never reaches the CLI at all.)
- **Keys: `home` and `icons`.** `home` names the entry page by file base name or page title — the same two
  keys a `[[wiki link]]` resolves by, so it is named the way everything else in a directory site is named.
  `icons: {tags: {...}, kinds: {...}}` is the same shape and meaning as the ambient config's `icons:`;
  knowledge carries over from a vault unchanged. No `title`, no `base_url`, nothing speculative.
- **Entry page: `site.yml` `home`, else the `index` convention — and `--root` is gone from directory
  mode.** The site's home is now a config value, like the workspace's, and it is the *only* way to name it:
  by the ownership rule above, a site's front door does not change when the same content is deployed
  somewhere else, so it belongs with the content and not on the command line. Keeping `--root` as a
  one-off override would also have kept it mode-overloaded — "a note id (vault mode) or file base name
  (with `--src`)", one flag with two meanings — and nothing ever passed it anything but the convention
  default anyway. So directory mode rejects `--root` loudly, naming `site.yml` `home` as its replacement
  (silently ignoring it would publish a front door the caller did not ask for), and in vault mode `--root`
  now means one thing: the landing note's id, still required. The name resolves against file base names
  first and page titles only then: the two share one namespace in the link map, and a page whose H1 happens
  to spell another page's file name must not inherit the front door. Resolving to nothing stays a loud
  error; a site that silently publishes a different front door is worse than one that fails to build.
- **Icon precedence is `config.NoteIcon`'s, literally.** `BuildDir` calls the single resolver with the
  site's maps, so a page sidecar's `icon` beats the `tags` map, which beats the `kinds` map (a directory
  page is always kind `note`) — the same order a vault note's sidecar override, tags and kind resolve in.
  One resolver, no second precedence rule to drift.
- **A page's tags come from its sidecar's `tags` key**, like its icon and its props. This is what gives
  the `icons.tags` map something to match.
- **Strict decoding, loud failures — for both files.** `site.yml` and every page sidecar are decoded with
  `yaml.Decoder` + `KnownFields(true)` (the idiom `note.ParseMetaDoc` already uses): an unknown key is an
  error naming the file and the key, as is a second `---` document, whose keys one `Decode` would never
  even read. A page sidecar naming no page (`.track/ghost.yml` with no `ghost.md`) and one page spelled
  both `.yml` and `.yaml` are errors too. Files exercised only at publish time are exactly where a
  silently-dropped typo ships a wrong site — or a page missing the metadata its author wrote.

## Consequences

- `docs/help/site.yml` ships with this ADR (an explicit `home: index` plus both icon maps), and
  `docs/help/.track/*.yml` carries the pages' own metadata. All three icon paths are live on that site:
  twelve pages set their own `icon`, `cli.md` sets none and takes 📖 from its `reference` tag, and
  `syntax.md` has no sidecar at all and takes 📄 from `kinds`. The maps are no longer dead config.
- Follow-up for PR #15 (`feat/query`), which adds `tags::` inline fields to a dozen `docs/help` pages to
  drive directory-mode tag pages: under this decision a `tags::` field is an ordinary prop, not the page's
  tags. Those must move into the page sidecars' `tags` key when that branch lands.
- `make site` and both site workflows keep working: their `--root index` was exactly the convention default,
  so dropping it (and the `SITE_ROOT` variable) leaves the published site byte-for-byte identical, and
  everything else they pass is a build flag.
- Vault mode is untouched. A vault note's icons and the workspace's home still come from the ambient
  config; a vault export has no `site.yml` to read (its content is not a directory).
- The cost is a second config surface. It is bounded by the ownership rule above: a key belongs in
  `site.yml` only if it describes the published site itself, and in the CLI only if it describes one
  deployment of it.
