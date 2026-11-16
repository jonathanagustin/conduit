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
- Propose a canonical JSON plan covering the target application name, problem statement summary, HTTP port (`8080`), health endpoint (`/healthz`), non-root container posture (`USER 65532:65532`), memory ceiling (`128Mi` request / `256Mi` limit, within the `1Gi` ceiling), and the three generated source files (`main.go`, `index.html`, `values.yaml`).
- Record `resolvedCommit` as `none (no source repository — problem-statement intake)` so the Accept binding explicitly documents that intake came from a problem statement rather than a git revision.

## Post-Accept generation and packaging

Only after the operator clicks Accept and triggers compute:

1. Run the `appgen` generator and pre-pipeline gates (`schema`, `allowlist`, `secrets`, `resources`, `build`, `health`):
   - `main.go` must use only the allowed Go standard library packages (`bytes`, `context`, `embed`, `encoding/json`, `errors`, `fmt`, `html`, `html/template`, `io`, `log`, `math`, `net/http`, `os`, `path`, `sort`, `strconv`, `strings`, `sync`, `time`, `unicode/utf8`).
   - No secret material (`PRIVATE KEY`, `AKIA`, `ghp_`, `xoxb-`) and no oversized files (`<= 32 KiB`).
   - Resource limits must stay within `512Mi` memory and `1000m` CPU for the generated app spec (`1Gi` packaging ceiling).
2. Commit the generated application files (`main.go`, `index.html`, `values.yaml`) into the managed workspace repository.
3. Run the shared packaging flow (`Dockerfile`, `.dockerignore`, `zarf.yaml`, `uds-package.yaml`, `package-source.yaml`) and deterministic packaging gates so the generated application enters the same governed review and promotion path as a repository-backed onboarded application.
