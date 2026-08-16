// Personal, machine-local config — NOT loaded from the public repo.
//
// Copy this file to config.local.js (already gitignored) and fill in your own
// keys/URLs below. Never commit config.local.js — it contains personal API keys.
// Anything left blank is simply skipped — the site works fine either way.

// ---- CalTopo Pro/Desktop (needs the Desktop tier for WMTS/WMS access) ----
// How to get the URL:
//   1. caltopo.com -> click your account name (top-left) -> "Your Account"
//   2. Find the "API Access" section -> copy the WMTS endpoint URL
//   3. CalTopo's URL likely uses WMTS-standard placeholder tokens like
//      {TileMatrix}/{TileRow}/{TileCol} — Leaflet needs {z}/{y}/{x} instead,
//      so rename those three tokens (case-sensitive) if present.
//   4. Pick which CalTopo layer you want (e.g. their blended topo /
//      "MapBuilder Topo") — the layer is usually baked into the URL path
//      CalTopo gives you, so just paste it as-is once the placeholders match.
window.CALTOPO_CONFIG = {
  wmtsUrl: "",              // e.g. "https://caltopo.com/wmts/xxxxxxxx/xxxxx/{z}/{x}/{y}.png"
  maxNativeZoom: 16,
  attribution: "CalTopo",
};

// Mapbox lives in the committed config.js instead of here — its token is
// domain-restricted (Account -> Tokens -> URL restrictions) so it's safe to
// publish, unlike the CalTopo WMTS URL above.
