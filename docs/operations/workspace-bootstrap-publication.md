# Workspace bootstrap publication safety

Bootstrap publishes through ordinary fast-forward Git pushes. It does not force
an existing durable branch to match a new source checkout. An explicit Git ref
rejection stops publication and cannot trigger the GitHub API fallback. Push
credentials are supplied through per-process configuration rather than command
arguments; returned push errors omit captured credential-bearing output.

The API fallback is create-only. It reads the exact requested ref before
creating objects. An existing different head is a conflict; an exact existing
local head is an idempotent read. Authentication, cancellation, malformed ref
responses and unexpected lookup failures stop the operation. GitHub's specific
empty-repository response is distinguished from other conflicts.

After absence is established, the fallback creates a source snapshot and uses
GitHub's atomic ref creation. If another writer creates that ref first, the
operation returns a conflict. It never PATCHes or force-updates the competing
ref. Executable and symlink blob modes are retained. Unsupported tree entries,
including gitlinks, stop API publication instead of being silently omitted.
Native Git publication continues to support those entries.

This fallback does not update existing API-created branches or pretend that a
snapshot commit has the original source Git SHA. Existing branch updates need a
working Git transport and compatible history. A failed idle preservation push
continues to retain its local volume under the existing retention policy. Do not
work around that failure by resetting or force-pushing a durable repository.

Tests use local bare repositories, a loopback HTTP fixture and a synthetic Git
executable for the credential-argument check. They cover initial and
fast-forward publication, divergent remote work, non-fast-forward rejection,
authentication, lookup failures, cancellation, a competing ref creator and
source file modes. No live repository is changed during verification.

## Source ingestion safety

Source ingestion prepares a new checkout in a sibling staging directory and
promotes it only after successful import and ref resolution. Populated
checkout destinations, destination symlinks and overlapping source/destination
paths are rejected. Local source aliases are resolved before that comparison.
Failure removes only this call's staging directory; it does not erase an
existing checkout. A competing writer that populates the destination prevents
promotion. An existing empty destination retains its permissions.

Git sources resolve the requested branch, tag, full ref or commit ID to an
actual commit. The requested `main` is checked even when the source's default
branch differs. Missing refs fail instead of using that default. Plain
unversioned directories support `main` or `HEAD` as a newly committed snapshot;
other refs are unavailable. File copies stream through a source-root handle,
retain executable bits and stop between files when cancelled.

These checks protect ingestion and publication boundaries. They do not provide
process isolation or an atomic snapshot of a concurrently edited plain source
directory. The workspace sandbox remains a separate runtime concern.
