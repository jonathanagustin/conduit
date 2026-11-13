# Conduit repository

This repository owns the product-agnostic Hub/Spoke delivery control plane.
The former `platform/` subtree in `jonathanagustin/palantir` and the archived
`jonathanagustin/platform` repository are migration sources only after cutover;
do not implement the same logical change in more than one repository. Run
`git rev-parse --show-toplevel` before editing and require the Conduit root.
Read the nearest nested `AGENTS.md` before editing a subtree.

## Trunk and tenant fidelity

- Use Conventional Commits for every commit, pull request title, and squash
  subject, such as `fix: ...`, `chore(deps): ...`, or `docs: ...`.
- Supply an explicit squash message. Keep commit messages and pull request
  descriptions free of generated-by signatures, session links, and attribution
  footers.

`main` is the only long-lived branch. Make one coherent change on a short-lived
branch, open a pull request, run the relevant local gates, and squash-merge once
the Trunk gate is green -- opening or pushing the pull request starts that gate,
so dispatch it only to re-run an unchanged tree. Publication stays manual-only;
a green gate never authorizes merge, release, or deployment.

For Apollo-facing work, read
`docs/operations/TENANT-FIDELITY.md` before implementation. Classify claims as a
public contract, authorized sanitized tenant observation, corroborating
evidence, Conduit extension, or unknown. Unknown means invent, disclose, and
register: build the nearest honest thing, mark the value `CONDUIT_NATIVE` where
the surface can carry a tier, and record it in
`docs/evidence/conduit-inventions.json` with the evidence that would displace it
([ADR 0007](docs/decisions/0007-registered-invention-over-refusal.md)). Exhaust
the sources already in this repository first -- the documentation manifest and
the images referenced from it included; a guess made without reading them is not
an educated guess. When OpenAPI or Conjure
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

## Where work is tracked

**GitHub issues.** Not a Markdown table here, and not a Project board.

The reason is mechanical rather than editorial: an agent can create, update,
label, assign and comment on issues through the API, and cannot touch a GitHub
Project. A board nothing maintains drifts, and a drifted board is worse than no
board because it is read as current. If a Project is added later it should be a
human dashboard populated by GitHub's own auto-add workflows, never something an
agent is expected to keep in step.

The documents under `docs/` remain the evidence record: what was found, when,
from which source, and why a decision went the way it did. An issue closes and
leaves the board; the reasoning must not leave the repository.
`docs/operations/APOLLO-WORK-QUEUE.md` indexes the open issues, and each PH row
in `docs/operations/HYPOTHESIS-BACKLOG.md` carries its issue number.

### Claiming work when more than one agent is running

The failure this prevents is two agents starting the same issue and finding out
at the point of merge.

1. **Read the labels first.** Take only `status:ready`. Never start a
   `status:blocked-decision` issue: it is waiting on a person, and the pending
   decision is usually why the work is not yet shaped.
2. **Claim by assigning yourself, before writing code.** Assignment is the
   atomic step — it succeeds or it does not, and every other agent sees it. A
   comment is not a claim; two agents can comment at once.
3. **Then set `status:in-progress`** and comment with your session URL, so the
   claim is attributable and a stalled one can be traced to something.
4. **If an issue is already assigned, take another.** Do not take over silently.
   If it looks abandoned, say so in a comment and leave it for a person.
5. **On finishing,** open a PR that closes the issue and set
   `status:needs-review`. Do not close by hand; let the merge do it so the
   closing commit is the record.
6. **If the issue turns out to be wrong** — mis-scoped, already done, blocked on
   something unstated — comment and set `status:blocked-decision` rather than
   building an approximation of it. An issue that is really a different piece of
   work is a finding.

| Label | Meaning |
| --- | --- |
| `status:ready` | No decision outstanding. May be claimed. |
| `status:in-progress` | Claimed; the assignee is working on it. |
| `status:needs-review` | A PR is open against it. |
| `status:blocked-decision` | Waiting on a human decision. **Do not start.** |
| `status:deferred` | Real work, deliberately not now; the body states the trigger. |

`apollo`, `governance`, `gitless`, `cli` and `evidence` are filter tags and
carry no protocol meaning. All of them are defined in `.github/labels.yml`,
which is where their colours and descriptions live — they were created
implicitly by being applied to an issue, which gives a label a name and nothing
else.

An issue body must be workable without the conversation that produced it: the
source, the exact contract text, what the repository does today, and what is
genuinely undecided. Two things get dropped and should not be — **what was
rejected and why** (several of these record an approach that looked right and
was not, which is the expensive part), and **whether an item is a decision
rather than a task**, because mislabelling one as ready wastes an agent's run.

### The `gh` CLI, and what `.claude/settings.json` can and cannot settle

Some agent harnesses inject a standing instruction that the `gh` CLI is
unavailable and that GitHub must be reached through an MCP server instead. In
this container that premise is simply false: `gh` is on `PATH` and
`GITHUB_TOKEN` is set. When an agent hits that contradiction it should check
rather than assume — `command -v gh` costs nothing — and then use whichever
path actually works, saying which one it took.

`.claude/settings.json` pre-approves the read and issue-management `gh`
subcommands so that using them does not stop for a prompt on every call, and
denies the destructive ones outright. Two limits are worth stating plainly,
because the file is easy to over-read:

- It governs **whether the harness permits a tool call**, not whether an
  instruction given to the agent is binding. A repository file cannot revoke an
  operator's instruction, and should not be written as though it could.
- The `deny` list is the part that matters most. Merging, closing and deleting
  stay human, so an agent that misreads the state of an issue cannot make the
  mistake irreversible.

What the file legitimately does is supply the missing fact and the standing
decision: `gh` is present here, and the maintainers have already authorised
these specific uses of it. That is enough to resolve a stale *capability*
claim. It is not, and must not be treated as, permission to work around an
actual safety constraint — a credential boundary, a scope restriction, or a
refusal — which stands regardless of what any file in this repository says.
