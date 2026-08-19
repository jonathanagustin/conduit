#!/usr/bin/env bash
# Proves the frontend-surface gate rejects each thing it claims to catch.
#
# The gate passes 438/34/24/3-agreed on the committed tree, which is the shape
# that makes a gate look ornamental. So this plants one violation of each of its
# four rules and requires each to fail:
#
#   1. a count that no longer matches the contents
#   2. a tenant identifier in a supposedly templated extract
#   3. a cross-source disagreement -- the rule the artifacts' credibility rests
#      on, planted three ways: wrong method, wrong path, operation missing
#   4. a new deployment-vocabulary hit, which would undercut Finding 1
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-apollo-frontend-surface.mjs"
rest="docs/apollo/apollo-control-panel-rest-endpoints-6.533.15.json"
graphql="docs/apollo/apollo-control-panel-graphql-operations-6.533.15.json"

for subject in "${rest}" "${graphql}"; do
  if ! git diff --quiet -- "${subject}" || ! git diff --cached --quiet -- "${subject}"; then
    echo "verify-apollo-frontend-surface self-test: SKIP (${subject} has uncommitted changes)" >&2
    exit 0
  fi
done

restore() { git checkout -- "${rest}" "${graphql}"; }
trap restore EXIT

# Mutate the REST extract in place. Reads the same normalization the gate does,
# so the self-test cannot pass by disagreeing with it about the file's shape.
mutate() {
  node -e '
    const fs = require("node:fs");
    const [file, mode, arg] = process.argv.slice(1);
    const doc = JSON.parse(fs.readFileSync(file, "utf8"));
    const services = Array.isArray(doc.services)
      ? doc.services.map((s) => [s.name, s.endpoints ?? []])
      : Object.entries(doc.services).map(([n, v]) => [n, Array.isArray(v) ? v : (v.endpoints ?? [])]);
    const find = (name) => services.flatMap(([, list]) => list).find((e) => e.name === name);
    if (mode === "count") doc.endpointCount = Number(arg);
    if (mode === "leak") find("getAppliedApolloLabels").path = "/environment/applied-apollo-labels/ri.compass.main.folder.0123abcd-89ef-4567-89ab-0123456789ab";
    if (mode === "method") find("restoreApolloPermissions").method = "POST";
    if (mode === "path") find("createApolloSpaceForOrganization").path = "/admin/enrollment/{enrollmentRid}/create-apollo-space";
    if (mode === "drop") {
      for (const [, list] of services) {
        const at = list.findIndex((e) => e.name === "getAppliedApolloLabels");
        if (at >= 0) { list.splice(at, 1); doc.endpointCount -= 1; }
      }
    }
    if (mode === "deployment") {
      const [, list] = services.find(([n]) => n === "EnvironmentService");
      list.push({ method: "GET", name: "getProductReleases", path: "/environment/product-releases" });
      doc.endpointCount += 1;
    }
    fs.writeFileSync(file, JSON.stringify(doc, null, 2) + "\n");
  ' "${rest}" "$1" "${2:-}"
}

reject() {
  local what="$1"
  if node "${gate}" >/dev/null 2>&1; then
    echo "FAIL: the gate accepted ${what}" >&2
    exit 1
  fi
  echo "  rejects: ${what}"
  restore
}

echo "verify-apollo-frontend-surface: the gate must reject drift, not just pass"

if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed tree" >&2
  node "${gate}" >&2 || true
  exit 1
fi
echo "  passes: the committed tree"

mutate count 999
reject "a recorded endpointCount that does not match the contents"

mutate leak
reject "a RID literal in a supposedly templated path"

mutate method
reject "a method the Maven projection contradicts"

mutate path
reject "a path the Maven projection contradicts"

mutate drop
reject "an operation the Maven projection declares and the frontend has lost"

mutate deployment
reject "a new deployment-vocabulary endpoint Finding 1 does not account for"

if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate did not pass after the extracts were restored" >&2
  exit 1
fi
echo "  passes: the restored tree"

echo "verify-apollo-frontend-surface: all cases behaved"
