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
