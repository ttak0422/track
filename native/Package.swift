// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "TrackNative",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "TrackAPI", targets: ["TrackAPI"]),
        .library(name: "TrackUI", targets: ["TrackUI"]),
    ],
    dependencies: [
        // GFM rendering (tables, task lists, strikethrough, bare-URL autolinks)
        // via MarkdownUI, a cmark-gfm-backed SwiftUI renderer. 2.4.1 is the
        // version verified against this repo's CLT toolchain.
        .package(url: "https://github.com/gonzalezreal/MarkdownUI", from: "2.4.1"),
    ],
    targets: [
        .target(name: "TrackAPI"),
        .target(name: "TrackUI", dependencies: ["TrackAPI", .product(name: "MarkdownUI", package: "MarkdownUI")]),
        .executableTarget(name: "TrackApp", dependencies: ["TrackUI"]),
        // swift-testing / XCTest ship with full Xcode only, so verification
        // is a runnable executable (`swift run VerifyFixtures`), not a test
        // target. It must keep passing on CLT-only Macs.
        .executableTarget(
            name: "VerifyFixtures",
            dependencies: ["TrackAPI"],
            path: "Tools/VerifyFixtures",
            resources: [.process("Fixtures")]
        ),
    ]
)
