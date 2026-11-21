// Runs Pi Durable's conformance suite for an ExecutionEnv against the computer's, and prints a JSON line a case.
import { exit, run, tell } from "./host.js";
import { createEnvConformance } from "@earendil-works/pi-durable/testing";
import { computerEnv } from "./env.js";

run(async () => {
  const show = (v) => { try { return JSON.stringify(v, (_k, x) => (x instanceof Uint8Array ? [...x] : x)); } catch { return String(v); } };
  const same = (a, b, partial) => {
    if (Object.is(a, b)) return true;
    if (a instanceof Uint8Array || b instanceof Uint8Array) return a instanceof Uint8Array && b instanceof Uint8Array && a.length === b.length && a.every((x, i) => x === b[i]);
    if (typeof a !== "object" || typeof b !== "object" || a === null || b === null || Array.isArray(a) !== Array.isArray(b)) return false;
    const keys = (o) => Object.keys(o).filter((k) => o[k] !== undefined);
    if (!partial && keys(a).length !== keys(b).length) return false;
    return keys(b).every((k) => same(a[k], b[k], partial));
  };
  const check = (pass, message) => { if (!pass) throw new Error(message); };
  const assertions = {
    ok: (value, message) => check(value, message ?? `expected a true value, got ${show(value)}`),
    strictEqual: (actual, expected) => check(Object.is(actual, expected), `expected ${show(expected)}, got ${show(actual)}`),
    deepEqual: (actual, expected) => check(same(actual, expected, false), `expected ${show(expected)}, got ${show(actual)}`),
    partialDeepEqual: (actual, expected) => check(same(actual, expected, true), `expected to include ${show(expected)}, got ${show(actual)}`),
    greaterThan: (actual, expected) => check(actual > expected, `expected more than ${expected}, got ${actual}`),
    rejects: async (operation, includes) => { try { await operation; } catch (e) { return check(String(e?.message ?? e).includes(includes), `rejected with ${e?.message}, not ${includes}`); } throw new Error("did not reject"); },
  };

  let n = 0, failed = 0;
  const home = scriptArgs[1] ?? "/home/user";
  const withEnv = async (use) => {
    const env = computerEnv({ cwd: `${home}/case-${++n}` });
    const made = await env.createDir(env.cwd, { recursive: true }, undefined);
    if (!made.ok) throw made.error;
    await use(env);
  };
  // The computer's shell makes no symbolic links, and the optional listFiles is not implemented.
  for (const c of createEnvConformance({ assertions, withEnv, shell: ["sh", "-c"], symlinks: false, listFiles: false })) {
    try { await c.run(); tell({ case: c.name, ok: true }); } catch (e) { failed++; tell({ case: c.name, ok: false, error: String(e?.message ?? e).slice(0, 400) }); }
  }
  exit(failed ? 1 : 0);
});
