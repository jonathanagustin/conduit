# Operations documentation rules

This directory records how Conduit work is evaluated and promoted. It does not
grant authority to modify a tenant, publish an artifact, run paid CI, merge a
pull request, or deploy a release.

Read these files before a substantial change:

1. `TENANT-FIDELITY.md` for Apollo and tenant claims.
2. `ASSUMPTIONS-LEDGER.md` for facts that still need proof.
3. `CAPABILITY-MAP.md` and `HYPOTHESIS-BACKLOG.md` for current coverage and open work.
4. `PROCESS-IMPROVEMENTS.md`, `METRICS.md`, and `metrics-ledger.jsonl` for the
   repository-local Zenith record.

Use the vendored playbook at
`tools/zenith-protocol/skills/zenith-protocol/SKILL.md`. Preserve one writer per
file set, name the evidence and acceptance gate before implementation, and have
the orchestrator rerun the gate. A green Zenith conformance result proves only
that the ledgers are well formed.

Release and publication are manual, and no automated trigger may perform them.
Automated triggers are permitted for verification and maintenance workflows
under ADR 0006's guardrails; the Trunk gate itself stays dispatch-based. Follow
`manual-ci.md`, `local-verification.md`, and `release-and-publication.md`. Never place a token,
private key, tenant payload, or `.env` value in a document, log, fixture, or
command transcript.
