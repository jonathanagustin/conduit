#!/usr/bin/env bash
# Proves the observed-schema gate refuses what it exists to refuse.
#
# The gate's job is to stop tenant material and silent drift reaching the
# repository. A gate that passes the committed tree proves only that the tree is
# currently clean; it says nothing about whether the gate would notice. So each
# case below plants exactly one violation into a copy of the evidence and
# requires a non-zero exit.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

observed="docs/reference-platform/observed/reference-graphql-observed.json"
schema="api/graphql/apollo.observed.graphql"
report="docs/reference-platform/observed/reference-graphql-completeness.json"

# A copy of the repository's evidence, so a planted violation never touches the
# working tree. git checkout would restore the source; this cannot reach it.
stage() {
  rm -rf "$work/tree"
  mkdir -p "$work/tree/scripts" "$work/tree/docs/reference-platform/observed" "$work/tree/api/graphql"
  cp -r "$repo_root/scripts/apollo-graphql" "$work/tree/scripts/apollo-graphql"
  cp "$repo_root/$observed" "$work/tree/$observed"
  cp "$repo_root/$schema" "$work/tree/$schema"
  cp "$repo_root/$report" "$work/tree/$report"
}

expect_reject() {
  local name="$1"
  if (cd "$work/tree" && node scripts/apollo-graphql/verify-observed-schema.mjs >/dev/null 2>&1); then
    echo "FAIL: the gate accepted ${name}" >&2
    exit 1
  fi
  printf '  rejected: %s\n' "$name"
}

echo "verify-apollo-graphql: the gate must accept the committed evidence"
stage
(cd "$work/tree" && node scripts/apollo-graphql/verify-observed-schema.mjs >/dev/null)
echo "  accepted: the committed evidence"

echo "verify-apollo-graphql: and refuse each of these"

# Tenant material, one shape per case. Each is something a careless paste or a
# future edit could plausibly introduce.
stage && python3 - "$work/tree/$observed" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["diagnostics"].append("probed https://tenant.example.palantirfoundry.com/graphql")
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a Hub origin in the diagnostics"

stage && python3 - "$work/tree/$observed" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["diagnostics"].append("failed for ri.compass.main.folder.0123456789abcdef")
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a tenant RID in the diagnostics"

stage && python3 - "$work/tree/$observed" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["diagnostics"].append("Authorization: Bearer eyJhbGciOiJFUzI1NiJ9.abcdefghijklmnop.signature")
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a bearer token in the diagnostics"

stage && python3 - "$work/tree/$observed" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["diagnostics"].append("captured by operator@example.test")
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "an email address in the diagnostics"

# Drift: the SDL must be exactly what the record derives, in both directions.
stage && printf '\ntype ApolloInventedType {\n  invented: String\n}\n' >> "$work/tree/$schema"
expect_reject "a hand-added type in the SDL that the record does not derive"

stage && python3 - "$work/tree/$observed" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
for t in d["types"]:
    if t["name"]=="ApolloQuery":
        t["fields"].append({"name":"invented","shape":"scalar","arguments":[],"returns":None})
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a field added to the record without rebuilding the SDL"

# Provenance: a report that describes a different capture must not ride along.
stage && python3 - "$work/tree/$report" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["candidatesSha256"]="0"*64
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a report produced from a different candidate set"

stage && python3 - "$work/tree/$report" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["hubFingerprint"]="0"*64
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a report describing a different Hub"

# The claim itself: the method is what bounds what this evidence can support.
stage && python3 - "$work/tree/$observed" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["method"]="full-introspection"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a record claiming a recovery method the tooling did not use"

echo "verify-apollo-graphql: all cases behaved"
