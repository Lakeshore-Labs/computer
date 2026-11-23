#!/usr/bin/env bash
# OPTIONAL, tested on macOS arm64. usage: fetch-macos.sh NEWDIR  -> prints the --nats-server and --mcp-nats values.
# Downloads official release assets (github.com nats-io/nats-server v2.15.3; registry.npmjs.org mcp-nats 0.1.7) into NEWDIR and verifies checksums.
# Linux works too if you change the suffix to linux-amd64/linux-arm64; the run.sh recipe itself is OS-independent.
set -euo pipefail
D="$1"; [ ! -e "$D" ] || { echo "NEWDIR must not exist" >&2; exit 2; }
case "$(uname -s)-$(uname -m)" in Darwin-arm64) SUF=darwin-arm64;; Darwin-x86_64) SUF=darwin-amd64;; Linux-x86_64) SUF=linux-amd64;; Linux-aarch64) SUF=linux-arm64;; *) echo unsupported >&2; exit 2;; esac
NV=v2.15.3; A="nats-server-$NV-$SUF.tar.gz"; MCP_SHA1=fedf6ba47e557124ed1d38e363d32de2b7f44fbe
mkdir -p "$D"/{dl-nats,dl-mcp,nats,mcp}
curl -fsSL -o "$D/dl-nats/$A" "https://github.com/nats-io/nats-server/releases/download/$NV/$A"
curl -fsSL -o "$D/dl-nats/SHA256SUMS" "https://github.com/nats-io/nats-server/releases/download/$NV/SHA256SUMS"
grep " $A\$" "$D/dl-nats/SHA256SUMS" >"$D/dl-nats/one.sum"; (cd "$D/dl-nats" && shasum -a 256 -c one.sum >/dev/null)
curl -fsSL -o "$D/dl-mcp/mcp-nats-0.1.7.tgz" "https://registry.npmjs.org/mcp-nats/-/mcp-nats-0.1.7.tgz"
[ "$(shasum "$D/dl-mcp/mcp-nats-0.1.7.tgz" | cut -d' ' -f1)" = "$MCP_SHA1" ]
tar -xzf "$D/dl-nats/$A" -C "$D/nats"; tar -xzf "$D/dl-mcp/mcp-nats-0.1.7.tgz" -C "$D/mcp"
echo "--nats-server $(cd "$D/nats/nats-server-$NV-$SUF" && pwd)/nats-server --mcp-nats $(cd "$D/mcp/package/dist" && pwd)/index.js"
