#!/usr/bin/env python3
"""Query the Idaho Fishing Planner (IFWIS) for waters by species observed.

Wraps the undocumented JSON API behind https://idfg.idaho.gov/ifwis/fishingplanner/filter/
    GET https://idfg.idaho.gov/ifwis/fishingplanner/api/2.0/list/
    params: presence=<species ids, comma = OR>, game=, body=, region=, county=, limit=, offset=

The API only supports OR-inclusion, so this tool fetches full result sets and
does AND / NOT set operations locally (results are small, one request per species).

Examples:
    python streams.py --with "cutthroat"                         # any water with Cutthroat observed
    python streams.py --with cutthroat-any --county Lemhi        # all cutthroat variants, Lemhi county
    python streams.py --with cutthroat --without "brook trout"   # cutthroat but NO brook trout
    python streams.py --with "brook trout" --region 7 --body streams
    python streams.py --with cutthroat --and-with "bull trout"   # must have BOTH
    python streams.py --list-species
"""
import argparse
import csv
import json
import sys
import urllib.parse
import urllib.request

API = "https://idfg.idaho.gov/ifwis/fishingplanner/api/2.0/list/"
WATER_URL = "https://idfg.idaho.gov/ifwis/fishingplanner/water/{id}"

# "Fish Observed in Surveys and Stocking" ids scraped from the filter page (2026-07).
SPECIES = {
    "American Shad": 16293, "Arctic Char": 16448, "Arctic Grayling": 19610,
    "Atlantic Salmon": 18530, "Bear Lake Whitefish": 17770, "Black Bullhead": 18721,
    "Black Crappie": 17591, "Bluegill": 16589, "Bonneville Cisco": 16255,
    "Bonneville Cutthroat Trout": 80327, "Bonneville Whitefish": 16582,
    "Brook Trout": 18219, "Brook Trout - Triploid": 93800, "Brown Bullhead": 17549,
    "Brown Trout": 17938, "Bull Trout": 19737, "Burbot": 16458, "Catfish": 18956,
    "Channel Catfish": 19986, "Chinook Salmon": 17129,
    "Cobow - Coho Salmon x Rainbow Trout": 1855836, "Coho Salmon": 17203,
    "Crayfish": 10793, "Cutbow - Cutthroat x Rainbow Trout": 78500,
    "Cutthroat Trout": 18154, "Flathead Catfish": 19177, "Golden Trout": 79972,
    "Green Sunfish": 18179, "Kokanee": 16015, "Lahontan Cutthroat Trout": 79970,
    "Lake Trout": 17608, "Lake Trout - Triploid": 93801, "Lake Whitefish": 19320,
    "Largemouth Bass": 16488, "Mountain Whitefish": 18744, "Northern Pike": 17438,
    "Pumpkinseed": 19003, "Pygmy Whitefish": 15409, "Rainbow Trout": 19083,
    "Rainbow x Cutthroat - Diploid": 93799, "Rainbow x Cutthroat - Triploid": 93788,
    "Redband Trout": 79983, "Sauger": 16776, "Smallmouth Bass": 18815,
    "Snake River Fine-spotted Cutthroat Trout": 79758, "Sockeye Salmon": 79784,
    "Splake": 78506, "Steelhead (Snake River Basin DPS)": 79812,
    "Sunapee Trout": 81082, "Tiger Muskie": 78495, "Tiger Trout": 78498,
    "Walleye": 16548, "Warmouth": 17350, "Westslope Cutthroat Trout": 80442,
    "White Crappie": 19841, "White Sturgeon": 15794, "Yellow Perch": 17482,
    "Yellowstone Cutthroat Trout": 80060,
}

# Convenience groups (name -> list of species names)
GROUPS = {
    "cutthroat-any": [
        "Cutthroat Trout", "Westslope Cutthroat Trout", "Yellowstone Cutthroat Trout",
        "Bonneville Cutthroat Trout", "Lahontan Cutthroat Trout",
        "Snake River Fine-spotted Cutthroat Trout",
    ],
    "cutthroat-pure": [  # excludes hybrids, includes all pure subspecies
        "Cutthroat Trout", "Westslope Cutthroat Trout", "Yellowstone Cutthroat Trout",
        "Bonneville Cutthroat Trout", "Lahontan Cutthroat Trout",
        "Snake River Fine-spotted Cutthroat Trout",
    ],
    "cutbow-any": [
        "Cutbow - Cutthroat x Rainbow Trout", "Rainbow x Cutthroat - Diploid",
        "Rainbow x Cutthroat - Triploid",
    ],
    "brook-any": ["Brook Trout", "Brook Trout - Triploid"],
}

BODY = {"streams": 1, "rivers": 1, "lakes": 2, "hml": 3, "high-mountain-lakes": 3}
REGIONS = {
    1: "Panhandle", 2: "Clearwater", 3: "SW-Nampa", 8: "SW-McCall",
    4: "Magic Valley", 5: "Southeast", 6: "Upper Snake", 7: "Salmon",
}
COUNTIES = {
    "Ada": 1, "Adams": 2, "Bannock": 3, "Bear Lake": 4, "Benewah": 5, "Bingham": 6,
    "Blaine": 7, "Boise": 8, "Bonner": 9, "Bonneville": 10, "Boundary": 11,
    "Butte": 12, "Camas": 13, "Canyon": 14, "Caribou": 15, "Cassia": 16, "Clark": 17,
    "Clearwater": 18, "Custer": 19, "Elmore": 20, "Franklin": 21, "Fremont": 22,
    "Gem": 23, "Gooding": 24, "Idaho": 25, "Jefferson": 26, "Jerome": 27,
    "Kootenai": 28, "Latah": 29, "Lemhi": 30, "Lewis": 31, "Lincoln": 32,
    "Madison": 33, "Minidoka": 34, "Nez Perce": 35, "Oneida": 36, "Owyhee": 37,
    "Payette": 38, "Power": 39, "Shoshone": 40, "Teton": 41, "Twin Falls": 42,
    "Valley": 43, "Washington": 44,
}


