// Capture the Apollo authorization graph from the live tenant.
//
// The recovery corpus carries only shape captures of /multipass/api/roles/v2/config
// ("shape inferred; values scrubbed"), so it recovers identifiers but not the
// role-to-operation, workflow-to-operation, or category-to-workflow edges. That
// scrubbing is done by the recovery pipeline, not by the server, so one read-only
// authenticated GET recovers the missing edges exactly.
//
//   node scripts/capture-apollo-authorization-graph.mjs -o docs/evidence/apollo-authorization-graph.json
//
// Read-only and narrow by construction:
//   - one HTTP GET, no mutation verb is reachable from this script;
//   - retention is the transitive closure of the Apollo roles and categories,
//     so borrowed operations owned by other products are kept and the graph
//     closes; every tenant role outside that closure is dropped before writing;
//   - the credential, its claims, and non-Apollo tenant configuration are never
//     printed or persisted.
//
// Exit 0 on a complete capture, 1 on any missing prerequisite or non-2xx.
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";

const PREFIX = "apollo-";
const ENDPOINT = "/multipass/api/roles/v2/config";
const API_VERSION = "apollo.platform.example/authorization-graph/v1alpha1";

const host = (process.env.FOUNDRY_HOST || process.env.FOUNDRY_URL || "").replace(/\/$/, "");
const token = process.env.PALANTIR_TOKEN || process.env.FOUNDRY_TOKEN || "";

const outIndex = process.argv.indexOf("-o");
const outPath = outIndex > -1 ? process.argv[outIndex + 1] : null;

if (!host || !token) {
  console.error("need FOUNDRY_HOST + PALANTIR_TOKEN in the environment");
  process.exit(1);
}

try {
  const ca = readFileSync("/root/.ccr/ca-bundle.crt", "utf8");
  const { setGlobalDispatcher, Agent } = await import("undici");
  setGlobalDispatcher(new Agent({ connect: { ca } }));
} catch {
  /* no bundle / undici unavailable — fetch uses system trust */
}

const response = await fetch(`${host}${ENDPOINT}`, {
  headers: { authorization: `Bearer ${token}`, accept: "application/json" },
});
if (!response.ok) {
  console.error(`${ENDPOINT} returned HTTP ${response.status}`);
  process.exit(1);
}

const raw = await response.text();
const body = JSON.parse(raw);

// Hash the exact bytes the server returned so the derived graph is anchored to
// them, without retaining the bytes themselves.
const responseDigest = "sha256:" + createHash("sha256").update(raw).digest("hex");

const isApollo = (id) => id.startsWith(PREFIX);
const namesApollo = (id) => id.toLowerCase().includes("apollo") && !isApollo(id);

// Retain every edge target. A role's authority is the complete set of
// operations it grants, and Apollo roles do grant operations outside the
// apollo- namespace; dropping those would understate authority and make 24
// viewer roles look empty. Foreign targets are recorded separately so the
// Apollo-owned and borrowed halves of the vocabulary stay distinguishable.
const foreignTargets = new Set();
const keepEdges = (values) => {
  if (!Array.isArray(values)) return [];
  const kept = [];
  for (const value of values) {
    if (typeof value !== "string") continue;
    if (!isApollo(value) && !namesApollo(value)) foreignTargets.add(value);
    kept.push(value);
  }
  return kept.sort();
};

const project = (map, fields) => {
  const out = {};
  for (const [id, entry] of Object.entries(map ?? {})) {
    if (!isApollo(id) && !namesApollo(id)) continue;
    if (entry === null || typeof entry !== "object") continue;
    const row = {};
    for (const field of fields) {
      const value = entry[field];
      if (value === undefined) continue;
      row[field] = Array.isArray(value) ? keepEdges(value) : value;
    }
    out[id] = row;
  }
  return out;
};

const roles = project(body.templateRoles, [
  "id",
  "displayName",
  "description",
  "operations",
  "canAssigns",
  "trackedWorkflows",
  "isDeleted",
]);
const categories = project(body.categories, ["id", "name", "description", "workflows"]);

// Completeness here is transitive closure, not prefix matching. An Apollo role
// grants operations owned by other products (containers:, ccs:,
// connectivityconfig:), and a prefix filter drops exactly those, leaving edges
// that resolve to nothing. Retain every definition reachable from the retained
// roles and categories so the graph closes over itself.
const reachable = new Set();
for (const row of Object.values(roles)) {
  for (const field of ["operations", "canAssigns", "trackedWorkflows"]) {
    for (const target of row[field] ?? []) reachable.add(target);
  }
}
for (const row of Object.values(categories)) {
  for (const target of row.workflows ?? []) reachable.add(target);
}

