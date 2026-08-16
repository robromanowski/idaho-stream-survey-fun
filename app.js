/* Idaho Stream Finder — client app.
   Data comes from data.js (built by build_data.py). Stream/lake outlines are
   fetched on demand from IDFG's public ArcGIS hydrography service. */
"use strict";

const DATA = window.IDAHO_WATERS;
const HYDRO = "https://gisportal-idfg.idaho.gov/hosting/rest/services/Hydrography/Hydrography_Public/MapServer";
const WATER_URL = id => `https://idfg.idaho.gov/ifwis/fishingplanner/water/${id}`;
const REGION_NAMES = {
  1: "Panhandle", 2: "Clearwater", 3: "SW — Nampa", 8: "SW — McCall",
  4: "Magic Valley", 5: "Southeast", 6: "Upper Snake", 7: "Salmon",
};
const GROUPS = {
  "Any cutthroat": ["Cutthroat Trout", "Westslope Cutthroat Trout", "Yellowstone Cutthroat Trout",
    "Bonneville Cutthroat Trout", "Lahontan Cutthroat Trout", "Snake River Fine-spotted Cutthroat Trout"],
  "No brook trout": ["Brook Trout", "Brook Trout - Triploid"],
  "Any cutbow/hybrid": ["Cutbow - Cutthroat x Rainbow Trout", "Rainbow x Cutthroat - Diploid", "Rainbow x Cutthroat - Triploid"],
};
// MAP_CAP: tested at 2,613 waters (~broadest realistic single-species statewide
// query) drawing correctly in ~23s — 3000 leaves headroom for that while still
// guarding against the pathological "no filters at all" case (~14,000 waters).
// Both capped at the same value so the table and "Map results" never disagree
// about how much of a large filter is actually shown. Tested at 2,613 rows
// (~broadest realistic single-species statewide query): sort/search/render all
// land ~350-400ms, no jank — comfortable headroom under 3000.
const ROW_CAP = 3000, MAP_CAP = 3000;

const speciesById = {};
for (const [name, id] of Object.entries(DATA.species)) speciesById[id] = name;
const gameById = {};
for (const [name, id] of Object.entries(DATA.game)) gameById[id] = name;

for (const w of DATA.waters) {
  w.spSet = new Set(w.sp);
  w.gmSet = new Set(w.gm);
  w.spn = w.sp.length;
  w.sizeNum = parseFloat(w.size) || 0;
  w.searchName = (w.name + " " + (w.var || "")).toLowerCase();
  // sort key ignoring leading punctuation (e.g. 'Imnamatnoon Creek files under I) and case
  w.sortName = w.name.replace(/^[^a-z0-9]+/i, "").toLowerCase();
  // species id -> year last observed (from build_years.py); absent = year unknown
  w.spyMap = new Map(Object.entries(w.spy || {}).map(([id, yr]) => [+id, yr]));
}

// True unless a recency filter is set and we have a too-old (or no) record.
// Used symmetrically: for "must have" it's whether the sighting still counts;
// for "must not have" it's whether an old sighting still disqualifies the water.
function recentEnough(w, id, minYear) {
  if (!minYear) return true;
  if (!w.spyMap.has(id)) return true;   // no year on file — don't penalize either way
  return w.spyMap.get(id) >= minYear;
}

// ---------- state ----------
const spState = new Map();       // species id -> "inc" | "exc"
let reqMode = "any";
let sortKey = "name", sortDir = 1;
let filtered = [];

// ---------- filter panel ----------
const $ = s => document.querySelector(s);
// tolerate an element missing (e.g. stale cached HTML alongside fresh app.js)
// instead of throwing and silently aborting the whole filter pass
const val = s => $(s)?.value ?? "";
const setVal = (s, v) => { const el = $(s); if (el) el.value = v; };
document.getElementById("gendate").textContent = "data " + DATA.generated;

const splist = $("#splist");
const sortedSpecies = Object.keys(DATA.species).sort();
for (const name of sortedSpecies) {
  const div = document.createElement("div");
  div.className = "sp";
  div.dataset.id = DATA.species[name];
  div.innerHTML = `<span class="st"></span><span class="lbl">${name}</span>`;
  div.onclick = () => { cycle(DATA.species[name]); };
  splist.appendChild(div);
}

function cycle(id) {
  const cur = spState.get(id);
  if (!cur) spState.set(id, "inc");
  else if (cur === "inc") spState.set(id, "exc");
  else spState.delete(id);
  refresh();
}

function paintSpecies() {
  for (const div of splist.children) {
    const st = spState.get(+div.dataset.id);
    div.classList.toggle("inc", st === "inc");
    div.classList.toggle("exc", st === "exc");
    div.querySelector(".st").textContent = st === "inc" ? "✓" : st === "exc" ? "✕" : "";
  }
}

