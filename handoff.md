# Handoff

A snapshot of the work done in the session ending 2026-08-17, what is merged,
what is still open, and the mechanisms a next contributor should know before
touching this tree. Everything is on `main` unless marked otherwise.

The previous handoff covered the session ending 2026-08-16 (PRs #30–#44). Its
"Mechanisms" and "Bootstrapping" sections are carried forward below, corrected
where this session proved them wrong.

## What shipped this session

### #145 — a GitHub outage no longer decides whether the code is good

The Trunk gate posted its commit status with a bare `gh api` call under
`bash -e`. Two consequences, both real rather than theoretical, and both hit
during this session's multi-hour 503 on GitHub's commit-status API:

- The **opening** status post ran before the 30 verifications. A transient 503
  there aborted the whole job, so an outage in a status API decided whether the
  code was tested.
- The **closing** post ran after them. A 503 there turned an all-green run red.

Both calls now go through `scripts/retry-github-api.sh` (four attempts,
exponential backoff) and **neither can fail the job**. Proven by
`scripts/test-retry-github-api.sh`, which is itself a gate.

This is why runs in this period show `TRUNK_STATE: success` with a failing final
step and a green job — that is the fix working, not a broken gate. It also means
**a PR can be fully verified and show no status badge**; read the run, not the
badge.

### #144 — the signal that separates a wedged Hub from a busy one

`conduit_reconciliation_pending`, `_leased`, `_exhausted` and
`_oldest_pending_seconds` per queue, plus `conduit_build_info`. Depth alone
cannot distinguish a stuck Hub from a busy one — both sit at some number — so
the lag gauge is the one that matters.

**A correction worth keeping, because it was mine.** I read a stale `pending`
commit status as a hung test, invented `ACCESS EXCLUSIVE` lock contention to
explain it, and weakened a test to avoid a hazard that had no evidence behind
it. Run 47's log ends `TRUNK_STATE: success` — every gate passed, including
PostgreSQL — and the only failure was a 503 on the final status POST. The commit
was reverted and the record corrected in the branch and the PR body. **A missing
status badge is not a failing test**; that mistake is now cheap to repeat and
worth guarding against.

### #146 — schema migrations applied once, and recorded

`internal/pgmigrate` applies an ordered slice of named migrations once per
database and records them in `conduit_schema_migrations`. Each migration commits
**in one transaction with the row that claims it ran**, so there is no state in
which the ledger is wrong. Both schemas (`reconciliation`, `controlplane`) are
on it; the idiom is written down in `docs/operations/schema-migrations.md`.

**An adversarial review found the design's central claim false, and it was.**
The first version took one advisory lock keyed on the component and claimed that
solved the concurrent `CREATE TABLE IF NOT EXISTS` hazard. Two different
components hash to two different keys and exclude each other not at all: four
components racing a fresh database failed **36 times in 20 rounds** with
`SQLSTATE 23505` on `pg_type_typname_nsp_index`. Reachable in production — a Hub
opens the control-plane repository *and* the reconciliation store. Fixed by
creating the ledger under a fixed lock every caller shares, taken and released
before the per-component one.

Five further findings, all fixed: a best-effort unlock that could strand a lock
on a pooled connection for `ConnMaxLifetime`; a concurrency test flaky in the
*passing* direction; a retype guard that knew `ALTER COLUMN c TYPE` but not
`ALTER COLUMN c SET DATA TYPE`; an adoption test running migration 0002's
backfill against an empty outbox; and no adoption test at all for the control
plane.

Also removed: the `-run '^TestPostgres'` filter in
`scripts/test-postgres-repository.sh` for two of its three packages. That filter
encoded a naming convention nobody wrote down and silently excluded new
database-backed tests, which then ran **nowhere** — skipped locally for want of a
DSN and filtered out in CI. `cmd/platform-delivery` keeps its filter deliberately.

### Sandbox skills (`.claude/skills/cluster`, `.claude/skills/containers`)

The environment knowledge needed to run k3s, Docker, podman and a real
PostgreSQL in a web session, as two skills with their scripts. On branch
`claude/adversarial-review-run-52-kxnmvk`.

**One substantive change from the upstream copy: fix 5 was ported in.** This
repository's own `scripts/bootstrap-k3s.sh` resolves the runc to wrap by reading
the data-bin directory from the running server's `PATH`
(`/proc/<pid>/environ`); the skill's copy used `data/current`, which goes stale
once more than one k3s version has run — the wrapper is then installed where
nothing reads it while every pod keeps failing with the fix apparently applied.
`bootstrap-k3s.test.sh` covers five cases; reverting the resolution kills three
of them, including the one that reproduces the field failure.

Still referenced and still absent: `containers/docs/USE-CASES.md` and the
`ccw-sandbox:diagnose` skill.

## Environment facts this session established (read before re-deriving them)

- **Docker is installed and its daemon is not running.** `scripts/start-docker.sh`
  starts it and works as shipped. An earlier note in this session that "Docker is
  unavailable" was wrong.
- **Docker Hub's blob CDN is blocked by network policy.** `docker pull` of the
  pinned PostgreSQL image fails with `403 Forbidden` from
  `production.cloudfront.docker.com`, so **`scripts/test-postgres-repository.sh`
  cannot run in this sandbox at all** — the daemon is fine, the pull is refused.
  Confirm with `curl -sS "$HTTPS_PROXY/__agentproxy/status"`, which lists the
  rejection under `recentRelayFailures`.
- **Run the PostgreSQL-backed tests against the installed server instead.**
  PostgreSQL 16.13 is present. This is the exact path used to verify #146:

  ```bash
  export PATH=/usr/lib/postgresql/16/bin:$PATH
  mkdir -p /tmp/pgdrill && chown postgres:postgres /tmp/pgdrill
  su postgres -c "initdb -D /tmp/pgdrill/data -U conduit --auth=trust"
  su postgres -c "pg_ctl -D /tmp/pgdrill/data -o '-p 5434 -k /tmp/pgdrill' -l /tmp/pgdrill/server.log start"
  psql -h 127.0.0.1 -p 5434 -U conduit -d postgres -c 'CREATE DATABASE conduit_test'
  export CONDUIT_TEST_POSTGRES_DSN="postgres://conduit@127.0.0.1:5434/conduit_test?sslmode=disable"
  go test -race ./internal/controlplane \
                 ./internal/delivery/reconciliation/adapters/postgres \
                 ./internal/pgmigrate
  ```

- **Never judge a gate by a piped exit code.** `./scripts/test-postgres-repository.sh | tail -20`
  reports `tail`'s status, which is always 0. A failing gate read as passing
  here until the log was actually read.
- **CI was unreliable for reasons outside this repository**: a multi-hour 503 on
  the commit-status API, and a 429 on the `actions/setup-go` download that killed
  run 52 during `Set up job` before any step ran. Re-dispatching is the correct
  response to the latter and to nothing else. Run 54 is the green one for #146.

## Open branches and PRs

- **`claude/adversarial-review-run-52-kxnmvk`** — this session's skills work,
  pushed. Two commits.
- **`claude/schema-migration-ledger`** — merged as #146; the branch is stale and
  can be deleted.
- **#114 and #115 (Renovate, Rust crates).** `base64` `0.22.1 → 0.23.1` and
  `sha2` `0.10.9 → 0.11.0`. **Do not merge these without building the crate.**
  Both are breaking-version bumps: `base64` 0.23 raises the MSRV to 1.71 and adds
  a default-on `simd-unsafe` feature, and `sha2` 0.11 is a major RustCrypto
  release. Their base is also stale (`89d5534`). They need a `cargo build`/test
  against `solver/`, which was not run this session.

## Next work, already scoped

The production-readiness record (`docs/operations/PRODUCTION-READINESS.md`)
Tier 1 rows, in the order they are worth taking:

1. **Backup, restore and disaster recovery (gap 4) — designed, not started.**
   The only Tier 1 row with *nothing* behind it: no backup method, no restore
   drill, no RPO/RTO, for the database holding desired state, decisions, the
   audit trail and the outbox. The scoped design:
   - `pgmigrate.Pending(ctx, db, component, migrations)` — a dry run reporting
     what `Apply` would run without running it. Must **not** create the ledger
     (check `to_regclass` first), or a dry run mutates the schema. This also
     closes the first "what remains" bullet of gap 3.
   - A restore drill: seed both components through their real constructors,
     `pg_dump --format=custom` **while writes are in flight**, restore into a
     fresh database, then assert (a) nothing pending for either component, so the
     ledger survived intact, (b) state matches through the real repositories, and
     (c) no torn `decision`/`audit`/`outbox` triple — `CommitDecision` writes all
     three in one transaction, so a backup that tears them is worthless.
   - `docs/operations/backup-and-restore.md`: the procedure, RPO/RTO, and what
     is out of scope.
   - Where it goes: `cmd/platform-delivery` already opens both the control-plane
     repository and the reconciliation store against one DSN, and its
     `^TestPostgres` filter means a test named `TestPostgres…` there will run.
   - Watch the client/server version coupling: `pg_dump` must be at least as new
     as the server. The runner's client is 16 and the pinned image is what it is;
     if they ever diverge the drill should fail loudly rather than skip.

   **Research already done for this document — do not re-derive it.** A scout
   swept the deployment and persistence surface; the load-bearing findings:

   - **The database is BYO and its provisioning is unspecified.** The chart
     consumes only a DSN from a pre-existing deployment-owned Secret, mounted
     read-only as a file; it never creates a StatefulSet, Service, operator CR or
     subchart (`deploy/helm/platform-hub/values.yaml:13-17`,
     `templates/deployment.yaml:145-151`, `Chart.yaml` has no `dependencies`).
     `scripts/verify-helm-runtime.rb:130-131` pins the *complete* set of rendered
     kinds, so a database StatefulSet cannot be added without failing the gate,
     and `:160-161` forbids a Hub PVC in postgres mode. No operator or managed
     service is named anywhere in the repo. The only place backup responsibility
     is assigned is `docs/decisions/0002-transactional-hub-persistence.md:51-52`
     — "PostgreSQL becomes a production dependency that the Environment owner
     must operate and back up."
   - **Artifact blobs are IN PostgreSQL**, not an OCI registry:
     `conduit_repository_blobs (digest, descriptor, contents bytea)` at
     `internal/controlplane/postgres_repository.go:31-36`. So one `pg_dump`
     covers metadata *and* artifact bytes — and the dump is as large as the
     artifacts. The OCI registry in the gitless path is a delivery source, not
     Hub state.
   - **A restore needs more than the database, and one gap is silent.** If
     `BUNDLE_SIGNING_KEY_FILE` is absent the Hub logs a warning and generates an
     **ephemeral** Ed25519 key (`cmd/platform-delivery/main.go:75-77`), so
     previously issued Bundle signatures stop verifying against the running key.
     Everything else fails closed and loudly (`main.go:96-121`, `:315`,
     `:329-351`). The full required set is pinned at
     `scripts/verify-helm-runtime.rb:204-205`. No secrets backup or rotation
     procedure exists anywhere.
   - **Two things already state the key invariant — cite them, do not contradict
     them.** `docs/operations/schema-migrations.md:117-122` already says restore
     is not covered and that a consistent backup holds both the migration and its
     ledger row or neither; `internal/pgmigrate/migrate.go:255-262` carries the
     same reasoning as the justification for a plain `INSERT`.
   - **Confirmed absent everywhere** (searched docs, scripts, deploy, CI, code):
     any RPO/RTO figure; any backup command (`pg_dump`, `pg_basebackup`,
     pgBackRest, Barman, wal-g); any WAL archiving or PITR configuration
     (`archive_mode`, `wal_level`, `restore_command`); any restore procedure,
     drill or script; any DR runbook; any PostgreSQL HA/failover guidance; any
     VolumeSnapshot policy. The gap statement is accurate as written.
   - **Startup coordination is PostgreSQL advisory locks, not Kubernetes leases**
     (`pgmigrate.Apply` from both components), and runtime work coordination is
     DB-row leases with a 30 s lease duration
     (`cmd/platform-delivery/main.go:260-264`). Two replicas are enforced by
     `verify-helm-runtime.rb:194` with a PDB at `maxUnavailable: 1`. Restore
     ordering has to account for both replicas racing `Apply` on first start.
2. **Gap 3 remainder** — no schema verification against the ledger (a row asserts
   a migration ran; it does not detect a column dropped by hand), and no upgrade
   rehearsal against a production-shaped dataset.
3. **Observability (gap 2)** — no tracing, metrics only under
   `REPOSITORY_KIND=postgres`, endpoint off unless `METRICS_ADDRESS` is set, and
   no alerting rules ship.
4. **Notifications (gap 1)** — a whole subsystem: no ports, no store, no HTTP
   surface, no emit sites, no console inbox.

One further research subagent was still running when this session ended and its
findings did not survive: classifying every table as authoritative or derivable,
and deriving SQL predicates for the cross-table invariants a torn backup could
violate (starting from `CommitDecision`'s decision/audit/outbox triple, and
covering `UpsertDesired`, `UpsertObservation`, `LeaseWork`, `FailWork`,
`RepairDroppedWork`, `LeaseProposal`, `CompleteProposal`, `FailProposal`). Worth
re-running — those predicates are what the drill asserts.

## Mechanisms a next contributor must know

- **Registered invention / provenance tiers.** The Apollo GraphQL interface
  serves a coordinate from real Conduit data, or from a value Conduit invented
  that says so on the wire. It never nulls — a null is a claim of absence. Under
  [ADR 0007](docs/decisions/0007-registered-invention-over-refusal.md) an invented
  value carries `@provenance(tier: OBSERVED, valueTier: CONDUIT_NATIVE)`. ADR 0007
  supersedes 0005 and reopens #17. See `internal/apollographql/`.
- **Schema changes go through `internal/pgmigrate`.** Position is the order, the
  name is identity and never changes once merged, and each migration commits with
  the row recording it. Forward-only by decision, not omission. Read
  `docs/operations/schema-migrations.md` before adding one; it has the
  add-nullable / backfill / constrain idiom and the rules
  (`CREATE INDEX CONCURRENTLY` cannot appear in a migration — every one runs in a
  transaction).
- **The controlplane line ceiling** (`docs/operations/controlplane-ceiling.json`)
  only moves down; raising it requires a justification in the same commit.
- **Fixtures must be corrected from the Go source, never from what makes a test
  pass.** The console's browser tests mock the Hub, so a fixture using an
  invented status passes while lying. The contract gate guards the status
  vocabularies; the discipline still applies to any new reader field.
- **The embedded console bundle** (`internal/consoleui/dist`) is committed and
  checked byte-for-byte. After any `web/src` change run `pnpm --dir web build`
  and commit the regenerated bundle.
- **Local verification:** `./scripts/verify-local.sh` runs every gate the Trunk
  gate runs (it reads `.github/workflows/platform-ci.yml` so the two cannot
  drift). CI is manual (`workflow_dispatch`); dispatch per
  `docs/operations/manual-ci.md`. A green run is evidence, not authorization to
  merge or release (assumption A5). In this sandbox the PostgreSQL gate cannot
  run — see the environment section above for the substitute.
- **Mutation testing:** one hand-planted mutant at a time via
  `scripts/verify-mutant-killed.sh` to prove a specific gate rejects. Two traps
  seen this session: a mutation applied with shell escaping that mangled the
  anchors was **not applied at all**, so its "survival" was void; and a mutation
  can be killed *for the wrong reason* (a driver type error rather than the
  property under test), which is not a kill.
- **The merge queue (#20) is not a setting to flip.** GitHub offers merge queues
  on public repositories, and on private ones only under Team or Enterprise
  Cloud, so this private repository under a personal account has no control at
  all. `merge-queue.yml` stays inert unless the repository moves to an
  organization or becomes public. If it ever does, the required check must become
  `Merge queue`, not `Trunk gate`: `platform-ci.yml` is `workflow_dispatch` and
  cannot run on a `merge_group`.

## Bootstrapping a fresh session

Live PR subscriptions and scheduled self-check-ins are session-local and do not
carry over; re-`subscribe_pr_activity` for anything that needs watching.

### First message to paste into the new session

```
Work on jonathanagustin/conduit. Attach it with add_repo if it isn't in scope,
then clone and register it.

Before doing anything, read these to load context -- do not skip:
- handoff.md                                  (this file: state, open items, gotchas)
- docs/operations/PRODUCTION-READINESS.md     (the Tier 1 gaps and what remains)
- docs/operations/manual-ci.md                (CI is manual workflow_dispatch)
- docs/operations/schema-migrations.md        (how a schema change is made here)
- docs/operations/ASSUMPTIONS-LEDGER.md       (A1-A9; A4 overturned by ADR 0006)
- docs/operations/local-verification.md
- AGENTS.md and docs/operations/AGENTS.md

Adopt these working rules for this repo:
- New branch per change; open a PR; never push to main. CI is manual -- I
  dispatch the Trunk gate (platform-ci.yml) against the PR head.
- Run ./scripts/verify-local.sh before handoff. The PostgreSQL gate cannot run
  in this sandbox (the image pull is blocked); use the local PostgreSQL 16
  recipe in handoff.md instead, and say which path you used.
- Verify by reading output, never by a piped exit code, and never by a status
  badge -- an absent badge here often means GitHub's status API failed, not that
  a gate did.
- Ground every Apollo/Hub-contract claim in the code; never reason from
  plausibility. Console fixtures come from the Go source, not from what makes a
  test pass.
- Registered invention (ADR 0007): serve every coordinate, from real data or
  from an invention that discloses itself. Never null.
- internal/controlplane has a line ceiling that only moves down.
- After any web/src change, run `pnpm --dir web build` and commit the
  regenerated internal/consoleui/dist bundle.
- Put design decisions and premise-reversals through an adversarial review
  before implementing. It has now overturned a central design claim twice --
  most recently the migration ledger's locking, which was false as written.

Then tell me the open issues and PRs and which decisions still need me, and wait
for direction. Read the live issue list rather than any to-do list quoted here.
```

### Shell bootstrap (rebuild the toolchain)

```bash
cd /workspace/conduit || (git clone <clone-url-from-add_repo> /workspace/conduit && cd /workspace/conduit)
git fetch origin && git checkout main && git pull

# Toolchains verify-local.sh needs on PATH (it fetches pinned helm/hadolint/zarf itself):
#   Go (go.mod toolchain go1.26.6), Node 24.x + pnpm 11.x, ruby, jq -- usually preinstalled.
pnpm --dir web install --frozen-lockfile

# Only for the mutation SWEEP (not the Trunk gate): pinned gremlins.
GOTOOLCHAIN=go1.26.6 go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

# Prove the tree is where we left it:
node scripts/verify-console-contract.mjs        # 7 vocabularies match the Hub
node scripts/verify-mutation-scores.mjs         # mutation baseline intact
./scripts/verify-local.sh                        # full gate set
```
