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
        .testTarget(
            name: "TrackAPITests",
            dependencies: ["TrackAPI"],
            resources: [.process("Fixtures")]
        ),
    ]
)
