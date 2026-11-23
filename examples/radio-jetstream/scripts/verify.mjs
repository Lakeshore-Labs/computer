// Independent raw check (no MCP, no receiver code path): object digest + bytes + durable consumer state.
import { connect } from 'nats'; import { createHash } from 'node:crypto'; import { readFileSync, writeFileSync } from 'node:fs';
const dir = process.argv[2]; const nc = await connect({ servers: process.env.NATS_URL }); const js = nc.jetstream(); const jsm = await nc.jetstreamManager();
const os = await js.views.os('CUES'); const oi = await os.info('stinger-v2.wav.b64'); const st = await os.getBlob('stinger-v2.wav.b64');
const wav = Buffer.from(Buffer.from(st).toString('ascii'), 'base64'); const rec = JSON.parse(readFileSync(dir + '/receiver.json'));
const ci = await jsm.consumers.info('RUNDOWN', 'receiver-a'); const si = await jsm.streams.info('RUNDOWN');
const message = await jsm.streams.getMessage('RUNDOWN', { seq: rec.seq });
const order = JSON.parse(new TextDecoder().decode(message.data));
const durationMs = wav.readUInt32LE(40) * 1000 / wav.readUInt32LE(28);
const v = { duration_ms: durationMs, slot_ms: order.items[1].slot_ms, object_size_chars: oi.size, object_digest: oi.digest, decoded_bytes: wav.length, sha256: createHash('sha256').update(wav).digest('hex'),
  stream_storage: si.config.storage, stream_msgs: si.state.messages, consumer: { ack_floor: ci.ack_floor.stream_seq, num_pending: ci.num_pending, num_ack_pending: ci.num_ack_pending, num_redelivered: ci.num_redelivered } };
v.pass = rec.onair === true && Math.abs(durationMs - order.cue.expected_duration_ms) <= 5 &&
  durationMs <= v.slot_ms && rec.duration_ms === durationMs && v.sha256 === rec.expected_sha256 && v.sha256 === rec.sha256 && v.decoded_bytes === rec.decoded_bytes && v.consumer.ack_floor === rec.seq &&
  v.consumer.num_pending === 0 && v.consumer.num_ack_pending === 0 && v.consumer.num_redelivered === 0 && v.stream_storage === 'file';
writeFileSync(dir + '/verify.json', JSON.stringify(v, null, 1)); console.log(JSON.stringify(v)); await nc.close(); process.exit(v.pass ? 0 : 1);
