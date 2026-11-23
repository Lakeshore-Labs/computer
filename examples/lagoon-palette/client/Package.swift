// swift-tools-version:6.0
import PackageDescription

let package = Package(
  name: "LagoonPalette",
  platforms: [.macOS(.v14)],
  dependencies: [
    .package(url: "https://github.com/modelcontextprotocol/swift-sdk.git", exact: "0.10.0")
  ],
  targets: [
    .executableTarget(
      name: "LagoonPalette", dependencies: [.product(name: "MCP", package: "swift-sdk")])
  ]
)
