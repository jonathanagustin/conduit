# Production readiness

The go/no-go record for running Conduit in production. Every `READY` row
cites the executable gate or committed evidence that proves it — a row
without a passing check does not belong here. The residual items at the
bottom are decisions or actions only a human operator can take; nothing in
this repository can complete them.

Status date: 2026-08-15, trunk `7a4b487`.

## Verified by gates

| Area | Claim | Evidence |
| --- | --- | --- |
| Correctness | All 26 local gates pass at the trunk tip (tests, vet, lint, formatting, domain boundaries, controlplane ceiling, contract validation, GitOps acceptance). | `./scripts/verify-local.sh` — "all CI gates passed locally"; hosted `Trunk gate` status green at the same tree (run 31866216577). |
| Availability | The Hub runs two replicas over a transactional PostgreSQL repository, with a PodDisruptionBudget protecting one replica; concurrent lease, approval, idempotency, and crash-recovery are proven against real PostgreSQL. | A7; `scripts/test-postgres-repository.sh`; `ruby scripts/verify-helm-runtime.rb` (aborts unless replicas == 2 and the PDB exists). |
| Identity | Production identity is OIDC for operators and mTLS or SPIFFE for Agents. Insecure identity headers exist only behind explicit `ALLOW_INSECURE_*` opt-ins; `cmd/platform-delivery` exits rather than combine them with any secure mechanism, and the chart gate aborts if any `ALLOW_INSECURE_` variable is set. | `cmd/platform-delivery/main.go` startup checks; `verify-helm-runtime.rb` ("chart enabled insecure identity"). |
| Delivery | A clean production cluster reconciles the signed private runtime artifact from OCI with no Git source or credential: pull-only registry authority, trusted-signature success, wrong-signer rejection, exact-digest application. | A8; `scripts/test-gitless-clean-cluster.sh`; ADR 0001. |
| Reconciliation | Tenant intent converges through Conduit-owned durable work (leases fenced by input revision, atomic decision/audit/outbox, crash-safe receipts); Flux owns only runtime convergence. | A9; ADR 0003; `scripts/test-postgres-repository.sh`. |
| Air gap | The Zarf package assembles and validates for disconnected installation. | `Validate Zarf air-gap package` gate. |
| Supply chain | Every workflow action, container base, and tool is digest- or checksum-pinned; images carry SBOM and vulnerability evidence with zero high/critical matches; Hadolint and the trusted-technology policy gate the Containerfiles. | `Validate trusted supply-chain policy`, `Scan for known vulnerabilities`, `Lint Containerfiles` gates; `docs/evidence/highs-1.15.1-shim-candidate-amd64.json`. |
| CI provenance | Publication, release, and deployment run only on explicit human dispatch; verification and maintenance workflows may be automated under ADR 0006's guardrails (cancel superseded runs, cap runtime, least-privilege, never publish). The Trunk gate stays dispatch-based, authenticates the exact pull-request head, and publishes a commit status; each run writes a per-gate summary with failure annotations. | A4 (ADR 0006); `docs/operations/manual-ci.md`; `.github/workflows/platform-ci.yml`; `scripts/verify-pr-fidelity.test.sh`. |
| Frontend | The operator console ships one tenant-neutral bundle; tenant runtime overrides pass two independent closed validators that reject executable content and policy weakening; browser tests cover URL-owned pagination state. | A3; `Validate operator console` gate; `docs/ui/contracts/runtime-bootstrap.md`. |
| Fidelity boundary | No private Apollo behavior is inferred: the public OpenAPI is authoritative only for published operations, and the private Hub-to-Agent protocol remains an explicit non-claim. | A1, A2; H9 (BLOCKED by design); `docs/apollo/conformance.md`. |

## Explicitly out of production scope

- **HiGHS optimizer candidate** — `UNADMITTED` by construction. The native
  backend serves production; the candidate runs shadow-only. Admission is a
  separate release decision with its own four open gates recorded in
  `docs/evidence/highs-1.15.1-shim-candidate-amd64.json`.
- **File-mode repository** — supports exactly one replica (chart enforces
  `Recreate` and `replicas: 1`); production uses the PostgreSQL repository.

## Residual items owned by a human operator

1. **Release instruction.** Passing CI does not authorize release,
   publication, or deployment (A5). Follow
   `docs/operations/release-and-publication.md`: pick the commit and tag,
   dispatch the release gate, publish immutable digests with SBOM,
   provenance, signature, and attestation, and verify from a clean client.
2. **Production secrets.** Provision in the target environment: PostgreSQL
   DSN, TLS certificate and key (or a SPIFFE workload endpoint), the Agent
   client CA, the Bundle signing key, OIDC issuer configuration, and the
   bootstrap administrator list. The chart refuses to render a production
   Hub without them.
3. **Renovate activation.** `renovate.json` is committed and gated; the
   Renovate GitHub App still needs an admin install on the repository
   before update PRs and the Dependency Dashboard appear.
4. **Tenant deployment authorization.** Deploying to any tenant requires
   its own explicit approval through the owning deployment repository;
   a successful build or upload never authorizes it.

Update this document in the same change as anything that invalidates a row,
and treat a stale row exactly like a failing gate.
