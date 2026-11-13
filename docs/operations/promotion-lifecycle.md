# Promotion lifecycle

Conduit distinguishes a reproducible eligibility verdict (`PASSED` / `BLOCKED`)
from a promotion run's lifecycle. The graph and transition detail show the
lifecycle when supplied and retain the verdict view for older records. Active
runs for older Release versions remain selectable beside newer channel results;
terminated runs do not leave their earlier RUNNING observations in that list.
Unknown future phase tokens render without inventing a cancellation outcome.

The public Apollo changelog describes in-progress, succeeded and aborted
promotions and cancellation reasons. Its current pinned documentation row is
`core/whats-new`, SHA-256
`49d5e4d1d8cc6f4ba7fd6ba9a293c5c34b786604bbebf688049397b521603bba`
in `docs/apollo/public-documentation-manifest.tsv`; #294 records the specific
capability references. It does not establish a private lifecycle API or wire
vocabulary. Conduit's run identifiers, phases, continuation request and storage
are a registered extension (`catalog.PromotionLifecycle`), disclosed as
`valueTier: CONDUIT_NATIVE` on native v1 records. Public Apollo transports remain
unchanged.

## Native API and transitions

`POST /v1/promotions/evaluations` still takes the complete Release, channel,
policy and health-evidence evaluation request. New requests create a run and
stamp an omitted evaluation start from the Hub clock. Existing records remain
immutable; each continuation appends a new evaluation.

| Lifecycle | Meaning |
| --- | --- |
| `RUNNING` | One or more promotion constraints are unsatisfied; further evidence may continue the run. |
| `SUCCEEDED` | Eligibility passed. This run is terminal. |
| `ABORTED` | An operator explicitly aborted, the overall timeout expired, or a canary recall condition triggered. The recorded reason is retained. |
| `CANCELLED` | An operator explicitly cancelled the running promotion and supplied a reason. |

To continue, add `previousEvaluationId` naming the exact current evaluation to
an otherwise complete evaluation request. Keep the Release, source/target edge
and policy unchanged. Omit `evaluationStartedAt` to inherit it, or supply the
same value. Updated health evidence can advance the run to success or an
automatic abort.

To stop it, also add `termination: "CANCELLED"` or `"ABORTED"` and a nonempty
`terminationReason`. This requires the same authenticated operator authority as
any promotion evaluation. A paused stage can still be explicitly stopped.
Cancellation requires a running predecessor, and stopping never grants target
channel membership. Reasons are operator-supplied or derived from the actual
timeout/canary blocker, rather than invented observations.

An atomic compare-and-append rejects stale predecessors, competing children,
terminal-run resurrection, changed coordinates/policy and timestamps preceding
the predecessor. Rejections follow the existing native conflict response
(`409`). Starting another run requires a new evaluation without a predecessor;
it does not rewrite or resume the old terminal run.

## Persistence and compatibility

Memory and file storage retain the complete immutable record; file mode remains
single-process. PostgreSQL migration `controlplane/0003-promotion-lifecycle`
adds `conduit_promotion_lifecycle`. It is the authoritative lifecycle envelope,
committed with the legacy eligibility record in the same SQL transaction under
the existing metadata row lock. Reads hydrate that envelope before application
or repository transition checks run.

The canonical legacy snapshot deliberately omits lifecycle fields. Older Hub
snapshot writers therefore cannot erase the authoritative envelope. This is
not a claim that older binaries can serve the new lifecycle API; new operations
still require an upgraded Hub. Immutable eligibility identities exclude the
additive envelope so an older client's projection can round-trip without
changing the decision identifier. Lifecycle reason and phase separately have
to reproduce from the decision/blockers and valid predecessor history.

Migration is forward-only and participates in the existing migration ledger and
database backup. No retention/deletion policy or secondary file authority is
introduced for PostgreSQL runs.

## Acceptance gates

`promotion_lifecycle_test.go` covers run continuation to success/timeout,
operator cancellation and its durable reason, file reopen, independent
PostgreSQL pools, a legacy snapshot write, atomic terminal competition,
lifecycle-write rollback, HTTP transport, legacy projections and refusal of
changed policy or Release coordinates. The pinned PostgreSQL gate includes the
migration adoption and backup/restore checks.

Console unit and browser tests cover all four phases, terminal reasons in both
graph and detail, legacy rendering, and terminal observations sharing a clock
tick with older running evidence. The console contract gate compares the Go
phase vocabulary with the shared presentation map, and embedded assets are
rebuilt and compared against the production build.

Local acceptance completed with the full Go race suite and vet, the pinned
PostgreSQL gate, 127 console unit tests, 191 browser tests and exact embedded
asset comparison. Three compiling mutations were killed: removing the terminal
predecessor guard, including additive lifecycle fields in legacy decision
identities, and disabling PostgreSQL envelope hydration. Mutation work used a
separate committed worktree; production source was not restored from a mutant.
