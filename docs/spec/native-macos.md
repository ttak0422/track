# macOS Native App (SwiftUI) — Architecture & API Contract

`[[20260906 macOSネイティブアプリ計画]]` の実装仕様。MVP: 閲覧+検索+タスク。
macOS専用、Web技術スタック不使用、Goエンジン流用。

## Architecture: bundled sidecar + localhost HTTP

- アプリは Go 製 `track` バイナリを同梱し、起動時に子プロセスとして
  `track web --addr 127.0.0.1:<free-port>` を起動する。終了時に `track web stop`
  または子プロセス kill で停止する。
- SwiftUI 側は `URLSession` で `http://127.0.0.1:<port>/api/*` のみを叩く。
  パース・索引・検索・リンク解決の再実装は一切しない。
- `track web` の既定 bind は loopback のみ (`internal/cli/cli.go` の usage:
  `track web [--addr 127.0.0.1:8765]`)。外部公開は `guard` が拒否する。
- 型の正本は `web/src/types.ts`、通信手順の正本は `web/src/api.ts`。
  Swift の `Codable` モデルはここからの機械的移植とする。
- 注意: サーバは note id を JSON 数値 + 別の `vault` ラベルで返すが、クライアントは
  id を opaque 文字列として扱う (`api.ts` の `stringifyIDs` と同じ正規化。
  他 vault の id は `"<vault>~<id>"` — `~` 区切り。`vaultId.ts` 参照)。

## Endpoint inventory (`internal/track/webui/webui.go:290-309`)

21 endpoints。MVP が使うものに ★。

| Endpoint | 用途 | MVP |
|---|---|---|
| `GET /api/search?q=` | 全文検索。`{results:[{note_id,title,match,tags,...}]}` | ★ |
| `GET /api/note?id=` | ノート本文+backlinks/children。`{note:{body,...},backlinks,children,external}` | ★ |
| `GET /api/note/meta` | メタ取得 | ★ |
| `POST /api/note/meta` | メタ保存 | — (MVP外) |
| `GET /api/note/read` | 既読位置 | ★ (任意) |
| `POST /api/note/read` | 既読記録 (`markSeen`) | ★ (任意) |
| `GET /api/tasks` | タスク一覧。`{tasks:[{note_id,line,state,done,priority,due,scheduled,text}]}` | ★ |
| `POST /api/task` | タスク状態変更 | ★ |
| `GET /api/resolve?` | タイトル→id 解決 | ★ |
| `GET /api/notes` | ノート一覧 | ★ (任意) |
| `GET /api/activity` | 活動ヒートマップ | — |
| `GET /api/agenda` | 1日の agenda | — |
| `GET /api/journal` | ジャーナル | — |
| `POST /api/render` | Markdown→HTML レンダリング (ネイティブ描画を使うため MVP では不使用) | — |
| `GET /api/viewspec` | 可視化 spec | — |
| `/api/asset...` | asset upload/serve | — |
| `GET /api/ogp` | OGP 取得 | — |
| `GET /api/hierarchy` | 階層メニュー | — |
| `GET /api/graph/local`, `GET /api/graph` | グラフ | — (MVP外) |
| `GET /api/follow` | エディタ追従 (Neovim 連携用。ネイティブ版は対象外) | — |
| `GET /api/events` | SSE。vault 変更通知 → ネイティブ側は再 fetch の契機に使う | ★ (任意) |

## PoC (verified 2026-09-06, commit `aa86def`)

`docs/help` vault を `127.0.0.1:18765` で serve し、curl (Web以外のHTTPクライアントの代理) で確認:

- `GET /api/search?q=track` → 200, `{results:[...]}` (title マッチ等)
- `GET /api/tasks` → 200, `{tasks:[{note_id,line,state,done,priority,due,scheduled,text}],vault:""}`
- `GET /api/note?id=1785024015000` → 200, `{note:{body:"# Tasks\n\n... plain GFM ..."},backlinks:[...],children:[],external:[]}`
- `track web stop --addr` → `{"stopped":true}` で停止確認

結論: Swift の `URLSession` からそのまま駆動できる。`/api/render` を使わず
`note.body` (素の GFM) を受け取り、描画は `swift-markdown` 系でネイティブに行う。

## MVP scope

- 画面: ノート閲覧 (本文+バックリンク+メタ)、検索 (全文+タグ+履歴はクライアント保持)、タスク (一覧・状態遷移・日付)。
- 描画対象外: 数式 (KaTeX)、図 (Mermaid/D2)、地図 (Leaflet)、PDF デッキ、グラフ、
  カレンダーフルビュー、音声入力。該当ブロックはプレースホルダ表示とする。
- デザイントークンは `docs/spec/design.md` を Swift に直訳する
  (`--bg/--panel/--text/--muted/--faint/--line/--mark` の light/dark)。

## Stack order (PR 積み上げ順)

1. 本書 (spec) ← now
2. `native/Sources/TrackAPI`: Codable モデル + API クライアント (SwiftPM, `swift test` で検証)
3. MVP-1 閲覧画面 (SwiftUI)
4. MVP-2 検索画面
5. MVP-3 タスク画面
6. 同梱・起動/停止・署名/配布
