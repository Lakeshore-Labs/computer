python3 - <<"PY"
from tools import lv
import json
s=lv.search_stops(query="Площа Ринок",limit=3)["data"]["stops"][0]
print("stop_id=%s"%s["id"]); print("eng=%s"%s["eng_name"]); print("lat=%s"%s["lat"]); print("lng=%s"%s["lng"]); print("routes=%s"%",".join(s["routes"]))
r=lv.get_route_static(route_name=s["routes"][0])
j=json.dumps(r,ensure_ascii=False)
print("route_mentions_stop_name=%d"%("Площа Ринок" in j))
print("route_json_bytes=%d"%len(j))
PY
curl -s -A "computer-gallery-check/1.0" "https://www.wikidata.org/w/api.php?action=wbgetclaims&entity=Q36036&property=P625&format=json" > wd.json
python3 - <<"PY"
import json,math
v=json.load(open("wd.json"))["claims"]["P625"][0]["mainsnak"]["datavalue"]["value"]
print("wd_lat=%s"%v["latitude"]); print("wd_lng=%s"%v["longitude"])
a=49.84146;b=24.03227
p=math.pi/180
x=math.sin((v["latitude"]-a)*p/2)**2+math.cos(a*p)*math.cos(v["latitude"]*p)*math.sin((v["longitude"]-b)*p/2)**2
d=2*6371*math.asin(math.sqrt(x)); print("distance_km=%.2f"%d); print("within_2km=%d"%(d<2))
PY