def resolve_species(term):
    """Match a user term to species ids. Returns (label, [ids])."""
    t = term.strip().lower()
    if t in GROUPS:
        return term, [SPECIES[n] for n in GROUPS[t]]
    exact = [n for n in SPECIES if n.lower() == t]
    if exact:
        return exact[0], [SPECIES[exact[0]]]
    partial = [n for n in SPECIES if t in n.lower()]
    if len(partial) == 1:
        return partial[0], [SPECIES[partial[0]]]
    if len(partial) > 1:
        # "cutthroat" matches many; treat as the whole family unless ambiguous otherwise
        sys.stderr.write(f"note: '{term}' matches {len(partial)} species, using all: "
                         + ", ".join(partial) + "\n")
        return term, [SPECIES[n] for n in partial]
    sys.exit(f"error: no species matches '{term}' (try --list-species)")


def fetch(presence_ids=None, body=None, region=None, county=None, game=None):
    """Fetch full water list for the given filters. Returns {id: row}."""
    params = {"limit": 20000, "offset": 0}
    if presence_ids:
        params["presence"] = ",".join(str(i) for i in presence_ids)
    if game:
        params["game"] = ",".join(str(i) for i in game)
    if body:
        params["body"] = body
    if region:
        params["region"] = region
    if county:
        params["county"] = county
    url = API + "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(url, headers={"User-Agent": "idaho-stream-finder/1.0"})
    with urllib.request.urlopen(req, timeout=60) as r:
        data = json.load(r)
    if data.get("response") != "success":
        sys.exit(f"error: API returned {data}")
    return {row["id"]: row for row in data["rows"]}


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0],
                                formatter_class=argparse.RawDescriptionHelpFormatter,
                                epilog=__doc__.split("Examples:")[1])
    p.add_argument("--with", dest="include", action="append", default=[],
                   metavar="SPECIES", help="species that must be observed (repeatable = OR)")
    p.add_argument("--and-with", dest="require", action="append", default=[],
                   metavar="SPECIES", help="species that must ALSO be observed (AND)")
    p.add_argument("--without", action="append", default=[],
                   metavar="SPECIES", help="species that must NOT be observed (repeatable)")
    p.add_argument("--body", choices=sorted(BODY), help="water body type")
    p.add_argument("--region", type=int, choices=sorted(REGIONS),
                   help="IDFG region (1 Panhandle, 2 Clearwater, 3 Nampa, 8 McCall, "
                        "4 Magic Valley, 5 Southeast, 6 Upper Snake, 7 Salmon)")
    p.add_argument("--county", help="county name, e.g. Lemhi")
    p.add_argument("--csv", metavar="FILE", help="write results to CSV")
    p.add_argument("--list-species", action="store_true", help="print known species and groups")
    args = p.parse_args()

    if args.list_species:
        print("Species:")
        for n in sorted(SPECIES):
            print(f"  {n}")
        print("\nGroups:")
        for g, members in GROUPS.items():
            print(f"  {g}: {', '.join(members)}")
        return
    if not args.include:
        p.error("--with is required (or use --list-species)")

    county = None
    if args.county:
        match = [c for c in COUNTIES if c.lower() == args.county.strip().lower()]
        if not match:
            sys.exit(f"error: unknown county '{args.county}'")
        county = COUNTIES[match[0]]
    body = BODY[args.body] if args.body else None
    scope = dict(body=body, region=args.region, county=county)

    inc_ids = []
    for term in args.include:
        _, ids = resolve_species(term)
        inc_ids += ids
    result = fetch(presence_ids=inc_ids, **scope)

    for term in args.require:
        label, ids = resolve_species(term)
        req = fetch(presence_ids=ids, **scope)
        result = {k: v for k, v in result.items() if k in req}
        sys.stderr.write(f"AND {label}: {len(result)} remain\n")

    for term in args.without:
        label, ids = resolve_species(term)
        excl = fetch(presence_ids=ids, **scope)
        before = len(result)
        result = {k: v for k, v in result.items() if k not in excl}
        sys.stderr.write(f"NOT {label}: removed {before - len(result)}, {len(result)} remain\n")

    rows = sorted(result.values(), key=lambda r: (r["name"], r["id"]))
    layer_names = {0: "lake", 1: "stream"}
    out_rows = [{
        "name": r["name"] + (f" ({r['var']})" if r.get("var") else ""),
        "county": r.get("loc") or "",
        "tributary": r.get("trib") or "",
        "type": layer_names.get(r.get("layer"), r.get("layer")),
        "size": r.get("size") or "",
        "url": WATER_URL.format(id=r["id"]),
    } for r in rows]

    if args.csv:
        with open(args.csv, "w", newline="", encoding="utf-8") as f:
            w = csv.DictWriter(f, fieldnames=out_rows[0].keys() if out_rows else
                               ["name", "county", "tributary", "type", "size", "url"])
            w.writeheader()
            w.writerows(out_rows)
        print(f"{len(out_rows)} waters written to {args.csv}")
        return

    print(f"{len(out_rows)} matching waters\n")
    for r in out_rows:
        print(f"{r['name']}  [{r['county']}]  {r['tributary']}")
        print(f"    {r['url']}")


if __name__ == "__main__":
    main()
