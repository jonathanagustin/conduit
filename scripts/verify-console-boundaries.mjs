#!/usr/bin/env node
// Fails when the console's internal architecture stops being what it claims.
//
// `web/README.md` and `docs/ui/foundry-frontend-architecture.md` describe a
// layered console: a runtime bootstrap under everything, one network seam in
// `api/`, presentation primitives in `components/`, screens in `features/`, and
// composition in `app/`. Nothing checked any of it. A layering that is only a
// convention survives exactly as long as the reviewer who remembers it, and the
// two failures it is meant to prevent are both quiet:
//
//   - A `fetch` in a feature file works, ships, and is invisible until someone
//     needs every request to carry a header, or to be cancelled, or to be
//     retried, and finds the seam is no longer one place.
//   - An import from `components/` up into `features/` typechecks and bundles.
//     It is only when the components are extracted for a second consumer that
//     it turns out they were never extractable.
//
// The fourth rule is different in kind and is the reason this file reads Go.
// The `/console/` prefix is written in three places -- the Vite base, the
// router basename, and the Hub's path check -- and `web/README.md` already
// calls it "load-bearing": with the wrong base the console works under
// `vite dev` and 404s on every asset once embedded. That failure appears after
// every console test has passed, because the tests run against a server that
// serves what it was given. Only the three files together can catch it, so
// this compares them the way `verify-console-contract.mjs` compares status
// vocabularies -- by reading the Go source as text, which needs no toolchain.
//
// The fifth rule -- one feature may not import another -- was deliberately left
// out of the first version of this file, because four such imports existed
// (products -> promotions twice, environments -> fleet, environments -> plans)
// and forbidding them meant a refactor of `web/src`, which regenerates the
// bundle in `internal/consoleui/dist`. Stage 2 of
// docs/ui/foundry-frontend-architecture.md did that refactor: `statusTags`,
// `PlanStatusTag`, `ReleaseChannelGraph` and `TransitionDetail` are now in
// `web/src/components/`, and the rule can be enforced.
//
// It is worth stating why it is a rule and not a preference. A feature that
// imports another compiles, bundles, and renders. What it costs is paid later:
// deleting or reshaping a screen becomes an archaeology exercise, because the
// screen was also somebody's component library. Layering (rule 1) cannot catch
// it -- `features` importing `features` is a same-layer import, which is legal
// by construction. Only a sibling rule can.
//
// `web/src/features/useIndexQuery.ts` sits directly in `features/`, not in a
// feature, and is imported by nine screens. That is shared feature-layer logic
// rather than a cross-feature reach, so it is allowed: the rule compares the
// *feature directory*, and a file with no feature directory has none to differ
// from.
//
// Runs offline against committed files and needs no credentials.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const SRC = resolve(ROOT, "web/src");

const failures = [];
const read = (path) => readFileSync(path, "utf8");
const show = (path) => relative(ROOT, path).split("\\").join("/");

// --- rule 4: the /console/ prefix, in the three places it is written --------
// Normalised to a single trailing slash so `/console` and `/console/` compare
// equal; the three files spell it both ways for their own good reasons and the
// disagreement worth catching is the path, not the slash.
const slash = (value) => (value.endsWith("/") ? value : `${value}/`);

const PREFIX_SITES = [
  {
    what: "the Vite base path",
    file: "web/vite.config.ts",
    pattern: /\bbase:\s*"([^"]+)"/,
  },
  {
    what: "the router basename",
    file: "web/src/app/providers.tsx",
    pattern: /\bbasename="([^"]+)"/,
  },
  {
    what: "the Hub's asset path check",
    file: "internal/consoleui/handler.go",
    pattern: /strings\.HasPrefix\(requestPath,\s*"([^"]+)"\)/,
  },
];

const prefixes = [];
for (const site of PREFIX_SITES) {
  let text;
  try {
    text = read(resolve(ROOT, site.file));
  } catch {
    failures.push(`console prefix: cannot read ${site.file}, which states ${site.what}`);
    continue;
  }
  const found = site.pattern.exec(text);
  if (!found) {
    failures.push(
      `console prefix: ${site.file} no longer states ${site.what} in a form this gate can read`,
    );
    continue;
  }
  prefixes.push({ ...site, value: slash(found[1]) });
}
const distinct = new Set(prefixes.map((site) => site.value));
if (distinct.size > 1) {
  failures.push(
    `console prefix: the three statements disagree -- ${prefixes
      .map((site) => `${site.file} says "${site.value}" (${site.what})`)
      .join("; ")}. Assets resolve under vite dev and 404 once embedded.`,
  );
}

