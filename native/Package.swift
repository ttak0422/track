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
        // Already resolved through MarkdownUI; direct use preserves table cells
        // in rich clipboard exports without adding another parser or library.
        .package(url: "https://github.com/swiftlang/swift-cmark", from: "0.8.0"),
    ],
    targets: [
        .target(name: "TrackAPI"),
        .target(name: "TrackUI", dependencies: [
            "TrackAPI", .product(name: "MarkdownUI", package: "MarkdownUI"),
            .product(name: "cmark-gfm", package: "swift-cmark"),
            .product(name: "cmark-gfm-extensions", package: "swift-cmark"),
        ]),
        .executableTarget(name: "TrackApp", dependencies: ["TrackUI"]),
        .executableTarget(name: "VerifyFiguresMedia", dependencies: ["TrackUI"], path: "Tools/VerifyFiguresMedia"),
        .executableTarget(name: "VerifyPreviews", dependencies: ["TrackUI"], path: "Tools/VerifyPreviews"),
        .executableTarget(name: "VerifyVoice", dependencies: ["TrackUI"], path: "Tools/VerifyVoice"),
        .executableTarget(name: "VerifyNoteActions", dependencies: ["TrackUI"], path: "Tools/VerifyNoteActions"),
        .executableTarget(name: "VerifyNavigation", dependencies: ["TrackUI"], path: "Tools/VerifyNavigation"),
        .executableTarget(name: "VerifyAgentRequests", dependencies: ["TrackUI"], path: "Tools/VerifyAgentRequests"),
        .executableTarget(name: "VerifyVaultScope", dependencies: ["TrackUI"], path: "Tools/VerifyVaultScope"),
        .executableTarget(name: "VerifyReader", dependencies: ["TrackUI"], path: "Tools/VerifyReader"),
        .executableTarget(name: "VerifyTasks", dependencies: ["TrackUI"], path: "Tools/VerifyTasks"),
        .executableTarget(name: "VerifyReading", dependencies: ["TrackUI"], path: "Tools/VerifyReading"),
        .executableTarget(name: "VerifyDesign", dependencies: ["TrackUI"], path: "Tools/VerifyDesign"),
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
