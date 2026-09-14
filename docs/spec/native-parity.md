# Web / Native 互換対応リスト

更新日: 2026-09-15。比較基準: `origin/main` の `e24c235`。
対象はライブ版 `track web` と macOS Native。基本機能・データの意味・操作結果を互換にし、見た目と操作性を Web と同等以上にする。
本書は現在の残対応表であり、以前の MVP 時点の未実装一覧を置き換える。

## 対象・状態の読み方

SSG/SSR の生成・配信、静的検索インデックス、sitemap/SEO、デプロイ、モバイル専用 dock、ブラウザ拡張そのものは対象外。
音声、図表、PDF、地図、依頼、複数タブ、Neovim follow、ライブ版の選択コピー/参照操作は対象に含む。
公開URL共有は現状Webの静的専用面だけにmountされるため、今回の必須互換差分から除外する（`W: components/NoteReaderStatic.tsx:92`）。公開基盤の追加は不要。
macOS 標準のメニュー・共有・ファイル選択・ウィンドウ操作へ置き換えてよいが、情報や必要な操作を失わないこと。

| 状態 | 意味 |
| --- | --- |
| 実装済・確認待ち | 対応コードと回帰チェックがある。実機の一連の操作、または Web との画面比較が未完了 |
| 残実装 | 現行コードで情報・操作・描画の欠落を確認した |
| 配置差・要判定 | 両方に機能はあるが、構成・寸法・操作方式が異なる。同等以上かを比較して採用・修正を決める |
| 観測済・原因切分中 | 実アプリで問題を観測したが、特定の実装だけを原因とは確定していない |

優先度は **P0: 日常操作を妨げる不具合、P1: 情報・操作の欠落または主要画面、P2: 全画面の統一・横断検証**。
「確認待ち」を「未実装」と数えない。新しい欠落を確認していない機能を、再実装の TODO に戻さない。
C01–31 は従来の追跡番号として維持する。実行時は下の UI/F/V 番号を単位にして、合格した範囲だけを完了にする。

## 取り込み済みの基盤

#265–270 は比較基準までに main に取り込み済み。旧記述の「統合中」「main merge 未実施」は解消した。
以下は既存コードの到達点であり、全画面のデザイン合格を意味しない。
根拠の `N:` は `native/Sources/`、`W:` は `web/src/`。行番号は比較基準のもの。

| C番号 | 現行 Native の到達点 | 現在残す作業 |
| --- | --- | --- |
| C01–02 | `VaultScope` の選択・保存・復帰、Vault API、修飾 ID と元 Vault に沿う読み書き | V01 の実 Vault 往復確認。Vault選択の追加実装は不要 |
| C03–05 | `WorkspaceNavigation` の共通 reader/戻る、`NoteTabs` の永続化、遷移・close/quit の draft 保護 | 同名別Vaultタブの識別は UI12、V01 の再起動・focus・scroll・OS終了。別画面 reader の全面作り直しは不要 |
| C06 | `LiveEventPoller` の所有者統一、SSE・30秒 fallback・復帰 refresh | V02 の外部更新/切断/復帰。非表示画面の更新コストは UI01 |
| C07 | 検索順・キーボード選択順・複数語強調・選択行スクロール・履歴 | 検索面の配置は UI02、複数 Vault/空/失敗操作は V01 |
| C08 | 目次の全項目、heading/block/脚注の本文ジャンプと同一ノートの移動 | 入れ子の位置精度・複雑な構文は F01。先頭12項目/抜粋だけという旧差分は解消 |
| C09 | children/backlink/外部 Vault の全件導線 | 長い関連一覧の配置は UI03。props の切捨ては F02 |
| C10–11 | 共有既読、非表示・編集中の計時抑制、閲覧中の日誌の日付/Vaultに基づく On this day | Calendar のコストは UI01、日付条件・活動表示は V01/UI07 |
| C12 | GFM・コード保護・callout・脚注処理・本文アンカー | include本文・複数inline数式等は F01 |
| C13 | 全文NSPanel、350ms hover待機、取消・古い応答抑止、pin/移動/resize/close、元reader保持 | 本文内hoverとswap/collapseは UI05。固定280pt/6行抜粋という旧差分は解消 |
| C14–15 | figure初回描画・source・再試行・凡例/provenance・注釈全文、添付全文、HTML分離、PDFページ操作 | 図表注釈位置・HTML options等は UI06/F03、全種類の実体比較は V03 |
| C16 | query list/board/gallery/calendar・dashboard の入口、compact list・タイトル非表示カードの遷移 | 全種類の値/配置/更新を F04 で照合 |
| C17 | Canvas hover/click、pan/pinch/reset、非同期layout、300件上限表示、全件ListとCenter in canvas | 実Canvasの配置・操作・大規模データ確認は UI08 |
| C18 | タスク一覧/board/本文の更新、ノート固有board、raw行対応・etag/期待状態の保護 | include内操作は F01、初期一覧条件は F06。画面横断更新は V02 |
| C19 | 選択検索、録音再開・手入力保持、差分保存・重複防止、認識訂正の保持 | 実マイク/IME/手動scrollは V04、画面密度は UI09 |
| C20–21 | 依頼API/履歴/取消/再試行/続き/回答保存/更新競合、選択文字列quote | 実agentとの一巡は V04、依頼面の狭幅・文字拡大は UI09 |
| C22 | 全文/draftのMarkdown・HTMLコピー、NSTextView選択コピー、引用依頼、Vault付き参照、OS共有 | 行範囲参照のコピーと複数block選択は F05/V05 |
| C23 | メタの取得時etag/ID保持、未知YAML保持、失敗時入力保持、画像選択、edit/preview/split | メタ/画像/OS共有の実機操作は V05。propsの自由記述YAMLはWebにもあり、型付き専用UIの欠如とは扱わない |
| C24 | line/top_lineをraw/rendered対応から包含ブロックへ移動、OFF/非表示時取消、draft保持 | 内部行精度は F01、実Neovimの往復は V04 |
| C25–27 | compact dock、日本語フォント、本文/図表の幅、見出し余白、light/darkトークンとコントラスト | shell/aside/全画面への適用と比較は UI02–04/UI07–10 |
| C28–30 | 各画面と浮動面は存在し、個別操作・一部アクセシビリティ属性がある | 全画面の密度、最大文字サイズ、focus/VoiceOver/reduced motionは UI07–11/V06 |
| C31 | Native部品画像・モデル回帰・WebKit/PDFKitの個別証跡がある | Web同一条件の対になる全画面画像と操作結果を V06 へ追加 |

