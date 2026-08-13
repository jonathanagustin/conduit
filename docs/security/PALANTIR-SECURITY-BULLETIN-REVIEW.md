# Palantir security bulletin review

Reviewed 2026-08-02. This note records derived controls; it does not reproduce
the bulletin bodies or claim that Platform is Palantir software.

## Evidence boundary

- [`palantir/security-bulletins`](https://github.com/palantir/security-bulletins)
  was shallow-cloned at commit
  `a9b4b2f3029cd83aca2c8a9ea73fa5276008e398`. All 16 bulletins present at that
  commit, from PLTRSEC-2022-01 through PLTRSEC-2023-01, were reviewed.
- [`palantir/k8s-spark-scheduler`](https://github.com/palantir/k8s-spark-scheduler)
  was shallow-cloned at commit
  `acc007d3a5d7a6b863820f20b194d681f1058a06`. It is archived and describes a
  generic Spark scheduler extender, not a current or complete Rubix contract.
  Its `com.palantir.rubix/instance-group` fixture corroborates node-placement
  vocabulary only.
- [`palantir/k8s-spark-scheduler-lib`](https://github.com/palantir/k8s-spark-scheduler-lib)
  was shallow-cloned at commit
  `0e821b8dbe3f0fb1c1d9ea7c366fa4c9a911d414`. It is also archived. Its
  namespaced `Demand` and `ResourceReservation` APIs corroborate separation of
  desired capacity from observed fulfillment, but their CRDs and conversion
  webhook still require cluster bootstrap ownership.
- [`palantir/palantir-cloudpak`](https://github.com/palantir/palantir-cloudpak)
  was shallow-cloned at commit
  `4d087b0b78e0cbd1bc9856d3d830186acac51c6f`. It is an IBM CASE bundle for an
  OpenShift-era full-Foundry installer, not a Helm chart or a current Rubix
  contract. It corroborates SLS/Rubix metadata vocabulary and external
  storage, CA, registry, registration, and encryption inputs.

The bulletins establish recurring failure classes and Apollo's role in rolling
out remediations. They do not disclose Apollo's private Hub-to-Agent protocol,
Rubix cluster bootstrap, or an authorization wire format.

## Bulletin disposition

| Bulletin | Disclosed failure class | Platform disposition |
| --- | --- | --- |
| PLTRSEC-2022-01 | Session tokens written to service logs | COVERED: the Hub does not log requests or headers; secrets remain external; Agent diagnostics are bounded and redact common credential/PEM forms. Token revocation and log retention remain identity-provider/log-platform responsibilities. |
| PLTRSEC-2022-02 | Unauthenticated request paths enabling authentication-service denial of service | COVERED/BOUNDARY: production control routes require verified identities, request bodies and server timeouts are bounded, and the chart must expose only HTTPS. Distributed rate limiting and edge denial-of-service protection belong to the Environment front door. |
| PLTRSEC-2022-03 | Authorization headers and Basic/Kerberos credentials written to connector logs | COVERED/BOUNDARY: Platform never logs HTTP headers; external material-provider stdout and stderr are discarded. Product connector logging and credential rotation remain Product/Environment responsibilities. |
| PLTRSEC-2022-04 | Browser execution of attacker-controlled uploaded content | FIXED: authenticated blob responses now force attachment handling, prevent MIME sniffing, use a restrictive CSP, and forbid shared caching. `TestArtifactHTTPUploadVerifyAndRead` is the gate. |
| PLTRSEC-2022-05 | Apollo deployment and ownership metadata readable without authentication | FIXED: every one of the pinned 13 `/api/v0` operations now requires an operator or exact Environment Agent identity. Status writers cannot cross Environments. `TestApolloPublicAPIEveryOperationRequiresAuthenticatedIdentity` and `TestApolloPublicAPIStatusIdentityCannotCrossEnvironments` are the gates. |
| PLTRSEC-2022-06 | Arbitrary customer data included in Spark exception logs | BOUNDARY: Platform does not collect workload logs. It persists only bounded/redacted execution diagnostics attributable to a Plan. Product exception formatting and log deletion remain outside the delivery control plane. |
| PLTRSEC-2022-07 | A node log collector copied malformed source lines containing tokens | BOUNDARY: Rubixbeat/Libbeat is not part of Platform and the runtime chart must not install a cluster-wide log collector. The Environment logging owner must redact before export and govern historical deletion. |
| PLTRSEC-2022-08 | Interactive commands and tokens written by a workload to central logs | BOUNDARY: Code-Workbooks is a Product concern. Platform does not ingest console commands or workload logs and does not broaden access to them. |
| PLTRSEC-2022-09 | TLS peer hostname not verified | COVERED: Go's normal hostname verification is retained for static TLS; SPIFFE mode authorizes the exact Hub ID. No transport sets `InsecureSkipVerify`. |
| PLTRSEC-2022-10 | Unauthenticated active-user enumeration | COVERED by the same exhaustive API authentication gate; Platform exposes no unauthenticated principal/team/catalog listing. Liveness, readiness, build identity, validation, and Bundle public-key/preview utilities deliberately contain no stored user inventory. |
| PLTRSEC-2022-11 | Unauthenticated unbounded logging endpoint exhausted memory | COVERED: no unauthenticated logging endpoint exists; JSON inputs are strictly decoded under byte limits and the HTTP server has read/write/header timeouts. |
| PLTRSEC-2022-12 | Crafted archive upload exhausted memory | COVERED: large artifacts stream into content-addressed storage under a hard limit; structured Bundle and validation inputs have much smaller limits. Archive interpretation occurs only in governed Product tooling, not the Hub blob endpoint. |
| PLTRSEC-2022-13 | TLS hostname verification omitted in an FTP connector | BOUNDARY/COVERED: connector behavior is Product-owned; Platform transports retain peer verification. |
| PLTRSEC-2022-14 | TLS hostname verification omitted in a logging library | BOUNDARY/COVERED: logging libraries are Product-owned; Platform transports retain peer verification. |
| PLTRSEC-2022-15 | TLS hostname verification omitted in a database client | BOUNDARY/COVERED: database clients are Product-owned; Platform transports retain peer verification and mTLS does not substitute for hostname/identity verification. |
| PLTRSEC-2023-01 | Database password override leaked through controller configuration logs | COVERED/BOUNDARY: secret values never enter desired state, Plans, or Hub persistence; external material-provider output is discarded. Database credential generation and rotation remain Environment/provider responsibilities. |

## Cross-bulletin delivery consequence

Many bulletins describe Apollo both upgrading affected services and preventing
the vulnerable versions from being deployed again. Platform already models
scanner-derived security recalls, but Entity commands intentionally bypass
ordinary recall selection. The review therefore introduced one narrower hard
stop: install/reconcile commands cannot deploy a Release covered by a
`SECURITY` recall, including an automatic scanner-derived recall. Manual
incident recalls remain explicitly overrideable, while uninstall and restart
commands remain available for containment. The executable gate is
`TestCommandCannotDeploySecurityRecalledRelease`.

## Rubix scheduler consequence

The archived scheduler source supports gang scheduling, FIFO driver ordering,
resource reservations, dynamic executor allocation, bin-packing strategies,
AZ placement, and configurable instance-group labels. Its example also creates
a cluster binding and explicitly calls that example a super user. The library
adds typed CPU/memory/GPU demand units, Pod-based deduplication, ephemeral
immutable versus long-lived mutable demand, single-zone intent, fulfillment
phases and transition time, and per-node reservations bound to Pod names. It
also explicitly warns that a fulfilled zone does not guarantee where a Pod is
scheduled.

Platform therefore treats `com.palantir.rubix/instance-group` as a
consumer-supplied affinity value only. Affinity does not assert available
capacity, make a reservation, or guarantee an availability zone. Platform must
not ship the scheduler, scaler, CRDs, conversion webhook, a second
`kube-scheduler`, node mutation, or cluster-wide RBAC. Those are cluster
bootstrap responsibilities; Kubernetes scheduling and Pod status remain the
reported-state authority.

## Cloud Pak operator consequence

The Cloud Pak CASE launches a privileged Palantir operator that creates several
namespaces and has broad ClusterRole/ClusterRoleBinding authority, including
namespace, cluster-RBAC, and workload mutation. That is bootstrap ownership and
must not be copied into Platform's Hub or Spoke charts. The CASE also uses
mutable image tags, deprecated beta architecture labels, and an annotation that
disables status-sidecar TLS verification; none crosses into Platform.

The operator Deployment carries `com.palantir.sls.status/*` metadata and a
`com.palantir.rubix.pod/sls-service-info-v2` Pod annotation, but it also serves
an SLS-specific `/<context>/status/health` response. Annotation shape alone is
therefore insufficient to claim SLS integration. Platform keeps standard HTTPS
Kubernetes probes and does not advertise the private SLS contract until an
authorized payload fixture and sidecar conformance gate are available.

Portable configuration lessons do cross the boundary: exact external storage,
CA, registry, identity/registration, and encryption inputs; restricted Pod
execution; explicit architecture/placement; and separation between runtime
packaging and cluster bootstrap. Platform implements the relevant subset
without installing a full-platform operator.
