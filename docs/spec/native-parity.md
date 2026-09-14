# Web / Native 互換対応リスト

調査日: 2026-09-14。比較基準: commit `d8e31bb` の `web/` と `native/`。
対象は `track web` のライブ版と macOS Native 版。
基本機能・データの意味・操作の結果を互換にし、見た目は Web 版と同等以上にする。
SSG の生成・公開基盤は移植しない。

旧版の「P1–P2 対応済み」は機能の骨格があることを示しており、操作・描画の互換性を保証していなかった。
本書は旧 MVP の対象外指定を更新する。
音声、図表、PDF、地図、共有、複数タブ、Neovim follow も、現在 Web にある利用機能として比較対象に含める。

調査はソースコードの照合であり、アプリ実機操作・画面キャプチャによる比較は未実施。
以下の「部分対応」は入口や描画が存在しても、完了条件の確認が残るものを含む。
未チェックは今回の対応・検証が未完了という意味で、機能全体が未実装という意味ではない。

## 対象と完了の定義

- 同じ Vault・ノート・タスクに対して、検索結果、リンク先、日付、保存結果が一致する。
- 検索から読む、編集して保存する、関連ノートへ移る、元の作業へ戻る操作を通して完結できる。
- 本文だけでなく、埋め込み・プレビュー・エラー・空状態も同じ情報を伝える。
- SwiftUI のメニュー、ファイル選択、共有、キーボード操作など、macOS 標準の操作へ置き換えてよい。
- Native で既に使えるタスク一覧の直接編集やドラッグ操作などは維持する。
- 見た目の合格には、後述の同一条件での画面比較と操作確認が必要。トークン定義やビルド成功だけでは完了にしない。

対象外は SSG/SSR の配信処理、静的検索インデックス、公開データのロック、sitemap/SEO、デプロイ、モバイル専用 dock、ブラウザ拡張そのもの。
公開サイトの生成は対象外だが、既存の公開先を使う共有やノートの閲覧機能は除外しない。
新しい同期基盤や独自パーサーは追加せず、既存 Go API・SwiftUI・現在の figure-host を優先する。

## 現在の差分

根拠欄の `web:` は `web/src/`、`native:` は `native/Sources/` からの相対パス。