const projectReachable = (map, fields) => {
  const out = {};
  for (const [id, entry] of Object.entries(map ?? {})) {
    if (!isApollo(id) && !namesApollo(id) && !reachable.has(id)) continue;
    if (entry === null || typeof entry !== "object") continue;
    const row = {};
    for (const field of fields) {
      const value = entry[field];
      if (value === undefined) continue;
      row[field] = Array.isArray(value) ? keepEdges(value) : value;
    }
    out[id] = row;
  }
  return out;
};

const workflows = projectReachable(body.workflows, [
  "id",
  "name",
  "description",
  "operations",
  "contexts",
]);
const operations = projectReachable(body.operations, ["id", "name", "description"]);

// Any edge target with no definition in the retained maps is an unresolved
// reference. It must be reported, never silently tolerated, because it means
// the captured graph does not close.
const defined = new Set([
  ...Object.keys(roles),
  ...Object.keys(operations),
  ...Object.keys(workflows),
  ...Object.keys(categories),
]);
const unresolved = [...reachable].filter((target) => !defined.has(target)).sort();

const edgeCount = (map, field) =>
  Object.values(map).reduce((sum, row) => sum + (row[field]?.length ?? 0), 0);

// An entry whose edge array came back empty is a real signal, not a shrug: it
// means the role grants nothing we retained. Surface it rather than hide it.
const rolesWithNoOperations = Object.entries(roles)
  .filter(([, row]) => !row.operations?.length)
  .map(([id]) => id)
  .sort();

const evidence = {
  apiVersion: API_VERSION,
  kind: "ApolloAuthorizationGraph",
  source: {
    host,
    endpoint: ENDPOINT,
    method: "GET",
    status: response.status,
    responseSha256: responseDigest,
    responseBytes: Buffer.byteLength(raw),
    capturedAt: new Date().toISOString(),
  },
  counts: {
    roles: Object.keys(roles).length,
    operations: Object.keys(operations).length,
    workflows: Object.keys(workflows).length,
    categories: Object.keys(categories).length,
    roleOperationEdges: edgeCount(roles, "operations"),
    roleCanAssignEdges: edgeCount(roles, "canAssigns"),
    roleTrackedWorkflowEdges: edgeCount(roles, "trackedWorkflows"),
    workflowOperationEdges: edgeCount(workflows, "operations"),
    categoryWorkflowEdges: edgeCount(categories, "workflows"),
  },
  sourceTotals: {
    templateRoles: Object.keys(body.templateRoles ?? {}).length,
    operations: Object.keys(body.operations ?? {}).length,
    workflows: Object.keys(body.workflows ?? {}).length,
    categories: Object.keys(body.categories ?? {}).length,
  },
  rolesWithNoRetainedOperations: rolesWithNoOperations,
  unresolvedEdgeTargets: unresolved,
  unresolvedEdgeTargetCount: unresolved.length,
  graphCloses: unresolved.length === 0,
  borrowedOperationTargets: [...foreignTargets].sort(),
  borrowedOperationTargetCount: foreignTargets.size,
  graph: { roles, operations, workflows, categories },
  boundary:
    "Tenant-observed Apollo authorization configuration. It establishes the " +
    "identifier graph this tenant serves. It does not establish private Hub " +
    "behavior, nor that a Platform role granting the same identifier grants the " +
    "same authority.",
};

