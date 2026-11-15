# Native organization snapshots

The existing Organization view remains read only and uses the existing Hub directory, grant, policy, identity, session, readiness and public signing-key reads. Successful empty arrays remain empty. Denied, malformed and unavailable reads clear old records and show unavailable states; the view no longer substitutes example teams or grants. User and group membership counts are separate, and stored grants are not presented as a complete effective-access calculation.

Organization discovery/banner placeholder editing controls have been removed from this administration surface. Personal preference views are separate. Refresh snapshot performs GET reads only. Reads have a 15-second timeout. A snapshot is hidden while loading and discarded if the observed native identity changes during the read; a later render reloads for that identity. A cached snapshot is not a real-time permission decision, and all backend authorization remains authoritative.

The session identity shown on the health tab comes from the session response, not an editable display-name preference. Failed identity reads are unavailable rather than an assertion that sign-in is disabled. Readiness is the Hub's repository-read check, not workload health. The published signing key is not claimed to be the full trusted-key store. Missing metadata is not replaced by assumed values.

Settings-only refreshes update settings views and shared chrome without replacing unrelated active workspaces. An awaited organization refresh therefore preserves focus and text in an in-progress change-review draft. Hub request/revision changes still use the existing review-context invalidation.

This implements the Admin snapshot portion of #558. The broader studio, builder, engineer-workspace and ontology enhancements remain open in that issue. No user administration, policy editing, permission change, publication, second approval flow or private reference-platform protocol is added.
