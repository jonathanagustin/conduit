#!/usr/bin/env node
// Fails when a Hub route has no registered authorization decision.
//
// docs/operations/PRODUCTION-READINESS.md filed this under "Test types":
// "Refusals are tested where someone thought to; nothing enumerates route x
// role, so a new route defaults to untested." Untested was the smaller half.
// The measurement is in internal/hub/composition/authorization.go and in
// TestARouteWithNoRegisteredDecisionIsNotServed: a route mounted without a
// decision was not untested, it was unauthenticated, and it served its body to
// a caller holding no credential while the route beside it answered 401.
//
// The routing fix is the register plus the guard in that Go file. This is the
// other half: the register has to keep naming the routes the Hub actually
// mounts. Nothing about the guard makes that true -- a route missing from the
// register is refused at runtime, which is safe but is a 404 on a route
// somebody meant to ship, and the failure a developer needs is at build time
// with the route's name in it.
//
// So this reads the route registrations out of the Go sources as text, the way
// verify-console-boundaries.mjs reads the console's /console/ prefix out of
// three files, and reconciles them against the register in both directions:
//
//   - a mounted pattern with no register entry fails, which is the gate the
//     document said did not exist;
//   - a register entry that names no mounted pattern fails too, because a stale
//     row is how a register stops describing the thing it claims to describe,
//     and because without it the guard would happily carry patterns for routes
//     that were deleted a year ago.
//
// Reading text rather than running Go is deliberate and is the same choice
// verify-console-contract.mjs makes: this needs no toolchain, no build, and no
// credentials, so it runs in the same second as the linters. The cost is that
// it can only see routes written as literals -- which is why it also asserts a
// floor on how many it found, so a regex that stopped matching reports a
// failure rather than an empty agreement.
//
// Three surfaces are enumerated, because the Hub mounts routes three ways and a
// gate that knew only the first would have said nothing about the newest:
//
//   - internal/controlplane, which registers patterns on its own mux;
//   - internal/apollo/openapi.gen.go, the generated /api/v0 transport, whose
//     patterns are assembled from http.MethodX + a path literal;
//   - internal/notifications/adapters/httpapi, mounted in front of the facade
//     from internal/hub/composition.
//
// Runs offline against committed files and needs no credentials.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const REGISTER = "internal/hub/composition/authorization.go";

// The floor is a smoke alarm rather than a budget. It is well under the number
// currently mounted, so adding routes never trips it; what trips it is a regex
// that stopped matching, which would otherwise turn this gate into a
// tautology -- nothing found, nothing missing, PASS.
const FLOOR = 100;

// Where routes are mounted. Directories are walked; files are read directly.
// Test files are excluded everywhere: a route registered in a _test.go is a
// fixture, and demanding a register entry for it would make this gate a
// nuisance in exactly the place people plant deliberate violations.
const SOURCES = [
 {path:"internal/hub/composition/live_events.go",surface:"general live events"},
 { path: "internal/adminstatus/transport/openapi.gen.go", surface: "administrator status" },
  { path: "internal/debugsessions/adapters/httpapi", surface: "debug request metadata" },
 {path:"internal/delivery/maintenance/httpapi",surface:"temporary maintenance admission"},
 {path:"internal/installoverride/adapters/httpapi",surface:"Module override metadata"},
  { path: "internal/controlplane", surface: "controlplane REST" },
  { path: "internal/apollo/openapi.gen.go", surface: "Apollo /api/v0" },
  { path: "internal/notifications/adapters/httpapi", surface: "notifications" },
{ path:"internal/spaces/adapters/httpapi",surface:"spaces" },
  { path: "internal/productassets/adapters/httpapi", surface: "product assets" },
 {path:"internal/changeactivity/adapters/httpapi",surface:"request activity"},
  { path: "internal/packagingworkflow/httpapi", surface: "packaging" },
  { path: "internal/planning/transport/openapi.gen.go", surface: "planning sessions" },
  { path: "internal/ontology/httpapi", surface: "ontology" },
  { path: "internal/devworkspace/httpapi", surface: "developer workspaces" },
  { path: "internal/assistant/httpapi", surface: "assistant" },
  { path: "internal/intelligence/httpapi", surface: "intelligence" },
  { path: "internal/delivery/channels/httpapi", surface: "channel subscriptions" },
  { path: "internal/delivery/driftfix/httpapi", surface: "propose fix" },
  { path: "internal/delivery/waves/httpapi", surface: "staged rollouts" },
  { path: "internal/delivery/releasestatus/httpapi", surface: "release rollout read" },
  { path: "internal/delivery/blackout/httpapi", surface: "blackouts" },
  { path: "internal/delivery/checkin/httpapi", surface: "site check-in" },
  { path: "internal/appgen/httpapi", surface: "app generation" },
  { path: "internal/delivery/offlinebundle/httpapi", surface: "offline bundles" },
  { path: "internal/delivery/upgrades/httpapi", surface: "dependency and upgrade views" },
  { path: "internal/delivery/timeline/httpapi", surface: "environment timeline" },
  { path: "internal/attachments/httpapi", surface: "intake attachments" },
  { path: "internal/reporting/httpapi", surface: "reporting Connect overview" },
  { path: "internal/planning/agentstream", surface: "planning agent runs" },
  { path: "internal/workspaceacp/httpapi", surface: "workspace ACP" },
  { path: "internal/identity/adapters/revocationhttp", surface: "Agent credential revocation" },
  { path: "internal/identity/adapters/signinhttp", surface: "operator sign-in" },
];

