import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, symlink, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { checkBundleSize } from "./check-bundle-size.mjs";

async function fixture(t) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "conduit-budget-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  await mkdir(path.join(directory, "assets"));
  await writeFile(
    path.join(directory, "assets/app.js"),
    "export const value = 1;",
  );
  await writeFile(
    path.join(directory, "assets/app.css"),
    "body {color: black}",
  );
  return directory;
}

test("counts nested lazy assets and accepts the exact limits", async (t) => {
  const directory = await fixture(t);
  await mkdir(path.join(directory, "assets/lazy"));
  await writeFile(
    path.join(directory, "assets/lazy/optional.js"),
    "export const optional = true;",
  );
  const result = await checkBundleSize(directory);
  assert.equal(result.assets.length, 3);
  assert.equal(
    result.total,
    result.assets.reduce((sum, asset) => sum + asset.bytes, 0),
  );
  const limits = {
    total: result.total,
    asset: Math.max(...result.assets.map((asset) => asset.bytes)),
  };
  await checkBundleSize(directory, limits);
  await assert.rejects(
    checkBundleSize(directory, { ...limits, total: limits.total - 1 }),
    /total:/,
  );
  await assert.rejects(
    checkBundleSize(directory, { ...limits, asset: limits.asset - 1 }),
    /optional.js:/,
  );
});

test("refuses absent output and missing JavaScript or CSS", async (t) => {
  const directory = await fixture(t);
  await assert.rejects(
    checkBundleSize(path.join(directory, "missing")),
    /ENOENT/,
  );
  await rm(path.join(directory, "assets/app.css"));
  await assert.rejects(checkBundleSize(directory), /JavaScript and CSS/);
  await rm(path.join(directory, "assets/app.js"));
  await writeFile(path.join(directory, "assets/app.css"), "body {}");
  await assert.rejects(checkBundleSize(directory), /JavaScript and CSS/);
});

test("refuses symlink assets and directories instead of omitting them", async (t) => {
  const directory = await fixture(t);
  await symlink(
    path.join(directory, "assets/app.js"),
    path.join(directory, "linked.js"),
  );
  await assert.rejects(checkBundleSize(directory), /symlink/);
  await rm(path.join(directory, "linked.js"));
  await symlink(path.join(directory, "assets"), path.join(directory, "linked"));
  await assert.rejects(checkBundleSize(directory), /symlink/);
  await assert.rejects(
    checkBundleSize(path.join(directory, "linked")),
    /directory/,
  );
});
