#!/usr/bin/env node
// Fails when the console's design tokens drift from the values a live Foundry
// tenant actually serves.
//
// The console is a reconstruction of a product nobody here can open. Its
// palette, intents, type scale, surfaces and shadows come from Blueprint, and
// the standing assumption has been that installing Blueprint is the same thing
// as matching Foundry. That was an assumption, not a measurement: an npm bump
// could silently restyle every screen away from the product, and every existing
// gate would stay green, because none of them know what the product looks like.
//
// This gate knows. docs/reference-platform/contracts/reference-design-tokens.json holds the
// 205 `--bp-*` declarations cut from the captured apollo-control-panel 6.533.15
// stylesheet -- the bytes the tenant served -- and this script compares them
// against the tokens in the installed @blueprintjs/core.
//
// It compares VALUES, normalized, because the two sides are written by
// different builds: the tenant's minifier emits `#fff`, `.1s` and `#0000001a`
// where the npm package emits `#ffffff`, `100ms` and `rgba(0, 0, 0, 0.1)`.
// Those are the same colour and the same duration. Treating them as drift would
// make the gate cry wolf 16 times out of 205 and it would be turned off within
// a week, so the normalizer below folds notation and nothing else. A genuinely
// different colour still fails.
//
// The assistant UI does not render Blueprint. @blueprintjs/core stays a
// devDependency so this comparison can still run: the captured Foundry token
// file is evidence, and an unnoticed edit to it would otherwise survive every
// other gate. It is not a claim that the assistant uses these tokens.
//
// What this gate does NOT do: it says nothing about layout, spacing, component
// choice or copy, and it is not evidence about the Apollo deployment console --
// the capture is Foundry Control Panel under an Apollo skin, which
// docs/reference-platform/FRONTEND-CAS-CAPTURE.md explains at length. Tokens are shared
// platform styling, which is exactly why they transfer and why the screens
// built on them do not.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const CONTRACT = join(root, "docs/reference-platform/contracts/reference-design-tokens.json");
const INSTALLED = join(root, "web/node_modules/@blueprintjs/core/lib/css/blueprint-design-tokens.css");

/** Fold the differences between two minifiers, and only those.
 *
 *   #fff                    -> #ffffff        (short hex)
 *   #0000001a               -> rgba(0,0,0,0.1) (hex with alpha)
 *   .05                     -> 0.05           (leading zero)
 *   100ms                   -> 0.1s           (duration unit)
 *   "0px 0px 0px 1px  x"    -> single-spaced
 *
 * Anything left over after this is a real difference in the value. */
function normalize(value) {
  let out = value.toLowerCase().trim();
  // Hex with an alpha byte -> the rgba() the unminified package writes.
  out = out.replace(/#([0-9a-f]{6})([0-9a-f]{2})\b/g, (_, rgb, alpha) => {
    const channel = (index) => parseInt(rgb.slice(index, index + 2), 16);
    const opacity = Math.round((parseInt(alpha, 16) / 255) * 100) / 100;
    return `rgba(${channel(0)}, ${channel(2)}, ${channel(4)}, ${opacity})`;
  });
  // Three-digit hex -> six.
  out = out.replace(/#([0-9a-f])([0-9a-f])([0-9a-f])\b/g, "#$1$1$2$2$3$3");
  // Milliseconds -> seconds, so 100ms and .1s compare equal.
  out = out.replace(/(\d*\.?\d+)ms\b/g, (_, n) => `${Number(n) / 1000}s`);
  // Leading-zero and trailing-zero normalization on bare decimals.
  out = out.replace(/(^|[\s(,])\.(\d)/g, "$10.$2");
  out = out.replace(/\b(\d+\.\d*?)0+\b/g, "$1").replace(/\b(\d+)\.(?=\D|$)/g, "$1");
  // rgba(0, 0, 0, 0.15) and rgba(0,0,0,.15) are the same value.
  return out.replace(/\s+/g, " ").replace(/\s*,\s*/g, ", ");
}

function parseTokens(css) {
  const tokens = new Map();
  for (const [, name, value] of css.matchAll(/(--bp-[a-z0-9-]+)\s*:\s*([^;}]+)/g)) {
    if (!tokens.has(name)) tokens.set(name, value.trim());
  }
  return tokens;
}

let contract;
let installed;
try {
  contract = JSON.parse(readFileSync(CONTRACT, "utf8"));
  installed = parseTokens(readFileSync(INSTALLED, "utf8"));
} catch (error) {
  // A missing node_modules is a setup problem, not a drift. Say which.
  console.error(`FAIL design tokens: could not read both sides -- ${error.message}`);
  console.error("  (run `pnpm --dir web install --frozen-lockfile` first)");
  process.exit(1);
}

const captured = contract.tokens;
const failures = [];
const missing = Object.keys(captured).filter((name) => !installed.has(name));
const added = [...installed.keys()].filter((name) => !(name in captured));

if (missing.length) {
  failures.push(
    `the installed Blueprint is missing ${missing.length} token(s) the tenant serves: ` +
      `${missing.slice(0, 6).join(", ")}${missing.length > 6 ? ", ..." : ""}`,
  );
}
if (added.length) {
  // Not automatically wrong -- a newer Blueprint may add tokens -- but it means
  // the console can style something the captured product had no value for, so
  // it is a deliberate decision rather than a silent one.
  failures.push(
    `the installed Blueprint defines ${added.length} token(s) the captured product does not: ` +
      `${added.slice(0, 6).join(", ")}${added.length > 6 ? ", ..." : ""}`,
  );
}
for (const [name, value] of Object.entries(captured)) {
  const mine = installed.get(name);
  if (mine === undefined) continue;
  if (normalize(mine) !== normalize(value)) {
    failures.push(`${name}: console has \`${mine}\`, the tenant serves \`${value}\``);
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL design tokens: ${failure}`);
  console.error(
    `  captured from ${contract.source.artifact} (${contract.source.stylesheet.slice(0, 12)}...)`,
  );
  process.exit(1);
}
console.log(
  `PASS design tokens: ${Object.keys(captured).length} Blueprint tokens match the values ` +
    `${contract.source.batch} ${contract.source.version} serves`,
);
