---
name: appgen
description: Generate a small HTTP application from a problem statement, run the deterministic pre-pipeline gates, and hand the generated source to the packaging flow.
when_to_use: Use when a planning session is started with the appgen work profile and the operator supplies a problem statement instead of a repository URL.
---

# Appgen

This skill is the application generation convention a planner and generator must follow when `profile.name` is `appgen`. The control plane records the `sha256` of this file in `binding.profile.hash` when the operator accepts a plan.

## Problem-statement intake

An `appgen` planning session starts from a problem statement rather than a source repository:

- `problemStatement` — the operator's description of the HTTP application to build.
- `appName` — the lowercase DNS label (`^[a-z][a-z0-9-]{1,30}[a-z0-9]$`) used for the generated service, package, and chart.

When `problemStatement` or `appName` is missing at intake, the planning turn raises `NeedsInput` (`problem_statement` and/or `app_name`) before writing a plan. No source repository is cloned.

## Planning before Accept

During planning turns (before Accept):

- Do not generate source files, run a container build, or write package artifacts before Accept.
- Propose a canonical JSON plan covering the target application name, problem statement summary, HTTP port (`8080`), health endpoint (`GET /health` returning `ok`), non-root container posture (`USER 65532:65532`), and exactly one source file. The allowed source shapes are `main.go` (language `go`), `main.py` (language `python`), and `index.html` (language `html`). A second entrypoint is rejected.
- Record `resolvedCommit` as `none (no source repository — problem-statement intake)` so the Accept binding explicitly documents that intake came from a problem statement rather than a git revision.

## Post-Accept generation and packaging

Only after the operator clicks Accept and triggers compute:

1. Run the `appgen` generator and pre-pipeline gates (`schema`, `allowlist`, `secrets`, `resources`, `build`, `health`):
   - Exactly one source file, and only `main.go`, `main.py`, or `index.html`, with the matching language.
   - No secret material and no file over `4096` bytes (`8192` bytes across the source set).
   - Memory at most `1Gi` and cpu at most `1` core.
   - `main.go` and `index.html` build a non-root scratch image. `main.py` copies the file into the digest-pinned CPython 3.12 interpreter. The build has no network. `GET /health` must return `200` and `ok`.
2. Commit the source file and the rendered package into the managed workspace repository.
3. The rendered chart contains a Deployment and a Service. The Deployment pins the built image by digest, runs as uid `65532`, sets resource limits, and probes `/health`. `zarf.yaml` installs that chart and a Package resource whose `spec.network.expose` names the Service. `zarf dev lint` and `zarf package create` still have to pass. `mise run appgen-deploy` deploys that package on k3d or kind and requests the Service; it is a manual check, not a trunk-gate step.
