# 0079. Vault-local static apps use a separate origin

Status: Accepted

## Context

Small browser tools belong with the vault data they operate on, but serving their HTML and JavaScript
from the workspace origin would give them the workspace's browser origin. That origin also carries
the read/write API and websocket. An iframe is not the product: an app should open as a normal,
top-level document and load its own relative files.

The static site has a different problem. Apps are arbitrary authored files, unlike note attachments,
which are content-addressed and selected by references in notes. Publishing every directory under the
vault would expose apps the site owner did not mean to publish.

## Decision

- A vault app is `apps/<name>/index.html` plus relative files. Names are stable lowercase segments
  matching `[a-z0-9][a-z0-9-]{0,62}`; they are not hashes of app content. No manifest or build system is
  required.
- The workspace's stable launch URL is `/apps/<name>/`. The workspace listener redirects to a second
  listener in the same process on the workspace port plus one. A requested port `0` uses the actual
  bound workspace port before adding one. Port `65535` and an unavailable adjacent port fail startup;
  there is no ephemeral fallback. The app listener uses the same explicit bind host, except a wildcard
  workspace bind uses loopback `127.0.0.1`. The redirect hostname is canonical for the bind: `localhost`
  stays `localhost`, numeric loopback stays numeric, and an explicit non-loopback hostname is preserved.
  A path prefix selects the launch vault or a registry-named vault, keeping vault identity out of the
  app's user query string. The workspace-only `__track_vault` selector is consumed before the redirect;
  app query parameters and fragments remain app data.
- The app listener has no API or vault-file routes, directory listing, or SPA fallback. File opens are
  rooted at one app directory and reject traversal and symlinks that escape it. Its Host policy is
  explicit; the main workspace rejects cross-origin API requests on reads, mutations, and websocket
  upgrades, with Fetch Metadata and Referer checks for API requests that omit Origin.
- These are trusted, self-authored static apps, not a sandbox for arbitrary untrusted code. Separate
  ports do not isolate cookies, and apps on the shared app origin can communicate with one another.
  `localStorage` is also shared across every app and vault on that origin; apps should namespace keys
  by app and an app-owned stable vault identifier. The internal `launch` path selector aliases the
  workspace's active vault and may name a different vault after restart, so it is not that identifier.
  Track API integration is unsupported.
- `export-site` copies only explicitly allowlisted apps (`--app`, default none), preserving their
  directory structure, names, and bytes under `apps/`. A rebuild replaces that output tree, preventing
  a previously selected but now unselected app from remaining published. `/apps/<name>/` and
  vault-relative `apps/<name>/` links resolve under the site's base path; `--apps-base-url` optionally
  routes them to a separately hosted apps prefix.

## Consequences

- App launch keeps normal HTML behavior, including relative script/style loading, without giving the
  app the workspace origin. With a fixed workspace address, the adjacent app port and canonical host
  keep that origin stable across restarts; with a workspace port of `0`, the process-selected origin
  is not persistent. The extra listener is process-scoped and is closed with the web server.
- The workspace-only `__track_vault` query key is reserved on `/apps/` launch links. Applications may
  use other query keys unchanged, including `vault`.
- Host/path separation is a narrow boundary against accidental workspace API access, not protection
  against malicious code running with the user's browser privileges. Cookies scoped to a hostname may
  be sent on both ports.
- Static app export does not hash app contents or publish apps implicitly. A change to an app is a
  normal static-file deployment; the site owner chooses which apps are public on every export.
