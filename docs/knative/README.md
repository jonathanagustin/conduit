# The Knative interface

A read-only projection that presents Conduit's delivery data in the shape of
[Knative Serving v1](https://knative.dev/docs/serving/reference/serving-api/), so
a client that speaks Knative can read Conduit's Products and Releases as Knative
resources. It is the sibling of the [Apollo GraphQL interface](../apollo/reference/graphql-api.md)
and follows the same discipline, with one deliberate difference in where the
authority comes from.

## Why the authority is different from Apollo's

The Apollo interface tiers every field by *who established it* — a Hub capture
(`OBSERVED`) versus Apollo's published docs (`DOCUMENTED`) — because Apollo was
recovered rather than specified. Knative is the opposite: its API is a public,
versioned specification, so nothing here is uncertain about *Knative*. What is
uncertain is whether **Conduit** holds data behind a given Knative field.

So a Knative coordinate is either **MAPPED** — Conduit has a real source, cited
in `internal/knative/application`'s mapping table — or **REFUSED** — Knative's
spec declares the field and Conduit has nothing that honestly fills it. A
refused field is left off the wire object entirely, never emitted as a zero
value, because a zero reads as a Knative default Conduit never chose.

## The correspondence, and why it is real rather than a re-labelling

| Knative kind | Conduit source | Basis |
| --- | --- | --- |
| **Revision** — "immutable snapshot of code and configuration" | **Release** — immutable, versioned, digest-pinned | Both are the immutable, versioned unit of their system. This is the load-bearing correspondence. |
| **Configuration** — "floating HEAD of a linear history of Revisions" | **Product** — a linear history of versioned Releases | A Product *is* a linear history of Releases; its `latestCreatedRevisionName` is the newest Release. |
| **Service** — a deployed, routed workload | **Entity / Installation** — a Product deployed in an Environment | Moderate: the running instance. Not served yet (see below). |
| **Route** — `traffic[].percent` across Revisions | **Channel** — which Release is current | Partial. The structure exists; **`percent` is refused** — Conduit promotes a Release through a channel all-or-nothing per Environment and has no percentage HTTP traffic split. Serving a percent would invent one. |

## What this slice serves

`internal/knative` currently projects the two strong mappings:

- **`Revision` ← `Release`**: envelope (`serving.knative.dev/v1`), DNS-normalized
  name, the `serving.knative.dev/configuration` ownership label, the
  digest-pinned image from the Release's first image artifact, and
  `timeoutSeconds` from the Release's resource-readiness timeout when set.
  `containerConcurrency` and the rest of the PodSpec refuse.
- **`Configuration` ← `Product`**: envelope, DNS-normalized name, and
  `latestCreatedRevisionName` (the newest Release by version order).

## What refuses, and why

- **`Configuration.status.latestReadyRevisionName`** — refused. "Ready" is the
  revision currently serving, and the catalog cannot say which that is:
  `EffectiveChannels` folds in the channels a *version* implies, so channel
  membership means "eligible," not "promoted." An honest `latestReady` needs
  either a chosen channel's head or Reporting's observed-healthy revision (the
  same source the Apollo interface draws health from). That is a decision, filed
  rather than guessed — emitting the newest as "ready" would route a client to a
  Release no consumer was given.
- **`Route` and its `traffic[].percent`** — no Conduit backing at all. Conduit
  has no percentage HTTP traffic; its only "percent" is a canary *health*
  threshold, which is a different concept.
- **`Revision.spec.containerConcurrency`, `Service.spec.template`, and the rest
  of the PodSpec** — Conduit projects delivery records, not running pods, so it
  holds no value for these.

## Bounds

- **Read-only.** Knative's write path (a `kubectl apply` of a Service) is a
  control surface, and Conduit's control surface is its own governed change
  pipeline. A writable Knative API would invite a client to believe an apply
  here reconciles, which it does not.
- **The name normalization is lossy.** Product ids are Maven coordinates
  (`group:name`) and versions carry dots; a Knative object name must be a
  DNS-1123 label. The exact coordinate travels in a label; the name is only the
  addressable handle, and a collision is possible in principle. Hardening it (a
  digest suffix) is tracked with the interface's issue.

## What this is not for, which is the more useful thing to record

This mapping was built while answering a different question badly, and the
correction is worth keeping because the reasoning generalizes.

The question was: how should Conduit deploy a workload whose execution is an
**invocation** rather than a **process** — something that is not an always-on
container? Knative looks like the answer, and it is not. Three findings, in
ascending order of force:

1. **Scale-to-zero does not save what it appears to.** On owned or fixed
   capacity you have already paid for the nodes, so a parked pod costs
   approximately nothing; the chain only yields money if a node actually
   empties, is consolidated, and stops being billed. Where that chain does hold,
   Knative's own always-on floor -- activator on the request path, autoscaler,
   networking layer, webhooks -- has to be paid first, and for a modest fleet of
   small workloads it can exceed the idle cost it removes.
2. **Something must always listen.** "Not always running" is unachievable for
   HTTP at the platform level; the only real choice is *what* is always on and
   how widely it amortizes. Knative's answer reintroduces cold starts measured
   in seconds, which is precisely the property edge-style workloads are chosen
   for.
3. **The deployable shape that works needs nothing from Knative.** A workload
   bundled into a digest-pinned image, installed as an ordinary `helm-chart.v1`
   release, gives per-workload promotion, approval, health and recall with **no
   new agent execution path**. Scheduled workloads are a `CronJob` in the same
   chart; queue-driven ones a KEDA `ScaledJob`. Both work today.

A design that shipped the bundle *outside* the image -- a small object naming an
artifact for a shared runtime to fetch -- was considered and rejected for a
reason specific to this repository: Helm would succeed, Conduit would record the
release as deployed, and a later fetch failure would leave the record false with
nothing to surface it. For a system whose contract is that the record matches
reality, a silent lie in the system of record is worse than an honest new verb.

So Knative stays what this directory says it is: a correct correspondence, and a
possible adapter target for a substrate that already runs Knative. It is not the
model, and it should not anchor the decision in
[#27](https://github.com/jonathanagustin/conduit/issues/27), which is now scoped
to the one part that does belong here -- whether Conduit should publish a
per-binding portability matrix, applying the MAPPED-or-REFUSED discipline to
workload bindings per target the way
[`docs/decisions/0005`](../decisions/0005-apollo-coordinates-that-stay-refused.md)
applies it to coordinates.
