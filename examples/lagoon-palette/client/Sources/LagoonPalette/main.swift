import AppKit
import CoreGraphics
import CryptoKit
import Foundation
import ImageIO
import MCP
import System

struct Failure: Error, CustomStringConvertible {
  let description: String
  init(_ message: String) { description = message }
}
struct Swatch: Codable {
  var name: String
  var site: String
  var x: Int
  var y: Int
  var rgb: [Int]
}

func pixel(_ image: CGImage, x: Int, y: Int) throws -> [Int] {
  guard image.width == 256, image.height == 256,
    let crop = image.cropping(to: CGRect(x: x, y: y, width: 1, height: 1)),
    let space = CGColorSpace(name: CGColorSpace.sRGB)
  else { throw Failure("Invalid image window") }
  var rgba = [UInt8](repeating: 0, count: 4)
  try rgba.withUnsafeMutableBytes { raw in
    guard
      let context = CGContext(
        data: raw.baseAddress, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4,
        space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
    else { throw Failure("Cannot create RGB context") }
    context.draw(crop, in: CGRect(x: 0, y: 0, width: 1, height: 1))
  }
  return rgba.prefix(3).map(Int.init)
}

func main() async throws {
  let args = Array(CommandLine.arguments.dropFirst())
  guard args.count == 5 else {
    throw Failure(
      "Expected Computer binary, WASI Python directory, scenes, recipe, and a fresh output directory"
    )
  }
  let urls = args.map { URL(fileURLWithPath: $0).standardizedFileURL }
  let (binary, python, scenes, recipe, out) = (urls[0], urls[1], urls[2], urls[3], urls[4])
  guard !FileManager.default.fileExists(atPath: out.path) else {
    throw Failure("Choose a fresh output directory")
  }
  try FileManager.default.createDirectory(at: out, withIntermediateDirectories: true)
  let client = Client(name: "lagoon-palette", version: "1.0")
  let child = Process()
  let toChild = Pipe()
  let fromChild = Pipe()
  child.executableURL = binary
  child.arguments = [
    "-workdir", "/work", "-mount", out.path + ":/work", "-mount", scenes.path + ":/scenes:ro",
    "-mount", recipe.path + ":/recipe:ro", "-python",
    python.appendingPathComponent("python.wasm").path, "-python-lib",
    python.appendingPathComponent("lib").path, "mcp",
  ]
  let home = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
  try FileManager.default.createDirectory(at: home, withIntermediateDirectories: false)
  defer { try? FileManager.default.removeItem(at: home) }
  child.environment = ["HOME": home.path, "PATH": "/usr/bin:/bin", "LANG": "en_US.UTF-8", "TZ": "UTC"]
  child.standardInput = toChild
  child.standardOutput = fromChild
  child.terminationHandler = { _ in Task { await client.disconnect() } }
  try child.run()
  defer {
    if child.isRunning { child.terminate() }
    child.waitUntilExit()
  }
  let transport = StdioTransport(
    input: FileDescriptor(rawValue: fromChild.fileHandleForReading.fileDescriptor),
    output: FileDescriptor(rawValue: toChild.fileHandleForWriting.fileDescriptor))
  _ = try await client.connect(transport: transport)
  let tools = try await client.listTools().tools.map(\.name).sorted()
  guard tools == ["bash", "edit", "read", "write"] else { throw Failure("Unexpected tool list") }
  let locations = [
    ("Roof terracotta", "san-marco", 30, 80), ("Canal teal", "san-marco", 50, 210),
    ("Marsh olive", "burano", 40, 66), ("Pale reed", "burano", 71, 79),
    ("Light sand", "lido", 202, 94), ("Adriatic green", "lido", 216, 127),
  ]
  var colors = [Swatch]()
  var hashes = [String: String]()
  for site in ["san-marco", "burano", "lido"] {
    let read = try await client.callTool(
      name: "read", arguments: ["path": .string("/scenes/" + site + ".png")])
    let images = read.content.compactMap { block -> String? in
      if case .image(let data, let mime, _) = block, mime == "image/png" { return data }
      return nil
    }
    guard read.isError != true, let encoded = images.first,
      let bytes = Data(base64Encoded: encoded),
      let source = CGImageSourceCreateWithData(bytes as CFData, nil),
      let image = CGImageSourceCreateImageAtIndex(source, 0, nil)
    else { throw Failure("MCP read did not return a PNG") }
    hashes[site] = SHA256.hash(data: bytes).map { String(format: "%02x", $0) }.joined()
    for (name, s, x, y) in locations where s == site {
      colors.append(Swatch(name: name, site: site, x: x, y: y, rgb: try pixel(image, x: x, y: y)))
    }
  }
  let encoder = JSONEncoder()
  encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
  let content = String(decoding: try encoder.encode(colors), as: UTF8.self)
  let write = try await client.callTool(
    name: "write", arguments: ["path": .string("selection.json"), "content": .string(content)])
  guard write.isError != true else { throw Failure("Cannot write selection") }
  let edit = try await client.callTool(
    name: "edit",
    arguments: [
      "path": .string("selection.json"), "oldText": .string("Light sand"),
      "newText": .string("Lido sand"),
    ])
  guard edit.isError != true else { throw Failure("Cannot rename swatch") }
  let revised = try await client.callTool(
    name: "read", arguments: ["path": .string("selection.json")])
  guard revised.isError != true, case .text(let text) = revised.content.first else {
    throw Failure("Missing revised selection")
  }
  let final = try JSONDecoder().decode([Swatch].self, from: Data(text.utf8))
  guard final.count == 6 && final[4].name == "Lido sand" else {
    throw Failure("Unexpected revised swatches")
  }
  let publish = try await client.callTool(
    name: "bash", arguments: ["command": .string("python3 /recipe/palette.py")])
  guard publish.isError != true,
    publish.content.contains(where: { block in
      if case .text(let result) = block { return result.contains("\"swatches\": 6") }
      return false
    })
  else { throw Failure("Guest export failed") }
  await client.disconnect()
  let list = NSColorList(name: "Venetian Lagoon")
  for c in final {
    let rgb = c.rgb.map { CGFloat($0) / 255 }
    list.setColor(NSColor(srgbRed: rgb[0], green: rgb[1], blue: rgb[2], alpha: 1), forKey: c.name)
  }
  let file = out.appendingPathComponent("venetian-lagoon.clr")
  try list.write(to: file)
  guard let reopened = NSColorList(name: "Readback", fromFile: file.path),
    reopened.allKeys.count == final.count
  else { throw Failure("Native color list did not reopen") }
  for c in final {
    guard let rgb = reopened.color(withKey: c.name)?.usingColorSpace(.sRGB) else {
      throw Failure("Native color is missing")
    }
    let values = [rgb.redComponent, rgb.greenComponent, rgb.blueComponent].map {
      Int(($0 * 255).rounded())
    }
    guard values == c.rgb else { throw Failure("Native color list differs for " + c.name) }
  }
  let native: [String: Any] = [
    "sdk": "modelcontextprotocol/swift-sdk 0.10.0", "tools": tools, "png_sha256": hashes,
    "native_keys": reopened.allKeys, "native_round_trip": true,
  ]
  try JSONSerialization.data(withJSONObject: native, options: [.prettyPrinted, .sortedKeys]).write(
    to: out.appendingPathComponent("swift.json"))
  print("Read three original PNGs. Exported and reopened six native palette swatches.")
}
do { try await main() } catch {
  FileHandle.standardError.write(Data(("\(error)\n").utf8))
  exit(1)
}
