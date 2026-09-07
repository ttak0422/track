# Native (macOS) / Web Parity Gap Inventory

`track web` (React, `web/`) に対する `native/` (SwiftUI, macOS) の差分洗い出し。
正本: `docs/spec/web.md`, `docs/spec/native-macos.md`, `docs/spec/design.md`。
調査日: 2026-09-07。native は MVP 途上 (閲覧+検索+タスクの骨格のみ)。

## 1. API カバレッジ (21 endpoints, `internal/track/webui/webui.go`)

| Endpoint | web (`web/src/api.ts`) | native (`TrackAPI/Client.swift`) | 差分 |
|---|---|---|---|
| `GET /api/search` | `searchNotes` (+static 合成) | ✅ `searchNotes` | 同等 (live のみ) |
| `GET /api/note` | `getNote` | ✅ `getNote` | 同等 (GET のみ) |
| `GET /api/tasks` / `?open=1` | `listDatedTasks` / `listOpenTasks` | ✅ 両方 | 同等 |
| `POST /api/task` | `setTaskState` / `setTaskDate` (expect+etag) | ✅ 両方 | 同等 |
| `GET /api/resolve` | `resolveTerm` | ✅ メソッドのみ | UI 未接続 (wikilink 未解決) |
| `GET /api/note/meta` | `getNoteMeta` | ❌ | メタ表示・編集の前提 |
| `POST /api/note/meta` | `saveNoteMeta` | ❌ | 同上 |
| `GET/POST /api/note/read` | `reading.ts` (seen/read) | ❌ | NEW/read バッジなし |
| `GET /api/notes` (`sort=created`) | `listNotes` / `listNewNotes` | ❌ | 最近・新着・カレンダー導出なし |
| `GET /api/activity` | `getActivity` | ❌ | ヒートマップなし |
| `GET /api/agenda` | `getAgenda` | ❌ | Day ビューなし |
| `POST /api/journal` | `openJournal` | ❌ | 日誌導線なし |
| `PUT/POST/DELETE /api/note` | `saveNote` / `createNote` / `deleteNote` (etag 409) | ❌ | 書込みパス全体なし (読取専用) |
| `POST /api/render` | `renderMarkdown` | ❌ (設計上不使用) | raw body のため action-link/`track-query`/`dashboard`/include 未解決 |
| `POST /api/viewspec` | `renderViewSpec` | ❌ | チャートなし |
| `POST /api/asset` | `uploadAsset` | ❌ | 画像・添付なし |
| `GET /api/ogp` | `getOgp` | ❌ | OGP カードなし |
| `GET /api/hierarchy` | `getHierarchy` | ❌ | up ツリー・パンくずなし |
| `GET /api/graph/local`, `/api/graph` | `getLocalGraph` / `getGraph` | ❌ | グラフなし |
| `GET /api/follow` | `getFollowState` | ❌ (対象外) | Neovim 連携のみ |
| `GET /api/events` (SSE) | `useLiveEvents` + 30s poll | ❌ | ライブ更新なし (手動 reload) |

モデル差 (`Models.swift` vs `types.ts`): `NoteDetail.includes/tasks/props/created/updated`、
`Notes/Activity/Agenda/Journal/Hierarchy/Graph/Follow/Ogp/Render/ViewSpec` 系、
書込み系 (`SaveNote*`, `DeleteNote*`, `NoteMeta*`, `AssetUpload*`) が未定義。

## 2. 画面・機能 (web 約50 → native 2タブ)

| web 機能 | native 現状 |
|---|---|
| 9 ルート (`/`, `/notes`, `/graph`, `/voice`, `/calendar`, `/tasks`, `/day`, `/tags`, `/empty`) | 2 タブのみ (Notes=検索+閲覧同居, Tasks)。タブバー/複数タブなし |
| ノート閲覧 (title+body+backlinks) | ✅ 骨格のみ。meta (tags/flags/icon/created/updated/props/copy_path)、trail/children/external/unavailable、目次、On-this-day なし |
| 検索 (title/body/path グループ, `#tag`, NEW, キーボード, スニペット) | 部分。title+snippet の素朴リスト。debounce・履歴・タグ・unavailable・match 表示・エラー表示なし |
| タスク一覧 | 部分。一覧内直接編集 (web は読取専用一覧→note 内編集)。priority/completed 表示・空状態・エラー・note への導線・409 専用表示なし |
| ノート編集/削除/作成/メタ編集 | ❌ 全なし |
| カレンダー/Day/タグ/ヒートマップ/新着/履歴 | ❌ 全なし |
| グラフ (full/local/panel)・階層メニュー・パンくず | ❌ 全なし |
| 読書状態 (NEW/read/stale, seen/read 共有) | ❌ (`adoptReadState` 相当なし) |
| SSE ライブ更新・VaultActivity 通知・30s poll | ❌ |
| 音声入力・共有・テーマ設定・モバイル dock・ショートカット | ❌ (設定・モバイルは macOS 対象外も多い) |

## 3. Markdown 描画 (`MarkdownBody` = `AttributedString(markdown:)` の CommonMark subset)

- ○: 見出し/段落/引用/リスト/コード/強調/リンク属性、textSelection、backlinks 一覧。
- × (素テキストで残る): `[[wikilink]]` (タップ不可), GFM 表, `- [ ]` チェック, `~~`, 脚注, アラート, `^block`/見出し ID, タスク表・カンバン, 全図フェンス (mermaid/dot/d2/drawio/mindmap/map/echarts/viewspec/track-view), `![[...]]` include, 画像/asset, YouTube/Maps/X/OGP/PDF/HTML 埋め込み, KaTeX。
- 入力差: web は `/api/render` 解決済みテキスト、native は raw。本文内タスク書込みは別画面 `TasksView` にのみ存在。
- プレースホルダ表示 (spec が描画対象外ブロックに要求) は未実装 — ソースがそのまま出る。

## 4. TODO (優先度順, 詳細は本 PR の連番 issues)

1. Models: `NoteDetail` 欠落 field + 未定義 response 型の追加 (P0, 並列可)
2. Client: `listNotes` / `note/meta` GET / `note/read` / `hierarchy` / `graph` / `activity`+`agenda`+`journal` / `events` poll の追加 (P0-P1)
3. Reader: メタ表示・trail/children/external・wikilink 解決遷移・リッチブロック placeholder (P0)
4. Search: debounce・unavailable/match・履歴・エラー表示 (P0)
5. Tasks: エラー/空状態・priority・note 導線・409 表示 (P0)
6. 書込み: note 編集/作成/削除・meta 保存・asset (P1)
7. グラフ/カレンダー/Day/タグ/ヒートマップ (P1-P2)
8. 読書状態・SSE・テーマ適用・同梱/署名/配布 (P1-P2)

本ドキュメントは洗い出しの正本。実装は TODO 順に小 PR で進め、埋まった行から ✅ に更新する。
