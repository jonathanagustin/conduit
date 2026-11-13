# Previous-release database upgrade rehearsal

Run `./scripts/test-upgrade-data.sh /tmp/old-data-receipt.json` with Go selected
by the current `go.mod`, Python 3, Git, tar and Docker available. Fetch the tags
first. The manual **Old data upgrade** workflow runs the same script with full
history, read-only repository permission and a twenty-minute limit. It neither
publishes artifacts nor connects to a supplied database.

The runner selects the newest stable `vX.Y.Z` tag whose resolved commit differs
from current HEAD. It records both immutable commits, archives their sources,
and compiles the same fixture against each revision's unmodified production
repository, reconciliation and Gitless constructors. Both builds use the
current compiler; this tests source-version compatibility, not the exact
executables shipped in the old release. The fixture and runner hashes are
recorded separately from the production source commits. The foundation example
comes from the old revision and remains fixed between builds.

The previous binary seeds one disposable loopback-only PostgreSQL 18 database:
500 Teams, 100 disabled approval policies, 100 lifecycle-created Plans, 50
Environment reports, 1,000 authorization audit events, ten approximately 88 KiB
artifacts, 100 desired-state/decision/audit/proposal groups, 100 additional
desired states with queued work and 50 Gitless
resolutions. Metadata objects are written in a single real transaction; blobs
and reconciliation decisions use their production write paths. Empty tables
cannot satisfy the comparison. These are synthetic fixtures, including opaque
reconciliation payloads that no dispatcher executes; they are not captured
tenant data, production traffic, or executable proposal acceptance evidence.

The old binary reads the populated state back before upgrade. The current
binary opens the same database, times constructor migration and adoption, and
reads every seeded Team, policy, Plan, report, audit and desired snapshot
through production repositories. It also compares every artifact byte and
descriptor, and all physical desired, work, decision, audit, proposal and
Gitless rows, including serialized documents and identities. Counts alone
cannot establish preservation. Every old object field must survive unchanged;
additional fields within objects are allowed, but missing, extra, reordered or
changed records fail. Reopening the current constructors must preserve the
first current snapshot and leave no pending migrations in the three shared
fixture components. The runner removes its container and temporary sources on
exit and writes a receipt only after success.

The receipt records logical object counts separately from physical SQL rows:
metadata is stored as a snapshot, so 1,000 audit objects in an old snapshot are
not 1,000 old SQL rows. It records constructor elapsed time, binary hashes,
source commits, compiler and PostgreSQL image digest. Timing includes opening
the repositories and their adoption work, not just DDL, and is a measurement
of this stated synthetic dataset on this host, not a production capacity bound.

This addresses the populated-data part of #263. It does **not** run a Hub
server or Agent, establish either direction of wire-version compatibility,
roll two serving replicas, renew an in-flight lease, or establish backward
audit-reader compatibility. Those remain production blockers until separately
rehearsed. There is no down migration or implicit rollback claim.
