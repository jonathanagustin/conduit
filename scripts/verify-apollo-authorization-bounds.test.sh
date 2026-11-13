#!/usr/bin/env bash
# Proves the Apollo authorization bounds gate rejects the drift it was written
# for, and does not reject the changes that are supposed to be routine.
#
# Both halves matter. The gate exists because three fields were hand-added to
# docs/evidence/apollo-authorization-graph.json after generation and nothing
# noticed for as long as nobody regenerated -- so the rejections below plant
# exactly that shape. But the artifacts are regenerated from a live tenant, and
# a gate that failed every time a count moved would be switched off within a
# week, so the acceptances below plant a tenant that changed and require the
# gate to stay quiet.
#
# Unlike the older self-tests in this directory, this one does NOT mutate the
# working tree and `git checkout` it back. Those skip themselves when the file
# they plant into is already dirty, which is exactly the state a change to these
# artifacts is reviewed in -- the gate would go untested precisely when it
# matters. This copies the four files into a temporary root instead and points
# the gate at it with APOLLO_BOUNDS_ROOT, so it is meaningful before a commit as
# well as after one.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-apollo-authorization-bounds.mjs"
graph="docs/evidence/apollo-authorization-graph.json"
vocabulary="docs/evidence/apollo-authorization-vocabulary.json"
graph_generator="scripts/capture-apollo-authorization-graph.mjs"
vocabulary_generator="scripts/extract-apollo-authorization-vocabulary.py"

sandbox="$(mktemp -d)"
trap 'rm -rf "${sandbox}"' EXIT

reset() {
  rm -rf "${sandbox:?}/docs" "${sandbox:?}/scripts"
  mkdir -p "${sandbox}/docs/evidence" "${sandbox}/scripts"
  cp "${graph}" "${vocabulary}" "${sandbox}/docs/evidence/"
  cp "${graph_generator}" "${vocabulary_generator}" "${sandbox}/scripts/"
  cp "${gate}" "${sandbox}/scripts/"
  gate_under_test="${gate}"
}

# Runs the real gate by default. One case below runs the sandbox's copy instead,
# because the only way to exercise the handAuthored escape hatch is to put
# something in it, and the committed list is empty on purpose.
run_gate() { APOLLO_BOUNDS_ROOT="${sandbox}" node "${gate_under_test}"; }

# Rewrite a JSON artifact in the sandbox with a snippet of node.
edit() {
  local file="$1" script="$2"
  node -e "
    const fs = require('node:fs');
    const path = process.argv[1];
    const doc = JSON.parse(fs.readFileSync(path, 'utf8'));
    (${script})(doc);
    fs.writeFileSync(path, JSON.stringify(doc, null, 2) + '\n');
  " "${sandbox}/${file}"
}

reject() {
  local description="$1"
  if run_gate >/dev/null 2>&1; then
    echo "FAIL: the gate accepted ${description}" >&2
    exit 1
  fi
  echo "  rejects: ${description}"
  reset
}

accept() {
  local description="$1"
  if ! run_gate >/dev/null 2>&1; then
    echo "FAIL: the gate rejected ${description}" >&2
    run_gate >&2 || true
    exit 1
  fi
  echo "  accepts: ${description}"
  reset
}

echo "verify-apollo-authorization-bounds: the gate must reject drift, not just pass"

reset

# --- the committed tree passes ----------------------------------------------
if ! run_gate >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed tree" >&2
  run_gate >&2 || true
  exit 1
fi
echo "  passes: the committed tree"

# --- THE drift class: a field in the artifact that no generator emits --------
# This is the shape that already happened, three times over, unnoticed.
edit "${graph}" "(d) => { d.rolesWithSomethingInteresting = ['apollo-hubs:administrator']; }"
reject "a field hand-added to the graph that the generator does not emit"

edit "${vocabulary}" "(d) => { d.observedByHand = 'a note somebody typed in'; }"
reject "a field hand-added to the vocabulary that the generator does not emit"

# --- the same drift in the other direction ----------------------------------
edit "${graph}" "(d) => { delete d.rolesWithNoTransitiveOperations; }"
reject "an artifact missing a field its generator emits"

# --- prose edited in the artifact and not in the generator -------------------
edit "${graph}" "(d) => { d.boundary = d.boundary.replace('Tenant-observed', 'Tenant observed'); }"
reject "a one-character prose edit to the graph boundary"

edit "${vocabulary}" "(d) => { d.notRecovered[0] = 'role -> operations edges.'; }"
reject "prose edited inside a generated list"

