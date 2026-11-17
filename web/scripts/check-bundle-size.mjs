import { readdir, readFile, lstat } from "node:fs/promises";
import { gzipSync } from "node:zlib";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

// Includes lazy chunks: splitting an asset must not evade the total budget.
export const budget = Object.freeze({ total: 2_359_296, asset: 1_310_720 });
export async function checkBundleSize(directory, limits = budget) {
  const assets = [];
  async function walk(current) {
    for (const entry of await readdir(current, { withFileTypes: true })) {
      const filename = path.join(current, entry.name);
      if (entry.isSymbolicLink())
        throw new Error(`Bundle contains symlink: ${filename}`);
      if (entry.isDirectory()) await walk(filename);
      else if (/\.(js|css)$/.test(entry.name)) {
        if (!entry.isFile())
          throw new Error(`Bundle asset is not regular: ${filename}`);
        assets.push({
          path: path.relative(directory, filename),
          bytes: gzipSync(await readFile(filename), { level: 9, mtime: 0 })
            .length,
        });
      }
    }
  }
  if (!(await lstat(directory)).isDirectory())
    throw new Error("Bundle output must be a directory");
  await walk(directory);
  if (
    !assets.some((asset) => asset.path.endsWith(".js")) ||
    !assets.some((asset) => asset.path.endsWith(".css"))
  )
    throw new Error("Bundle must contain JavaScript and CSS");
  const total = assets.reduce((sum, asset) => sum + asset.bytes, 0);
  const failures = assets
    .filter((asset) => asset.bytes > limits.asset)
    .map((asset) => `${asset.path}: ${asset.bytes} > ${limits.asset}`);
  if (total > limits.total) failures.push(`total: ${total} > ${limits.total}`);
  if (failures.length)
    throw new Error(
      `Console gzip budget exceeded (bytes): ${failures.join("; ")}`,
    );
  return { total, assets };
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const directory =
    process.argv[2] ??
    process.env.PLATFORM_CONSOLE_OUT_DIR ??
    fileURLToPath(new URL("../../internal/consoleui/dist", import.meta.url));
  try {
    const result = await checkBundleSize(directory);
    console.log(
      `Console gzip budget: ${result.total}/${budget.total} bytes across ${result.assets.length} JS/CSS assets; per-asset limit ${budget.asset}`,
    );
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
