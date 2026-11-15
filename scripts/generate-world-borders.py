#!/usr/bin/env python3
"""Derive the local, non-executable globe layer from the pinned public dataset."""
import hashlib
import json
import math
import pathlib
import sys

if len(sys.argv) != 3:
    raise SystemExit("usage: generate-world-borders.py SOURCE.geojson OUTPUT.json")

SOURCE_SHA256 = "6866c877d39cba9c357620878839b336d569f8c662d3cfab4cb1dbe2d39c977f"
raw = pathlib.Path(sys.argv[1]).read_bytes()
if hashlib.sha256(raw).hexdigest() != SOURCE_SHA256:
    raise SystemExit("source does not match the reviewed Natural Earth snapshot")
source = json.loads(raw)
if source.get("type") != "FeatureCollection":
    raise SystemExit("expected a GeoJSON FeatureCollection")
rings = []
for feature in source["features"]:
    geometry = feature["geometry"]
    if geometry["type"] == "Polygon":
        polygons = [geometry["coordinates"]]
    elif geometry["type"] == "MultiPolygon":
        polygons = geometry["coordinates"]
    else:
        raise SystemExit("unexpected geometry type")
    for polygon in polygons:
        for ring in polygon:
            result = []
            for point in ring:
                if len(point) != 2 or any(type(x) not in (int, float) or not math.isfinite(x) for x in point):
                    raise SystemExit("invalid coordinate")
                if not -180 <= point[0] <= 180 or not -90 <= point[1] <= 90:
                    raise SystemExit("coordinate outside degree bounds")
                rounded = [round(point[0], 3), round(point[1], 3)]
                if not result or rounded != result[-1]:
                    result.append(rounded)
            if result and result[-1] != result[0]:
                result.append(result[0])
            if len(result) >= 4:
                rings.append(result)
encoded = (json.dumps(rings, separators=(",", ":")) + "\n").encode()
pathlib.Path(sys.argv[2]).write_bytes(encoded)
print(json.dumps({"rings": len(rings), "points": sum(map(len, rings)), "sha256": hashlib.sha256(encoded).hexdigest(), "bytes": len(encoded)}))
