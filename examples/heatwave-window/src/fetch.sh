#!/bin/sh
# Runs INSIDE Computer (gbash + curl). Mounts: /src = cache (writable), /recipe = this dir (ro), /plan = plan.tsv (ro).
# plan.tsv lines: <object key> TAB <GCS generation>. Each object is read at that exact generation
# (...?generation=N), so a later overwrite of the live object cannot change what is fetched.
set -e
B=https://storage.googleapis.com/gcp-public-data-arco-era5/ar/full_37-1h-0p25deg-chunk-1.zarr-v3
OUT=${OUT:-/src}
while IFS="$(printf '\t')" read -r k gen; do
  [ -n "$k" ] || continue
  f=$OUT/$(echo "$k" | tr '/' '_')
  curl -fsS -i "$B/$k?generation=$gen" > "$f.raw"
  python3 /recipe/split.py "$f.raw" "$f" "$k" "$OUT/ledger.tsv"
done < /plan/plan.tsv
