# Idaho Stream Finder

Having fun with the official [IDFG Fishing Planner](https://idfg.idaho.gov/ifwis/fishingplanner/). 

Basic CLI + Charm-ified, bubble tea-ified TUI + full fledged interactive map website.

### tl;dr:
Two ways to use it, same underlying data:

| | Requires | Run |
|---|---|---|
| **Website** | just a browser | open `index.html` |
| **TUI** | [Go](https://go.dev/dl/) | `cd charm && go build -o streams-charm.exe . && ./streams-charm.exe` |

`streams.py` is a third, simpler option: a scriptable command-line query, no
build step, just Python.

# Use it

## Interactive Website

Open `index.html` in a browser (double-click works), or serve it:

```
python -m http.server 8000
# then http://localhost:8000
```

- Click a species to cycle **✓ must have → ✕ must not have → off**; quick buttons for
  "Any cutthroat" / "No brook trout".
- **Recency**: require a species to have been seen since a given year — a stale exclusion
  (e.g. brook trout only ever recorded once, decades ago) stops disqualifying a water once
  its record ages out, and a required species only counts with recent-enough evidence.
  Years are shown next to each species in the table, e.g. `Cutthroat Trout ('06)`.
- Click a table row to draw that stream/lake on the map (live from IDFG's ArcGIS
  hydrography service); **Map results** draws the whole filtered set (first 3,000 — tested
  drawing 2,613 statewide in ~23s; only the broadest unfiltered queries hit this cap). The
  table shows the same first 3,000, so it never disagrees with what's mapped/exported.
- Every row links to its official IDFG water page. **Export CSV** saves the filtered list;
  **Export GPX** saves whatever's currently drawn on the map (a highlighted water and/or a
  full "Map results" set) as GPS tracks — import straight into Gaia GPS, onX, CalTopo, or
  any GPX-reading app. **onX-safe splitting** (checkbox next to the export button, on by
  default) works around onX's real limits — 4MB/file, ~1,500 points/track even in an
  otherwise-compliant file — by cutting long/multi-part streams into numbered pieces
  across multiple files, e.g. "Bear Valley Creek (1/6)". Uncheck it for Gaia, CalTopo, or
  anything else that doesn't need that: exactly one track per water, always, however big —
  a large export can still span multiple files either way (a save/upload step can fail on
  one huge file even when its preview parser handles it fine; observed with a 13MB
  single-file export), that splitting just never touches any individual track's data.
- Basemap picker (top-right of the map) includes **OpenTopoMap**, a trail-rendering topo
  layer, and a **Public land** overlay (BLM/USFS/NPS/State/Tribal — from BLM's national
  land-ownership tile service) for scouting access before a trip.
- Optional premium basemaps: copy `config.local.example.js` to `config.local.js`
  (already gitignored — never committed) and paste in a **Mapbox** token (free, 200k
  tile requests/mo, no card — mapbox.com → Account → Tokens) for the "Mapbox Outdoors" style,
  and/or a **CalTopo** WMTS URL if you're on their Desktop tier. Either, both, or
  neither — missing ones just don't show up as basemap options.

## Terminal app (TUI) [Charm/Bubble Tea]

`charm/` is a full Charm-stack TUI — **huh** forms, **lip gloss** styling,
**Bubble Tea** views — reading the `waters.json` snapshot (no network needed
once it's built). Requires [Go](https://go.dev/dl/) 1.21+; there's no prebuilt
binary in this repo, so build it once:

```
cd charm
go build -o streams-charm.exe .    # add .exe only on Windows; omit it on macOS/Linux
./streams-charm.exe                # run in Windows Terminal/PowerShell/a real terminal
./streams-charm.exe --demo         # non-interactive smoke test, no keyboard needed
./streams-charm.exe --maps         # just the region/county reference maps
```

Walkthrough: pick species to require/exclude (species picker also has 🗺 bundle
shortcuts like "Any cutthroat"), then region/county/water type — press **?**
anytime on that page for a labeled reference map of IDFG regions ⇄ counties
(`tab` toggles between them). Results print as a card + species histogram, then
**Browse the full table** scrolls every match. From there, **enter/→** on a
water opens an in-TUI recreation of its IDFG page with a live **terminal map**:
the stream traced in cyan braille, the surrounding drainage in dim blue, over a
dimmed Esri topo tile backdrop (`b` toggles it). **`i`** opens a statewide
overview — every result stream plotted against Idaho's counties at once.

## CLI

`streams.py` is a standalone command-line version of the same queries (hits the live API):

```
python streams.py --with cutthroat-any --without brook-any --county Lemhi --body streams
python streams.py --list-species
```

# Data 

## Refresh the data

The species/water dataset is a static snapshot (`data.js`, generated date shown in the
header). Rebuild it from the live IFWIS API (~5 min, ~110 throttled requests):

```
python build_data.py
```

Species have no observation year at this point — `build_data.py`'s bulk API doesn't expose
one. Add per-species years (powers the Recency filter) with a second pass, one page fetch
per surveyed water (~4,000, small thread pool, a few minutes):

```
python build_years.py
```

# Notes

## How it works

The Fishing Planner's filter page is backed by an undocumented JSON API
(`/ifwis/fishingplanner/api/2.0/list/`) that can only OR-combine species. `build_data.py`
calls it once per species and inverts the result into a water→species map so the site can
do AND/NOT set logic instantly client-side. Stream traces come from
`gisportal-idfg.idaho.gov` (`Hydrography_Public` MapServer, layers 0=lakes 1=streams,
keyed by LLID, `f=geojson`, CORS-open).

Caveat: "not observed" means *no survey ever recorded it* — small waters are surveyed
rarely, so absence of a record is not proof of absence in the water.

Each water's `presence` list actually conflates two separate sources IDFG tracks
separately: **survey observations** and **stocking records** — a water can show a species
purely because it was stocked there, with no survey ever confirming it stuck around.
`build_years.py` reads both tables per water page and keeps the most recent year per
species regardless of source (stocking names are free text, not IDs, so a small alias/
pattern table maps common cases like `"Cutthroat Trout - Bonneville"` back to the species
dict; unrecognized names are left unmapped rather than guessed). ~99% of surveyed waters
end up with at least one dated species; the rest have a presence flag from IDFG with no
corresponding record on either table — treated as "unknown year," which never counts
against a water under the Recency filter.
