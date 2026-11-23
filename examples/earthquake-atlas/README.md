# A week beneath our feet

[![Map of 96 earthquakes in the USGS weekly feed on November 22, 2026](media/atlas.png)](media/atlas.svg)

Download the SVG, open it in a browser and hover over a point for the magnitude and location. This capture contains 96 earthquakes from the USGS magnitude 4.5+ past-week feed generated at 17:04 UTC on November 22, 2026. The live recipe will show the week at the time you run it.

## Replay with live observations

From the repository root, use a built Computer binary and its [WASI Python distribution](../../python/README.md).

```sh
CGO_ENABLED=0 go build -o ./bin/computer ./cmd/computer
sh examples/earthquake-atlas/run.sh ./bin/computer ./python-wasi ./bin/earthquake-atlas
```

The shell has two allowed URLs. It downloads the feed and the pinned Natural Earth land geometry into the output mount. Python projects longitude and latitude onto a world map and writes an SVG with a title for every observation. The output directory holds both source responses and the map.

## Replay the captured week

A second mode uses the committed USGS observations and pinned Natural Earth geometry. It runs offline into a fresh directory and verifies the projected coordinates. The host can rasterize the resulting SVG with librsvg to reproduce the PNG preview.

```sh
sh examples/earthquake-atlas/run.sh --captured ./bin/computer ./python-wasi ./bin/captured-week
rsvg-convert -w 1080 ./bin/captured-week/atlas.svg -o ./bin/captured-week/atlas.png
```

[Map renderer](map.py) · [USGS GeoJSON feed documentation](https://earthquake.usgs.gov/earthquakes/feed/v1.0/geojson.php) · [Natural Earth land data and reuse terms](https://www.naturalearthdata.com/about/terms-of-use/)
