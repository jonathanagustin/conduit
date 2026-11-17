// Authored presentation colors belong to the native token sheet. Vendor CSS,
// image pixels and generated protocol validators are independently owned.
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join, relative } from "node:path";
import { parse } from "@babel/parser";

const root = fileURLToPath(new URL("..", import.meta.url));
function walk(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? walk(path) : [path];
  });
}
const failures = [];
for (const file of [
  "src/console",
  "src/components",
  "src/styles",
  "components",
].flatMap((directory) => walk(join(root, directory)))) {
  if (
    !/\.(css|tsx?|m?js)$/.test(file) ||
    file.includes("/assets/") ||
    file.includes(".generated.") ||
    file.endsWith("/operator/tokens.css")
  )
    continue;
  let source = readFileSync(file, "utf8");
  if (file.endsWith(".css")) source = source.replace(/\/\*[\s\S]*?\*\//g, "");
  else {
    const tree = parse(source, {
      sourceType: "module",
      plugins: ["typescript", "jsx"],
    });
    for (const comment of [...tree.comments].reverse())
      source = source.slice(0, comment.start) + source.slice(comment.end);
  }
  if (
    /(?<!&)#[a-f\d]{3}(?:[a-f\d]{3})?(?:[a-f\d]{2})?\b/i.test(source) ||
    /\b(?:rgba?|hsla?|oklch|oklab)\(\s*[\d.]/i.test(source) ||
    /\b(?:text|bg|border(?:-[lrtbxy])?|ring|fill|stroke)-(?:white|black|(?:slate|gray|zinc|red|orange|amber|yellow|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|pink|rose)-\d{2,3})\b/.test(
      source,
    )
  )
    failures.push(relative(root, file));
}
if (failures.length) {
  console.error(
    "Presentation colors must use native semantic tokens:\n" +
      failures.join("\n"),
  );
  process.exitCode = 1;
} else console.log("Presentation colors use native semantic tokens");
