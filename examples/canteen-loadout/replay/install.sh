#!/bin/sh
# Pinned installs (~110 MiB). Usage: sh install.sh SUPPORT_DIR   (needs uv, node/npm, and PostgreSQL 17 binaries on the host)
set -e
S="$1"; mkdir -p "$S/uv/tools" "$S/uv/bin" "$S/npm"
UV_TOOL_DIR="$S/uv/tools" UV_TOOL_BIN_DIR="$S/uv/bin" uv tool install --python 3.12 mcp-server-motherduck==1.1.1   # brings duckdb 1.5.6, local-only
(cd "$S/npm" && npm init -y >/dev/null && npm install --no-audit --no-fund @modelcontextprotocol/server-postgres@0.6.2)  # ARCHIVED upstream
uv run --python 3.13 --no-project --with pyarrow==26.0.1 python -I -c "import pyarrow.parquet"
