#!/usr/bin/env node
// Guards the two API-surface extracts recovered from the captured
// apollo-control-panel frontend, and the one cross-source agreement that gives
// them their credibility.
//
// They arrive with this gate for the reason docs/evidence/README.md and
// verify-apollo-control-panel-seam.mjs both give: an artifact no gate reads can
// drift arbitrarily and nobody notices, and that has already happened once in
// this repository -- a count in an unread artifact overstated a divergence by a
// factor of two.
//
// Four things are checked, and the third is the one that matters:
//
//   1. Self-consistency. Each extract records its own counts; they must match
//      what it actually contains, or the headline numbers quoted in
//      docs/apollo/FRONTEND-CAS-CAPTURE.md are decoration.
//   2. Disclosure boundary. Templated paths only -- no RID literal, hostname,
//      JWT, UUID or absolute URL. Same boundary the seam gate keeps, for the
//      same reason: it is the part that erodes when someone makes provenance
//      "easier to trace".
//   3. Cross-source agreement. docs/evidence/apollo-control-panel-seam.json is
//      a bytecode-exact projection of the published Maven artifact -- a SERVER
//      side source at Primary fidelity. This extract is the SHIPPED FRONTEND,
//      mined statically. They were produced two days apart by unrelated
//      methods, and they overlap on exactly the three Apollo-named operations.
//      All three must agree on service, method, and path -- where the frontend
//      path is the seam path minus the constant `/control-panel/api` base the
//      generated bridge supplies. That agreement is the whole reason the other
//      435 rows can be read as service-relative rather than as guesses.
//   4. The deployment-vocabulary hit set. Finding 1 rests on "438 endpoints and
//      not one of them deploys anything". Four rows match the vocabulary scan
//      and all four are false friends (task-request revisions; FRONTEND SLS
//      assets on a release track). This pins that set, so a NEW hit fails and a
//      human has to look at it, rather than the claim quietly going stale.
//
// Runs offline against committed files and needs no credentials.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const REST = resolve(ROOT, "docs/apollo/apollo-control-panel-rest-endpoints-6.533.15.json");
const GRAPHQL = resolve(ROOT, "docs/apollo/apollo-control-panel-graphql-operations-6.533.15.json");
const SEAM = resolve(ROOT, "docs/evidence/apollo-control-panel-seam.json");

/** The base the generated IHttpApiBridge supplies and the bundle therefore
 * never carries. Established by the three-way agreement below, not assumed. */
const BRIDGE_BASE = "/control-panel/api";

/** Every row whose service, endpoint name or path matches the deployment
 * vocabulary, with why each is a false friend. Adding a row here is a decision;
 * that is the point. */
const KNOWN_NEAR_MISSES = new Map([
  ["TaskRequestService.getBatchTaskRequestVersionDetails", "a revision of a task request"],
  ["TaskRequestService.createRequestVersion", "a revision of a task request"],
  ["WorkspaceService.getAssetsInstalledOnTrack", "frontend SLS assets on a release track"],
  ["WorkspaceService.getAssetRoleInfoInstalledOnTrack", "frontend SLS assets on a release track"],
]);

const DEPLOYMENT_VOCABULARY =
  /release|product|install|deploy|plan|spoke|recall|rollout|artifact|helm|chart|version|upgrade|promot|maestro|agent/i;

