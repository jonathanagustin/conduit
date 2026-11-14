# Dependency update maintenance

Renovate creates reviewable pull requests; it does not merge them automatically.
Major updates require dashboard approval, and the normal three-day release-age
policy remains in force. Container references retain explicit tags and digests.

The mise Grok tool uses an HTTP backend in `mise.lock`, with versions supplied by
`https://x.ai/cli/stable`. Renovate's default mise mapping instead looks for the
nonexistent GitHub repository `x.ai/cli/grok`. The scoped `grok-stable` custom
plain-text datasource replaces that lookup for this tool only; it does not change
the installed version or download URL.

The official stable feed exposes a version without release timestamps. Renovate
therefore keeps its updates pending under `minimumReleaseAgeBehaviour`'s default
`timestamp-required` policy. A successful lookup is not proof that a release has
aged three days. This configuration does not fabricate timestamps or exempt the
tool from release-age review.

The configuration is checked with Renovate's strict config validator. A local
lookup dry run can verify extraction and datasource selection without writing to
GitHub; GitHub-tag lookups need a GitHub read credential, while the Grok HTTP
lookup requires none. Do not forward that credential into local CI jobs.

The runtime-base refresh in PR #478 updates the explicit reviewed-index guard
together with the Hub/Agent Containerfiles. The new Chainguard index is
`sha256:fc42af33e643f20d08baf03cddcc1c02ba57052dfb02edb0d79f3fc47bcea8d9`;
its registry manifest includes linux/amd64 and linux/arm64, and the pulled amd64
image retains configured UID 65532. The CP-SAT runtime index is
`sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab`;
it also includes both architectures and retains UID 65532 on amd64. These checks
establish availability and image metadata, not a new signature/provenance
claim or a completed build of every application image. The exact-digest guard
remains enforced; changing a tag alone cannot satisfy it.
