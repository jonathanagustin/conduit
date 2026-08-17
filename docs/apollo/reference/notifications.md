# Apollo notifications — recovered contract

This note describes Apollo's notification model as far as public evidence
establishes it, and marks the rest. Every claim carries a tier from the
confidence vocabulary in
[`../../operations/TENANT-FIDELITY.md`](../../operations/TENANT-FIDELITY.md):
Public contract, Tenant observation, Corroboration, Conduit extension, or
Unknown.

The capture is [`docs/evidence/apollo-notification-types.json`](../../evidence/apollo-notification-types.json).

> **This note used to open by saying no public documentation page had been
> captured for this capability.** That was false against this repository's own
> [`../public-documentation-manifest.tsv`](../public-documentation-manifest.tsv),
> which has listed three `managing-notifications` pages with their content
> digests the whole time, and it was never checked against it. A notification
> domain was invented on the strength of that sentence and displaced the
> following day. The claim is now gated by
> `scripts/verify-documentation-claims.mjs`.

## Recovered evidence

### Notification event types — Public contract

Apollo's own term is **notification event type**, from the team-contact
dialog: "Select the notification event types you'd like to receive across all
resources."

**The vocabulary is open**, and this is the load-bearing property. The overview
page says the listed examples are "non-comprehensive and the supported types
will grow over time", and the resource settings page carries a per-channel
toggle whose tooltip reads "If enabled, you will opt into future notification
types added for this resource". Apollo handles growth with **forward consent**:
a subscriber declares in advance whether types added later reach them. A closed
set cannot express that, and choosing one converts every future type into
something nobody is subscribed to.

Six types are legible on a Product's settings page:

| Type | Description as published |
| --- | --- |
| Latest Product Release Vulnerability Update | New vulnerabilities discovered on the latest Product Release |
| Installed Product Release Vulnerability Update | — |
| Product Release Promotion Failed | — |
| Product In Open Ended Recall | Future releases of this Product will be recalled |
| Blue Green Upgrade Failed | — |
| Role Change | — |

Three more are legible in the team-contact dialog's cross-resource list:

| Type | Description as published |
| --- | --- |
| Product recalls | Anytime a bug or critical issue has been discovered in my product |
| RBAC Change | A team's roles were created or updated on this resource |
| Change Request Pending Approval | A change request has been created and is pending approval from this team |

Two further rows in that list are blanked in the published image, so the set is
known to be larger than what is readable — a second reason not to model it as
closed.

The prose on the overview page names types more loosely (`Change requests`,
`Change request summary`, `Product recalls`, `RBAC changes`,
`Promotion failures`, `Plan failures`) and **neither list is a superset of the
other**: the prose omits both vulnerability types and Blue Green Upgrade Failed,
and adds a daily digest and Plan failures that no screenshot shows. Neither can
be treated as the vocabulary on its own.

### Delivery — Public contract

A resource's settings page configures **two channels independently over the same
type list**, each with its own master toggle: **Email** and **In-app**.

A team's destination is a **contact**, created in Hub Settings rather than on a
resource, in one of three kinds:

| Kind | Description as published |
| --- | --- |
| Email | Notifications will be forwarded to users on the specified email |
| Slack | Notifications will be delivered to the specified channel |
| Pagerduty | Receive notifications set-up for a Jira team… |

Email destinations additionally require the domain to be on the Control Panel
email allowlist, or nothing is delivered.

### Subscription shape — Public contract

- The unit is a preference per **(resource, principal, channel)**. Principals are
  a **user** or a **team contact**, which is the For User / For Team split on
  every resource's settings page.
- **Opt into future types** is per resource and channel. While enabled the
  individual checkboxes render checked and disabled.
- **Opt into future resources** is per team contact: "If you're the contact team
  on these 12 products, you'll auto-subscribe to all of these and future
  resources added to this team."
- A team that is the **contact team** on a Product or Environment is notified
  automatically; teams subscribe manually only to resources they do not own.
