#!/usr/bin/env python3
"""Add per-species "last known year" (survey or stocking) to waters.json / data.js.

The bulk list API (used by build_data.py) doesn't expose *when* a species was
last observed — its `presence` filter actually conflates two separate sources
that only appear on each water's own page:

  Species Observed in Surveys:
    <li><a .../species/taxa/18219 ...>Brook Trout</a>
        <small><em>Salvelinus fontinalis observed in 2025</em></small> ...

  Fish Stocking Records (species given as a display name, no id):
    <tr><td>2024/06/18</td><td>Cutthroat Trout - Bonneville</td>...

A water can appear in `sp` purely because it was *stocked*, with no survey
record at all — so both tables have to be read and merged (most-recent-year
wins per species) or a lot of waters would wrongly show "no year data".

One HTTP GET per water that has species data (~4,000), small thread pool.
Adds a "spy" field per water: {species_id: year}.

Run after build_data.py:
    python build_years.py
Takes several minutes. Safe to re-run (only re-fetches waters missing "spy" —
delete that key first, e.g. via a one-off script, to force a full redo).
"""
import json
import re
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

from streams import SPECIES

WATER_URL = "https://idfg.idaho.gov/ifwis/fishingplanner/water/{id}"
SURVEY_ROW_RE = re.compile(
    r'<li><a href="https://idfg\.idaho\.gov/species/taxa/(\d+)"[^>]*>[^<]*</a>\s*'
    r'<small[^>]*><em>[^<]* observed in (\d{4})</em>'
)
STOCKING_ROW_RE = re.compile(r'<tr>\s*<td>(\d{4})/\d{2}/\d{2}</td>\s*<td>([^<]+)</td>')
WORKERS = 8
RETRIES = 3

# Stocking rows give a free-text display name, not a species id, and the
# naming doesn't match SPECIES consistently ("Cutthroat Trout - Bonneville"
# vs. our "Bonneville Cutthroat Trout"). Handle the patterns actually seen on
# the site; anything else is left unmapped rather than guessed.
_ALIASES = {
    "steelhead": "Steelhead (Snake River Basin DPS)",
    "tiger muskellunge": "Tiger Muskie",
}


def match_species(name):
    name = name.strip()
    if name in SPECIES:
        return SPECIES[name]
    if name.lower() in _ALIASES:
        return SPECIES[_ALIASES[name.lower()]]
    if " - " in name:
        base, variant = (p.strip() for p in name.split(" - ", 1))
        vlow = variant.lower()
        if vlow == "unspecified":
            return SPECIES.get(base)
        if vlow in ("triploid", "diploid"):
            # some dict entries keep this order literally (Brook/Lake Trout,
            # Rainbow x Cutthroat); otherwise fall back to the base species
            return SPECIES.get(name) or SPECIES.get(base)
        # named strains read reversed: "Cutthroat Trout - Bonneville" -> "Bonneville Cutthroat Trout"
        return SPECIES.get(f"{variant} {base}") or SPECIES.get(base)
    return None


def fetch_years(water_id):
    url = WATER_URL.format(id=water_id)
    req = urllib.request.Request(url, headers={"User-Agent": "idaho-stream-finder-build/1.0"})
    for attempt in range(RETRIES):
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                html = r.read().decode("utf-8", errors="replace")
            break
        except (urllib.error.URLError, TimeoutError):
            if attempt == RETRIES - 1:
                raise
            time.sleep(1.5 * (attempt + 1))

    years = {}
    for sid, yr in SURVEY_ROW_RE.findall(html):
        years[sid] = max(years.get(sid, 0), int(yr))
    stock_start = html.find('id="fish-stocking"')
    stock_html = html[stock_start:] if stock_start != -1 else ""
    for yr, name in STOCKING_ROW_RE.findall(stock_html):
        sid = match_species(name)
        if sid is not None:
            sid = str(sid)
            years[sid] = max(years.get(sid, 0), int(yr))
    return years


def main():
    with open("waters.json", encoding="utf-8") as f:
        data = json.load(f)

    todo = [w for w in data["waters"] if w["sp"] and "spy" not in w]
    print(f"{len(data['waters'])} waters total, {len(todo)} need year data "
          f"({sum(1 for w in data['waters'] if w['sp']) - len(todo)} already done)")
    if not todo:
        print("Nothing to do.")
        return

    by_id = {w["id"]: w for w in todo}
    done = 0
    failed = []
    t0 = time.time()
    with ThreadPoolExecutor(max_workers=WORKERS) as ex:
        futures = {ex.submit(fetch_years, wid): wid for wid in by_id}
        for fut in as_completed(futures):
            wid = futures[fut]
            try:
                years = fut.result()
                by_id[wid]["spy"] = {str(k): v for k, v in years.items()}
            except Exception as e:
                failed.append(wid)
                by_id[wid]["spy"] = {}  # mark attempted so a re-run doesn't loop forever
            done += 1
            if done % 200 == 0 or done == len(todo):
                elapsed = time.time() - t0
                rate = done / elapsed if elapsed else 0
                eta = (len(todo) - done) / rate if rate else 0
                print(f"  {done}/{len(todo)}  ({rate:.1f}/s, ~{eta/60:.1f} min left)",
                      file=sys.stderr)

    if failed:
        print(f"{len(failed)} waters failed after {RETRIES} retries "
              f"(marked with empty spy so a re-run won't retry them by default): "
              f"{failed[:10]}{'...' if len(failed) > 10 else ''}")

    with open("waters.json", "w", encoding="utf-8") as f:
        json.dump(data, f, separators=(",", ":"))
    with open("data.js", "w", encoding="utf-8") as f:
        f.write("window.IDAHO_WATERS = ")
        json.dump(data, f, separators=(",", ":"))
        f.write(";\n")
    n_with_years = sum(1 for w in data["waters"] if w.get("spy"))
    print(f"\nDone: {n_with_years} waters have year data "
          f"-> data.js ({round(len(json.dumps(data))/1e6, 1)} MB)")


if __name__ == "__main__":
    main()
