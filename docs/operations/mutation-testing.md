# Mutation testing

Coverage answers "did a test run this line". Mutation testing answers the
question that actually matters: "would a test have failed if this line were
wrong". A line can be run by a test that would pass whether or not the line is
correct; mutation testing is what finds those.

Three things do this work here, at three different scopes.

## The one-mutant harness — proving a named claim

`scripts/verify-mutant-killed.sh` plants a single, hand-specified mutant and
reports whether the suite rejects it. It is the right tool for proving one
claim in a pull request — "this new gate refuses this exact change" — and it
is trusted in a dangerous way (it corrupts a source file and then says the tree
is fine), so `scripts/verify-mutant-killed.test.sh` proves it restores
byte-identically and never plants in a dirty or untracked file. That self-test
runs in the Trunk gate as **Validate mutation harness**.

It cannot tell you about a defect nobody imagined. Every mutant it plants is one
a person thought to write down.

## The sweep — measuring a score nobody planted

`scripts/mutation-sweep.sh` runs a real mutation engine over whole packages,
mutants and all, and records an efficacy per unit:

- **Go** bounded-context domains, via `gremlins` (`.gremlins.yaml`):
  `internal/{environments,governance,catalog,apollographql,reporting}/domain`.
  `internal/controlplane` is deliberately excluded — it is the largest and
  least-changing target, and the domains are where the rules live.
- **Web** console files only when `web/stryker.config.json` is present.
  The replacement console currently has no Stryker configuration and the
  committed web baseline is empty; frontend mutation efficacy is unmeasured.
  Removing configuration while recorded frontend targets remain fails closed.

Efficacy is `killed / (killed + lived)`. A **survivor** (a LIVED mutant) is a
change no test noticed — the real signal, and the deterministic one. Efficacy
itself jitters by about a point between runs because a merely-slow mutant lands
as killed on one machine and timed-out on another, and a timed-out mutant is
excluded from the denominator rather than counted; the sweep's regression check
carries a two-point tolerance so that jitter does not read as a real drop.
The configured timeout coefficient is 100: warm coverage baselines can be much
faster than compiling a mutant. A fresh measurement containing any TIMED OUT
mutant is rejected; the engine omits those mutants from its denominator and
could otherwise turn untested survivors into a misleading 100% score.

A full sweep compiles and reruns a suite once per mutant, so it is minutes long.
It is separate from the candidate gate in `.github/workflows/mutation-sweep.yml`,
which permits a capped monthly maintenance measurement and manual dispatch.
Neither path publishes or commits results automatically. Raise the baseline where a package improved with:

```bash
scripts/mutation-sweep.sh --write   # rewrites docs/operations/mutation-scores.json
```

The raw score always remains `killed/(killed+lived)`; equivalent mutations
remain LIVED and are never relabeled KILLED. Individually reviewed equivalents
are listed in `mutation-equivalents.json`, with exact file hashes, operator,
line, column and the argument for equivalence. A missing/changed source, missing reviewed
mutation, duplicate identity, or reviewed mutation no longer LIVED fails the
classification. Unreviewed survivors remain in the quality denominator.

The existing quality floor is applied to `nonEquivalentEfficacy`, recorded
separately from raw `efficacy`. This avoids changing production behavior or
inventing tests merely to reject a no-op. A changed source requires renewed
review and measurement; source hashes prevent a general operator exemption.

The rewrite is a reviewed commit, never automatic. The runner compares each
measured efficacy with its existing tolerated floor independently of the engine's
exit status. A regressed result refuses `--write` and leaves the baseline intact.
`scripts/test-mutation-sweep.sh` checks this with failed and misleadingly
successful engine exits, plus missing configuration for recorded web targets.
These fixture results are not measurements; only an actual engine sweep can
refresh the committed baseline.

## The ratchet — cheap on every candidate

`scripts/verify-mutation-scores.mjs` runs in the Trunk gate as **Validate
mutation scores**. It measures nothing and reaches nothing; it reads
`docs/operations/mutation-scores.json` and fails when the recorded baseline has
rotted: a hand-edited efficacy that does not follow from its own killed/lived
counts, an entry pointing at a package or file no longer on disk, or a capture
more than ninety days old, at which point "no regression" has stopped meaning
anything and a fresh sweep is due. `scripts/verify-mutation-scores.test.sh`
proves it rejects each of those rather than only ever passing, and runs in the
same gate.

The division of labour is the point: the sweep produces a number that can
regress and be caught, and the ratchet — nearly free — refuses to let that
number rot silently between sweeps.

## Refreshed measurement

The Go 1.27.4 / gremlins v0.6.0 source-built sweep completed with zero timed-out,
non-viable or skipped mutants in every package. Values below are actual engine
counts; reporting's reviewed equivalents remain included in its LIVED column.

| Package domain | Killed | LIVED | Reviewed equivalents | Raw efficacy | Non-equivalent efficacy |
| --- | ---: | ---: | ---: | ---: | ---: |
| `internal/environments/domain` | 67 | 10 | 0 | 87.01% | 87.01% |
| `internal/governance/domain` | 104 | 3 | 0 | 97.20% | 97.20% |
| `internal/catalog/domain` | 112 | 22 | 0 | 83.58% | 83.58% |
| `internal/apollographql/domain` | 25 | 3 | 0 | 89.29% | 89.29% |
| `internal/reporting/domain` | 56 | 14 | 14 | 80.00% | 100.00% |

The reporting tests now cover exact freshness boundaries, future-clock clamping,
report-time fallback, release/configuration drift, every summary status count,
authorization rebuilds, and deterministic row/facet ordering. Its raw result
improved from the first refreshed sweep's 28 killed / 37 LIVED to 56 / 14.
Those remaining 14 cases have individual arguments in the source-bound manifest.
The other packages retain their unreviewed survivors; this is not a claim that
all executable faults are detected. Frontend mutation efficacy remains unmeasured.

The expired historical reporting number (20 killed / 2 LIVED) covered an older,
smaller mutation surface. It is not reused as a current measurement or hidden
behind a date-only update.