- **A team with no contact receives nothing**, however many resources name it.
- **There is no severity dimension.** Filtering is by type and channel.

### Authorization model — Tenant observation

From [`../../evidence/apollo-authorization-graph.json`](../../evidence/apollo-authorization-graph.json)
(object keys and values from the live tenant). Three roles in a strict
hierarchy, administrator ⊇ editor ⊇ viewer, with descriptions verbatim:

- **administrator** — "view, create and delete any notifications or notification
  subscriptions. Administrator access does not respect user and team boundaries.
  Should only be reserved for platform and security administrators."
- **editor** — "manage the user's own notification subscriptions or subscriptions
  of teams on which the user has the 'Edit' permission."
- **viewer** — "view the user's own notifications inbox and subscriptions. Can
  view subscriptions of teams on which the user is a member."

These corroborate the user/team split seen in the UI, name an **inbox**, and
show that administrator is a deliberate break-glass role that crosses ownership
boundaries.

### Data Health subscription seam — Corroboration

From `mcp/foundry/schema.observed.graphql` and the recovered REST path
`/data-health/api/monitoring/v2/monitoring-view/apollo-event-receivers`. This is
Foundry's Data Health subscribing a monitoring view to an Apollo **event
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
```

`MonitoringSeverity`, `MonitoringView`, and `organizationRid` are **Data Health
types, not Apollo's** — they bound the seam, not Apollo's model. The severity on
this call is Foundry's own, which is why it does not contradict the absence of a
severity dimension in Apollo's notification settings.

`disableResolutionNotifications` is corroborated from an unrelated direction:
the 2020 Events capture shows Monitor events in **Triggered** and **Resolved**
states.

### Event severity is a different surface — Tenant observation, historical

[`../../ui/evidence/legacy-apollo-2020/environment-events.webp`](../../ui/evidence/legacy-apollo-2020/environment-events.webp),
committed in this repository, shows an Environment's **Events** tab grouped by
Category, Severity, and Installation, with facet counts:

- **Severity**: Success 52, Info 28, Warning 18, Error 3
- **Category**: Plan 48, Monitor 46, Adjudication 7

This is a 2020 capture whose source article carries an editor note saying the
product has since changed, and the counts are one tenant's three-hour window, so
these are values observed rather than a complete vocabulary. It is recorded here
because severity in Apollo lives **here**, on Events, and not on notification
subscriptions — a distinction an earlier implementation got wrong in both
directions at once.

## Upstream ambiguities

Recorded, not resolved by naming one side correct:

- The team-notifications page says "While you can add a PagerDuty contact for a
  team, PagerDuty is not supported for notifications", while the New Team Contact
  dialog offers Pagerduty as one of three kinds with a description of what it
  does. Conduit models a PagerDuty contact as creatable and undeliverable, which
  is what both statements together say.
- That Pagerduty option's description reads "Receive notifications set-up for a
  **Jira** team…".
- The Product-scoped and cross-resource lists may name the same events at two
  granularities — `Product In Open Ended Recall` against `Product recalls`,
  `Role Change` against `RBAC Change`. Nothing captured says whether these are
  one type or two, so Conduit carries them separately: a merge is reversible, an
  invented identity is not.

## Still Unknown

- **The Hub's notification and subscription API wire format.** Everything above
  is read off the UI, so it establishes vocabulary and settings shape, not
  request bodies or routes. This note must not be read as that API.
- **The inbox read model.** Hub Settings has a "My notifications" page and the
  viewer role names an inbox, but no capture shows an entry or its read state.
  Conduit's `InboxEntry` is registered in
  [`../../evidence/conduit-inventions.json`](../../evidence/conduit-inventions.json).
- **Fan-out semantics** when one recipient is reached by two preferences.
  Registered likewise.
- **Environment- and Release-Channel-scoped type lists.** Only a Product's was
  published.
- **Webhooks**, a separate Hub Settings section that may or may not be a
  notification delivery path.
