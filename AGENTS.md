# Conduit repository

This repository owns the product-agnostic Hub/Spoke delivery control plane.
The former `platform/` subtree in `jonathanagustin/palantir` and the archived
`jonathanagustin/platform` repository are migration sources only after cutover;
do not implement the same logical change in more than one repository. Run
`git rev-parse --show-toplevel` before editing and require the Conduit root.
Read the nearest nested `AGENTS.md` before editing a subtree.

## Trunk and tenant fidelity

`main` is the only long-lived branch. Make one coherent change on a short-lived
branch, open a pull request, run the relevant local gates, manually dispatch the
Trunk gate for the exact commit, and squash-merge. CI and publication stay
manual-only; a merge never authorizes release or deployment.

For Apollo-facing work, read
`docs/operations/TENANT-FIDELITY.md` before implementation. Classify claims as a
public contract, authorized sanitized tenant observation, corroborating
evidence, Conduit extension, or unknown. Unknown means do not invent: keep the
gap explicit or use a clearly Conduit-native contract. When OpenAPI or Conjure
exists, generate the transport interfaces and keep domain behavior behind the
bounded adapter.

- Keep application Product catalogs, charts, credentials, environment
  inventories, and installation configuration in their product repositories.
- Preserve immutable Product/Release coordinates, outbound-only Spoke
  execution, explicit approval, exact reported state, and rollback behavior.
- Do not claim compatibility with private Palantir Apollo protocols. Public API
  and CLI claims must remain bounded by pinned fixtures and conformance tests.
- Run `go test -race ./...`, `go vet ./...`, and the relevant script/E2E gate.
- Release artifacts use semantic tags, exact OCI digests, SBOMs, and available
  provenance/attestation mechanisms.

## Zenith operating model

For substantial reconstruction, migration, or release work, read
`docs/operations/AGENTS.md` and its ledgers before editing. Use the adapted
orchestrate → verify loop: decompose by independently testable capability,
name the ground-truth source and acceptance gate, preserve a single writer per
file set, and independently rerun every reported gate before commit. Delegate
only when the user or applicable higher-priority instructions authorize it.

The complete repository-local Zenith playbook is pinned under
`tools/zenith-protocol/`; read
`tools/zenith-protocol/skills/zenith-protocol/SKILL.md` for a substantial
cycle. Its ledgers resolve through `.zenith-protocol.json`. Do not publish the
vendored private plugin or treat its workflow as authority to delegate, commit,
push, release, or deploy.

Write domain findings to their canonical evidence or contract note in the same
change, reflect closure state in the capability map, gatekeep new hypotheses,
and record load-bearing assumptions. Record changes to the working method first
as measured PDR trials; append objective measurements rather than relying on
memory. Run `node scripts/verify-zenith-protocol.mjs` at the commit boundary.
Zenith records index evidence; they do not replace executable tests.

## Mutation testing