// species-list scope: "all" or "trout" (salmonids only); active picks always stay visible
const SALMONID_RE = /trout|salmon|char|steelhead|kokanee|grayling|whitefish|splake|cisco|cutbow/i;
const TYPICAL_TROUT = new Set([
  "Bonneville Cutthroat Trout", "Brook Trout", "Brook Trout - Triploid", "Brown Trout",
  "Bull Trout", "Cutbow - Cutthroat x Rainbow Trout", "Cutthroat Trout", "Golden Trout",
  "Lahontan Cutthroat Trout", "Lake Trout", "Lake Trout - Triploid", "Rainbow Trout",
  "Redband Trout", "Snake River Fine-spotted Cutthroat Trout", "Tiger Trout",
  "Westslope Cutthroat Trout", "Yellowstone Cutthroat Trout",
]);
let spScope = "all";

function filterSpeciesList() {
  const q = $("#spsearch").value.trim().toLowerCase();
  for (const div of splist.children) {
    const name = div.textContent;
    const active = spState.has(+div.dataset.id);
    const plain = div.querySelector(".lbl").textContent;   // name without the ✓/✕ marker
    const scopeOk = spScope === "all" ? true
      : spScope === "typical" ? TYPICAL_TROUT.has(plain)
      : SALMONID_RE.test(plain);
    const show = name.toLowerCase().includes(q) && (scopeOk || active);
    div.style.display = show ? "" : "none";
  }
}

$("#spsearch").addEventListener("input", filterSpeciesList);
$("#spscope").addEventListener("click", e => {
  const b = e.target.closest("button[data-s]");
  if (!b) return;
  spScope = b.dataset.s;
  for (const btn of $("#spscope").children) btn.classList.toggle("on", btn.dataset.s === spScope);
  filterSpeciesList();
});

const groupbtns = $("#groupbtns");
for (const [label, members] of Object.entries(GROUPS)) {
  const b = document.createElement("button");
  b.className = "mini";
  b.textContent = label;
  const mode = label.startsWith("No ") ? "exc" : "inc";
  b.onclick = () => {
    for (const n of members) if (DATA.species[n] != null) spState.set(DATA.species[n], mode);
    refresh();
  };
  groupbtns.appendChild(b);
}
{
  const b = document.createElement("button");
  b.className = "mini";
  b.textContent = "Clear species";
  b.onclick = () => { spState.clear(); refresh(); };
  groupbtns.appendChild(b);
}

$("#reqmode").addEventListener("click", e => {
  if (e.target.dataset.v) {
    reqMode = e.target.dataset.v;
    for (const b of $("#reqmode").children) b.classList.toggle("on", b.dataset.v === reqMode);
    refresh();
  }
});

const regionSel = $("#region");
regionSel.innerHTML = `<option value="">All regions</option>` +
  [1, 2, 3, 8, 4, 5, 6, 7].map(r => `<option value="${r}">${REGION_NAMES[r]}</option>`).join("");

const countySel = $("#county");
// loc is a comma-separated list for waters spanning county lines
const counties = [...new Set(DATA.waters.flatMap(w => w.county.split(", ")).filter(Boolean))].sort();
countySel.innerHTML = `<option value="">All counties</option>` +
  counties.map(c => `<option>${c}</option>`).join("");

for (const id of ["t-stream", "t-lake", "surveyed", "region", "county"])
  document.getElementById(id).addEventListener("change", refresh);
$("#namesearch").addEventListener("input", debounce(refresh, 200));
$("#minyear")?.addEventListener("input", debounce(refresh, 300));

$("#reset").onclick = () => {
  spState.clear();
  $("#t-stream").checked = $("#t-lake").checked = true;
  $("#surveyed").checked = true;
  regionSel.value = countySel.value = "";
  $("#namesearch").value = ""; $("#spsearch").value = ""; setVal("#minyear", "");
  $("#presets").value = "";   // so re-selecting the same preset fires change again
  spScope = "all";
  for (const btn of $("#spscope").children) btn.classList.toggle("on", btn.dataset.s === "all");
  refresh();
};

function debounce(fn, ms) { let t; return () => { clearTimeout(t); t = setTimeout(fn, ms); }; }

