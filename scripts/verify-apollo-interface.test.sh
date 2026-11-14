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

observed="docs/reference-platform/observed/reference-graphql-observed.json"
schema="api/graphql/apollo.interface.graphql"
report="docs/reference-platform/observed/reference-interface-provenance.json"
native="scripts/apollo-graphql/conduit-native.json"
manifest="docs/reference-platform/public-documentation-manifest.tsv"
cli="docs/reference-platform/cli-command-contract.json"
# The pinned OpenAPI snapshot every `apollo-openapi X.y` citation resolves
# against. Found rather than hard-coded, so pinning a newer capture does not
# silently stage an empty file and turn every citation check into a no-op.
openapi="$(cd "$repo_root" && ls -d docs/reference-platform/snapshots/reference-openapi-*/api-definition.yml | head -1)"
if [[ -z "$openapi" ]]; then
  echo "FAIL: no pinned OpenAPI snapshot found; the citation checks cannot run" >&2
  exit 1
fi

stage() {
  rm -rf "$work/tree"
  mkdir -p "$work/tree/scripts" "$work/tree/docs/reference-platform/observed" "$work/tree/api/graphql" \
    "$work/tree/$(dirname "$openapi")"
  cp -r "$repo_root/scripts/apollo-graphql" "$work/tree/scripts/apollo-graphql"
  # The manifest and the OpenAPI snapshot are staged because the gate verifies
  # citations against them. Omitting them would make every citation unresolvable
  # and every case below reject for that reason instead of its own -- the whole
  # harness would pass while testing nothing.
  for file in "$observed" "$schema" "$report" "$manifest" "$openapi" "$cli"; do
    cp "$repo_root/$file" "$work/tree/$file"
  done
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
accepted="$(run)"
echo "  accepted: the committed interface"

# Beta citations are disclosed, not refused. Asserting the disclosure here is
# what stops it from being quietly dropped: a NOTE that vanishes looks exactly
# like a NOTE that had nothing to say, and the two mean opposite things.
# ApolloEntity.health and ApolloEnvironment.health both cite the Apollo Product
# Specification Health page, which upstream marks [Beta] -- "may not be
# available on your Apollo Hub".
if [[ "$accepted" != *"cite a page Apollo marks [Beta]"* ]]; then
  echo "FAIL: the gate no longer discloses citations that rest on Beta documentation:" >&2
  echo "$accepted" >&2
  exit 1
fi
if [[ "$accepted" != *"ApolloEntity.health"* ]]; then
  echo "FAIL: the Beta disclosure does not name the coordinates it covers:" >&2
  echo "$accepted" >&2
  exit 1
fi
echo "  disclosed: 2 documented coordinates rest on Beta documentation"

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

# --- citations ---------------------------------------------------------------
#
# A tier is a claim about a source. These prove the source is checked rather than
# trusted, because a citation nobody verifies is decoration: it reads as evidence
# and constrains nothing, which is worse than an honest CONDUIT_NATIVE.

stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloPlan"]["fields"][0]["documentedBy"]="https://www.palantir.com/docs/apollo/invented-page/"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "documentation cited at a URL the manifest does not carry" "invented-page"

stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloEntity"]["fields"][1]["publishedAs"]="apollo-openapi Installation.versionThatApolloDoesNotPublish"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a published name the pinned OpenAPI does not declare" "versionThatApolloDoesNotPublish"

# THE CASE THAT CAUGHT A REAL DEFECT. The first version of the OpenAPI check
# collected every property name in the document into one flat set, so a citation
# naming the wrong type passed: Environment declares only rid, name,
# environmentType and status, yet `apollo-openapi Environment.productId` was
# accepted because some OTHER schema declares productId. A citation check that
# does not check the type only asserts that Apollo uses the word somewhere.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
for f in d["types"]["ApolloPromotion"]["fields"]:
    if f["name"]=="productId":
        f["publishedAs"]="apollo-openapi Environment.productId"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a real property cited on the wrong type" "must belong to the type cited"

stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
for f in d["types"]["ApolloPromotion"]["fields"]:
    if f["name"]=="productId":
        f["publishedAs"]="apollo-openapi NoSuchSchema.productId"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a citation of a schema the contract does not declare" "no schema named"

# The CLI contract is checked the same way, down to the individual flag.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
for f in d["types"]["ApolloChangeRequest"]["fields"]:
    if f["name"]=="title":
        f["publishedAs"]="apollo-cli changerequest list --no-such-flag"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a flag the captured CLI contract does not carry" "Do not claim a flag the CLI does not carry"

stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
for f in d["types"]["ApolloChangeRequest"]["fields"]:
    if f["name"]=="title":
        f["publishedAs"]="apollo-cli changerequest invent --title"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a citation of an apollo-cli command that was never captured" "records no command"

# Prose. Six citations sat in this file in forms nothing could resolve --
# "rid convention", a bare "apollo-openapi status" -- reading as evidence and
# constraining nothing. An unresolvable citation now fails rather than being
# silently skipped, which is what let them survive.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
for f in d["types"]["ApolloChangeRequest"]["fields"]:
    if f["name"]=="title":
        f["publishedAs"]="apollo publishes this name, obviously; title"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a citation in no verifiable form" "in no verifiable form"

stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloPlanPage"]["fields"][1]["publishedAs"]="apollo-graphql ApolloEnvironmentPage.nextPageTokenInvented"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a GraphQL citation of a coordinate the recovery record lacks" "recovery record"

# The rule that makes displacement mean something. Citing a name Apollo
# publishes while serving a different one is the exact state this whole tier was
# built to end: the evidence would sit beside the invention rather than replace
# it.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
field=d["types"]["ApolloEntity"]["fields"][1]
field["name"]="installedVersion"          # Conduit's invented name, kept...
field["publishedAs"]="apollo-openapi Installation.version"   # ...while citing Apollo's
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a field citing Apollo's published name while serving its own" "that is what displacement means"

# patternedOn is the weaker claim and must stay weaker. A citation carrying the
# field's own name is really a published name, and filing it under the weaker
# word would blur the two.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
d["types"]["ApolloPlanPage"]["fields"][0]["patternedOn"]="apollo-graphql ApolloEnvironmentPage.environments, ApolloPlanPage.plans"
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a pattern citation carrying the field's own name" "say so with publishedAs"

# A pattern establishes the shape, never the noun. Claiming one without citing
# the noun would let any invented name ride in on Apollo's paging convention.
stage && python3 - "$work/tree/$native" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]))
del d["types"]["ApolloPlanPage"]["fields"][0]["documentedBy"]
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a pattern claimed with nothing cited for the field's own name" "establishes the shape, not the noun"

# Provenance accounting must describe this interface, not an earlier one.
#
# The wrong count is 99 rather than 0 because 0 is the true count today: every
# served field traced to an Apollo source. Mutating it to the real value would
# have made this case assert nothing.
stage && python3 - "$work/tree/$report" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["conduitNativeFields"]=99
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a provenance report that miscounts the uncited surface" ""

stage && python3 - "$work/tree/$report" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["documentedFields"]=0
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a provenance report that miscounts the documented surface" ""

stage && python3 - "$work/tree/$report" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); d["hubFingerprint"]="0"*64
json.dump(d,open(sys.argv[1],"w"),indent=2)
PY
expect_reject "a provenance report describing a different Hub" ""

echo "verify-apollo-interface: all cases behaved"
