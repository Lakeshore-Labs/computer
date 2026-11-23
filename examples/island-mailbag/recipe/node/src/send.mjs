// Sender side: Store A (blockstore-fs, a flat-file content-addressed store) and CARv1 export.
//   node send.mjs --edition N --bag DIR --store DIR --boat DIR --measure DIR --report FILE [--have FILE]
// Edition 1 ships every block. With --have (CIDs the receiving store says it holds) the CAR carries only the blocks
// the receiver lacks; a standalone CAR of the whole edition is written to --measure, never sent, to measure the saving.
import { readFile, readdir, writeFile, mkdir } from 'node:fs/promises'
import { join, extname } from 'node:path'
import { FsBlockstore } from 'blockstore-fs'
import { CID, dagCbor, rawBlock, cborBlock, manifestValue, MEDIA, writeCar, sha256hex, hex, CODECS, fetchVerified } from './lib.mjs'

const arg = (n) => { const i = process.argv.indexOf('--' + n); return i < 0 ? undefined : process.argv[i + 1] }
const edition = Number(arg('edition'))
const [bag, storeDir, boat, measure, reportPath] = ['bag', 'store', 'boat', 'measure', 'report'].map(arg)
await Promise.all([mkdir(boat, { recursive: true }), mkdir(measure, { recursive: true })])

const have = new Set(arg('have') ? (await readFile(arg('have'), 'utf8')).split('\n').filter(Boolean) : [])
const store = new FsBlockstore(storeDir)
await store.open?.()

// 1. raw blocks, one per file, and the DAG-CBOR manifest that links them
const names = (await readdir(bag)).sort()
const entries = []
for (const name of names) {
  const bytes = await readFile(join(bag, name))
  const block = await rawBlock(bytes)
  entries.push({ name, mediaType: MEDIA[extname(name)], size: bytes.length, cid: block.cid, block })
}
const manifest = await cborBlock(manifestValue(edition, entries))
const all = [manifest, ...entries.map((e) => e.block)] // root first, then links in manifest order

// 2. Store A takes every block
for (const b of all) await store.put(b.cid, b.bytes)

// 3. sender-side round trip: rebuild the edition from Store A alone and compare with the bag
const m = dagCbor.decode(await fetchVerified(store, manifest.cid))
let roundTrip = m.files.length === entries.length
for (const f of m.files) {
  const got = await fetchVerified(store, f.link)
  roundTrip &&= sha256hex(got) === sha256hex(await readFile(join(bag, f.name))) && got.length === f.size
}

// 4. the CAR that crosses the boat, and the counterfactual
const role = (e) => (e.name.endsWith('.epub') ? 'book' : 'card')
const describe = (b, r) => ({ cid: b.cid.toString(), codec: CODECS[b.cid.code], bytes: b.bytes.length, role: r })
const roles = new Map([[manifest.cid.toString(), 'manifest'], ...entries.map((e) => [e.cid.toString(), role(e)])])
// the CAR is filled from Store A, not from memory
const fromStore = await Promise.all(all.map(async (b) => ({ cid: b.cid, bytes: await fetchVerified(store, b.cid) })))
const sent = fromStore.filter((b) => !have.has(b.cid.toString()))
const withheld = fromStore.filter((b) => have.has(b.cid.toString()))
const carName = edition === 1 ? 'edition-1.car' : `edition-${edition}.delta.car`
const car = await writeCar(manifest.cid, sent)
await writeFile(join(boat, carName), car)
let standalone = null
if (withheld.length) {
  const full = await writeCar(manifest.cid, fromStore)
  await writeFile(join(measure, `edition-${edition}.standalone.car`), full)
  standalone = { file: `edition-${edition}.standalone.car`, bytes: full.length, sha256: sha256hex(full), blocks: all.length }
}

let stored = 0
for await (const _ of store.getAll()) stored++
const report = {
  edition,
  root: manifest.cid.toString(),
  blocks: all.map((b) => describe(b, roles.get(b.cid.toString()))),
  sent: sent.map((b) => describe(b, roles.get(b.cid.toString()))),
  withheld: withheld.map((b) => ({ ...describe(b, roles.get(b.cid.toString())), reason: 'the receiving store already holds this block' })),
  car: { file: carName, bytes: car.length, sha256: sha256hex(car), blocks: sent.length, roots: [manifest.cid.toString()] },
  standalone,
  storeA: { library: 'blockstore-fs 4.0.1 (FsBlockstore)', blocksNow: stored, roundTripFromStoreA: roundTrip },
  manifestValue: { schema: m.schema, library: m.library, edition: m.edition, files: m.files.map((f) => ({ name: f.name, mediaType: f.mediaType, size: f.size, link: f.link.toString() })) },
}
await writeFile(reportPath, JSON.stringify(report, null, 2) + '\n')
console.log(JSON.stringify({ edition, root: report.root, carBytes: car.length, sent: sent.length, withheld: withheld.length, storeABlocks: stored, roundTrip }))
