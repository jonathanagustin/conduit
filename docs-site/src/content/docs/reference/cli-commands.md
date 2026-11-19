---
title: CLI command reference
description: Generated from the native conduit command tree, including arguments and flags.
---

Generated from `conduitcli.NewRoot` with an empty configuration environment. No command is executed and no credential is read. Defaults below are parser defaults, not deployment configuration. A command's presence does not promise that its backing service is configured: explicit unsupported and unavailable responses remain authoritative. See [Using the CLI](/conduit/cli/) before running a mutation.

## Connection and output flags

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--bearer-token-file` | `string` | `` | mode-0600 Multipass bearer token file |
| `--ca-file` | `string` | `` | Hub CA PEM file (CONDUIT_CA_FILE) |
| `--client-cert-file` | `string` | `` | operator client certificate PEM file |
| `--client-key-file` | `string` | `` | operator client private key PEM file |
| `--development-groups` | `stringSlice` | `[]` | groups for the loopback-only development identity |
| `--development-subject` | `string` | `` | explicit loopback-only insecure development identity |
| `--hub-spiffe-id` | `string` | `` | exact Conduit Hub SPIFFE ID |
| `--oidc-ca-file` | `string` | `` | CA PEM for the OIDC issuer when it is not public (CONDUIT_OIDC_CA_FILE) |
| `--oidc-client-id` | `string` | `` | OIDC client id (CONDUIT_OIDC_CLIENT_ID) |
| `--oidc-client-secret-file` | `string` | `` | mode-0600 OIDC client secret file (CONDUIT_OIDC_CLIENT_SECRET_FILE) |
| `--oidc-issuer` | `string` | `` | OIDC issuer for the client-credentials grant (CONDUIT_OIDC_ISSUER) |
| `--oidc-scope` | `string` | `` | optional OIDC scope (CONDUIT_OIDC_SCOPE) |
| `--oidc-token-url` | `string` | `` | OIDC token endpoint; discovered from the issuer when omitted (CONDUIT_OIDC_TOKEN_URL) |
| `--output` | `string` | `json` | output format: json, yaml, csv, or table |
| `--profile` | `string` | `` | named local connection profile |
| `--server` | `string` | `` | Conduit Hub base URL (CONDUIT_SERVER) |
| `--space-id` | `string` | `` | native Space selection for Environment listing and publication (CONDUIT_SPACE_ID) |
| `--spiffe-endpoint` | `string` | `` | SPIFFE Workload API address |
| `--timeout` | `duration` | `30s` | request timeout |

## conduit

Operate the Conduit Hub/Spoke delivery control plane

```text
conduit
```

## conduit agent-credential

Revoke Agent credentials at the Hub and see when Agent certificates expire

```text
conduit agent-credential
```

## conduit agent-credential list

List each Agent's last presented certificate, its expiry and whether it is revoked

```text
conduit agent-credential list ENVIRONMENT
```

## conduit agent-credential revocations

List the revocations in force for an Environment

```text
conduit agent-credential revocations ENVIRONMENT
```

## conduit agent-credential revoke

Refuse one certificate (--serial), one Agent (--agent-id) or every Agent (--all-agents) of an Environment

```text
conduit agent-credential revoke ENVIRONMENT [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--agent-id` | `string` | `` | Agent identity to refuse until a certificate is issued after --not-before |
| `--all-agents` | `bool` | `false` | refuse every Agent of the Environment |
| `--not-before` | `string` | `` | RFC 3339 instant; certificates issued at or before it are refused (default: now) |
| `--reason` | `string` | `` | why, for the audit trail (required) |
| `--serial` | `string` | `` | certificate serial in hexadecimal, as \`openssl x509 -noout -serial\` prints it |

## conduit agent-credential unrevoke

Lift one revocation, readmitting the credentials it refused

```text
conduit agent-credential unrevoke ENVIRONMENT REVOCATION_ID
```

## conduit ai

Read model usage and export AI transcripts; this does not call a model

```text
conduit ai
```

## conduit ai transcript

List and export stored AI transcripts (administrator only; every export is audited)

```text
conduit ai transcript
```

## conduit ai transcript export

Export one run's exchanges, verified against the audit log; exits non-zero when verification fails

```text
conduit ai transcript export RUN_ID
```

## conduit ai transcript list

List runs that have a stored transcript, without their text

```text
conduit ai transcript list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--owner` | `string` | `` | only this owner's runs |
| `--session` | `string` | `` | only this session's runs, for example assistant-thread/&lt;id&gt; |

## conduit ai usage

Show your own model usage and remaining daily budget; --all shows every owner and needs an administrator

```text
conduit ai usage [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--all` | `bool` | `false` | every owner's usage (administrator only) |
| `--days` | `int` | `0` | UTC days to include, today counted (default 1) |
| `--owner` | `string` | `` | with --all, one owner's usage |

## conduit approval-policy

Manage approval policies

```text
conduit approval-policy
```

## conduit approval-policy apply

Create or update an approval policy from a document

```text
conduit approval-policy apply POLICY_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | approval-policy YAML or JSON file; use - for stdin |

## conduit artifact-registry

Inspect Artifact Registries assigned to visible Environments

```text
conduit artifact-registry
```

## conduit artifact-registry list

List all visible Artifact Registries

```text
conduit artifact-registry list
```

## conduit bundle

Inspect a Bundle without contacting the Hub

```text
conduit bundle
```

## conduit bundle verify

Verify a Bundle offline with the Hub verifier

```text
conduit bundle verify PATH [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--trusted-keys` | `string` | `` | JSON map of trusted Bundle signing keys (BUNDLE_TRUSTED_KEYS_FILE) |

## conduit changerequest

Inspect and review Change Requests

```text
conduit changerequest
```

## conduit changerequest approve

Approve a Change Request

```text
conduit changerequest approve REQUEST_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate and display the action without mutating state |
| `--rationale` | `string` | `` | review rationale |

## conduit changerequest cancel

Cancel a Change Request

```text
conduit changerequest cancel REQUEST_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate and display the action without mutating state |
| `--reason` | `string` | `` | cancellation reason |

## conduit changerequest comment

Add a discussion comment; this does not approve a Change Request

```text
conduit changerequest comment REQUEST_RID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--comment` | `string` | `` | discussion text (required) |

## conduit changerequest comments

List Conduit-native discussion comments with server-recorded authors and times

```text
conduit changerequest comments REQUEST_RID
```

## conduit changerequest get

Get a Change Request

```text
conduit changerequest get REQUEST_ID
```

## conduit changerequest label

Add or remove one existing Change Request label

```text
conduit changerequest label
```

## conduit changerequest label add

Change one label without replacing unrelated labels

```text
conduit changerequest label add REQUEST_RID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | preview without changing discussion, labels, approval or Plan state |
| `--label-name` | `string` | `` | existing label name (required) |

## conduit changerequest label remove

Change one label without replacing unrelated labels

```text
conduit changerequest label remove REQUEST_RID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | preview without changing discussion, labels, approval or Plan state |
| `--label-name` | `string` | `` | existing label name (required) |

## conduit changerequest list

List visible Change Requests

```text
conduit changerequest list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--author` | `string` | `` | filter by exact author username |
| `--environment` | `string` | `` | filter by Environment ID |
| `--limit` | `int` | `0` | maximum number of results; zero returns all |
| `--status` | `string` | `` | filter by status |
| `--title` | `string` | `` | filter by title substring |

## conduit changerequest reject

Reject a Change Request

```text
conduit changerequest reject REQUEST_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate and display the action without mutating state |
| `--rationale` | `string` | `` | review rationale |

## conduit changerequest reopen

Reopen a Change Request

```text
conduit changerequest reopen REQUEST_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate and display the action without mutating state |

## conduit completion

Generate shell completion

```text
conduit completion [bash|zsh|fish|powershell]
```

## conduit configure

Create or update a secure local connection profile

```text
conduit configure
```

## conduit cve

Scan immutable Product Release artifacts

```text
conduit cve
```

## conduit cve check

Run the configured vulnerability and malware scanner

```text
conduit cve check [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--details` | `bool` | `false` | include full normalized assessment evidence |
| `--environment` | `string` | `` | require visibility of this Environment before scanning |
| `--maven-coordinates-file` | `string` | `` | typed Product Release YAML or JSON to scan |
| `--product-id` | `string` | `` | validate against the Product in the input Release |
| `--release-channel` | `string` | `` | require the input Release to be assigned to this channel |

## conduit cve grype

Run local Grype scans with NVD normalization

```text
conduit cve grype
```

## conduit cve grype scan

Scan locally with installed Grype; no Hub data is fetched

```text
conduit cve grype scan SOURCE [flags] [-- GRYPE_FLAGS]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--output-file` | `string` | `` | write the complete normalized report as private JSON |

## conduit cve list

Unavailable: Conduit records grace-period findings, not CVE due dates

```text
conduit cve list
```

## conduit entity

Propose governed Entity operations

```text
conduit entity
```

## conduit entity aggregate

Generate a deterministic Product version aggregate

```text
conduit entity aggregate [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--generate-lowest` | `bool` | `false` | select the lowest eligible version |
| `--generate-versions` | `bool` | `false` | include all eligible versions |
| `--output-dir` | `string` | `` | optional output directory |
| `--release-channel` | `string` | `` | filter by Release Channel |

## conduit entity enforce

Issue an exact governed Entity enforcement command

```text
conduit entity enforce INSTALLATION_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | exact desired-state YAML or JSON |
| `--reason` | `string` | `` | enforcement rationale |
| `--version` | `string` | `` | exact target version |

## conduit entity get

Get an Entity by Apollo entity ID

```text
conduit entity get AEID
```

## conduit entity install

Propose an exact desired-state installation for approval

```text
conduit entity install [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--environment` | `string` | `` | Environment ID |
| `--file` | `string` | `` | exact desired-state YAML or JSON file |
| `--product-id` | `string` | `` | Product group:name coordinate |

## conduit entity list

List desired Entities for an Environment

```text
conduit entity list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--environment` | `string` | `` | Environment ID |

## conduit entity maintenancewindowoverride

Experimental: propose governed temporary Entity maintenance windows

```text
conduit entity maintenancewindowoverride
```

## conduit entity maintenancewindowoverride create

Experimental: one reviewed request per unique Entity; default no downtime

```text
conduit entity maintenancewindowoverride create AEID [AEID...] [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--compliance-ticket` | `string` | `` | compliance ticket retained with each request |
| `--downtime` | `bool` | `false` | allow downtime; otherwise only no-downtime work is eligible |
| `--dry-run` | `bool` | `false` | preview normal approval decisions without creating requests |
| `--duration` | `string` | `` | positive duration, at most 3600h (for example 30m or 2h) |
| `--reason` | `string` | `` | request description explaining the override |

## conduit environment

Inspect Environment state

```text
conduit environment
```

## conduit environment artifact-registry

Govern Environment Artifact Registry assignments

```text
conduit environment artifact-registry
```

## conduit environment artifact-registry assign

Assign Artifact Registries through a reviewed desired-state Plan

```text
conduit environment artifact-registry assign ENVIRONMENT_ID ARTIFACT_REGISTRY_ID [ARTIFACT_REGISTRY_ID...] [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | next-generation desired-state document |

## conduit environment artifact-registry list

List Artifact Registries assigned to an Environment

```text
conduit environment artifact-registry list ENVIRONMENT_ID
```

## conduit environment artifact-registry unassign

Unassign Artifact Registries through a reviewed desired-state Plan

```text
conduit environment artifact-registry unassign ENVIRONMENT_ID ARTIFACT_REGISTRY_ID [ARTIFACT_REGISTRY_ID...] [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | next-generation desired-state document |

## conduit environment config

Govern Environment profile configuration

```text
conduit environment config
```

## conduit environment config apply

Propose Environment profile configuration

```text
conduit environment config apply [ENVIRONMENT_ID] [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--environment-config` | `string` | `` | Environment profile YAML or JSON |

## conduit environment config get

Get Environment profile configuration

```text
conduit environment config get [ENVIRONMENT_ID] [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--output-dir` | `string` | `` | optional directory for environment-profile.yaml |

## conduit environment create

Author a Git-owned Environment descriptor

```text
conduit environment create ENVIRONMENT_NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--contact-team` | `string` | `` | Team responsible for the Environment. If omitted, the Environment is authored without an owning team. |

## conduit environment delete

Propose uninstalling all Entities from an Environment

```text
conduit environment delete ENVIRONMENT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | next-generation desired-state document |

## conduit environment get

Get a declared and reported Environment projection

```text
conduit environment get ENVIRONMENT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--read-transport` | `string` | `rest` | detail transport: rest or connect; failures are never replayed |

## conduit environment list

List visible Environments

```text
conduit environment list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--read-transport` | `string` | `rest` | summary transport: rest or connect; failures are never replayed |
| `--search` | `string` | `` | Environment ID substring search; requires summary view |
| `--view` | `string` | `full` | Environment projection: full or summary |

## conduit environment manifest

Generate Spoke deployment manifests

```text
conduit environment manifest
```

## conduit environment manifest generate

Generate a pipeable restricted Spoke manifest

```text
conduit environment manifest generate ENVIRONMENT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--agent-id` | `string` | `` | Spoke agent ID; defaults to the Environment ID |
| `--client-tls-secret` | `string` | `` | existing mTLS Secret name |
| `--control-plane-url` | `string` | `` | Conduit Hub URL reachable from the Spoke |
| `--external-ca-configmap` | `string` | `` | existing Hub CA ConfigMap name |
| `--image` | `string` | `` | immutable conduit-agent image digest reference |
| `--k8s-storage-class` | `string` | `` | recorded for compatibility; the stateless Spoke does not claim storage |
| `--namespace` | `string` | `` | externally supplied Kubernetes namespace |

## conduit environment release-channel

Set an Environment default Release Channel

```text
conduit environment release-channel
```

## conduit environment release-channel set

Propose a default Release Channel in desired state

```text
conduit environment release-channel set ENVIRONMENT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | next-generation desired-state document |
| `--release-channel` | `string` | `` | default Release Channel |

## conduit environment reported-state

Get the latest exact state reported by a Spoke

```text
conduit environment reported-state ENVIRONMENT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--read-transport` | `string` | `rest` | reported-state transport: rest or connect; failures are never replayed |

## conduit environment setting

Read and replace Environment settings (deployment.yml)

```text
conduit environment setting
```

## conduit environment setting edit

Open a change request replacing Environment settings (deployment.yml)

```text
conduit environment setting edit [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--content` | `string` | `` | complete inline settings document |
| `--file` | `string` | `` | desired-state YAML or JSON the settings replace into |
| `--settings-file` | `string` | `` | file holding the complete settings document |

## conduit environment setting get

Project a desired-state document into Environment settings (deployment.yml)

```text
conduit environment setting get [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | desired-state YAML or JSON to project |

## conduit environment version

Compare exact Entity versions

```text
conduit environment version
```

## conduit environment version compare

Return exact versions reported by an Environment

```text
conduit environment version compare ENVIRONMENT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--release-channel` | `string` | `` | include only Entities assigned to this Release Channel |

## conduit grant

Manage resource role grants

```text
conduit grant
```

## conduit grant create

Create an immutable resource grant from a document

```text
conduit grant create [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | grant YAML or JSON file; use - for stdin |

## conduit helm-chart

Validate Helm chart Products

```text
conduit helm-chart
```

## conduit helm-chart validate

Run Helm lint and deterministic template validation

```text
conduit helm-chart validate [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--chart-path` | `string` | `` | local Helm chart path |
| `--name` | `string` | `` | expected chart name |
| `--repository-url` | `string` | `` | source repository recorded by the caller |
| `--values` | `string` | `` | values file |
| `--version` | `string` | `` | expected chart version |

## conduit label

Read and govern labels in Git-owned desired state

```text
conduit label
```

## conduit label delete

Delete a label through a reviewed Plan

```text
conduit label delete RESOURCE_TYPE RESOURCE_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate and print without proposing |
| `--file` | `string` | `` | next-generation desired-state YAML or JSON |
| `--label-id` | `string` | `` | label definition ID |
| `--label-value` | `string` | `` | label value |

## conduit label get

Get desired-state labels

```text
conduit label get RESOURCE_TYPE RESOURCE_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | desired-state YAML or JSON |
| `--label-id` | `string` | `` | label definition ID |

## conduit label list

List desired-state labels

```text
conduit label list RESOURCE_TYPE RESOURCE_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | desired-state YAML or JSON |
| `--label-id` | `string` | `` | label definition ID |

## conduit label set

Set a label through a reviewed Plan

```text
conduit label set RESOURCE_TYPE RESOURCE_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate and print without proposing |
| `--file` | `string` | `` | next-generation desired-state YAML or JSON |
| `--label-id` | `string` | `` | label definition ID |
| `--label-value` | `string` | `` | label value |

## conduit login

Sign in as a person and store the token in the bearer token file

```text
conduit login [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--flow` | `string` | `auto` | auto, device, or loopback |
| `--login-timeout` | `duration` | `5m0s` | how long to wait for the sign-in to finish |
| `--refresh` | `bool` | `false` | renew a stored refresh grant without interactive sign-in |
| `--store-refresh-token` | `bool` | `false` | retain the refresh grant in the private credential file |
| `--token` | `string` | `id` | which token to store: id (what the Hub's default policy verifies) or access |

## conduit logout

Remove the stored bearer token

```text
conduit logout
```

## conduit module

Manage versioned Modules

```text
conduit module
```

## conduit module create

Create immutable Module Releases

```text
conduit module create [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate without publishing |
| `--module-definition-file` | `string` | `` | Module definition YAML or JSON |
| `--module-directory` | `string` | `` | directory of Module definitions |

## conduit module diff

Diff two typed Module definitions

```text
conduit module diff [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `stringArray` | `[]` | Module definition; specify exactly twice |

## conduit module evaluate

Evaluate derived Module features from an exact lock

```text
conduit module evaluate [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--feature` | `stringSlice` | `[]` | feature to evaluate |
| `--file` | `string` | `` | Module definition YAML or JSON |

## conduit module flatten

Flatten a Module into exact locked Entities

```text
conduit module flatten [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--module-definition-file` | `string` | `` | Module definition YAML or JSON |

## conduit module get

Get all visible Releases for a Module

```text
conduit module get MODULE_PRODUCT_ID
```

## conduit module install

Install an exact or channel-tracking Module into an Environment

```text
conduit module install MODULE_PRODUCT_ID_OR_COORDINATE [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--environment` | `string` | `` | Environment ID |
| `--ignore-recalls` | `bool` | `false` | request independent review for the exact recalled Module release |
| `--ignore-secret-requirements` | `bool` | `false` | record reviewed intent only; Module metadata has no runtime secret gate |
| `--rationale` | `string` | `` | reason required when requesting a reviewed override |
| `--release-channel` | `string` | `` | Release Channel to track |
| `--variable` | `stringArray` | `[]` | Module variable as key=value; repeatable |
| `--version` | `string` | `` | exact Module version |

## conduit module installation

Conduit-native installed-Module metadata operations

```text
conduit module installation
```

## conduit module installation update

Propose an independently reviewed installed-Module override; not Module-definition publication

```text
conduit module installation update INSTALLATION_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | preview against current Hub facts without recording review or Module metadata |
| `--environment` | `string` | `` | Environment ID |
| `--ignore-recalls` | `bool` | `false` | request a reviewed exception for the exact recalled Module release |
| `--ignore-secret-requirements` | `bool` | `false` | record reviewed intent only; Module metadata has no runtime secret gate |
| `--rationale` | `string` | `` | reason retained in the independently reviewed request |
| `--version` | `string` | `` | exact target Module version; omitted selects the tracked channel |

## conduit module list

List visible Module Releases

```text
conduit module list
```

## conduit module lock

Resolve an immutable Module lock

```text
conduit module lock [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--module-definition-file` | `string` | `` | Module definition YAML or JSON |
| `--module-directory` | `string` | `` | directory containing one root Module definition |
| `--verify` | `bool` | `false` | fail unless the resolved lock is complete and exactly pinned |

## conduit module override

Apply a separately approved Conduit Module metadata override

```text
conduit module override
```

## conduit module override apply

Admit metadata only after exact-scope independent review

```text
conduit module override apply CHANGE_REQUEST_ID
```

## conduit module update

Update immutable Module Releases

```text
conduit module update [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate without publishing |
| `--module-definition-file` | `string` | `` | Module definition YAML or JSON |
| `--module-directory` | `string` | `` | directory of Module definitions |

## conduit plan

Propose, inspect, approve, and cancel Plans

```text
conduit plan
```

## conduit plan approve

Approve a reviewed Plan

```text
conduit plan approve PLAN_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--rationale` | `string` | `` | review rationale |
| `--write-transport` | `string` | `rest` | Approval transport: rest (full Plan) or connect (conduit.plan-approval/v1 receipt); failures are not replayed |

## conduit plan cancel

Cancel a Plan before or during execution

```text
conduit plan cancel PLAN_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--reason` | `string` | `` | cancellation reason |
| `--write-transport` | `string` | `rest` | Cancellation transport: rest (full Plan) or connect (conduit.plan-cancellation/v1 receipt); failures are not replayed |

## conduit plan get

Get a Plan and its exact execution evidence

```text
conduit plan get PLAN_ID
```

## conduit plan list

List visible Plans

```text
conduit plan list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--environment` | `string` | `` | Environment id |
| `--installation-id` | `string` | `` | installation targeted by a Plan step |
| `--read-transport` | `string` | `rest` | Plan summary transport: rest or connect; failures are not replayed |
| `--search` | `string` | `` | case-insensitive search applied by the Hub |
| `--status` | `string` | `` | Plan status |

## conduit plan propose

Propose a Plan from a desired-state document

```text
conduit plan propose [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | desired-state YAML or JSON file; use - for stdin |

## conduit product

Inspect Products and govern promotion pipelines

```text
conduit product
```

## conduit product list

List visible Products

```text
conduit product list
```

## conduit product promotion

Govern Product promotion pipelines in desired state

```text
conduit product promotion
```

## conduit product promotion append

Propose a Product promotion pipeline update

```text
conduit product promotion append PRODUCT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate without proposing |
| `--file` | `string` | `` | typed Product promotion pipeline YAML or JSON |
| `--override` | `bool` | `false` | allow the appended document to replace enabled state |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |

## conduit product promotion disable

Propose changing Product promotion evaluation state

```text
conduit product promotion disable PRODUCT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |

## conduit product promotion enable

Propose changing Product promotion evaluation state

```text
conduit product promotion enable PRODUCT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |

## conduit product promotion get

Get a Product promotion pipeline from desired state

```text
conduit product promotion get PRODUCT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--state-file` | `string` | `` | desired-state YAML or JSON |

## conduit product promotion set

Propose a Product promotion pipeline update

```text
conduit product promotion set PRODUCT_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate without proposing |
| `--file` | `string` | `` | typed Product promotion pipeline YAML or JSON |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |

## conduit product-release

Author and publish immutable Product Releases

```text
conduit product-release
```

## conduit product-release asset

Publish immutable Product Release assets

```text
conduit product-release asset
```

## conduit product-release asset publish

Upload an immutable content-addressed asset

```text
conduit product-release asset publish [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--asset-path` | `string` | `` | asset file to upload |
| `--manifest-file` | `string` | `` | optional manifest associated with the asset |
| `--maven-coordinate` | `string` | `` | owning Product Release coordinate |

## conduit product-release create

Publish an immutable Product Release from a validated manifest

```text
conduit product-release create [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--config-schema` | `string` | `` | optional values.schema.json bound to the Release |
| `--default-config` | `string` | `` | optional default values.yaml bound to the Release |
| `--manifest` | `string` | `` | helm-chart.v1 manifest path |
| `--product-tgz` | `string` | `` | canonical Helm Product TGZ |
| `--values` | `string` | `` | alias for --default-config, Apollo's published spelling |
| `--values-schema` | `string` | `` | alias for --config-schema, Apollo's published spelling |

## conduit product-release definition

Validate Apollo Product Definition documents

```text
conduit product-release definition
```

## conduit product-release definition schema

Print the Apollo Product Definition JSON Schema

```text
conduit product-release definition schema
```

## conduit product-release definition verify

Strictly validate a manifest and configuration pair

```text
conduit product-release definition verify [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--configuration` | `string` | `` | Product configuration.yml |
| `--manifest` | `string` | `` | Product manifest.yml |
| `--print-definition` | `bool` | `false` | include the normalized definition |
| `--templates-dir` | `string` | `` | directory holding tmpl managed file templates (defaults to templates/ beside the configuration) |

## conduit product-release download-assets

Publish and read Conduit-native download metadata; no execution or signature verification

```text
conduit product-release download-assets
```

## conduit product-release download-assets get

Read authorized receipt-bound download metadata

```text
conduit product-release download-assets get PRODUCT VERSION
```

## conduit product-release download-assets list

List authorized download-asset releases

```text
conduit product-release download-assets list PRODUCT
```

## conduit product-release download-assets publish

Catalogue immutable publisher-declared downloads; '-' reads stdin

```text
conduit product-release download-assets publish DECLARATION_JSON [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | validate and display metadata locally without contacting or publishing to the Hub |

## conduit product-release get

Get one or more immutable Product Releases

```text
conduit product-release get MAVEN_COORDINATE [MAVEN_COORDINATE...]
```

## conduit product-release helm-chart

Author Helm chart Product manifests

```text
conduit product-release helm-chart
```

## conduit product-release helm-chart init

Initialize a digest-pinned helm-chart.v1 Product manifest

```text
conduit product-release helm-chart init [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--artifact` | `stringArray` | `[]` | exact OCI artifact reference with @sha256 digest; repeatable |
| `--as-tgz` | `bool` | `false` | write one deterministic Product TGZ instead of loose files |
| `--chart-path` | `string` | `` | Helm chart directory containing Chart.yaml and values.yaml |
| `--config-schema` | `string` | `` | optional values.schema.json copied or packaged with the manifest |
| `--maven-coordinate` | `string` | `` | Product group:name:version coordinate |
| `--name` | `string` | `` | Helm chart name |
| `--output-dir` | `string` | `` | output directory |
| `--repository-url` | `string` | `` | Helm repository URL |
| `--values` | `string` | `` | values.yaml copied beside the manifest |
| `--version` | `string` | `` | Helm chart version |

## conduit product-release init

Initialize a Product Release directory from a validated manifest

```text
conduit product-release init [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--manifest-file` | `string` | `` | validated helm-chart.v1 manifest |
| `--output-dir` | `string` | `` | new Product Release directory |

## conduit product-release list

List immutable Product Releases

```text
conduit product-release list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--page-size` | `int` | `0` | maximum number of Releases; zero returns all |
| `--product-id` | `string` | `` | Product group:name coordinate |
| `--release-channel` | `string` | `` | filter by Release Channel |

## conduit product-release release-channel

Govern Product Release channel memberships

```text
conduit product-release release-channel
```

## conduit product-release release-channel edit

Edit a Release's channel memberships in $EDITOR

```text
conduit product-release release-channel edit MAVEN_COORDINATE [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | preview the edited memberships without submitting a Plan |
| `--next-generation` | `string` | `` | new reviewed Git generation for the proposed memberships (Conduit-native) |
| `--rationale` | `string` | `` | rationale persisted in the proposal's Change Request |
| `--state-file` | `string` | `` | current Git-owned desired-state document (Conduit-native) |

## conduit product-release terraform

Author Terraform Product Release manifests

```text
conduit product-release terraform
```

## conduit product-release terraform init

Initialize a typed terraform.v1 Product manifest

```text
conduit product-release terraform init [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--maven-coordinate` | `string` | `` | Product group:name:version |
| `--output-dir` | `string` | `` | new Product Release directory |
| `--terraform-repository-url` | `string` | `` | immutable OCI Terraform bundle URL |

## conduit product-release verify

Strictly validate a Product Release manifest

```text
conduit product-release verify [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--manifest` | `string` | `` | Product manifest |
| `--print-manifest` | `bool` | `false` | include the normalized manifest |

## conduit profile

Manage local connection profiles

```text
conduit profile
```

## conduit profile create

Create a profile from the connection flags

```text
conduit profile create NAME
```

## conduit profile delete

Delete a local profile

```text
conduit profile delete NAME
```

## conduit profile export

Export a profile

```text
conduit profile export [NAME]
```

## conduit profile import

Import a profile document

```text
conduit profile import [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | profile YAML or JSON file |

## conduit profile list

List profiles

```text
conduit profile list
```

## conduit profile rename

Rename a local profile

```text
conduit profile rename OLD NEW
```

## conduit profile schema

Print the versioned profile document JSON Schema

```text
conduit profile schema
```

## conduit profile show

Show a profile

```text
conduit profile show [NAME]
```

## conduit profile use

Select the default profile

```text
conduit profile use NAME
```

## conduit release-channel

Govern Release Channels in Git-owned desired state

```text
conduit release-channel
```

## conduit release-channel add

Propose adding immutable Product Releases to a Release Channel

```text
conduit release-channel add [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--maven-coordinates-file` | `string` | `` | newline-delimited group:name:version coordinates |
| `--release-channel` | `string` | `` | target Release Channel |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |
| `--yes` | `bool` | `false` | confirm non-interactive mutation |

## conduit release-channel create

Propose a new Release Channel definition

```text
conduit release-channel create CHANNEL_NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--description` | `string` | `` | Release Channel description |
| `--dry-run` | `bool` | `false` | validate without proposing |
| `--label-requirement` | `stringArray` | `[]` | label requirement ID or ID=value |
| `--label-requirement-value` | `stringArray` | `[]` | accepted value paired by position |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |

## conduit release-channel diff

Diff two Release Channels by definition or by Release set

```text
conduit release-channel diff [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--all` | `bool` | `false` | include unchanged fields (definitions) or shared Releases (--releases) |
| `--releases` | `bool` | `false` | diff the Releases on each channel to find promotions, not the channel definitions |
| `--source-channel` | `string` | `` | source Release Channel |
| `--state-file` | `string` | `` | desired-state YAML or JSON |
| `--target-channel` | `string` | `` | target Release Channel |

## conduit release-channel export

Export a Release Channel definition

```text
conduit release-channel export CHANNEL_NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--latest` | `bool` | `false` | unavailable: export reads the document named by --state-file |
| `--state-file` | `string` | `` | desired-state YAML or JSON |

## conduit release-channel get

Get a Release Channel definition

```text
conduit release-channel get CHANNEL_NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--state-file` | `string` | `` | desired-state YAML or JSON |

## conduit release-channel import

Propose a Release Channel definition exported from another desired state

```text
conduit release-channel import [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--definition-file` | `string` | `` | exported Release Channel definition |
| `--dry-run` | `bool` | `false` | validate without proposing |
| `--overwrite` | `bool` | `false` | replace an existing Release Channel of the same name |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |

## conduit release-channel list

List Release Channel definitions

```text
conduit release-channel list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--page-size` | `int` | `0` | maximum results |
| `--page-token` | `string` | `` | continue after this Release Channel name |
| `--state-file` | `string` | `` | desired-state YAML or JSON |

## conduit release-channel remove

[Experimental] Propose removing Release memberships from a channel

```text
conduit release-channel remove CHANNEL_NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--dry-run` | `bool` | `false` | preview memberships and coordinates without submitting a Plan |
| `--maven-coordinate` | `stringArray` | `[]` | group:name:version coordinate; repeatable |
| `--maven-coordinates-file` | `string` | `` | YAML list of group:name:version coordinates |
| `--next-generation` | `string` | `` | new reviewed Git generation for the proposed memberships (Conduit-native) |
| `--rationale` | `string` | `` | rationale persisted in the proposal's Change Request |
| `--state-file` | `string` | `` | current Git-owned desired-state document (Conduit-native) |

## conduit release-channel update

Propose a Release Channel update

```text
conduit release-channel update CHANNEL_NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--archive` | `bool` | `false` | archive the channel |
| `--clear-label-requirements` | `bool` | `false` | remove all requirements |
| `--description` | `string` | `` | new description |
| `--dry-run` | `bool` | `false` | validate without proposing |
| `--label-requirement` | `stringArray` | `[]` | label requirement ID or ID=value |
| `--label-requirement-value` | `stringArray` | `[]` | accepted value paired by position |
| `--state-file` | `string` | `` | next-generation desired-state YAML or JSON |
| `--unarchive` | `bool` | `false` | unarchive the channel |

## conduit run

Inspect packaging runs reported by the Hub

```text
conduit run
```

## conduit run get

Read native packaging-run status (not Plan approval)

```text
conduit run get RUN_ID [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--read-transport` | `string` | `rest` | packaging detail transport: rest or connect; failures are never replayed |

## conduit run list

List packaging runs

```text
conduit run list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--outcome` | `string` | `` | outcome reported by the Hub |
| `--phase` | `string` | `` | phase reported by the Hub |
| `--plan-id` | `string` | `` | Plan id the run is aimed at |
| `--read-transport` | `string` | `rest` | packaging index transport: rest or connect; failures are never retried over the other transport |
| `--search` | `string` | `` | case-insensitive search applied by the Hub |

## conduit space

Manage Conduit-native grouping; membership grants no resource access

```text
conduit space
```

## conduit space bind

Immutably assign an ENVIRONMENT, PRODUCT or BUNDLE grouping anchor

```text
conduit space bind SPACE_ID RESOURCE_KIND RESOURCE_ID
```

## conduit space bindings

List independently authorized grouping anchors

```text
conduit space bindings SPACE_ID
```

## conduit space create

Create a native Space with authenticated provenance

```text
conduit space create NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--description` | `string` | `` | Space description |

## conduit space get

Read one visible native Space

```text
conduit space get SPACE_ID
```

## conduit space list

List visible native Spaces

```text
conduit space list
```

## conduit suppression

Inspect and remove deployment suppressions

```text
conduit suppression
```

## conduit suppression list

List suppressions for an Environment

```text
conduit suppression list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--environment` | `string` | `` | Environment ID |

## conduit suppression remove

Remove a suppression through the governed operator API

```text
conduit suppression remove SUPPRESSION_ID
```

## conduit target-hub

Unavailable: Conduit names target Hubs per Export Pipeline and keeps no registry of them

```text
conduit target-hub
```

## conduit target-hub get

Unavailable: no target Hub registry exists

```text
conduit target-hub get ID
```

## conduit target-hub list

Unavailable: no target Hub registry exists

```text
conduit target-hub list
```

## conduit target-hub pipeline

Unavailable: no target Hub registry exists

```text
conduit target-hub pipeline
```

## conduit target-hub pipeline list

Unavailable: no target Hub registry exists

```text
conduit target-hub pipeline list TARGET_HUB_ID
```

## conduit team

Manage governance Teams

```text
conduit team
```

## conduit team apply

Create or update a Team from a document

```text
conduit team apply TEAM_NAME [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--file` | `string` | `` | Team YAML or JSON file; use - for stdin |

## conduit terminal

Manage debug request metadata; interactive broker execution remains unavailable

```text
conduit terminal [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--session-id` | `string` | `` | external audited break-glass session identifier (never executed by Conduit) |

## conduit terminal grant

Broker/JIT implementation outstanding: just-in-time terminal access grants

```text
conduit terminal grant
```

## conduit terminal grant list

Broker/JIT implementation outstanding: just-in-time terminal access grants

```text
conduit terminal grant list
```

## conduit terminal policy

Broker/JIT implementation outstanding: terminal access policies

```text
conduit terminal policy
```

## conduit terminal policy list

Broker/JIT implementation outstanding: terminal access policies

```text
conduit terminal policy list
```

## conduit terminal request

Manage native non-executing debug request metadata; this does not open a terminal

```text
conduit terminal request
```

## conduit terminal request create

Record an audited metadata request; profile and broker preflight remain unverified

```text
conduit terminal request create [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--environment` | `string` | `` | Environment ID (required) |
| `--lifetime` | `duration` | `4h0m0s` | metadata lifetime, positive whole seconds up to 4h |
| `--namespace` | `string` | `` | declared Kubernetes namespace (required) |
| `--request-id` | `string` | `` | retained native request ID for an idempotent retry |
| `--terminal-profile` | `string` | `` | requested profile reference; not an execution preflight (required) |

## conduit terminal request delete

delete native debug request metadata

```text
conduit terminal request delete REQUEST_ID
```

## conduit terminal request get

get native debug request metadata

```text
conduit terminal request get REQUEST_ID
```

## conduit terminal request history

history native debug request metadata

```text
conduit terminal request history REQUEST_ID
```

## conduit terminal request list

List an owner-visible page of native metadata

```text
conduit terminal request list [flags]
```

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--after` | `string` | `` | visible request ID cursor |
| `--environment` | `string` | `` | Environment ID (required) |
| `--limit` | `int` | `50` | page size, 1..100 |
| `--namespace` | `string` | `` | namespace (required) |

## conduit terminal request revoke

revoke native debug request metadata

```text
conduit terminal request revoke REQUEST_ID
```

## conduit version

Print Conduit CLI build identity

```text
conduit version
```

