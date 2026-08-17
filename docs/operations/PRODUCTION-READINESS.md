# Production readiness

The go/no-go record for running Conduit in production. Every `READY` row
cites the executable gate or committed evidence that proves it — a row
without a passing check does not belong here. The residual items at the
bottom are decisions or actions only a human operator can take; nothing in
this repository can complete them.

Status date: 2026-08-17, trunk `c53723e`.

**Scope.** Two different questions get asked of this document and they have
different answers. *Can Conduit run as a delivery control plane in production?*
is what the rows below answer. *Can Conduit replace Apollo?* is a larger
question the [blocking gaps](#blocking-gaps) and the Apollo parity backlog
answer, and nothing here should be read as claiming it.

## Verified by gates

| Area | Claim | Evidence |
| --- | --- | --- |
| Correctness | All 30 local gates pass at the trunk tip (tests, vet, lint, formatting, domain boundaries, controlplane ceiling, contract validation, GitOps acceptance). | `./scripts/verify-local.sh` — "all CI gates passed locally"; hosted `Trunk gate` status green at the same tree. **Qualified by #128**: the hosted Apollo step runs fewer gates than the local script, so a green Trunk gate is not yet equivalent to a green `verify-local.sh`. |
| Availability | The Hub runs two replicas over a transactional PostgreSQL repository, with a PodDisruptionBudget protecting one replica; concurrent lease, approval, idempotency, and crash-recovery are proven against real PostgreSQL. | A7; `scripts/test-postgres-repository.sh`; `ruby scripts/verify-helm-runtime.rb` (aborts unless replicas == 2 and the PDB exists). |
| Identity | Production identity is OIDC for operators and mTLS or SPIFFE for Agents. Insecure identity headers exist only behind explicit `ALLOW_INSECURE_*` opt-ins; `cmd/platform-delivery` exits rather than combine them with any secure mechanism, and the chart gate aborts if any `ALLOW_INSECURE_` variable is set. | `cmd/platform-delivery/main.go` startup checks; `verify-helm-runtime.rb` ("chart enabled insecure identity"). |
| Delivery | A clean production cluster reconciles the signed private runtime artifact from OCI with no Git source or credential: pull-only registry authority, trusted-signature success, wrong-signer rejection, exact-digest application. | A8; `scripts/test-gitless-clean-cluster.sh`; ADR 0001. |
| Reconciliation | Tenant intent converges through Conduit-owned durable work (leases fenced by input revision, atomic decision/audit/outbox, crash-safe receipts); Flux owns only runtime convergence. | A9; ADR 0003; `scripts/test-postgres-repository.sh`. |
| Air gap | The Zarf package assembles and validates for disconnected installation. | `Validate Zarf air-gap package` gate. |
| Supply chain | Every workflow action, container base, and tool is digest- or checksum-pinned; images carry SBOM and vulnerability evidence with zero high/critical matches; Hadolint and the trusted-technology policy gate the Containerfiles. | `Validate trusted supply-chain policy`, `Scan for known vulnerabilities`, `Lint Containerfiles` gates; `docs/evidence/highs-1.15.1-shim-candidate-amd64.json`. |
| CI provenance | Publication, release, and deployment run only on explicit human dispatch; verification and maintenance workflows may be automated under ADR 0006's guardrails (cancel superseded runs, cap runtime, least-privilege, never publish). The Trunk gate stays dispatch-based, authenticates the exact pull-request head, and publishes a commit status; each run writes a per-gate summary with failure annotations. | A4 (ADR 0006); `docs/operations/manual-ci.md`; `.github/workflows/platform-ci.yml`; `scripts/verify-pr-fidelity.test.sh`. |
| Frontend | The operator console ships one tenant-neutral bundle; tenant runtime overrides pass two independent closed validators that reject executable content and policy weakening; browser tests cover URL-owned pagination state. | A3; `Validate operator console` gate; `docs/ui/contracts/runtime-bootstrap.md`. |
| Fidelity boundary | No private Apollo behavior is inferred: the public OpenAPI is authoritative only for published operations, and the private Hub-to-Agent protocol remains an explicit non-claim. | A1, A2; H9 (BLOCKED by design); `docs/apollo/conformance.md`. |

## Blocking gaps

Found by audit on 2026-08-17 and not covered by any row above. Each cites what
was checked, because "we should probably have X" is not a finding.

### Tier 1 — blocks any production use

1. **Nothing delivers notifications.** `internal/notifications/domain` exists;
   there are no ports, no store, no HTTP surface, no emit sites and no console
   inbox. `PUBLIC-DOCUMENTATION-COVERAGE.md` states the consequence exactly: *"A
   Plan fails, a recall opens, a grant changes, and nobody learns unless they
   were already looking."* For a control plane that is an operability failure,
   not a missing feature.

2. **No observability.** No metrics, traces or `/metrics` endpoint anywhere in
   shipped code — the only Prometheus reference is
   `internal/optimization/providerprometheus`, which *reads* Prometheus as an
   optimizer input. Kubernetes startup, readiness and liveness probes exist in
   the Hub chart, so a dead pod is noticed; nothing exposes queue depth, lease
   contention, reconciliation lag, retry exhaustion or decision latency. Two
   replicas over a leased store with no telemetry cannot be operated: there is
   no way to tell working from wedged.

3. **No schema migration path.** `internal/delivery/reconciliation/adapters/postgres/store.go`
   creates seven tables with `CREATE TABLE IF NOT EXISTS` and carries no
   `ALTER`, no schema version and no migration runner. First install works. The
   first release that changes a column has no defined upgrade, and no test
   upgrades a populated database from release N to N+1.

4. **No backup, restore or disaster-recovery procedure.** PostgreSQL holds
   desired state, decisions, the audit trail and the proposal outbox. No
   document in `docs/operations/` or `docs/architecture/` states a backup
   method, a restore drill, or an RPO/RTO.

### Tier 2 — blocks Apollo-parity production

5. **Recalls never reach Target Hubs** (#126). A recall is a source-side
   selection filter; `Recalls` is a field of `DesiredState`, not `Environment`,
   and none of the five `addBundleResource` call sites carries it. A Release
   recalled on the source keeps running on a disconnected target with no
   roll-off instruction, and `Source: SECURITY` provenance is lost.

6. **Unsafe rollback of stateful services is not representable** (#134). Apollo
   declares, at compile time, the schema set a service can read and write, the
   migration steps it supports, and a runtime-reported current version. Conduit
   models none of the three, so rolling a storage service back to a version that
   cannot read its on-disk schema is not refused — it cannot be expressed.

7. **Monitors are unmodelled** (#131). Apollo's product-declared, severity-
   carrying SLO monitors drive automatic recall with a stated roll-off strategy.
   Conduit has the word only as two mute permissions in the generated role table
   and an unrelated internal prober.

## Test levels

The point of listing these is to say what each gap needs, and to stop anyone
rebuilding what exists. **The existing posture is strong**: 230 test files and
1,355 test functions, thirteen `scripts/test-*.sh` integration and acceptance
harnesses, Playwright end-to-end and live suites for the console, and mutation
testing on both sides with committed scores.

| Level | Exists | Gap to close |
| --- | --- | --- |
| Unit | 1,355 Go test functions; mutation-scored | — |
| Domain boundary | `test-domain-boundaries.mjs`; `Reconcile` fails the build on interface/resolver disagreement | — |
| Contract | Pinned OpenAPI adapter (13/13 routes), CLI fixtures, Module definitions, provenance gates | — |
| Integration | `test-postgres-repository.sh` against real PostgreSQL: leases, fencing, atomic commits, crash recovery | — |
| Acceptance | `test-gitless-clean-cluster.sh`, `test-gitops.sh`, `test-spoke-e2e.sh` | — |
| Browser E2E | Playwright `e2e/` and `live/` suites | — |
| **Upgrade** | none | Restore a database written by release N, run release N+1, assert no data loss and no manual step. Gap 3. |
| **Restore drill** | none | Back up, destroy, restore, converge; assert the audit trail and outbox survive. Gap 4. |
| **Delivery E2E** | none | Emit an event, assert it reaches a subscriber on each channel and appears in the inbox. Gap 1. Cannot be written until the ports exist. |
| **Observability** | none | Assert the metrics an operator needs are exported and move under load. Gap 2. Nothing to assert against today. |
| **Load and soak** | concurrency tests exist in `plan`, `postgres` and `maintenance` | Sustained multi-replica reconciliation: lease contention, queue depth under backlog, no starvation. |
| **Failure injection** | wrong-signer and permission refusals are covered | PostgreSQL failover mid-transaction, agent disconnect mid-plan, partial network. |
| **Bundle round-trip** | export/import covered | A recall issued after a Bundle is built, asserted to reach the target. Gap 5; blocked on the design decision in #126. |

Two of these cannot be written yet and saying so is the point: the delivery and
observability suites have nothing to assert against until the code exists, and
the recall round-trip is blocked on a decision rather than on effort.

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
