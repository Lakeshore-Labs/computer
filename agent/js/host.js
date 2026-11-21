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
let last = 0, onStop;

// The host's lines are read as they arrive, by the engine's event loop, so timers go on firing while a request is
// unanswered: Pi Durable's own timers, and a caller's that gives a request up.
const chunk = new Uint8Array(65536), decoder = new g.TextDecoder();
let held = new Uint8Array(0);
os.setReadHandler(0, () => {
  const n = os.read(0, chunk.buffer, 0, chunk.length);
  if (n <= 0) std.exit(130); // the host is gone
  const all = new Uint8Array(held.length + n);
  all.set(held); all.set(chunk.subarray(0, n), held.length);
  let start = 0;
  for (let end = all.indexOf(10); end >= 0; start = end + 1, end = all.indexOf(10, start)) {
    const m = JSON.parse(decoder.decode(all.subarray(start, end))), p = pending.get(m.id);
    if (m.stop) onStop?.();
    else if (p && m.event !== undefined) p.onEvent?.(m.event);
    else if (p) { pending.delete(m.id); m.error !== undefined ? p.reject(new Error(m.error)) : p.resolve(m.result); }
  }
  held = all.slice(start);
});

// hostCall asks the host for one thing. The promise it returns carries the request's id, for __cancel.
g.hostCall = (op, arg, onEvent) => {
  const id = ++last;
  const answered = new Promise((resolve, reject) => pending.set(id, { resolve, reject, onEvent }));
  answered.id = id;
  send({ id, op, arg });
  return answered;
};
g.__cancel = (id) => send({ cancel: id });

export const tell = (event) => send({ event });
export const whenStopped = (f) => { onStop = f; };
export const env = (name, otherwise) => std.getenv(name) || otherwise;
export const exit = (code) => std.exit(code);

// run runs the program. QuickJS drives a module paused on a top-level await with a loop that runs one ready job and
// then waits for input, so a program that awaits at its top level crawls or stalls once it reads from the host. Inside
// a function the engine's ordinary loop applies, which runs everything ready before it waits.
export const run = (main) => { main().catch((e) => { console.log(String(e?.stack ?? e)); std.exit(1); }); };
