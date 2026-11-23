"""OUTSIDE Computer: independent native check with zarr + numcodecs + numpy. Args: SOURCE_DIR RESULTS_JSON OUT_JSON MIRROR_DIR"""

import sys, json, os, hashlib, re
import numpy as np, zarr, numcodecs

src, resf, outf, mir = sys.argv[1:5]
os.makedirs(mir, exist_ok=True)
names = {".zgroup": ".zgroup", ".zattrs": ".zattrs"}
for f in os.listdir(src):
    if f.endswith(".headers") or f == "ledger.tsv":
        continue
    d = f.rsplit("_", 1)
    key = f if f.startswith(".") else None
    if key is None:
        a, b = (
            f.split("_", 1)
            if not f.startswith("2m_")
            else ("2m_temperature", f[len("2m_temperature_") :])
        )
        os.makedirs(f"{mir}/{a}", exist_ok=True)
        key = f"{a}/{b}"
    dst = f"{mir}/{key}"
    if not os.path.lexists(dst):
        os.symlink(os.path.abspath(f"{src}/{f}"), dst)
r = json.load(open(resf))
checks = []


def chk(name, ok, detail=""):
    checks.append(dict(name=name, ok=bool(ok), detail=detail))
    print("PASS" if ok else "FAIL", name, detail)


g = zarr.open_group(mir, mode="r", zarr_format=2)
t2m, lat, lon = g["2m_temperature"], g["latitude"][:], g["longitude"][:]
chk(
    "observed zarr_format is 2",
    t2m.metadata.zarr_format == 2 and r["zarr_format_observed"] == 2,
    f"zarr lib read format {t2m.metadata.zarr_format}; name suffix says v3",
)
chk("shape/chunks", t2m.shape == (1323648, 721, 1440) and t2m.chunks == (1, 721, 1440))
w = r["window"]
chk(
    "latitude axis (full 721) descends 90..-90",
    lat[0] == 90 and lat[-1] == -90 and r["lat_first_last"] == [90.0, -90.0],
)
chk("longitude axis stored 0..359.75", lon[0] == 0 and lon[-1] == 359.75)
ri = np.where((lat >= w["lat_s"] - 1e-6) & (lat <= w["lat_n"] + 1e-6))[0]
ci_w = np.where(
    ((lon + 180) % 360 - 180 >= w["lon_w"] - 1e-6)
    & ((lon + 180) % 360 - 180 <= w["lon_e"] + 1e-6)
)[0]
lonw = (lon[ci_w] + 180) % 360 - 180
order = np.argsort(lonw)
ci = ci_w[order]
chk(
    "window latitude axis equals Computer",
    np.array_equal(lat[ri].astype(float), np.array(w["lat"])),
    f"{lat[ri][0]}..{lat[ri][-1]}",
)
chk(
    "window longitude axis equals Computer (wraps 0°)",
    np.array_equal(lonw[order].astype(float), np.array(w["lon"])),
    f"{lonw[order][0]}..{lonw[order][-1]}",
)
tm = g["time"]
for name, idx in (("morning", 1074222), ("afternoon", 1074231)):
    ts = np.datetime64("1900-01-01T00") + int(tm[idx]) * np.timedelta64(1, "h")
    chk(
        f"time[{idx}] is {name}",
        str(ts) == r["time_check"][name]["utc"].replace("Z", "").replace(":00:00", "")
        or str(ts)[:13] == r["time_check"][name]["utc"][:13],
        str(ts),
    )
    full = t2m[idx, :, :]
    sub = full[np.ix_(ri, ci)]
    mine = np.array(r["grid_K"][name], dtype=np.float32)
    chk(
        f"{name} window K bit-identical ({sub.size} cells)",
        np.array_equal(sub.view(np.uint32), mine.view(np.uint32)),
    )
    cexp = sub.astype(np.float64) - 273.15
    chk(
        f"{name} °C max abs error < 1e-9",
        np.abs(cexp - np.array(r["grid_C"][name])).max() < 1e-9,
        f"{np.abs(cexp - np.array(r['grid_C'][name])).max():.2e}",
    )
    fh = open(f"{src}/2m_temperature_{idx}.0.0", "rb").read()
    hd = open(f"{src}/2m_temperature_{idx}.0.0.headers").read()
    md5 = hashlib.md5(fh).hexdigest()
    import base64

    hdr = base64.b64decode(re.search(r"md5=([^\s,]+)", hd).group(1)).hex()
    chk(
        f"{name} chunk MD5 equals server X-Goog-Hash and Computer receipt",
        md5 == hdr == r["sources"][name]["md5_hex"],
        md5,
    )
    chk(
        f"{name} chunk full decode matches numcodecs Blosc",
        np.array_equal(
            np.frombuffer(numcodecs.Blosc().decode(fh), dtype="<f4")
            .reshape(721, 1440)[np.ix_(ri, ci)]
            .view(np.uint32),
            mine.view(np.uint32),
        ),
    )
    chk(
        f"{name} chunk header generation recorded",
        "X-Goog-Generation" in hd,
        re.search(r"X-Goog-Generation: (\d+)", hd).group(1),
    )
dif = np.array(r["grid_C"]["afternoon"]) - np.array(r["grid_C"]["morning"])
chk("difference grid recomputed", np.abs(dif - np.array(r["diff_C"])).max() < 1e-9)
for c in r["cities"]:
    i = int(np.argmin(np.abs(lat[ri] - c["lat"])))
    j = int(np.argmin(np.abs(lonw[order] - c["lon"])))
    chk(
        f"{c['name']} nearest cell + values",
        (i, j) == (c["row"], c["col"])
        and abs((float(t2m[1074231, ri[i], ci[j]]) - 273.15) - c["afternoon_C"]) < 1e-9,
        f"cell {lat[ri[i]]},{lonw[order][j]}",
    )
json.dump(
    dict(
        libs=dict(
            zarr=zarr.__version__, numcodecs=numcodecs.__version__, numpy=np.__version__
        ),
        checks=checks,
        passed=sum(c["ok"] for c in checks),
        total=len(checks),
    ),
    open(outf, "w"),
    indent=1,
)
print(sum(c["ok"] for c in checks), "/", len(checks))
sys.exit(0 if all(c["ok"] for c in checks) else 1)
