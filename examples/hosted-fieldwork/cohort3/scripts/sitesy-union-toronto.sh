python3 - <<"PY"
from tools import tr
import time,json
r=tr.search_stops(query="Union Station",agency="ttc",limit=3)
r=json.loads(r) if isinstance(r,str) else r
s=r["stops"][0]
print("agency=%s"%s["agency"]); print("name=%s"%s["name"]); print("lat=%s"%s["latitude"]); print("lng=%s"%s["longitude"]); print("total_matches=%s"%r["total_matches"])
time.sleep(1.5)
a=tr.service_alerts()
a=json.loads(a) if isinstance(a,str) else a
print("alerts_keys=%s"%",".join(sorted(a.keys())))
PY
curl -s -A "computer-gallery-check/1.0" "https://www.wikidata.org/w/api.php?action=wbsearchentities&search=Union%20Station&language=en&format=json&limit=10" > s.json
python3 - <<"PY"
import json
ids=[x["id"] for x in json.load(open("s.json"))["search"] if "Toronto" in (x.get("description") or "")]
print("wd_toronto_union=%s"%ids[:1])
open("id.txt","w").write(ids[0] if ids else "")
PY
curl -s -A "computer-gallery-check/1.0" "https://www.wikidata.org/w/api.php?action=wbgetclaims&entity=$(cat id.txt)&property=P625&format=json" > wd.json
python3 - <<"PY"
import json,math
v=json.load(open("wd.json"))["claims"]["P625"][0]["mainsnak"]["datavalue"]["value"]
a=43.645523;b=-79.379723;p=math.pi/180
x=math.sin((v["latitude"]-a)*p/2)**2+math.cos(a*p)*math.cos(v["latitude"]*p)*math.sin((v["longitude"]-b)*p/2)**2
d=2*6371000*math.asin(math.sqrt(x)); print("distance_m=%.0f"%d); print("within_500m=%d"%(d<500))
PY
