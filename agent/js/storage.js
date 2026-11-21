// The files Pi Durable's JSONL storage keeps a conversation in, on the computer's own filesystem, so that every
// commit is on disk when it is made and a later run of the agent opens the conversation where it stood.
// Only the methods JsonlStorage calls are implemented.
import * as std from "qjs:std";
import * as os from "qjs:os";
import { err, FileError, ok } from "@earendil-works/pi-durable/env";

const encoder = new TextEncoder();
const bytes = (content) => (typeof content === "string" ? encoder.encode(content) : content);
const failed = (path, code) => err(new FileError(code === 44 || code === 2 ? "not_found" : "unknown", `${path}: error ${code}`, path));

export function diskFiles() {
  const stat = (path) => { const [s, e] = os.stat(path); return e ? undefined : s; };
  const info = (path, s) => ({ name: path.slice(path.lastIndexOf("/") + 1), path, kind: (s.mode & os.S_IFMT) === os.S_IFDIR ? "directory" : "file", size: s.size, mtimeMs: s.mtime });
  const put = (path, content, mode) => {
    const e = {}, f = std.open(path, mode, e);
    if (!f) return failed(path, e.errno);
    const b = bytes(content);
    if (b.byteLength) f.write(b.buffer, b.byteOffset, b.byteLength);
    f.close();
    return ok(undefined);
  };
  const dirs = (path) => { let at = ""; for (const part of path.split("/").filter(Boolean)) { at += "/" + part; if (!stat(at)) os.mkdir(at, 0o755); } };
  return {
    id: "computer", cwd: "/",
    absolutePath: async (path) => ok(path),
    joinPath: async (parts) => ok(parts.join("/").replace(/\/+/g, "/")),
    createDir: async (path) => { dirs(path); return ok(undefined); },
    flushFile: async () => ok(undefined), // each write is closed before it returns; WASI offers no stronger flush here
    listDir: async (dir) => { const [names, e] = os.readdir(dir); if (e) return failed(dir, -e); return ok(names.filter((n) => n !== "." && n !== "..").map((n) => info(`${dir}/${n}`, stat(`${dir}/${n}`)))); },
    fileInfo: async (path) => { const s = stat(path); return s ? ok(info(path, s)) : failed(path, 44); },
    readBinaryFile: async (path) => { const b = std.loadFile(path, { binary: true }); return b === null ? failed(path, 44) : ok(b); },
    writeFile: async (path, content) => put(path, content, "wb"),
    appendFile: async (path, content) => put(path, content, "ab"),
    truncateFile: async (path, size) => { const b = std.loadFile(path, { binary: true }); return b === null ? failed(path, 44) : put(path, b.slice(0, size), "wb"); },
    renameFile: async (from, to) => { const e = os.rename(from, to); return e ? failed(from, -e) : ok(undefined); },
    remove: async (path, options) => { const e = os.remove(path); return e && !options?.force ? failed(path, -e) : ok(undefined); },
  };
}
