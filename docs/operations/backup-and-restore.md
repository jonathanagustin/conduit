# Backup, restore and disaster recovery

The procedure for backing up a Conduit Hub's PostgreSQL database, restoring it,
and knowing what is still missing afterwards. It closes Tier 1 gap 4 of
[`PRODUCTION-READINESS.md`](PRODUCTION-READINESS.md), which recorded that no
document stated a backup method, a restore drill, or an RPO/RTO.

Every claim here is either executable or cited. The drill is
`TestPostgresBackupAndRestoreDrill` in `cmd/platform-delivery`, run by
`scripts/test-postgres-repository.sh`; the numbers below that a deployment owns
rather than this repository are marked as such, because a document that invents
an RPO it cannot enforce is worse than one that says who has to choose it.

## What is in the database, and what is not

**In it, and recoverable from a `pg_dump` alone:**

| Held by | Tables |
| --- | --- |
| `controlplane` | `conduit_repository_state` (the versioned metadata snapshot: environments, plans, change requests, grants), `conduit_repository_blobs` |
| `reconciliation` | `conduit_reconciliation_desired`, `_observations`, `_work`, `_decisions`, `_audit`, `_proposal_outbox`, `_plan_receipts` |
| `gitless-resolution-log` | `conduit_gitless_resolutions` (the append-only source selector, resolved digest, publisher, movement, and failure history shared by Hub replicas) |
| all components | `conduit_schema_migrations` — the migration ledger |

**Artifact bytes are in the database.** `conduit_repository_blobs` holds
`contents bytea` (`internal/controlplane/postgres_repository.go`), so one dump
covers metadata *and* artifacts — and the dump is as large as the artifacts. The
OCI registry on the gitless delivery path is a source Conduit reads, not Hub
state; restoring it is not part of this procedure.

**Not in it, and a restore is incomplete without them.** The chart pins the
complete set at `scripts/verify-helm-runtime.rb:231-232` (`required_hub_env`):

| Secret | If it is missing on restore |
| --- | --- |
| `BUNDLE_SIGNING_KEY_FILE` | **Fails closed at startup.** `checkBundleSigningPosture` in `cmd/platform-delivery/main.go` refuses to start a Hub that has not declared itself a development Hub, naming the variable and what to set it to. It used to fail *open*: the Hub warned, generated an ephemeral Ed25519 key and reported healthy while every Bundle it signed stopped verifying against the signer the fleet trusts. That is why it is still the first thing to check — a restore that forgets it now stalls visibly rather than rejoining the fleet under a new signing identity. Which signer a Hub did come up with is on its startup line as `bundleSigner`, the base64 public key `BUNDLE_TRUSTED_KEYS_FILE` would carry. |
| `POSTGRES_DSN_FILE` | Fails closed at startup. |
| `TLS_CERT_FILE`, `TLS_KEY_FILE` | Fails closed at startup. |
| `AGENT_CLIENT_CA_FILE` | Fails closed; no Agent can bind. |
| OIDC issuer configuration, `PLATFORM_BOOTSTRAP_ADMINISTRATORS` | Fails closed; no operator can authenticate. |

**Back the signing key and the Agent client CA up wherever the deployment keeps
its secrets, and restore them with the database.** No secrets backup or rotation
procedure exists in this repository, and adding one is out of its scope: the
Secret is created and owned by the deployment repository, not by the chart.

## Who runs the database

Nobody in this repository. The chart consumes a DSN from a pre-existing,
deployment-owned Secret mounted read-only as a file
(`deploy/helm/platform-hub/values.yaml:13-17`,
`templates/deployment.yaml:145-151`); `Chart.yaml` declares no dependencies, and
`scripts/verify-helm-runtime.rb:130-131` pins the complete set of rendered kinds,
so a database StatefulSet cannot be added without failing that gate. The
responsibility is assigned in
[ADR 0002](../decisions/0002-transactional-hub-persistence.md): *"PostgreSQL
becomes a production dependency that the Environment owner must operate and back
up."*

That is why the numbers below are a floor and a method, not a service level.

## Taking a backup

```bash
pg_dump --format=custom --file=conduit-$(date -u +%Y%m%dT%H%M%SZ).dump "${CONDUIT_DSN}"
```

The Hub's chart does not run the database, so there is no container here to
borrow a client from; use one at least as new as the server (see below).

`--format=custom` rather than plain SQL: it is compressed, it can be restored
selectively, and `pg_restore --exit-on-error` fails on a partial archive instead
of leaving a half-built database behind.

**The dump does not need the Hub stopped, and must not require it.** `pg_dump`
takes one transactional snapshot, and every invariant Conduit depends on is
established inside a single transaction — `CommitDecision` writes the decision,
its audit record and its outbox entry together
(`internal/delivery/reconciliation/adapters/postgres/store.go`), and each schema
migration commits with the ledger row that claims it ran
(`internal/pgmigrate/migrate.go`, `applyOne`). So a snapshot holds all of a
transaction or none of it. The drill proves that rather than assuming it:
it takes its dump **while decisions are being committed**, and then checks the
restored database for a decision without its audit record, an audit record
without its decision, and a queued proposal without the decision that produced
it.

