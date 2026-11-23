python3 - <<"PY"
from tools import inat
import json,time
c=inat.inaturalist_resolve_name(q="Lynx pardinus",type="taxon",limit=1)["candidates"][0]
print("taxon_id=%s"%c["id"]); print("common=%s"%c["common_name"])
time.sleep(1.5)
t=inat.inaturalist_get_taxon(taxon_id=c["id"],sections=["conservation"])
print("mcp_global_status=%s"%t["conservation"]["global_status"]["status"])
time.sleep(1.5)
o=inat.inaturalist_search_observations(taxon_id=c["id"],quality_grade=["research"],per_page=1)
print("mcp_research_obs=%s"%o["total_results"])
PY
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://api.inaturalist.org/v1/observations?taxon_id=41975&quality_grade=research&captive=false&per_page=0" > rest.json
python3 -c "import json;print('rest_research_obs=%s'%json.load(open('rest.json'))['total_results'])"
curl -s -A "computer-gallery-check/1.0" "https://www.wikidata.org/w/api.php?action=wbgetclaims&entity=Q129727&property=P141&format=json" > wd.json
python3 - <<"PY"
import json
d=json.load(open("wd.json"))
q=d["claims"]["P141"][0]["mainsnak"]["datavalue"]["value"]["id"]
print("wikidata_iucn_item=%s"%q); print("wikidata_is_vulnerable=%d"%(q=="Q278113"))
PY
