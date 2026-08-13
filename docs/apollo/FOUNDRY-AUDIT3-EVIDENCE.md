# Foundry audit.3 evidence boundary

Captured public contract: 2026-08-03.

Primary public sources:

- `https://www.palantir.com/docs/foundry/security/audit-log-categories`
- `https://www.palantir.com/docs/foundry/security/audit-logs-overview`
- `https://www.palantir.com/docs/foundry/security/monitor-audit-logs`
- `https://www.palantir.com/docs/foundry/api/audit-v2-resources/log-files/list-log-files`
- `https://www.palantir.com/docs/foundry/api/audit-v2-resources/log-files/get-log-file-content/`

The public documentation establishes that audit.3 events have one or more
standard categories, category-specific `requestFields` and `resultFields`, and
trace correlation. It also states that audit.2 category presence and field
shape are not enforced. This is product-wide schema evidence. It does **not**
prove that a particular tenant invoked an operation, that a named service
implemented it, or that the operation succeeded.

## Authorized source

There are two documented ways to obtain records rather than just schema:

- Preferred SIEM ingestion polls
  `GET /api/v2/audit/organizations/{organizationRid}/logFiles`, preserving its
  `nextPageToken`, and fetches each body from
  `GET /api/v2/audit/organizations/{organizationRid}/logFiles/{logFileId}/content`.
  OAuth applications need `api:audit-read`, and the calling principal needs
  `audit-export:view` on the organization.
- An Organization administrator can create a marked audit.3 export dataset in
  Control Panel with `audit-export:orchestrate-v3`. The dataset is sensitive
  and should be filtered by its partitioned `date` column before analysis.

The wire-schema discriminator is `type: "audit.3"`. The sanitizer also accepts
common SIEM-normalized aliases such as `schemaVersion`, but never accepts an
`audit.2` record as audit.3 evidence. Audit.3 has no historical backfill, so an
absence claim must state the enabled period and searched time window.

## Ground-truth rule

A tenant behavior claim requires an authorized audit.3 record. Interpret the
records in this order:

1. `apiGatewayRequest.operationNames` identifies the observed gateway
   operation name. Join related records using `traceId`; the name alone does
   not establish completion.
2. `requestCreate`, `requestApprove`/`requestDisapprove`, and `requestExecute`
   establish distinct request lifecycle facts. Do not collapse approval into
   execution.
3. `createInfra`, `configureInfra`, or `upgradeInfra` may establish the
   category of the resulting infrastructure action, subject to their required
   request/result fields.
4. `authenticationCheck` and `authorizationCheck` establish the recorded
   checks and outcomes, not the complete implementation of the identity or
   policy engine.
5. A record with only audit.2 shape is corroborating at best and cannot satisfy
   an audit.3 field contract.

## Sanitized capture

`scripts/normalize-foundry-audit3.mjs` accepts an authorized JSON document,
JSON array, `{ "events": [...] }`, or JSONL export. It rejects audit.2, verifies
the bounded category fields Platform relies on, and emits a mode-0600 receipt.
By default the receipt retains category names, timestamps, service names,
field names, value types/counts, and hashes; identifiers and field values are
pseudonymized. Operation names are retained only with the explicit
`--retain-operation-names` option.

```sh
node scripts/normalize-foundry-audit3.mjs \
  --input authorized-audit3.jsonl \
  --output docs/apollo/evidence/tenant-audit3-shapes.json \
  --captured-at 2026-08-03T00:00:00Z
```

The raw export is sensitive and must not be committed. A receipt is admissible
only after review confirms its source channel, time window, tenant scope, and
absence of secrets or personal data. Hashes prove integrity/correlation of the
captured input; they do not disclose or prove semantics that were removed.

For a bounded API capture, use a new directory outside the repository. The
capture command stores exact mode-0600 response bodies plus an identifier-free
hash receipt; it never persists the bearer token:

```sh
FOUNDRY_TOKEN='...' node scripts/capture-foundry-audit3.mjs \
  --hostname https://example.palantirfoundry.com \
  --organization-rid ri.multipass..organization.example \
  --start-date 2026-08-01 --end-date 2026-08-02 \
  --output-dir /secure/audit3-capture
```

Review and normalize each returned body, then destroy or retain the raw capture
according to the organization's evidence-handling policy. Never place raw
audit files in `docs/apollo/evidence` directly.

## Conduit mapping

Conduit persists a provider-neutral audit.3-shaped subset for authorization
decisions and Change Request create/approve/disapprove/cancel/update events.
The validator fails closed for missing required fields and for categories not
in the pinned local subset. These records describe Conduit behavior only; they
are not emitted or represented as Palantir records.
