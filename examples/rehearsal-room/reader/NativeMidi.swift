import AudioToolbox
import Foundation

struct Failure: Error, CustomStringConvertible {
  let description: String
  init(_ text: String) { description = text }
}
func checked(_ status: OSStatus, _ label: String) throws {
  guard status == noErr else { throw Failure("\(label) failed with OSStatus \(status)") }
}
struct Note: Codable, Equatable {
  let pitch: Int
  let beat: Double
  let length: Double
}
struct FileObservation: Codable {
  let notes: [Note]
  let tempo: Double
}
func readMidi(_ url: URL) throws -> FileObservation {
  var seq: MusicSequence?
  try checked(NewMusicSequence(&seq), "NewMusicSequence")
  guard let sequence = seq else { throw Failure("Cannot create sequence") }
  defer { DisposeMusicSequence(sequence) }
  try checked(
    MusicSequenceFileLoad(sequence, url as CFURL, .midiType, MusicSequenceLoadFlags()),
    "MusicSequenceFileLoad")
  var trackCount: UInt32 = 0
  try checked(MusicSequenceGetTrackCount(sequence, &trackCount), "MusicSequenceGetTrackCount")
  var notes: [Note] = []
  var tempo: Double?
  func inspect(_ track: MusicTrack) throws {
    var iter: MusicEventIterator?
    try checked(NewMusicEventIterator(track, &iter), "NewMusicEventIterator")
    guard let iterator = iter else { throw Failure("Cannot create iterator") }
    defer { DisposeMusicEventIterator(iterator) }
    var has = DarwinBoolean(false)
    try checked(
      MusicEventIteratorHasCurrentEvent(iterator, &has), "MusicEventIteratorHasCurrentEvent")
    while has.boolValue {
      var time = MusicTimeStamp(0)
      var eventType = MusicEventType(0)
      var size: UInt32 = 0
      var data: UnsafeRawPointer?
      try checked(
        MusicEventIteratorGetEventInfo(iterator, &time, &eventType, &data, &size),
        "MusicEventIteratorGetEventInfo")
      if let data {
        if eventType == kMusicEventType_MIDINoteMessage {
          let n = data.loadUnaligned(as: MIDINoteMessage.self)
          notes.append(Note(pitch: Int(n.note), beat: time, length: Double(n.duration)))
        } else if eventType == kMusicEventType_ExtendedTempo {
          tempo = data.loadUnaligned(as: ExtendedTempoEvent.self).bpm
        }
      }
      try checked(MusicEventIteratorNextEvent(iterator), "MusicEventIteratorNextEvent")
      try checked(
        MusicEventIteratorHasCurrentEvent(iterator, &has), "MusicEventIteratorHasCurrentEvent")
    }
  }
  var tempoTrack: MusicTrack?
  try checked(MusicSequenceGetTempoTrack(sequence, &tempoTrack), "MusicSequenceGetTempoTrack")
  if let tempoTrack { try inspect(tempoTrack) }
  for i in 0..<trackCount {
    var track: MusicTrack?
    try checked(MusicSequenceGetIndTrack(sequence, i, &track), "MusicSequenceGetIndTrack")
    if let track { try inspect(track) }
  }
  guard let tempo else { throw Failure("Missing MIDI tempo") }
  return FileObservation(notes: notes, tempo: tempo)
}
func main() throws {
  let args = Array(CommandLine.arguments.dropFirst())
  guard args.count == 2 else {
    throw Failure("Expected concert score CSV and directory containing both MIDI files")
  }
  let rows = try String(contentsOfFile: args[0], encoding: .utf8).split(separator: "\n").dropFirst()
  var expected: [Note] = []
  var beat = 0.0
  for row in rows {
    let cells = row.split(separator: ",")
    guard cells.count == 3, let pitch = Int(cells[1]), let length = Double(cells[2]) else {
      throw Failure("Invalid source row")
    }
    expected.append(Note(pitch: pitch, beat: beat, length: length))
    beat += length
  }
  var results: [String: FileObservation] = [:]
  for name in ["flute", "clarinet"] {
    let file = URL(fileURLWithPath: args[1]).appendingPathComponent(name + ".mid")
    let observed = try readMidi(file)
    guard observed.notes == expected, abs(observed.tempo - 96) < 0.001 else {
      throw Failure("Score mismatch in " + name)
    }
    results[name] = observed
  }
  let encoder = JSONEncoder()
  encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
  FileHandle.standardOutput.write(try encoder.encode(results))
}
do { try main() } catch {
  FileHandle.standardError.write(Data(("\(error)\n").utf8))
  exit(1)
}
