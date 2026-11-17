# Scoped workspace studio

Packaging's existing Workspace, Source, Preview and Lifecycle views now mount the native workspace panel. The previous source/preview/lifecycle aliases landed on a tab without a renderer. The Change Requests diff remains its own existing route; workspace diffs are read-only sections within the studio.

The controller scopes caches to the observed native identity and selection. Mount/selection generations and independent read generations reject late index, directory, file, diff, runtime and log results. Changing a workspace or source context clears old file/diff/runtime data. File path/digest and diff workspace/revision/source coordinates must match. Directory pagination is bounded to 256 entries with a notice; source, diff and log displays are bounded. Untrusted file content is escaped and download destinations are limited to the selected workspace's same-origin API paths.

The selected workspace drives the area header and default inspector instead of an unrelated packaging run. Inspector cache identity includes the native caller, and workspace revision/source changes rebind its snapshot. Selecting no workspace does not substitute a packaging run.

Owner actions first stage a visible draft. Editing coordinates clears confirmation. Fork/review titles and metadata-upgrade coordinates are explicit input; there is no synthetic source digest or implicit next version. A fresh workspace GET checks current actor/owner and recorded context before the existing POST. This is a client preflight, not a server-side compare-and-swap transaction. Returned receipts are checked where the existing API exposes coordinates. Exact write attempts are not automatically retried during the console instance, including after an uncertain response or navigation.

Runtime stop uses the preview-stop endpoint. Runtime and log origin, status and available revision/artifact fields are shown as returned snapshots; no deployment or lease is implied. Metadata upgrades do not prove files changed. Review submission requests `approve:false` and accepts only a pending/rejected separate review, backed by the mandatory-human submission fix. The UI never approves, includes work in a Plan, publishes, or invokes direct workspace deletion. Governed deletion, real branch-backed editing and commit history remain separate backend work.

The local process preview shares the host filesystem and network. It does not provide container isolation. Governed workspace commands use the isolated container runtime in `docs/operations/workspace-isolation.md`: non-root, no host container socket, default-deny egress except the operator allowlist, and no cluster credentials. The posture is container isolation now, stronger isolation planned. A rendered Pod or a cooperative dial check is not that boundary.

This contributes to #558. It reuses existing authenticated APIs, the existing review flow and existing assistant/inspector surfaces. No new dependencies or private protocol claims are introduced.

