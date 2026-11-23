"""tamper.py IN OUT --needle TEXT: copy a CAR and flip the lowest bit of the first byte of TEXT. For the negative test."""
import argparse
p = argparse.ArgumentParser()
p.add_argument("src"); p.add_argument("dst"); p.add_argument("--needle", required=True)
a = p.parse_args()
data = bytearray(open(a.src, "rb").read())
i = data.find(a.needle.encode())
if i < 0:
    raise SystemExit("needle not found")
data[i] ^= 1
open(a.dst, "wb").write(data)
print(f"flipped bit 0 of byte {i} ({a.needle[0]!r} -> {chr(data[i])!r}); {a.src.split('/')[-1]} -> {a.dst.split('/')[-1]}")
