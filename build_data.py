#!/usr/bin/env python3
"""Build the static dataset for the Idaho Stream Finder site.

Fetches every water from the IFWIS Fishing Planner API, then tags each water
with the species observed there (one API call per species — the API can only
OR-filter, so we invert it into a water -> species map), the recommended game
fish, and the IDFG region. Writes:

    data.js       window.IDAHO_WATERS = {...}  (loaded by index.html, works from file://)
    waters.json   same payload as plain JSON

Run:  python build_data.py
Takes a couple of minutes (~110 sequential requests, politely throttled).
"""
import json
import time
import urllib.parse
import urllib.request
from datetime import date

from streams import API, COUNTIES, SPECIES, REGIONS

_ID2COUNTY = {str(v): k for k, v in COUNTIES.items()}


def norm_county(loc):
    """Clean the API's loc field: 'Ada,Canyon' spacing, bare county ids ('25'),
    and the 'Owhyee' typo all appear in source data."""
    if not loc:
        return ""
    out = []
    for t in (t.strip() for t in loc.split(",")):
        if not t:
            continue
        t = {"Owhyee": "Owyhee"}.get(t, t)
        t = _ID2COUNTY.get(t, t)
        if t not in out:
            out.append(t)
    return ", ".join(out)

# "Recommended Game Fish" ids scraped from the filter page (2026-07).
GAME = {
    "Arctic Grayling": 19610, "Black Crappie": 17591,
    "Bluegill / Pumpkinseed / Sunfish": 7257, "Brook Trout": 18219,
    "Brown Trout": 17938, "Bull Trout": 19737, "Bullhead Catfish": 7391,
    "Catfish": 18956, "Chinook Salmon": 17129, "Coho Salmon": 17203,
    "Crappie": 8097, "Cutbow - Cutthroat x Rainbow Trout": 78500,
    "Cutthroat Trout": 18154, "Kokanee": 16015, "Lake Trout": 17608,
    "Largemouth Bass": 16488, "Mountain Whitefish": 18744, "Northern Pike": 17438,
    "Rainbow Trout": 19083, "Redband Trout": 79983, "Smallmouth Bass": 18815,
    "Steelhead": 80796, "Tiger Muskie": 78495, "Tiger Trout": 78498,
    "Walleye": 16548, "White Crappie": 19841, "White Sturgeon": 15794,
    "Yellow Perch": 17482,
}


def fetch_ids(**params):
    q = {"limit": 20000, "offset": 0, **params}
    url = API + "?" + urllib.parse.urlencode(q)
    req = urllib.request.Request(url, headers={"User-Agent": "idaho-stream-finder-build/1.0"})
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=120) as r:
                data = json.load(r)
            if data.get("response") != "success":
                raise RuntimeError(f"API error: {data}")
            return data["rows"]
        except Exception as e:
            if attempt == 2:
                raise
            print(f"  retry after error: {e}")
            time.sleep(5)


def main():
    print("Fetching base water list...")
    base = fetch_ids()
    waters = {}
    for row in base:
        waters[row["id"]] = {
            "id": row["id"], "name": row["name"], "var": row.get("var"),
            "county": norm_county(row.get("loc")), "trib": row.get("trib") or "",
            "layer": row.get("layer"), "size": row.get("size") or "",
            "rfw": row.get("rfw", 0), "ffw": row.get("ffw", 0),
            "region": None, "sp": [], "gm": [],
        }
    print(f"  {len(waters)} waters")

    for rid, rname in REGIONS.items():
        rows = fetch_ids(region=rid)
        for row in rows:
            w = waters.get(row["id"])
            if w:
                w["region"] = rid
        print(f"  region {rid} {rname}: {len(rows)}")
        time.sleep(0.5)

    print("Tagging species observed (one call per species)...")
    for name, sid in sorted(SPECIES.items()):
        rows = fetch_ids(presence=sid)
        for row in rows:
            w = waters.get(row["id"])
            if w is None:  # water not in unfiltered base list; keep it anyway
                w = waters[row["id"]] = {
                    "id": row["id"], "name": row["name"], "var": row.get("var"),
                    "county": norm_county(row.get("loc")), "trib": row.get("trib") or "",
                    "layer": row.get("layer"), "size": row.get("size") or "",
                    "rfw": row.get("rfw", 0), "ffw": row.get("ffw", 0),
                    "region": None, "sp": [], "gm": [],
                }
            w["sp"].append(sid)
        print(f"  {name}: {len(rows)}")
        time.sleep(0.5)

    print("Tagging recommended game fish...")
    for name, gid in sorted(GAME.items()):
        rows = fetch_ids(game=gid)
        for row in rows:
            w = waters.get(row["id"])
            if w:
                w["gm"].append(gid)
        print(f"  {name}: {len(rows)}")
        time.sleep(0.5)

    payload = {
        "generated": date.today().isoformat(),
        "species": SPECIES,
        "game": GAME,
        "regions": {str(k): v for k, v in REGIONS.items()},
        "waters": sorted(waters.values(), key=lambda w: (w["name"], w["id"])),
    }
    with open("waters.json", "w", encoding="utf-8") as f:
        json.dump(payload, f, separators=(",", ":"))
    with open("data.js", "w", encoding="utf-8") as f:
        f.write("window.IDAHO_WATERS = ")
        json.dump(payload, f, separators=(",", ":"))
        f.write(";\n")
    n_sp = sum(1 for w in payload["waters"] if w["sp"])
    print(f"\nDone: {len(payload['waters'])} waters, {n_sp} with species data "
          f"-> data.js ({round(len(json.dumps(payload))/1e6, 1)} MB)")


if __name__ == "__main__":
    main()
