---
status: accepted
date: 2026-08-16
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Permit automated workflow triggers for verification and maintenance, keeping publication manual

## Context and problem statement

Assumption **A4** held that *repository-authored GitHub workflows must run only
after a person dispatches them*, and `docs/operations/manual-ci.md` enforced it
by forbidding `push`, `pull_request`, `schedule`, and automatic workflow chains
outright. The rule came from a real cost incident recorded in the Trunk gate's
own header: automatic triggers had queued one verification per merge, each
superseding the last, spending far more runner time than they caught.

But the blanket form of the rule blocked work that has nothing to do with that
incident and cannot be done any other way:

- **A merge queue (#20)** is by definition a workflow that runs on `merge_group`
  with no human in the loop. The assumption was the obstacle, not an
  implementation detail around it.
- **A documentation-freshness crawl (#19)** needs to run on a timer, because the
  thing it guards against is the corpus *aging* — a fifth Apollo Environment
  connection setting appeared while nobody re-crawled. The offline age gate that
  exists (`verify-documentation-freshness.mjs`) can fail on staleness but cannot
  refresh; only a scheduled job can.
- **A mutation sweep (#28)** is minutes long, too slow for every candidate, and
  most useful on a cadence.

Overturning a confirmed assumption is the repository owner's call. It was made:
overturn A4 broadly. This ADR records the decision, its cost, and the guardrails
that keep the original incident from recurring.

## Decision drivers

- The cost incident behind A4 was *superseded runs stacking*, not automation as
  such. A guardrail that cancels superseded runs addresses the incident directly.
- Publication, release, signing, and deployment must stay manual — that is
  ADR 0001's driver ("publication must remain manual because hosted Actions time
  is controlled") and assumption A5 ("passing CI does not authorize merge,
  release, publication, or deployment"). Neither is weakened here.
- Automated runner time must stay bounded and least-privilege, or the overturn
  reintroduces exactly the spend it is meant to make safe.

## Considered options

- **Keep A4 as-is.** Blocks #19, #20, and a scheduled #28 permanently.
- **Carve narrow exceptions** for `merge_group` and one scheduled crawl only.
- **Overturn A4 broadly**: permit automated triggers for verification and
  maintenance workflows under cost guardrails, publication excepted.

## Decision outcome

Chosen option: **overturn A4 broadly**. Repository-authored workflows may use
`schedule`, `merge_group`, `push`, `pull_request`, and automatic
`workflow_run` chains when the guardrails below are met. The blanket prohibition
in `manual-ci.md` is replaced by those guardrails.

What does **not** change:

- **Publication and release stay manual.** No automated trigger may sign,
  publish, release, or deploy. `platform-optimizer-candidate-release.yml`,
  `platform-gitless-e2e.yml`, and the release path remain `workflow_dispatch`
  only, and `scripts/verify-optimizer-candidate-release-workflow.rb` and
  `scripts/verify-flux-runtime.rb` continue to enforce that shape on those named
  workflows.
- **The Trunk gate stays dispatch-based** for the reason it always was: it
  authenticates the exact pull-request head SHA before running and bridges a
  commit status the PR rollup cannot otherwise see. A merge queue, when built,
  is a *separate* `merge_group` workflow; it does not change how the Trunk gate
  is invoked.
- **A5 stands.** A green automated run is evidence, never authorization to
  merge, release, publish, or deploy.

### Guardrails

Any repository-authored workflow that runs without a person dispatching it must:

1. **Cancel superseded runs.** Set `concurrency` with `cancel-in-progress: true`,
   keyed so a newer run of the same scope cancels the older — this is the direct
   remedy for the stacking incident that motivated A4.
2. **Cap its runtime.** Set `timeout-minutes` so a hung run fails fast rather
   than billing to GitHub's six-hour default.
3. **Run no more often than it must.** A `schedule` uses the lowest useful
   frequency and states it in a comment; a poll that could be an event should be
   the event.
4. **Stay least-privilege.** Declare a minimal `permissions:` block; never grant
   write scope an automated job does not use.
5. **Never publish.** Perform no signing, publication, release, or deployment.
   Those remain manual per ADR 0001 and A5.

### Consequences

- Good: #19 (scheduled freshness crawl) and #20 (merge queue) become buildable,
  and #28's sweep can move to a timer, each as its own change.
- Good: the stacking incident cannot recur, because guardrail 1 makes it
  structurally impossible for the class of workflow that caused it.
- Bad: automated runner time is now spent without a person in the loop, bounded
  but non-zero. The guardrails cap it; they do not eliminate it.
- Neutral: existing manual workflows are unchanged. This permits new automation;
  it does not convert anything.

### Confirmation

`docs/operations/manual-ci.md` carries the guardrails as the operative rule.
The per-workflow shape checks that already exist —
`scripts/verify-optimizer-candidate-release-workflow.rb` and
`scripts/verify-flux-runtime.rb` — continue to hold the *publication* and
*acceptance* workflows to `workflow_dispatch`, so the "publication stays manual"
half of this decision remains executable, not merely documented.

## More information

- Supersedes: the blanket prohibition previously stated by assumption A4 and
  `manual-ci.md`. ADR 0001's manual-publication driver is preserved, not
  superseded.
- Unblocks: #19, #20, and a scheduled form of #28.
- Assumptions ledger: A4 updated in the same change to point here.
