# Planning source checkout identity

Hub planning and the packaging agent resolve the requested repository and ref
through the same isolated Git checkout adapter. Each checkout returns the real
commit resolved from `HEAD^{commit}`. It does not return a workspace's mutable
directory, cached metadata or an invented commit identifier.

An existing workspace for the same repository may contain another branch or
uncommitted files. Planning does not reset, delete or overwrite that workspace,
and another owner's matching workspace does not become an implicit source cache.
`CONDUIT_DEMO_SOURCE_DIR` no longer replaces the requested repository. Offline
tests use temporary Git repositories served over loopback HTTP, with real refs
and commits, rather than relabeling a plain directory as a remote commit.

Intake rejects option-like refs and refspec destinations. The clone boundary
also validates refs when called directly. Missing branches, tags or commits fail;
they do not fall back to an unrelated `HEAD`. A branch moving between planning
turns is detected by the existing source binding and requires a new planning run.

The clone adapter's transport proxy restricts Git requests to the requested host.
This is not a process filesystem or network sandbox; workspace-container
isolation remains a separate boundary. The Excalidraw recording in #615 must use
actual pinned source and build evidence, not a fixture source identifier.

Tests exercise distinct branches, moved heads, dirty and broken cached
workspaces, ignored demo overrides, invalid refs, checkout cleanup and existing
workspace preservation. Both Hub and executable-agent entry points are covered.