# --- the bound #122 asked for, deleted or hollowed out ----------------------
edit "${graph}" "(d) => { delete d.identifierPartition; }"
reject "the graph with its partition bound removed"

edit "${vocabulary}" "(d) => { delete d.identifierPartition; }"
reject "the vocabulary with its partition bound removed"

edit "${vocabulary}" "(d) => { d.identifierPartition.prefix = 'apollo'; }"
reject "a stated prefix that is not the prefix the generator filters on"

edit "${vocabulary}" "(d) => { d.identifierPartition.bound = 'The vocabulary lists apollo- identifiers.'; }"
reject "a bound reworded into a description of the file"

# --- the two artifacts must state the SAME bound ----------------------------
# Reword it in both copies of one side, so the byte-identity check above is
# satisfied and only the cross-artifact comparison can catch it.
edit "${graph}" "(d) => { d.identifierPartition.bound = d.identifierPartition.bound + ' Also.'; }"
node -e "
  const fs = require('node:fs');
  const p = process.argv[1];
  const s = fs.readFileSync(p, 'utf8');
  fs.writeFileSync(p, s.replace('which neither artifact attempts.\"', 'which neither artifact attempts. Also.\"'));
" "${sandbox}/${graph_generator}"
reject "the two artifacts stating different partition bounds"

# --- the count caveat -------------------------------------------------------
edit "${vocabulary}" "(d) => { delete d.countsCaveat; }"
reject "the vocabulary with its role-count caveat removed"

edit "${vocabulary}" "(d) => { d.countsCaveat = 'Counts are from one capture.'; }"
reject "a count caveat that drops the capture-date point"

edit "${vocabulary}" "(d) => { d.notRecovered = d.notRecovered.filter((e) => !e.includes('identifierPartition')); }"
reject "notRecovered no longer pointing at the partition"

# --- the drift the caveat describes must stay re-derivable ------------------
edit "${vocabulary}" "(d) => { d.vocabulary.roles.push('apollo-invented:role'); }"
reject "a vocabulary role the later graph capture does not carry"

# --- and now the counter-tests: routine change must NOT fail ---------------
# A gate that only ever says no is not tested. Every plant below is what a real
# re-capture of a moved tenant looks like.
edit "${graph}" "(d) => { d.counts.roles += 1; d.source.capturedAt = '2027-01-01T00:00:00.000Z'; }"
accept "a recapture with a different date and a moved count"

edit "${vocabulary}" "(d) => { d.contributingEndpoints[1].maps.roles.total = 173; }"
accept "the role total in contributingEndpoints moving"

edit "${graph}" "(d) => { d.graph.roles['apollo-environments:auditor'] = { id: 'apollo-environments:auditor', operations: ['environment:view-config-history'] }; }"
accept "a role the tenant added since the capture"

# A field added to the generator AND the artifact together is the supported way
# to extend these files, and it must pass without touching this gate.
node -e "
  const fs = require('node:fs');
  const p = process.argv[1];
  const s = fs.readFileSync(p, 'utf8');
  fs.writeFileSync(p, s.replace('  graphCloses:', '  captureLane: \"trunk\",\n  graphCloses:'));
" "${sandbox}/${graph_generator}"
edit "${graph}" "(d) => { d.captureLane = 'trunk'; }"
accept "a new field added to the generator and the artifact in the same change"

# --- the escape hatch has to work, or the gate becomes a wall ---------------
# handAuthored is committed empty. A maintainer who genuinely cannot emit a
# field from the generator must have a tested way through, otherwise the first
# person to hit this deletes the gate instead of the list. Patch the sandbox's
# copy of the gate, plant the field it now declares, and require acceptance.
node -e "
  const fs = require('node:fs');
  const p = process.argv[1];
  const s = fs.readFileSync(p, 'utf8');
  const patched = s.replace('    handAuthored: [],', '    handAuthored: [\"observedByHand\"],');
  if (patched === s) { console.error('self-test could not find handAuthored to patch'); process.exit(1); }
  fs.writeFileSync(p, patched);
" "${sandbox}/${gate}"
edit "${graph}" "(d) => { d.observedByHand = 'a note somebody typed in'; }"
gate_under_test="${sandbox}/${gate}"
accept "a hand-authored field once handAuthored declares it"

# --- restored, it passes again ----------------------------------------------
reset
if ! run_gate >/dev/null 2>&1; then
  echo "FAIL: the gate did not pass after the sandbox was restored" >&2
  exit 1
fi
echo "  passes: the restored tree"

echo "verify-apollo-authorization-bounds: all cases behaved"