const LEAKS = [
  [/\bri\.[a-z0-9-]+\.[a-z0-9-]*\.[a-z0-9-]+\.[0-9a-f-]{8,}/, "a RID literal"],
  [/\b[a-z0-9-]+\.(?:palantirfoundry|palantircloud|palantir)\.com\b/, "a Palantir hostname"],
  [/eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/, "a JWT"],
  [/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/, "a UUID"],
  [/https?:\/\/[^\s"')<>]{6,}/, "an absolute URL"],
];

const failures = [];
const fail = (message) => failures.push(message);

let rest;
let graphql;
let seam;
try {
  rest = JSON.parse(readFileSync(REST, "utf8"));
  graphql = JSON.parse(readFileSync(GRAPHQL, "utf8"));
  seam = JSON.parse(readFileSync(SEAM, "utf8"));
} catch (error) {
  console.error(`FAIL apollo frontend surface: ${error.message}`);
  process.exit(1);
}

/** Both extracts have been seen with `services` as an object and as an array;
 * normalize rather than depending on which producer wrote the file. */
function endpoints(document) {
  const services = document.services;
  const pairs = Array.isArray(services)
    ? services.map((service) => [service.name, service.endpoints ?? []])
    : Object.entries(services).map(([name, value]) => [
        name,
        Array.isArray(value) ? value : (value.endpoints ?? []),
      ]);
  return pairs.flatMap(([service, list]) => list.map((endpoint) => ({ service, ...endpoint })));
}

// --- 1. self-consistency ----------------------------------------------------
const rows = endpoints(rest);
const services = new Set(rows.map((row) => row.service));
if (rows.length !== rest.endpointCount) {
  fail(`REST extract records endpointCount ${rest.endpointCount} but contains ${rows.length}`);
}
if (services.size !== rest.serviceCount) {
  fail(`REST extract records serviceCount ${rest.serviceCount} but contains ${services.size}`);
}
const operations = graphql.operations ?? [];
if (operations.length !== graphql.summary?.operationVariantCount) {
  fail(
    `GraphQL extract records ${graphql.summary?.operationVariantCount} operation variants ` +
      `but contains ${operations.length}`,
  );
}
const incomplete = operations.filter((operation) => operation.complete !== true);
if (incomplete.length) {
  fail(
    `GraphQL extract claims every closure is complete, but ${incomplete.length} are not: ` +
      incomplete.map((operation) => operation.operation).join(", "),
  );
}

// --- 2. disclosure boundary -------------------------------------------------
for (const [label, text] of [
  ["REST extract", JSON.stringify(rest)],
  ["GraphQL extract", JSON.stringify(graphql)],
]) {
  for (const [pattern, what] of LEAKS) {
    const found = text.match(pattern);
    if (found) fail(`${label} contains ${what}: ${found[0].slice(0, 40)}`);
  }
}

// --- 3. cross-source agreement ----------------------------------------------
const byName = new Map();
for (const row of rows) {
  if (!byName.has(row.name)) byName.set(row.name, []);
  byName.get(row.name).push(row);
}
let agreed = 0;
for (const operation of seam.operations ?? []) {
  const candidates = byName.get(operation.name) ?? [];
  if (!candidates.length) {
    fail(
      `the Maven projection declares ${operation.name} but the shipped frontend never calls it ` +
        `-- one of the two sources is wrong and the disagreement needs a human`,
    );
    continue;
  }
  const match = candidates.find(
    (row) =>
      row.service === operation.service &&
      row.method === operation.method &&
      `${BRIDGE_BASE}${row.path}` === operation.path,
  );
  if (match === undefined) {
    const seen = candidates
      .map((row) => `${row.service} ${row.method} ${BRIDGE_BASE}${row.path}`)
      .join(" | ");
    fail(
      `${operation.name}: the projection has ${operation.service} ${operation.method} ` +
        `${operation.path}, the frontend has ${seen}`,
    );
    continue;
  }
  agreed += 1;
}
if (agreed === 0 && (seam.operations ?? []).length > 0) {
  fail("no operation agreed across the two sources, which makes the whole cross-check vacuous");
}

// --- 4. the deployment-vocabulary hit set -----------------------------------
const hits = rows
  .filter((row) => DEPLOYMENT_VOCABULARY.test(`${row.service} ${row.name} ${row.path}`))
  .map((row) => `${row.service}.${row.name}`);
const unexpected = hits.filter((hit) => !KNOWN_NEAR_MISSES.has(hit));
const vanished = [...KNOWN_NEAR_MISSES.keys()].filter((known) => !hits.includes(known));
if (unexpected.length) {
  fail(
    `new deployment-vocabulary hit(s) the receipt does not account for: ${unexpected.join(", ")} ` +
      `-- read them and either explain them in FRONTEND-CAS-CAPTURE.md Finding 6 or revise Finding 1`,
  );
}
if (vanished.length) {
  fail(`the extract no longer contains recorded near-miss(es): ${vanished.join(", ")}`);
}

if (failures.length) {
  for (const failure of failures) console.error(`FAIL apollo frontend surface: ${failure}`);
  process.exit(1);
}
console.log(
  `PASS apollo frontend surface: ${rows.length} endpoints across ${services.size} services and ` +
    `${operations.length} complete GraphQL operations; ${agreed} Apollo operation(s) agree with the ` +
    `Maven projection under ${BRIDGE_BASE}; ${hits.length} deployment-vocabulary hit(s), all recorded false friends`,
);
