// Public Mapbox token, safe to commit — restricted in the Mapbox dashboard to
// only work from idahostreams.robromo.dev (Account -> Tokens -> URL
// restrictions). Free tier: 200,000 tile requests/month, no card required.
// CalTopo's WMTS URL stays out of here (in gitignored config.local.js
// instead) since it isn't a domain-restrictable credential.
window.MAPBOX_CONFIG = {
  token: "pk.eyJ1Ijoicm9icm9tbzEwMjMiLCJhIjoiY21yd2M4Y2lqMDRrdjJ3cHNlNXBmN2t4ZyJ9.6E41CIBCnDWvbrTZw92JEA",
};
