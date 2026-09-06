// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "TrackNative",
    platforms: [.macOS(.v15)],
    products: [
        .library(name: "TrackAPI", targets: ["TrackAPI"]),
        .library(name: "TrackUI", targets: ["TrackUI"]),
    ],
    targets: [
        .target(name: "TrackAPI"),
        .target(name: "TrackUI", dependencies: ["TrackAPI"]),
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
