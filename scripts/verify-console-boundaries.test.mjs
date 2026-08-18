#!/usr/bin/env node
// Proves the console-boundaries gate rejects each thing it claims to reject.
//
// A gate that only ever passes is indistinguishable from no gate, and this one
// is more exposed to that than most: four of its five rules are satisfied by a
// tree that has been correct since it was written, so a regex that matched
// nothing at all would look identical to a regex that matched correctly.
//
// So this builds a minimal console in a temporary directory -- five layers, a
// Vite config, a router, and a Go handler -- confirms the gate passes on it,
// then plants one violation per rule and confirms each is caught. The fixture
// is used rather than the repository tree because planting a violation in
// `web/src` would mean editing the source the committed bundle is built from,
// and a self-test must not be able to leave a modified `web/src` behind.
//
// Two negative controls run alongside: `void refetch()` in a feature and
// `queryClient.fetchQuery(...)` must NOT read as a direct network call, since
// react-query puts both in every page of this console and a network rule that
// flagged them would be turned off within a day.
import { cpSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const SOURCE_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const GATE = "scripts/verify-console-boundaries.mjs";
const fixture = mkdtempSync(join(tmpdir(), "conduit-console-boundaries-"));

const write = (path, contents) => {
  const target = join(fixture, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, contents);
};
const run = () =>
  execFileSync(process.execPath, [join(fixture, GATE)], { encoding: "utf8", stdio: "pipe" });

// The smallest tree that exercises every rule: one file per layer, each
// importing only downwards, plus the three places the /console/ prefix is
// written and a test file that reaches outside web/src on purpose.
const BASELINE = [
  [GATE, null], // copied, not written
  ["web/vite.config.ts", 'export default { base: "/console/" };\n'],
  [
    "web/src/app/providers.tsx",
    'import { App } from "./App.js";\nexport const P = () => <BrowserRouter basename="/console">{App}</BrowserRouter>;\n',
  ],
  [
    "internal/consoleui/handler.go",
    'package consoleui\n\nfunc name(requestPath string) bool {\n\tif !strings.HasPrefix(requestPath, "/console/") {\n\t\treturn false\n\t}\n\treturn true\n}\n',
  ],
  ["web/src/runtime/bootstrap.ts", "export const bootstrap = { tenant: \"example\" };\n"],
  ["web/src/api/errors.ts", "export class ApiError extends Error {}\n"],
  [
    "web/src/api/client.ts",
    'import { ApiError } from "./errors.js";\nexport async function getJSON(path: string) {\n  const response = await fetch(path);\n  if (!response.ok) throw new ApiError();\n  return response.json();\n}\n',
  ],
  [
    "web/src/components/QueryState.tsx",
    'import { ApiError } from "../api/errors.js";\nexport const QueryState = (error: ApiError) => error.message;\n',
  ],
  [
    "web/src/features/plans/PlansPage.tsx",
    'import { QueryState } from "../../components/QueryState.js";\n' +
      'import { getJSON } from "../../api/client.js";\n' +
      "export const PlansPage = ({ refetch, queryClient }) => {\n" +
      "  void refetch();\n" +
      '  void queryClient.fetchQuery({ queryKey: ["plans"], queryFn: () => getJSON("/plans") });\n' +
      "  return QueryState;\n};\n",
  ],
  [
    "web/src/app/App.tsx",
    'import { PlansPage } from "../features/plans/PlansPage.js";\n' +
      'import { bootstrap } from "../runtime/bootstrap.js";\n' +
      "export const App = () => [PlansPage, bootstrap];\n",
  ],
  ["web/src/main.tsx", 'import { App } from "./app/App.js";\nexport default App;\n'],
  ["testdata/console-bootstrap/tenant.json", '{"tenant":"example"}\n'],
  [
    "web/src/test/unit/bootstrap.test.ts",
    'import fixture from "../../../../testdata/console-bootstrap/tenant.json";\n' +
      'import { bootstrap } from "../../runtime/bootstrap.js";\n' +
      "export default [fixture, bootstrap];\n",
  ],
];

// Each planted violation: the rule it exercises, the file, the bad contents,
// and the substring the gate's message must contain. Checking the message
// matters -- a gate that fails for the wrong reason is a gate that will be
// "fixed" by changing the wrong thing.
const PLANTED = [
  {
    rule: "layering (a component reaching up into a feature)",
    file: "web/src/components/QueryState.tsx",
    contents:
      'import { PlansPage } from "../features/plans/PlansPage.js";\nexport const QueryState = PlansPage;\n',
    expect: "layering:",
  },
  {
    rule: "network seam (a direct fetch outside web/src/api)",
    file: "web/src/features/plans/PlansPage.tsx",
    contents: 'export const PlansPage = () => fetch("/v1/plans");\n',
    expect: "network seam:",
  },
  {
    rule: "network seam (globalThis.fetch outside web/src/api)",
    file: "web/src/features/plans/PlansPage.tsx",
    contents: 'export const PlansPage = () => globalThis.fetch("/v1/plans");\n',
    expect: "network seam:",
  },
  {
    rule: "unresolved relative import",
    file: "web/src/app/App.tsx",
    contents: 'import { Gone } from "./Gone.js";\nexport const App = Gone;\n',
    expect: "unresolved import:",
  },
  {
    rule: "an import escaping web/src",
    file: "web/src/api/client.ts",
    contents: 'import fixture from "../../../testdata/console-bootstrap/tenant.json";\nexport default fixture;\n',
    expect: "escaping import:",
  },
  {
    rule: "the Vite base disagreeing with the router and the Hub",
    file: "web/vite.config.ts",
    contents: 'export default { base: "/operator/" };\n',
    expect: "console prefix:",
  },
  {
    rule: "the Hub's path check disagreeing with the console",
    file: "internal/consoleui/handler.go",
    contents:
      'package consoleui\n\nfunc name(requestPath string) bool {\n\treturn strings.HasPrefix(requestPath, "/operator/")\n}\n',
    expect: "console prefix:",
  },
];

const baseline = () => {
  for (const [path, contents] of BASELINE) {
    if (contents === null) {
      mkdirSync(join(fixture, dirname(path)), { recursive: true });
      cpSync(join(SOURCE_ROOT, path), join(fixture, path));
    } else {
      write(path, contents);
    }
  }
};

try {
  baseline();

  const passed = run();
  if (!passed.includes("PASS console boundaries")) {
    throw new Error(`the baseline console did not pass:\n${passed}`);
  }
  // The negative controls live in the baseline feature file, so a pass here is
  // also proof that neither `refetch(` nor `queryClient.fetchQuery(` was read
  // as a direct network call.
  console.log("  passes: a correct five-layer console, with refetch and fetchQuery present");

  for (const { rule, file, contents, expect } of PLANTED) {
    write(file, contents);
    let message = "";
    let rejected = false;
    try {
      run();
    } catch (error) {
      rejected = true;
      message = `${error.stdout ?? ""}${error.stderr ?? ""}`;
    }
    if (!rejected) throw new Error(`planted violation survived: ${rule}`);
    if (!message.includes(expect)) {
      throw new Error(`${rule} was rejected, but not as "${expect}":\n${message}`);
    }
    console.log(`  rejects: ${rule}`);
    baseline();
  }

  const restored = run();
  if (!restored.includes("PASS console boundaries")) {
    throw new Error(`the restored fixture did not pass:\n${restored}`);
  }
  console.log("  passes: the restored fixture");
  console.log(`PASS console boundary regression: positive=2 planted=${PLANTED.length} caught=${PLANTED.length}`);
} finally {
  rmSync(fixture, { recursive: true, force: true });
}
