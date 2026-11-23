// Subscriber role: raw nats.js. Connects LATE, consumes existing durable, reads object bytes, verifies, ackAck.
import { connect } from 'nats'; import { createHash } from 'node:crypto'; import { writeFileSync, readFileSync } from 'node:fs';
const dir = process.argv[2]; const T2 = new Date().toISOString();
const nc = await connect({ servers: process.env.NATS_URL }); const js = nc.jetstream(); const jsm = await nc.jetstreamManager();
const before = await jsm.consumers.info('RUNDOWN', 'receiver-a');
const c = await js.consumers.get('RUNDOWN', 'receiver-a'); const msgs = await c.fetch({ max_messages: 1, expires: 5000 });
let res;
for await (const m of msgs) {
  const o = JSON.parse(new TextDecoder().decode(m.data)); const os = await js.views.os(o.cue.bucket);
  const stored = await os.getBlob(o.cue.name); const wav = Buffer.from(Buffer.from(stored).toString('ascii'), 'base64');
  const sha = createHash('sha256').update(wav).digest('hex');
  const rate = wav.readUInt32LE(24), ch = wav.readUInt16LE(22), bits = wav.readUInt16LE(34), dl = wav.readUInt32LE(40), dur = dl / (rate * ch * bits / 8) * 1000;
  const proof = JSON.parse(readFileSync(dir + '/producer-proof.json'));
  const checks = { hash: sha === o.cue.sha256, bytes: wav.length === o.cue.decoded_bytes, duration: Math.abs(dur - o.cue.expected_duration_ms) <= 5,
    fits_slot: dur <= o.items[1].slot_ms, matches_guest_wav: sha === proof.sha256 };
  const ok = Object.values(checks).every(Boolean);
  await m.ackAck(); const T3 = new Date().toISOString();
  res = { T1_amendment_published: proof.T1_amendment_published, T2_receiver_connected: T2, T3_decision_acked: T3, seq: m.seq, rev: o.rev, stored_chars: stored.length,
    decoded_bytes: wav.length, sha256: sha, expected_sha256: o.cue.sha256, duration_ms: dur, checks,
    decision: ok ? `ON AIR: rev ${o.rev} - play STINGER-v2 at ${o.items[1].t} (${dur.toFixed(0)} ms), news-top at ${o.items[2].t}` : 'HOLD: cue failed verification - keep rev 1 order',
    onair: ok, pending_before: before.num_pending };
  writeFileSync(dir + '/cue.wav', wav);
}
if (!res) throw new Error('no message delivered');
const after = await jsm.consumers.info('RUNDOWN', 'receiver-a');
res.consumer_after = { ack_floor: after.ack_floor.stream_seq, num_pending: after.num_pending, num_ack_pending: after.num_ack_pending };
writeFileSync(dir + '/receiver.json', JSON.stringify(res, null, 1)); console.log(JSON.stringify(res)); await nc.close();
