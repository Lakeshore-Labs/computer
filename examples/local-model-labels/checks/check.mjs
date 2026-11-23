#!/usr/bin/env node
// Functional check: node check.mjs READER_DIR OUT_JSON RUN_DIR [RUN_DIR ...]
// RUN_DIRs must be the same runs, in the same order, the reader was built from.
import fs from "node:fs";
import path from "node:path";
import { launch, verifyRuns } from "./lib.mjs";

const [readerDir, outFile, ...runs] = process.argv.slice(2);
if (!readerDir || !outFile || runs.length === 0) {
  console.error("usage: node check.mjs READER_DIR OUT_JSON RUN_DIR [RUN_DIR ...]");
  process.exit(2);
}
const session = await launch();
await session.view(1280, 800, false);
await session.open(readerDir);
const records = await verifyRuns(session, runs, "desktop");
const external = session.requestSchemes.filter((s) => !["file", "data", "blob"].includes(s));
const result = { runs: runs.map((r) => path.basename(r)), records, externalRequests: external.length,
  pass: records.every((r) => r.pass) && external.length === 0 };
fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
fs.writeFileSync(outFile, JSON.stringify(result, null, 2) + "\n");
console.log(`${result.pass ? "PASS" : "FAIL"}: ${records.length} run/stage checks (${runs.map((r) => path.basename(r)).join(", ")})`);
await session.close();
process.exit(result.pass ? 0 : 1);