// --verify re-captures and compares against a committed graph instead of
// writing one. The generated role table pins the response digest, but a digest
// only proves the file has not changed; it cannot notice that the TENANT has.
// Apollo added twelve roles and two families between the corpus capture and the
// first live one, and nothing would have reported it. This is the check that
// would have. It needs a live credential, so it is a scheduled or manual gate
// rather than a CI one.
const verifyIndex = process.argv.indexOf("--verify");
if (verifyIndex > -1) {
  const committedPath = process.argv[verifyIndex + 1];
  const committed = JSON.parse(readFileSync(committedPath, "utf8"));

  // The response digest is provenance, not identity. Two captures taken minutes
  // apart hashed differently while every identifier and edge was equal, so the
  // bytes carry something volatile and hashing them would make this gate cry
  // wolf on every run. Drift is therefore measured over the semantic
  // projection, the same correction A43 forced on CP-SAT receipts.
  const drift = [];
  const digestChanged = committed.source?.responseSha256 !== evidence.source.responseSha256;

  for (const map of ["roles", "operations", "workflows", "categories"]) {
    const before = new Set(Object.keys(committed.graph?.[map] ?? {}));
    const after = new Set(Object.keys(evidence.graph[map]));
    const added = [...after].filter((id) => !before.has(id)).sort();
    const removed = [...before].filter((id) => !after.has(id)).sort();
    if (added.length) drift.push(`${map}: ${added.length} added -> ${added.join(", ")}`);
    if (removed.length) drift.push(`${map}: ${removed.length} removed -> ${removed.join(", ")}`);
  }

  // Edge drift matters even when no identifier moves: a role can quietly gain
  // or lose grants while its name stays put.
  for (const [id, role] of Object.entries(evidence.graph.roles)) {
    const priorRole = committed.graph?.roles?.[id];
    if (!priorRole) continue;
    for (const field of ["operations", "canAssigns", "trackedWorkflows"]) {
      const before = (priorRole[field] ?? []).join(" ");
      const after = (role[field] ?? []).join(" ");
      if (before !== after) drift.push(`role ${id}: ${field} changed`);
    }
  }

  // The graph is captured from /config alone. /all and /all-including-deleted
  // return the same role set from an independent code path, so a mismatch means
  // either the endpoints have diverged or one capture is partial. Cross-checking
  // them turns a one-off corroboration into a repeatable one, and would catch a
  // soft-deleted role that /config never shows.
  const configRoleSet = new Set(Object.keys(evidence.graph.roles));
  for (const endpoint of ["all", "all-including-deleted"]) {
    try {
      const crossResponse = await fetch(`${host}/multipass/api/roles/v2/${endpoint}`, {
        headers: { authorization: `Bearer ${token}`, accept: "application/json" },
      });
      if (!crossResponse.ok) {
        drift.push(`/roles/v2/${endpoint}: HTTP ${crossResponse.status}`);
        continue;
      }
      const crossApollo = new Set(
        Object.keys((await crossResponse.json()).roles ?? {}).filter((id) => id.startsWith(PREFIX)),
      );
      const onlyInConfig = [...configRoleSet].filter((id) => !crossApollo.has(id));
      const onlyInCross = [...crossApollo].filter((id) => !configRoleSet.has(id));
      if (onlyInConfig.length) drift.push(`/roles/v2/${endpoint}: missing ${onlyInConfig.length} role(s) config has`);
      if (onlyInCross.length) {
        drift.push(`/roles/v2/${endpoint}: carries ${onlyInCross.length} role(s) config lacks -> ${onlyInCross.slice(0, 5).join(", ")}`);
      }
    } catch (error) {
      drift.push(`/roles/v2/${endpoint}: ${String(error).slice(0, 80)}`);
    }
  }

  if (drift.length) {
    console.error("FAIL apollo-authorization-graph-drift:");
    for (const entry of drift) console.error(`  - ${entry}`);
    console.error(
      `\n${drift.length} difference(s). Re-run without --verify to recapture, ` +
        "regenerate with apolloroleg, and review the alias map before committing.",
    );
    process.exit(1);
  }
  console.error(
    `PASS apollo-authorization-graph-drift: live tenant matches ${committedPath} ` +
      `(${evidence.counts.roles} roles cross-checked against /all and /all-including-deleted, ` +
      `${evidence.counts.roleOperationEdges} role-to-operation edges)`,
  );
  if (digestChanged) {
    console.error(
      "  note: the response digest differs while the semantic graph is equal, " +
        "so the raw bytes carry volatile content and are provenance only.",
    );
  }
  process.exit(0);
}

const serialized = JSON.stringify(evidence, null, 2) + "\n";
if (outPath) {
  writeFileSync(outPath, serialized);
  console.error(`wrote ${outPath}`);
} else {
  process.stdout.write(serialized);
}

const c = evidence.counts;
console.error(
  `roles=${c.roles} operations=${c.operations} workflows=${c.workflows} ` +
    `categories=${c.categories} | edges role->op=${c.roleOperationEdges} ` +
    `wf->op=${c.workflowOperationEdges} cat->wf=${c.categoryWorkflowEdges}`,
);
console.error(`graph closes: ${unresolved.length === 0} (unresolved targets: ${unresolved.length})`);
if (rolesWithNoOperations.length) {
  console.error(`NOTE: ${rolesWithNoOperations.length} retained roles have no retained operations`);
}
