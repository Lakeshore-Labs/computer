import * as std from "qjs:std";
std.out.puts(JSON.stringify({ id: 1, op: "sh", arg: { command: "seq 1 60000" } }) + "\n"); std.out.flush();
for (;;) {}
