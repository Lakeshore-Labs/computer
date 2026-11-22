#!/bin/sh
set -eu
if [ "$#" -ne 3 ]; then
    echo "Usage: sh run.sh COMPUTER PYTHON_WASI_DIRECTORY OUTPUT_DIRECTORY" >&2
    exit 2
fi
example_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
mkdir -p "$3"
land=https://raw.githubusercontent.com/nvkelso/natural-earth-vector/ca96624a56bd078437bca8184e78163e5039ad19/geojson/ne_110m_land.geojson
feed=https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_week.geojson
"$1" -mount "$3:/work" -mount "$example_dir:/recipe:ro" -python "$2/python.wasm" -python-lib "$2/lib" \
    -allow-url "$land" -allow-url "$feed" -c "
set -eo pipefail
curl -fsS '$land' > land.json
curl -fsS '$feed' > earthquakes.json
python3 /recipe/map.py
"