// --- the source tree --------------------------------------------------------
// `web/src/test` is the test tree. It reaches into every layer on purpose, so
// the layering and network rules do not apply to it; its imports are still
// resolved, because a dead specifier there is still a dead specifier.
const LAYERS = new Map([
  ["runtime", 0],
  ["api", 1],
  ["components", 2],
  ["features", 3],
  ["app", 4],
]);
const ENTRY_LAYER = 5;
const SOURCE = /\.(?:ts|tsx)$/;

function walk(directory, matches) {
  const found = [];
  let entries;
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    return found;
  }
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) found.push(...walk(path, matches));
    else if (matches.test(entry.name)) found.push(path);
  }
  return found;
}

const files = walk(SRC, SOURCE);
if (files.length === 0) {
  console.error("FAIL console boundaries: no TypeScript sources under web/src");
  process.exit(1);
}

// The layer a path belongs to: its first segment below web/src, or the entry
// layer for a file sitting directly in web/src (main.tsx). `null` means the
// path is outside the layered tree (the test tree, or outside web/src).
function layerOf(path) {
  const rest = relative(SRC, path);
  if (rest.startsWith("..")) return null;
  const segments = rest.split(/[\\/]/);
  if (segments.length === 1) return ENTRY_LAYER;
  const layer = LAYERS.get(segments[0]);
  return layer === undefined ? null : layer;
}
const nameOf = (path) => {
  const rest = relative(SRC, path);
  const segments = rest.split(/[\\/]/);
  return segments.length === 1 ? rest : segments[0];
};

// --- rule 5: a feature may not import another feature ------------------------
// The feature a path belongs to: the directory below `web/src/features/`.
// `null` for anything else, including a file sitting directly in `features/`
// (`useIndexQuery.ts`), which is shared feature-layer logic rather than a
// feature of its own. Two `null`s never compare unequal below, so such a file
// is reachable from every feature and can itself reach none.
function featureOf(path) {
  const rest = relative(SRC, path);
  if (rest.startsWith("..")) return null;
  const segments = rest.split(/[\\/]/);
  if (segments[0] !== "features" || segments.length < 3) return null;
  return segments[1];
}

