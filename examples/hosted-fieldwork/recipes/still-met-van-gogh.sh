curl -s -A x https://collectionapi.metmuseum.org/public/collection/v1/objects/436535 > met.json
python3 - <<"PY"
from tools import st
import json
r=st.still_resolve_artwork(source_url="https://www.metmuseum.org/art/collection/search/436535")
r=json.loads(r) if isinstance(r,str) else r
rec=r["record"]; met=json.load(open("/work/met.json"))
print("MCP  :",rec["title"],"|",rec["artist"],"|",rec["date"],"|",rec["medium"],"|",rec["collection"])
print("Met  :",met["title"],"|",met["artistDisplayName"],"|",met["objectDate"],"|",met["medium"],"| accession",met["accessionNumber"])
ok=[rec["title"]==met["title"],rec["artist"]==met["artistDisplayName"],rec["date"]==met["objectDate"],rec["medium"]==met["medium"],rec["requiredCredit"]==met["creditLine"],r["provenance"]["rawRights"]["isPublicDomain"]==met["isPublicDomain"]]
print("fields matching Met API:",sum(ok),"of",len(ok))
print("status:",r["status"])
PY
