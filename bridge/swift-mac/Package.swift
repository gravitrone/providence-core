// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "providence-mac-bridge",
    platforms: [.macOS(.v12)],
    products: [
        .executable(name: "providence-mac-bridge", targets: ["providence-mac-bridge"]),
        .library(name: "ProvidenceCaptureKit", targets: ["ProvidenceCaptureKit"]),
    ],
    targets: [
        .executableTarget(
            name: "providence-mac-bridge",
            dependencies: ["ProvidenceCaptureKit"],
            path: "Sources/providence-mac-bridge"
        ),
        .target(
            name: "ProvidenceCaptureKit",
            path: "Sources/ProvidenceCaptureKit"
        ),
    ]
)
