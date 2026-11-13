# Operations documentation rules

This directory records how Conduit work is evaluated and promoted. It does not
grant authority to modify a tenant, publish an artifact, run paid CI, merge a
pull request, or deploy a release.

Read `TENANT-FIDELITY.md` before Apollo-facing changes. Keep domain findings in
the relevant contract or operations note and track remaining work in GitHub
issues. Verify changes with the applicable executable gates before promotion.

Release and publication are manual, and no automated trigger may perform them.
Automated triggers are permitted for verification and maintenance workflows
under ADR 0006's guardrails, and the Trunk gate runs on `pull_request` under
them (#195); dispatch it only to re-run an unchanged tree. Follow
`manual-ci.md`, `local-verification.md`, and `release-and-publication.md`. Never place a token,
private key, tenant payload, or `.env` value in a document, log, fixture, or
command transcript.
