// Normal-API helpers over multiformats, @ipld/dag-cbor and @ipld/car. No hand-built CID, CBOR or CAR bytes.
import { CID } from 'multiformats/cid'
import { sha256 } from 'multiformats/hashes/sha2'
import * as raw from 'multiformats/codecs/raw'
import * as dagCbor from '@ipld/dag-cbor'
import { CarWriter, CarReader } from '@ipld/car'
import { createHash } from 'node:crypto'

export const CODECS = { [raw.code]: 'raw', [dagCbor.code]: 'dag-cbor' }
export const hex = (u8) => Buffer.from(u8).toString('hex')
export const sha256hex = (u8) => createHash('sha256').update(u8).digest('hex')

/** A raw block: CIDv1, codec raw (0x55), multihash sha2-256. */
export async function rawBlock(bytes) {
  return { cid: CID.createV1(raw.code, await sha256.digest(bytes)), bytes }
}

/** A DAG-CBOR block: CIDv1, codec dag-cbor (0x71), multihash sha2-256. CID values in `value` become links. */
export async function cborBlock(value) {
  const bytes = dagCbor.encode(value)
  return { cid: CID.createV1(dagCbor.code, await sha256.digest(bytes)), bytes }
}

export const MEDIA = { '.epub': 'application/epub+zip', '.md': 'text/markdown' }

/** The edition manifest: a DAG-CBOR map whose `link` fields are CIDs of raw blocks. */
export function manifestValue(edition, entries) {
  return {
    schema: 'island-mailbag/edition/v1',
    library: 'Gull Island Library (made up for this exhibit)',
    edition,
    files: entries.map((e) => ({ name: e.name, mediaType: e.mediaType, size: e.size, link: e.cid })),
  }
}

/** Write a CARv1 whose single root is `root`, holding `blocks` in the given order. Returns the file's bytes. */
export async function writeCar(root, blocks) {
  const { writer, out } = CarWriter.create([root])
  const chunks = []
  const drained = (async () => { for await (const c of out) chunks.push(c) })()
  for (const b of blocks) await writer.put(b)
  await writer.close()
  await drained
  return Buffer.concat(chunks)
}

/** Read a CARv1 and check every block: the digest must match its CID (sha2-256 only; raw or dag-cbor only). */
export async function readCarVerified(bytes) {
  const reader = await CarReader.fromBytes(bytes)
  const roots = await reader.getRoots()
  const blocks = []
  for await (const { cid, bytes: data } of reader.blocks()) {
    const digest = await sha256.digest(data)
    const hashOk = cid.version === 1 && cid.multihash.code === sha256.code && hex(cid.multihash.digest) === hex(digest.digest)
    blocks.push({ cid, bytes: data, hashOk, codec: CODECS[cid.code] ?? `0x${cid.code.toString(16)}` })
  }
  return { version: reader.version, roots, blocks }
}

/** Read a block back out of a blockstore (which returns chunks) and check it still hashes to its CID. */
export async function fetchVerified(store, cid) {
  const chunks = []
  for await (const c of store.get(cid)) chunks.push(c)
  const bytes = new Uint8Array(Buffer.concat(chunks))
  const digest = await sha256.digest(bytes)
  if (hex(digest.digest) !== hex(cid.multihash.digest)) throw new Error(`Store A block ${cid} does not hash to its CID`)
  return bytes
}

export { CID, dagCbor, sha256, raw }
