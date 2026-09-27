# Native note actions — C22 / C23

2026-09-14。実装baseは `84e71d0`。メタデータ競合・未知キー保存のbackendは PR #267 (`2e2b17a`) に依存する。Nativeの統合先はpreview対応 PR #269。

## 実装

- 本文コピーとOS共有は編集時のdraftを参照する。本文のGFM HTMLコピーは、MarkdownUIで既に使用しているcmark-gfm 0.8.0を直接使用する。MarkdownUIの公開HTML出力で `<th>` / `<td>` が失われることを再現したため、この経路だけparserを直接呼び出す。新しいライブラリやバージョンは導入していない。
- 選択コピーはNSTextViewの実際の選択範囲を取得し、ソース編集では選択Markdownをそのまま、リッチテキストでは選択された属性・リンク・表セルを保持する。取得した選択文字列をAgentRequestTarget.quoteへ渡す。別ノート・モードへの変更時や、本文外へキーボードでフォーカスが移った際は選択を消去する。
- 選択取得はクリップボードを変更しない。SwiftUI用fallbackは同一processのaccessibilitySelectedTextを読むだけで、取得できなければ選択操作を無効にする。
- Copy wikilinkはVaultを含むIDで区別する。OS共有はタイトルと本文を共有する。公開URLを提供するAPIがないため、API baseやwikilinkを公開URLとして提示せず、X導線を表示しない。
- メタフォームは取得時のetagとノートIDを保持する。400/409で入力を保持し、別ノートへ移動済みのフォームからの保存を拒否する。propsのYAMLはそのまま送信し、型やスキーマの検証はWebと同じengineに任せる。Journalタイトルを編集不可にし、画像選択をengine対応のPNG/JPEG/GIF/WebPへ揃える。読み込み失敗時のRetry/Cancelも追加した。

## 実行チェック

CLT macOS 26.5 SDK / Swift 6で `VerifyNoteActions` と `TrackApp` をビルドした。SwiftPMの入れ子sandboxがこの環境では起動できないため、buildは `--disable-sandbox` とworktree内のcacheを使用した。

`VerifyNoteActions` は次を検証した。

- NSTextViewの日本語・改行を含む部分選択、空選択、太字の部分選択。
- 専用の一時NSPasteboardへ書き込まれるMarkdown・HTML・plain text。一般クリップボードは変更しない。
- 非表示NSWindow内で本文から別の検索欄へfirstResponderを移した場合、引用を消去すること。macOSのvisibleRectがbounds外へ広がるケースもboundsとの交差で制限する。
- 表の一部のセル、GFM表のHTML、include/wikilinkのportable化、`<br>`とコード中のリテラル、打消し線、task checkbox。
- 未指定/非HTTP共有URLの拒否と、明示された実URLを変更しないX intent生成。
- mocked HTTPでの型付きprops文字列・空props・etag送信、400の型エラー/重複タイトル、409の再試行、異なるVaultの同じbare IDへの保存拒否、本文保持、引用とVaultのAgent依頼への引渡し。

pasteboardサービスは通常sandboxから利用できないため、検証バイナリの実行のみsandbox外で行った。

## 未確認・残対応

- MarkdownUIの実画面をマウスで選択した際のaccessibility取得、複数描画block/includeにまたがる選択、元Markdownの見出し・リスト構文への完全な逆変換は未確認。accessibility fallbackがplain textのみを返す場合、元の装飾は復元できない。C22の全面完了とはしていない。
- Confluence等の外部アプリへの実際の貼り付け、OS共有パネル、画像ファイル選択・アップロード失敗時のGUI操作は未確認。
- 公開base URLの取得契約がないため公開URL共有は未対応。SSGの追加実装は行っていない。
- nativeのメタHTTPチェックはmockを使用する。実engineの未知YAMLキー保持・型付きprops検証・etag競合保証はbackend PR #267の検証と合わせて確認する。etagを返さない旧serverとの接続では競合検知は提供できない。
