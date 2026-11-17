---
title: Tenant delivery
description: Declare immutable tenant releases, propose and review Plans, and verify the Environment's reported state.
---

Conduit owns the reusable delivery control plane. Keep an application's Product
catalog, charts, installation configuration, environment inventory and
credentials in its own repository or deployment system. A tenant author supplies
reviewable intent; the Hub validates and governs it; an Environment-scoped Spoke
connects outbound to execute a lease and reports the state it actually observed.

## Declare the release and target

A Product Release names immutable artifact coordinates. The native desired-state
document selects an Environment generation and its Entities. A tag is a label,
not proof of bytes: retain the exact digest, image inventory, vulnerability and
availability evidence required by the target's policy.

The [artifact model](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/artifacts-and-releases.md)
defines Helm, package and bundle declarations. Use the fields supported by the
merged Hub and Spoke version. Package execution is separately enabled and
requires an admitted publisher policy; it must not fall through to Helm. A
generated package definition is not evidence that its application workload was
built, signed, published or installed.

For a package target, declare `environment.packageArchitecture` as `amd64` or
`arm64` in the native desired-state document. This is an operator declaration,
not discovered cluster inventory. A package Release must name exactly one
matching architecture and an immutable platform-manifest digest; multiarchitecture
declarations and OCI indexes are refused. The Spoke checks its own architecture
again before execution. The console's artifact coordinates are declared input,
not proof of installation.

Register the target namespaces and dependencies, review values and secret
requirements, and ensure the Spoke can prepare the referenced material without
placing secret values in a Plan or repository. External provider hooks and
network allowances need a tested deployment-specific composition.

## Propose and approve

Use the native [CLI](/conduit/cli/) or the available console journey to submit a
desired-state proposal. Read the validation errors and resolved Plan: Product,
version, Release digest, namespace, configuration, dependencies, maintenance and
approval requirements must match the intended target.

Approval is a separate authorized action. The author cannot satisfy an
independent reviewer requirement by changing a UI control or submitting a model
answer. Keep the rationale and Plan identity, then inspect the Hub's recorded
approval result. A packaging-run approval is distinct from Plan approval and
does not establish deployed state.

## Execute and observe

The Spoke rechecks the execution conditions, verifies admitted artifact bytes,
performs the selected action and reports the installation's exact state. A Plan
is successful only when the Hub accepts that evidence. The presence of a
Release, an accepted planning conversation or a successful build does not prove
deployment.

Compare desired and reported Product/version, Release and configuration digests,
namespace, health and rollout state. An indeterminate or drifted configuration
is not success. If an execution fails, inspect its diagnostic and rollback
record before another proposal. Stateful application schema compatibility is a
separate unresolved safety boundary, not guaranteed by an infrastructure
rollback command.

## Disconnected targets and current limits

Bundle signing authenticates the exported envelope; it does not establish the
truth of every caller-supplied assessment inside it. Independent assessment
trust and suppression provenance remain an explicit policy decision. A
disconnected installation also does not receive every later source-side recall;
account for that operational limit before admitting it.

The [readiness record](/conduit/readiness/) links the live work for credential
lifecycle, package publication, complete generated workloads, target network
paths, schema compatibility and full cluster acceptance. No candidate PR or demo
fixture overrides those bounds.
