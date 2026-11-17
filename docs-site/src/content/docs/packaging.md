---
title: Packaging profiles
description: The packaging-v1 and appgen-v1 profiles from the console demo, including gates and the change-request handoff.
sidebar:
  label: Packaging profiles
  order: 4
---

The console demo walks two profiles. Both stop for Accept before workspace compute, and both hand off through a governed change request. The profile name, version, and hash are part of the Accept binding, with the repository ref, the resolved commit, and the plan hash.

| Profile | Intake | What the reproduction does after Accept |
| --- | --- | --- |
| `onboard` | A repository at a pinned commit | A workspace commit of `Dockerfile`, `zarf.yaml`, and a UDS `Package` custom resource, then the dockerfile gate, the Package CR schema gate, `zarf dev lint`, and `zarf package create` |
| `appgen` | A problem statement | Application and package synthesis, then `zarf dev lint .` and `zarf package create` |

## onboard

Source-based packaging. [`scripts/demo-excalidraw-e2e.sh`](https://github.com/jonathanagustin/conduit/blob/main/scripts/demo-excalidraw-e2e.sh) intakes Excalidraw at `a2ec2889babf7d2295469c6d90ebe77fae57df84`, inspects that commit, asks Needs Input, and reviews the plan before Accept.

After Accept, compute commits the image definition, the Zarf package manifest, and the UDS package resource in the workspace. The upstream image definition is not the source of truth. The gates are the dockerfile check, the Package CR schema check, `zarf dev lint`, and `zarf package create`. The printed digest is the local engine image id. It is not a registry manifest digest unless the image is pushed.

`POST /v1/planning-sessions/{id}/compute` returns `COMPUTE_NOT_CONFIGURED` until the Hub is started with `CONDUIT_IMAGE_BUILDER=docker` and both docker and zarf are available. `scripts/demo-seed.sh` and `scripts/demo-excalidraw-e2e.sh` set that admission and exit with an install message when a prerequisite is missing. See [How it works](/conduit/how-it-works/#compute-before-accept-is-refused).

## appgen

Problem-statement application generation. Intake is the statement, then operator clarification, plan acceptance, and post-accept synthesis of the application and the package. The validation gates named in the walkthrough are `zarf dev lint .` and `zarf package create`.

The generator accepts exactly one source file: `main.go`, `main.py`, or `index.html`. A second entrypoint is rejected, and the rejection names that set. The rendered chart is a Deployment and a Service. The Deployment pins the built image by digest, runs as a non-root user, sets resource limits, and probes `/health`. The package manifest installs that chart and a Package resource that exposes the Service. `mise run appgen-deploy` deploys the package on k3d or kind and requests the Service. That probe is documented in the repository operations note and is not a trunk-gate step.

The same HTTP model configuration applies. Leave model configuration unset to keep recorded proposals and make no model call. Host model command settings are refused. A model does not approve a change. See [Quick start](/conduit/getting-started/#recorded-proposals).

## Gates and evidence

A failed required validation, unknown security evidence, or an unsupported requirement blocks handoff. Fixing a failed proposal is a new revision. Generated output cannot approve itself.

The packaging validation library verifies fixed attestations against operator policy supplied by the receiving host. Unknown, blocked, not-run, and missing checks cannot produce verified evidence. The library does not invoke the tools, and it is not a Hub route or an approval. The held-out checks that still need an executable run are tracked in [#639](https://github.com/jonathanagustin/conduit/issues/639). The boundary is [packaging validation evidence](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/packaging-validation-evidence.md).

## Change request

Both profiles open a governed change request. The author cannot approve their own request. Review identity comes from the Hub session. The constraint view shows the evidence and the no-self-approval rule, then links back to review. It does not compute a deployment grant.

Second-person approval for a UDS install is described in [the package note](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/uds-package.md). The console behavior is [change request views](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/change-request-views.md).

Deploying a package onto UDS and reading its state back is open work: [#427](https://github.com/jonathanagustin/conduit/issues/427) and [#619](https://github.com/jonathanagustin/conduit/issues/619).
