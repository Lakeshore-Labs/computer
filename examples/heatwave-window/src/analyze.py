"""Runs under Computer's WASI CPython. Args: SRC_DIR OUT_DIR. Pure stdlib: Blosc1/LZ4 block decoder, window select, K->C, HTML."""

import sys, json, struct, hashlib
from datetime import datetime, timedelta

SRC, OUT = sys.argv[1], sys.argv[2]
PINS = {
    o["key"].replace("/", "_"): o
    for o in json.load(open("/recipe/pins.json"))["objects"]
}


def rd(n):
    b = open(f"{SRC}/{n}", "rb").read()
    if n in PINS:  # refuse any object that is not exactly the pinned bytes
        assert hashlib.sha256(b).hexdigest() == PINS[n]["sha256"], (
            "sha256 differs from pins.json: " + n
        )
    return b


def js(n):
    return json.loads(rd(n))


def lz4_block(src, off, n, outlen):
    out = bytearray()
    end = off + n
    while off < end:
        t = src[off]
        off += 1
        ll = t >> 4
        if ll == 15:
            while True:
                b = src[off]
                off += 1
                ll += b
                if b != 255:
                    break
        out += src[off : off + ll]
        off += ll
        if off >= end:
            break
        mo = src[off] | (src[off + 1] << 8)
        off += 2
        ml = t & 15
        if ml == 15:
            while True:
                b = src[off]
                off += 1
                ml += b
                if b != 255:
                    break
        ml += 4
        st = len(out) - mo
        if mo >= ml:
            out += out[st : st + ml]
        else:
            for i in range(ml):
                out.append(out[st + i])
    assert len(out) == outlen, (len(out), outlen)
    return bytes(out)


def blosc_header(buf):
    ver, verlz, flags, ts, nbytes, bs, cbytes = struct.unpack("<BBBBIII", buf[:16])
    return dict(
        ver=ver,
        verlz=verlz,
        flags=flags,
        typesize=ts,
        nbytes=nbytes,
        blocksize=bs,
        cbytes=cbytes,
    )


def blosc_block(buf, h, bi):
    ts, bs, nb = h["typesize"], h["blocksize"], h["nbytes"]
    nblocks = (nb + bs - 1) // bs
    start = struct.unpack_from("<I", buf, 16 + 4 * bi)[0]
    bsize = bs if bi < nblocks - 1 else nb - bs * (nblocks - 1)
    leftover = bsize != bs
    if h["flags"] & 2:  # memcpy
        return buf[16 + bi * bs : 16 + bi * bs + bsize]
    split = (not (h["flags"] & 0x10)) and ts <= 16 and bs // ts >= 128 and not leftover
    ns = ts if split else 1
    sl = bsize // ns
    p = start
    parts = []
    for _ in range(ns):
        cs = struct.unpack_from("<i", buf, p)[0]
        p += 4
        parts.append(buf[p : p + cs] if cs == sl else lz4_block(buf, p, cs, sl))
        p += cs
    raw = b"".join(parts)
    if h["flags"] & 1 and ts > 1:  # byte shuffle
        ne = bsize // ts
        o = bytearray(bsize)
        for j in range(ts):
            o[j : ne * ts : ts] = raw[j * ne : (j + 1) * ne]
        o[ne * ts :] = raw[ne * ts :]
        raw = bytes(o)
    return raw


def blosc_range(buf, a, b):
    """bytes [a,b) of the decompressed chunk, decoding only blocks that overlap; returns (data, blocks_used, nblocks)"""
    h = blosc_header(buf)
    bs = h["blocksize"]
    b0, b1 = a // bs, (b - 1) // bs
    data = b"".join(blosc_block(buf, h, i) for i in range(b0, b1 + 1))
    s = a - b0 * bs
    return data[s : s + (b - a)], b1 - b0 + 1, (h["nbytes"] + bs - 1) // bs


def blosc_all(buf):
    h = blosc_header(buf)
    n = (h["nbytes"] + h["blocksize"] - 1) // h["blocksize"]
    return b"".join(blosc_block(buf, h, i) for i in range(n))


