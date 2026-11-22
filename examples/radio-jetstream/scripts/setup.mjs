// Node admin (raw nats.js): temporary stream + EXISTING durable consumer. Receiver stays offline.
import { connect, AckPolicy } from 'nats';
const nc = await connect({ servers: process.env.NATS_URL }); const jsm = await nc.jetstreamManager();
await jsm.streams.add({ name: 'RUNDOWN', subjects: ['desk.rundown.>'], storage: 'file', max_msgs: 100 });
await jsm.consumers.add('RUNDOWN', { durable_name: 'receiver-a', ack_policy: AckPolicy.Explicit, ack_wait: 30e9 });
const ci = await jsm.consumers.info('RUNDOWN', 'receiver-a');
console.log(JSON.stringify({ stream: 'RUNDOWN', consumer: ci.name, pending: ci.num_pending, ack_floor: ci.ack_floor.stream_seq }));
await nc.close();
