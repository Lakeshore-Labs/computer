import * as std from "qjs:std";
import * as os from "qjs:os";
std.out.puts(JSON.stringify({ id: 1, op: "sh", arg: { command: "seq 1 60000" } }) + "\n"); std.out.flush();
std.exit(0);