**Client and server versions are coupled, and this is not theoretical.**
`pg_dump` refuses a server newer than itself. The gate's pinned image is
PostgreSQL 18; a CI runner's client is 16. A drill built on the runner's client
could not have taken a single backup.

So the drill uses **the server's own client**:
`scripts/test-postgres-repository.sh` passes
`CONDUIT_TEST_PG_TOOL_PREFIX="docker exec -i <container>"` and the server's
in-container address, and the archive moves over stdin and stdout because that
client does not share the test's filesystem. The version matches by construction
and nothing extra is installed on the runner. With the variable unset the tools
are taken from `PATH`, which is the path a developer with PostgreSQL installed
takes.

**Operators should do the same thing.** Take the dump with a client at least as
new as the server — the server's own container or host package is the reliable
way to be sure. The drill reads `server_version_num`, compares it, and fails
rather than skips when the client is older, so backup coverage cannot quietly
end.

## Restoring

1. **Stop the Hub, or restore into a database no Hub is pointed at.** Both
   replicas race `pgmigrate.Apply` on first start (see below); a restore
   underneath a running Hub is a race with writes.
2. **Restore into an empty database.**

   ```bash
   createdb "${TARGET}"
   pg_restore --dbname="${TARGET_DSN}" --exit-on-error --no-owner --no-privileges conduit-….dump
   ```

3. **Ask what a start would apply, before starting anything.**
   `pgmigrate.Pending` reports what `Apply` would run without running it and
   without creating the ledger table it reads. On a correctly restored database
   the answer is empty for every component; anything else means the backup did
   not carry the ledger intact, and the Hub would migrate a database that is
   already migrated.
4. **Restore the secrets**, above — the signing key first, because it is the one
   whose absence does not stop the Hub.
5. **Start the Hub and verify through it, not through the tables.** Row counts
   prove the bytes arrived; only the real repositories prove they still decode.
   The drill reads a Plan back through `ListPlans`, an artifact back through
   `OpenBlob` and compares the bytes, desired state back through
   `CurrentDesired`, and an exact Gitless resolution digest back through the
   shared history table after opening its real adapter.

### Two replicas on first start

Startup coordination is PostgreSQL advisory locks rather than Kubernetes leases:
both replicas call `pgmigrate.Apply`, the second blocks on the first and then
finds the ledger already recording every migration. The chart runs two replicas
(`verify-helm-runtime.rb:194`) with a PDB at `maxUnavailable: 1`. Nothing extra
is needed for a restore — but a restore that brings the Hub up before the
database is fully restored gives both replicas a partial schema to migrate, which
is why step 1 is first.

Runtime work coordination is separate: database-row leases with a 30-second
duration (`cmd/platform-delivery/main.go:260-264`). Leases held at the moment of
the backup are restored held, and expire on their own.

## RPO and RTO

**These are the deployment's to set. What this repository can state is what its
own mechanisms bound.**

| | Value | What determines it |
| --- | --- | --- |
| **RPO** | the backup interval | A `pg_dump` schedule alone gives an RPO of one interval. There is no WAL archiving or PITR configuration anywhere in this repository (`archive_mode`, `wal_level` and `restore_command` are absent from every chart, script and manifest), so continuous recovery is a property of the database the deployment owner runs, not of anything Conduit ships. |
| **RTO** | restore time + one Hub start | Restore time is proportional to the dump, which is dominated by artifact bytes. The Hub's own contribution is one start: migrations are a no-op on a restored database, which is what step 3 checks. |

**Suggested floor for a deployment with no other requirement:** a daily dump
(RPO 24 h) is enough only if losing a day of decisions, audit trail and queued
proposals is acceptable. It usually is not for the audit trail. A deployment
that needs better should enable PITR on its own PostgreSQL and treat the dump as
the disaster copy rather than the primary mechanism.

## What is deliberately out of scope

- **A backup schedule, a retention policy, or a CronJob to run it.** The chart
  does not own the database and cannot own its backups.
- **PostgreSQL HA, failover and PITR configuration.** Same reason. ADR 0002
  records node and database failover behaviour as unproved, and that is still
  true.
- **Secrets backup and rotation.** Named above as a requirement, owned by the
  deployment repository.
- **A down path.** Migrations are forward-only by decision
  ([`schema-migrations.md`](schema-migrations.md)); recovering from a bad
  migration means restoring a backup and rolling forward, which is what makes
  this document load-bearing rather than optional.
- **Verifying a restored schema against the migrations that claim to have built
  it.** A ledger row asserts a migration ran; it does not detect a column
  somebody dropped by hand. That is gap 3's remainder, not this one — a restore
  reproduces whatever the backup held, including hand damage.
