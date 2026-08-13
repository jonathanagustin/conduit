# Session capability projection

`GET /v1/session` is the authenticated, read-only UI projection of the Hub's
existing governance model. It helps the console render only deployed
applications the caller can use. It does not grant authority, replace endpoint
authorization, or reveal credentials.

## Contract

- `actor` is the authenticated principal. `effective` is the delegated
  principal when on-behalf-of execution is active, and `delegated` makes that
  distinction explicit.
- Identity output contains username, service-account status, and at most 64
  teams. `teamsTruncated` is true when more exist. Identity-provider groups,
  authorization headers, tokens, and credentials are never projected.
- `globalOperations` evaluates `platform/root`. Optional repeated
  `resourceRid` queries return only the effective caller's operations for that
  coordinate; they do not attest that a resource exists.
- A request may contain at most 32 resource coordinates. Each is at most 512
  bytes and must match the bounded canonical RID grammar. Results are
  deduplicated and sorted.
- Operations come from the fixed 11-operation public governance vocabulary.
  The response cannot reflect arbitrary stored strings as UI authority.
- `applications` currently publishes the four implemented console surfaces:
  Change Requests, Fleet, Plans, and Promotions. Change Requests and Plans are
  available to authenticated callers because their list endpoints filter each
  record. Fleet and Promotions require global read because their list routes do.
  Planned Export, Import, Risk, and Teams screens are not advertised.

The client also keeps a fixed application-ID allowlist. Unknown server IDs are
ignored, and an unreadable session produces an explicit warning with no
hardcoded application fallback. Primary navigation remains independent where
its native list endpoints already apply per-record authorization.

## Evidence

- `go test -race ./internal/controlplane -run TestSession`
- `pnpm --dir web typecheck`
- `pnpm --dir web playwright test src/test/e2e/overview.spec.ts`
- The `ConsoleShell` page object owns stable structural test IDs and domain
  actions. Link targets, visible names, keyboard behavior, and accessibility
  remain independently asserted through semantic browser APIs.
