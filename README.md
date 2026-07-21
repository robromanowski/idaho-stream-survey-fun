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
- Click a table row to draw that stream/lake on the map (live from IDFG's ArcGIS
  hydrography service); **Map results** draws the whole filtered set (first 300).
- Every row links to its official IDFG water page. **Export CSV** saves the filtered list.

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
