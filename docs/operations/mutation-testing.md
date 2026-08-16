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
- **Web** console files, via the already-configured Stryker
  (`web/stryker.config.json`), which was configured but never run until this
  gate — the same "validation surface that never executes" this repository has
  found before.

Efficacy is `killed / (killed + lived)`. A **survivor** (a LIVED mutant) is a
change no test noticed — the real signal, and the deterministic one. Efficacy
itself jitters by about a point between runs because a merely-slow mutant lands
as killed on one machine and timed-out on another, and a timed-out mutant is
excluded from the denominator rather than counted; the sweep's regression check
carries a two-point tolerance so that jitter does not read as a real drop.

A full sweep compiles and reruns a suite once per mutant, so it is minutes long.
It cannot run on every candidate and — while assumption **A4** stands — it
cannot run on a timer, so it lives in `.github/workflows/mutation-sweep.yml`,
dispatched deliberately. Raise the baseline where a package improved with:

```bash
scripts/mutation-sweep.sh --write   # rewrites docs/operations/mutation-scores.json
```

The rewrite is a reviewed commit, never automatic — the same discipline the
`internal/controlplane` line ceiling uses.

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
