# Native (macOS) / Web Parity Gap Inventory

`track web` (React, `web/`) に対する `native/` (SwiftUI, macOS) の差分洗い出し。
正本: `docs/spec/web.md`, `docs/spec/native-macos.md`, `docs/spec/design.md`。
調査日: 2026-09-07。P0 (閲覧+検索+タスク骨格) は PR #242 で対応済み。
残TODO (P1-P2) は本ブランチで対応し、本書 §4 に記録する。

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
| `POST /api/render` | `renderMarkdown` | ✅ `renderMarkdown` (解決済み描画+raw fallback) | 解決済みテキストを描画 |
| `POST /api/viewspec` | `renderViewSpec` | ✅ `renderViewSpec` (option JSON 文字列) | figure-host に渡す |
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

## 3. Markdown 描画 (`GFMBody` = MarkdownUI 2.4.1 ネイティブ描画)

- ○: GFM 準拠の見出し/段落/引用/リスト/表/タスクリスト/取消線/autolink/コード/リンク/画像
  (cmark-gfm 拡張 autolink・strikethrough・tagfilter・tasklist・table)。
  画像は `assets/…`→`/api/asset` 解決の ImageProvider、http(s) は直接表示。
  `[[wikilink]]` は本文内リンク (trackwiki scheme 横取り) + Links レールで遷移。
- × (プレースホルダ): taskboard/track-query/dashboard フェンス (サーバ解決対象)、脚注 `[^1]`。
- mermaid・数式・echarts/viewspec・dot/d2/drawio/mindmap(mermaid 変換)/map(Leaflet) は
  figure-host (WKWebView 島) で実描画。track-view JSON はネイティブ描画。
- `![[...]]` include は NoteInclude 差し込みで描画。
- メディア埋め込み (OGP カード・PDFKit・text asset・YouTube/Maps) は renderer に配線済み。
- タスクボード (状態別カラム)、カレンダーの日誌開く/作成、音声のジャーナル追記、
  MRU・共有・NEW バッジ・分割編集・graceful 停止に対応。
- 入力差: なし。`/api/render` 解決済みテキストを描画し、失敗時は raw にフォールバック。

## 4. TODO 対応状況

- [x] 1. Models 欠落 field + 未定義 response 型 (P0, #242)
- [x] 2. Client: listNotes / meta GET / read / hierarchy / graph (P0, #242) +
      書込み (save/create/delete/meta) / activity+agenda+journal / ogp (P1, 本ブランチ)
- [x] 3. Reader: メタ・trail/children/external・wikilink・placeholder (P0, #242) +
      編集/保存(etag 409)/削除(確認)/作成/メタ保存/seen 報告 (P1, 本ブランチ)
- [x] 4. Search: debounce・unavailable/match・履歴・エラー (P0, #242)
- [x] 5. Tasks: エラー/空状態・priority・409 (P0, #242)
- [x] 6. 書込み: note 編集/作成/削除・meta 保存 (本ブランチ。asset アップロードは手入力パス指定のみ)
- [x] 7. グラフ (full/local 一覧) / カレンダー (月グリッド+agenda) / 階層ツリー / タグ索引 /
      ヒートマップ (いずれも notes listing 導出。Canvas 力学レイアウト・D&D カンバンは対象外)
- [x] 8. 読書状態 (ReadingStore + seen 報告・30s read 閾値)・SSE ライブ更新
      (LiveEventPoller → .trackVaultChanged → Tasks/Calendar 自動 reload)・
      テーマ適用 (design.md 10トークン + system/light/dark + 文字サイズ)

対象外として残すもの (macOS ネイティブの範囲外か、MVP 対象外の明示事項):
音声入力、Neovim follow、モバイル dock、静的サイト基盤、共有アクション、
図フェンスの実描画 (placeholder 表示)、PDF デッキ、地図埋め込み、OGP カード描画
(取得 API のみ)、アセットのバイナリ表示、タブバー複数タブ。
