import { test } from "node:test"
import assert from "node:assert/strict"

import { commandEnded, resize, socketUrl } from "./protocol.ts"

test("a relative URL is a WebSocket to the page's origin, wss on an https page", () => {
  assert.equal(socketUrl("/computer/terminal", { href: "https://example.com/desktop/" }), "wss://example.com/computer/terminal")
  assert.equal(socketUrl("/computer/terminal", { href: "http://127.0.0.1:8113/desktop/" }), "ws://127.0.0.1:8113/computer/terminal")
  assert.equal(socketUrl("terminal", { href: "http://localhost/a/b" }), "ws://localhost/a/terminal")
})

test("a WebSocket URL is kept as it is", () => {
  assert.equal(socketUrl("wss://other.example/t", { href: "http://localhost/" }), "wss://other.example/t")
})

test("a command's end is told by OSC 133's D mark, with its exit code", () => {
  assert.equal(commandEnded("D;0"), 0)
  assert.equal(commandEnded("D;130"), 130)
  assert.equal(commandEnded("D"), 0)
  assert.equal(commandEnded("A"), undefined)
  assert.equal(commandEnded("C;"), undefined)
})

test("a resize is the columns and rows as JSON", () => {
  assert.deepEqual(JSON.parse(resize(80, 24)), { cols: 80, rows: 24 })
})
