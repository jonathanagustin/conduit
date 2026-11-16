---
title: How it works
description: Intake at a pinned commit, planning, Needs Input, Accept, the compute refusal, validation gates, change requests, and receipts.
sidebar:
  label: How it works
  order: 2
---

Every step in the console demo is planned, accepted, validated, and receipted. Accept is the control point. No package or image compute runs before it.

The planning session contract is [planning sessions](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/planning-sessions.md). The wire contract is [`api/planning-sessions.yaml`](https://github.com/jonathanagustin/conduit/blob/main/api/planning-sessions.yaml).

## Intake at a pinned commit

A session starts from a repository URL and ref. `POST /v1/planning-sessions` records intake only.

`scripts/demo-excalidraw-e2e.sh` intakes Excalidraw at commit `a2ec2889babf7d2295469c6d90ebe77fae57df84`. A planning turn clones that intake at the given ref into an isolated directory. Inspection runs there. No package or image compute runs during inspection.

Application generation (profile `appgen`) starts from a problem statement instead of a repository checkout. Both profiles still go through plan review and Accept. See [Packaging profiles](/conduit/packaging/).

## Planning and Needs Input

A turn may inspect the source, ask questions, and write a plan. Outstanding questions are Needs Input. Questions with choices become `select` questions; the rest are `string`. Answer them with `POST /answers`.

Accept stays refused while a required question is unanswered, and a turn leaves the plan unwritten until they are. The operator console shows these as Needs Input cards.

## Plan review and Accept

With no open question, the plan is written and stored as canonical JSON in a fixed field order, so the plan hash is the same for the same plan. The console shows that plan for review, including a JSON view in the demo.

Accept records, and never rewrites:

- the onboard profile name, version, and hash
- the intake repository URL and ref
- the resolved source commit SHA
- the plan text and its SHA-256
- the accept time

Accept writes an authorization audit receipt before the session is bound. If that write fails, the session stays unaccepted. The receipt binds profile coordinates, intake, the source commit, and the plan hash. It does not copy the plan text into the audit.

Sessions are isolated by the authenticated owner. Another operator receives 404 for that session, including for Accept and compute. Session documents in this slice are process-local and disappear on Hub restart.

## Compute before Accept is refused

> [!IMPORTANT]
> `POST /v1/planning-sessions/{sessionID}/compute` is refused with `COMPUTE_BEFORE_ACCEPT` until the Accept binding exists.

After Accept, that route returns `COMPUTE_NOT_CONFIGURED` until `CONDUIT_IMAGE_BUILDER=docker` admits the local engine builder, the engine image inspector, and the zarf package creator together. `scripts/demo-seed.sh` and `scripts/demo-excalidraw-e2e.sh` set that variable. They exit with an install message when docker or zarf is missing. The digest they record is the local engine image id, not a registry manifest digest, unless the image is pushed.

## Workspace, gates, and the change request

For profile `onboard`, `scripts/demo-excalidraw-e2e.sh` commits a `Dockerfile`, `zarf.yaml`, and a UDS `Package` custom resource in the workspace, then runs the dockerfile gate, the Package CR schema gate, `zarf dev lint`, and `zarf package create`.

For profile `appgen`, `scripts/demo-generate.sh` synthesizes the application and the package after Accept, then runs `zarf dev lint .` and `zarf package create`.

Both hand off through a governed change request. A change request still needs a second person: the author cannot approve their own request. The console does not offer action controls for an unknown identity or for self-review. Hub authorization, current policy, status, and the no-self-approval check remain the authority at the write endpoint. The review screen does not grant a deployment by itself.

Required validation that fails, unknown security evidence, and unsupported requirements block handoff. A workflow signal is not an approval.

## Receipts

Governed mutation does not proceed unless its decision event is durably persisted. Accept's audit receipt is one of those events. Reported state comes back from the Spoke and stays distinct from desired state. A Spoke may not reuse a receipt from a different plan or attempt.

The packaging validation library is a separate boundary. It verifies attestations against operator policy. A missing, blocked, or not-run check cannot become verified evidence. That library is not composed into a Hub route, worker, approval, or deployment path. See [packaging validation evidence](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/packaging-validation-evidence.md).

## Read the contracts

- [Planning sessions](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/planning-sessions.md)
- [Change request views](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/change-request-views.md)
- [Deployment invariants](https://github.com/jonathanagustin/conduit/blob/main/docs/architecture/deployment-invariants.md)
- [UDS package and second-person approval](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/uds-package.md)
- [Packaging profiles](/conduit/packaging/)