## UI の残対応

数値の一致だけで合格にしない。Native の置換が同等以上なら、相違の採用理由と実測を残して完了にしてよい。

| ID / 優先度 / C番号 | 状態・具体的な差分と根拠 | 次の対応・合格条件 |
| --- | --- | --- |
| **UI01 / P0 / C11,C28,C30** | **観測済・原因切分中**。Notes表示中のウィンドウ移動に停止があり、CPU sampleではCalendarの日別集計が現れた。`N: TrackApp/main.swift:105` は訪問済み画面をopacityで保持。`N: TrackUI/CalendarView.swift:53,259` は各日セルのタイトル/件数/active判定で全notesを3回走査。実バイナリと合成VaultでもCalendar→Notes後の移動遅延を再現した。[PR #272](https://github.com/ttak0422/track/pull/272)で日別集計を索引化し全件走査の負荷を解消したが、実ドラッグの遅延とSwiftUIのlayout更新は残る。最小windowではtoolbarを含めても再現しない | 日別集計の計算量と表示更新の発火条件を切り分ける。同じデータ・軌跡の実ドラッグで標準window同等に追従し、秒単位の座標停止をなくす。Calendar表示/月移動、元画面・draft保持も確認する |
| **UI02 / P1 / C07,C25** | **配置差・要判定**。Webは浮動railと中央検索palette（`W: components/Shell.tsx:87`, `styles.css:273,590`）。Nativeは左端固定dock＋toolbar、検索時にNavigationSplitViewを開く（`N: TrackApp/main.swift:101,171`, `TrackUI/ReaderViews.swift:116`）。新着/履歴/日誌/検索の入口の配置も異なる | 検索→本文→戻るの主要導線を比較し、本文が圧迫されない配置にする。900px相当の幅で主操作が隠れず、検索取消後のfocusと読書位置を保持する |
| **UI03 / P1 / C09,C25,C26** | **配置差・要判定**。Webの広幅asideはsticky・独立縦scroll、本文とのgap60px（`W: styles.css:5016,5031`）。Nativeは本文とasideを同じScrollView内に置き、広幅切替は`1080×min(scale,1.3)`、gap40pt、aside最大340pt、外側padding24pt（`N: TrackUI/ReaderViews.swift:1222`） | 長文・全目次・多数backlinkでも関連操作へ容易に戻れる構成を決める。長い本文を末尾まで読んでもasideが操作不能にならず、図/本文との重なり・不要な空白がない |
| **UI04 / P1 / C26–27** | **配置差・確認待ち**。Web本文はIBM Plex Sans JP、16px/1.85、約40em、BudouX境界を使う（`W: styles.css:86,91,2832`）。Native本文はHiragino Sans、16ptと0.85em追加行間、Normal640×scale、段落/見出しごとの幅・余白（`N: TrackUI/Theme.swift:228,244`, `TrackUI/MarkdownRenderer.swift:695`）。定義だけでは同じ行高・改行にはならない | 同じ日本語/英数/URL/コード/表を同じ文字サイズで対比。本文の行長、禁則、見出し前後の間隔、表/コードの読みやすさを確認。既存フォント指定を追加し直す作業ではない |
| **UI05 / P1 / C13,C29** | **残実装＋配置差**。Web本文WikiLink自身にhoverがあり、浮動面はswap/collapseも持つ（`W: components/preview/WikiLink.tsx:36`, `components/preview/FloatingWindow.tsx:43,53,97`）。Nativeの共通previewはaside/graph/末尾link rail。本文中のTextリンク、queryカード（`N: TrackUI/MarkdownRenderer.swift:1350,1559`）、props値（`N: TrackUI/ReaderViews.swift:1624`）に同じhoverがなく、NSPanelにswap/collapseはない（`N: TrackUI/MarkdownRenderer.swift:83,831`, `TrackUI/NotePreview.swift:51,148`） | 本文/query/propsのリンク位置から遅延hover・focusで同じ全文previewに到達できるようにする。pin/移動/resize/closeは保持し、swap/collapseの採否を明示。連続hover、遅延応答、複数panel、画面境界、元のdraft/focusを実機で確認する |
| **UI06 / P1 / C14–15,C29** | **残実装＋配置差**。Webのbox注釈は軸位置に沿うrail（`W: components/markdown/MarkerRail.tsx:24,82`）。Nativeは注釈全文の下部リスト。画像/PDF拡大はmodalで、Webのpin/floatとは異なる（`N: TrackUI/FigureEvidence.swift:32`, `TrackUI/MediaEmbeds.swift:369`; `W: components/markdown/EChartsBlock.tsx:112`, `components/markdown/MediaFrame.tsx:46`）。詳細は[図表証跡](../evidence/native-parity/figures-media.md) | zoom/pan後も注釈が該当位置に対応し、範囲外を適切に処理する。画像/PDFを本文横にpinして移動/resize/closeでき、遷移後も元Vaultのassetを保持する。凡例/source、比率・caption・PDFページ操作も照合する |
| **UI07 / P1 / C11,C18,C28** | **配置差・要判定**。Web Tasksは日付ページに沿うcompact行（`W: components/TasksView.tsx:29`）。NativeはOpen only＋List/Board、直接状態/日付編集（`N: TrackUI/TasksView.swift:18`）。Calendarのセル・活動図、Browseの階層/タグは独立のListや固定12ptのheatmapセル（`N: TrackUI/CalendarView.swift:259`, `TrackUI/BrowseViews.swift:124,348`） | Nativeの直接編集を維持し、title/meta/date/stateの優先順位を揃える。長いタイトル、多数タスク、空月、年境界、文字32ptで件数・状態・主操作が読める。全ノートを切り捨てず開ける |
| **UI08 / P1 / C17,C28** | **実装済・確認待ち**。Canvas hover/選択・300件cap・全件Listは実装済（`N: TrackUI/GraphViews.swift:62,214,397,430`）。WebはoverviewとCanvas上のpreviewを持つ（`W: components/GraphFullView.tsx:35,65`）。既存bitmap証跡ではGPU Canvasが写らず、グラフ自体の外観比較は未了 | 実アプリでノード/線/ラベル、密集部、pan/pinch/reset、選択・中心変更を確認。cap外ノードをListから中心表示/本文へ開け、操作中に主スレッドを長時間占有しない |
| **UI09 / P1 / C19–21,C28,C30** | **配置差・要判定**。Voiceは認識文と検索結果、Agentは入力と履歴/結果を分ける。Native依頼面は最小660×560、左右pane最小260/340pt、標準fontと赤errorを使用（`N: TrackUI/AgentRequestsView.swift:31,70,91,136`, `TrackUI/VoiceInput.swift:406,428`）。Web側は`components/AgentRequestPanel.tsx:49`、`components/voice/VoiceView.tsx` | 長文の指示/結果/認識文でも送信・保存・取消が隠れないようにする。狭いwindow/32ptで入力と結果を読める構成を比較し、処理中・競合・agent不在/権限拒否も確認する |
| **UI10 / P2 / C27–29** | **配置差・確認待ち**。共通paletteと本文スケールはあるが、補助画面に`.font(.caption/.body)`・`.secondary/.red`・materialが残る（例`N: TrackUI/BrowseViews.swift:121,221`, `TrackUI/AgentRequestsView.swift:136`）。Web設定はrail内menu、NativeはSettingsのForm/Stepper。範囲13–32・幅880/1280/fullは共通（`W: components/ThemeMenu.tsx:21`, `N: TrackApp/main.swift:395`, `TrackUI/Theme.swift:259`） | system色の使用を一律禁止せず、本文との階層・danger/focus・補助文字のcontrastを全画面で比較。設定を変更すると本文/preview/図/一覧が一貫して追従し、見分けを色だけに依存しない |
| **UI11 / P1 / C30–31** | **実機確認待ち、一部対応要調査**。キーボード操作・accessibilityLabel・非表示画面のAX除外は個別実装済。全画面のVoiceOver順序/最大文字/閉じた後のfocusは未比較。Webにreduced-motion規則（`W: styles.css:465`）、Nativeの独自transition/animationにその対応があるとは確認できない（例`N: TrackUI/ReaderViews.swift:913`） | V06の同一条件でキーボードのみの主要操作、VoiceOverの名前/順序/重複、focus可視化、Reduce Motionを確認。未確認のまま「アクセシビリティなし」または「対応完了」としない |
| **UI12 / P1 / C04,C13,C25** | **残実装＋配置差**。Nativeのタブはtitle/close中心でVaultラベル・タブからのfloat導線がない（`N: TrackUI/ReaderViews.swift:437`）。WebはVaultラベル、FloatNoteButton、overflow、中クリックclose（`W: components/tabs/TabBar.tsx:66,97,123,132`）。タブの永続化とNSPanel自体は実装済 | 同名別Vaultの2タブを識別でき、タブから既存previewを固定できるようにする。20タブでも現在位置・選択・閉じるに到達できる。中クリック/overflowはmacOS代替操作の使いやすさと併せて判定する |

## 情報・操作の残差分と種類別確認

| ID / 優先度 / C番号 | 現在の差分・根拠 | 合格条件 |
| --- | --- | --- |
| **F01 / P1 / C08,C12,C18,C24** | include本文は`Text(include.lines.joined...)`で、構文描画・include内task操作がない（`N: TrackUI/MarkdownRenderer.swift:1603`）。Webは再帰MarkdownViewとsource line/etag付きtask操作（`W: components/MarkdownView.tsx:344,379`）。Nativeのinline数式は行内でfigureへ分割（`N: TrackUI/MarkdownRenderer.swift:382,448`）。リスト/表/コード内のanchor/followは包含block先頭 | includeのheading/block/range/only-contents、表/リンク/図/checkboxを同じサンプルで比較。一続き範囲のtaskだけ元ID/Vault/行/etagへ結線し、複数範囲/etag不一致は無効にする。hostノートを書き換えず、anchor衝突とネストincludeの展開方針を保護。複数inline数式、複数行脚注、参照リンクの内容と位置を確認する |
| **F02 / P1 / C09,C23** | props表示は先頭10組、captionも先頭10件（`N: TrackUI/ReaderViews.swift:1620,1671`）。さらに同キーのlink値を`, `で結合し単一リンクへ渡す（同`:1657,1624`）。Webは全キーを表示し各link値を別WikiLinkにする（`W: components/noteShared.tsx:498,502`）。編集用YAML欄の問題ではない | 11件以上のキー/値にも展開または一覧から到達できる。link配列[A,B]はA/Bを個別に開き、`A, B`という別タイトルを解決しない |
| **F03 / P1 / C15** | HTML embedの`:height`/`:frame`が未対応（`N: TrackUI/MarkdownRenderer.swift:205,1700`）。Web HTML optionsは`W: components/markdown/Embed.tsx:29,45`。Nativeの現状・未実装は[図表証跡](../evidence/native-parity/figures-media.md) | 指定値で高さ/枠を制御し、外部URL分類と失敗時のリンクを確認。既存のHTML隔離・非永続storeを維持する |
| **F04 / P1 / C16** | queryの全view/dashboardは入口があっても、全セル・group・cover・軸・更新の対応は未確認（`N: TrackUI/MarkdownRenderer.swift:1248`, `W: components/markdown/QueryView.tsx`） | 同じGo render応答でlist/table/board/gallery/calendar/dashboardを比較。値や型・空状態・group・リンクを落とさず、外部更新で追従する。具体的な欠落を確認した種類ごとに修正する |
| **F05 / P1 / C22** | Webは選択に対して元ノートpath＋行範囲をコピーするCopy rangeを持つ（`W: components/MarkdownView.tsx:319`）。Nativeは選択text/Markdown/HTMLのみで、行範囲情報とrange actionがない（`N: TrackUI/NoteSelection.swift:10`, `TrackUI/ReaderViews.swift:1091`）。AX fallbackがplain textだけなら元の装飾も復元できない（同`NoteSelection.swift:47`） | 読取選択から正しい元path/開始終了行の参照をコピーできるようにする。既存のMarkdown/rich copy・quoteを維持し、複数block選択の精度はV05で確認する。静的専用の公開URL共有はこの必須項目へ混ぜない |
| **F06 / P1 / C18** | Nativeの初期値`showOpenOnly=false`は完了を含むdated一覧を取得（`N: TrackUI/TasksModel.swift:21,47`）。Web Tasksはopen query（`W: components/TasksView.tsx:16`） | 初期表示の集合・順序をWebのopen datedと一致させる。Native追加のall切替・直接編集は維持できる |

## 残る実機・横断確認

| ID / 優先度 / C番号 | 確認シナリオ・合格条件 |
| --- | --- |
| **V01 / P1 / C01–05,C07–11** | 同名/同IDを持つ2 Vaultで検索→本文→関連/Calendar/Tasks/Graph→戻る→編集→保存→タブ再起動。選択・Vault・既読・draft・focus・scrollが混線しない。過去日誌、空日、月次日誌、年境界も確認 |
| **V02 / P1 / C05–06,C18** | CLIで変更/削除、SSE切断/復帰、409、タスクの行移動を発生させる。本文/一覧/board/Calendarに反映され、draft喪失・古い行への書込み・二重保存がない |
| **V03 / P1 / C14–17** | 実Mermaid/D2/DOT/drawio/KaTeX/ECharts/Leaflet、オフライン、画像/PDF/YouTube/Maps/HTMLを本文とpreviewで確認。各表示のsource/注釈/図内リンク/拡大/失敗/再試行が使える。模擬chart engineの成功だけで全rendererを完了にしない |
| **V04 / P1 / C19–21,C24** | 実マイク・権限拒否・IME・録音再開・手動scroll・保存失敗、実agentの成功/取消/失敗/競合、実Neovimのノート/行移動を確認。内容・依頼・保存の欠落/重複がない |
| **V05 / P1 / C22–23** | SwiftUI本文の実マウス選択、複数block/include/表の部分選択と外部アプリへの貼付、OS共有、画像選択/upload失敗、実engineのメタ競合を確認。取得時etag/ID、未知props/YAML、入力を保持する |
| **V06 / P2 / C25–31** | 同一入力・同一表示領域900×650/1280×800/1600×1000、文字16/32、light/darkでWeb/Nativeを対で撮影。全主要画面とloading/empty/error/conflict、浮動面を含む。本文幅・余白・文字階層・情報密度・focus・VoiceOver・Reduce Motion・操作到達性についてUI02–12を判定する |

## 証跡と完了更新

- [読書面・アンカー・Follow・回帰チェック](../evidence/native-parity/README.md): Native本文部品のlight/dark・3幅と長文ジャンプ。アプリ全体やWebとの対比較の証明ではない。
- [プレビュー・Graph](../evidence/native-parity/previews.md): 実NSPanelの460×460/680×600画像とモデル/操作チェック。GPU Canvasの外観は未確認。
- [図表・メディア](../evidence/native-parity/figures-media.md): 実WebKitの生成SVG、模擬chart engine、生成PDF。外部CDN全体は未確認。
- [ノート操作](../evidence/native-parity/note-actions.md): NSTextView、専用pasteboard、mock HTTPのコピー/quote/メタ検証。実画面の複数block選択・実共有・全外部貼付は未確認。

本更新はソース照合と既存画像の確認であり、全アプリの再撮影・全テストの再実行ではない。
完了時は対象ID、実装commit、比較入力、操作結果、対になる画像、残る制限を証跡へ追加し、本表の該当行を更新する。
ビルド/モデルテスト成功を視覚・操作合格の代わりにしない。手順は [Native README](../../native/README.md)。

参照仕様: [Web](web.md)、[Native](native-macos.md)、[Design](design.md)。
