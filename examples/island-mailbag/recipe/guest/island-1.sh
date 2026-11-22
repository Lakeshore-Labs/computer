set -eo pipefail
# Island library, first visit: pack edition 1 from the original book on the read-only shelf.
ORIGINAL=db45721e5c875bc1be2b4b0870132c70d04d2e0aa6e29b48729ee771268b8ee5
echo "== the shelf is read-only =="
ls /shelf/four-moons.epub
sha256sum /shelf/four-moons.epub
test "$(sha256sum /shelf/four-moons.epub | cut -d' ' -f1)" = "$ORIGINAL"
echo "original book verified: $(stat -c %s /shelf/four-moons.epub) bytes"
echo "== pack edition 1 =="
mkdir -p /bag/edition-1
cp /shelf/four-moons.epub /bag/edition-1/four-moons.epub
python3 /recipe/card.py make --epub /shelf/four-moons.epub \
  --where "Ferry Landing Reading Room" --when "Saturday 28 November 2026, 10:00" \
  --out /bag/edition-1/reading-card.md
cat /bag/edition-1/reading-card.md
echo "== guest's own digests =="
python3 /recipe/report.py /bag/edition-1 /bag/edition-1.guest.json
