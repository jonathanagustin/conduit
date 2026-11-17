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
// Fixed named colors are forbidden; adaptive keywords (currentColor, transparent) remain valid.
const cssNamedColors = [
  "aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood",
  "cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod",
  "darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon",
  "darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray",
  "dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green",
  "greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon",
  "lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon",
  "lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen",
  "magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue",
  "mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy",
  "oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred",
  "papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon",
  "sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue",
  "tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen",
]
  .join(" ")
  .split(" ")
  .join("|");
const namedCSSPattern =
  /(?:[:,(]\s*|(?<![\w-])(?:color|background(?:Color|-color)?|fill|stroke|border(?:Color|-color)?)\s*[:=]\s*["']?)(?:NAMED_COLOR)(?=[\s),;/]|$)/i;
const namedCSSValue = new RegExp(
  namedCSSPattern.source.replace("NAMED_COLOR", cssNamedColors),
  "i",
);
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
    /(?<!&)#(?:[a-f\d]{8}|[a-f\d]{6}|[a-f\d]{4}|[a-f\d]{3})\b/i.test(source) ||
    /\b(?:rgba?|hsla?|oklch|oklab)\(\s*[\d.]/i.test(source) ||
    namedCSSValue.test(source) ||
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