// ---------- filtering ----------
function applyFilters() {
  const inc = [], exc = [];
  for (const [id, st] of spState) (st === "inc" ? inc : exc).push(id);
  const wantStream = $("#t-stream").checked, wantLake = $("#t-lake").checked;
  const region = +regionSel.value || null;
  const county = countySel.value || null;
  const surveyed = $("#surveyed").checked;
  const q = $("#namesearch").value.trim().toLowerCase();
  const minYear = +val("#minyear") || null;

  filtered = DATA.waters.filter(w => {
    if (w.layer === 1 ? !wantStream : !wantLake) return false;
    if (region && w.region !== region) return false;
    if (county && !w.county.split(", ").includes(county)) return false;
    if (surveyed && w.spn === 0) return false;
    if (q && !w.searchName.includes(q)) return false;
    if (inc.length) {
      const has = id => w.spSet.has(id) && recentEnough(w, id, minYear);
      if (reqMode === "all") { for (const id of inc) if (!has(id)) return false; }
      else if (!inc.some(has)) return false;
    }
    for (const id of exc) if (w.spSet.has(id) && recentEnough(w, id, minYear)) return false;
    return true;
  });

  filtered.sort((a, b) => {
    let va, vb;
    if (sortKey === "size") { va = a.sizeNum; vb = b.sizeNum; }
    else if (sortKey === "spn") { va = a.spn; vb = b.spn; }
    else if (sortKey === "region") { va = REGION_NAMES[a.region] || ""; vb = REGION_NAMES[b.region] || ""; }
    else if (sortKey === "name") { va = a.sortName; vb = b.sortName; }
    else { va = a[sortKey] ?? ""; vb = b[sortKey] ?? ""; }
    if (va < vb) return -sortDir;
    if (va > vb) return sortDir;
    return a.sortName < b.sortName ? -1 : 1;
  });
}

// ---------- table ----------
const tbody = $("#tbl tbody");

// "Cutthroat Trout" -> "Cutthroat Trout ('14)", or plain if no year on file
function speciesLabel(w, id) {
  const name = speciesById[id] || id;
  const yr = w.spyMap.get(id);
  return yr ? `${name} ('${String(yr).slice(-2)})` : name;
}

function speciesCell(w) {
  const ids = [...w.sp].sort((a, b) => (speciesById[a] || "").localeCompare(speciesById[b] || ""));
  const shown = ids.slice(0, 6).map(id => {
    const label = speciesLabel(w, id);
    return /Trout|Salmon|Steelhead|Grayling|Whitefish|Kokanee|Char/i.test(speciesById[id] || "")
      ? `<b>${label}</b>` : label;
  });
  const more = ids.length > 6 ? ` <i>+${ids.length - 6} more</i>` : "";
  return `<span class="sptags">${shown.join(", ")}${more}</span>`;
}

function renderTable() {
  const rows = filtered.slice(0, ROW_CAP);
  tbody.innerHTML = rows.map(w => `
    <tr data-id="${w.id}">
      <td><span class="nm">${w.name}</span>${w.var ? ` <span class="var">(${w.var})</span>` : ""}
        · <a href="${WATER_URL(w.id)}" target="_blank" rel="noopener" title="open IDFG page">IDFG ↗</a></td>
      <td>${w.county}</td>
      <td>${REGION_NAMES[w.region] || ""}</td>
      <td>${w.trib}</td>
      <td>${w.layer === 1 ? "stream" : "lake"}</td>
      <td>${w.size ? w.size + (w.layer === 1 ? " mi" : " ac") : ""}</td>
      <td>${speciesCell(w)}</td>
    </tr>`).join("");
  const capNote = filtered.length > ROW_CAP ? ` (showing first ${ROW_CAP})` : "";
  $("#count").textContent = `${filtered.length.toLocaleString()} waters${capNote}`;
}

function selectRow(id, scroll) {
  for (const r of tbody.querySelectorAll("tr.sel")) r.classList.remove("sel");
  const tr = tbody.querySelector(`tr[data-id="${id}"]`);
  if (!tr) return;
  tr.classList.add("sel");
  if (scroll) tr.scrollIntoView({ block: "center", behavior: "smooth" });
}

tbody.addEventListener("click", e => {
  const tr = e.target.closest("tr");
  if (!tr || e.target.tagName === "A") return;
  selectRow(tr.dataset.id, false);
  highlightWater(tr.dataset.id);
});

document.querySelector("#tbl thead").addEventListener("click", e => {
  const th = e.target.closest("th");
  if (!th) return;
  const k = th.dataset.k;
  if (sortKey === k) sortDir = -sortDir; else { sortKey = k; sortDir = 1; }
  for (const t of th.parentNode.children) t.querySelector(".dir").textContent = "";
  th.querySelector(".dir").textContent = sortDir === 1 ? "▲" : "▼";
  refresh();
});

