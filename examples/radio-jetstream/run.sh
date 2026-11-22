#!/usr/bin/env bash
# Producer = stock Computer (stdio upstream mcp-nats via generated Python bindings). Subscriber = Node raw nats.js.
# usage: run.sh --computer BIN --python-dir DIR --nats-server BIN --mcp-nats PATH --out NEWDIR [--node BIN] [--node-modules DIR]
#               [--chrome BIN --ffmpeg BIN] [--runs N]
#   --python-dir  directory holding python.wasm and lib/ (a CPython built for WASI)
#   --mcp-nats    mcp-nats executable, or its dist/index.js (run with --node)
#   --node-modules  optional dir holding installed `nats` 2.29.3; otherwise `npm ci` runs in each run's app dir
# Each run uses a fresh isolated root under --out. Server binds 127.0.0.1 only; no accounts.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPUTER=; PYDIR=; NATSBIN=; MCP=; OUT=; NODE=node; NM=; CHROME=; FFMPEG=; RUNS=2
while [ $# -gt 0 ]; do case "$1" in
  --computer) COMPUTER="$2";; --python-dir) PYDIR="$2";; --nats-server) NATSBIN="$2";; --mcp-nats) MCP="$2";;
  --out) OUT="$2";; --node) NODE="$2";; --node-modules) NM="$2";; --chrome) CHROME="$2";; --ffmpeg) FFMPEG="$2";; --runs) RUNS="$2";;
  *) echo "unknown option $1" >&2; exit 2;; esac; shift 2; done
for v in COMPUTER PYDIR NATSBIN MCP OUT; do [ -n "${!v}" ] || { echo "missing --$(echo "$v" | tr A-Z a-z)" >&2; exit 2; }; done
[ ! -e "$OUT" ] || { echo "--out must not exist" >&2; exit 2; }
[ -f "$PYDIR/python.wasm" ] && [ -d "$PYDIR/lib" ] || { echo "--python-dir needs python.wasm and lib/" >&2; exit 2; }
if [ -n "$CHROME" ]; then [ -n "$FFMPEG" ] || { echo "--chrome needs --ffmpeg" >&2; exit 2; }; fi
mkdir -p "$OUT"; OUT="$(cd "$OUT" && pwd)"
now(){ "$NODE" -e 'console.log(new Date().toISOString())'; }
ev(){ printf '%s %s\n' "$(now)" "$*" >>"$LOG"; }
for n in $(seq "$RUNS"); do
  R="$OUT/run-$n"; mkdir -p "$R"/{app,work,js-store,proof,logs}; LOG="$R/logs/events.log"; : >"$LOG"
  cp "$HERE/package.json" "$HERE/package-lock.json" "$HERE"/scripts/* "$R/app/"
  mkdir -p "$R/scripts-ro"; cp "$HERE/scripts/guest_producer.py" "$R/scripts-ro/"
  if [ -n "$NM" ]; then ln -s "$NM" "$R/app/node_modules"; else (cd "$R/app" && npm ci --ignore-scripts --no-audit --no-fund --silent); fi
  PORT="$("$NODE" -e 'require("net").createServer().listen(0,"127.0.0.1",function(){console.log(this.address().port);this.close()})')"
  printf 'host: 127.0.0.1\nport: %s\nserver_name: radio-loopback\njetstream { store_dir: "%s" }\n' "$PORT" "$R/js-store" >"$R/nats.conf"
  export NATS_URL="nats://127.0.0.1:$PORT"
  start(){ "$NATSBIN" -c "$R/nats.conf" >>"$R/logs/nats-server.log" 2>&1 & echo $! >"$R/nats.pid"
    for _ in $(seq 100); do "$NODE" -e 'require("net").connect(+process.argv[1],"127.0.0.1").on("connect",function(){process.exit(0)}).on("error",function(){process.exit(1)})' "$PORT" && return 0; sleep 0.1; done; return 1; }
  stop(){ local p; p="$(cat "$R/nats.pid")"; kill "$p"; while kill -0 "$p" 2>/dev/null; do sleep 0.1; done; }
  trap 'kill "$(cat "$R/nats.pid" 2>/dev/null)" 2>/dev/null || true' EXIT
  start; ev "run $n server up 127.0.0.1:$PORT"
  "$NODE" "$R/app/mkconfig.mjs" "$R/computer.json" "$R/work" "$R/scripts-ro" "$MCP" "$NATS_URL" "$NODE"
  (cd "$R/app" && "$NODE" setup.mjs >"$R/proof/setup.json")
  # --- producer inside stock Computer: Python bindings + one generated CLI call ---
  "$COMPUTER" -config "$R/computer.json" -python "$PYDIR/python.wasm" -python-lib "$PYDIR/lib" -c 'set -eo pipefail
python3 /scripts/guest_producer.py
nats nats-obj-info --bucket CUES --name stinger-v2.wav.b64 > /work/cli-obj-info.json' >"$R/logs/computer.out" 2>"$R/logs/computer.err"
  ev "run $n producer done (T1 in producer-proof.json)"
  cp "$R/work/producer-proof.json" "$R/work/cli-obj-info.json" "$R/proof/"
  stop; ev "run $n server stopped"; start; ev "run $n server restarted"
  (cd "$R/app" && "$NODE" receiver.mjs "$R/proof" >/dev/null && "$NODE" verify.mjs "$R/proof" >/dev/null); ev "run $n receiver acked + raw verify pass"
  cp "$R/work/cue-guest.wav" "$R/proof/cue-guest.wav"
  if [ -n "$CHROME" ]; then
    "$NODE" "$R/app/listen_page.mjs" "$R/proof" "$R/proof/control-room.html"
    "$NODE" "$R/app/browser_listen.mjs" "$CHROME" "$FFMPEG" "$R/proof/control-room.html" "$R/proof" >"$R/logs/listen.out"; ev "run $n browser listening verified"
  fi
  stop; trap - EXIT; ev "run $n done"
done
echo "OK runs=$RUNS out=$OUT"
