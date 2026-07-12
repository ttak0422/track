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

Directory *pages* could say nothing about themselves either — no icon. The first attempt gave them an
`icon::` **inline field**, which was a mistake: it put note-level metadata back inside the body file, the
very thing ADR 0002 and ADR 0032 exist to prevent, and then forced the web render to blank every
whole-line inline field to hide it — which blanked users' real prose (`weight:: 68.2` in a journal) in
the live workspace. ADR 0032 records the correction and its rule.

The second attempt gave each page a **sidecar file** at `<dir>/.track/<name>.yml`, mirroring the vault's
`.track/notes/<id>.yaml`. That got the *split* right and the *medium* wrong. A vault's sidecar is never
hand-written: `track new` and `track open` create it, `track meta` and `track rename` maintain it, and the
user only ever sees the body. A published directory has no such tool — `docs/help/*.md` are just files in a
repository — so the sidecar there is a file you hand-author, thirteen of them, plus one more to hand-rename
every time a page is renamed. That is a lot of boilerplate for an emoji.

The obvious fix — read the ambient user config in directory mode — is wrong: `docs/help` must publish
identically from a contributor's laptop and from CI, and CI has no `~/.config/track/config.yml` at all.

## Decision

- **A per-site config file, opt-in, living with the content.** Directory mode auto-discovers
  `<srcDir>/site.yml` (or `site.yaml` — both spellings are read, and finding both is an error, because a
  filename typo that silently publishes a different site is the failure this config's strictness exists to
  prevent). **Absent file = no site-level config at all** — the `index` convention, no icons — so a
  plain Markdown directory still publishes with no config, which is what makes the mode safe to point at
  any directory. (`BuildDir` only ever scanned top-level `*.md` and skipped directories, so the config file
  sits harmlessly among the pages it describes.)
- **A page's note-level metadata lives in the site config, not in a per-page file — and never in the
  body.** The body/metadata split of ADR 0002/0032 is not negotiable; the *medium* of the metadata is. In a
  vault a per-note sidecar is right because no human writes one: `track new` and `track open` create it and
  `track rename` maintains it. A published directory has no such path — its pages are files in a repo — so
  a per-page sidecar is thirteen hand-written files and one hand-rename per page rename: boilerplate whose
  only content is an emoji. A directory's page metadata therefore goes where the directory already speaks
  for itself, in one map in `site.yml`, keyed by file base name (a directory has no note ids).
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
  knowledge carries over from a vault unchanged. `icons: {pages: {...}}` is the one key a directory has and
  a vault does not: file base name → that page's icon. No `title`, no `base_url`, nothing speculative.
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
- **Icon precedence is `config.NoteIcon`'s, literally — and `icons.pages` is its override slot, not a
  fourth level.** `BuildDir` calls the single resolver with the site's maps and the page's `icons.pages`
  entry as the *override* argument, the argument a vault note's sidecar `icon` fills. So the order is
  override → `tags` → `kinds` (a directory page is always kind `note`), the same order a vault note
  resolves in. One resolver, one precedence rule, nothing to drift.
- **An `icons.pages` entry naming no page is a build error**, naming the entry and the `<name>.md` it
  looked for. It is a typo or a forgotten rename, and the page it meant to decorate would otherwise publish
  with the wrong icon and no one would hear a word. An orphan mapping is never a silent no-op.
- **Strict decoding, loud failures.** `site.yml` is decoded with `yaml.Decoder` + `KnownFields(true)` (the
  idiom `note.ParseMetaDoc` already uses): an unknown key is an error naming the file and the key, as is a
  second `---` document, whose keys one `Decode` would never even read. A file exercised only at publish
  time is exactly where a silently-dropped typo ships a wrong site.

## Consequences

- `docs/help/site.yml` ships with this ADR: an explicit `home: index`, an `icons.pages` entry for thirteen
  of the fourteen pages, and a `kinds` map. `syntax.md` has no entry and takes 📄 from `kinds`, so the
  fallback is live and visible rather than asserted. There is no `icons.tags` map in it: these pages carry
  no tags, and shipping a map that can never match would be dead config. The key and the code path stay —
  it is the shared resolver's, and a directory site whose pages *do* have tags will use it.
- Follow-up for PR #15 (`feat/query`), which adds `tags::` inline fields to a dozen `docs/help` pages to
  drive directory-mode tag pages: under this decision a page's tags are note-level metadata and a `tags::`
  field in a body is the wrong place for them, exactly as `icon::` was. When that branch lands its tags
  belong in `site.yml` beside the icons — an entry growing from `cli: ⌨️` to `cli: {icon: ⌨️, tags:
  [reference]}` — which is also what would make the `icons.tags` map live on this site. Not done here: this
  ADR ships the icon, and the tag pages arrive with the branch that needs them.
- `make site` and both site workflows keep working: their `--root index` was exactly the convention default,
  so dropping it (and the `SITE_ROOT` variable) leaves the published site byte-for-byte identical, and
  everything else they pass is a build flag.
- Vault mode is untouched. A vault note's icons and the workspace's home still come from the ambient
  config; a vault export has no `site.yml` to read (its content is not a directory).
- The cost is a second config surface. It is bounded by the ownership rule above: a key belongs in
  `site.yml` only if it describes the published site itself, and in the CLI only if it describes one
  deployment of it.
