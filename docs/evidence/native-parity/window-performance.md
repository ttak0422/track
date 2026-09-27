# Calendar集計とウィンドウ移動の性能

2026-09-15、macOS 26.4.1 / arm64、Releaseビルドで確認。
この変更はCalendarの全ノート・タスク走査を読込時の日別索引へ置き換える。
セル内容、配列順序、重複日、同日due/scheduled、先頭journal、再読込と失敗時の置換、日付をまたぐ期限判定を維持する。

## 検証

```sh
export DEVELOPER_DIR=/Library/Developer/CommandLineTools
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk
swift build --package-path native -c release --product TrackApp
swift run --package-path native -c release VerifyReading
swift run --package-path native -c release VerifyReading --calendar-benchmark
```

`VerifyReading`は実CalendarModelをmock HTTPから読み込み、変更前の走査方式を期待値として全結果を比較する。
ベンチマークは3,598ノート・8,672活動日、42セル、タスク0/512件、warmup 5回・測定31回。
合成データの集計時間であり、アプリ全体の高速化率ではない。

| 42セル・タスク数 | 全件走査の中央値 / p95 | 索引参照の中央値 / p95 |
| --- | ---: | ---: |
| 0件 | 365.324 / 371.746 ms | 0.125 / 0.136 ms |
| 512件 | 414.043 / 421.631 ms | 0.344 / 0.400 ms |

同じ変更後バイナリ内で、変更前の走査式と索引参照を比較した。通信と索引構築はこの表の計測外。

## 実アプリでの位置移動

個人ノートを含まない専用vaultと別bundle IDのアプリを使用。
変更前はmain `e24c235`、変更後は本PRのRelease TrackApp、同じGo helperを使用した。
APIで3,598ノート・8,672活動日を確認し、ウィンドウを1861×1515ptにしてCalendarを表示後、Notesへ戻る。
タイトルバーに横方向±150ptの軌跡を180回送信し、WindowServerから座標を取得した。
AXは最初の対象確認に用い、移動ループでは反復問合せしない。

| 対象 | 指示位置との差 p95 |
| --- | ---: |
| 標準NSWindow（調査時の対照） | 0.91pt |
| 変更前のTrack | 247.66pt |
| Calendar索引化後のTrack・初回 | 161.36pt |
| Calendar索引化後・sample同時計測 | 199.58pt |

索引化後の移動中sampleではCalendarの全件走査が消えた。
ただしSwiftUIのlayout/AttributeGraph更新が残り、標準window相当の追従性には到達していない。
単体のNSHostingView/WindowGroupにCalendarを置く再現では停止しなかった。
最小WindowGroupへtoolbarのButton/Menuを加えた条件でもp95は0.91ptで、toolbarの存在だけでは説明できない。
`isDocumentEdited`の同値代入を抑えた診断版でも停止が残ったため、その変更は採用していない。
アプリ全体の構成に依存する更新条件を引き続き切り分ける必要がある。
**このPRをウィンドウ移動の問題全体の解決とは扱わない。** 残対応は[互換対応表](../../spec/native-parity.md)のウィンドウ性能項目で追跡する。

座標差は描画FPSではない。今回の比較は位置移動で、サイズ変更の前後測定は未実施。
