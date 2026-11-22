python3 - <<"PY"
from tools import met
import json
r=met.met_search_collections(q="Wheat Field with Cypresses",match_field="title",limit=5)
print("mcp_ids=%s"%json.dumps(r.get("objectIDs") or r)[:200])
o=met.met_get_object(object_i_ds=[436535])
j=json.dumps(o)
print("mcp_has_436535=%d"%("436535" in j)); print("mcp_title_hit=%d"%("Wheat Field with Cypresses" in j)); print("mcp_artist_hit=%d"%("Vincent van Gogh" in j))
PY
curl -s -A "computer-gallery-check/1.0" "https://collectionapi.metmuseum.org/public/collection/v1/objects/436535" > m.json
python3 - <<"PY"
import json
d=json.load(open("m.json"))
print("rest_title=%s"%d["title"]); print("rest_artist=%s"%d["artistDisplayName"]); print("rest_accession=%s"%d["accessionNumber"])
PY
