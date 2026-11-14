#!/usr/bin/env node
// Fails when the console reads a CSS custom property nothing defines.
//
// `var(--x, fallback)` is silent by construction: when `--x` is undefined the
// fallback is used and the page looks deliberate, so a token name that is
// merely WRONG is indistinguishable from one that is right until somebody
// compares it against the design system. This repository shipped nine of them,
// found only by auditing every `var()` against the UI library's token file:
//
//   --pt-font-family-monospace   (x5)  UI library v4/v5 name; 6 has no such token
//   --bp-font-family-monospace   (x2)  never existed in any UI-library version
//   --pt-intent-primary-muted    (x4)  UI library v4/v5 name
//   --bp-text-color-muted        (x2)  near-miss for --bp-typography-color-muted
//   --bp-typography-color        (x1)  near-miss for -color-default-rest
//   --bp-background-color-subtle (x1)  near-miss for --bp-palette-light-gray-5
//   --brand                      (x2)  never defined anywhere
//   --font-ui                    (x1)  never defined anywhere
//
// Seven of those had fallbacks that happened to be the right value, which is
// exactly why they survived: the screens looked correct and the tokens were
// fiction. Two did not. `--font-ui` sat inside a `font:` shorthand, and a
// shorthand always writes font-family, so the ObjectHeader title rendered in
// generic sans-serif on every object page while the rest of the console used
// Source Sans. `--brand` was the logo accent's default, so an AppLogo with no
// accent resolved to an invalid colour.
//
// A custom property is legitimate here if it is defined by the installed
// UI library, defined by the console's own CSS, or set as an inline style from a
// component. Anything else is a name nobody honours.
//
// Runs offline against committed files and needs no credentials.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const SOURCE_ROOTS = ["web/src", "web/app", "web/components"].map((directory) => resolve(ROOT, directory));
const BLUEPRINT = resolve(ROOT, "web/node_modules/@blueprintjs/core/lib/css/blueprint-design-tokens.css");

function walk(directory) {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

let files;
let blueprint;
try {
  files = SOURCE_ROOTS.filter((directory) => {
    try {
      return statSync(directory).isDirectory();
    } catch {
      return false;
    }
  }).flatMap((directory) => walk(directory));
  blueprint = readFileSync(BLUEPRINT, "utf8");
} catch (error) {
  console.error(`FAIL console css variables: could not read the console sources -- ${error.message}`);
  console.error("  (run `pnpm --dir web install --frozen-lockfile` first)");
  process.exit(1);
}

const defined = new Set();
for (const [, name] of blueprint.matchAll(/(--[a-z0-9-]+)\s*:/g)) defined.add(name);

const stylesheets = files.filter((path) => path.endsWith(".css"));
const sources = files.filter((path) => path.endsWith(".ts") || path.endsWith(".tsx"));

// Declared in the console's own CSS...
for (const path of stylesheets) {
  const text = readFileSync(path, "utf8");
  for (const [, name] of text.matchAll(/^\s*(--[a-z0-9-]+)\s*:/gm)) defined.add(name);
}
// ...or handed to an element as an inline style by a component.
for (const path of sources) {
  const text = readFileSync(path, "utf8");
  for (const [, name] of text.matchAll(/["'](--[a-z0-9-]+)["']\s*:/g)) defined.add(name);
}

/** Blank out block comments, preserving newlines so reported line numbers still
 * point at the real line. A `var()` inside a comment is prose -- these
 * stylesheets explain their own history, and two of them quote the very names
 * this gate was written to ban. Matching per-line on `/*` was not enough: a
 * quoted name three lines into a comment is not at its start. */
function stripComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, " "));
}

const failures = [];
for (const path of stylesheets) {
  const lines = stripComments(readFileSync(path, "utf8")).split("\n");
  lines.forEach((line, index) => {
    for (const [, name] of line.matchAll(/var\(\s*(--[a-z0-9-]+)/g)) {
      if (!defined.has(name)) {
        failures.push(`${relative(ROOT, path)}:${index + 1} reads ${name}, which nothing defines`);
      }
    }
  });
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL console css variables: ${failure}`);
  console.error(
    "  Define it in web/src/styles/index.css, or use the real UI-library token. " +
      "A var() with a plausible fallback hides the mistake instead of showing it.",
  );
  process.exit(1);
}
console.log(
  `PASS console css variables: every var() across ${stylesheets.length} console stylesheet(s) ` +
    `names a property the UI library, the console's CSS, or a component's inline style defines`,
);
