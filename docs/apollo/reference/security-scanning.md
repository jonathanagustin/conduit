# Security scanning boundary

Apollo's public material says vulnerability scans run for every container
declared in a Product manifest, while malware and artifact-availability
evidence may cover other Release artifacts. Tenant role evidence also separates vulnerability scanning,
third-party scan modification, suppression, SLA settings, and SBOM
administration. These are distinct authorization and provider concerns.

The local Hub invokes a provider-neutral `ArtifactScanner` only when explicitly
configured. An external executable receives Product/version plus immutable
chart and image coordinates. Vulnerability results must exactly cover the image
digest set; malware results must exactly cover the chart and images. It returns normalized vulnerability and/or malware
assessments; the Hub rejects output larger than 1 MiB, unknown JSON fields,
multiple JSON values, incomplete status, inconsistent PASS/FAIL counts,
duplicate/extra/missing digests, missing malware assessment time, failures, and
timeouts. Credentials, raw findings, scan definitions, and schedules never
enter desired state or repository storage.

## Suppression provenance

A suppressed count is not trusted on its own. Every suppressed finding must
carry a provider-stable finding ID, vulnerability ID, assessed artifact digest,
and the exact suppression provenance that caused it to be excluded. The
normalized scope vocabulary is `IMAGE`, `IMAGE_PREFIX`, `DISTRIBUTION`,
`SCANNER`, or `GLOBAL`; non-global scopes require their match value. Categories
are `FALSE_POSITIVE`, `WONT_FIX`, `VENDOR_DEPENDENCY`, and the subsequently
published `PLANNED_FIX` category.

The external scanner adapter preserves the provider suppression ID, requester,
approver, rationale, Change Request ID, creation/approval times, and expiry.
Validation requires one unique record per suppressed finding, an artifact from
the exact assessed digest set, approval before the scan, and an expiry strictly
after the scan. Missing, expired, unapproved, mis-scoped, or count-only
suppressions fail closed and cannot turn a vulnerability outcome into `PASS`.

Platform still does not create or remove provider-side suppressions, schedule
scans, store scanner credentials, or claim Apollo's private Risk Management API.
Those stay with the scanner/identity control plane; the delivery kernel admits
only the provenance needed to trust normalized policy evidence.

When an Environment security policy requires missing vulnerability evidence,
Plan proposal obtains and validates evidence before governance evaluation. A
separate authenticated `POST /v1/artifact-scans` operation supports deliberate
operator invocation. Bundle admission continues to enforce its malware gate.

## Release-channel promotion labels

Apollo publishes two vulnerability-scanner-owned Product Release labels for
Release Channel requirements:

- `vulnerability-scanner.palantir.build/security-scanned=true` gates on a scan
  having completed, regardless of outcome.
- `vulnerability-scanner.palantir.build/security-scan-outcome=pass` gates on no
  active vulnerabilities. Suppressed findings and findings inside a grace
  period may still produce `pass`; `fail` means active findings remain.

Platform derives these labels only from a normalized `COMPLETE` assessment
whose digest set exactly covers the immutable images. A missing or
invalid assessment emits neither label, so both gates fail closed. Product
authors cannot set either reserved key. This avoids converting untrusted
generic labels into scan evidence; assessment admission remains governed by
the external scanner and exact-artifact validation boundary described above.
Platform does not synthesize the documented `security-scanned=false` value
because the public page does not define which non-complete scanner states
receive it and the local normalized assessment contract persists completed
evidence only.

## Concrete local scanner adapter

The Hub image includes `/platform-grype-scanner`, a provider-neutral adapter
for the public Grype JSON format. The Hub still executes it only through the
bounded external-scanner seam. `GRYPE_BIN` names an Environment-supplied clean
absolute executable and `GRYPE_CONFIG_FILE` names an explicit YAML configuration
so ambient user/project configuration cannot change a scan; registry authentication, the vulnerability database,
cache storage, and update policy remain deployment inputs. The adapter invokes
Grype for each exact `repository@sha256` image, emits one deduplicated finding
per CVE with the strongest available CVSS score, and rejects ignored matches.
Grype's ignore record has a rule reason, but it cannot establish the approver,
Change Request, scope, or active expiry required by Platform's suppression
contract.

The Hub Helm chart exposes this boundary without installing a scanner. It can
set the external command/timeout, mount an existing read-only tools PVC, mount
an existing writable database/cache PVC, and mount an existing registry-auth
Secret as `DOCKER_CONFIG`. Those object names and their contents are supplied
by the deployment repository; Platform creates none of them and still runs one
restricted Hub container with a read-only root filesystem.

This is a local technology choice, not Apollo scanner compatibility. Palantir's
current public overview names Trivy for vulnerability scanning and ClamAV for
virus scanning. Platform's normalized evidence boundary permits either through
another adapter without changing policy semantics.

The reviewed `palantir/terraform-provider-tenablesc` source at commit
`7e6ae16ae1e6461dbae27db58ca6dee84c6dab5e` provisions Tenable.SC scans,
repositories, policies, assets, credentials, scan zones, and accept/recast-risk
rules. Its generated data sources do not export scan results and its resources
do not bind a result to an immutable OCI digest. The accept-risk resource names
a repository, plugin, host, port, protocol, expiration, and comments, but not
the independent approver and Change Request evidence required here. It is
therefore useful deployment-side scanner administration, not an implementation
of the ArtifactScan result boundary. The related public `tenablesc-client`
`AgentScan` API manages Nessus Agent host scan definitions and Agent Groups; it
is not an Apollo Spoke Agent protocol.

Executable evidence:

```text
go test ./internal/controlplane ./internal/grypescanner -run 'Test.*ArtifactScan|TestApolloSecurityPromotionLabels|TestSuppressedFindings|TestScanner'
```
