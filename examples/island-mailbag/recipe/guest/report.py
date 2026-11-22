"""Runs inside Computer's guest. report.py DIR OUT.json: size and SHA-256 of every file in DIR (stdlib only)."""
import hashlib, json, os, sys

d, out = sys.argv[1], sys.argv[2]
files = []
for name in sorted(os.listdir(d)):
    data = open(os.path.join(d, name), "rb").read()
    files.append({"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()})
json.dump({"dir": os.path.basename(d.rstrip("/")), "files": files}, open(out, "w"), indent=2)
open(out, "a").write("\n")
print(json.dumps(files))