| 領域 | Native の現状と残る差分 | 根拠 | 対応 |
| --- | --- | --- | --- |
| Vault 選択 | 修飾 ID と横断検索はある。Vault 一覧 API、作業 Vault 選択、作成・日誌等へのスコープ伝播がない | web: `vaultScope.tsx`, `components/VaultSwitcher.tsx`; native: `TrackAPI/Client.swift` | C01–02 |
| 遷移・タブ | Notes 内のノートタブはあるがメモリ保持。Calendar/Browse/Tasks 等は別 reader/sheet に分かれ、Graph タブの選択は中心変更へ進む | web: `components/Shell.tsx`, `components/tabs/tabsStore.tsx`; native: `TrackApp/main.swift`, `TrackUI/ReaderViews.swift` | C03–04 |
| 編集保護 | 保存の etag/409 と一部の破棄確認はある。キーボード検索選択、MRU、新着等は直接 `reader.open` を呼び、同関数が draft を消す | web: `components/NoteEditor.tsx`; native: `TrackUI/ReaderViews.swift`, `TrackUI/ReaderModels.swift` | C05 |
| ライブ更新 | SSE 接続と再接続はある。30秒の待機は再接続用で、データ再取得の定期 polling ではない。Root と SearchReader に接続所有者がある | web: `hooks/useLiveEvents.ts`, `components/VaultActivityWatcher.tsx`; native: `TrackUI/LiveEvents.swift`, `TrackApp/main.swift`, `TrackUI/ReaderViews.swift` | C06 |
| 検索 | debounce、グループ、タグ、履歴、強調、キー操作、NEW はある。選択順と表示順、行への移動、Vault 文脈を通して照合が必要 | web: `components/SearchPanel.tsx`, `keys.ts`; native: `TrackUI/ReaderViews.swift`, `TrackUI/ReaderModels.swift` | C07 |
| アンカー・目次 | 目次や見出しリンクは本文へのジャンプではなく抜粋カード。目次は先頭12件。heading/block の解析も独自簡略処理 | web: `components/markdown/toc.ts`, `components/markdown/plugins.ts`; native: `TrackUI/ReaderViews.swift`, `TrackUI/ReaderModels.swift` | C08 |
| 関連ノート | パンくず、子、backlink、外部 Vault 警告はある。複数の一覧が先頭10件と操作できない `+N more` で終わる | web: `components/noteShared.tsx`; native: `TrackUI/ReaderViews.swift` | C09 |
| 既読 | shared milestone の参照と報告はある。MRU 等に bare ID の記録があり、ReadingStore は複数インスタンス。非表示・編集時の計時条件も要照合 | web: `reading.ts`, `components/NoteEditor.tsx`; native: `TrackUI/ReadingState.swift`, `TrackUI/ReaderViews.swift` | C10 |
| 日付・活動 | Calendar/Day/日誌/heatmap はある。On this day は閲覧中の日誌日付でなく今日を使い、journal だけを絞る。heatmap は notes.days 由来 | web: `components/noteShared.tsx`, `components/ActivityPanel.tsx`, `components/DayView.tsx`; native: `TrackUI/CalendarView.swift`, `TrackUI/BrowseViews.swift`, `TrackUI/ReaderViews.swift` | C11 |
| Markdown | GFM、脚注前処理、callout、数式、include はある。脚注往復、インライン配置、複雑な入れ子・アンカーの互換は別途検証が必要 | web: `components/MarkdownView.tsx`, `components/markdown/plugins.ts`; native: `TrackUI/MarkdownRenderer.swift` | C12 |
| プレビュー | aside の hover、Graph のリスト popover、固定幅280pt・6行抜粋の移動カードがある。本文リンクからの統一動作、完全な内容、resize が揃っていない | web: `components/preview/`; native: `TrackUI/ReaderViews.swift`, `TrackUI/GraphViews.swift` | C13 |
| 図表 | Mermaid/D2/DOT/drawio/mindmap/ECharts/viewspec/地図は figure-host で描画済み。注釈、出典、図内リンク、拡大等は種類ごとの照合が必要 | web: `components/markdown/`, `components/markdown/MarkerRail.tsx`; native: `TrackUI/FigureHost.swift`, `TrackUI/MarkdownRenderer.swift` | C14 |
| メディア | 画像、OGP、PDFKit、YouTube/Maps、text/HTML asset がある。Web の MediaFrame/PDF deck と操作・レイアウトを比較する | web: `components/markdown/MediaFrame.tsx`, `components/markdown/Embed.tsx`, `components/PdfDeck.tsx`; native: `TrackUI/MediaEmbeds.swift`, `TrackUI/MarkdownRenderer.swift` | C15 |
| クエリ表示 | track-view の list/board/gallery/calendar、dashboard、inline taskboard がある。サーバ解決後の表示だけでなくセル・リンク・更新操作を照合する | web: `components/markdown/QueryView.tsx`, `components/markdown/TaskBoard.tsx`; native: `TrackUI/MarkdownRenderer.swift` | C16 |
| グラフ | Canvas 力学配置、pan、pinch、reset、リストがある。Canvas は300ノード上限、hover はリスト側。選択後の閲覧導線も画面によって異なる | web: `components/GraphCanvas.tsx`, `components/GraphFullView.tsx`; native: `TrackUI/GraphViews.swift`, `TrackApp/main.swift` | C17 |
| タスク | 一覧、状態・日付変更、競合表示、D&D board はある。本文の inline board と全体 board は経路が別。本文上の表示との更新整合を確認する | web: `components/TasksView.tsx`, `components/markdown/TaskControls.tsx`; native: `TrackUI/TasksModel.swift`, `TrackUI/TaskBoard.swift`, `TrackUI/ReaderModels.swift` | C18 |
| 音声 | 認識・再開・全文編集・検索・作成・日誌保存はある。Web は選択文字列を検索し、Native は transcript 編集から自動検索する | web: `components/voice/VoiceView.tsx`; native: `TrackUI/VoiceInput.swift` | C19 |
| エージェント依頼 | Native の API/model/UI に対応がない。Web は説明・調査・更新、履歴、取消、再試行、続きの質問、回答保存まである | web: `api.ts`, `components/AgentRequestPanel.tsx`; native: `TrackAPI/Client.swift`, `TrackAPI/Models.swift` | C20–21 |
| コピー・共有 | 本文の Markdown/HTML コピー、wikilink、OS共有、X 導線はある。選択範囲コピーと、共有する URL/ノート参照の意味が同一ではない | web: `components/MarkdownView.tsx`, `components/markdown/copyRange.ts`, `components/ShareActions.tsx`; native: `TrackUI/PortableMarkdown.swift`, `TrackUI/ReaderViews.swift` | C22 |
| メタ・添付・編集 | 作成/編集/分割 preview/削除/メタ保存と画像アップロードはある。props はテキスト編集。フィールド・検証・画像選択後の操作を比較する | web: `components/NoteMetaDialog.tsx`, `components/NoteEditor.tsx`; native: `TrackUI/ReaderViews.swift`, `TrackAPI/Models.swift` | C23 |
| Neovim follow | 5秒 polling によるノート単位の追従。line/top_line の追従なし | web: `components/NoteEditor.tsx`; native: `TrackUI/ReaderViews.swift` | C24 |
| デザイン | light/dark・本文/preview文字サイズ・幅設定はある。一方で7機能タブ＋分割 sidebar、固定幅の抜粋カード、system font/色とテーマ色が混在 | web: `styles.css`, `components/ThemeMenu.tsx`; native: `TrackUI/Theme.swift`, `TrackUI/ReaderViews.swift`, `TrackApp/main.swift` | C25–30 |

