curl -s -A x "https://www.wikidata.org/w/api.php?action=wbgetclaims&entity=Q4310527&property=P195&format=json" > wd_coll.json
curl -s -A x "https://www.wikidata.org/w/api.php?action=wbgetentities&ids=Q194626&props=labels&languages=en&format=json" > wd_label.json
curl -s -A x "https://www.wikidata.org/w/api.php?action=wbgetclaims&entity=Q4310527&property=P571&format=json" > wd_year.json
python3 - <<"PY"
from tools import art,ch
import json,re,datetime
J=lambda x: json.loads(x) if isinstance(x,str) else x
print("STEP 1  wikioo.search_artworks -> artwork")
a=J(art.search_artworks(query="Dead Christ in the Tomb",artist="Hans Holbein the Younger",limit=1))["results"][0]
print("  ",a["title"],"|",a["artist_name"],"|",a["year"])
print("STEP 2  wikioo.get_detail_artwork -> museum")
m=J(art.get_detail_artwork(pk=a["pk"]))["museum"]
print("  ",m["name"],"|",m["city"],",",m["country"])
mm=J(art.search_museums(query="Kunstmuseum Basel",city=m["city"],limit=1))["results"][0]
print("  museum search in same city:",mm["name"],"| pk",mm["pk"])
print("STEP 3  transport.opendata.ch.connections -> Zürich HB to Basel, Kunstmuseum")
t=ch.connections(origin="Zürich HB",destination="Basel, Kunstmuseum")
first=[l for l in t.splitlines() if re.match(r"1\. ",l)][0]
print("  ",first[:150])
mt=re.search(r"(\d\d):(\d\d) Zürich HB(?:, platform \w+)?(?:, \+(\d+) min)? .*?(\d\d):(\d\d) Basel, Kunstmuseum . (?:(\d+) h )?(\d+) min",first)
dep=int(mt[1])*60+int(mt[2])+int(mt[3] or 0); arr=int(mt[4])*60+int(mt[5]); dur=(arr-dep)%1440
said=int(mt[6] or 0)*60+int(mt[7])
print("STEP 4  independent checks")
coll=json.load(open("/work/wd_coll.json"))["claims"]["P195"][0]["mainsnak"]["datavalue"]["value"]["id"]
label=json.load(open("/work/wd_label.json"))["entities"][coll]["labels"]["en"]["value"]
yr=int(json.load(open("/work/wd_year.json"))["claims"]["P571"][0]["mainsnak"]["datavalue"]["value"]["time"][1:5])
print("   Wikidata: collection of Q4310527 =",coll,"=",label,"| inception",yr)
print("   city is Basel in both sources:",m["city"]=="Basel" and "Basel" in label)
print("   year within 2 of Wikidata:",abs(a["year"]-yr)<=2)
print("   arrival minus delayed departure =",dur,"min equals stated",said,"min:",dur==said)
PY
