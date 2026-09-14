# Native 互換対応の検証記録

2026-09-14、`feat/native-parity-integration` で worktree ごとの変更を統合して検証。
Web 基準は `d8e31bb`。検証対象の実装末尾は `4083667`。
CLT/SDK macOS 26.5、SwiftPM debug buildで検証した。

## 読書面の描画

[共通入力](reader-sample.md) を実際の `GFMBody` / `NSHostingView` で描画し、各ビュー自身の bitmap を保存する。
スクリーン全体のキャプチャではない。
本文の約40emの幅、日本語の行間、見出し、コードの広い幅、表、query gallery を対象にする。

| 大きさ | Light | Dark |
| --- | --- | --- |
| 900×650 | [画像](20260914/reader-light-900x650.png) | [画像](20260914/reader-dark-900x650.png) |
| 1280×800 | [画像](20260914/reader-light-1280x800.png) | [画像](20260914/reader-dark-1280x800.png) |
| 1600×1000 | [画像](20260914/reader-light-1600x1000.png) | [画像](20260914/reader-dark-1600x1000.png) |

```sh
scripts/check-native-design.sh --snapshots docs/evidence/native-parity/reader-sample.md docs/evidence/native-parity/20260914
```

画像は本文コンポーネントの確認用で、アプリ全体のレイアウトやWebとの比較合格を示すものではない。
同一入力によるWeb画像、全画面・最大文字サイズ・キーボード・VoiceOverの比較は未実施。

## 本文ジャンプとFollow

`f62fd63` に対応するアンカー/Follow実装を、担当worktreeで長い日本語ノートを実際に `NoteReaderView` へ読み込んで確認した。

- [先頭](20260914/anchor-before.png) → [最終見出しへのジャンプ](20260914/anchor-after.png)
- [脚注へのジャンプ](20260914/anchor-footnote.png)
- [Followのtop_line=47で段落23が上端へ移動](20260914/anchor-follow.png)

目次の重複表示を除去した後の画像。
リスト・表・コード内の細かな行位置は包含ブロックの先頭に移動するため、行の完全一致は未対応。

## 実行可能な回帰チェック

手順は [Native README](../../../native/README.md)。CLT環境なのでXCTestを追加せず、SwiftPMの実行ターゲットを使用する。
HTTP応答の順序、409、通信失敗は各ターゲット内のURLProtocolで制御し、実ユーザーのノートや実エージェントへ書き込まない。

| チェック | 対象 |
| --- | --- |
| VerifyFixtures | APIモデルの基本decode |
| VerifyVaultScope | 同一IDの別Vault、選択保持・復帰、スコープ付きAPI |
| VerifyReader | draft破棄取消、失敗・競合・遅い応答、タブ復元、検索順、本文アンカー |
| VerifyReading | Vault別既読、server milestone、UTF-16閾値、過去日誌のagenda |
| VerifyNavigation | 共通reader、元画面への戻り、履歴、dirty時の遷移取消 |
| VerifyAgentRequests | 数値IDとVault、作成/保存の冪等性、再試行、取消、続き、更新競合 |
| VerifyVoice | 選択のみの検索、認識と手入力、停止/再開、差分保存、応答喪失、競合保持、IME中断・認識訂正の欠落防止 |
| VerifyTasks | ノート固有board、一覧の出入り、etag/期待状態、古い行の拒否、本文更新、描画失敗 |
| VerifyDesign | 日本語段落の実寸、本文幅、フォント、light/darkの補助文字コントラスト、gallery幅 |
| check-native-anchors.sh | 日本語/重複見出し、block、脚注往復、コード保護、行番号 |
| check-native-live-events.sh | SSE切断時のpoll、停止、復帰、通知の重複防止 |
| Go request package | 保存結果で起動Vaultの空ラベルを保持 |
| Go TestTaskEndpoints / TestTaskWriteRejectsShiftedSameStateWithStaleETag | etag必須、古い行への書込み拒否 |

音声の手編集と認識訂正の対応が曖昧な場合、`[認識の訂正候補・要確認]` ブロックとして候補全文を一度だけ保持する。候補も保存対象なので後から手編集で整理できる。

API・モデル検証とbuild成功は実機の操作確認を代替しない。
実マイク、IME、OSのウィンドウ終了/アプリ終了、画面間のスクロール復元、外部サービスの実実行は未確認。
SSG生成・配信・公開基盤は対象外。

## 確認用アプリ

`/private/tmp/track-native-parity-preview/Track.app` に、統合版のdebug実行ファイルと同じソースからビルドしたGo helperを配置。
Info.plistと動的リンク先を確認済み。配布用の署名・公証やアプリ全体の起動操作は未実施。
ソースから通常のrelease bundleを作る手順は `make native-app`。
