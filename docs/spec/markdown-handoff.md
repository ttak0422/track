# External Markdown editor handoff

`:Track markdown` previews the saved current `.md` / `.markdown` buffer using the existing external
reader. It refuses unnamed, unreadable, special and modified buffers; it never saves a buffer or
imports a note. The plugin uses argv arrays, not shell interpolation. A changed buffer during
startup cancels the handoff. It probes a compatible workspace on `web_addr`, reuses it without
owning/stopping it, or starts the usual editor-owned `track web` job. The latter stops on Neovim exit.

`track markdown [--addr HOST:PORT] -- FILE` requires a running compatible workspace and prints JSON
`{"url":"http://127.0.0.1:8765/markdown#handoff=..."}`. It does not open the browser itself.
`--check` prints `{"ready":true|false}` without reading files. An occupied incompatible server is an
error. CLI HTTP disables proxies and redirects and only accepts loopback IPs or `localhost`.

The CLI snapshots the current file's containing folder recursively, so relative Markdown links and
PNG/JPEG/GIF/WebP images work within that scope. All supported sibling files are included, not only
linked ones. Hidden directories, `node_modules`, `vendor` and descendant symlinks are excluded.
The selected file is canonicalized first; `os.OpenRoot` confines all subsequent reads. The folder
may contain at most 5,000 visited entries; each Markdown file is valid UTF-8 and at most 2 MiB,
each image at most 10 MiB, and total decoded bytes at most 16 MiB (24 MiB JSON transfer).

The HTTP server never opens a path supplied by this feature. Under the existing Host/Origin/Fetch
Metadata guard, loopback-only endpoints provide:

- `GET /api/markdown`: protocol identification (`track-markdown-v1`), with no pending-file listing.
- `POST /api/markdown`: JSON `{active, files:[{path,data}]}`; data is base64. Only supported relative
  names and bounded byte payloads pass validation. Returns a route with a random 256-bit capability
  in its fragment, keeping it out of HTTP URL and Referer logs.
- `POST /api/markdown/consume`: JSON `{token}` consumes a capability once and returns the snapshot.
  Missing, replayed and expired capabilities return 410. Both responses use `Cache-Control: no-store`.

Pending snapshots live only in server memory, expire after one minute, and are removed on consumption
or the next inbox access. The queue is capped at eight grants and 32 MiB encoded data. Restarting
the server drops them. No endpoint accepts a disk root or absolute path, reads links on demand,
persists source data, or changes a vault. The feature is disabled on non-loopback server binds.

The browser consumes the capability via POST, reconstructs browser `File` objects, removes the
capability from the current history entry, and passes the selection to `ExternalMarkdownSource`.
The requested file wins over README selection. Existing UTF-8/image checks, scoped links, inert
HTML/code and object-URL cleanup remain in force. StrictMode shares a pending consume; navigation,
manual selection and closing invalidate stale responses. Refresh by saving and invoking the editor
command again. A full reload forgets an opened source; the capability cannot restore it.