`scripts/run-mutation-testing.sh <package>` runs [gremlins](https://github.com/go-gremlins/gremlins)
over one package and lists the mutants that survived.

It is a review tool, not a gate. A full pass over `internal/controlplane` is
roughly four hours, because mutation testing recompiles the package under test
once per mutant and that compile is always a cache miss. Run it deliberately
against a package you are changing.

Stryker does not support Go, so it cannot cover this repository. The eight
`.mjs` verification scripts have no unit tests for Stryker to kill mutants
against — they are the tests.

### Reading the output

A `LIVED` mutant is a line the tests execute and do not constrain: change it and
nothing fails. That is the finding. `NOT COVERED` means no test reaches the line
at all, which is ordinary coverage and a different problem.

Some survivors are equivalent mutants and cannot be killed. Read the list rather
than chasing the percentage. Efficacy of 100% is a reason to check the tests
assert something, not a target.

These classes account for every equivalent survivor found in a full pass over
this module. Each needs the argument, not just the label — "probably equivalent"
is how a real one gets filed away:

- **Maximum selection.** `>` against `>=` where a loop keeps the largest so far.
  They differ only when the two compare equal, and then both assign a value that
  compares equal to the other. Check nothing downstream can tell the two apart by
  identity rather than by comparison.
- **Sort comparators over keys unique by construction.** `<` against `<=`. Equal
  keys would mean the same record twice.
- **Capacity hints.** `make(map[…], len(a)+len(b))` — arithmetic that only sizes
  an allocation.
- **Truncation to a length the value already has.** `len(v) > n` against `>=`,
  where the branch does `v = v[:n]`. At exactly `n` that is a no-op.
- **Assignments that are already no-ops at the boundary.** `if x < 1 { x = 1 }`
  against `<= 1`.
- **A comparison the surrounding code has already decided.** For example `>`
  against `>=` on candidate-versus-current where reaching the line at all
  requires the current one to be excluded by the next clause.

Two look equivalent and are not:

- A guard whose inversion makes the result depend on **map iteration order**
  rather than making it wrong. It passes a single run some fraction of the time.
  Assert the property that is actually missing — repeat the call and require the
  same answer — rather than filing it as equivalent.
- A bound another rule has already made unreachable. It cannot change what is
  *accepted*, but it can still change **which error** the caller gets, and that
  is testable.

### Re-checking survivors without a second full pass

The survivor list ages the moment a test lands, and a four-hour pass to find out
which entries are stale is not worth running. Replaying only the survivors is:
apply the operator gremlins names at the line and column it reports, run the
suite, and record whether it now fails. Two hundred survivors take about an hour
sharded across copies of the module, and the question answered -- is this mutant
alive today -- is the same one.

Three things make the replay trustworthy.

**Commit before you plant a mutant.** The kill check ends by restoring the file
the mutant went into, and `git checkout` cannot tell your work from the mutant --
it discards whatever is uncommitted in that file. The rule is often stated as
"restore the source, never the test", which is right and incomplete: restoring
the source is only safe when the source is clean. Verifying a mutant in a file
you are still editing throws your edit away, and the throw-away is silent.

**A passing gate does not vouch for the commit after it.** If a file-restoring
command runs between the two, the gate measured something the commit does not
contain. That is not hypothetical here: a gate passed on an implementation, the
kill check restored the file, and the commit that followed shipped the test
without the code it tested -- a build break on main that every local signal had
just called green.

**Check the harness against known answers, in both directions** -- one mutant
that must now be killed and one proven equivalent -- before believing a list of
two hundred.

### Two traps

The default timeout coefficient is far too low here. A first run reported all 42
mutants of `internal/grypescanner` as `TIMED OUT` at 0.00% efficacy, which reads
as a catastrophic result and is entirely a tool artifact. The script pins 20,
which is right for the sub-second packages and too low for `internal/controlplane`:
gremlins multiplies its **coverage** baseline of about a second, while a killed
mutant there takes nearly seven, so 20 still timed out about one mutant in six
and 100 timed out none. Since timed-out mutants leave the ratio rather than
counting as survivors, the tighter timeout reports the *higher* efficacy. Read
the TIMED OUT count before believing the percentage.

More importantly: **verify a new test against the mutation it targets.** Write
the test, then change the line it claims to cover and confirm the test goes red.
Three times while building the current suite a test passed on first write and
constrained less than it appeared to — one asserted against a duplicated copy of
the production comparator, one covered a boundary only on one side, one omitted
the equality case that `>` and `>=` disagree about. Each looked complete. A test
that passes tells you nothing about whether it would fail for the right reason.

Nested instructions:

- `cmd/AGENTS.md`: executable composition, startup, and identity configuration.
- `internal/AGENTS.md`: control-plane and agent invariants.
- `deploy/AGENTS.md`: reusable runtime and environment-ownership boundary.
- `.github/AGENTS.md`: CI and release workflow rules.
- `docs/operations/AGENTS.md`: adapted Zenith ledgers and conformance rules.
