# Native notification-event read over Connect

The existing Audit notification-events panel now calls the generated unary
`conduit.notifications.v1.NotificationService.ListNotificationEvents` client.
It remains a resource-authorized, polled latest-event window. It is not a
subscription, outbox, resume protocol or complete authorization audit.

| Existing route | RPC | Source |
| --- | --- | --- |
| `GET /v1/notifications/events?resourceRid=…&limit=…` | `POST /conduit.notifications.v1.NotificationService/ListNotificationEvents` | `internal/notifications/adapters/httpapi/httpapi.go`, `internal/notifications/application/emitter.go`, `internal/notifications/domain/notifications.go` |

These are Conduit-native contracts, based on source at
`4c2cf6836e81df3d11c7cbf1d93598b77ef2b353`, not evidence of a private upstream
notification API. The proto is in
`proto/conduit/notifications/v1/notification_event.proto`; `task proto` regenerates
committed clients and the existing proto gate checks lint, FILE compatibility
and deterministic output. The banner normalizer now leaves authored files
without generated markers untouched; its idempotence/authored-source tests run
in that same gate.

## Authority and availability

Both transports use the same principal resolver, resource-read authorizer,
limit parser and application Reader. The composition's native operator carrier
is preserved for resource authorization; the represented user's name does not
replace the carrier or bootstrap-administrator policy. Neither request can
select an actor, subscribe, emit, approve or alter an event.

Authentication runs before reading the RPC body. A valid resource request from
a principal with no grant reaches 403; the matrix probe sends a valid Connect
body and headers rather than accepting an incidental protocol error as proof
of authorization. Unknown RPC methods are not served. A deployment with no
notification Store continues to mount neither REST nor RPC event reads; the
client reports that absence as 404/unavailable rather than an empty stream.

The shared query also checks that every returned event belongs to the authorized
resource and that the Store did not exceed the applied limit. A bad source
fails with 500 on both transports. RPC backend-failure messages are sanitized.
The UI retains its independent response-scope check, generation/identity checks,
abort controller and pause-on-failure behavior.

## Exact query and record semantics

`resourceRid` is required and trimmed, as on REST. The proto's optional int64
`limit` preserves the difference between absence (the native ceiling of 100)
and explicit zero (400). Positive limits above 100 are bounded to 100 by the
same native Reader. Typed malformed protobuf values are protocol errors;
valid values still go through native resource authorization before domain limit
validation. Native notification validation errors map to `HubError(400)`;
overview's existing 422 mapping remains separate. Authentication and permission
failures retain 401/403.

The response retains `resourceRid`, the applied `limit` and ordered `events`.
Each record preserves its stable id, open event-type string, resource,
environment, summary, optional actor and timestamp. Timestamp conversion keeps
nanosecond precision using the protobuf JSON formatter. There is no inferred
total, fabricated event, cursor or exactly-once promise. Empty authorized results
stay empty; denied, malformed or unavailable reads do not establish emptiness.

The Audit client sends only the selected visible resource and limit 100, uses
same-origin credentials, preserves cancellation and has a ten-second deadline.
It shares the transport and `HubError` class with the overview client. No REST
fallback or additional assistant/review store is introduced. The original
polling interval, deduplication and visible-resource selection remain in place.

## Verification

Tests compare REST/RPC envelopes and every event field, absent/zero/large limits,
trimmed resources, unauthorized callers, actual delegated carriers and absent
storage. Adversarial Store tests return foreign-resource and oversized results.
Native/proto field checks detect DTO drift and loss of optional limit presence.
Client tests cover exact request bodies, precise timestamps, malformed scope,
errors, cancellation and absent readers. The browser regression observes the
actual RPC, escapes event text and rejects a response for another selected
resource. These complete only this read-family slice of #391–#393; the live
transport/outbox work remains #371/#389/#390.

## Refresh lifecycle

A native Audit refresh rebinds the polling session to the new view when the
identity and selected visible resource remain valid. Selection, pause state,
returned rows and an in-flight read survive that remount without a duplicate
request. This state stays in memory for this mounted operator console.

Changing identity, removing the selected resource, leaving Audit, showing an
empty ledger or disposing the console stops polling and aborts pending reads.
Late completions cannot refill the cleared view. A manual pause also aborts an
in-flight read and rejects its late result. This preserves the original
resource checks; a refresh does not grant access or create durable events.

The browser regression forces a native refresh between receiving the selected
resource's events and clicking Pause. Unit tests hold the network result open
to verify cancellation and late-result rejection independently of response timing.
