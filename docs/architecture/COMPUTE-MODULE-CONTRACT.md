# Generic compute-module contract

The supplied 2025 Apache-licensed Palantir API types are evidence for the
runtime contract around Spark and containerized modules. They are not copied
into this repository and do not make our API compatible with Palantir's private
Compute Service or Spark Module Manager.

## Contracts to preserve

- A module request separates immutable image identity, configuration,
  inputs/outputs, authentication mode, network/service access, scaling, queue
  policy, and resource attribution.
- Image identity is a tagged or digested union in the source API. This platform
  adopts the stricter deployment rule already used by the delivery plane: a
  unique tag **and** a digest are both required.
- Configuration is a discriminated `plain` or `secret` reference. Secret values
  never enter a Product, Plan, job request, or status document.
- Inputs and outputs are typed unions (dataset, stream, media set, model, live
  model, named credential) with stable aliases in the V2 shape. A generic `any`
  payload is permitted only behind a versioned schema reference.
- Network access is allowlisted by policy reference or a destination namespace,
  service name, and ports. “Foundry service access” becomes a catalog of
  operator-defined service capabilities rather than a hard-coded private enum.
- Runtime policy includes min/max replicas, concurrency, scale delays and
  thresholds, rolling-update availability, queue duration, and lack-of-polling
  cancellation. These settings are independent of Product promotion.
- Every job has resource attribution. It records one or more catalog resource
  IDs and a usage type so compute cannot become unattributed spend.
- Desired progress (`pending`, `applying`, `applied`) is separate from observed
  replica/container state. Observations retain readiness, restarts, waiting or
  termination reason, image-pull/crash-loop failures, and current/target replica
  sets during rolling updates.
- User-facing exceptions distinguish safe from unsafe arguments. Unsafe details
  remain in operator logs and never cross the tenant API boundary.

## Placement

These contracts define a future `platform.compute` API and worker Products.
They do not belong in the generic Apollo-aligned delivery API: delivery decides
which immutable runtime Release may be installed; the compute plane queues
jobs, scales sessions, and reports workload state after installation.

The first implementation should support container jobs with fixed scaling and
bounded queue/poll timeouts. Spark configuration, cache trees, day/night
scaling, deployed applications, and dynamic HTTP endpoints remain later
capabilities that require concrete use cases and end-to-end tests.