## TODO

優先度は A: データ保護・日常作業の基盤・デザイン基準、B: 機能互換、C: 最終横断検証。
サイズは S: 局所的、M: 複数経路の設計・照合が必要、L: 段階分割が必要。
全体は L。以下は実行単位へ分けた項目で、先行項目と合格条件を併記する。
依存のない項目は順番を入れ替えてよい。デザインを最後まで先送りせず、C25を先に決めて各画面へ適用する。

### 1. データと日常操作の基盤

- [ ] [#A] C01 (M) 作業 Vault の選択を追加する。`/api/vaults` のモデル・取得と選択の保持、未登録化した選択の復帰、到達不能表示を実装する。検証: 起動 Vault と別 Vault を切り替え、再起動後も対象が正しい。
- [ ] [#A] C02 (M、C01後) 検索・新着・新規ノート・日誌・活動・グラフ等のスコープを Web と揃え、ノート起点のリンク・書込みは元の Vault を保持する。検証: 2 Vault に同一 ID/同名ノートを置き、検索→関連リンク→編集→保存で混線しない。
- [ ] [#A] C03 (M、C02/C25後) 全画面のノートを開く操作を作業中の reader/タブへ接続する。日付、タグ、タスク、graph、履歴から開き、戻る操作で元の画面・選択を復元できる。sheet を使う場合も「本文を開く」が機能する。
- [ ] [#B] C04 (M、C03後) 開いたノートタブを永続化し、タイトル更新・閉じる・現在のタブ・削除済みノートの復元を整える。検証: 複数タブを開いて再起動し、Web 同様に残り、存在しないノートで操作不能にならない。
- [ ] [#A] C05 (M) 未保存内容を失う遷移を塞ぐ。検索のクリック/Enter、MRU、新着、wikilink、preview、follow、作成、タブ終了、ウィンドウ終了を列挙し、共通の遷移境界で保護する。検証: draft を作って各経路を実行し、取消で保持され、409・通信失敗でも本文が失われない。連続 open の古い応答が新しい選択を上書きしないことも確認する。
- [ ] [#A] C06 (M) SSE の所有を整理し、Web 同等の再取得 fallback、復帰時 refresh、変更/削除/データ更新通知を接続する。検証: SSE切断中の外部更新→再接続で検索・本文・task・図表が追いつき、重複通知やdraftの上書きがない。

### 2. 読む・探す機能

- [ ] [#B] C07 (M、C02/C05後) 検索の表示順とキーボード選択順、複数語強調、タグ付加、履歴、選択行のスクロール、空/失敗/一部Vault失敗を揃える。検証: 同じクエリで title/body/file の順序・選択対象・遷移先が Web と一致する。
- [ ] [#A] C08 (M) 目次・heading/blockアンカー・脚注リンクを本文内の実位置へ移動させる。階層アンカー、同名見出し、日本語、他Vault、自ノート内リンクを既存の正準ルールへ合わせる。検証: 13見出し以上のノートでも全項目に到達し、抜粋表示だけで完了扱いしない。
- [ ] [#B] C09 (S、C02後) 目次以外の子・backlink・外部リンク一覧の省略分へ到達できるようにする。検証: 11件以上を用意し、展開/一覧遷移から末尾のノートも開ける。ラベルからの再解決ではなく保持しているID/Vaultを使用する。
- [ ] [#A] C10 (M、C02後) 既読状態を修飾IDで統一し、server の seen/read を画面間へ反映する。閲覧時間は表示中かつ読書中だけ加算し、NEW/read/stale の判定を Web に合わせる。検証: 同一IDの別Vault、別画面、アプリ非アクティブ、編集中、外部からの既読更新で誤判定しない。
- [ ] [#A] C11 (M、C02後) On this day を閲覧中の日誌の日付とVaultの agenda に直す。Calendar/Day/heatmap も activity/agenda の意味に照合し、空日の日誌作成・月次日誌・予定/期限・年境界を揃える。検証: 過去の日誌でその日に活動した通常ノートが現れ、今日のjournal一覧にならない。
- [ ] [#B] C12 (M、C08後) Markdown の構文互換を共通サンプルで確認し、差分を修正する。対象: 表、入れ子リスト、改行、コード、callout、複数行脚注と戻るリンク、複数のinline数式、includeの見出し/ブロック/範囲/only-contents。検証: 表示内容とリンク先が一致し、コード中の構文を誤変換しない。

### 3. プレビュー・図表・タスク

- [ ] [#B] C13 (M、C03/C08/C25後) 本文wikilink・aside・graphのpreviewを統一する。hover意図の待機、遅い応答の取消、未解決表示、本文/図表の描画、固定・移動・resize・閉じる・元画面保持を実現する。検証: 連続hoverで別ノートが出ず、固定後も内容と操作が失われず、画面外へ閉じ込められない。
- [ ] [#B] C14 (M) 既存の図表レンダラーをWebのサンプルごとに照合する。Mermaid/D2/DOT/drawio/mindmap、数式、ECharts/viewspec、地図について色・高さ・拡大縮小・source表示・出典/注釈/図内リンクを検証する。凡例やマーカーの情報を落とさず、失敗時に理由とsourceへアクセスできる。
- [ ] [#B] C15 (M、C13後) 画像・OGP・PDF・YouTube/Maps・text/HTML asset を、通常本文とpreviewの両方で揃える。検証: 比率、caption/source、拡大/別表示、PDFのページ移動、長いテキスト、ロード失敗、他Vault assetが機能する。HTML/外部リンクの既存の隔離も維持する。
- [ ] [#B] C16 (M、C02/C12後) track-query/track-view の全表示とdashboardを照合する。検証: 同じ応答でlist/table/board/gallery/calendarの列・値・group・cover・空状態が一致し、カード/セルのリンクとライブ更新が働く。取得と構文解決はGo側を使う。
- [ ] [#B] C17 (M、C03/C13後) GraphのCanvas上でもhover previewとノートを開く操作を提供し、pan/zoom/reset/中心切替/選択を整える。検証: Webと同じノード集合・関係が見え、300件上限で重要な情報が消える場合は明示と到達手段を設ける。大きなグラフで操作を止めない。
- [ ] [#B] C18 (M、C05/C06後) task一覧・本文checkbox・本文taskboard・全体board・Calendarの更新整合を揃える。検証: 5状態、予定/期限の設定と解除、優先度/完了、D&D、元ノートへの遷移、409時の保持と再取得を確認し、変更後の本文表示も古いまま残らない。

### 4. 入力・連携

- [ ] [#B] C19 (M、C02/C05後) 音声入力の選択範囲検索・一致箇所・ノート作成/リンク操作を Web と揃える。録音停止時の未保存部分だけの追記、録音再開、認識と手入力の併用、末尾追従と手動スクロールを確認する。検証: 停止→再開→停止や保存失敗後の再試行でも二重追記/欠落せず、権限拒否から復帰できる。
- [ ] [#B] C20 (M、C02後) エージェント依頼のAPI/モデルと、ノート・選択範囲から説明/調査/更新を依頼するUIを追加する。検証: intent、quote、note ID、Vault、更新対象、冪等キーをWebと同じ契約で送り、未対応操作・agent不在を表示する。既存サービスを再利用する。
- [ ] [#B] C21 (M、C20/C06後) 依頼履歴・進行状態・取消・再試行・続きの質問・回答表示/ノート保存・更新結果/競合表示を追加する。検証: pendingから完了/失敗/競合まで辿れ、閉じて開き直しても確認でき、保存/更新を重複実行しない。
- [ ] [#B] C22 (M、C02/C12後) 本文全体と選択範囲のMarkdown/リッチコピー、タイトル/wikilink/パス/共有リンクを揃える。検証: 表の部分選択、改行、include、別Vaultの参照、HTML貼付を確認する。公開先が設定済みなら共有は実際のURLを使い、wikilinkを公開URLと見せない。
- [ ] [#B] C23 (M、C02/C05後) メタ編集のtitle/tags/flags/icon/description/image/props、画像選択とupload、編集preview/splitをWebと照合する。検証: 型付き値、削除/空値、重複title、不正入力、画像取得失敗、保存競合を通し、本文や未知のメタデータを落とさない。
- [ ] [#B] C24 (M、C08/C05後) Neovim follow のline/top_lineとノート遷移を接続する。検証: 行移動とノート切替に追従し、OFFで止まり、編集中のdraftを破棄しない。

### 5. Web 同等以上のデザイン

- [ ] [#A] C25 (M、他の画面改修の前提) Webの「一枚の読書面＋compact rail」を基準にNativeのshellと情報配置を決める。主要画面の比較用レイアウトを作り、機能タブ/sidebar/toolbarに分散した導線を整理する。合格: 現在位置と主操作が明瞭で、同じウィンドウ幅で本文・関連情報がWeb以上に読みやすい。OS標準操作は保持する。
- [ ] [#A] C26 (M、C25後) 日本語の文字組み、body/title/metaの階層、行高、段落/見出し余白、本文の約40emのmeasureと図表の広い幅を揃える。IBM Plex Sans JP等のWeb基準に比較可能なフォントを適用する。合格: 長い日本語・英数・表・コードが読め、幅設定や文字拡大でasideと本文が衝突しない。
- [ ] [#A] C27 (M、C25後) 色・罫線・角丸・選択・focus・NEW/flag・dangerの使い方を画面全体とfigure-hostで統一する。合格: light/dark双方で色に頼らず状態を区別でき、本文・補助文字のコントラストをWeb基準から下げない。system色やmaterialの採用箇所も比較する。
- [ ] [#B] C28 (M、C25–27後) 検索/新着/履歴、日付/活動、タグ/階層、task、graph、音声、依頼画面の密度と余白を揃える。合格: 同じデータでtitle・meta・badge・主操作を迷わず読み取れ、重複したID表示や不要な枠が本文を圧迫しない。
- [ ] [#B] C29 (M、C25–27後) preview、図/画像拡大、メタ編集、設定、通知の浮動面を整える。合格: 開閉とfocus復帰、境界、サイズ変更、スクロール、テーマ切替が一貫し、長い内容や複数previewでも主操作が隠れない。
- [ ] [#B] C30 (M、C25–29後) 狭い/広いウィンドウと文字サイズ最大時のレイアウト、キーボード完結、VoiceOverの名前と順序、focus表示、動きの低減を確認する。合格: macOSで同じ主要作業をマウスなしでも完了でき、横にはみ出して操作不能になる画面がない。

### 6. 横断検証と完了判定

- [ ] [#C] C31 (M、C01–30後) 共通Vaultによる機能比較と画面比較を実施し、本書を実測結果へ更新する。未確認・暫定回避を完了にせず、下記の証跡を残す。

## 検証方法

| 比較 | 条件 | 合格条件 |
| --- | --- | --- |
| 日常シナリオ | 2つのVault、同名/同ID、長い日本語ノート、13見出し・11件以上のbacklink、タスク5状態 | 検索→読む→関連ノート→戻る→編集→保存→再起動まで完結し、データと行き先が一致 |
| 描画 | `docs/help/` の構文・図表・埋め込みサンプルを共通入力に使う。不足する境界例だけ追加 | 本文、図、表、リンク、脚注、include、previewに情報の欠落や意図しないsource表示がない |
| 競合・更新 | CLIからの変更/削除、同時保存、SSE切断と復帰、asset失敗 | draft喪失・二重保存・古い画面の放置がなく、失敗内容と再試行手段が分かる |
| 音声・連携 | 短い/長い認識、手入力併用、停止/再開、agent成功/失敗/競合、follow切替 | 文字列・参照・依頼履歴・保存結果が欠けず、ユーザーの作業が中断後も続けられる |
| 見た目 | 同じノート・文字サイズ・light/dark。コンテンツ領域を900×650、1280×800、1600×1000相当で比較 | 本文幅、文字階層、余白、罫線、情報密度、focus、図表の読みやすさでWebを下回らない |
| 浮動面と状態 | 上記にpreview、メタ編集、loading/empty/error/conflictを重ねて撮影 | 切れ・重なり・読めない文字・隠れた操作がない。主要導線は操作録画または手順でも確認 |

既存の `native/Tools/VerifyFixtures` はAPIのdecode中心で、画面・操作互換の証明にはならない。
実装時は既存のWebテストとfixtureを再利用し、変更したロジックの最小回帰チェックを残す。
Nativeのbuild/fixture検証方法は `native/README.md` に従う。
完了報告には対象commit、比較データ、画面キャプチャ、操作結果、残差分を付ける。

参照仕様: [Web](web.md)、[Native](native-macos.md)、[Design](design.md)。
旧Native仕様のMVP制限は今回の互換対象を縮める根拠にしない。
