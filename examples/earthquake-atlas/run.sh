#!/bin/sh
set -eu
mode=live
if [ "${1:-}" = --captured ]; then
    mode=captured
    shift
fi
if [ "$#" -ne 3 ]; then
    echo "Usage: sh run.sh [--captured] COMPUTER PYTHON_WASI_DIRECTORY NEW_OUTPUT_DIRECTORY" >&2
    exit 2
fi
example_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
[ -x "$1" ] && [ -f "$2/python.wasm" ] && [ -d "$2/lib" ] || {
    echo "Provide a Computer executable and its WASI Python distribution." >&2
    exit 2
}
case "$3$example_dir" in *:*) echo "Use mount paths without colons." >&2; exit 2;; esac
mkdir -p "$(dirname "$3")"
mkdir "$3" || { echo "Choose a new directory for the map." >&2; exit 2; }
feed=https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_week.geojson
land=https://raw.githubusercontent.com/nvkelso/natural-earth-vector/ca96624a56bd078437bca8184e78163e5039ad19/geojson/ne_110m_land.geojson
set -- "$1" -mount "$3:/work" -mount "$example_dir:/recipe:ro" -python "$2/python.wasm" -python-lib "$2/lib"
if [ "$mode" = captured ]; then
    acquire='cp /recipe/media/snapshot.geojson earthquakes.json
cp /recipe/media/ne_110m_land.geojson land.json'
else
    set -- "$@" -allow-url "$feed" -allow-url "$land"
    acquire="curl -fsS '$feed' > earthquakes.json
curl -fsS '$land' > land.json"
fi
"$@" -c "set -eo pipefail
$acquire
echo '9e0729ee253ca7d7a5c4ae9395fb1902264c5377c52e224d13dd85010e2835d9  land.json' | sha256sum -c -
python3 /recipe/map.py"
