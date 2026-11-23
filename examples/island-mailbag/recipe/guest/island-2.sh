set -eo pipefail
# Island library, second visit: amend the time. Edition 1 is mounted read-only as /prev; only the card is rewritten.
echo "== edition 1, read-only =="
ls /prev/edition-1
mkdir -p /bag/edition-2
cp /prev/edition-1/four-moons.epub /bag/edition-2/four-moons.epub
python3 /recipe/card.py amend --prev /prev/edition-1/reading-card.md \
  --when "Saturday 28 November 2026, 14:30" --out /bag/edition-2/reading-card.md
echo "== what changed in the card =="
diff /prev/edition-1/reading-card.md /bag/edition-2/reading-card.md || true
echo "== the book is byte-identical to edition 1 =="
A=$(sha256sum < /prev/edition-1/four-moons.epub)
B=$(sha256sum < /bag/edition-2/four-moons.epub)
if [ "$A" = "$B" ]; then echo "identical: ${A%% *}"; else echo "DIFFERENT"; exit 1; fi
echo "== guest's own digests =="
python3 /recipe/report.py /bag/edition-2 /bag/edition-2.guest.json
