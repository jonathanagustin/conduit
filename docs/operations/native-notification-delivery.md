# Native notification delivery

Issue #119 adds Conduit-owned SMTP and Slack submission behind the existing
PostgreSQL inbox delivery queue. This is a native extension, not a claim about
private Apollo notification transports. `SLACK` preferences are registered as
`notifications.SlackChannel`; the captured resource-setting vocabulary establishes
only EMAIL and IN_APP. Existing generic HTTPS relay and logging modes remain
available. PagerDuty contacts remain stored but undeliverable.

## Configuration

Delivery is disabled when no delivery settings are supplied. Native delivery
requires the PostgreSQL repository; file mode rejects configured delivery at
startup. Upgrade every Hub/dispatcher replica before enabling SLACK preferences:
older binaries do not understand the new channel. No schema migration is needed
because the stored channel columns are text.

| Variable | Meaning |
| --- | --- |
| `NOTIFICATION_SMTP_ADDRESS` | SMTP hostname and numeric port, required with SMTP settings |
| `NOTIFICATION_SMTP_FROM` | Sender mailbox, required with SMTP settings |
| `NOTIFICATION_SMTP_TLS_MODE` | `starttls` (default, required upgrade) or implicit `tls`; plaintext is refused |
| `NOTIFICATION_SMTP_USERNAME_FILE` | Mounted SMTP username file; requires password file |
| `NOTIFICATION_SMTP_PASSWORD_FILE` | Mounted SMTP password file; requires username file |
| `NOTIFICATION_SMTP_CA_FILE` | Optional mounted PEM certificates added to system trust |
| `NOTIFICATION_SLACK_TOKEN_FILE` | Mounted Slack bot token with `chat:write` access to intended channels |
| `NOTIFICATION_NATIVE_TIMEOUT` | Per-submission duration, default 10s, positive and below the 30s queue lease |

SMTP and Slack can be enabled together. Native settings cannot be combined with
`NOTIFICATION_WEBHOOK_URL`, its timeout setting, or enabled delivery logging.
Partial configuration fails startup. Credential files are bounded to 64 KiB;
one trailing line ending is removed. Credentials and trust are loaded from
files, never persisted in notification records or included in adapter errors.
Mount them using the deployment's secret mechanism. No default credentials or
new product-specific Helm values are introduced.

## Source intent and recovery

