# Hub–Agent security review commissioning brief

Version 1, 2026-11-17. **Status: NOT COMMISSIONED.** No reviewer, engagement,
authorization to test, schedule, report or successful outcome is asserted by
this brief. It prepares a concrete scope for the owner decisions in
[#846](https://github.com/jonathanagustin/conduit/issues/846); it does not replace
them. Source baseline:
[`de657c0266ece0e567b460d3d37aba1212658727`](https://github.com/jonathanagustin/conduit/tree/de657c0266ece0e567b460d3d37aba1212658727).

## Objective and supplied evidence

Independently assess whether Conduit's native Hub–Agent boundary prevents a
compromised Environment Agent from obtaining another Environment's work or
authority, and whether authenticated lease/report/Bundle exchanges preserve
their documented constraints. Examine implementation and assembled deployment,
including negative and concurrent behavior. This is not a request to certify
private Apollo protocol compatibility or an unspecified regulatory baseline.

Provide the reviewer the exact source and image digests, [threat model](THREAT-MODEL.md),
[control evidence](CONTROL-EVIDENCE.md), [Hub/Spoke contract](../architecture/hub-spoke.md),
[route authorization register](../operations/route-authorization.md),
[identity/governance limits](../architecture/identity-and-governance.md),
[audit custody procedure](../operations/authorization-audit-integrity.md), and a
sanitized rendered deployment with its trust/credential-role diagram. Existing
tests are useful starting points and are not the independent review result.
The [2026-08-18 audit](../operations/EXTERNAL-AUDIT-2026-08-18.md) explicitly could
not establish OIDC/mTLS correctness or protocol soundness; preserve it as
historical evidence and check current source instead of repeating stale gaps.

## Proposed attack and evidence matrix

| Area / threat IDs | Reviewer tasks and expected evidence |
| --- | --- |
| Credential separation, TM-01/02/03 | Test no credential, forged development headers, unverified/expired/wrong-CA certificates, wrong URI/trust domain, Agent-only SAN used as operator, operator-only SAN used as Agent, ambiguous/multiple SANs and mixed bearer/certificate carriers. Inspect CA issuance policy and ingress TLS termination. Show authorized positive cases beside exact denied responses. Review OIDC issuer/audience/purpose/freshness, issuer key rotation and delegation as relevant to operator access. |
| Environment and Agent scope, TM-02/03 | Seed A and B with existing Plans, Bundles and reports. As A's Agent, exercise ordinary and maintenance-admission lease/start routes, heartbeat/finish, health targets/observations, Plan discovery and all Bundle read/import/export paths. Try both known and guessed B identifiers, path/body mismatches and A/B Agent names. As a scoped operator, drive indexes, by-name reads and writes with B's rows present. Prove refusal is the intended authorization decision rather than an incidental missing-record response. |
| Lease replay and races, TM-04 | Replay empty, stolen, expired, cancelled and superseded tokens; vary Agent/Environment and attempt numbers. Exercise exact expiry, renewal/start/finish races, disconnect/reconnect and two Hub replicas sharing state. Attempt duplicate/reordered reports and observe database transitions, rollback and error receipts. Define which facts still rely on an honest authorized Agent. |
| Bundle/media boundary, TM-05/06 | Mutate manifest/layers/chunks/digests/signatures; substitute signer/target/Environment; replay checkpoints/imports and recalled Releases; vary admission/approval/security policy and missing bytes. Inspect transport/media permissions and signing-key custody. Separate valid-byte/signature evidence from authority to import and from trust in embedded assessments (#887). Keep the offline metadata UI service's process-local handoff/report freshness separate from installation proof. |
| Credential lifecycle, TM-01/05 | Evaluate the implementation actually landed for #847: issuance, overlapping trust roots, Agent/Environment revocation, certificate expiry, Bundle signing/trusted-key rotation and restart/replica behavior. Record absent or unmerged controls as such; do not commission against a hypothetical branch. |
| Cluster blast radius, TM-07 | Compare rendered Spoke RBAC with effective bindings and tenant chart kinds in the authorized test cluster. Attempt only agreed non-destructive privilege-boundary probes. Distinguish Hub cross-Environment authorization from local Kubernetes authority; a namespaced RBAC floor does not cap tenant-added privileges. |
| Audit and failure handling, TM-08/14 | Correlate denied/allowed decisions and actual outcomes without leaking credentials. Inject agreed audit/state failures, test concurrent mutation and verify exact persisted records. Exercise trusted-checkpoint verification after change/truncation/rollback; state what a database owner can rewrite without independent custody. Check approved recovery and rollback paths, not just happy-path HTTP status. |
| Console/model/ACP extensions, TM-09–13 | Trace any route through these surfaces that can reach Hub–Agent authority. Treat model/provider outputs as hostile and inspect action/approval binding. Current local CLI and optional ACP subprocess limitations remain explicit (#809/#647/#616). Expand active process-isolation testing only after the owner authorizes that additional scope. |

## Decisions required before commissioning

The accountable repository/deployment owner must select a reviewer with the
required independence and expertise, agree the engagement and findings process,
and authorize a concrete assessment environment. Record:

- Exact source/artifact digests, feature flags, identity carriers and issuer,
  proxy, database, registry, scanner and tenant-cluster boundaries in scope.
- Synthetic tenant/Environment/Agent roles, test credentials and safe resource
  ownership; explicit permitted techniques, rate/resource limits, outage windows,
  stop conditions, recovery owner and communication path. No production access
  is authorized by this document.
- Deliverable dates, assessor identity/independence, evidence handling and the
  private finding channel. Visibility, support/disclosure and licence decisions
  remain [#845](https://github.com/jonathanagustin/conduit/issues/845); do not invent
  a public disclosure policy or publish sensitive exploit material here.
- Whether assessment of a named control profile, operating environment or FIPS
  cryptographic boundary is requested. #846 must settle those requirements
  explicitly; a Go build flag or an example Environment label is insufficient.

Credential lifecycle #847, artifact-evidence admission #887 and audit
custody/retention decisions keep their own owners. The reviewer may assess their
landed behavior and report interactions; this brief does not decide them.

## Required independent deliverables and completion record

Ask for an independently authored report stating assessed versions and
configuration, methods, tested boundaries, reproducible sanitized evidence,
assumptions, excluded areas, severity rationale, residual risk and untested
questions. Include positive controls, earned authorization refusals, cross-replica
and replay results, and the precise limits of any no-finding conclusion. A green
repository test run alone is not this deliverable.

Each verified finding should be checked for duplicates and tracked in an issue
with affected coordinates, expected/actual behavior, evidence location, owner,
remediation or explicit decision, and a regression/retest criterion. Keep
credentials, tenant payloads and sensitive reproduction details in the
owner-approved restricted evidence store; use sanitized public summaries only
when authorized. Retest remediation against the original exploit and an
authorized positive case. Do not equate an issue's closure with independent
retest evidence.

After the review, the accountable owner should record the report's immutable
reference/digest, exact scope/versions, findings and dispositions, retest results,
and accepted residual risks under #846. Change **NOT COMMISSIONED** only when an
actual engagement exists, and distinguish commissioned, assessed, remediated and
accepted states. This brief and the documentation-only #901 do not establish
any of those states.
