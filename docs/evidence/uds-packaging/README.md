# UDS packaging reference corpus and acceptance plan

This first slice of #343 curates source evidence and freezes a proposed
held-out acceptance candidate. It does not complete application validation,
verify a receipt producer, authorize a build/deployment/restore, or establish
an independent benchmark result.

`corpus-v1.json` contains nine revision-pinned documents with exact content
hashes, observed licensing, source class, alternatives, rejected approaches and
limitations. `matrix-v1.json` defines ten acceptance checks and sixteen negative
cases. Static rendering/schema, package build, isolated integration, restore,
scan and authorization checks remain separate. Every application check is
`NOT_RUN`; runtime evidence coverage is zero.

## What the sources establish

TAK ADR 0023 explicitly identifies itself as retrospective. It describes a
post-install patch race in which first pods started without prepared
configuration, compares five adaptations and marks several alternatives as
reasoned rather than tested. Its workload-specific Pepr decision is an example,
not a default for other applications. The inspected integration template shows
CA/SSO declarations and monitoring conditional on API capabilities. Its smoke
test contains a no-SNI transport check; reading that test is not running it.
ADR 0014 records KVM/amd64, privileged KubeVirt, Android age, mesh/network and
physical-device limitations. No device was deployed here.

The selected normative target is UDS Core v1.15.0 at
`338b8dde2081b3e7aab12acbff225c3f2a0865b9`. TAK's example discusses Core 1.16.0
and Zarf 0.89.0; these are not silently substituted for the target. Requirements
include upstream-chart suitability, bounded justified exemptions, declarative
integration, dependency completeness, least-privilege networking, conditional
end-user SSO, application metrics, journey and upgrade tests. Applicability and
schema/tool compatibility still require actual validation.

Platform upgrade guidance recommends sequential minor upgrades with validation,
release-note review, staging and backup. It does not officially support Core
rollback. Application versions and migrations remain separate contracts.
The manual-backup example sets `snapshotVolumes: false`; Completed resource
backup is insufficient evidence for volume or external database recovery. A
restore test needs pre/post application data assertions and separate authority.

Epinio's pinned MCP reference already describes agent-facing API workflows.
Compare with that baseline; MCP alone is not novelty or approval authority.
Epinio documentation contributors are credited under CC-BY-4.0. GPL/AGPL
application/template implementation was not copied into Conduit runtime code.
The corpus uses references and original factual paraphrases; its root-license
observations are not an artifact license adjudication.

## Frozen candidate, not an approved blind trial

`held-out/valkey-v1.json` selects Valkey 8.1.3 at
`d063dff523340e5a075f7ddc436e0c2a43d04061`, a stateful key/value datastore,
materially different from TAK. Only tag metadata was queried before the
criteria freeze; no Valkey implementation or packaging solution was read.
The first freeze commit contains only the profile and its checksum. The exact
criteria digest is also pinned by the trusted checker, so changing the profile
and checksum together cannot relax this version's requirements.

The author selected the candidate and has general software knowledge. This is
not an independent blind assignment, and TAK/previously reviewed GeoServer
remain excluded. Maintainer/evaluator approval of the exact digest, an exposure
declaration and an independently recorded trial are prerequisites to any
held-out evaluation claim. Required criteria are operator/profile-owned,
outside generated output; a future approved revision is a separately reviewed
profile, not a generator edit to this frozen v1.

The current `internal-stateless/v1` generator cannot support this stateful case.
Its `schema-helm/v1` unsigned structural receipt uses another UDS CRD pin and
Zarf v0.65.1. Neither proves the selected target, a real UDS package, disconnected
operation, trustworthy production results or deployment permission. Application
correctness and runtime reproducibility are therefore `NOT_RUN`, not PASS.

Before a trial, bind tool versions/binary hashes, architecture, artifact/chart
and source/template digests, target/configuration, security/license thresholds,
data fixture and external trust/approval identities. Missing tools/databases,
unsupported capabilities, unknown migrations, malformed scans and unverified
provenance block admission. A successful package must produce both lintable
`zarf.yaml` and a selected-schema-valid UDS Package CR with `spec.sso`,
`spec.network.allow` and `spec.monitor`; empty/not-applicable sections require
operator-owned evidence rather than invented integrations.

## Reproduce the dataset checks

Run `python3 -I -B scripts/verify-packaging-reference-corpus.py` and
`python3 -I -B scripts/test-packaging-reference-corpus.py` from the repository.
The checker resolves files from its trusted source location, not a generated
working directory. It performs no network lookup, source execution, scanner,
package build or cluster action. PASS means the dataset contract is consistent;
it does not independently re-fetch or authenticate remote evidence.

Ten negative/contract tests cover frozen criteria tampering, forged PASS and
receipt summaries, relaxed/missing required checks, retrospective evidence
being promoted to runtime proof, mutable/unresolved source coordinates,
unsigned structural results being promoted to authority, missing negative
cases, known-example contamination and duplicate JSON keys. They run in the
existing pinned-Python packaging regression gate.

The matrix references existing planner/validator and real-Helm test sources.
Their protocol/structural evidence is explicitly distinct from generated
application runtime receipts. #341 owns assessment/generation integration,
#133 owns library inventory semantics and #134 owns migration/rollback safety.
This corpus does not create competing contracts for those issues.

No new cluster/device deployment, application runtime, package build or restore
is authorized by this change. Those remain separately authorized, isolated
operations with explicit unrun records until evidence exists.

## Corpus reconciliation and retained negative baseline

The corpus/criteria scope of #343 retains the original Valkey profile and its
exact frozen digest as an **unsupported negative baseline**. This is a deliberate
selection: it differs materially from TAK and exercises the current generator's
stateful-capability rejection. It is not selected as a supported success
benchmark. Its criteria have not been rewritten to accommodate the generator.
A supported application assignment, independent evaluator, exact-profile trial
approval and actual execution remain #639. No independent or successful
held-out result is claimed by retaining this nomination.

`matrix-v1.json` now records that disposition separately from the immutable
profile's unapproved authority. All sixteen negative cases have individual
`negativeCaseCoverage` records with status **NOT_RUN**, empty application
receipts, a concrete reason and #639 ownership. Some related protocol/unit
fixtures already exist in `existingEvidence`; those are not application trials.
For example, a subprocess timeout fixture does not run a real scanner, and an
unsigned structural receipt does not prove producer identity or an application
recovery drill. Keeping those levels separate prevents a green dataset gate
from silently becoming a green application report.

The licensing check for this corpus is explicit: the retained dataset consists
of original factual paraphrases, pinned references and license observations.
It does not vendor GPL/AGPL application implementation, the TAK integration
template or its smoke-test implementation. The nine source records each declare
`implementationCopied: false`, and the checker rejects promotion to copied
implementation. This is a bounded corpus-content review, not artifact license
adjudication or a resolution of the project's separate licensing decision #546.

The report keeps correctness and reproducibility **NOT_RUN**, runtime evidence
coverage **0**, all ten required application checks unrun and explicit failures.
Static schema/render checks, package builds, integration, restore, scanning and
admission have separate levels. The checker only verifies the dataset contract;
it does not measure application correctness. Four additional negative tests
reject missing/duplicated case records, protocol results promoted to application
receipts, accidental trial approval and copied-implementation declarations.
