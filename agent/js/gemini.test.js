import test from "node:test";
import assert from "node:assert/strict";
import { stream } from "@earendil-works/pi-ai/api/openai-completions";
import { geminiChunks, geminiRequest } from "./gemini.js";

test("a signed parallel tool turn survives serialization and keeps each signature on its own call", async () => {
  const chunk = geminiChunks();
  const first = { choices: [{ index: 0, delta: { tool_calls: [
    { index: 0, id: "a", function: { name: "read" } },
    { index: 1, id: "b", function: { name: "write" }, extra_content: { google: { thought_signature: "opaque-b" } } },
    { index: 2, id: "unsigned", function: { name: "read" } },
  ] } }] };
  chunk(first);
  const last = { choices: [{ index: 0, delta: { tool_calls: [
    { index: 0, extra_content: { google: { thought_signature: "opaque-a" } } },
  ] }, finish_reason: "stop" }] };
  chunk(last);
  assert.equal(last.choices[0].finish_reason, "tool_calls");
  // Round-trip the assistant message in the same JSON form that Pi Durable persists.
  const model = { id: "fixture", api: "openai-completions", provider: "host", compat: { supportsStore: false },
    baseUrl: "https://example.invalid", input: ["text"], cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } };
  const user = { role: "user", content: "Use both tools.", timestamp: 0 };
  const response = (chunks) => new Response(
    chunks.map((value) => "data: " + JSON.stringify(value) + "\n\n").join("") + "data: [DONE]\n\n",
    { headers: { "Content-Type": "text/event-stream" } });
  const reply = await stream(model, { messages: [user] }, {
    apiKey: "offline", fetch: async () => response([first, last]),
  }).result();
  assert.equal(reply.stopReason, "toolUse");
  assert.equal(reply.provider, "host");
  const history = JSON.parse(JSON.stringify(reply));
  const results = history.content.filter((part) => part.type === "toolCall").map((call) => ({
    role: "toolResult", toolCallId: call.id, toolName: call.name, content: [{ type: "text", text: "done" }], timestamp: 1,
  }));
  let params;
  await stream(model, { messages: [user, history, ...results] }, {
    apiKey: "offline", fetch: async (_url, request) => {
      params = JSON.parse(request.body);
      return response([{ choices: [{ delta: { content: "done" }, finish_reason: "stop" }] }]);
    },
  }).result();
  assert.equal(Object.hasOwn(params, "store"), false);
  geminiRequest(params);
  const request = structuredClone(params);
  geminiRequest(params); // Pi retries the same parameter object.
  assert.deepEqual(params, request);
  const assistant = params.messages.find((message) => message.role === "assistant");
  assert.equal(Object.hasOwn(assistant, "reasoning_details"), false);
  assert.deepEqual(assistant.tool_calls.map((call) => [call.id, call.extra_content?.google.thought_signature]),
    [["a", "opaque-a"], ["b", "opaque-b"], ["unsigned", undefined]]);
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
  const params = { messages: [
    { role: "assistant", tool_calls: [{ id: "repeat" }], reasoning_details: [
      { type: "reasoning.encrypted", format: "google-gemini-v1", id: "repeat", data: "first" },
    ] },
    { role: "assistant", tool_calls: [{ id: "repeat" }] },
  ] };
  geminiRequest(params);
  assert.equal(params.messages[0].tool_calls[0].extra_content.google.thought_signature, "first");
  assert.deepEqual(params.messages[1].tool_calls, [{ id: "repeat" }]);
});
