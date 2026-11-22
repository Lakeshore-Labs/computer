#!/bin/sh
# Replay the island-mailbag pipeline into a NEW directory. Every path is passed explicitly; spaces in paths are fine.
#
#   sh recipe/replay.sh --out RUN_DIR --repo REPO_ROOT [options]
#   sh recipe/replay.sh --out RUN_DIR --epub FILE --computer BIN --wasi DIR [options]
#
#   --out DIR         the run directory to create; refuses to reuse one that exists           (required)
#   --repo DIR        a Lakeshore-Labs/computer checkout; sets the three defaults below
#                       --epub     DIR/examples/moon-almanac/sample/four-moons.epub   (the original book, mounted read-only)
#                       --computer DIR/bin/computer                                   (the Computer binary)
#                       --wasi     DIR/bin/python-wasi-2                              (holds python.wasm and lib/)
#   --python FILE     Python with libipld and carbox installed (default: recipe/.venv/bin/python, else python3)
#   --home DIR        HOME for the guests; Computer keeps its compiled-Python cache (about 28 MB) there
#                       (default: OUT.home, a sibling of the run directory; delete it when you are done)
#   --published DIR   after the replay, decode the CAR files published in DIR with both decoders and require them to be
#                       byte-identical to the ones this replay just made
#   --git-repo DIR    a checkout or clone that has the book's commit; adds verify.py's byte-for-byte `git show` check
#
# Needs on PATH: node (with recipe/node/node_modules installed), sqlite3 is NOT needed on the host (the guest has its own).
# No network is used. Steps (every Computer step is a fresh guest of the binary you pass):
#   1  Computer guest      packs edition 1 from the original book on a read-only shelf and writes the reading card
#   2  Node (Store A)      blockstore-fs + multiformats + @ipld/dag-cbor + @ipld/car -> boat/edition-1.car
#   3  Python (Store B)    carbox + libipld + hashlib verify and store the CAR; B lists what it holds; B restores edition 1
#   4  Computer guest      amends the time (edition 1 mounted read-only), writes edition 2
#   5  Node (Store A)      reads B's have-list, ships only the blocks B lacks -> boat/edition-2.delta.car
#   6  negative            a delta with one flipped byte is refused; Store B is unchanged
#   7  Python (Store B)    verifies and stores the real delta; restores edition 2 (the book comes from B's own copy)
#   8  negative            the delta alone cannot restore the edition
#   9  decoders            both CARs read by @ipld/car (JS) and by carbox+libipld (Python); libipld.decode_car probed
#  10  Computer guest      a NEW read-only guest consumes both restored editions and Store B
#  11  Computer guests     four refused writes, one invocation each
#  12  verify.py           independent assertions -> checks.json
#  13  published CARs      (with --published) decoded by both readers and compared byte for byte
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
die() { echo "replay.sh: $*" >&2; exit 2; }
abspath() { case $1 in /*) printf '%s' "$1" ;; *) printf '%s/%s' "$PWD" "$1" ;; esac; }

OUT= REPO= EPUB= COMPUTER= WASI= PYBIN= HOMEDIR= PUBLISHED= GITREPO=
while [ $# -gt 0 ]; do
  [ $# -ge 2 ] || die "option $1 needs a value"
  case $1 in
    --out) OUT=$2 ;; --repo) REPO=$2 ;; --epub) EPUB=$2 ;; --computer) COMPUTER=$2 ;; --wasi) WASI=$2 ;;
    --python) PYBIN=$2 ;; --home) HOMEDIR=$2 ;; --published) PUBLISHED=$2 ;; --git-repo) GITREPO=$2 ;;
    *) die "unknown option $1" ;;
  esac
  shift 2
done
[ -n "$OUT" ] || die "usage: replay.sh --out RUN_DIR --repo REPO_ROOT   (see the top of this file)"
if [ -n "$REPO" ]; then
  [ -n "$EPUB" ] || EPUB=$REPO/examples/moon-almanac/sample/four-moons.epub
  [ -n "$COMPUTER" ] || COMPUTER=$REPO/bin/computer
  [ -n "$WASI" ] || WASI=$REPO/bin/python-wasi-2
fi
[ -n "$EPUB" ] && [ -n "$COMPUTER" ] && [ -n "$WASI" ] || die "give --repo, or all of --epub --computer --wasi"
if [ -z "$PYBIN" ]; then
  if [ -x "$HERE/.venv/bin/python" ]; then PYBIN=$HERE/.venv/bin/python; else PYBIN=$(command -v python3) || die "no python3"; fi
fi
OUT=$(abspath "$OUT"); EPUB=$(abspath "$EPUB"); COMPUTER=$(abspath "$COMPUTER"); WASI=$(abspath "$WASI"); PYBIN=$(abspath "$PYBIN")
[ -n "$HOMEDIR" ] || HOMEDIR=$OUT.home
HOMEDIR=$(abspath "$HOMEDIR")
[ -n "$PUBLISHED" ] && PUBLISHED=$(abspath "$PUBLISHED")
[ -n "$GITREPO" ] && GITREPO=$(abspath "$GITREPO")

[ -e "$OUT" ] && die "refusing to reuse $OUT"
for f in "$COMPUTER" "$WASI/python.wasm" "$PYBIN" "$EPUB" "$HERE/node/node_modules/@ipld/car/package.json"; do
  [ -e "$f" ] || die "missing $f   (node deps: cd recipe/node && npm ci --ignore-scripts)"
done
[ "$(basename "$EPUB")" = four-moons.epub ] || die "the EPUB must be named four-moons.epub"
"$PYBIN" -c 'import libipld, carbox' 2>/dev/null || die "$PYBIN cannot import libipld and carbox   (pip install -r recipe/requirements.txt)"
command -v node >/dev/null || die "node is not on PATH"
SHELF=$(dirname "$EPUB")

rx() { "$PYBIN" "$HERE/py/receive.py" "$@"; }
send() { node "$HERE/node/src/send.mjs" "$@"; }
jsdec() { node "$HERE/node/src/jsdecode.mjs" "$@"; }

run=$OUT
mkdir -p "$run/island" "$run/boat" "$run/measure" "$run/store-b" "$run/restored" "$run/guest-b/out" \
         "$run/reports" "$run/transcripts" "$run/negative" "$HOMEDIR"
chmod 755 "$run" "$run/island" "$run/restored" "$run/store-b" "$run/guest-b" "$run/guest-b/out"

# Strip this machine's paths from anything written to a transcript (paths escaped for sed, so spaces and symbols are safe).
esc() { printf '%s' "$1" | sed 's/[][\.*^$#&]/\\&/g'; }
norm() {
  sed -e "s#$(esc "$run")#<run>#g" -e "s#$(esc "$HERE")#<recipe>#g" -e "s#$(esc "$COMPUTER")#bin/computer#g" \
      -e "s#$(esc "$WASI")#<python-wasi>#g" -e "s#$(esc "$SHELF")#<shelf>#g" -e "s#$(esc "$HOMEDIR")#<home>#g"
}
step() { echo; echo "== $*"; }

# capture TRANSCRIPT CMD...: run CMD, append its output and "[exit N]" to TRANSCRIPT, show it; the exit code is left in $rc.
capture() {
  ctr=$1; shift
  rc=0
  "$@" > "$run/.cap" 2>&1 || rc=$?
  norm < "$run/.cap" | tee -a "$ctr"
  echo "[exit $rc]" | tee -a "$ctr"
  rm -f "$run/.cap"
}
need0() { [ "$rc" = 0 ] || { echo "FAILED: exit $rc"; exit 1; }; }

# guest NAME SCRIPTFILE MOUNTARGS...: one fresh Computer guest, transcript in transcripts/NAME.txt, exit code in $rc.
guest() {
  gname=$1; gscript=$2; shift 2
  {
    echo "\$ bin/computer -python <python-wasi>/python.wasm -python-lib <python-wasi>/lib $* -c <script>" | norm
    echo "--- script ---"
    cat "$gscript"
    echo "--- output ---"
  } > "$run/transcripts/$gname.txt"
  rc=0
  (cd "$HOMEDIR" && env -i PATH=/usr/bin:/bin HOME="$HOMEDIR" TZ=UTC LANG=C \
      "$COMPUTER" -python "$WASI/python.wasm" -python-lib "$WASI/lib" "$@" -c "$(cat "$gscript")") > "$run/.guest.out" 2>&1 || rc=$?
  norm < "$run/.guest.out" >> "$run/transcripts/$gname.txt"
  echo "[exit $rc]" >> "$run/transcripts/$gname.txt"
  rm -f "$run/.guest.out"
  echo "guest $gname: exit $rc"
  echo "$gname $rc" >> "$run/reports/guest-exits.txt"
}
# probe NAME 'one-line script' MOUNTARGS...: a guest whose whole script is one line
probe() {
  pname=$1; pline=$2; shift 2
  printf '%s\n' "$pline" > "$run/.probe.sh"
  guest "$pname" "$run/.probe.sh" "$@"
  rm -f "$run/.probe.sh"
}

step "1  Computer: pack edition 1 from the original book on a read-only shelf"
guest guest-a1 "$HERE/guest/island-1.sh" -mount "$SHELF:/shelf:ro" -mount "$run/island:/bag" -mount "$HERE/guest:/recipe:ro"
need0

step "2  Store A (Node): edition 1 -> CAR"
capture "$run/transcripts/send-1.txt" send --edition 1 --bag "$run/island/edition-1" --store "$run/store-a" --boat "$run/boat" \
  --measure "$run/measure" --report "$run/reports/send-1.json"; need0
ROOT1=$(node -p "require(process.argv[1]).root" "$run/reports/send-1.json")

step "3  Store B (Python): verify, store, report holdings, restore edition 1"
capture "$run/transcripts/ingest-1.txt" rx ingest --car "$run/boat/edition-1.car" --store "$run/store-b/blocks.sqlite" --report "$run/reports/ingest-1.json"; need0
capture "$run/transcripts/have-1.txt" rx have --store "$run/store-b/blocks.sqlite" --out "$run/boat/receiver-have-after-1.txt"; need0
capture "$run/transcripts/restore-1.txt" rx restore --root "$ROOT1" --store "$run/store-b/blocks.sqlite" --out "$run/restored/edition-1"; need0

step "4  Computer: amend the time (edition 1 mounted read-only)"
guest guest-a2 "$HERE/guest/island-2.sh" -mount "$run/island:/prev:ro" -mount "$run/island:/bag" -mount "$HERE/guest:/recipe:ro"
need0

step "5  Store A (Node): edition 2 -> delta CAR, withholding what Store B already holds"
capture "$run/transcripts/send-2.txt" send --edition 2 --bag "$run/island/edition-2" --store "$run/store-a" --boat "$run/boat" \
  --measure "$run/measure" --report "$run/reports/send-2.json" --have "$run/boat/receiver-have-after-1.txt"; need0
ROOT2=$(node -p "require(process.argv[1]).root" "$run/reports/send-2.json")

step "6  negative: a delta with one flipped byte is refused and Store B is unchanged"
capture "$run/transcripts/tamper.txt" "$PYBIN" "$HERE/py/tamper.py" "$run/boat/edition-2.delta.car" "$run/negative/edition-2.delta.tampered.car" --needle "Changed: time only"; need0
capture "$run/transcripts/tamper.txt" rx ingest --car "$run/negative/edition-2.delta.tampered.car" --store "$run/store-b/blocks.sqlite" --report "$run/negative/ingest-tampered.json"
echo "$rc" > "$run/negative/ingest-tampered.exit"
rx have --store "$run/store-b/blocks.sqlite" --out "$run/negative/have-after-tamper.txt" > /dev/null
if cmp -s "$run/boat/receiver-have-after-1.txt" "$run/negative/have-after-tamper.txt"; then
  echo "Store B holds exactly the blocks it held before: $(wc -l < "$run/negative/have-after-tamper.txt" | tr -d ' ')" | tee -a "$run/transcripts/tamper.txt"
else
  echo "Store B CHANGED" | tee -a "$run/transcripts/tamper.txt"
fi
capture "$run/transcripts/tamper.txt" jsdec "$run/negative/edition-2.delta.tampered.car" "$run/negative/js-decode-tampered.json"
echo "$rc" > "$run/negative/js-decode-tampered.exit"

step "7  Store B (Python): verify and store the real delta; restore edition 2"
capture "$run/transcripts/ingest-2.txt" rx ingest --car "$run/boat/edition-2.delta.car" --store "$run/store-b/blocks.sqlite" --report "$run/reports/ingest-2.json"; need0
capture "$run/transcripts/restore-2.txt" rx restore --root "$ROOT2" --store "$run/store-b/blocks.sqlite" --out "$run/restored/edition-2"; need0
rx dump --store "$run/store-b/blocks.sqlite" --out "$run/reports/store-b-dump.json"

step "8  negative: the delta alone cannot restore edition 2 (a fresh store that never saw edition 1)"
capture "$run/transcripts/delta-only.txt" rx ingest --car "$run/boat/edition-2.delta.car" --store "$run/negative/delta-only.sqlite" --report "$run/negative/ingest-delta-only.json"; need0
capture "$run/transcripts/delta-only.txt" rx restore --root "$ROOT2" --store "$run/negative/delta-only.sqlite" --out "$run/negative/delta-only-restored/edition-2"
echo "$rc" > "$run/negative/delta-only.exit"

step "9  independent decoders on every CAR"
for c in edition-1.car edition-2.delta.car; do
  capture "$run/transcripts/decode.txt" jsdec "$run/boat/$c" "$run/reports/js-decode-$c.json"; need0
  capture "$run/transcripts/decode.txt" rx decode --car "$run/boat/$c" --report "$run/reports/py-decode-$c.json"; need0
  "$PYBIN" "$HERE/py/probe_libipld.py" "$run/boat/$c" >> "$run/reports/libipld-decode-car-probe.txt"
done
capture "$run/transcripts/decode.txt" jsdec "$run/measure/edition-2.standalone.car" "$run/reports/js-decode-edition-2.standalone.car.json"; need0

step "10  Computer: a NEW guest, /mailbag and /store read-only, consumes both editions"
guest guest-b "$HERE/guest/receiving.sh" -mount "$run/restored:/mailbag:ro" -mount "$run/store-b:/store:ro" -mount "$run/guest-b/out:/out" -mount "$HERE/guest:/recipe:ro"
need0

step "11  Computer: refused writes, one fresh guest each"
probe probe-append 'echo tampered >> /mailbag/edition-2/reading-card.md' -mount "$run/restored:/mailbag:ro" -mount "$run/store-b:/store:ro"
probe probe-remove 'rm /mailbag/edition-1/four-moons.epub' -mount "$run/restored:/mailbag:ro" -mount "$run/store-b:/store:ro"
probe probe-mkdir 'mkdir /mailbag/edition-3' -mount "$run/restored:/mailbag:ro" -mount "$run/store-b:/store:ro"
probe probe-sqlite 'sqlite3 /store/blocks.sqlite "DELETE FROM blocks"' -mount "$run/restored:/mailbag:ro" -mount "$run/store-b:/store:ro"

step "12  verify.py"
if [ -n "$GITREPO" ]; then
  "$PYBIN" "$HERE/verify.py" "$run" --epub "$EPUB" --git-repo "$GITREPO"
else
  "$PYBIN" "$HERE/verify.py" "$run" --epub "$EPUB"
fi

if [ -n "$PUBLISHED" ]; then
  step "13  the published CAR files: decoded by both readers, byte-identical to this replay's"
  for c in edition-1.car edition-2.delta.car; do
    [ -e "$PUBLISHED/$c" ] || die "missing published $PUBLISHED/$c"
    jsdec "$PUBLISHED/$c" "$run/reports/published-js-$c.json"
    rx decode --car "$PUBLISHED/$c" --report "$run/reports/published-py-$c.json"
    if cmp -s "$PUBLISHED/$c" "$run/boat/$c"; then
      echo "IDENTICAL $c ($(wc -c < "$PUBLISHED/$c" | tr -d ' ') bytes, sha256 $(shasum -a 256 < "$PUBLISHED/$c" | cut -d' ' -f1))"
    else
      echo "DIFFERENT $c: published file is not what this replay made"; exit 1
    fi
  done
fi
echo; echo "replay complete: $run"