// ---------- basemaps ----------
const ESRI = "https://server.arcgisonline.com/ArcGIS/rest/services";
const BASEMAP_DEFS = {   // name -> {urls: [base, optional labels layer], maxNative, subdomains?}
  "Topo": { urls: [`${ESRI}/World_Topo_Map/MapServer/tile/{z}/{y}/{x}`], maxNative: 17 },
  "OpenTopoMap (trails)": { urls: ["https://{s}.tile.opentopomap.org/{z}/{x}/{y}.png"],
                            maxNative: 17, subdomains: "abc",
                            attribution: "OpenTopoMap (CC-BY-SA) | map data OSM" },
  "USA Topo (USGS)": { urls: [`${ESRI}/USA_Topo_Maps/MapServer/tile/{z}/{y}/{x}`], maxNative: 15 },
  "National Geographic": { urls: [`${ESRI}/NatGeo_World_Map/MapServer/tile/{z}/{y}/{x}`], maxNative: 16 },
  "Streets": { urls: [`${ESRI}/World_Street_Map/MapServer/tile/{z}/{y}/{x}`], maxNative: 17 },
  "Satellite": { urls: [`${ESRI}/World_Imagery/MapServer/tile/{z}/{y}/{x}`,
                        `${ESRI}/Reference/World_Boundaries_and_Places/MapServer/tile/{z}/{y}/{x}`], maxNative: 17 },
  "Light Gray": { urls: [`${ESRI}/Canvas/World_Light_Gray_Base/MapServer/tile/{z}/{y}/{x}`,
                         `${ESRI}/Canvas/World_Light_Gray_Reference/MapServer/tile/{z}/{y}/{x}`], maxNative: 16 },
  "Dark Gray": { urls: [`${ESRI}/Canvas/World_Dark_Gray_Base/MapServer/tile/{z}/{y}/{x}`,
                        `${ESRI}/Canvas/World_Dark_Gray_Reference/MapServer/tile/{z}/{y}/{x}`], maxNative: 16 },
};
// Personal, gitignored (see config.local.example.js) — absent for anyone else who clones this repo,
// in which case the CalTopo option just doesn't appear and everything else works as normal.
if (window.CALTOPO_CONFIG?.wmtsUrl) {
  BASEMAP_DEFS["CalTopo"] = {
    urls: [window.CALTOPO_CONFIG.wmtsUrl],
    maxNative: window.CALTOPO_CONFIG.maxNativeZoom || 16,
    attribution: window.CALTOPO_CONFIG.attribution || "CalTopo",
  };
}
if (window.MAPBOX_CONFIG?.token) {
  BASEMAP_DEFS["Mapbox Outdoors"] = {
    urls: [`https://api.mapbox.com/styles/v1/mapbox/outdoors-v12/tiles/256/{z}/{x}/{y}` +
           `?access_token=${window.MAPBOX_CONFIG.token}`],
    maxNative: 18,
    attribution: '© <a href="https://www.mapbox.com/about/maps/">Mapbox</a> ' +
                 '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
  };
}
const THEME_BASEMAP = { field: "Topo", brutal: "Light Gray", glass: "Dark Gray", retro: "Dark Gray" };
const baseGroups = {};
for (const [name, def] of Object.entries(BASEMAP_DEFS)) {
  baseGroups[name] = L.layerGroup(def.urls.map((url, i) => L.tileLayer(url, {
    maxNativeZoom: def.maxNative, maxZoom: 17,
    subdomains: def.subdomains || "abc",
    attribution: i === 0 ? (def.attribution || "Esri, USGS | IDFG hydrography") : "",
  })));
}
let currentBase = null;

function setBasemap(name) {
  if (currentBase === name) return;
  if (currentBase) map.removeLayer(baseGroups[currentBase]);
  baseGroups[name].addTo(map);
  currentBase = name;
}

// ---------- public land overlay (BLM Surface Management Agency) ----------
// tile pyramid, no API key; shows federal/state land ownership over any basemap.
// (built here, wired to the map + control further down once `map` exists)
const PUBLIC_LAND_URL = "https://gis.blm.gov/arcgis/rest/services/lands/" +
  "BLM_Natl_SMA_Cached_without_PriUnk/MapServer/tile/{z}/{y}/{x}";
const publicLandLayer = L.tileLayer(PUBLIC_LAND_URL, {
  maxNativeZoom: 15, maxZoom: 17, opacity: 0.55,
  attribution: "BLM Surface Management Agency",
});
const overlayGroups = { "Public land (BLM/USFS/NPS/State…)": publicLandLayer };
const MAP_COLORS = {   // sel = clicked highlight; stream/lake = "Map results" colors
  field: { sel: "#e2712e", stream: "#1e6fb8", lake: "#1a8f9c" },
  brutal: { sel: "#ef476f", stream: "#0057ff", lake: "#1a936f" },
  glass: { sel: "#fbbf24", stream: "#7dd3fc", lake: "#34d399" },
  retro: { sel: "#ff2ec4", stream: "#00e5ff", lake: "#b388ff" },
};
const mapColors = () => MAP_COLORS[document.documentElement.dataset.theme] || MAP_COLORS.field;