# ---- metadata
za = js("2m_temperature_.zarray")
zat = js("2m_temperature_.zattrs")
root = js(".zattrs")
zg = js(".zgroup")
assert (
    za["dtype"] == "<f4"
    and za["compressor"]["id"] == "blosc"
    and za["compressor"]["cname"] == "lz4"
)
assert za["chunks"] == [1, 721, 1440] and zat["units"] == "K"
tz = js("time_.zarray")
tat = js("time_.zattrs")
assert tat["units"].startswith("hours since 1900-01-01") and tz["chunks"] == [67706]
# ---- coordinates (whole coordinate chunks)
lat = struct.unpack("<721f", blosc_all(rd("latitude_0")))
lon = struct.unpack("<1440f", blosc_all(rd("longitude_0")))
tch = 15
tvals = struct.unpack("<67706q", blosc_all(rd("time_15"))) if False else None
traw = blosc_all(rd("time_15"))
tvals = struct.unpack("<%dq" % (len(traw) // 8), traw)
T0 = datetime(1900, 1, 1)
WANT = [("morning", datetime(2022, 7, 19, 6)), ("afternoon", datetime(2022, 7, 19, 15))]
chunkfile = {
    "morning": ("2m_temperature_1074222.0.0", 1074222),
    "afternoon": ("2m_temperature_1074231.0.0", 1074231),
}
timecheck = {}
for name, dt in WANT:
    idx = chunkfile[name][1]
    v = tvals[idx - tch * 67706]
    got = T0 + timedelta(hours=v)
    assert got == dt, (got, dt)
    timecheck[name] = dict(index=idx, hours_since_1900=v, utc=got.isoformat() + "Z")
# ---- window (lon stored 0..359.75, so Britain's west is 354.5+)
LAT_N, LAT_S, LON_W, LON_E = 52.5, 50.0, -5.5, 2.0
rows = [i for i in range(721) if LAT_S - 1e-6 <= lat[i] <= LAT_N + 1e-6]
cols = [
    i
    for i in range(1440)
    if ((lon[i] + 180) % 360 - 180) >= LON_W - 1e-6
    and ((lon[i] + 180) % 360 - 180) <= LON_E + 1e-6
]
cols.sort(key=lambda i: (lon[i] + 180) % 360 - 180)
lon_w = [(lon[i] + 180) % 360 - 180 for i in cols]
lat_w = [lat[i] for i in rows]
r0, r1 = rows[0], rows[-1] + 1
res = {}
src_info = {}
for name, _ in WANT:
    fn, idx = chunkfile[name]
    buf = rd(fn)
    a, b = r0 * 1440 * 4, r1 * 1440 * 4
    data, used, nb = blosc_range(buf, a, b)
    allrows = struct.unpack("<%df" % (len(data) // 4), data)
    grid = [[allrows[(r - r0) * 1440 + c] for c in cols] for r in rows]
    res[name] = grid
    h = blosc_header(buf)
    src_info[name] = dict(
        key=f"2m_temperature/{idx}.0.0",
        compressed_bytes=len(buf),
        md5_hex=hashlib.md5(buf).hexdigest(),
        sha256=hashlib.sha256(buf).hexdigest(),
        blosc=h,
        blocks_decoded=used,
        blocks_total=nb,
        rows=[r0, r1 - 1],
    )
C = lambda k: k - 273.15
gc = {n: [[C(v) for v in row] for row in g] for n, g in res.items()}
diff = [
    [gc["afternoon"][i][j] - gc["morning"][i][j] for j in range(len(cols))]
    for i in range(len(rows))
]
CITIES = [
    ("London", 51.51, -0.13),
    ("Bristol", 51.45, -2.59),
    ("Southampton", 50.91, -1.40),
]


def nearest(la, lo):
    i = min(range(len(lat_w)), key=lambda k: abs(lat_w[k] - la))
    j = min(range(len(lon_w)), key=lambda k: abs(lon_w[k] - lo))
    return i, j


cities = []
for nm, la, lo in CITIES:
    i, j = nearest(la, lo)
    cities.append(
        dict(
            name=nm,
            lat=la,
            lon=lo,
            row=i,
            col=j,
            cell_lat=lat_w[i],
            cell_lon=lon_w[j],
            morning_K=res["morning"][i][j],
            afternoon_K=res["afternoon"][i][j],
            morning_C=gc["morning"][i][j],
            afternoon_C=gc["afternoon"][i][j],
            diff_C=diff[i][j],
        )
    )
fl = lambda g: [v for r in g for v in r]
stats = {
    n: dict(min=min(fl(g)), max=max(fl(g)), mean=sum(fl(g)) / len(fl(g)))
    for n, g in {
        "morning_C": gc["morning"],
        "afternoon_C": gc["afternoon"],
        "diff_C": diff,
    }.items()
}
results = dict(
    zarr_format_observed=zg["zarr_format"],
    array_zarr_format=za["zarr_format"],
    store_name_suffix="zarr-v3",
    store_root_attrs=root,
    shape=za["shape"],
    chunks=za["chunks"],
    dtype=za["dtype"],
    compressor=za["compressor"],
    units=zat["units"],
    time_check=timecheck,
    lat_first_last=[lat[0], lat[-1]],
    lon_first_last=[lon[0], lon[-1]],
    window=dict(
        lat_n=LAT_N,
        lat_s=LAT_S,
        lon_w=LON_W,
        lon_e=LON_E,
        rows=len(rows),
        cols=len(cols),
        lat=lat_w,
        lon=lon_w,
        stored_lon_cols=[cols[0], cols[-1]],
        stored_lon_first=lon[cols[0]],
        stored_lon_last=lon[cols[-1]],
        wraps_prime_meridian=True,
    ),
    sources=src_info,
    cities=cities,
    stats=stats,
    grid_K={n: res[n] for n in res},
    grid_C=gc,
    diff_C=diff,
)
open(f"{OUT}/results.json", "w").write(json.dumps(results, indent=1))
with open(f"{OUT}/window.csv", "w") as f:
    f.write("time_utc,lat,lon,t2m_K,t2m_C\n")
    for name, dt in WANT:
        for i, la in enumerate(lat_w):
            for j, lo in enumerate(lon_w):
                f.write(
                    f"{dt.isoformat()}Z,{la:.2f},{lo:.2f},{res[name][i][j]!r},{gc[name][i][j]:.4f}\n"
                )
print(
    "window",
    len(rows),
    "x",
    len(cols),
    "rows",
    r0,
    r1 - 1,
    "blocks",
    {n: (s["blocks_decoded"], s["blocks_total"]) for n, s in src_info.items()},
)
for c in cities:
    print(
        c["name"],
        round(c["morning_C"], 2),
        round(c["afternoon_C"], 2),
        round(c["diff_C"], 2),
    )
print(json.dumps(stats))
# ---- HTML
tpl = open("/recipe/template.html").read()
payload = dict(
    lat=lat_w,
    lon=lon_w,
    K={"am": res["morning"], "pm": res["afternoon"]},
    cities=cities,
    src={
        n: dict(key=s["key"], bytes=s["compressed_bytes"], md5=s["md5_hex"])
        for n, s in src_info.items()
    },
    stats=stats,
    zf=zg["zarr_format"],
)
html = tpl.replace("/*DATA*/null", json.dumps(payload, separators=(",", ":")))
open(f"{OUT}/index.html", "w").write(html)
print("html bytes", len(html))
