set -eo pipefail
# A new guest. /mailbag (the restored editions) and /store (Store B) are read-only; only /out can be written.
echo "== checksum lists derived by the receiving store from the CIDs, checked here =="
for n in 1 2; do
  (cd /mailbag/edition-$n && sha256sum -c ../edition-$n.SHA256SUMS)
done
echo "== Store B, opened read-only from inside the guest =="
sqlite3 -readonly -header /store/blocks.sqlite "SELECT count(*) AS blocks, sum(size) AS payload_bytes FROM blocks"
sqlite3 -readonly -header /store/blocks.sqlite "SELECT car, count(*) AS blocks, sum(duplicate) AS already_held FROM arrivals GROUP BY car ORDER BY min(seq)"
sqlite3 -readonly -json /store/blocks.sqlite "SELECT a.seq, a.car, a.cid, a.duplicate, b.codec, b.size FROM arrivals a JOIN blocks b USING(cid) ORDER BY a.seq" > /out/store-b-arrivals.json
echo "== read both editions and decide =="
python3 /recipe/consume.py