const METHOD = "GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS";
// A pattern written as one literal: "GET /v1/plans".
const LITERAL = new RegExp(`"((?:${METHOD}) /[^"]*)"`, "g");
// A pattern the generated transport assembles: http.MethodGet+" "+base+"/api/v0/...".
const ASSEMBLED = new RegExp(
  `http\\.Method(${METHOD.split("|")
    .map((method) => method[0] + method.slice(1).toLowerCase())
    .join("|")})\\s*\\+\\s*" "\\s*\\+\\s*options\\.BaseURL\\s*\\+\\s*"([^"]+)"`,
  "g",
);
// A wildcard-only pattern with no method, which is how the notification
// adapter registers its method-not-allowed responder.
const WILDCARD = /"(\/v1\/[a-z0-9/-]*\{[a-z]+\.\.\.\})"/g;

const failures = [];
const read = (path) => readFileSync(resolve(ROOT, path), "utf8");
const show = (path) => relative(ROOT, path).split("\\").join("/");

function goFilesBelow(path) {
  const absolute = resolve(ROOT, path);
  let info;
  try {
    info = statSync(absolute);
  } catch (error) {
    // A fixture that copies this gate does not contain every surface. A missing
    // directory contributes no routes; a present one still has to be registered.
    if (error && error.code === "ENOENT") return [];
    throw error;
  }
  if (!info.isDirectory()) return [absolute];
  const found = [];
  for (const entry of readdirSync(absolute)) {
    const child = join(absolute, entry);
    if (statSync(child).isDirectory()) found.push(...goFilesBelow(child));
    else if (entry.endsWith(".go") && !entry.endsWith("_test.go")) found.push(child);
  }
  return found;
}

// --- the mounted surface ----------------------------------------------------
const mounted = new Map();
for (const source of SOURCES) {
  for (const file of goFilesBelow(source.path)) {
    const text = readFileSync(file, "utf8");
    const patterns = [
      ...[...text.matchAll(LITERAL)].map((match) => match[1]),
      ...[...text.matchAll(ASSEMBLED)].map((match) => `${match[1].toUpperCase()} ${match[2]}`),
      ...[...text.matchAll(WILDCARD)].map((match) => match[1]),
    ];
    for (const pattern of patterns) {
      if (!mounted.has(pattern)) mounted.set(pattern, { file: show(file), surface: source.surface });
    }
  }
}

if (mounted.size < FLOOR) {
  failures.push(
    `only ${mounted.size} routes were found in the Go sources, which is below the floor of ${FLOOR}. ` +
      "Either the Hub shrank enormously or this gate has stopped reading route registrations, " +
      "and the second is far more likely.",
  );
}

