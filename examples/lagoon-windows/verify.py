"""Read the same public GeoTIFF through GDAL and compare every RGB pixel to Computer."""

import hashlib
import json
import sys
from pathlib import Path

import rasterio
from rasterio.warp import transform
from rasterio.windows import Window

here = Path(__file__).resolve().parent
out = Path(sys.argv[1]).resolve()
receipt = json.loads((out / "windows.json").read_bytes())
probe = json.loads((here / "source/probe.json").read_bytes())
checks = []
with (
    rasterio.Env(
        GDAL_DISABLE_READDIR_ON_OPEN="EMPTY_DIR",
        CPL_VSIL_CURL_ALLOWED_EXTENSIONS=".tif",
        GDAL_HTTP_TIMEOUT="30",
    ),
    rasterio.open(probe["url"]) as image,
):
    assert (
        image.crs.to_epsg() == 32632
        and image.width == receipt["width"]
        and image.height == receipt["height"]
    )
    for w in receipt["windows"]:
        x, y = transform("EPSG:4326", image.crs, [w["lon"]], [w["lat"]])
        row, col = image.index(x[0], y[0])
        assert (col - 128, row - 128) == (w["x"], w["y"])
        array = image.read([1, 2, 3], window=Window(w["x"], w["y"], 256, 256))
        expected = array.transpose(1, 2, 0).tobytes()
        with rasterio.open(out / (w["id"] + ".png")) as png:
            actual = png.read([1, 2, 3]).transpose(1, 2, 0).tobytes()
        assert (
            actual == expected and hashlib.sha256(actual).hexdigest() == w["rgb_sha256"]
        )
        checks.append(
            {
                "place": w["name"],
                "pixels_matched": 256 * 256,
                "rgb_sha256": w["rgb_sha256"],
                "window": [w["x"], w["y"], 256, 256],
            }
        )
meta = {
    "independent_reader": "Rasterio "
    + rasterio.__version__
    + " / GDAL "
    + rasterio.__gdal_version__,
    "whole_object_bytes": int(receipt["full_cog_bytes"]),
    "guest_image_range_bytes": receipt["image_range_bytes"],
    "requested_percent": round(
        receipt["image_range_bytes"] * 100 / int(receipt["full_cog_bytes"]), 4
    ),
    "compressed_blocks": len(receipt["tiles"]),
    "images": checks,
}
(out / "independent.json").write_text(json.dumps(meta, indent=2) + "\n")
print(json.dumps(meta))
