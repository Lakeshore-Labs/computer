// Pi Durable's ExecutionEnv over the computer the agent runs in: `exec` is the computer's shell and the file methods
// are its filesystem, each one request to the host. The agent runs no command and opens no file of the person's itself.
import { err, ExecutionError, FileError, LineScanner, ok } from "@earendil-works/pi-durable/env";
import { fromBase64, toBase64 } from "./base64.js";

const encoder = new TextEncoder(), decoder = new TextDecoder();

export function hostEnv({ cwd = "/home/user", id = "computer" } = {}) {
  const resolve = (path) => { const out = []; for (const part of (path.startsWith("/") ? path : `${cwd}/${path}`).split("/")) { if (part === "..") out.pop(); else if (part && part !== ".") out.push(part); } return "/" + out.join("/"); };
  const vfs = async (op, path, more) => {
    const r = await hostCall("file", { op, path, ...more });
    if (r.err) throw new FileError(r.err === "ENOENT" ? "not_found" : "unknown", r.err === "ENOENT" ? `no such file or directory: ${path}` : r.err, path);
    return r.out;
  };
  const wrap = (f) => async (path, ...rest) => { const p = resolve(path); try { return ok(await f(p, ...rest)); } catch (e) { return err(e instanceof FileError ? e : new FileError("unknown", String(e?.message ?? e), p)); } };
  const info = async (p) => { const s = await vfs("stat", p); return { name: s.name, path: p, kind: s.isDirectory ? "directory" : "file", size: s.size, mtimeMs: s.mtimeMs }; };
  return {
    id, cwd,
    absolutePath: async (path) => ok(resolve(path)),
    joinPath: async (parts) => ok(parts.join("/").replace(/\/+/g, "/")),
    canonicalPath: wrap(async (p) => { await vfs("stat", p); return p; }),
    exists: wrap(async (p) => { try { await vfs("stat", p); return true; } catch { return false; } }),
    fileInfo: wrap(info),
    readTextFile: wrap(async (p) => decoder.decode(fromBase64(await vfs("read", p)))),
    writeFile: wrap(async (p, content) => { await vfs("write", p, { data: toBase64(typeof content === "string" ? encoder.encode(content) : content) }); }),
    openBinaryReader: wrap(async (p) => {
      const meta = await info(p);
      if (meta.kind === "directory") throw new FileError("is_directory", "illegal operation on a directory, read", p);
      const bytes = fromBase64(await vfs("read", p));
      return {
        info: async () => ok(meta),
        read: async (offset, length) => ok(bytes.subarray(offset, offset + length)),
        scanLines: async ({ startLine, endLine }) => { const s = new LineScanner(startLine, endLine); s.push(bytes); return ok(s.finish()); },
        close: async () => {},
      };
    }),
    // ponytail: output arrives when the command ends.
    exec: async (command, options, context) => {
      if (typeof command !== "string") return err(new ExecutionError("spawn_error", "no program can be started here; use a shell command"));
      try {
        const r = await hostCall("sh", { command, cwd: options?.cwd ?? cwd });
        if (r.stdout) options?.onOutput?.(r.stdout, context, { stream: "stdout" });
        if (r.stderr) options?.onOutput?.(r.stderr, context, { stream: "stderr" });
        return ok({ exitCode: r.exitCode });
      } catch (e) { return err(new ExecutionError(context.abortSignal?.aborted ? "aborted" : "unknown", String(e?.message ?? e))); }
    },
    cleanup: async () => {},
  };
}
