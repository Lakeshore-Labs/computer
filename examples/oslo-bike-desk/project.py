"""Use GDAL through Fiona to verify the original features and write a metric GeoPackage."""

import json
from pathlib import Path

import fiona
from fiona.transform import transform


def project(edition: Path, source: Path):
    info = json.loads((source / "station_information.json").read_bytes())["data"][
        "stations"
    ]
    info = {str(r["station_id"]): r for r in info}
    latest = max(source.glob("2*/station_status.json"))
    observed = {
        str(r["station_id"]): r
        for r in json.loads(latest.read_bytes())["data"]["stations"]
        if r["is_installed"]
    }
    count = 0
    with fiona.open(
        "zip://" + str(edition / "oslo-stations.zip"), layer="oslo"
    ) as data:
        if data.crs.to_epsg() != 4326:
            raise ValueError("unexpected input coordinate system")
        with fiona.open(
            edition / "stations.gpkg",
            "w",
            driver="GPKG",
            layer="stations",
            schema=data.schema,
            crs="EPSG:25832",
        ) as gpkg:
            for feature in data:
                row = dict(feature["properties"])
                station = info[row["ID"]]
                lon, lat = feature["geometry"]["coordinates"]
                live = observed.get(row["ID"])
                if (lon, lat) != (station["lon"], station["lat"]) or row[
                    "NAME"
                ] != station["name"]:
                    raise ValueError("GIS feature differs from the station feed")
                if live:
                    if (
                        row["BIKES"],
                        row["DOCKS"],
                        row["RENTING"],
                        row["RETURNING"],
                        row["INSTALLED"],
                    ) != (
                        live["num_bikes_available"],
                        live["num_docks_available"],
                        int(live["is_renting"]),
                        int(live["is_returning"]),
                        1,
                    ):
                        raise ValueError("GIS availability differs from the feed")
                elif (
                    row["INSTALLED"] != 0
                    or row["BIKES"] is not None
                    or row["DOCKS"] is not None
                ):
                    raise ValueError("inactive station has recorded availability")
                east, north = transform("EPSG:4326", "EPSG:25832", [lon], [lat])
                gpkg.write(
                    {
                        "geometry": {
                            "type": "Point",
                            "coordinates": (east[0], north[0]),
                        },
                        "properties": row,
                    }
                )
                count += 1
    if count != len(info):
        raise ValueError("station count differs from the feed")
    with fiona.open(edition / "stations.gpkg", layer="stations") as gpkg:
        assert gpkg.crs.to_epsg() == 25832 and len(gpkg) == count
    return {
        "fiona": fiona.__version__,
        "gdal": fiona.__gdal_version__,
        "read_driver": "ESRI Shapefile",
        "written_driver": "GPKG",
        "crs": "EPSG:25832",
        "points_checked_against_feed": count,
    }
