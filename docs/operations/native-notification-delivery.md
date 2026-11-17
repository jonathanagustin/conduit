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

## Routing and delivery semantics

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

EMAIL selects SMTP, SLACK selects Slack, and IN_APP remains inbox-only. The
native resolver re-reads the current resource preference and owner contacts at
delivery time. It requires the queued preference ID, resource, owner and channel
to match. When `contactId` is set, only that exact contact belonging to the owner
and matching the channel is eligible. Team preferences require that selection.
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
