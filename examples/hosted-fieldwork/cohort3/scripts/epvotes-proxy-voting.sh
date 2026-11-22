python3 - <<"PY"
from tools import ep
import json
r=ep.list_votes(day="2026-11-12")
it=[i for i in r["items"] if i["finalVote"]["id"]=="199979"][0]
f=it["finalVote"]
print("mcp_for=%s"%f["for"]); print("mcp_against=%s"%f["against"]); print("mcp_abstention=%s"%f["abstention"]); print("mcp_result=%s"%f["result"]); print("mcp_ref=%s"%it["reference"])
PY
curl -s -A "computer-gallery-check/1.0" "https://howtheyvote.eu/api/votes/199979" > v.json
python3 - <<"PY"
import json
d=json.load(open("v.json")); t=d["stats"]["total"]
print("hv_for=%s"%t["FOR"]); print("hv_against=%s"%t["AGAINST"]); print("hv_abstention=%s"%t["ABSTENTION"]); print("hv_result=%s"%d["result"]); print("hv_ref=%s"%d["reference"])
g=d["stats"]["by_group"]
sf=sum(x["stats"]["FOR"] for x in g); print("group_sum_for=%d"%sf); print("country_sum_for=%d"%sum(x["stats"]["FOR"] for x in d["stats"]["by_country"]))
print("cast=%d"%(t["FOR"]+t["AGAINST"]+t["ABSTENTION"]))
PY
