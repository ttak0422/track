// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "TrackWeb",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "TrackWeb", targets: ["TrackWebApp"]),
        .executable(name: "TrackWebTests", targets: ["TrackWebTests"]),
    ],
    targets: [
        .target(name: "TrackWebCore"),
        .executableTarget(name: "TrackWebApp", dependencies: ["TrackWebCore"]),
        .executableTarget(name: "TrackWebTests", dependencies: ["TrackWebCore"]),
    ]
)
