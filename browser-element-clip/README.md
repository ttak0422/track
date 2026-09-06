# browser-element-clip

A thin Chrome extension (Manifest V3) that captures **one element** on a page as a
`track-fetch-elem` **grab payload** and copies the JSON to the clipboard. It does no conversion —
`track-fetch-elem` is the converter (see `docs/spec/fetch.md`, "Web element clip"). The extension
only *captures*.

## Flow

1. Click the toolbar icon → popup opens.
2. Click **Pick element** → the picker content script is injected into the active tab
   (`activeTab` + `scripting`, so no host permissions).
3. Hover to highlight (Shadow DOM overlay), click to capture, `Esc` to cancel.
4. On capture the grab payload JSON is copied to the clipboard and a toast confirms it.
5. Hand it to the converter:

```sh
pbpaste | track-fetch-elem --note | track new
```

## Permissions (minimal)

| Permission      | Why                                                        |
|-----------------|------------------------------------------------------------|
| `activeTab`     | access the current tab only when the user clicks the icon  |
| `scripting`     | inject the picker content script on demand                 |
| `clipboardWrite`| copy the payload JSON                                      |

No `<all_urls>` host permissions are requested.

## Field mapping — capture vs. `fetch.md` contract

Grab payload shape is `page` / `target` / `nearbyText` / `ancestorPath` / `nearbyElements`.
Field names match the converter's JSON tags in `internal/fetch/elem/elem.go`.

### Page context (`page`)

| Payload field       | Source on page                | Contract note                                 |
|---------------------|-------------------------------|-----------------------------------------------|
| `sanitizedUrl`      | `location.href`               | query + fragment stripped, http(s) only       |
| `title`             | `document.title`              |                                               |
| `viewportWidth`     | `window.innerWidth`           | fallback `documentElement.clientWidth`        |
| `viewportHeight`    | `window.innerHeight`          | fallback `documentElement.clientHeight`       |
| `capturedAt`        | `new Date().toISOString()`    | RFC 3339 (`time` normalization)               |

### Target element (`target`)

| Payload field       | Source on page                       | Budget / policy                                |
|---------------------|--------------------------------------|------------------------------------------------|
| `tagName`           | `el.tagName.toLowerCase()`           |                                                |
| `selector`          | CSS path to nearest `#id`            | ≤ 700                                          |
| `elementPath`       | `tag:nth-child(...)` path (DevTools) | ≤ 900 (`path` budget)                          |
| `fullPath`          | `tag#id` / `tag.class` path          | ≤ 900 (`path` budget)                          |
| `cssClasses`        | `el.className` (collapsed)           | ≤ 500 (`cssClasses` budget)                    |
| `selectedText`      | `getSelection().toString()`          | ≤ 500 (`selectedText` budget)                  |
| `textSnippet`       | `el.innerText` (whitespace-collapsed)| ≤ 200 (`textSnippet` budget)                   |
| `htmlSnippet`       | `el.outerHTML`                       | ≤ 4096 (`htmlSnippet` budget)                  |
| `attributes`        | allowlisted attributes only          | see allowlist below                            |
| `accessibility`     | `role`/`aria-label`/`title`/`alt`/text | `role`, `accessibleName`, `ariaLabel`         |
| `rectViewport`      | `getBoundingClientRect()`            | `x`,`y`,`width`,`height` (rounded to px)       |
| `computedStyles`    | `getComputedStyle()` curated subset  | see curated subset below                       |

### Surrounding context

| Payload field       | Source on page                     | Budget / policy                              |
|---------------------|------------------------------------|----------------------------------------------|
| `nearbyText`        | parent + sibling `innerText`       | ≤ 10 entries, ≤ 200 chars each               |
| `ancestorPath`      | ancestor tag names (root→parent)   | ≤ 10 entries (closest kept)                  |
| `nearbyElements`    | sibling `tag.class "text"` strings | ≤ 6 entries, ≤ 160 chars each                |

### Omitted on purpose

- `reactComponents`, `sourceFile` — require React DevTools fiber / source-map access a content
  script cannot obtain; the converter accepts their absence (spec: "anything the browser tool
  cannot provide is simply omitted").

## Policies mirrored from the contract

These are enforced again by `track-fetch-elem` (defense in depth), but the extension applies them at
capture time too so the payload is already lean.

- **Attribute allowlist**: `id`, `class`, `name`, `type`, `role`, `href`, `src`, `alt`, `title`,
  `placeholder`, `for`, `action`, `method`, plus any `aria-*`. Event handlers and `data-*`
  (other than `aria-*`) are discarded.
- **Computed-style curated subset**: `display`, `position`, `width`, `height`, `margin`, `padding`,
  `color`, `background-color`, `border`, `border-radius`, `font-family`, `font-size`, `font-weight`,
  `line-height`, `text-align`, `z-index` (emitted as camelCase keys). Noise values (`auto`,
  `normal`, `static`, `inline`, `rgba(0, 0, 0, 0)`) are omitted.
- **Redaction** (`[redacted]`, credential-shaped attribute values) and **URL sanitization**
  (drop query/fragment, reject non-http(s)) are the converter's responsibility and are *not*
  duplicated here beyond `sanitizedUrl` stripping — see `internal/fetch/elem/elem.go`.

## Files

| File                 | Role                                            |
|----------------------|-------------------------------------------------|
| `manifest.json`      | MV3 manifest, minimal permissions               |
| `background.js`      | injects `content.js` on demand (service worker) |
| `content.js`         | picker UI + payload capture (Shadow DOM)        |
| `popup.html`/`popup.js` | toolbar popup: pick trigger + hand-off command |
| `sample-payload.json`| a within-budget grab payload for reference      |
