---
title: Roadmap
description: Open follow-on issues for the recorded packaging run, workspace isolation, UDS deployment, and executable validation.
sidebar:
  label: Roadmap
  order: 5
---

These issues are the open follow-ons for the path this site describes. Each issue states its own acceptance criteria. This page does not add scope.

| Issue | What remains |
| --- | --- |
| [#615](https://github.com/jonathanagustin/conduit/issues/615) | Record an end-to-end console run that packages an upstream app from a pinned commit, through Accept, an isolated workspace, a governed build, gates, a change request, and review, and pin the image digest. |
| [#616](https://github.com/jonathanagustin/conduit/issues/616) | Run that work in a per-user isolated workspace container: a rootless builder, no host builder socket, default-deny egress except the git host and the local registry, and no cluster credentials. |
| [#427](https://github.com/jonathanagustin/conduit/issues/427) | Deploy through Zarf and UDS, read reported state back, and map bundles. Packaging can emit `zarf.yaml` and a UDS `Package` resource; this issue is the adapter that converges an environment and reports health. |
| [#619](https://github.com/jonathanagustin/conduit/issues/619) | Bring up UDS Core on k3d with single sign-on, two users so a second person can approve a change request, and portal tiles only when the package resource supports them. |
| [#639](https://github.com/jonathanagustin/conduit/issues/639) | Executable application validation, upgrade and recovery drills, and receipt admission for the packaging matrix. Missing tools or scanner data stay unknown or blocked, never a pass. |

## How they fit together

[#615](https://github.com/jonathanagustin/conduit/issues/615) is the recorded packaging run. [#616](https://github.com/jonathanagustin/conduit/issues/616) is the isolation that run needs. [#619](https://github.com/jonathanagustin/conduit/issues/619) is the cluster and the two-person approval path. [#427](https://github.com/jonathanagustin/conduit/issues/427) deploys from the control plane onto that cluster and reads state back. [#639](https://github.com/jonathanagustin/conduit/issues/639) holds the validation that still needs an executable run, a durable receipt receiver, or a separately authorized environment.

None of these issues authorize a production deployment by being listed here.

## Design already recorded

- [How it works](/conduit/how-it-works/)
- [Architecture](/conduit/architecture/)
- [Decisions](https://github.com/jonathanagustin/conduit/tree/main/docs/decisions)
