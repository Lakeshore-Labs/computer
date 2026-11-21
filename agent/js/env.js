// Pi Durable's ExecutionEnv over the computer the agent runs in. The agent is a program of that computer and has its
// directories, so the file methods use them directly; a command is one request to the host, which runs it in the
// computer's shell. Pi Durable's own conformance suite for an environment is run against this one (conformance.js).
import * as std from "qjs:std";
import * as os from "qjs:os";
import { err, ExecutionError, FileError, LineScanner, ok } from "@earendil-works/pi-durable/env";

const encoder = new TextEncoder(), decoder = new TextDecoder();
const bytesOf = (content) => (typeof content === "string" ? encoder.encode(content) : content);
// WASI's error numbers, as QuickJS reports them, and what Pi Durable calls each.
const codes = { 44: "not_found", 2: "permission_denied", 63: "permission_denied", 69: "permission_denied", 54: "not_directory", 31: "is_directory", 28: "invalid", 32: "invalid" };
const refuse = (errno, path) => { throw new FileError(codes[Math.abs(errno)] ?? "unknown", `${path}: ${std.strerror(Math.abs(errno))}`, path); };
const normalize = (path) => { const out = []; for (const part of path.split("/")) { if (part === "..") out.pop(); else if (part && part !== ".") out.push(part); } return "/" + out.join("/"); };
const quote = (word) => `'${word.replaceAll("'", `'\\''`)}'`;

// This QuickJS has no lstat. Nothing in the computer can make a symbolic link, and one the host left in a mount is
// followed only inside the mount, so stat serves for both.
const lstat = os.lstat ?? os.stat;

// `hidden` are directories this environment refuses: the agent program has them, and the tools that work through
// this environment are not to.
export function computerEnv({ cwd = "/home/user", id = "computer", hidden = [] } = {}) {
  const at = (path) => {
    const p = normalize(path.startsWith("/") ? path : `${cwd}/${path}`);
    if (hidden.some((dir) => p === dir || p.startsWith(dir + "/"))) throw new FileError("permission_denied", `${p} is not for this environment`, p);
    return p;
  };
  const stat = (path, follow = true) => { const [s, e] = (follow ? os.stat : lstat)(path); if (e) refuse(e, path); return s; };
  const kind = (s) => ((s.mode & os.S_IFMT) === os.S_IFDIR ? "directory" : (s.mode & os.S_IFMT) === os.S_IFLNK ? "symlink" : (s.mode & os.S_IFMT) === os.S_IFREG ? "file" : undefined);
  const info = (path, s) => ({ name: path.slice(path.lastIndexOf("/") + 1), path, kind: kind(s), size: s.size, mtimeMs: s.mtime });
  const file = (path) => { const s = stat(path); if (kind(s) === "directory") throw new FileError("is_directory", `${path} is a directory`, path); if (kind(s) !== "file") throw new FileError("invalid", `${path} is not a regular file`, path); return s; };
  const load = (path) => { file(path); const b = std.loadFile(path, { binary: true }); if (b === null) refuse(44, path); return b; };
  const put = (path, content, mode) => {
    const e = {}, f = std.open(path, mode, e);
    if (!f) refuse(e.errno, path);
    const b = bytesOf(content);
    if (b.byteLength) f.write(b.buffer, b.byteOffset, b.byteLength);
    f.close();
  };
  const entries = (path) => { if (kind(stat(path)) !== "directory") throw new FileError("not_directory", `${path} is not a directory`, path); const [names, e] = os.readdir(path); if (e) refuse(e, path); return names.filter((n) => n !== "." && n !== "..").sort(); };
  // The directories above a mount are not themselves in any mount and cannot be examined or made, so only the last
  // one's failure counts.
  const mkdirs = (path) => { let dir = "", failed = 0; for (const part of path.split("/").filter(Boolean)) { dir += "/" + part; failed = os.stat(dir)[0] ? 0 : os.mkdir(dir, 0o755); } if (failed) refuse(failed, path); if (kind(stat(path)) !== "directory") throw new FileError("not_directory", `${path} is not a directory`, path); };
  const removeAll = (path) => { if (kind(stat(path, false)) === "directory") for (const name of entries(path)) removeAll(`${path}/${name}`); const e = os.remove(path); if (e) refuse(e, path); };
  const unique = () => Array.from(crypto.getRandomValues(new Uint8Array(8)), (x) => x.toString(16).padStart(2, "0")).join("");
  const aborted = (context) => context?.abortSignal?.aborted;

  // Every file method takes its context last, answers an aborted one with `aborted`, and never throws.
  const method = (f) => async (...args) => {
    if (aborted(args.at(-1))) return err(new FileError("aborted", "aborted"));
    try { return ok(await f(...args)); } catch (e) { return err(e instanceof FileError ? e : new FileError("unknown", String(e?.message ?? e))); }
  };
  const closed = (path) => new FileError("invalid", `${path}: the reader is closed`, path);

  const openBinaryReader = (path, options) => {
    path = at(path);
    if (options?.noFollow && kind(stat(path, false)) === "symlink") throw new FileError("invalid", `${path} is a symbolic link`, path);
    const meta = info(path, file(path));
    let fd = os.open(path, os.O_RDONLY);
    if (fd < 0) refuse(fd, path);
    // The open file is read, whatever its path names by then.
    const whole = (n) => Number.isInteger(n) && n >= 0;
    const read = (offset, length) => {
      if (fd < 0) throw closed(path);
      if (!whole(offset) || !whole(length)) throw new FileError("invalid", `${path}: not a byte range: ${offset}, ${length}`, path);
      const buffer = new Uint8Array(Math.max(0, Math.min(length, meta.size - offset)));
      let got = 0;
      os.seek(fd, offset, std.SEEK_SET);
      while (got < buffer.length) { const n = os.read(fd, buffer.buffer, got, buffer.length - got); if (n <= 0) break; got += n; }
      return buffer.subarray(0, got);
    };
    return {
      info: method(() => { if (fd < 0) throw closed(path); return meta; }),
      read: method((offset, length) => read(offset, length)),
      scanLines: method(({ startLine, endLine }) => { if (!whole(startLine) || (endLine !== undefined && (!whole(endLine) || endLine <= startLine))) throw new FileError("invalid", `${path}: not a range of lines: ${startLine}, ${endLine}`, path); const scanner = new LineScanner(startLine, endLine); for (let offset = 0; offset < meta.size; offset += 65536) scanner.push(read(offset, 65536)); return scanner.finish(); }),
      close: async () => { if (fd >= 0) os.close(fd); fd = -1; },
    };
  };

  const openDirReader = (path) => {
    path = at(path);
    let names = entries(path), next = 0;
    return {
      next: method((maxEntries) => {
        if (!names) throw closed(path);
        if (!Number.isInteger(maxEntries) || maxEntries < 1) throw new FileError("invalid", `${path}: not a number of entries: ${maxEntries}`, path);
        const page = [];
        while (page.length < maxEntries && next < names.length) {
          const child = `${path}/${names[next++]}`, [s] = lstat(child);
          if (s && kind(s)) page.push(info(child, s)); // one removed since the listing, or of no supported kind, is skipped
        }
        return { entries: page, done: next >= names.length };
      }),
      close: async () => { names = undefined; },
    };
  };

  const lines = (path) => { const text = decoder.decode(load(at(path))), parts = text.split("\n"), last = parts.pop(); return [...parts.map((t) => ({ text: t, terminated: true })), ...(last ? [{ text: last, terminated: false }] : [])]; };

  // A string runs through the shell. An array is a program and its arguments, which reach it unparsed.
  const exec = async (command, options, context) => {
    if (aborted(context)) return err(new ExecutionError("aborted", "aborted"));
    const argv = typeof command === "string" ? undefined : [...command];
    if (argv && !argv.length) return err(new ExecutionError("spawn_error", "no program to run"));
    let dir;
    try { dir = at(options?.cwd ?? "."); } catch (e) { return err(new ExecutionError("spawn_error", e.message)); }
    const call = hostCall("sh", { command: argv ? undefined : command, argv, cwd: dir, env: options?.env, inheritEnv: options?.inheritEnv ?? true, timeoutMs: options?.timeout ? options.timeout * 1000 : undefined });
    const stop = () => __cancel(call.id);
    context?.abortSignal?.addEventListener("abort", stop);
    let r;
    try { r = await call; } catch (e) { return err(new ExecutionError(aborted(context) ? "aborted" : "unknown", String(e?.message ?? e))); } finally { context?.abortSignal?.removeEventListener("abort", stop); }
    if (aborted(context)) return err(new ExecutionError("aborted", "aborted"));
    // ponytail: output arrives when the command ends, a chunk a stream; the host streaming it is the upgrade.
    try {
      if (r.stdout) options?.onOutput?.(r.stdout, context, { stream: "stdout" });
      if (r.stderr) options?.onOutput?.(r.stderr, context, { stream: "stderr" });
    } catch (e) { return err(new ExecutionError("callback_error", String(e?.message ?? e))); }
    let spillPath;
    const output = r.stdout + r.stderr;
    if (options?.spill && (encoder.encode(output).length > options.spill.afterBytes || output.split("\n").length - 1 > options.spill.afterLines)) {
      spillPath = `/tmp/output-${unique()}.txt`;
      put(spillPath, output, "wb");
    }
    if (r.timedOut) return err(Object.assign(new ExecutionError("timeout", `timed out after ${options.timeout}s`), { spillPath }));
    if (r.notFound) return err(new ExecutionError("spawn_error", `${argv?.[0] ?? command}: command not found`));
    return ok({ exitCode: r.exitCode, ...(spillPath ? { spillPath } : {}) });
  };

  return {
    id, cwd,
    absolutePath: method((path) => at(path)),
    joinPath: method((parts) => parts.join("/").replace(/\/+/g, "/")),
    readTextFile: method((path) => decoder.decode(load(at(path)))),
    readBinaryFile: method((path) => load(at(path))),
    openTextLineReader: method((path) => { const all = lines(path); let next = 0; return { readLine: method(() => all[next++]), close: async () => {} }; }),
    readTextLines: method((path, options) => lines(path).slice(0, options?.maxLines).map((l) => l.text)),
    openBinaryReader: method(openBinaryReader),
    writeFile: method((path, content) => { path = at(path); mkdirs(path.slice(0, path.lastIndexOf("/")) || "/"); put(path, content, "wb"); }),
    appendFile: method((path, content) => put(at(path), content, "ab")),
    truncateFile: method((path, size) => { path = at(path); const now = load(path), next = new Uint8Array(size); next.set(now.subarray(0, size)); put(path, next, "wb"); }),
    flushFile: method(() => {}), // a write is closed before it returns; WASI offers this program no stronger flush
    renameFile: method((from, to) => { const e = os.rename(at(from), at(to)); if (e) refuse(e, at(from)); }),
    fileInfo: method((path) => info(at(path), stat(at(path), false))),
    listDir: method((path) => entries(at(path)).flatMap((name) => { const child = `${at(path)}/${name}`, [s] = lstat(child); return s && kind(s) ? [info(child, s)] : []; })),
    openDirReader: method(openDirReader),
    watch: method(() => { throw new FileError("not_supported", "this environment does not report changes"); }),
    canonicalPath: method((path) => { stat(at(path)); return at(path); }), // no symbolic link can be made in the computer
    exists: method((path) => !lstat(at(path))[1]),
    createDir: method((path, options) => { path = at(path); if (options?.recursive) return mkdirs(path); const e = os.mkdir(path, 0o755); if (e) refuse(e, path); }),
    remove: method((path, options) => { path = at(path); const [s, e] = lstat(path); if (e) { if (options?.force && Math.abs(e) === 44) return; refuse(e, path); } if (kind(s) === "directory" && options?.recursive) return removeAll(path); const failed = os.remove(path); if (failed) refuse(failed, path); }),
    createTempDir: method((prefix) => { const path = `/tmp/${prefix ?? "tmp-"}${unique()}`; mkdirs(path); return path; }),
    createTempFile: method((options) => { const path = `/tmp/${options?.prefix ?? "tmp-"}${unique()}${options?.suffix ?? ""}`; put(path, "", "wb"); return path; }),
    exec,
    cleanup: async () => {},
  };
}
