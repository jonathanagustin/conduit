---
title: Install Conduit
description: Choose a source build or reviewed deployment artifact and prepare the Hub and Spoke prerequisites.
---

The local demo and a production installation are different profiles. Start with
[Quick start](/conduit/getting-started/) for a disposable workstation demo. For a
deployment, record the exact source commit or authenticated artifact digest,
the selected chart values and the environment owner's approval.

## Source installation

Use the locked toolchain from the same checkout as the source:

```bash
git clone https://github.com/jonathanagustin/conduit.git
cd conduit
./install.sh
mise exec -- go build -trimpath -o bin/conduit ./cmd/conduit
mise exec -- go build -trimpath -o bin/platform-delivery ./cmd/platform-delivery
mise exec -- go build -trimpath -o bin/platform-spoke-agent ./cmd/platform-spoke-agent
```

Build the console with `./scripts/build-console.sh` before building the Hub when
the deployment needs the embedded web interface. The binary otherwise reports
that the assets are unavailable. The lockfile pins development tools; it does
not configure a database, credentials or a cluster.

On macOS, use the locked Ruby and the executable host-tool checks from
[local verification](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/local-verification.md).
The full aggregate verifier runs in the supported Linux environment. Focused
native Go tests are separate
evidence and do not require a claim that every container-backed gate ran.

## Deployment inputs

Prepare these outside the Conduit repository:

- A PostgreSQL database and its deployment-owned credential for the production
  repository profile. File storage is an explicit single-replica development or
  migration choice.
- The Hub server certificate, trusted Agent CA or workload identity setup,
  operator identity configuration, persistent Bundle signing key and trusted
  verification keys. Never place private material in chart values committed to
  a repository, a transcript or a support ticket.
- The managed namespaces and the Spoke's actual Kubernetes grants. The chart's
  namespaced roles are a floor; tenant workloads can need additional reviewed
  authority.
- The images and charts selected by digest, and any external database,
  workflow, registry, identity, monitoring or secret-provider dependencies
  required by the enabled components.

The [Helm guide](https://github.com/jonathanagustin/conduit/blob/main/deploy/helm/README.md)
and each chart's `values.yaml` and `values.schema.json` define the supported
settings. Render a proposed installation for review before applying it:

```bash
helm template conduit-hub ./deploy/helm/platform-hub \
  --namespace platform-system -f /secure/deployment-values.yaml
helm template conduit-spoke ./deploy/helm/platform-spoke \
  --namespace platform-system -f /secure/spoke-values.yaml
```

Applying those resources is a separate operator-authorized action. Confirm
readiness, identity refusal, outbound Agent connectivity, a reviewed test Plan
and its exact observed state in the target environment before admitting tenant
workloads.

## Released and disconnected artifacts

This baseline does not establish a complete authenticated install-from-release
path. [#844](https://github.com/jonathanagustin/conduit/issues/844) tracks that
work; a candidate verifier does not add missing assets to an older publication.
Do not infer an artifact digest from a tag or treat a successful download as
signature verification.

The [package operations guide](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/uds-package.md)
records the disconnected installation inputs and their limits. Definition and
schema checks do not prove gateway routing, a completed Spoke lease or an
offline upgrade/removal drill. Those remain in the
[readiness record](/conduit/readiness/).

Continue with [Operate a Hub](/conduit/operate/) and
[Tenant delivery](/conduit/tenant/).
