import test from "node:test";
import assert from "node:assert/strict";
import { geminiRequest, geminiChunks } from "./gemini.js";

test("a signed parallel tool turn survives serialization and keeps each signature on its own call", () => {
  const chunk = geminiChunks();
  const first = { choices: [{ index: 0, delta: { tool_calls: [
    { index: 0, id: "a", function: { name: "read" } },
    { index: 1, id: "b", function: { name: "write" }, extra_content: { google: { thought_signature: "opaque-b" } } },
  ] } }] };
  chunk(first);
  const last = { choices: [{ index: 0, delta: { tool_calls: [
    { index: 0, extra_content: { google: { thought_signature: "opaque-a" } } },
  ] }, finish_reason: "stop" }] };
  chunk(last);
  assert.equal(last.choices[0].finish_reason, "tool_calls");
  const details = [...first.choices[0].delta.reasoning_details, ...last.choices[0].delta.reasoning_details];
  const history = JSON.parse(JSON.stringify({ role: "assistant", reasoning_details: details,
    tool_calls: [{ id: "a" }, { id: "b" }, { id: "unsigned" }] }));
  const params = { store: false, messages: [history] };
  geminiRequest(params);
  assert.deepEqual(params, { messages: [{ role: "assistant", tool_calls: [
    { id: "a", extra_content: { google: { thought_signature: "opaque-a" } } },
    { id: "b", extra_content: { google: { thought_signature: "opaque-b" } } }, { id: "unsigned" },
  ] }] });
});

test("a text-only answer keeps its normal stop reason", () => {
  const result = { choices: [{ index: 0, delta: { content: "done" }, finish_reason: "stop" }] };
  geminiChunks()(result);
  assert.deepEqual(result, { choices: [{ index: 0, delta: { content: "done" }, finish_reason: "stop" }] });
});

test("a signature may precede the call ID and one choice cannot finish another", () => {
  const chunk = geminiChunks();
  chunk({ choices: [{ index: 1, delta: { tool_calls: [
    { index: 0, extra_content: { google: { thought_signature: "delayed" } } },
  ] } }] });
  const last = { choices: [
    { index: 0, delta: {}, finish_reason: "stop" },
    { index: 1, delta: { tool_calls: [{ index: 0, id: "a" }] }, finish_reason: "stop" },
  ] };
  chunk(last);
  assert.equal(last.choices[0].finish_reason, "stop");
  assert.equal(last.choices[1].delta.reasoning_details[0].id, "a");
  assert.equal(last.choices[1].delta.reasoning_details[0].data, "delayed");
  assert.equal(last.choices[1].finish_reason, "tool_calls");
});

test("a request sends signatures only on calls from the same assistant message", () => {
  const params = { store: false, messages: [
    { role: "assistant", tool_calls: [{ id: "repeat" }], reasoning_details: [
      { type: "reasoning.encrypted", format: "google-gemini-v1", id: "repeat", data: "first" },
    ] },
    { role: "assistant", tool_calls: [{ id: "repeat" }] },
  ] };
  geminiRequest(params);
  assert.equal(params.messages[0].tool_calls[0].extra_content.google.thought_signature, "first");
  assert.deepEqual(params.messages[1].tool_calls, [{ id: "repeat" }]);
});
