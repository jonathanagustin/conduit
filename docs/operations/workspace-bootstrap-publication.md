# Workspace bootstrap publication safety

Bootstrap publishes through ordinary fast-forward Git pushes. It does not force
an existing durable branch to match a new source checkout. An explicit Git ref
rejection stops publication and cannot trigger the GitHub API fallback. Git
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