function setTheme(theme) {
  document.documentElement.dataset.theme = theme;
  localStorage.setItem("isf-theme", theme);
  for (const b of document.getElementById("themepick").children)
    b.classList.toggle("on", b.dataset.t === theme);
  setBasemap(THEME_BASEMAP[theme] || "Topo");
  const c = mapColors();
  selLayer.eachLayer(gl => gl.setStyle({ color: c.sel }));
  resultsLayer.eachLayer(gl => gl.setStyle({ color: gl._hydroLayer === 1 ? c.stream : c.lake }));
}

document.getElementById("themepick").addEventListener("click", e => {
  const btn = e.target.closest("button[data-t]");
  if (btn) setTheme(btn.dataset.t);
});

// brutal colorway sub-picker (dots only visible while the brutal theme is active)
const brutalPick = document.getElementById("brutalpick");
function setBrutalColor(c) {
  document.documentElement.dataset.brutal = c;
  localStorage.setItem("isf-brutal", c);
  for (const b of brutalPick.children) b.classList.toggle("on", b.dataset.c === c);
}
brutalPick.addEventListener("click", e => {
  const b = e.target.closest("button[data-c]");
  if (b) setBrutalColor(b.dataset.c);
});
setBrutalColor(localStorage.getItem("isf-brutal") || "pine");

// ---------- sidebar collapse ----------
const sideTab = document.getElementById("sidetab");
function setSidebar(collapsed) {
  document.getElementById("layout").classList.toggle("collapsed", collapsed);
  localStorage.setItem("isf-sidebar", collapsed ? "1" : "0");
  sideTab.textContent = collapsed ? "»" : "«";
  sideTab.title = collapsed ? "show filters" : "hide filters";
  setTimeout(() => map.invalidateSize(), 200);   // map column width changed (after tab transition)
}
sideTab.onclick = () =>
  setSidebar(!document.getElementById("layout").classList.contains("collapsed"));
document.getElementById("rail").onclick = () => setSidebar(false);
if (localStorage.getItem("isf-sidebar") === "1") {
  document.getElementById("layout").classList.add("collapsed");
  sideTab.textContent = "»";
  sideTab.title = "show filters";
}

// ---------- expand map ----------
const mapTab = document.getElementById("maptab");
function setMapBig(big) {
  document.getElementById("main").classList.toggle("mapbig", big);
  localStorage.setItem("isf-mapbig", big ? "1" : "0");
  mapTab.textContent = big ? "⌃" : "⌄";
  mapTab.title = big ? "shrink map" : "expand map";
  setTimeout(() => map.invalidateSize(), 60);
}
mapTab.onclick = () =>
  setMapBig(!document.getElementById("main").classList.contains("mapbig"));
if (localStorage.getItem("isf-mapbig") === "1") {
  document.getElementById("main").classList.add("mapbig");
  mapTab.textContent = "⌃";
  mapTab.title = "shrink map";
}

// ---------- map ----------
const map = L.map("map", { preferCanvas: true }).setView([45.3, -114.2], 7);
const resultsLayer = L.featureGroup().addTo(map);
const selLayer = L.featureGroup().addTo(map);
L.control.layers(baseGroups, overlayGroups, { position: "topright" }).addTo(map);
const landLegend = document.getElementById("landlegend");
map.on("overlayadd", e => { if (e.layer === publicLandLayer) landLegend.style.display = "flex"; });
map.on("overlayremove", e => { if (e.layer === publicLandLayer) landLegend.style.display = "none"; });
map.on("baselayerchange", e => {   // user picked from the layers control
  currentBase = Object.keys(baseGroups).find(n => baseGroups[n] === e.layer) || currentBase;
});
setTheme(localStorage.getItem("isf-theme") || "field");
const geomCache = new Map();   // llid -> geojson feature collection
const mapstatus = $("#mapstatus");

async function fetchGeom(ids, layer) {
  const missing = ids.filter(id => !geomCache.has(id));
  for (let i = 0; i < missing.length; i += 50) {
    const chunk = missing.slice(i, i + 50);
    const where = `LLID IN (${chunk.map(id => `'${id}'`).join(",")})`;
    const url = `${HYDRO}/${layer}/query?` + new URLSearchParams({
      where, outFields: "LLID,NAME", f: "geojson", outSR: "4326",
    });
    const gj = await (await fetch(url)).json();
    const byId = {};
    for (const f of gj.features || []) (byId[f.properties.LLID] ??= []).push(f);
    for (const id of chunk) geomCache.set(id, byId[id] || []);
  }
  return ids.flatMap(id => geomCache.get(id) || []);
}

function waterById(id) { return DATA.waters.find(w => w.id === id); }

function popupHtml(w) {
  const ids = [...w.sp].sort((a, b) => (speciesById[a] || "").localeCompare(speciesById[b] || ""));
  const names = ids.map(id => speciesLabel(w, id)).join(", ");
  return `<b>${w.name}</b>${w.var ? ` (${w.var})` : ""}<br>${w.trib || ""}<br>
    <small>${names || "no survey records"}</small><br>
    <a href="${WATER_URL(w.id)}" target="_blank" rel="noopener">IDFG water page ↗</a>`;
}

