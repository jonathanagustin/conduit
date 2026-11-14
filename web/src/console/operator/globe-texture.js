/*
 * Daytime globe texture for the light theme. The fleet globe is an
 * orthographic projection; this inverts it per pixel and samples an
 * equirectangular image, so the dark theme's drawn globe and this textured
 * one share the same rotation, pins and corridors.
 */
import blueMarbleDayUrl from "./assets/blue-marble-day-2048.webp";

const DEG = Math.PI / 180;

/**
 * Screen offset (unit-disc x right, y down) to [lng, lat] in degrees for the
 * globe's rotation [rotLng, rotLat]. Returns null outside the disc.
 */
export function inverseOrthographic(x, y, rot) {
  const rho2 = x * x + y * y;
  if (rho2 > 1) return null;
  const cosc = Math.sqrt(1 - rho2);
  const phi0 = -rot[1] * DEG;
  const up = -y;
  const lat = Math.asin(
    Math.max(-1, Math.min(1, cosc * Math.sin(phi0) + up * Math.cos(phi0))),
  );
  const lng =
    -rot[0] * DEG + Math.atan2(x, cosc * Math.cos(phi0) - up * Math.sin(phi0));
  let lngDeg = lng / DEG;
  lngDeg = ((((lngDeg + 180) % 360) + 360) % 360) - 180;
  return [lngDeg, lat / DEG];
}

/** Same projection the globe draws with, on a unit disc, y down. */
export function forwardOrthographic(lng, lat, rot) {
  const lam = (lng + rot[0]) * DEG;
  const phi = lat * DEG;
  const phi0 = -rot[1] * DEG;
  const cosc =
    Math.sin(phi0) * Math.sin(phi) +
    Math.cos(phi0) * Math.cos(phi) * Math.cos(lam);
  if (cosc < 0) return null;
  return [
    Math.cos(phi) * Math.sin(lam),
    Math.sin(phi0) * Math.cos(phi) * Math.cos(lam) -
      Math.cos(phi0) * Math.sin(phi),
  ];
}

/**
 * Fills an RGBA buffer of size*size with the textured disc. texture is
 * { data, width, height } in RGBA. The limb darkens slightly for depth.
 */
export function paintDisc(out, size, texture, rot) {
  const half = size / 2;
  const { data, width, height } = texture;
  for (let py = 0; py < size; py++) {
    const y = (py + 0.5 - half) / half;
    for (let px = 0; px < size; px++) {
      const x = (px + 0.5 - half) / half;
      const o = (py * size + px) * 4;
      const ll = inverseOrthographic(x, y, rot);
      if (!ll) {
        out[o + 3] = 0;
        continue;
      }
      const u = Math.min(width - 1, Math.floor(((ll[0] + 180) / 360) * width));
      const v = Math.min(height - 1, Math.floor(((90 - ll[1]) / 180) * height));
      const i = (v * width + u) * 4;
      const shade = 0.72 + 0.28 * Math.sqrt(Math.max(0, 1 - x * x - y * y));
      out[o] = data[i] * shade;
      out[o + 1] = data[i + 1] * shade;
      out[o + 2] = data[i + 2] * shade;
      out[o + 3] = 255;
    }
  }
}

let texture = null;
let loading = null;

/** The decoded day texture, or null until it has loaded; onReady fires once. */
export function dayTexture(onReady) {
  if (texture) return texture;
  if (!loading && typeof Image !== "undefined") {
    loading = new Promise((resolve) => {
      const img = new Image();
      img.decoding = "async";
      img.onload = () => {
        const c = document.createElement("canvas");
        c.width = img.naturalWidth;
        c.height = img.naturalHeight;
        const g = c.getContext("2d");
        if (!g) return resolve(null);
        g.drawImage(img, 0, 0);
        const d = g.getImageData(0, 0, c.width, c.height);
        texture = { data: d.data, width: d.width, height: d.height };
        resolve(texture);
      };
      img.onerror = () => resolve(null);
      img.src = blueMarbleDayUrl;
    });
  }
  if (loading && onReady) loading.then((t) => t && onReady());
  return null;
}