For a PostgreSQL Hub, migration `0004-notification-intents` stores source intent
in the same database and SQL transaction as source metadata and its audit row.
This covers pending review, changed grant authority, failed Plan execution,
aborted promotion and recalls in an accepted desired state. The production
`platform-delivery packaging-worker` installs the same source factory before
submitting proposals (#914). The standalone file-backed packaging fixture and
file Hubs have no notification store and make no durable notification claim.

The intent freezes consent when preferences are read through that source
transaction, including its own uncommitted preference writes. It stores the
event, owner/channel candidates and preference identifiers, never destinations.
Concurrent preference edits follow PostgreSQL transaction visibility; this is
not a claim of serial ordering with every subscription edit. A later worker
does not expand the captured set, even when it was empty. It checks current
resource authority before admitting inbox entries; current destination and
authority checks still govern external submission. Changing or disabling a
subscription does not cancel an already captured IN_APP candidate or erase
history. Native transport resolution separately rechecks the current preference.

Admission starts immediately and polls every second, including deployments
with no external transport. Each intent has a one-minute lease, at most ten
persisted failed admission attempts, and retry delay doubling from one second,
bounded at one hour. A process interrupted before acknowledgment leaves the
intent available after lease expiry; immutable event recording prevents a lost
acknowledgment from duplicating recipients. Cancelled work is not acknowledged.
An intent insert failure aborts the source transaction before success is
returned. Nested rollback and cancelled savepoint release leave neither source
metadata nor intent behind. Historical source transitions are not backfilled.

Pending, completed and exhausted intents remain in
`conduit_notification_intents`; retries and exhaustion are logged by the
admission worker. A completed intent means event/inbox admission, not external
delivery or human receipt. A known authorization denial may yield a completed
event with an empty audience. Unknown authority retries the whole admission
without exposing a partial audience. Exhausted intents retain their bounded
`last_error`; they are never silently deleted or reset. There is no automatic
requeue endpoint, notification metric or alerting rule. After addressing the
cause, exhausted work requires explicit operator repair; preserve its event ID
and frozen candidates rather than re-emitting against today's preferences.

An operator with database access can inspect counts without reading contact or
event payloads:

```sql
SELECT count(*) FILTER (WHERE completed_at IS NULL AND NOT exhausted) AS pending,
       count(*) FILTER (WHERE completed_at IS NOT NULL) AS completed,
       count(*) FILTER (WHERE exhausted) AS exhausted
FROM conduit_notification_intents;

SELECT event_id, attempt, available_at, lease_expires_at, exhausted, last_error
FROM conduit_notification_intents
WHERE completed_at IS NULL
ORDER BY available_at, event_id;
```

This private handoff is registered as `notifications.ProducerIntent`; captured
Apollo notification settings establish no source-transaction or queue protocol.

## Routing and delivery semantics

Before recording an event's audience, the Hub checks each matching owner against
current resource-read authority. Removing a native user from a team or removing
a team's read grant prevents new events reaching its stale subscriptions
(#884). A user is checked using current native `AdminUsers`/`MemberUsers` and
the deployment's standing bootstrap policy; a team must still exist and hold
its own current authority. The emitter's actor and old subscription-time claims
are never substituted for the recipient. This is Conduit authorization policy,
not an assertion about private Apollo notification behavior.

Background checks cannot refresh an operator's OIDC group claims: no trusted
current external directory is configured. A definite native grant or bootstrap
allow remains sufficient. If only external group membership could authorize a
user, the check fails explicitly with unavailable current group membership;
use an appropriately granted team subscription or current native membership.
Unknown directory/grant lookups abort admission rather than committing a
partial audience. PostgreSQL source intents retain that obligation for bounded
retry rather than losing it when the originating request ends (#869).

Every external transport rechecks authority after destination resolution,
immediately before submission. A definite refusal records a terminal queue
failure with an authorization reason; it never marks the item delivered.
Lookup uncertainty follows the existing bounded retry budget and likewise
never counts as sent. These checks do not delete historical inbox entries or
decide a new policy for reading already-received notifications.

Recording the same event ID again preserves its first committed document and
recipient snapshot, including an empty snapshot. A subscriber added after the
event does not receive historical notifications when an emit site retries.
Issue #872 found that the PostgreSQL adapter ignored a duplicate event insert
but still inserted newly resolved recipients and queued their delivery. The
event insert now decides ownership of the entire fan-out; concurrent callers
commit one complete audience rather than their union. The event and inbox
entries still roll back together when an entry cannot be recorded. Regression
tests exercise changed and empty audiences, independent concurrent connections,
unchanged event documents and delivery counts against PostgreSQL.

Saving an empty type selection with future types off records explicit opt-out
for the same resource, owner and channel (#905). It affects subsequent fan-out
using that updated preference; it does not cancel an already selected audience,
historical inbox entry or queued delivery. The tuple's ID and creation time are
retained and its revision advances, so it can be loaded and subscribed again.
Off consent requires no team destination, including when a previous contact was
deleted. Existing selected contacts are carried by the console, and the API
retains its existing full-replacement semantics. Turning notifications off grants
no resource or owner authority. This is Conduit policy and makes no private
Apollo API compatibility claim.

EMAIL selects SMTP, SLACK selects Slack, and IN_APP remains inbox-only. The
native resolver re-reads the current resource preference and owner contacts at
delivery time. It requires the queued preference ID, resource, owner and channel
to match. When `contactId` is set, only that exact contact belonging to the owner
and matching the channel is eligible. Active team preferences require that selection.
User preferences without a contact ID use the owner's matching channel contact.
A deleted preference, missing contact, foreign contact, wrong contact kind or
unconfigured provider never falls back to another recipient. Store read failures
remain retriable. Generic relay mode retains its existing resolution semantics.

The queue retains its lease, durable attempt budget and exponential retry curve.
Slack HTTP 429 `Retry-After` sets a minimum retry delay without resetting that
budget. Provider refusals that cannot be repaired by retry are permanent; other
failures exhaust the existing bounded budget. Provider reply text, credentials
and recipient addresses are omitted from adapter errors. Existing delivery-state
inspection remains the operator's failure surface; this change adds no alerting
system or address-reachability preflight.

SMTP verifies TLS 1.2 or newer before optional AUTH PLAIN, uses mailbox envelopes
and MIME encoding, and acknowledges delivery only after DATA acceptance. A
failed QUIT after acceptance does not cause retry. Slack sends JSON to the fixed
`https://slack.com/api/chat.postMessage` endpoint, refuses redirects, disables
Markdown and unfurls, escapes Slack mention delimiters, and requires `ok: true`.
Unknown or malformed acknowledgements remain failures. Successful submission
proves provider acceptance, not that a person received or read the message.
Delivery is at least once: a crash after provider acceptance but before the queue
acknowledgement can duplicate submission. SMTP uses a stable logical Message-ID;
that is not a provider deduplication guarantee.

## Sources and acceptance

SMTP uses Go's SMTP client and the SMTP/STARTTLS protocol boundaries. Slack's
public contract is pinned to `slackapi/slack-api-specs` commit
`bc08db49625630e3585bf2f1322128ea04f2a7f3`,
`web-api/slack_web_openapi_v2_without_examples.json`, POST `/chat.postMessage`.
No authorized private Apollo per-resource Slack preference schema is available.

The native adapter tests use local TLS SMTP servers and isolated HTTP clients;
they send no real email or Slack message. They constrain authentication ordering,
certificate verification, envelopes/encoding, acknowledgement handling,
redirection, timeout and error privacy. Application tests constrain selected
contact ownership, provider routing and retry minimums. PostgreSQL integration
uses independent pools to fan out and drain EMAIL, SLACK, team-selected SLACK
and IN_APP entries without repeating acknowledged deliveries. Console coverage
checks SLACK selection, URL persistence and requests at the browser boundary.
