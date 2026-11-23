python3 - <<"PY"
from tools import gbif
import time
m=gbif.gbif_match_species(name="Lynx pardinus")
print("mcp_taxon_key=%s"%m["taxonKey"]); print("mcp_family=%s"%m["family"]); print("mcp_match_type=%s"%m["matchType"])
time.sleep(1.5)
n=gbif.gbif_count_occurrences(taxon_key=m["taxonKey"])
print("mcp_count=%s"%n["count"])
time.sleep(1.5)
f=gbif.gbif_occurrence_facets(taxon_key=m["taxonKey"],facet="COUNTRY",facet_limit=3)
print("mcp_facet_json=%s"%repr(f)[:400])
PY
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://api.gbif.org/v1/species/match?name=Lynx%20pardinus" > m.json
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://api.gbif.org/v1/occurrence/search?taxonKey=2435261&limit=0&occurrenceStatus=PRESENT" > p.json
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://api.gbif.org/v1/occurrence/search?taxonKey=2435261&limit=0&occurrenceStatus=ABSENT" > a.json
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://api.gbif.org/v1/occurrence/count?taxonKey=2435261" > c.txt
python3 - <<"PY"
import json
d=json.load(open('m.json'));print('rest_taxon_key=%s'%d['usageKey']);print('rest_family=%s'%d['family'])
p=json.load(open('p.json'))['count'];a=json.load(open('a.json'))['count'];t=int(open('c.txt').read())
print('rest_count=%d'%p);print('rest_absent=%d'%a);print('rest_total=%d'%t);print('present_plus_absent=%d'%(p+a))
PY
