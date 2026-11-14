# Debug terminal and JIT access boundary

Apollo source identity is recorded in `review-ledger.tsv`. Issue #303 reverses
H30's blanket refusal. [ADR 0015](../../decisions/0015-brokered-debug-terminals.md)
records the new target boundary; the runtime currently still refuses terminal
access. The earlier Teleport recommendation below is historical design context,
not an implemented adapter or the newly selected broker contract.

## Derived contract

- A base debug session is isolated per user, expires within four hours, and is
  read-only over a bounded set of namespaced Kubernetes resources. Environment
  Operator authority is required to start one.
- A debug profile has an immutable name and selects exactly one installed
  Artifact Registry plus a versioned image. Platform deployments should
  strengthen the upstream tag requirement to an admitted digest. Images must
  provide `/bin/bash`, use a numeric non-root UID, and match the target node
  architecture; the broker must fail before session creation when preflight
  cannot prove those properties.
- Elevated access is a separately governed, time-bound policy grant. The
  requester cannot approve their own grant. Approval, activation, extension,
  rejection, and revocation are durable audit events; revocation applies to
  active and future sessions.
- Session credentials and Kubernetes impersonation material never enter
  Product desired state, Plans, or the delivery Agent.

## Preferred existing technology

Use an existing Teleport Kubernetes Access deployment as the session broker.
Its Kubernetes service maps authenticated roles to Kubernetes users/groups and
resource verbs, supports time-limited Access Requests, and records interactive
`kubectl exec` sessions. Native Kubernetes RBAC remains the final enforcement
layer. Environment bootstrap owns Teleport enrollment, audit storage, session
recording policy, and any toolbox Pod/profile controller.

Official integration references:

- <https://goteleport.com/docs/enroll-resources/kubernetes-access/controls/>
- <https://goteleport.com/docs/connect-your-client/request-access/>
- <https://goteleport.com/docs/reference/architecture/session-recording/>

The existing Platform Change Request/RBAC model can authorize profile and JIT
policy changes, but a broker adapter and consumer-owned deployment are still
required before this row can be called implemented. This is deliberately
reported as a boundary rather than simulated with an unaudited shell Pod.


## 2026-11-14 implementation contract review (#303)

The terminal, terminal-profiles and four session CLI Markdown pages were
re-fetched and read; all six SHA-256 values match the committed manifest.
The ledger records the review without advancing the implemented 116-page CLI
profile. No session lifecycle, broker, native output schema or private wire
protocol has been implemented by this documentation review.

The public CLI `terminal session create` creates **and connects** to a new debug
Pod with preconfigured kubectl. It defaults to POD mode and accepts NODE mode,
`--node-name` and `--terminal-profile`. `session attach` takes a positional
session ID; `session list` is scoped to Environment and optional space. The
changelog's older `--session-id` wording does not replace that pinned reference
contract. NODE mode's privilege and admission remain separately governed by
#304; merely exposing a string enum must not authorize host access.

The base permission is get/list/watch on endpoints, events,
persistentvolumeclaims, services and pods, with pods/log dependent on Environment
type. Prose also mentions ConfigMaps, but the base YAML does not grant them.
Do not infer secret access, writes, arbitrary workload exec or cluster-admin
from permission to attach to the isolated debug Pod. Read-only describes the
Kubernetes API authority, not an inability to write the shell Pod's filesystem
or a promise that arbitrary tools cannot contact external services.

Profiles have immutable names, a Hub-installed registry, image/version, numeric
non-root UID, `/bin/bash` and matching architecture. A specific version other
than `latest` is required for custom images; the same page describes latest
selection for the built-in registry and explicitly warns tags can be overwritten.
The documentation therefore does not prove immutable image bytes. Conduit's
operator-admitted digest requirement is a deliberate strengthening, not a
reported Apollo guarantee. Source profile text requires registry availability
on the target Environment before selection is enabled.

Sessions are isolated per user, last at most four hours, and support explicit
deletion; the prose describes deletion on window close, while the newer CLI
supports reattachment. A detached resumable session must keep its original
expiry, never extend it implicitly. Details of detach/close semantics and all
Conduit record, identity, transport and audit schemas remain native decisions.

### Referenced images inspected

These are public documentation observations, not tenant captures. The seven
images were downloaded and visually inspected without committing their bytes.
Their canonical base is
`https://www.palantir.com/docs/resources/apollo/debugging/`.

| Image | SHA-256 | Bounded observation |
| --- | --- | --- |
| terminal-tab.png | c027f32e6a83d8f8b0531611b7246b2e73e5b0a4a525d704ef6cc6e727f8c572 | Session ID/status/creator, own and Environment counts, policy panel |
| terminal-confirm.png | 0ec2762a847934672d8944dbdcc9416be0a407bdde8b482955cf2922ad72ff07 | Resource-consumption confirmation and CLI copy action |
| terminal-wait.png | 547eba5a7f72c7ffa28e9c5df6e37b706fe62c673717ba3ecd29e2adfee89b0e | Opening state and session identifier |
| terminal-interact.png | c65ecb3bb0e5670b2e9f8f9321731a2757395a9ebfc9aa0ab85c4d3bcf3f5a9a | Identity/namespace/groups, kubectl output, user-Hub-Spoke latency |
| terminal-delete.png | 985bc4c98da0a90f0f9cf5c6cb2a5ca3f762442faa4e5119a2f1ccaa12e73101 | Created state, expiry, creator and delete action |
| terminal-profile-create.png | a0b2e7ade40452847e870ea37952005ed386d0110f86279d72b1c81bf4abf255 | Immutable-name notice, description, registry, image, version |
| terminal-profile-select.png | f7afc173d8d0473b6b08b414d39a71583d2bca087ed9d68f1beb48864eff072d | Default profile and selectable named profiles |

Displayed session counts are examples, not established limits. The interactive
image shows all-namespace access for that example; it does not override
Conduit's explicit namespace-scoping requirement. Images establish UI facts,
not authorization correctness, server state, transport or session durability.


## Native request metadata implementation (#303, phase 1)

`terminal request` and `/v1/debug-session-requests` now expose non-executing
metadata with current original/effective identity, dual Environment/namespace
permissions, owner isolation, fixed expiry, lazy persisted expiration and durable
transition/audit history. The [native lifecycle contract](../../architecture/debug-session-lifecycle.md)
records its registered choices, current-permission/journal separation,
idempotency, scope/paging bounds and actual CLI/Hub/file/PostgreSQL restore tests.
The runtime opens the journal in its existing file/PostgreSQL durability mode.

Profile reference remains requested intent, not image/registry verification.
No actual terminal, Pod, credential, broker, attachment or command recording is
implemented by this phase. The public session/grant additions remain outside
the 116-page implementation profile. No source hash, snapshot or source-review
claim was advanced by adding the native metadata surface.
