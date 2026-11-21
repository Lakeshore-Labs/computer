// What pi-ai's openai-completions module uses of the `openai` package, served by the host: the host makes the
// request, adds the credential, reads the event stream, and hands over each chunk as it arrives. Nothing here uses
// fetch or a web stream. A refused request rejects with an error carrying `status` and `headers`, the two fields
// pi-ai's retry reads (utils/provider-retry.js).
export default class OpenAI {
  constructor({ baseURL, defaultHeaders }) {
    this.chat = { completions: { create: (params, { signal } = {}) => ({ withResponse: () => request(baseURL, defaultHeaders, params, signal) }) } };
  }
}
const aborted = () => Object.assign(new Error("Request was aborted."), { name: "AbortError" });
function request(url, headers, params, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(aborted());
    const chunks = []; let wake, over = false, failure, started = false;
    const fail = (e) => { if (over) return; failure = e; over = true; started ? wake?.() : reject(e); };
    const call = hostCall("model", { url, headers, params }, (ev) => {
      if (ev.status !== undefined) { started = true; resolve({ data, response: { status: ev.status, headers: new Map(Object.entries(ev.headers)) } }); }
      else { chunks.push(ev); wake?.(); }
    });
    call.then(() => { over = true; wake?.(); }, (e) => fail(Object.assign(e, { headers: undefined })));
    signal?.addEventListener("abort", () => { __cancel(call.id); fail(aborted()); });
    const data = { async *[Symbol.asyncIterator]() {
      for (;;) {
        if (chunks.length) { yield chunks.shift(); continue; }
        if (failure) throw failure;
        if (over) return;
        await new Promise((f) => { wake = f; });
      }
    } };
  });
}
