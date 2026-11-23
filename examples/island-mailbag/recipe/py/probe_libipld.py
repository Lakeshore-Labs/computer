"""probe_libipld.py CAR: what libipld.decode_car says about this CAR (it only accepts DAG-CBOR blocks)."""
import importlib.metadata as m, sys, libipld
data = open(sys.argv[1], "rb").read()
try:
    header, blocks = libipld.decode_car(data)
    print(f"libipld {m.version('libipld')} decode_car({sys.argv[1].split('/')[-1]}): decoded {len(blocks)} blocks")
except Exception as e:
    print(f"libipld {m.version('libipld')} decode_car({sys.argv[1].split('/')[-1]}): {type(e).__name__}: {e}")
