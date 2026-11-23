import sys, os

raw, body, key, ledger = sys.argv[1:5]
b = open(raw, "rb").read()
i = b.index(b"\r\n\r\n")
head, data = b[:i].decode("latin1"), b[i + 4 :]
open(body, "wb").write(data)
open(body + ".headers", "w").write(head + "\n")
os.remove(raw)
open(ledger, "a").write("%s\t%d\t%d\n" % (key, len(data), len(b)))
print("got", key, len(data))
