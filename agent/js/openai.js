// What pi-ai's openai-completions module uses of the `openai` package, served by the host: the host makes the
// request with the model's address and key, reads the event stream, and hands over each chunk as it arrives.
//
// pi-ai calls `client.chat.completions.create(params, { signal, timeout }).withResponse()` and expects
// `{ data, response }`: an async iterable of chunks, and the response's status and headers. A request that fails
// must reject with an error that has `status` and `headers`, as the package's own errors do: pi-ai's retry
// (utils/provider-retry.js) tries again on 408, 409, 429, 5xx and on a failure with no status, after the time the
// `retry-after` header names, and treats an error without those two fields as final.
import { env } from "./host.js";
import { geminiChunks, geminiRequest } from "./gemini.js";

export default class OpenAI {
  constructor() {
    this.chat = { completions: { create: (params, { signal, timeout } = {}) => ({ withResponse: () => request(params, signal, timeout) }) } };
  }
}

const aborted = () => Object.assign(new Error("Request was aborted."), { name: "AbortError" });
// The host's failure as the package would report it: a status when the model answered with one, none otherwise.
const failure = (e) => Object.assign(e, { status: e.detail?.status, headers: e.detail?.retryAfter ? new Headers({ "retry-after": e.detail.retryAfter }) : undefined });

function request(params, signal, timeoutMs) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(aborted());
    const google = env("AGENT_MODEL_COMPAT") === "gemini";
    const adaptChunk = google ? geminiChunks() : undefined;
    if (google) geminiRequest(params);
    const chunks = [];
    let wake, over = false, failed, started = false;
    const fail = (e) => { if (over) return; failed = e; over = true; started ? wake?.() : reject(e); };
    const call = hostCall("model", { params, timeoutMs }, (event) => {
      if (event.status === undefined) { adaptChunk?.(event); chunks.push(event); wake?.(); return; }
      started = true;
      resolve({ data, response: { status: event.status, headers: new Headers(event.headers) } });
    });
    call.then(() => { over = true; wake?.(); }, (e) => fail(failure(e)));
    signal?.addEventListener("abort", () => { __cancel(call.id); fail(aborted()); });
    const data = { async *[Symbol.asyncIterator]() {
      for (;;) {
        if (chunks.length) { yield chunks.shift(); continue; }
        if (failed) throw failed;
        if (over) return;
        await new Promise((ready) => { wake = ready; });
      }
    } };
  });
}
