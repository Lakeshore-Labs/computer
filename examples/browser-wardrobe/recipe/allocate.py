"""Reserve jackets on paper using inventory returned by the browser MCP."""
import json
from tools import wardrobe

def unpack(value):
    return json.loads(value) if isinstance(value, str) else value

source = unpack(wardrobe.read_inventory())
required = 7
available = source["stock"]["M"]
allocated = min(required, available)
plan = dict(revision=source["revision"], required=required, available=available,
            allocated=allocated, short=required-allocated, spare=available-allocated)
result = unpack(wardrobe.save_plan(**plan))
if result.get("saved") is not True:
    raise RuntimeError("The browser did not save this inventory revision")
print(json.dumps(plan, separators=(",", ":")))
