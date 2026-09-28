# TrackNative (macOS SwiftUI MVP)

Spec: `docs/spec/native-macos.md`.

## Layout

- `Sources/TrackAPI` — Codable models (`types.ts` 対応) + URLSession client
  (`api.ts` live branch 対応) + `TrackProcess` (同梱 `track web` の起動/停止)。
- `Sources/TrackUI` — 検索・閲覧・タスクの SwiftUI。
- `Tools/VerifyFixtures` — fixture decode 検証の実行ファイル。

## Build / verify

`Package.swift` の `.macOS(.v15)` は最低デプロイ対象 OS であり、ビルドに使う SDK の
バージョン指定ではない。ビルド補助スクリプトは `xcode-select -p` で選ばれた Apple
Developer Directory から `/usr/bin/xcrun` 経由で Swift コンパイラと macOS SDK を組にして
選ぶ。Nix などが設定した汎用 `DEVELOPER_DIR` / `SDKROOT` は使わない。

```sh
make native-verify
scripts/check-native-live-events.sh
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyVaultScope
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyReader
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyNavigation
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyDesign
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyReading
sh scripts/apple-toolchain.sh native swift run -c release --package-path native VerifyReading --calendar-benchmark
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyTasks
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyAgentRequests
sh scripts/apple-toolchain.sh native swift run --package-path native VerifyVoice
```

Build the unsigned, non-sandboxed app bundle with `make native-app`. Override
`NATIVE_DEVELOPER_DIR` and/or `NATIVE_SDKROOT` for another installed Apple toolchain or SDK. The
desktop app uses the same discovery, with `DESKTOP_DEVELOPER_DIR` and `DESKTOP_SDKROOT` overrides.

If no compatible Apple toolchain is selected or installed, the helper reports how to install/select
one or how to provide explicit overrides.

## Notes

- swift-testing / XCTest は CLT に入っていないため、テストは `swift test`
  ではなく `swift run VerifyFixtures` で行う。フル Xcode があれば移行可。
- `VerifyAgentRequests` checks request lifecycle, follow-up context and lost-response retries against a mocked gateway; it never dispatches to an agent.
- `VerifyVaultScope` checks vault selection persistence and request/response identity with two mocked vaults sharing note IDs.
- `VerifyReading --calendar-benchmark` compares the previous full-scan calendar aggregation with indexed lookups using mocked HTTP (3,598 notes, 8,672 activity days, 0/512 tasks, 42 cells). It checks equivalence before 5 warmups and 31 timed runs; this measures aggregation, not window-drag latency.
- fixture (`Tools/VerifyFixtures/Fixtures/*.json`) は `track web` の実応答から採取。

`VerifyVoice` exercises transcript edits and mocked journal saves without accessing the microphone.

`VerifyFiguresMedia` checks scoped asset routing, annotation/source retention and HTML isolation.
Add `--webview` on a desktop Mac to exercise local SVG rendering, chart callbacks and PDF page controls,
and write `/private/tmp/native-figure-smoke.png`; no CDN or gateway is contacted.
