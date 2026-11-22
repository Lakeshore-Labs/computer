python3 - <<"PY"
from tools import inat,gbif
import time
s=inat.inaturalist_search_observations(taxon_id=41975,quality_grade=["research"],license=["cc0","cc-by","cc-by-nc"],captive=False,per_page=1)
print("inat_licensed_research=%s"%s["total_results"])
time.sleep(1.5)
g=gbif.gbif_count_occurrences(taxon_key=2435261,dataset_key="50c9509d-22c7-4a22-a47d-8c48425ef4a7")
print("gbif_inat_dataset=%s"%g["count"])
time.sleep(1.5)
d=gbif.gbif_get_dataset(dataset_key="50c9509d-22c7-4a22-a47d-8c48425ef4a7")
print("dataset_title=%s"%d.get("title"))
PY
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://api.gbif.org/v1/occurrence/count?taxonKey=2435261&datasetKey=50c9509d-22c7-4a22-a47d-8c48425ef4a7" > c.txt
echo "gbif_rest_inat_dataset=$(cat c.txt)"
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://api.inaturalist.org/v1/observations?taxon_id=41975&quality_grade=research&captive=false&license=cc0,cc-by,cc-by-nc&per_page=0" > r.json
python3 -c "import json;print('inat_rest_licensed_research=%s'%json.load(open('r.json'))['total_results'])"
