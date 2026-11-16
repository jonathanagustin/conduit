---
title: Packaging profiles
description: The packaging-v1 and appgen-v1 profiles from the console demo, including gates and the change-request handoff.
sidebar:
  label: Packaging profiles
  order: 4
---

The console demo walks two profiles. Both stop for Accept before workspace compute, and both hand off through a governed change request. The profile name, version, and hash are part of the Accept binding, with the repository ref, the resolved commit, and the plan hash.

| Profile | Intake | What the demo shows after Accept |
| --- | --- | --- |
| `packaging-v1` | A repository at a pinned commit | A workspace commit of `Dockerfile`, `zarf.yaml`, and a UDS `Package` custom resource, then Hadolint, Zarf, and schema gates |
| `appgen-v1` | A problem statement | Application and package synthesis, then `zarf dev lint .` and `zarf package create` |

## packaging-v1

Source-based packaging. The recorded walkthrough intakes Excalidraw at `3402f5bc78ae5f4ffbc7e9be4be1fc2c6c1fa6ef`, inspects that commit, asks Needs Input, and reviews the plan before Accept.

After Accept, the demo's workspace path commits the image definition, the Zarf package manifest, and the UDS package resource. The gates named in that walkthrough are Hadolint, Zarf, and schema validation. The upstream image definition is not the source of truth for that run; the workspace writes the files.

The planning session's own compute route does not execute this path. It returns `COMPUTE_NOT_CONFIGURED` after Accept. See [How it works](/conduit/how-it-works/#compute-before-accept-is-refused).

## appgen-v1

Problem-statement application generation. Intake is the statement, then operator clarification, plan acceptance, and post-accept synthesis of the application and the package. The validation gates named in the walkthrough are `zarf dev lint .` and `zarf package create`.

The same `CONDUIT_MODEL_CMD` rule applies. Leave it unset to keep recorded proposals and make no model call. The command does not approve a change. See [Quick start](/conduit/getting-started/#recorded-proposals).

## Gates and evidence

A failed required validation, unknown security evidence, or an unsupported requirement blocks handoff. Fixing a failed proposal is a new revision. Generated output cannot approve itself.

The packaging validation library verifies fixed attestations against operator policy supplied by the receiving host. Unknown, blocked, not-run, and missing checks cannot produce verified evidence. The library does not invoke the tools, and it is not a Hub route or an approval. The held-out checks that still need an executable run are tracked in [#639](https://github.com/jonathanagustin/conduit/issues/639). The boundary is [packaging validation evidence](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/packaging-validation-evidence.md).

## Change request

Both profiles open a governed change request. The author cannot approve their own request. Review identity comes from the Hub session. The constraint view shows the evidence and the no-self-approval rule, then links back to review. It does not compute a deployment grant.

Second-person approval for a UDS install is described in [the package note](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/uds-package.md). The console behavior is [change request views](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/change-request-views.md).

Deploying a package onto UDS and reading its state back is open work: [#427](https://github.com/jonathanagustin/conduit/issues/427) and [#619](https://github.com/jonathanagustin/conduit/issues/619).
