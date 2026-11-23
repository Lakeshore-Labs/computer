python3 - <<"PY"
from tools import an
r=an.search_artworks(q="Sunday on La Grande Jatte",artist="Georges Seurat",limit=2)["works"][0]
print("mcp_id=%s"%r["id"]); print("mcp_title=%s"%r["title"]); print("mcp_museum=%s"%r["museum"]); print("mcp_license=%s"%r["license"]); print("mcp_year=%s"%r["year"])
PY
curl -s -A "computer-gallery-check/1.0" "https://www.wikidata.org/w/api.php?action=wbsearchentities&search=A%20Sunday%20Afternoon%20on%20the%20Island%20of%20La%20Grande%20Jatte&language=en&format=json&limit=3" > s.json
python3 -c "import json;x=json.load(open('s.json'))['search'][0];print('wd_item=%s'%x['id']);print('wd_label=%s'%x['label']);open('id.txt','w').write(x['id'])"
curl -s -A "computer-gallery-check/1.0" "https://www.wikidata.org/w/api.php?action=wbgetclaims&entity=$(cat id.txt)&format=json" > wd.json
python3 - <<"PY"
import json
c=json.load(open("wd.json"))["claims"]
def ids(p): return [x["mainsnak"]["datavalue"]["value"]["id"] for x in c.get(p,[]) if "datavalue" in x["mainsnak"]]
print("wd_creator_is_seurat=%d"%("Q34013" in ids("P170")))
print("wd_collection_is_aic=%d"%("Q239303" in ids("P195")))
PY
