// What Pi Durable needs of its surroundings, on the stock QuickJS command. The host is reached over standard input
// and output, one JSON message a line. A request is {id, op, arg}; the host answers {id, result} or {id, error},
// after any number of {id, event}. A line {event} is told to whoever ran the agent, and a line {stop} from the host
// asks the agent to end its run.
import * as std from "qjs:std";
import * as os from "qjs:os";
import * as encoding from "@sinonjs/text-encoding";
import { Event, EventTarget } from "event-target-shim";
import { AbortController, AbortSignal } from "abortcontroller-polyfill/dist/cjs-ponyfill";
import structuredClone from "@ungap/structured-clone";
// QuickJS has no Intl. Pi Durable's tool search constructs an Intl.Segmenter when its module loads.
import "@formatjs/intl-getcanonicallocales/polyfill-force.js";
import "@formatjs/intl-segmenter/polyfill-force.js";

const g = globalThis;
g.TextEncoder ??= encoding.TextEncoder; g.TextDecoder ??= encoding.TextDecoder;
g.EventTarget ??= EventTarget; g.Event ??= Event;
g.AbortController ??= AbortController; g.AbortSignal ??= AbortSignal;
g.structuredClone ??= (value) => structuredClone(value);
g.setTimeout = (f, ms, ...args) => os.setTimeout(() => f(...args), ms ?? 0);
g.clearTimeout = (timer) => { if (timer !== undefined && timer !== null) os.clearTimeout(timer); };
// Standard output is the host's; what the agent logs goes to standard error.
const log = (...args) => { std.err.puts(args.map((a) => (typeof a === "string" ? a : JSON.stringify(a))).join(" ") + "\n"); std.err.flush(); };
g.console = { log, info: log, warn: log, error: log, debug: () => {} };
// ponytail: ids and retry jitter only, nothing secret, so the engine's own generator will do; a host request if
// that changes.
g.crypto ??= {};
g.crypto.getRandomValues ??= (array) => { const b = new Uint8Array(array.buffer, array.byteOffset, array.byteLength); for (let i = 0; i < b.length; i++) b[i] = Math.floor(Math.random() * 256); return array; };
g.crypto.randomUUID ??= () => {
  const b = g.crypto.getRandomValues(new Uint8Array(16)); b[6] = (b[6] & 15) | 64; b[8] = (b[8] & 63) | 128;
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
};

const send = (message) => { std.out.puts(JSON.stringify(message) + "\n"); std.out.flush(); };
const pending = new Map();
let last = 0, pumping = false, onStop;

// pump reads the host's lines while a request is unanswered. A read blocks the whole program, which is right: with
// a request out and no answer there is nothing else for it to do. After each line, whatever that line woke runs
// before the next is waited for.
async function pump() {
  if (pumping) return;
  pumping = true;
  while (pending.size) {
    const line = std.in.getline();
    if (line === null) std.exit(130); // the host is gone
    const m = JSON.parse(line), p = pending.get(m.id);
    if (m.stop) onStop?.();
    else if (p && m.event !== undefined) p.onEvent?.(m.event);
    else if (p) { pending.delete(m.id); m.error !== undefined ? p.reject(new Error(m.error)) : p.resolve(m.result); }
    await new Promise((run) => os.setTimeout(run, 0));
  }
  pumping = false;
}

// hostCall asks the host for one thing. The promise it returns carries the request's id, for __cancel.
g.hostCall = (op, arg, onEvent) => {
  const id = ++last;
  const answered = new Promise((resolve, reject) => pending.set(id, { resolve, reject, onEvent }));
  answered.id = id;
  send({ id, op, arg });
  os.setTimeout(pump, 0);
  return answered;
};
g.__cancel = (id) => send({ cancel: id });

export const tell = (event) => send({ event });
export const whenStopped = (f) => { onStop = f; };
export const env = (name, otherwise) => std.getenv(name) || otherwise;
export const exit = (code) => std.exit(code);
