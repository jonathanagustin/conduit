#!/usr/bin/env bash
# Proves the displacement gate fires.
#
# The gate's whole purpose is that an invented definition cannot outlive the
# evidence that would replace it. A gate that passes today's clean tree proves
# the tree is clean; it says nothing about whether displacement would be
# noticed. So the central case below declares a Conduit-native field for a
# coordinate the Hub already answers, and requires the gate to refuse it by name.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

observed="docs/apollo/observed/apollo-graphql-observed.json"
schema="api/graphql/apollo.interface.graphql"
report="docs/apollo/observed/apollo-interface-provenance.json"
native="scripts/apollo-graphql/conduit-native.json"

stage() {
  rm -rf "$work/tree"
  mkdir -p "$work/tree/scripts" "$work/tree/docs/apollo/observed" "$work/tree/api/graphql"
  cp -r "$repo_root/scripts/apollo-graphql" "$work/tree/scripts/apollo-graphql"
  cp "$repo_root/$observed" "$work/tree/$observed"
  cp "$repo_root/$schema" "$work/tree/$schema"
  cp "$repo_root/$report" "$work/tree/$report"
}

run() { (cd "$work/tree" && node scripts/apollo-graphql/verify-apollo-interface.mjs 2>&1); }

expect_reject() {
  local name="$1" expect="$2"
  local output
  if output="$(run)"; then
    echo "FAIL: the gate accepted ${name}" >&2
    exit 1
  fi
  if [[ -n "$expect" && "$output" != *"$expect"* ]]; then
    echo "FAIL: rejected ${name} but without naming '${expect}':" >&2
    echo "$output" >&2
    exit 1
  fi
  printf '  rejected: %s\n' "$name"
}

echo "verify-apollo-interface: the gate must accept the committed interface"
stage
run >/dev/null
echo "  accepted: the committed interface"

echo "verify-apollo-interface: and refuse each of these"

# THE CENTRAL CASE. ApolloEnvironment.name is in the recovery record, so
# declaring it Conduit-native means an invention is standing where ground truth
# already exists. The gate must say so, and must name the coordinate.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloEnvironment"]["fields"].append(
    {"name":"name","returns":"String","servedBy":"invented while the Hub already answers"})
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "an invented field for a coordinate the Hub already answers" "ApolloEnvironment.name"

# The same, on the root type, to show it is not special-cased to one type.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloQuery"]["fields"].append(
    {"name":"environments","arguments":[{"name":"pageSize","type":"Int"}],
     "returns":"ApolloEnvironmentPage","servedBy":"invented"})
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "an invented root field the Hub already answers" "ApolloQuery.environments"

# Drift between the declarations and the served schema, in both directions.
stage && printf '\ntype ApolloInvented {\n  x: String @provenance(tier: CONDUIT_NATIVE)\n}\n' >> "$work/tree/$schema"
expect_reject "a hand-added type the declarations do not derive" "re-run build-interface-schema.mjs"

stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloPlan"]["fields"].append({"name":"invented","returns":"String","servedBy":"x"})
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a declaration added without rebuilding the schema" "re-run build-interface-schema.mjs"

# A field claiming to be recovered when the record does not contain it. This is
# the mislabelling the provenance annotation exists to prevent.
stage && python3 - "$work/tree/$schema" <<'PY'
import sys,re
p=sys.argv[1]; s=open(p).read()
s=s.replace("type ApolloBundle {\n","type ApolloBundle {\n  invented: String @provenance(tier: OBSERVED)\n",1)
open(p,"w").write(s)
PY
expect_reject "a field labelled OBSERVED that the recovery record does not contain" ""

# An invented field returning a type nothing declares would serve a shape no
# client can select against.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloPlan"]["fields"].append({"name":"ghost","returns":"ApolloTypeThatDoesNotExist","servedBy":"x"})
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "an invented field returning an undeclared type" ""

# Provenance accounting must describe this interface, not an earlier one.
stage && python3 - "$work/tree/$report" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["conduitNativeFields"]=0
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a provenance report that miscounts the invented surface" ""

stage && python3 - "$work/tree/$report" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["hubFingerprint"]="0"*64
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a provenance report describing a different Hub" ""

echo "verify-apollo-interface: all cases behaved"
