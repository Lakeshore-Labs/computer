// Read a CARv1 with @ipld/car and verify every block's hash. node jsdecode.mjs CAR OUT.json
import { readFile, writeFile } from 'node:fs/promises'
import { readCarVerified, hex, sha256hex, dagCbor } from './lib.mjs'

const [path, out] = process.argv.slice(2)
const bytes = await readFile(path)
let report
try {
  const car = await readCarVerified(bytes)
  report = {
    reader: '@ipld/car 6.2.0 + multiformats 14.0.6 (Node)',
    file: path.split('/').pop(), bytes: bytes.length, sha256: sha256hex(bytes), carVersion: car.version,
    roots: car.roots.map(String),
    blocks: car.blocks.map((b) => ({ cid: b.cid.toString(), codec: b.codec, bytes: b.bytes.length, hashOk: b.hashOk })),
    allHashesOk: car.blocks.every((b) => b.hashOk),
  }
} catch (e) {
  report = { reader: '@ipld/car 6.2.0 + multiformats 14.0.6 (Node)', file: path.split('/').pop(), bytes: bytes.length, error: String(e.message ?? e), allHashesOk: false }
}
await writeFile(out, JSON.stringify(report, null, 2) + '\n')
console.log(`${report.file}: ${report.error ? 'ERROR ' + report.error : `${report.blocks.length} blocks, hashes ${report.allHashesOk ? 'ok' : 'BAD'}`}`)
process.exit(report.allHashesOk ? 0 : 3)
