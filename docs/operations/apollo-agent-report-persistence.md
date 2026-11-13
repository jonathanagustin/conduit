# Apollo agent report persistence

Apollo's public compatibility API accepts info and health as separate complete
snapshots. Conduit retains both snapshots per Environment and normalizes them
against the latest Plan in one storage transaction. A request to another Hub
replica therefore uses the same node filter, quorum, lifecycle and namespace
information. A native normalized report does not erase those raw inputs.

This persistence strategy is a Conduit extension, not a claim about Apollo's
private storage architecture. Public transport schemas remain generated and
unchanged. The application owns the merge policy; the compatibility storage
adapter retains the raw JSON without adding transport types to native domains.

## Atomicity and upgrades

The PostgreSQL migration `controlplane/0002-apollo-agent-reports` creates
`conduit_apollo_agent_reports`. The existing metadata row lock serializes
replacement of either raw snapshot and the normalized report. Both writes use
the same SQL transaction. The separate raw table has one authority and survives
legacy metadata snapshot writers; it is included in normal database backups.
As before, an older application binary must not be used to serve new writes:
it does not know how to merge shared raw inputs.

Memory storage uses an isolated transaction copy, including raw input bytes.
File mode remains limited to one process. It atomically replaces one report
file containing the normalized report and private raw inputs. Existing report
files decode without conversion. Native report writes preserve the private
inputs. Inputs received before a Plan persist as pending data and do not appear
as a fabricated normalized report in native reads or inventory.

Non-nil input members replace a complete snapshot, including an empty list;
an omitted member retains the other request's last committed snapshot.
Projection and storage errors fail the request and commit neither the new raw
input nor its normalized projection. Requests with no Plan retain inputs for
the next report after a Plan exists. Existing normalized state can provide the
quorum floor when raw info has never arrived; no missing observation is invented.

## Verification

`internal/controlplane/apollo_agent_reports_test.go` exercises restart/reopen,
native report interleaving, current-node filtering, namespace and lifecycle
propagation, simultaneous info/health posts, empty replacement, pre-Plan input,
rollback and invalid Environment paths. The PostgreSQL cases use independent
pools, and an injected database write rejection proves the normalized report
cannot commit without its raw inputs. `scripts/test-postgres-repository.sh`
runs them against the pinned disposable database, alongside migration adoption
and the backup/restore drill. The application quorum-floor regression remains.
