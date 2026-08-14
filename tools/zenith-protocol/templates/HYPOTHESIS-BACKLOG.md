# Hypothesis backlog — surveyed-but-unpursued leads

A standing queue so the sweep doesn't re-survey the same ground each cycle. Each row names a lead
(a channel, an artifact, a question) and the repeatable artifact it could become (a gate, a module, a
differential). When a lead is picked up it graduates to an H# in the capability map and is struck here.

Status: **QUEUED** (surveyed, viable) · **PICKED** (in progress) · **DONE** (→ H#).

## Hypothesis intake + gatekeeping

Hypotheses may originate from a **survey**, the **orchestrator** (proactively), or a **delegated agent**
(via `proposed_hypotheses` in its return). Log every proposal here with its **origin**, then the
orchestrator **gatekeeps**: ACCEPT → assign an id + track it, or REJECT → log a one-line reason so it's
never re-proposed. Nothing proposed is silently dropped; nothing is silently adopted.

| id | proposed hypothesis | origin | gatekeeper verdict |
|---|---|---|---|
| PH-1 | _the proposed lead_ | survey / orchestrator / agent-<thread> | **ACCEPT → id** or **REJECT — reason** |

| # | lead · source | what it holds | artifact it becomes | status |
|---|---|---|---|---|
| B1 | _the channel / evidence / question_ | _what's in it_ | _the gate / module / differential it becomes_ | QUEUED |

## Rule

A backlog row is a *lead*, not a conclusion. Before building its artifact, re-read the source's own
stated limits, and carry any confidence ceiling forward into the artifact. Do not promote a lead to a
claim without the gate that proves it.
