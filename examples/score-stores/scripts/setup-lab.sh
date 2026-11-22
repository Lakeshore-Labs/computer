#!/bin/bash
# Usage: setup-lab.sh LAB_DIR SOURCE_DIR   -- builds synthetic roots + a NEW temp rclone config. Never touches ~/.config/rclone.
set -euo pipefail
LAB=$(cd "$1" && pwd); SRC=$(cd "$2" && pwd)
R=$LAB/roots; mkdir -p "$R"/{alias-real,combine-a,combine-b,union-seeded,union-b,hasher-store,compress-store,chunker-store,crypt-store,local-dest} "$LAB/cache" "$LAB/tmp"
printf 'synthetic union-seed note (not part of the score)\n' > "$R/union-seeded/SEED-NOTE.txt"
CONF=$LAB/rclone.conf; : > "$CONF"
export RCLONE_CONFIG=$CONF RCLONE_CACHE_DIR=$LAB/cache
PW=$(rclone obscure "$(openssl rand -hex 16)"); PW2=$(rclone obscure "$(openssl rand -hex 16)")
cat > "$CONF" <<CFG
[lab-src]
type = local
[lab-local]
type = local
[lab-alias]
type = alias
remote = $R/alias-real
[lab-combine]
type = combine
upstreams = "part-midi=$R/combine-a" "part-xml=$R/combine-b"
[lab-union]
type = union
upstreams = "$R/union-seeded:nc" "$R/union-b"
create_policy = ff
action_policy = epall
search_policy = ff
[lab-hasher]
type = hasher
remote = $R/hasher-store
hashes = md5,sha1,sha256
max_age = off
[lab-compress]
type = compress
remote = $R/compress-store
mode = gzip
[lab-chunker]
type = chunker
remote = $R/chunker-store
chunk_size = 1k
hash_type = md5
[lab-crypt]
type = crypt
remote = $R/crypt-store
filename_encryption = standard
directory_name_encryption = true
password = $PW
password2 = $PW2
[lab-memory]
type = memory
[lab-memory-root]
type = memory
CFG
chmod 600 "$CONF"
echo "$CONF"
