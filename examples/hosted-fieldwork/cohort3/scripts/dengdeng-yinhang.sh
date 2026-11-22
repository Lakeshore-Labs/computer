python3 - <<"PY"
from tools import dd
import json,unicodedata
r=dd.look_up_word(word="银行")
py=unicodedata.normalize("NFC",r["pinyin"]) if "pinyin" in r else ""
print("found=%s"%r["found"]); print("mcp_pinyin=%s"%py.replace(" ","")); print("hsk3=%s"%r["hsk"]["hsk3_band"])
ex=r["examples"][0]; print("example=%s"%ex["chinese"]); print("example_en=%s"%ex["english"])
open("/work/ex.txt","w").write(ex["chinese"])
PY
curl -s -A "computer-gallery-check/1.0" "https://en.wiktionary.org/w/api.php?action=parse&page=%E9%8A%80%E8%A1%8C&prop=text&format=json" > w.json
sleep 1.5
curl -s -A "computer-gallery-check/1.0" "https://tatoeba.org/en/api_v0/search?from=cmn&to=eng&query=%E9%93%B6%E8%A1%8C" > t.json
python3 - <<"PY"
import json,re,unicodedata
t=json.load(open("w.json"))["parse"]["text"]["*"]
t=unicodedata.normalize("NFC",re.sub(r"<[^>]+>","",t))
mp=[l for l in open("/dev/stdin")] if False else []
print("wiktionary_has_yinhang=%d"%("yínháng" in t))
d=json.load(open("t.json"))
print("tatoeba_sentences_with_word=%d"%d["paging"]["Sentences"]["count"])
print("tatoeba_has_translated=%d"%any(r.get("translations") for r in d["results"]))
ex=open("ex.txt").read()
print("note_example_verbatim_in_tatoeba_search=%s"%("unchecked"))
PY
