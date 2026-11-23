import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

const scripts = new URL("../scripts/", import.meta.url);
const evidence = new URL("../evidence/run-2/", import.meta.url);

function workspace(t) {
  const dir = mkdtempSync(path.join(tmpdir(), "radio-replay-test-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return dir;
}

test("a held cue fails even after byte verification and acknowledgment", (t) => {
  const dir = workspace(t);
  cpSync(new URL("verify.mjs", scripts), path.join(dir, "verify.mjs"));
  const wav = readFileSync(new URL("cue.wav", evidence));
  const broker = path.join(dir, "node_modules/nats");
  mkdirSync(broker, { recursive: true });
  writeFileSync(path.join(broker, "index.js"), `
    exports.connect = async () => ({
      jetstream: () => ({views:{os:async () => ({
        info: async () => ({}), getBlob: async () => Buffer.from('${wav.toString("base64")}')
      })}}),
      jetstreamManager: () => ({
        streams:{info: async () => ({config:{storage:'file'},state:{messages:1}}),
          getMessage:async () => ({data:require('node:fs').readFileSync(process.argv[2]+'/rundown.json')})},
        consumers:{info: async () => ({ack_floor:{stream_seq:1},num_pending:0,num_ack_pending:0,num_redelivered:0})}
      }), close: async () => {}
    });`);
  const rec = JSON.parse(readFileSync(new URL("receiver.json", evidence)));
  const order = {cue:{expected_duration_ms:1500},items:[{}, {slot_ms:2000}]};
  const run = () => {
    writeFileSync(path.join(dir, "rundown.json"), JSON.stringify(order));
    writeFileSync(path.join(dir, "receiver.json"), JSON.stringify(rec));
    return spawnSync(process.execPath, [path.join(dir, "verify.mjs"), dir], {encoding:"utf8"});
  };
  assert.equal(run().status, 0);
  order.items[1].slot_ms = 1000;
  rec.checks.fits_slot = false;
  rec.onair = false;
  rec.decision = "HOLD: cue failed verification - keep rev 1 order";
  assert.equal(run().status, 1);
  assert.equal(JSON.parse(readFileSync(path.join(dir, "verify.json"))).pass, false);
  rec.onair = true;
  assert.equal(run().status, 1, "the independent verifier must check the published slot");
});

test("a debugger disconnect rejects a pending command and releases Chrome's profile", (t) => {
  const dir = workspace(t);
  const setup = `
    import net from 'node:net';
    import childProcess from 'node:child_process';
    import {EventEmitter} from 'node:events';
    import {syncBuiltinESMExports} from 'node:module';
    import {writeFileSync} from 'node:fs';
    net.createServer = () => ({listen(port, host, ready) {queueMicrotask(ready); return this;},
      address: () => ({port:1234}), close: callback => callback()});
    childProcess.spawn = () => Object.assign(new EventEmitter(), {
      exitCode:null, signalCode:null, kill() {
        queueMicrotask(() => { this.signalCode='SIGTERM'; this.emit('exit', null, 'SIGTERM'); this.emit('close', null, 'SIGTERM'); writeFileSync(process.env.EXIT_RECEIPT,'exited'); });
        return true;
      }
    });
    syncBuiltinESMExports();
    globalThis.fetch = async () => ({json: async () => [{type:'page',webSocketDebuggerUrl:'ws://example.invalid'}]});
    globalThis.WebSocket = class extends EventTarget {
      constructor() { super(); queueMicrotask(() => { this.onopen?.(); this.dispatchEvent(new Event('open')); }); }
      send() { queueMicrotask(() => { this.onclose?.(); this.dispatchEvent(new Event('close')); }); }
      close() {}
    };
  `;
  const proof = path.join(dir, "child-exit");
  const run = spawnSync(process.execPath, ["--import", "data:text/javascript," + encodeURIComponent(setup),
    new URL("browser_listen.mjs", scripts).pathname, "chrome", "ffmpeg", "page", dir], {
    env:{...process.env,TMPDIR:dir,EXIT_RECEIPT:proof}, timeout:5000, encoding:"utf8",
  });
  assert.equal(run.error, undefined);
  assert.notEqual(run.status, 0);
  assert.deepEqual(readdirSync(dir), ["child-exit"]);
});