// --- the register -----------------------------------------------------------
// Read as text for the same reason the routes are: this must not need a Go
// toolchain, and a register that could only be read by building it would be
// unreadable in exactly the failure -- a compile error -- where it matters.
const registerText = read(REGISTER);
const registered = new Map();
for (const row of registerText.matchAll(
  /\{Pattern:\s*"([^"]+)",\s*\n\s*Decision:\s*(\w+),\s*Detail:\s*"([^"]*)"\}/g,
)) {
  const [, pattern, decision, detail] = row;
  if (registered.has(pattern)) {
    failures.push(`${REGISTER} names ${pattern} twice; the guard's ServeMux would panic on it`);
  }
  registered.set(pattern, { decision, detail });
}
if (registered.size === 0) {
  failures.push(
    `${REGISTER} yielded no rows. Either the register was emptied or its table changed shape ` +
      "in a way this gate cannot read; both must fail loudly rather than pass on nothing.",
  );
}

// Every decision named in the table must be a constant the file declares. A row
// carrying an invented decision would compile only if the constant existed, but
// this gate is also read by people deciding what a row means, and a decision
// name with no declaration beside it is a claim with no definition.
const declared = new Set(
  [...registerText.matchAll(/^\t(Decision\w+)\s+Decision\s*=/gm)].map((match) => match[1]),
);
for (const [pattern, row] of registered) {
  if (!declared.has(row.decision)) {
    failures.push(`${pattern} carries the decision ${row.decision}, which ${REGISTER} does not declare`);
  }
  if (row.detail.trim() === "") {
    failures.push(`${pattern} names a decision with an empty detail; the matrix cell says nothing`);
  }
  // A public row has to say why it is safe to serve to anybody. "public" with
  // no reason is the shape a hole takes when somebody needed the gate to pass.
  if (row.decision === "DecisionPublic" && row.detail.trim().length < 20) {
    failures.push(
      `${pattern} is public but its detail does not say why that is safe: "${row.detail}"`,
    );
  }
}

// App generation and the intake attachments a result names are one operator's
// rows. operator-any would publish the description, the generated source, and
// the file bytes to every authenticated operator. A shared catalog is not a
// decision this register carries; adding one would be a separate explicit row.
const ownerScoped = [
  "GET /v1/app-generations",
  "GET /v1/app-generations/{id}",
  "POST /v1/app-generations",
  "GET /v1/intake-attachments",
  "GET /v1/intake-attachments/{digest}",
  "POST /v1/intake-attachments",
];
for (const pattern of ownerScoped) {
  if (!mounted.has(pattern)) continue;
  const row = registered.get(pattern);
  if (row && row.decision !== "DecisionOperatorSelf") {
    failures.push(
      `${pattern} must be DecisionOperatorSelf so one operator cannot read another's app generation or intake attachment. ` +
        `It is registered as ${row.decision}. There is no shared-catalog decision.`,
    );
  }
}

// --- reconciliation, both directions ---------------------------------------
for (const [pattern, where] of mounted) {
  if (!registered.has(pattern)) {
    failures.push(
      `${where.surface}: ${pattern} is mounted in ${where.file} with no authorization decision registered ` +
        `in ${REGISTER}. Add a row naming what it requires. Until then the Hub will not serve it.`,
    );
  }
}
for (const pattern of registered.keys()) {
  if (!mounted.has(pattern)) {
    failures.push(
      `${REGISTER} registers a decision for ${pattern}, which no Go source mounts. ` +
        "Remove the row, or restore the route it was written for.",
    );
  }
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
const byDecision = new Map();
for (const row of registered.values()) {
  byDecision.set(row.decision, (byDecision.get(row.decision) ?? 0) + 1);
}
const summary = [...byDecision]
  .sort()
  .map(([decision, count]) => `${decision.replace(/^Decision/, "").toLowerCase()} ${count}`)
  .join(", ");
console.log(
  `PASS Hub route authorization: ${mounted.size} mounted routes, every one with a registered decision (${summary})`,
);

