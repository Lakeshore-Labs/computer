"""Runs INSIDE Computer's WASI Python. Pulls the two aggregated tables through their MCP modules,
joins on hall/service/meal, and writes the balanced transfers. No files from the host fixture are read."""
import csv, json
from tools import orders_pg, loads_duck

ordered_rows = json.loads(orders_pg.query(sql="SELECT hall, service, meal, ordered FROM order_totals ORDER BY 1,2,3"))
duck = json.loads(loads_duck.execute_query(sql="SELECT hall, service, meal, loaded FROM loaded_totals ORDER BY 1,2,3"))
assert duck["success"], duck
loaded_rows = [dict(zip(duck["columns"], r)) for r in duck["rows"]]

key = lambda r: (r["hall"], r["service"], r["meal"])
ordered = {key(r): int(r["ordered"]) for r in ordered_rows}
loaded = {key(r): int(r["loaded"]) for r in loaded_rows}
assert ordered.keys() == loaded.keys(), "hall/service/meal keys differ between the two sources"

lines = [{"hall": h, "service": s, "meal": m, "ordered": ordered[(h, s, m)], "loaded": loaded[(h, s, m)],
          "delta": loaded[(h, s, m)] - ordered[(h, s, m)]} for (h, s, m) in sorted(ordered)]
transfers = []
for (s, m) in sorted({(l["service"], l["meal"]) for l in lines}):
    group = [l for l in lines if (l["service"], l["meal"]) == (s, m)]
    short = sorted([l for l in group if l["delta"] < 0], key=lambda l: (l["delta"], l["hall"]))
    spare = sorted([l for l in group if l["delta"] > 0], key=lambda l: (-l["delta"], l["hall"]))
    assert sum(l["delta"] for l in group) == 0, f"{s}/{m} cannot balance by transfers alone"
    left = {l["hall"]: l["delta"] for l in spare}
    for need in short:
        want = -need["delta"]
        for donor in spare:
            give = min(want, left[donor["hall"]])
            if give:
                transfers.append({"service": s, "meal": m, "from_hall": donor["hall"], "to_hall": need["hall"], "units": give})
                left[donor["hall"]] -= give; want -= give
        assert want == 0

final = {}
for l in lines:
    out = sum(t["units"] for t in transfers if (t["service"], t["meal"], t["from_hall"]) == (l["service"], l["meal"], l["hall"]))
    inn = sum(t["units"] for t in transfers if (t["service"], t["meal"], t["to_hall"]) == (l["service"], l["meal"], l["hall"]))
    l["transfer_out"], l["transfer_in"] = out, inn
    l["final"] = l["loaded"] - out + inn
    l["short_after"] = max(0, l["ordered"] - l["final"])

with open("/work/transfers.csv", "w", newline="") as f:
    w = csv.DictWriter(f, ["service", "meal", "from_hall", "to_hall", "units"], lineterminator="\n"); w.writeheader(); w.writerows(transfers)
with open("/work/balance.json", "w") as f:
    json.dump({"lines": lines, "transfers": transfers}, f, indent=1)

print("source ordered (PostgreSQL MCP):", sum(ordered.values()), " source loaded (DuckDB/Parquet MCP):", sum(loaded.values()))
print("original shortages:", [(l["hall"], l["meal"], -l["delta"]) for l in lines if l["delta"] < 0])
for t in transfers: print("transfer", t["meal"], t["from_hall"], "->", t["to_hall"], t["units"])
print("units still short:", sum(l["short_after"] for l in lines))
for l in lines: print(f'final {l["hall"]:8} {l["meal"]:17} ordered={l["ordered"]:3} loaded={l["loaded"]:3} final={l["final"]:3}')
