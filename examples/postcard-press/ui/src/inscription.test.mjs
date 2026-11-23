import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { checkInscription } from "./inscription.js";

test("the form accepts 48 printable code points and rejects 49", () => {
  const markup = readFileSync(new URL("./index.html", import.meta.url), "utf8");
  const input = markup.match(/<input\b[^>]*\bid="inscription"[^>]*>/s)[0];
  const maxUnits = Number(input.match(/\bmaxlength="(\d+)"/)?.[1] ?? Infinity);
  for (const n of [25, 48, 49]) {
    const { length, error } = checkInscription("🌷".repeat(n).slice(0, maxUnits));
    assert.equal(length, n);
    assert.equal(Boolean(error), n > 48);
  }
});
