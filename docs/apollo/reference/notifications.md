# Apollo notifications — reconstruction spec

This is a retained evidence note for the `apollo-notifications` gap. The
[evidence boundary](../evidence.md) records this private behavior as unestablished.
Unlike the other gaps it has recovered evidence in two independent channels, so
its shape can be reconstructed further than its authorization vocabulary alone.

No public Palantir docs page was captured for this capability, so the evidence
here is recovered from the tenant, not from documentation. Every claim is marked
against the confidence vocabulary in
[`../../operations/TENANT-FIDELITY.md`](../../operations/TENANT-FIDELITY.md): Public contract, Tenant observation,
Corroboration, Conduit extension, or Unknown.

## Recovered evidence

### Authorization model — Primary

From `docs/evidence/apollo-authorization-graph.json` (object keys and values from
the live tenant). Three roles in a strict hierarchy, administrator ⊇ editor ⊇
viewer, with descriptions verbatim:

- **administrator** — "view, create and delete any notifications or notification
  subscriptions. Administrator access does not respect user and team boundaries.
  Should only be reserved for platform and security administrators."
- **editor** — "manage the user's own notification subscriptions or subscriptions
  of teams on which the user has the 'Edit' permission."
- **viewer** — "view the user's own notifications inbox and subscriptions. Can
  view subscriptions of teams on which the user is a member."

Three facts follow directly from these descriptions:

1. The model has two nouns: **notifications** (an inbox) and **notification
   subscriptions**.
2. A subscription is owned by a **user** or a **team**; editor authority is
   scoped to what the caller owns or can edit on a team.
3. The administrator role deliberately **crosses** user and team boundaries — it
   is a break-glass role, not an ordinary one.

### Data Health subscription seam — Primary (of the seam), Corroborating (of Apollo's model)

From `mcp/foundry/schema.observed.graphql` and the recovered REST path
`/data-health/api/monitoring/v2/monitoring-view/apollo-event-receivers`. This is
Foundry's Data Health product subscribing a monitoring view to an Apollo **event
receiver**, not Apollo's own receiver management:

```graphql
interface ApolloEventReceiverConfiguration { id: String!, name: String! }

addApolloEventReceiverNotificationToMonitoringView(
  eventReceiverId: String!,
  receiverName: String!,
  monitoringViewRid: RID!,
  organizationRid: RID!,
  severity: MonitoringSeverity!,
  applicationConfiguration: [ApolloEventReceiverMonitoringViewApplicationConfiguration!]!,
  disableResolutionNotifications: Boolean,
): MonitoringView!

removeApolloEventReceiverNotificationFromMonitoringView(
  eventReceiverId: String!, monitoringViewRid: RID!, severity: MonitoringSeverity!,
): MonitoringView!
```

What this establishes: an Apollo event receiver has an `id` and a `name`; a
consumer subscribes it to a target (here a Data Health monitoring view) at a
given **severity**, with an opaque per-receiver application configuration and an
option to suppress resolution notifications.

## Derived contract — reconstruction target

Combining the two channels, an Apollo notifications capability would model:

- an **event receiver**, an addressable notification sink identified by
  `id` + `name`, that other products subscribe to;
- a **subscription** binding a receiver to a target at a severity, carrying a
  receiver-specific configuration and a resolution-notification flag;
- **ownership** by user or team, with editor authority scoped to owned or
  team-editable subscriptions and a break-glass administrator that ignores those
  boundaries;
- the three-role authorization hierarchy already generated into
  `apollo_roles.gen.go` and gated.

## Deliberate bounds — Unknown and Local choice

- **The Hub's own receiver CRUD is unrecovered.** The GraphQL seam is the
  Foundry→Apollo direction — a consumer subscribing to a receiver. How a receiver
  is created, edited, or deleted inside the Hub, and how a notification is
  delivered, needs an authorized Apollo Hub session (H38/H18). This spec must not
  be read as that API.
- **`MonitoringSeverity`, `MonitoringView`, and `organizationRid` are Data Health
  types**, not Apollo's. They bound the seam, not Apollo's internal model.
- **The inbox model is named but not shaped.** The viewer description proves an
  inbox exists; its structure is not recovered.
- Implementing this remains a provider-neutral engineering decision, not a
  reconstruction the current evidence completes.
