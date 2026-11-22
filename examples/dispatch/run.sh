#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
    echo "Usage: sh run.sh COMPUTER PYTHON_WASI_DIRECTORY OUTPUT_DIRECTORY" >&2
    exit 2
fi
example_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
[ -x "$1" ] && [ -f "$2/python.wasm" ] && [ -d "$2/lib" ] || {
    echo "Provide a Computer executable and its WASI Python distribution." >&2
    exit 2
}
case "$3$example_dir" in *:*) echo "Use mount paths without colons." >&2; exit 2;; esac
mkdir -p "$(dirname "$3")"
mkdir "$3" || { echo "Choose a new directory for the packet." >&2; exit 2; }
"$1" -mount "$3:/work" -mount "$example_dir:/recipe:ro" \
    -mount "$example_dir/../ferry/input:/input:ro" \
    -python "$2/python.wasm" -python-lib "$2/lib" -c '
set -eo pipefail
python3 /recipe/packet.py
pdftoppm -png -r 96 dispatch.pdf page
magick page-1.png -thumbnail 420x600 cover.png
identify page-1.png cover.png
'
