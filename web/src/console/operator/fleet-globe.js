import { observeGlobe } from "./globe-observers.js";
import { dayTexture, paintDisc } from "./globe-texture.js";

function canvasPaint(value) {
  if (typeof value !== "string") return value;
  const style = getComputedStyle(
    document.querySelector(".operator-root") || document.documentElement,
  );
  return value.replace(/var\((--[a-z0-9-]+)\)/g, (_, name) =>
    style.getPropertyValue(name).trim(),
  );
}

/**
 * Interactive canvas globe controller for the Fleet Overview workspace.
 * @param {{ isDisposed: () => boolean, onSelectScreen: (screen: string) => void }} options
 */
export function createFleetGlobe({ isDisposed, onSelectScreen }) {
  let disposeGlobe = () => {};
  let conduitGlobeRot = [96, -37, 0];

  function initConduitGlobe() {
    if (isDisposed()) return;
    disposeGlobe();
    const wrap = document.getElementById("conduit-globe-wrap");
    const canvas = document.getElementById("conduit-globe-canvas");
    if (!wrap || !canvas) return;

    const width = wrap.clientWidth || 560;
    const height = wrap.clientHeight || 290;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    canvas.style.width = width + "px";
    canvas.style.height = height + "px";

    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);

    const DEG2RAD = Math.PI / 180;
    const baseR = Math.min(width, height) * 0.465;
    // Light theme draws the daytime Blue Marble; dark keeps the night globe.
    const light =
      document.querySelector(".operator-root")?.getAttribute("data-theme") ===
      "light";
    const day = light
      ? dayTexture(() => {
          if (document.getElementById("conduit-globe-canvas"))
            initConduitGlobe();
        })
      : null;

    function project(coord) {
      const lam = (coord[0] + conduitGlobeRot[0]) * DEG2RAD;
      const phi = coord[1] * DEG2RAD;
      const phi0 = -conduitGlobeRot[1] * DEG2RAD;
      const cosc =
        Math.sin(phi0) * Math.sin(phi) +
        Math.cos(phi0) * Math.cos(phi) * Math.cos(lam);
      if (cosc < 0) return null;
      return [
        baseR * Math.cos(phi) * Math.sin(lam) + width / 2,
        baseR *
          (Math.sin(phi0) * Math.cos(phi) * Math.cos(lam) -
            Math.cos(phi0) * Math.sin(phi)) +
          height / 2 +
          4,
      ];
    }

    function geoDist(a, b) {
      const l1 = a[0] * DEG2RAD,
        p1 = a[1] * DEG2RAD,
        l2 = b[0] * DEG2RAD,
        p2 = b[1] * DEG2RAD;
      const c =
        Math.sin(p1) * Math.sin(p2) +
        Math.cos(p1) * Math.cos(p2) * Math.cos(l2 - l1);
      return Math.acos(Math.max(-1, Math.min(1, c)));
    }

    const landPolys = [
      [
        [-168, 65],
        [-162, 60],
        [-152, 60],
        [-140, 60],
        [-130, 54],
        [-125, 48],
        [-124, 40],
        [-120, 34],
        [-117, 32],
        [-110, 24],
        [-105, 20],
        [-97, 16],
        [-88, 16],
        [-83, 10],
        [-79, 9],
        [-77, 8],
        [-82, 15],
        [-87, 21],
        [-90, 21],
        [-97, 22],
        [-97, 28],
        [-90, 29],
        [-83, 29],
        [-81, 25],
        [-80, 27],
        [-81, 31],
        [-76, 35],
        [-75, 39],
        [-71, 41],
        [-70, 43],
        [-67, 45],
        [-64, 44],
        [-60, 46],
        [-65, 49],
        [-56, 52],
        [-62, 58],
        [-68, 62],
        [-78, 62],
        [-82, 55],
        [-90, 57],
        [-95, 61],
        [-105, 68],
        [-125, 70],
        [-145, 70],
        [-162, 70],
        [-168, 65],
      ],
      [
        [-74, 62],
        [-60, 60],
        [-44, 60],
        [-22, 70],
        [-18, 78],
        [-42, 82],
        [-62, 80],
        [-74, 75],
        [-74, 62],
      ],
      [
        [-85, 22],
        [-79, 22],
        [-74, 20],
        [-77, 20],
        [-84, 21],
        [-85, 22],
      ],
      [
        [-78, 9],
        [-72, 12],
        [-62, 10],
        [-50, 1],
        [-35, -6],
        [-38, -15],
        [-43, -23],
        [-53, -33],
        [-58, -38],
        [-70, -18],
        [-77, -12],
        [-81, -5],
        [-78, 2],
        [-78, 9],
      ],
      [
        [-10, 36],
        [-9, 43],
        [-2, 44],
        [-4, 48],
        [2, 51],
        [8, 54],
        [8, 58],
        [18, 68],
        [28, 70],
        [40, 67],
        [40, 47],
        [28, 41],
        [15, 38],
        [5, 43],
        [-5, 36],
        [-10, 36],
      ],
    ];

    const fleetNodes = [
      {
        lng: -77.4,
        lat: 38.9,
        color: "var(--info-text)",
        r: 4.5,
        label: "edge-east-01 (Hub)",
        screen: "s14-env-detail",
        dx: 9,
        dy: 2,
      },
      {
        lng: -122.3,
        lat: 47.6,
        color: "var(--warn-text)",
        r: 4.5,
        label: "edge-west-02 · 3 drifts",
        screen: "s14-env-detail",
        dx: -8,
        dy: -7,
        rightAlign: true,
      },
      {
        lng: -104.8,
        lat: 38.8,
        color: "var(--ok-text)",
        r: 4.2,
        label: "offline-vault-04 (Diode)",
        screen: "s08-staged-run",
        dx: -8,
        dy: 12,
        rightAlign: true,
      },
      {
        lng: -90.5,
        lat: 28.2,
        color: "var(--err-text)",
        r: 4.0,
        label: "remote-rig-09",
        screen: "s17-spokes-audit",
        dx: 8,
        dy: 11,
      },
      {
        lng: -87.6,
        lat: 41.8,
        color: "var(--info-text)",
        r: 4.0,
        label: "metro-core-03",
        screen: "s17-spokes-audit",
        dx: -6,
        dy: -12,
        rightAlign: true,
      },
      {
        lng: -147.7,
        lat: 64.8,
        color: "var(--info-text)",
        r: 3.8,
        label: "forward-arctic-01",
        screen: "s05-backlog",
        dx: 8,
        dy: -4,
      },
      {
        lng: -117.2,
        lat: 32.7,
        color: "var(--info-text)",
        r: 3.4,
        label: "",
        screen: "s14-env-detail",
      },
      {
        lng: -95.4,
        lat: 29.7,
        color: "var(--info-text)",
        r: 3.4,
        label: "",
        screen: "s17-spokes-audit",
      },
      {
        lng: -71.1,
        lat: 42.3,
        color: "var(--info-text)",
        r: 3.4,
        label: "",
        screen: "s14-env-detail",
      },
      {
        lng: -80.2,
        lat: 25.8,
        color: "var(--ok-text)",
        r: 3.4,
        label: "",
        screen: "s08-staged-run",
      },
    ];

    const corridors = [
      [-77.4, 38.9, -122.3, 47.6, "var(--warn-text)"],
      [-77.4, 38.9, -104.8, 38.8, "var(--ok-text)"],
      [-77.4, 38.9, -90.5, 28.2, "var(--err-text)"],
      [-77.4, 38.9, -87.6, 41.8, "var(--info-text)"],
      [-122.3, 47.6, -147.7, 64.8, "var(--info-text)"],
    ];

    function draw() {
      const cx = width / 2,
        cy = height / 2 + 4;
      ctx.clearRect(0, 0, width, height);

      // 1. Atmospheric outer blue limb halo (f_01_18_cc.png)
      const halo = ctx.createRadialGradient(
        cx,
        cy,
        baseR * 0.86,
        cx,
        cy,
        baseR * 1.28,
      );
      halo.addColorStop(
        0,
        canvasPaint("color-mix(in srgb, var(--info-text) 36%, transparent)"),
      );
      halo.addColorStop(
        0.45,
        canvasPaint("color-mix(in srgb, var(--info-text) 15%, transparent)"),
      );
      halo.addColorStop(1, canvasPaint("transparent"));
      ctx.fillStyle = canvasPaint(halo);
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR * 1.28, baseR * 1.28, 0, 0, Math.PI * 2);
      ctx.fill();

      if (day) {
        drawDayDisc(cx, cy);
      } else {
        drawNightDisc(cx, cy);
      }

      // 5. Spoke relay great-circle links
      ctx.save();
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.clip();
      drawCorridors();
      ctx.restore();
      drawRimAndPins();
    }

    function drawDayDisc(cx, cy) {
      const size = Math.max(2, Math.round(baseR * 2 * dpr));
      const off = document.createElement("canvas");
      off.width = size;
      off.height = size;
      const g = off.getContext("2d");
      if (!g) return;
      const img = g.createImageData(size, size);
      paintDisc(img.data, size, day, conduitGlobeRot);
      g.putImageData(img, 0, 0);
      ctx.drawImage(off, cx - baseR, cy - baseR, baseR * 2, baseR * 2);
      ctx.save();
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.clip();
      drawGraticules(
        "color-mix(in srgb, var(--text-1) 14.000000000000002%, transparent)",
      );
      ctx.restore();
    }

    function drawGraticules(color) {
      ctx.strokeStyle = canvasPaint(color);
      ctx.lineWidth = 0.6;
      for (let lat = -45; lat <= 75; lat += 20) {
        ctx.beginPath();
        let st = false;
        for (let lng = -180; lng <= 180; lng += 5) {
          const p = project([lng, lat]);
          if (p) {
            if (!st) {
              ctx.moveTo(p[0], p[1]);
              st = true;
            } else ctx.lineTo(p[0], p[1]);
          } else st = false;
        }
        ctx.stroke();
      }
      for (let lng = -160; lng <= 40; lng += 20) {
        ctx.beginPath();
        let st = false;
        for (let lat = -60; lat <= 80; lat += 4) {
          const p = project([lng, lat]);
          if (p) {
            if (!st) {
              ctx.moveTo(p[0], p[1]);
              st = true;
            } else ctx.lineTo(p[0], p[1]);
          } else st = false;
        }
        ctx.stroke();
      }
    }

    function drawNightDisc(cx, cy) {
      // 2. Deep bathymetric ocean sphere
      const ocean = ctx.createRadialGradient(
        cx - baseR * 0.22,
        cy - baseR * 0.22,
        baseR * 0.08,
        cx,
        cy,
        baseR,
      );
      ocean.addColorStop(0, canvasPaint("var(--globe-ocean-core)"));
      ocean.addColorStop(0.62, canvasPaint("var(--globe-ocean-mid)"));
      ocean.addColorStop(1, canvasPaint("var(--globe-ocean-edge)"));
      ctx.fillStyle = canvasPaint(ocean);
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.fill();

      // Subtle graticules clipped inside sphere
      ctx.save();
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.clip();

      drawGraticules("color-mix(in srgb, var(--info-text) 8%, transparent)");

      // 3. Dark satellite continents
      ctx.fillStyle = canvasPaint("var(--globe-land-mask)");
      ctx.strokeStyle = canvasPaint(
        "color-mix(in srgb, var(--info-text) 28.000000000000004%, transparent)",
      );
      ctx.lineWidth = 0.85;
      landPolys.forEach((ring) => {
        ctx.beginPath();
        let st = false;
        ring.forEach((c) => {
          const p = project(c);
          if (p) {
            if (!st) {
              ctx.moveTo(p[0], p[1]);
              st = true;
            } else ctx.lineTo(p[0], p[1]);
          } else st = false;
        });
        ctx.fill();
        ctx.stroke();
      });

      // 4. Golden city-light clusters
      CONDUIT_NIGHT_LIGHTS.forEach((c) => {
        if (
          geoDist([c[0], c[1]], [-conduitGlobeRot[0], -conduitGlobeRot[1]]) <
          1.46
        ) {
          const pt = project([c[0], c[1]]);
          if (pt) {
            if (c[2] > 1.35) {
              const g = ctx.createRadialGradient(
                pt[0],
                pt[1],
                0,
                pt[0],
                pt[1],
                c[2] * 3.2,
              );
              g.addColorStop(
                0,
                canvasPaint(
                  "rgba(var(--globe-city-core-rgb), " + c[3].toFixed(2) + ")",
                ),
              );
              g.addColorStop(
                0.4,
                canvasPaint(
                  "rgba(var(--globe-city-edge-rgb), " +
                    (c[3] * 0.38).toFixed(2) +
                    ")",
                ),
              );
              g.addColorStop(1, canvasPaint("transparent"));
              ctx.fillStyle = canvasPaint(g);
              ctx.beginPath();
              ctx.ellipse(
                pt[0],
                pt[1],
                c[2] * 3.2,
                c[2] * 3.2,
                0,
                0,
                Math.PI * 2,
              );
              ctx.fill();
            } else {
              ctx.fillStyle = canvasPaint(
                "rgba(var(--globe-city-point-rgb), " + c[3].toFixed(2) + ")",
              );
              ctx.beginPath();
              ctx.ellipse(pt[0], pt[1], c[2], c[2], 0, 0, Math.PI * 2);
              ctx.fill();
            }
          }
        }
      });

      ctx.restore();
    }

    function drawCorridors() {
      corridors.forEach((cor) => {
        const pA = project([cor[0], cor[1]]);
        const pB = project([cor[2], cor[3]]);
        if (pA && pB) {
          const mx = (pA[0] + pB[0]) * 0.5;
          const my =
            (pA[1] + pB[1]) * 0.5 -
            Math.hypot(pB[0] - pA[0], pB[1] - pA[1]) * 0.22;
          ctx.save();
          ctx.strokeStyle = canvasPaint(cor[4]);
          ctx.globalAlpha = 0.55;
          ctx.lineWidth = 1.35;
          ctx.setLineDash([3, 3]);
          ctx.beginPath();
          ctx.moveTo(pA[0], pA[1]);
          ctx.quadraticCurveTo(mx, my, pB[0], pB[1]);
          ctx.stroke();
          ctx.restore();
        }
      });
    }

    function drawRimAndPins() {
      const cx = width / 2,
        cy = height / 2 + 4;
      // Crisp atmospheric rim outline
      ctx.strokeStyle = canvasPaint(
        "color-mix(in srgb, var(--info-text) 35%, transparent)",
      );
      ctx.lineWidth = 1.1;
      ctx.beginPath();
      ctx.ellipse(cx, cy, baseR, baseR, 0, 0, Math.PI * 2);
      ctx.stroke();

      // 6. Glowing Kubernetes Environment & Spoke Pins + Clean Callouts
      ctx.font = "500 10px Inter, sans-serif";
      fleetNodes.forEach((n) => {
        if (
          geoDist([n.lng, n.lat], [-conduitGlobeRot[0], -conduitGlobeRot[1]]) >
          1.48
        )
          return;
        const pt = project([n.lng, n.lat]);
        if (!pt) return;
        const glow = ctx.createRadialGradient(
          pt[0],
          pt[1],
          0,
          pt[0],
          pt[1],
          n.r * 3.2,
        );
        glow.addColorStop(0, canvasPaint(n.color));
        glow.addColorStop(1, canvasPaint("transparent"));
        ctx.fillStyle = canvasPaint(glow);
        ctx.beginPath();
        ctx.ellipse(pt[0], pt[1], n.r * 3.2, n.r * 3.2, 0, 0, Math.PI * 2);
        ctx.fill();

        ctx.fillStyle = canvasPaint(n.color);
        ctx.beginPath();
        ctx.ellipse(pt[0], pt[1], n.r, n.r, 0, 0, Math.PI * 2);
        ctx.fill();
        ctx.strokeStyle = canvasPaint("var(--bg-1)");
        ctx.lineWidth = 1.2;
        ctx.stroke();

        if (n.label && (width >= 420 || n.r >= 4.2)) {
          const tw = ctx.measureText(n.label).width;
          const rawLx = n.rightAlign ? pt[0] + n.dx - tw - 10 : pt[0] + n.dx;
          const lx = Math.max(6, Math.min(width - tw - 16, rawLx));
          const ly = Math.max(6, Math.min(height - 22, pt[1] + n.dy - 10));
          ctx.fillStyle = canvasPaint(
            "color-mix(in srgb, var(--bg-1) 86%, transparent)",
          );
          ctx.strokeStyle = canvasPaint(
            "color-mix(in srgb, var(--border-subtle) 85%, transparent)",
          );
          ctx.lineWidth = 1;
          ctx.beginPath();
          ctx.roundRect(lx, ly, tw + 10, 16, 4);
          ctx.fill();
          ctx.stroke();
          ctx.fillStyle = canvasPaint("var(--text-1)");
          ctx.fillText(n.label, lx + 5, ly + 11.5);
        }
      });
    }

    draw();

    disposeGlobe = observeGlobe({
      wrap,
      themeRoot: document.body,
      onResize: () => {
        const nw = wrap.clientWidth,
          nh = wrap.clientHeight;
        if (
          nw > 40 &&
          nh > 40 &&
          (Math.abs(nw - width) > 2 || Math.abs(nh - height) > 2)
        )
          initConduitGlobe();
      },
      onTheme: () => {
        if (document.getElementById("conduit-globe-canvas")) initConduitGlobe();
      },
    });

    let dragging = false,
      startPt = null,
      startRot = null;
    canvas.onpointerdown = (e) => {
      dragging = true;
      startPt = [e.clientX, e.clientY];
      startRot = [...conduitGlobeRot];
      canvas.setPointerCapture(e.pointerId);
    };
    canvas.onpointermove = (e) => {
      if (!dragging || !startPt) return;
      conduitGlobeRot[0] = startRot[0] + (e.clientX - startPt[0]) * 0.35;
      conduitGlobeRot[1] = Math.max(
        -68,
        Math.min(68, startRot[1] - (e.clientY - startPt[1]) * 0.35),
      );
      draw();
    };
    canvas.onpointerup = (e) => {
      if (
        dragging &&
        startPt &&
        Math.hypot(e.clientX - startPt[0], e.clientY - startPt[1]) < 5
      ) {
        const rect = canvas.getBoundingClientRect();
        const mx = e.clientX - rect.left,
          my = e.clientY - rect.top;
        for (const n of fleetNodes) {
          const pt = project([n.lng, n.lat]);
          if (pt && Math.hypot(pt[0] - mx, pt[1] - my) < 14) {
            onSelectScreen(n.screen);
            break;
          }
        }
      }
      dragging = false;
    };
  }

  return {
    init: initConduitGlobe,
    dispose() {
      disposeGlobe();
      disposeGlobe = () => {};
    },
  };
}
