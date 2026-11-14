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
Every one of those is an external assertion preserved verbatim. In particular
the Change Request ID references the approving system's change record and is
not a Conduit `crq_`: the Hub never resolves it, and could not, since Conduit
does not create provider-side suppressions and an assessment relayed from
another Hub carries a reference only that Hub's approver can resolve. The
security boundary for a suppression is governance approval of the Plan that
carries it, not any property of these fields.
Validation requires one unique record per suppressed finding, an artifact from
the exact assessed digest set, approval before the scan, and an expiry strictly
after the scan. Suppressions that are count-only, mis-scoped, expired at the
time of the scan, or missing any provenance field fail closed and cannot turn a
vulnerability outcome into `PASS`.

**That fail-closed set is a check on shape, not on authority**, and the
distinction is the whole of what this section guarantees. `requestedBy`,
`approvedBy` and `changeRequestId` are checked for being present, ordered, and
inside their window; none of the three is resolved against a person, a role, or
a change record, here or anywhere else in the Hub. A submitter who supplies a
self-consistent provenance block naming an approver who approved nothing passes
every check listed above. An earlier revision of this paragraph said
"unapproved" suppressions fail closed, which read as authority and was not true
of the code: what fails closed is an *empty* approver, not an unauthorized one.
Establishing the stronger property is tracked in
[#54](https://github.com/jonathanagustin/conduit/issues/54), and by the
paragraph above it is not simply an unimplemented check — the reference these
fields carry belongs to the approving system, so a local resolution would be
answering a question about somebody else's record.

Platform still does not create or remove provider-side suppressions, schedule
scans, store scanner credentials, or claim Apollo's private Risk Management API.
Those stay with the scanner/identity control plane; the delivery kernel admits
only the provenance needed to trust normalized policy evidence.

When an Environment security policy requires missing vulnerability evidence,
Plan proposal obtains and validates evidence before governance evaluation. A
separate authenticated `POST /v1/artifact-scans` operation supports deliberate
operator invocation. Bundle admission continues to enforce its malware gate.

## Scan freshness

Evidence has an age bound and every policy has one. `maximumScanAgeSeconds` on
the Environment's `ArtifactSecurityPolicy` states it; a policy that states
nothing gets `DefaultMaximumScanAgeSeconds`, seven days, rather than no bound at
all. Evidence older than the window is re-derived at Plan proposal by the
configured scanner, and refused where no scanner is configured — the same
fail-closed path as evidence that was never supplied. A policy wanting a longer
window states the number, where whoever reviews the desired state can see it.

The seven days is Conduit's own choice with no Apollo ground truth behind it,
and is registered as such in `docs/evidence/conduit-inventions.json`.

Two bounds on what freshness means here, both deliberate. It is checked when the
upgrade is proposed and not again when a leased Plan executes, so a Plan
approved just inside the window can execute later against older evidence.
And the check is on the age a supplied assessment claims: a fresh `assessedAt`
is what a caller asserts, not what the Hub witnessed. Independent assurance
against the assessment itself is the open half of
[#54](https://github.com/jonathanagustin/conduit/issues/54) and is not what this
section provides.

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
`7e6ae16ae1e6461dbae27db58ca6dee84c6dab5e` provisions Tenable.SC scans, scan
policies, repositories, assets, scan zones, audit files, organizations, roles,
agent scans, two association resources, and accept/recast-risk rules. It does
**not** provision credentials — `credential` is a data source only, so the
provider reads them and never creates them. Its data sources (`agent_groups`,
`asset`, `assets`, `credential`, `plugin`, `repositories`, `repository`,
`scan_policy_template`) do not export scan results, and no resource or data
source anywhere in the provider mentions a digest, `sha256`, or an OCI
coordinate. The accept-risk resource names `repository_id`, `plugin_id`, a typed
host pair (`host_type`/`host_value`), `port`, `protocol`, `expiration`, and
`comments` — but no approver and no Change Request, which are the independent
evidence Platform's suppression contract requires. It is therefore useful
deployment-side scanner administration, not an implementation of the
ArtifactScan result boundary.

The related public `tenablesc-client` is **more than its `AgentScan` API**, and
an earlier revision of this note described only that file. At the same pinned
commit `35e868a` — verified, so this is a gap in the original reading rather
than drift since — `tenablesc/analysis.go` wraps the Tenable.sc Analysis API
across 239 lines, and `VulnDetailsResult` carries genuine per-finding detail:
`cve`, `cvssV3BaseScore`, `cvssV3TemporalScore`, `cvssV3Vector`, `pluginID`,
`pluginName`, `severity`, and `stigSeverity`, with `scanresult.go` beside it.
`AgentScan` is one file of twenty-eight.

The conclusion is unchanged, and the fuller reading strengthens rather than
weakens it: **every Analysis result type is keyed to a host, not an artifact.**
`VulnSumIPResult` and `VulnSumDNSNameResult` key on `ip`, `dnsName`,
`macAddress`, `netbiosName`, `osCPE` and a `repository`; `VulnHosts` and
`VulnIPSummaryResult` sit on the same axis; the summary tools report
`severityCritical`…`severityInfo` counts per host. Nothing binds a finding to an
immutable OCI digest, which is the coordinate Platform's assessment is built on.
So Tenable.sc is a different axis rather than a competing contract, and
`AgentScan` remains unrelated to any Apollo Spoke Agent protocol.

One consequence reaches the console and is recorded in
`docs/ui/APOLLO-UI-ALIGNMENT-AUDIT-2026-08-16.md`: Tenable publishes a
Critical/High/Medium/Low/Info severity vocabulary, but
`VulnerabilityFinding.Severity` here is an unconstrained string with no Go const
set, interpreted only by `findingScore` in `internal/controlplane/risk.go`, which
prefers `CVSS` whenever it is above zero. A reader that ranks or colours findings
by the severity string can therefore order them differently from how the Hub
scores the same Release, and `scripts/verify-console-contract.mjs` could not
gate such a map because it extracts vocabularies from named consts.

Executable evidence:

```text
go test ./internal/controlplane ./internal/grypescanner -run 'Test.*ArtifactScan|TestApolloSecurityPromotionLabels|TestSuppressedFindings|TestScanner'
```

## Local CLI Grype reports (2026-11-13)

`cve grype scan` invokes the installed Grype directly against the operator's
source, without fetching Hub or Apollo vulnerability data. The public contract
prefers NVD data, then CVSS v3/v2 and Grype severity; only the selected v3 score
remains. Output flags cannot be passed through, `--output-file` is always JSON,
and a valid report survives a provider nonzero exit while the command fails.
Invalid output produces no report. Grype itself can access registries/databases.

The [CLI review](../cli-additions-review-2026-11-13.md) states native schema,
tie, severity-zero, size, and credential-output choices. Provenance explicitly
marks this local report as CONDUIT_NATIVE and non-authoritative. This does not
weaken the separate Hub scanner's provenance requirements for ignored findings.
`internal/grypescanner/local_test.go` and
`internal/conduitcli/grype_commands_test.go` exercise these behaviors with an
executable fixture; they do not establish real-image or Apollo binary parity.
