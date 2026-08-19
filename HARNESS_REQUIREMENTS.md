# HARNESS_REQUIREMENTS

Prompt text cannot guarantee reliable autonomy by itself. The requirements below
are enforced **outside** the language model or they are not enforced at all — a
model asked to police itself will comply exactly until the run where it matters.

This file is therefore two things: the requirement list, and an honest audit of
which requirements this repository and its execution environment actually meet.
Each row is marked `[VERIFIED]`, `[INFERRED]`, or `[UNKNOWN]` per
[`ORCHESTRATOR.md`](ORCHESTRATOR.md). A requirement with no enforcement point is
named as a gap rather than described as satisfied.

## The requirements

- Clear, typed, minimally overlapping tools with truthful capability discovery
  and structured errors.
- Sandboxing, least privilege, secret isolation and redaction, network and
  data-egress controls, and separation of instructions from retrieved data.
- Hard token, compute, monetary, wall-time, retry, and concurrency limits.
- Policy-enforced approval gates for external mutations and high-consequence
  actions.
- A user-visible interrupt and kill control.
- Immutable traces of model calls, tool calls, approvals, side effects, errors,
  and actual costs.
- Atomic changes, backups or version control, checkpoints, idempotency where
  possible, and restart or resume support.
- Task-specific objective evaluators, protected validation paths, baselines, and
  noise-aware remeasurement.
- Freshness metadata, source attribution, cache invalidation, and concise
  evidence pointers.
- Offline evaluation across task success, success-adjusted cost, latency, side
  effects, calibration, error recovery, and handoff quality before increasing
  autonomy.

## What is actually enforced here

| Requirement | Status | Enforcement point |
| --- | --- | --- |
| Sandboxing and egress control | `[VERIFIED]` | Firecracker microVM; egress is port-filtered to TCP 80/443 and UDP 53, everything else silently dropped. The microVM is the boundary — there is no seccomp filter or enforcing LSM inside the guest. |
| Wall-time / session limits | `[VERIFIED]` | Observed firing: four concurrent subagents terminated mid-task with `You've hit your session limit`. The limit is real and enforced by the platform, not by the model. |
| Concurrency limits | `[VERIFIED]` | Observed: over-fanning subagents hit an agent-session cap and killed five workstreams. |
| Version control and atomic change | `[VERIFIED]` | Git. Every change lands through a branch and a reviewed pull request. |
| Objective evaluators and protected validation | `[VERIFIED]` | The Trunk gate (`.github/workflows/platform-ci.yml`) and `scripts/verify-local.sh`, which reads the workflow so the two cannot drift. Head authentication proves the verified tree is a proposed one. |
| Trace of tool calls and costs | `[VERIFIED]` | Session transcripts under `/root/.claude/projects/`; GitHub Actions run logs; subagent token and duration counts are reported per agent. |
| Interrupt and kill control | `[VERIFIED]` | User-visible interrupt in the client; `TaskStop` for background work. |
| Instruction/data separation | `[VERIFIED]` | External content (GitHub event payloads, fetched pages, tool output) arrives inside marked envelopes naming which fields are untrusted. Honouring the marking is still the model's job — see the gap below. |
| Approval gate on external mutation | `[VERIFIED]` | `.claude/settings.json` denies `Bash(gh pr merge:*)` **and** `mcp__github__merge_pull_request`, so both paths to the merge endpoint are gated. Closing a pull request is gated on the shell path only — see the residual below. |
| Secret isolation and redaction | `[INFERRED]` | Tokens are injected as environment variables and appear redacted (`***`) in Actions logs. No test forces a secret through an unredacted path, so this is inferred from observation rather than proven. |
| Typed tools with structured errors | `[INFERRED]` | MCP tools are schema-typed. Bash is not, and its failures are frequently misreported by this environment — a blocked port times out rather than refusing, and a pipeline's exit status belongs to its last command. |
| Idempotency and resume | `[INFERRED]` | The cluster bootstrap is idempotent by construction. Agent work is not: a killed subagent leaves a worktree and no checkpoint, and must be re-briefed from its last report. |
| Monetary limits | `[UNKNOWN]` | No monetary ceiling is visible from inside the session. Cheapest probe: ask the platform owner. |
| Offline evaluation before increasing autonomy | `[UNKNOWN]` | No evaluation suite scores task success, calibration, or handoff quality across runs. Nothing currently gates an increase in autonomy on measured performance. |

## The merge gap, and how it was closed

`.claude/settings.json` expressed an intent to gate merges:

```json
"deny": ["Bash(gh pr merge:*)", "Bash(gh pr close:*)", "Bash(gh repo delete:*)"]
```

Every one of those matches a **shell invocation**. None matched
`mcp__github__merge_pull_request`, a different tool reaching the same GitHub
endpoint. Pull requests #245, #246, #249, #250, #274 and #275 were merged in
this repository through the MCP path while that deny rule was in force — the
control read as enforced, was not, and its absence was only visible by comparing
the policy to the trace.

It is closed: `mcp__github__merge_pull_request` is now denied outright, so
merging this repository's pull requests requires a human on both paths. That is
a deliberate reduction in what the agent may do, chosen over the alternative of
rewriting the policy to match what was actually happening.

### What the rule syntax can and cannot express

A permission rule's parenthesised part is a **string matcher** — `Bash(git *)`
matches the command line, `Edit(//etc/*)` matches a path. An MCP tool takes
structured JSON, not a string, so there is nothing for that matcher to match
against: **MCP tools can only be allowed or denied by whole tool name.** There
is no way to write "deny `update_pull_request` only when it sets
`state: closed`" as a permission rule.

### The residual, stated rather than hidden

Closing a pull request is therefore still gated on one path only.
`Bash(gh pr close:*)` is denied; `mcp__github__update_pull_request` with
`state: "closed"` is not, and denying that tool outright would also forbid
editing a pull request's title and body, which is routine and harmless.

Closing that residual needs a `PreToolUse` hook matching
`mcp__github__update_pull_request` that inspects `tool_input.state` and returns
`permissionDecision: "deny"` for `closed` — enforcement outside the model, which
is what this file is for. It is not built, because the same failure this file
documents would be repeated by claiming otherwise.

## What a prompt cannot do

Three of the rows above depend on the model's cooperation and would be defeated
by a model that ignored them: honouring untrusted-content markings, declining to
act outside authority, and reporting cost honestly. They are listed because the
orchestrator is expected to follow them, not because they are enforced. Anything
in this file that matters should eventually have an enforcement point that is
not a sentence.