async function highlightWater(id, zoom = true) {
  const w = waterById(id);
  if (!w) return;
  mapstatus.textContent = `loading ${w.name}…`;
  try {
    const feats = await fetchGeom([id], w.layer === 1 ? 1 : 0);
    selLayer.clearLayers();
    if (!feats.length) { mapstatus.textContent = `no geometry found for ${w.name}`; return; }
    const gl = L.geoJSON({ type: "FeatureCollection", features: feats }, {
      style: { color: mapColors().sel, weight: 4, fillOpacity: 0.25 },
    }).bindPopup(popupHtml(w));
    selLayer.addLayer(gl);
    if (zoom) map.fitBounds(gl.getBounds().pad(0.3));
    gl.openPopup();
    mapstatus.textContent = "";
  } catch (err) {
    mapstatus.textContent = `geometry request failed (${err.message})`;
  }
}

$("#mapall").onclick = async () => {
  const subset = filtered.slice(0, MAP_CAP);
  if (!subset.length) return;
  mapstatus.textContent = `loading outlines for ${subset.length} waters…`;
  $("#mapall").disabled = true;
  try {
    resultsLayer.clearLayers();
    const byWater = new Map(subset.map(w => [w.id, w]));
    for (const layer of [1, 0]) {
      const ids = subset.filter(w => (w.layer === 1 ? 1 : 0) === layer).map(w => w.id);
      if (!ids.length) continue;
      const feats = await fetchGeom(ids, layer);
      const gl = L.geoJSON({ type: "FeatureCollection", features: feats }, {
        style: { color: layer === 1 ? mapColors().stream : mapColors().lake, weight: 2.5, fillOpacity: 0.3 },
        onEachFeature: (f, lyr) => {
          const w = byWater.get(f.properties.LLID);
          if (!w) return;
          lyr.bindPopup(popupHtml(w));
          lyr.on("click", () => {   // clicking a mapped water selects it like a table row
            selectRow(w.id, true);
            highlightWater(w.id, false);
          });
        },
      });
      gl._hydroLayer = layer;
      resultsLayer.addLayer(gl);
    }
    if (resultsLayer.getLayers().length) map.fitBounds(resultsLayer.getBounds().pad(0.1));
    mapstatus.textContent = filtered.length > MAP_CAP
      ? `mapped first ${MAP_CAP} of ${filtered.length.toLocaleString()} (narrow the filters for the rest)` : "";
  } catch (err) {
    mapstatus.textContent = `geometry request failed (${err.message})`;
  } finally {
    $("#mapall").disabled = false;
  }
};

$("#clearmap").onclick = () => { resultsLayer.clearLayers(); selLayer.clearLayers(); mapstatus.textContent = ""; };

