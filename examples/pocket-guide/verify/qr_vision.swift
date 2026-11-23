import Foundation
import Vision
import AppKit
let path = CommandLine.arguments[1]
guard let img = NSImage(contentsOfFile: path), let cg = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else { print("[]"); exit(2) }
let req = VNDetectBarcodesRequest()
req.symbologies = [.qr]
try VNImageRequestHandler(cgImage: cg, options: [:]).perform([req])
var out: [[String: Any]] = []
for o in (req.results ?? []) {
  let b = o.boundingBox
  out.append(["payload": o.payloadStringValue ?? "", "symbology": o.symbology.rawValue,
    "bbox_norm": [b.origin.x, b.origin.y, b.size.width, b.size.height]])
}
let d = try JSONSerialization.data(withJSONObject: out)
print(String(data: d, encoding: .utf8)!)
