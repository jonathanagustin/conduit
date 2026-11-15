// Static public-domain basemap plus operator-declared Environment locations.
// No geocoding, browser location, remote tiles, inferred sites or sample pins.
import borders from "./assets/world-borders-110m.json";
import { forwardOrthographic } from "./globe-texture.js";

export const LOCATION_LABELS = {
  latitude: "conduit.latitude",
  longitude: "conduit.longitude",
};

function decimal(value, minimum, maximum) {
  if (
    typeof value !== "string" ||
    !/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/.test(value.trim())
  )
    return null;
  const number = Number(value);
  return Number.isFinite(number) && number >= minimum && number <= maximum
    ? number
    : null;
}

export function recordedSites(environments) {
  return (Array.isArray(environments) ? environments : []).flatMap((row) => {
    const environment = row?.environment;
    if (typeof environment?.id !== "string" || !environment.id) return [];
    const labels = environment.labels || {};
    const latitude = decimal(labels[LOCATION_LABELS.latitude], -90, 90);
    const longitude = decimal(labels[LOCATION_LABELS.longitude], -180, 180);
    return [
      {
        id: environment.id,
        state: row.spoke?.state || "unknown",
        row,
        location:
          latitude === null || longitude === null
            ? null
            : [longitude, latitude],
      },
    ];
  });
}

function path(ring, rotation) {
  let result = "",
    open = false;
  for (const [longitude, latitude] of ring) {
    const point = forwardOrthographic(longitude, latitude, rotation);
    if (!point) {
      open = false;
      continue;
    }
    result += `${open ? "L" : "M"}${(180 + point[0] * 146).toFixed(2)},${(160 + point[1] * 146).toFixed(2)}`;
    open = true;
  }
  return result;
}

const toggle = (name, enabled) =>
  `<button type="button" class="so-btn so-btn-sm" data-cc-layer="${name.toLowerCase()}" aria-pressed="${enabled}">${name}</button>`;

export function geographyMarkup(hub, ui, esc) {
  const sites = recordedSites(hub?.environments);
  const located = sites.filter((site) => site.location);
  const rotation =
    ui.rotation ||
    (located[0]
      ? [-located[0].location[0], -located[0].location[1], 0]
      : [0, 0, 0]);
  const layers = ui.layers;
  const outlines = layers.borders
    ? borders.map((ring) => path(ring, rotation)).join("")
    : "";
  const pins = located
    .map((site) => {
      const point = forwardOrthographic(...site.location, rotation);
      if (!point) return "";
      const x = 180 + point[0] * 146,
        y = 160 + point[1] * 146;
      const tone = ["live", "stale", "unreachable"].includes(site.state)
        ? site.state
        : "unknown";
      return `<g role="button" tabindex="0" data-cc-site="${esc(site.id)}" aria-label="Inspect environment ${esc(site.id)}" class="kit-cc-pin kit-cc-pin-${tone}"><circle cx="${x}" cy="${y}" r="5"/>${layers.labels ? `<text x="${x + 9}" y="${y + 3}">${esc(site.id)}</text>` : ""}</g>`;
    })
    .join("");
  const selected = sites.find((site) => site.id === ui.selected);
  return `<section class="so-panel kit-view-panel kit-cc-geography">
    <div class="kit-view-head"><h2>Sites and connectivity</h2><span class="kit-count">${hub?.note ? "Unavailable" : `${sites.length} recorded`}</span></div>
    <div class="kit-cc-layers" role="group" aria-label="Globe layers">${Object.entries(
      layers,
    )
      .map(([name, enabled]) =>
        toggle(name[0].toUpperCase() + name.slice(1), enabled),
      )
      .join("")}</div>
    <div class="kit-cc-map-layout">
      <div><svg class="kit-cc-globe" viewBox="0 0 360 320" role="group" aria-label="Declared environment locations">
        <circle class="kit-cc-sphere${layers.globe ? " is-filled" : ""}" cx="180" cy="160" r="146"/>
        ${outlines ? `<path class="kit-cc-borders" d="${outlines}"/>` : ""}${pins}
      </svg><p class="kit-muted">${hub?.note ? esc(hub.note) : `${located.length} with declared coordinates · ${sites.length - located.length} without a mapped location`}</p>
      ${layers.links ? '<p class="kit-muted">Geographic link endpoints are unavailable. No network routes are inferred.</p>' : ""}</div>
      <div><ul class="kit-cc-list kit-cc-sites">${sites.map((site) => `<li><button type="button" class="so-btn so-btn-sm" data-cc-site="${esc(site.id)}">${esc(site.id)}</button> <span>${esc(site.state)}</span><span class="kit-muted">${site.location ? "Declared location" : "Location unavailable"}</span></li>`).join("") || '<li class="kit-muted">No recorded environments to plot.</li>'}</ul>
      ${selected ? `<section role="dialog" aria-label="Environment ${esc(selected.id)}" class="kit-cc-spatial-object"><div class="kit-view-head"><h3>${esc(selected.id)}</h3><button type="button" class="so-btn so-btn-sm" data-cc-close aria-label="Close environment details">Close</button></div><p>Recorded spoke state: ${esc(selected.state)}</p><p class="kit-muted">${selected.location ? `Declared coordinates: ${selected.location[1]}, ${selected.location[0]}` : "No valid coordinates are recorded for this environment."}</p><div class="kit-cc-actions">${["Ask", "COA", "Share in chat"].map((label) => `<button type="button" class="so-btn so-btn-sm" data-cc-context="${label === "COA" ? "coa" : label === "Ask" ? "ask" : "share"}" data-cc-environment="${esc(selected.id)}">${label}</button>`).join("")}<button type="button" class="so-btn so-btn-sm" data-cc-inspect="${esc(selected.id)}">Inspect</button></div></section>` : ""}</div>
    </div>
  </section>`;
}