// --- rule 3: every relative import resolves ---------------------------------
// The console writes `.js` specifiers for `.tsx` sources, so a resolver that
// only tried the literal path would call every one of them broken.
const IMPORT = /(?:\bfrom\s*|\bimport\s*\(|\brequire\s*\()\s*["'](\.{1,2}\/[^"']+)["']/gu;

// --- rule 6: no orphaned stylesheet -----------------------------------------
// A stylesheet reaches the bundle through exactly one line -- a side-effect
// `import "./thing.css"` in the component that needs it -- and nothing else in
// this repository can tell whether that line is there. Deleting it leaves a
// tree that typechecks, a build that succeeds, a bundle that matches a clean
// rebuild (because the rebuild is equally unstyled), and 122 browser tests that
// pass, because they assert on `data-testid` and text rather than on computed
// style. The screen simply loses its styling.
//
// This was found by planting it: `web/src/components/transition-detail.css` was
// orphaned deliberately, the bundle rebuilt, and every other gate stayed green.
// The exposure predates stage 2 -- it applies to all 22 stylesheets under
// `web/src` -- but stage 2 widened it, by splitting one feature stylesheet into
// two component ones whose only tether is a single import line each.
//
// The rule is deliberately weak: it proves a stylesheet is referenced, not that
// its rules are used. That is the part a static check can honestly make, and it
// covers the whole of the failure above.
//
// Side-effect imports need their own pattern: `import "./x.css";` has no
// `from`, so the rule 3 regex above does not see it. They are checked for
// existence and for orphaning, and deliberately NOT fed into the layering rule
// -- `main.tsx` imports `./styles/index.css`, and `styles` is not a layer.
const SIDE_EFFECT_IMPORT = /\bimport\s+["'](\.{1,2}\/[^"']+)["']/gu;
const STYLESHEET = /\.css$/;

function candidates(target) {
  const extension = extname(target);
  if (extension === ".js") {
    const stem = target.slice(0, -3);
    return [target, `${stem}.ts`, `${stem}.tsx`];
  }
  if (extension !== "") return [target];
  return [
    target,
    `${target}.ts`,
    `${target}.tsx`,
    `${target}.js`,
    join(target, "index.ts"),
    join(target, "index.tsx"),
    join(target, "index.js"),
  ];
}

const exists = (path) => {
  try {
    statSync(path);
    return true;
  } catch {
    return false;
  }
};

// --- rule 2: one network seam ----------------------------------------------
// `refetch(` and `queryClient.fetchQuery(` must not match, so a call is only a
// call when nothing word-like or a dot precedes it. `globalThis.fetch(` would
// slip past that lookbehind, so it is matched separately rather than loosened
// into a rule that also flags every react-query `refetch`.
const NETWORK = [
  { name: "fetch", pattern: /(?<![\w.$])fetch\s*\(/u },
  { name: "globalThis.fetch", pattern: /\b(?:globalThis|window|self)\s*\.\s*fetch\s*\(/u },
  { name: "XMLHttpRequest", pattern: /\bnew\s+XMLHttpRequest\b/u },
  { name: "EventSource", pattern: /\bnew\s+EventSource\b/u },
  { name: "navigator.sendBeacon", pattern: /\bnavigator\s*\.\s*sendBeacon\s*\(/u },
];

let layeredFiles = 0;
let checkedImports = 0;
const importedStylesheets = new Set();

for (const file of files) {
  const text = read(file);

  // Rule 6, first half: record every stylesheet some source imports, and fail
  // on one that names a file not on disk.
  for (const match of text.matchAll(SIDE_EFFECT_IMPORT)) {
    const specifier = match[1];
    if (!STYLESHEET.test(specifier)) continue;
    const target = resolve(dirname(file), specifier);
    if (!exists(target)) {
      failures.push(
        `unresolved stylesheet: ${show(file)} imports "${specifier}", which is not on disk`,
      );
      continue;
    }
    importedStylesheets.add(target);
  }
  const layer = layerOf(file);
  const layered = layer !== null;
  if (layered) layeredFiles += 1;

  if (layered && nameOf(file) !== "api") {
    for (const { name, pattern } of NETWORK) {
      if (pattern.test(text)) {
        failures.push(
          `network seam: ${show(file)} calls ${name} directly; ` +
            "web/src/api is the console's only transport boundary",
        );
      }
    }
  }

  for (const match of text.matchAll(IMPORT)) {
    const specifier = match[1];
    const target = resolve(dirname(file), specifier);
    checkedImports += 1;

    if (!candidates(target).some(exists)) {
      failures.push(`unresolved import: ${show(file)} imports "${specifier}", which is not on disk`);
      continue;
    }

    // The test tree is allowed to leave web/src: `web/src/test/unit/bootstrap.test.ts`
    // reads the shared `testdata/console-bootstrap/` fixtures the Go side reads,
    // which is the point of them. Nothing in the test tree is bundled.
    if (!layered) continue;

    if (relative(SRC, target).startsWith("..")) {
      failures.push(
        `escaping import: ${show(file)} imports "${specifier}", which leaves web/src; ` +
          "the console is a self-contained bundle",
      );
      continue;
    }

    const targetLayer = layerOf(target);
    if (targetLayer === null) {
      failures.push(
        `layering: ${show(file)} imports "${specifier}", which is outside the layered tree`,
      );
      continue;
    }
    if (targetLayer > layer) {
      failures.push(
        `layering: ${show(file)} (${nameOf(file)}) imports "${specifier}" from ` +
          `${nameOf(target)}; a lower layer may not depend on a higher one`,
      );
      continue;
    }

    // Rule 5. Checked after layering so a single bad import is reported once,
    // as the more specific of the two.
    const from = featureOf(file);
    const into = featureOf(target);
    if (from !== null && into !== null && from !== into) {
      failures.push(
        `cross-feature import: ${show(file)} (features/${from}) imports "${specifier}" from ` +
          `features/${into}; shared presentation belongs in web/src/components`,
      );
    }
  }
}

// Rule 6, second half: a stylesheet nothing imports is not in the bundle.
const stylesheets = walk(SRC, STYLESHEET);
for (const stylesheet of stylesheets) {
  if (importedStylesheets.has(stylesheet)) continue;
  failures.push(
    `orphaned stylesheet: nothing under web/src imports ${show(stylesheet)}; ` +
      "it is not in the bundle, and the screen it styles renders unstyled",
  );
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL console boundaries: ${failure}`);
  process.exit(1);
}
// The feature count is reported because rule 5 is the one rule that can pass
// vacuously: with one feature, or none, no import can cross. Printing the
// number the rule ranged over means a tree that stopped having features reads
// differently from a tree whose features stopped importing each other.
const features = new Set(files.map(featureOf).filter((name) => name !== null));
console.log(
  `PASS console boundaries: ${layeredFiles} layered sources, ${checkedImports} relative imports, ` +
    `${features.size} features with no cross-feature import, ` +
    `${stylesheets.length} stylesheets all imported, ` +
    `console prefix "${prefixes[0]?.value}" agrees in ${prefixes.length} places`,
);