// ---------- csv ----------
$("#exportcsv").onclick = () => {
  const esc = v => `"${String(v ?? "").replace(/"/g, '""')}"`;
  const lines = [["name", "variant", "county", "region", "drainage", "type", "size", "species_observed", "url"].join(",")];
  for (const w of filtered) {
    lines.push([w.name, w.var || "", w.county, REGION_NAMES[w.region] || "", w.trib,
      w.layer === 1 ? "stream" : "lake", w.size,
      [...w.sp].sort((a, b) => (speciesById[a] || "").localeCompare(speciesById[b] || ""))
        .map(id => speciesLabel(w, id)).join("; "), WATER_URL(w.id)].map(esc).join(","));
  }
  const blob = new Blob([lines.join("\r\n")], { type: "text/csv" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = "idaho-waters.csv";
  a.click();
  URL.revokeObjectURL(a.href);
};

// ---------- gpx (import into Gaia/onX/CalTopo/etc. as tracks) ----------
// Exports whatever's currently drawn on the map (highlighted water + any
// "Map results" set) — not the whole filtered table, just what's visible —
// using the same geometry already fetched from IDFG's hydrography service.
function currentMapFeatures() {
  const byLLID = new Map();
  for (const layer of [resultsLayer, selLayer]) {
    for (const f of layer.toGeoJSON().features) byLLID.set(f.properties.LLID, f);
  }
  return [...byLLID.values()];
}

function featureLines(geom) {
  if (geom.type === "LineString") return [geom.coordinates];
  if (geom.type === "MultiLineString") return geom.coordinates;
  if (geom.type === "Polygon") return geom.coordinates;       // lake ring(s)
  if (geom.type === "MultiPolygon") return geom.coordinates.flat();
  return [];
}

const GPX_HEADER = '<?xml version="1.0" encoding="UTF-8"?>\n' +
  '<gpx version="1.1" creator="Idaho Stream Finder" xmlns="http://www.topografix.com/GPX/1/1">\n';
const GPX_FOOTER = "</gpx>\n";
const byteLen = s => new Blob([s]).size;

// onX's own support docs: a single unusually large/dense track can fail an
// import independently of overall file size, and their fix is to split that
// track into smaller segments — so a long river gets cut into numbered parts
// rather than shipped as one giant <trk>, regardless of total file size.
const MAX_POINTS_PER_TRACK = 1500;

function trackXMLChunks(f) {
  const esc = s => String(s ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const w = waterById(f.properties.LLID);
  const baseName = w ? w.name + (w.var ? ` (${w.var})` : "") : (f.properties.NAME || f.properties.LLID);

  // Each entry from featureLines() is a genuinely separate physical line (a
  // real gap in the reach — disjoint segments of the same named stream).
  // Never merge two different lines into one track: onX was observed drawing
  // a straight "chord" across a <trkseg> break instead of treating it as a
  // real gap, so a new <trk> per physical line is the only reliably safe
  // boundary. A single line longer than the cap still splits mid-line —
  // those pieces stay genuinely adjacent, so no false connection there.
  const pieces = [];
  for (const line of featureLines(f.geometry)) {
    if (line.length > MAX_POINTS_PER_TRACK) {
      for (let i = 0; i < line.length; i += MAX_POINTS_PER_TRACK) pieces.push(line.slice(i, i + MAX_POINTS_PER_TRACK));
    } else {
      pieces.push(line);
    }
  }

  return pieces.map((line, i) => {
    const name = pieces.length > 1 ? `${baseName} (${i + 1}/${pieces.length})` : baseName;
    const out = [`  <trk><name>${esc(name)}</name>`, "    <trkseg>"];
    for (const [lon, lat] of line) out.push(`      <trkpt lat="${lat}" lon="${lon}"></trkpt>`);
    out.push("    </trkseg>", "  </trk>");
    return out.join("\n") + "\n";
  });
}

// Simple mode (Gaia, CalTopo, etc.): exactly one <trk> per water, no matter
// how big — a <trkseg> per genuinely disconnected line (correct per the GPX
// spec), full point count, never split. No onX-specific workarounds applied.
function simpleTrackXML(f) {
  const esc = s => String(s ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const w = waterById(f.properties.LLID);
  const name = w ? w.name + (w.var ? ` (${w.var})` : "") : (f.properties.NAME || f.properties.LLID);
  const out = [`  <trk><name>${esc(name)}</name>`];
  for (const line of featureLines(f.geometry)) {
    out.push("    <trkseg>");
    for (const [lon, lat] of line) out.push(`      <trkpt lat="${lat}" lon="${lon}"></trkpt>`);
    out.push("    </trkseg>");
  }
  out.push("  </trk>");
  return out.join("\n") + "\n";
}

// onX rejects GPX files over ~4MB (and caps at 3,000 tracks/markups per file);
// this budget stays well under both, plus the per-track point cap above. Also
// used as a general-purpose file-size safety net in simple/Gaia mode — a
// single giant file can fail an app's *save/upload* step even when its
// lightweight preview parser handled it fine (observed: a 13MB one-file
// export previewed correctly in Gaia but silently saved 0 tracks). Splitting
// into multiple files is unrelated to whether any one track gets split, so
// it applies in both modes — only trackXMLChunks vs. simpleTrackXML changes.
const GPX_MAX_BYTES = 2 * 1024 * 1024;

// Packs pre-built <trk>...</trk> XML strings into files, each staying under
// GPX_MAX_BYTES. A track is never split mid-way here — a single track larger
// than the budget still gets its own (over-budget) file rather than being cut.
function packIntoFiles(trackXMLs) {
  const budget = GPX_MAX_BYTES - byteLen(GPX_HEADER) - byteLen(GPX_FOOTER);
  const files = [];
  let body = "", bodyBytes = 0;
  for (const xml of trackXMLs) {
    const xmlBytes = byteLen(xml);
    if (body && bodyBytes + xmlBytes > budget) {
      files.push(GPX_HEADER + body + GPX_FOOTER);
      body = ""; bodyBytes = 0;
    }
    body += xml;
    bodyBytes += xmlBytes;
  }
  if (body) files.push(GPX_HEADER + body + GPX_FOOTER);
  return files;
}

function buildGPXFiles(features, onxSafe) {
  const trackXMLs = onxSafe ? features.flatMap(trackXMLChunks) : features.map(simpleTrackXML);
  return packIntoFiles(trackXMLs);
}

function downloadText(text, filename, mime) {
  const blob = new Blob([text], { type: mime });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = filename;
  a.click();
  URL.revokeObjectURL(a.href);
}

$("#exportgpx").onclick = () => {
  const features = currentMapFeatures();
  if (!features.length) {
    mapstatus.textContent = "nothing mapped yet — click a row or \"Map results\" first";
    return;
  }
  const files = buildGPXFiles(features, $("#gpx-onx-safe").checked);
  if (files.length === 1) {
    downloadText(files[0], "idaho-streams.gpx", "application/gpx+xml");
    mapstatus.textContent = "";
    return;
  }
  // onX's ~4MB-per-file limit means a big export needs multiple files; stagger
  // the downloads slightly so the browser doesn't treat them as download spam
  mapstatus.textContent = `splitting into ${files.length} files (onX/Gaia size limits)…`;
  files.forEach((content, i) => {
    setTimeout(() => {
      downloadText(content, `idaho-streams-part${i + 1}-of-${files.length}.gpx`, "application/gpx+xml");
      if (i === files.length - 1) {
        mapstatus.textContent = `saved ${files.length} files — import each into onX/Gaia separately`;
      }
    }, i * 400);
  });
};

// ---------- filter state: capture / apply / url hash / presets ----------
function captureState() {
  const inc = [], exc = [];
  for (const [id, st] of spState) (st === "inc" ? inc : exc).push(id);
  return {
    inc, exc, mode: reqMode,
    stream: $("#t-stream").checked, lake: $("#t-lake").checked,
    region: regionSel.value, county: countySel.value,
    q: $("#namesearch").value.trim(), surveyed: $("#surveyed").checked,
    minYear: val("#minyear").trim(),
  };
}

function applyState(s) {
  spState.clear();
  for (const id of s.inc || []) spState.set(+id, "inc");
  for (const id of s.exc || []) spState.set(+id, "exc");
  reqMode = s.mode === "all" ? "all" : "any";
  for (const b of $("#reqmode").children) b.classList.toggle("on", b.dataset.v === reqMode);
  $("#t-stream").checked = s.stream !== false;
  $("#t-lake").checked = s.lake !== false;
  regionSel.value = s.region || "";
  countySel.value = s.county || "";
  $("#namesearch").value = s.q || "";
  $("#surveyed").checked = s.surveyed !== false;
  setVal("#minyear", s.minYear || "");
  refresh();
}

function updateHash() {
  const s = captureState();
  const p = new URLSearchParams();
  if (s.inc.length) p.set("inc", s.inc.join("."));
  if (s.exc.length) p.set("exc", s.exc.join("."));
  if (s.mode === "all") p.set("mode", "all");
  if (!s.stream) p.set("nostream", "1");
  if (!s.lake) p.set("nolake", "1");
  if (s.region) p.set("region", s.region);
  if (s.county) p.set("county", s.county);
  if (s.q) p.set("q", s.q);
  if (!s.surveyed) p.set("all-waters", "1");
  if (s.minYear) p.set("since", s.minYear);
  const h = p.toString();
  history.replaceState(null, "", h ? "#" + h : location.pathname + location.search);
}

function parseHash() {
  if (location.hash.length < 2) return null;
  const p = new URLSearchParams(location.hash.slice(1));
  if (![...p.keys()].length) return null;
  return {
    inc: (p.get("inc") || "").split(".").filter(Boolean).map(Number),
    exc: (p.get("exc") || "").split(".").filter(Boolean).map(Number),
    mode: p.get("mode") || "any",
    stream: !p.get("nostream"), lake: !p.get("nolake"),
    region: p.get("region") || "", county: p.get("county") || "",
    q: p.get("q") || "", surveyed: !p.get("all-waters"),
    minYear: p.get("since") || "",
  };
}

const presetSel = $("#presets");
const loadPresets = () => JSON.parse(localStorage.getItem("isf-presets") || "{}");

function renderPresets(selected) {
  const names = Object.keys(loadPresets()).sort();
  presetSel.innerHTML = `<option value="">Saved filters…</option>` +
    names.map(n => `<option${n === selected ? " selected" : ""}>${n.replace(/</g, "&lt;")}</option>`).join("");
}

$("#savepreset").onclick = () => {
  const name = (prompt("Name this filter set:") || "").trim();
  if (!name) return;
  const p = loadPresets();
  p[name] = captureState();
  localStorage.setItem("isf-presets", JSON.stringify(p));
  renderPresets(name);
};

$("#delpreset").onclick = () => {
  const name = presetSel.value;
  if (!name || !confirm(`Delete preset "${name}"?`)) return;
  const p = loadPresets();
  delete p[name];
  localStorage.setItem("isf-presets", JSON.stringify(p));
  renderPresets("");
};

presetSel.addEventListener("change", () => {
  const p = loadPresets();
  if (presetSel.value && p[presetSel.value]) applyState(p[presetSel.value]);
});

// ---------- go ----------
function refresh() {
  paintSpecies();
  filterSpeciesList();
  applyFilters();
  renderTable();
  updateHash();
}
renderPresets("");
const fromUrl = parseHash();
if (fromUrl) applyState(fromUrl); else refresh();
