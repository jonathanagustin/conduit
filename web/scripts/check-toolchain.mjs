import assert from "node:assert/strict";
import { readFileSync, realpathSync } from "node:fs";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

export function verifyToolchain(snapshot) {
  assert.equal(
    snapshot.core,
    snapshot.expectedCore,
    "Vite core must match the installed Vite+ bundle",
  );
  assert.equal(
    snapshot.vitest,
    snapshot.expectedVitest,
    "Vitest must match the installed Vite+ bundle",
  );
  assert.equal(
    snapshot.sameCore,
    true,
    "Vite+ and plugins must resolve the same core",
  );
  assert.equal(
    snapshot.sameVitest,
    true,
    "Vite+ and tests must resolve the same Vitest instance",
  );
  assert.equal(
    snapshot.packageManager,
    `pnpm@${snapshot.pnpmPin}`,
    "Package manager must match mise.toml",
  );
}

function main() {
  const manifest = JSON.parse(
    readFileSync(new URL("../package.json", import.meta.url), "utf8"),
  );
  const require = createRequire(new URL("../package.json", import.meta.url));
  const bundlePath = require.resolve("vite-plus/package.json");
  const bundle = JSON.parse(readFileSync(bundlePath, "utf8"));
  const bundledRequire = createRequire(bundlePath);
  const core = require("vite/package.json");
  const vitest = require("vitest/package.json");
  const mise = readFileSync(
    new URL("../../mise.toml", import.meta.url),
    "utf8",
  );
  const pnpmPin = /^pnpm\s*=\s*"([^"]+)"/m.exec(mise)?.[1];
  assert.ok(pnpmPin, "mise.toml must declare pnpm");
  verifyToolchain({
    core: `${core.name}@${core.version}`,
    expectedCore: bundle.dependencies.vite.replace(/^npm:/, ""),
    vitest: vitest.version,
    expectedVitest: bundle.dependencies.vitest,
    sameCore:
      realpathSync(require.resolve("vite/package.json")) ===
      realpathSync(bundledRequire.resolve("vite/package.json")),
    sameVitest:
      realpathSync(require.resolve("vitest/package.json")) ===
      realpathSync(bundledRequire.resolve("vitest/package.json")),
    packageManager: manifest.packageManager,
    pnpmPin,
  });
  console.log(
    `Vite+ ${bundle.version}: matched core, Vitest instance, and pinned package manager`,
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main();
