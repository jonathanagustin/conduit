# Session capability contract

`GET /v1/session` is the console's authority for identity and application
navigation. The response distinguishes the authenticated actor from the
effective principal used for authorization and reports whether delegation is
active.

The projection contains:

- `actor` and `effective`, each with username, teams, truncation, and
  service-account fields;
- `delegated`;
- application IDs with an `enabled` flag;
- operations granted at `platform/root`;
- optional operation lists for requested `resourceRid` values.

The server accepts at most 32 resource coordinates per request, removes
duplicates, sorts them, and rejects a coordinate that is not bounded and
canonical. Team output is capped at 64 entries and sets `teamsTruncated` when
more exist.

The console shows only applications returned as enabled. It must not replace a
failed session request with a hardcoded capability set; it reports that
capabilities are unavailable and leaves application navigation incomplete.
Route handlers still enforce authorization. Hiding a navigation entry is not an
authorization control.

The current application vocabulary is `change-requests`, `fleet`, `plans`, and
`promotions`. This list is a Conduit projection and is not evidence of a private
Apollo capability protocol.

Source anchors: `internal/controlplane/session.go`,
`internal/controlplane/session_test.go`, `web/src/api/session.ts`, and
`web/src/test/e2e/overview.spec.ts`.
