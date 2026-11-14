# ADR 0015: Brokered debug terminals

Date: 2026-11-14
Status: Accepted direction; implementation outstanding
Decision source: maintainer reversal in issue #303; elevation tracked by #304.

## Decision

Supersede H30's blanket terminal refusal in the CLI and Governance boundary
comments, and the old broker-only recommendation in
`reference-platform/reference/debug-access.md`. Build a native
control-plane session resource and a separate, outbound-connected Spoke debug
broker. This is an explicit new execution boundary, not an extension of delivery
Plans, Helm commands, ordinary Agent credentials or cluster-admin authority.
The existing runtime refusal stays until the replacement is wired and tested.

Start with audited create/list/delete/expire lifecycle, then a constrained debug
Pod broker, then create-and-connect/reattach and console latency. Intermediate
work must not call a metadata record an executable terminal. #303 stays open
until the actual capabilities and relevant behavioral tests exist.

## Required boundary

- Use authenticated operator identity and current Environment/namespace grants.
  Bind owner, original actor and effective identity, Environment, namespace,
  profile/image digest and expiry into the immutable session request. List
  visibility, attachment and deletion need explicit current authority; knowing
  a session ID never grants access. Recheck revocation before attachment and
  while connected. No implicit terminal authority from an unrelated editor role.
- A baseline session creates its own non-root debug Pod, preconfigured with
  narrowly scoped Kubernetes API authority. Only get/list/watch on the approved
  namespaced resources is baseline; secrets, workload exec, mutations, host
  mounts and cluster-admin are excluded. Debug Pod attachment authority must
  not accidentally authorize exec into arbitrary workload Pods.
- Profiles select an operator-admitted digest from a registry installed on the
  Hub and target Environment. Preflight must establish numeric non-root UID,
  `/bin/bash` and target architecture. Pinning a tag alone does not establish
  these facts or immutable bytes. Do not invent a default toolbox image.
- Four hours is an upper bound from initial server creation. Reattachment,
  retries and broker restart cannot refresh expiry. Broker enforcement must
  terminate live streams and expire credentials independently of an open Hub
  connection. Explicit delete/revoke must also trigger credential and Pod
  cleanup; cleanup failure is observable and never reported as successful.
- Persist lifecycle transitions and audit before acknowledging them. Broker
  reconciliation must be idempotent, bind the exact session generation and
  reject stale reports. Authenticate the broker as the specific Environment;
  requests from operators cannot assert broker success or terminal readiness.
- Retain auditable session lifecycle and command/input activity. Raw terminal
  keystrokes are not proof of executed shell commands: aliases, scripts and
  arbitrary executables defeat that inference. The execution implementation
  must document and test its recording/enforcement mechanism and data access
  policy before claiming the issue's every-command audit requirement. Do not
  log credentials or claim complete command audit from transport logs alone.
- NODE mode and JIT writes need separately approved, time-bound, non-self-
  approved elevation (#304). Default POD mode is not a route to NODE authority.
  No elevation is enabled by this ADR. No installation into a real Environment
  is performed by a source change or CI pass.

## Provenance and validation

The six pinned public pages and seven inspected screenshots are indexed in
[the topic note](../reference-platform/reference/debug-access.md). They establish
public CLI/UI semantics only. There is no recovered private broker protocol,
resource DTO, stream framing, storage schema or audit schema. Those must be
registered Conduit-native choices when implemented, with in-band provenance
where supported and displacement evidence in the invention registry.

Validation must cover identity/delegation, cross-user and cross-Environment
access, exact namespace authorization, revoked grants, expired and replayed
attachment, concurrent creates/deletes, restart and multi-replica durability,
audit failure rollback, broker cleanup and disconnected expiry. Broker tests
must constrain actual Kubernetes requests: an argument array or Pod manifest
alone cannot establish the resulting RBAC authority. Fake-server integration
and separately scoped runtime trials must be identified as such.

Teleport remains a possible deployment integration, not the selected or shipped
implementation. The previous recommendation did not implement the public new-
debug-Pod or CLI lifecycle contract and cannot serve as evidence that #303 is
complete.
