# TrackNative (macOS SwiftUI MVP)

Spec: `docs/spec/native-macos.md`.

## Layout

- `Sources/TrackAPI` — Codable models (`types.ts` 対応) + URLSession client
  (`api.ts` live branch 対応) + `TrackProcess` (同梱 `track web` の起動/停止)。
- `Sources/TrackUI` — 検索・閲覧・タスクの SwiftUI。
- `Tools/VerifyFixtures` — fixture decode 検証の実行ファイル。

## Build / verify (CLT-only Mac)

このリポジトリは nix flake が `SDKROOT` / `DEVELOPER_DIR` を nix の
apple-sdk に固定する (`.envrc: use flake`)。nix の SDK は Swift 5.10 用で
手元の Swift 6.3 と合わないため、swift コマンドだけ nix の指定を外す。
リポジトリ側の変更は不要。

```sh
export DEVELOPER_DIR=/Library/Developer/CommandLineTools
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.sdk
swift build --package-path native
swift run --package-path native VerifyFixtures
```

Build the unsigned, non-sandboxed app bundle with `make native-app`. Override
`NATIVE_SDKROOT` when the installed Command Line Tools use another SDK path.

恒久的に切り替える場合は `sudo xcode-select --switch
/Library/Developer/CommandLineTools` (要管理者権限)。

## Notes

- swift-testing / XCTest は CLT に入っていないため、テストは `swift test`
  ではなく `swift run VerifyFixtures` で行う。フル Xcode があれば移行可。
- fixture (`Tools/VerifyFixtures/Fixtures/*.json`) は `track web` の実応答から採取。
