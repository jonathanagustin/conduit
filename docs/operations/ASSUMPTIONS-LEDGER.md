# Assumptions ledger

`ASSUMED` entries cannot support an Apollo compatibility claim or production
default. Change a status only with the named check and update every dependent
document in the same change.

| ID | Claim | Status | Check or evidence | Propagates to |
| --- | --- | --- | --- | --- |
| A1 | The pinned public Apollo OpenAPI is authoritative only for its published operations and schemas. | CONFIRMED | `scripts/apollo-docs/validate-public-api.rb`; pinned snapshot under `docs/apollo/snapshots/` | Apollo adapter and generated interfaces |
| A2 | No committed evidence establishes Palantir's private Hub-to-Agent wire protocol. | CONFIRMED | `docs/apollo/PUBLIC-DOCUMENTATION-COVERAGE.md`; `docs/apollo/conformance.md` | Compatibility wording and Spoke protocol |
| A3 | A tenant-neutral frontend can accept bounded runtime presentation and feature data without tenant-authored code. | ASSUMED | Missing typed bootstrap schema, signature/trust decision, and browser integration test | Frontend delivery and tenant overrides |
| A4 | Repository-authored GitHub workflows must run only after a person dispatches them. | CONFIRMED | `.github/workflows/*.yml`; `docs/operations/manual-ci.md` | Trunk checks and publication |
| A5 | Passing CI does not authorize merge, release, publication, or deployment. | CONFIRMED | `AGENTS.md`; `CONTRIBUTING.md`; `docs/operations/release-and-publication.md` | All promotion paths |
| A6 | Existing rows in `metrics-ledger.jsonl` are historical measurements, not claims about the current commit. | CONFIRMED | Each row carries its own date and commit coordinate | Zenith review and reports |
| A7 | The default file repository supports one Hub replica; multi-replica operation needs a transactional repository. | CONFIRMED | `internal/controlplane/repository.go`; `docs/architecture/deployment-invariants.md` | Deployment topology |
| A8 | A clean production cluster can reconcile the signed private Conduit runtime artifact without any Git credential or Git source. | ASSUMED | Missing clean-cluster private-pull, wrong-signer rejection, and no-Git inventory acceptance test | Gitless GitOps production claim |
