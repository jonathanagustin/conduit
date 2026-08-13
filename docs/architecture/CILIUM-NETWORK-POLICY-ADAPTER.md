# Cilium network-policy adapter

Status: bounded execution adapter; not wired into Plan execution.

Platform owns a provider-neutral `NetworkPolicyBackend` port. The first native
implementation renders only a namespaced `cilium.io/v2` `CiliumNetworkPolicy`.
It cannot represent or create `CiliumClusterwideNetworkPolicy`, install Cilium,
change cluster RBAC, or infer deletion authority. Bootstrap remains responsible
for the Cilium CRDs and cluster-wide privileges; a Spoke implementation of the
storage port must use namespace-scoped credentials.

The current port is intentionally not executable because it has no
receipt-preconditioned rollback/delete operation. Cilium policies compose
additively, so leaving a superseded broader policy after a failed Plan would
retain access even if the new policy rolled back. Wiring therefore also
requires an exact UID/generation-bound delete and a tested replacement sequence
that realizes the new policy before removing the old one and reverses only the
objects created by the failed attempt.

The implementation was derived from a read-only shallow clone of
`cilium/cilium` at exact commit
`8c0423e970e62706bcd5dd3a57e1ffaee697439c`. No Cilium Go package is imported.
Platform owns minimal JSON wire structures for the policy and the current
`Valid` condition it consumes. The pinned CRD also contains
`derivativePolicies`, but that map reports policies derived from `ToGroups`; it
is not ordinary `ToFQDN` endpoint or node realization evidence.

## Identity and authority

Every intent is bound to one Environment, namespace, Apollo Entity ID, Plan ID,
and positive lease attempt. The policy name is content-addressed from the full
intent. The full Apollo Environment and Entity values, Plan, attempt, and
intent digest are annotations on the policy. Admission requires the observed
object to preserve those annotations, UID, positive generation, and a current
`Valid=True` condition.

Cilium endpoint selectors operate on labels, not Pod annotations. The public
Apollo Entity ID contains colons (`aeid:...`), which are invalid in Kubernetes
label values. The adapter therefore exposes `CiliumWorkloadIdentityLabels`:
two Kubernetes-safe, 224-bit SHA-256-derived labels bound to the exact Apollo
Environment and Entity annotation values. This is not a private Apollo label
claim. Before execution wiring is allowed, the workload post-renderer must stamp
the same derived labels and an executable test must prove that the policy
selects only that Entity. Until that gate exists, this adapter remains an
unwired implementation boundary rather than claimed production enforcement.

The intent schema contains only FQDN/L7 policy. An egress rule must choose one
normalized exact DNS name or one leading-wildcard pattern, include positive
TCP/UDP ports, and use HTTP rules only over TCP with uppercase methods and
absolute single-line paths. It cannot carry a lease token, registry credential,
secret value, header, body, IP address, or user identity.

## Enforcement and telemetry

Enforcement and Hubble telemetry are independent evidence axes:

- a missing CRD, missing or false `Valid` condition, mismatched annotation,
  missing UID, or non-positive generation fails policy admission closed;
- the CNP CRD alone never proves enforcement. Without a separate realization
  observer, enforcement remains `UNKNOWN` even when `Valid=True`;
- realization evidence must echo the policy UID, generation, and intent digest,
  select at least one CiliumEndpoint, and show every selected endpoint ready,
  enforcing egress policy, and at a positive revision. Unavailable realization
  transport remains `UNKNOWN`; returned stale, empty, or non-enforcing evidence
  fails closed;
- Hubble absence or query failure reports telemetry `UNKNOWN`, never healthy and
  never "zero denied flows" evidence;
- a successful Hubble query reports the complete denied-flow count while
  retaining at most 20 detail rows and an `incomplete` marker;
- detail rows omit addresses, workload labels, headers, bodies, and user
  identity, strip control characters, and bound every text field;
- denied DNS/L7 evidence is a security observation, not automatically a failed
  deployment or proof that the destination was malicious.

The current `HubbleFlowObserver` is a port, not a relay client. A production
implementation still needs authenticated, Environment-scoped transport,
freshness and loss-event evidence, and an exact-query receipt before telemetry
can become a required acceptance gate.

## Verification

The focused gate is:

```text
go test -race ./internal/agent -run Cilium
```

It covers absent CRDs, cross-namespace rejection before discovery, normalized
FQDN/L7 validation, content/attempt-bound rendering, the pinned current CRD
condition shape, `Valid` without realization as `UNKNOWN`, exact endpoint
realization binding and enforcement, Hubble unavailability as `UNKNOWN`, exact
query scope, and bounded denied-DNS evidence. Repository-wide race, vet, static analysis,
public-boundary, Hub/Spoke, and Zenith gates remain required before integration.
