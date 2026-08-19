import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { deleteJSON, getJSON, putJSON } from "./client.js";
import { ApiError } from "./errors.js";

/** Notification transport.
 *
 * The four routes are a Conduit extension. The pinned Apollo OpenAPI publishes
 * no notification operation -- the word does not appear in it -- so nothing here
 * is claimed to be Apollo's wire shape, and the field names below are read off
 * the Go structs in `internal/notifications/domain` and the response envelopes
 * assembled in `internal/notifications/adapters/httpapi`, not invented to suit a
 * screen:
 *
 *   GET /v1/notifications/inbox?channel=…    -> {owner, channel, limit, entries}
 *   GET /v1/notifications/events?resourceRid=…
 *   GET /v1/notifications/preferences?resourceRid=…&channel=…  -> a Preference
 *   PUT /v1/notifications/preferences                          -> the stored one
 *
 * Two properties of that surface drive everything the console does with it.
 *
 * An inbox is per-principal and defaults to the caller: `ownerKind`/`ownerId`
 * are what make a *team* inbox reachable, and they are exactly the parameters
 * that get refused when they name somebody else. The console therefore never
 * sends them. It reads the caller's own inbox, and the response says whose it
 * was, so the screen reports the owner the Hub resolved rather than one the
 * console assumed.
 *
 * A read is bounded at application.MaximumLimit and the response reports the
 * limit that was *applied*, not the one requested, precisely so a caller can
 * tell a truncated page from the end of the inbox. The console shows that
 * distinction rather than swallowing it. */

/** Where a notification is delivered. Matched to `domain.Channel` by
 * `scripts/verify-console-contract.mjs`, which fails when the two sets drift. */
export const NOTIFICATION_CHANNELS = ["EMAIL", "IN_APP"] as const;

export type NotificationChannel = (typeof NOTIFICATION_CHANNELS)[number];

/** The event types this build knows, matched to the `domain.EventType` consts
 * by `scripts/verify-console-contract.mjs`.
 *
 * Apollo's vocabulary is open -- the documentation says the supported types
 * "will grow over time" -- so this list is what a preference editor can offer as
 * checkboxes, and nothing more. A type this build has never heard of still
 * arrives in an inbox entry and still renders, because an entry's type is shown
 * as the string the Hub sent; and `allFutureTypes` is how a subscriber opts into
 * growth without this array having to predict it. */
export const NOTIFICATION_EVENT_TYPES = [
  "BLUE_GREEN_UPGRADE_FAILED",
  "CHANGE_REQUEST_PENDING_APPROVAL",
  "INSTALLED_PRODUCT_RELEASE_VULNERABILITY_UPDATE",
  "LATEST_PRODUCT_RELEASE_VULNERABILITY_UPDATE",
  "PLAN_FAILURES",
  "PRODUCT_IN_OPEN_ENDED_RECALL",
  "PRODUCT_RECALLS",
  "PRODUCT_RELEASE_PROMOTION_FAILED",
  "RBAC_CHANGE",
  "ROLE_CHANGE",
] as const;

export interface NotificationOwner {
  kind: string;
  id: string;
}

export interface NotificationRecipient {
  owner: NotificationOwner;
  channel: string;
}

export interface NotificationEvent {
  id: string;
  type: string;
  resourceRid: string;
  environment?: string;
  summary: string;
  actor?: string;
  occurredAt: string;
}

export interface InboxEntry {
  recipient: NotificationRecipient;
  event: NotificationEvent;
  preferenceId: string;
  readAt?: string;
}

/** The inbox envelope. `entries` is optional and nullable because Go encodes an
 * empty slice as `null`: an inbox with nothing in it answers
 * `{"entries":null}`, and a screen that assumed an array would throw on the one
 * state every new operator sees first. */
export interface InboxResponse {
  owner: NotificationOwner;
  channel: string;
  limit: number;
  entries?: InboxEntry[] | null;
}

export interface InboxPage {
  owner: NotificationOwner;
  channel: string;
  /** The limit the Hub applied, which is not necessarily the one asked for. */
  limit: number;
  entries: InboxEntry[];
  /** The page filled the limit, so there may be more the Hub did not return.
   * Reported rather than inferred by the screen, because "100 entries" and "the
   * first 100 of an unknown number" are different facts to act on. */
  bounded: boolean;
}

export interface NotificationPreference {
  id: string;
  owner: NotificationOwner;
  resourceRid: string;
  channel: string;
  types?: string[];
  allFutureTypes?: boolean;
  contactId?: string;
  createdAt: string;
  updatedAt: string;
  revision: number;
}

/** What a caller may set, as `preferenceRequest` in httpapi.go declares it. The
 * identifier, the timestamps and the revision are absent because the
 * application layer mints them, and the decoder sets DisallowUnknownFields, so
 * sending one back would fail the whole write with a 400.
 *
 * `owner` is absent for the same reason the console never sends `ownerId` on a
 * read: an omitted owner defaults to the caller and cannot name the wrong one.
 * Editing a team's preference needs a contact this repository has no store for
 * (`domain.Contact` is modelled and nothing persists one), so the console edits
 * the caller's own subscription and says so. */
export interface PreferenceEdit {
  resourceRid: string;
  channel: NotificationChannel;
  types?: string[];
  allFutureTypes?: boolean;
}

export function inboxPage(response: InboxResponse): InboxPage {
  const entries = response.entries ?? [];
  return {
    owner: response.owner,
    channel: response.channel,
    limit: response.limit,
    entries,
    bounded: response.limit > 0 && entries.length >= response.limit,
  };
}

/** The caller's own inbox on one channel.
 *
 * The channel is required by the route -- `requestedChannel` refuses an absent
 * one rather than picking a default -- so it is part of the query key and part
 * of the URL the screen owns. */
export function useInbox(channel: NotificationChannel) {
  return useQuery({
    queryKey: ["notifications", "inbox", channel],
    queryFn: ({ signal }) =>
      getJSON<InboxResponse>("/notifications/inbox", { signal, query: { channel } }).then(inboxPage),
  });
}

/** A team's inbox on one channel.
 *
 * Unlike `useInbox`, this names the owner: `ownerKind`/`ownerId` are what make a
 * team inbox reachable, and are exactly the parameters the route refuses when
 * they name a team the caller is not on. A 403 is therefore "you are not a
 * member of this team", which the panel renders as its own state rather than as
 * a failure. The channel is required by the route, so it is part of the key. */
export function useTeamInbox(team: string, channel: NotificationChannel) {
  return useQuery({
    queryKey: ["notifications", "team-inbox", team, channel],
    enabled: team !== "",
    queryFn: ({ signal }) =>
      getJSON<InboxResponse>("/notifications/inbox", {
        signal,
        query: { ownerKind: "TEAM", ownerId: team, channel },
      }).then(inboxPage),
  });
}

/** One preference, or `null` when none is stored.
 *
 * The Hub answers 404 for `ErrNoPreference`, and "you are not subscribed to
 * this" is a state, not a failure: rendering it through the failure path would
 * tell an operator their console is broken when the honest answer is that they
 * have never subscribed. Every other status still throws, so a 403 still reads
 * as a refusal and a 500 still reads as a 500. */
export function usePreference(resourceRid: string, channel: NotificationChannel) {
  return useQuery({
    queryKey: ["notifications", "preference", resourceRid, channel],
    enabled: resourceRid !== "",
    queryFn: ({ signal }) =>
      getJSON<NotificationPreference>("/notifications/preferences", {
        signal,
        query: { resourceRid, channel },
      }).catch((error: unknown) => {
        if (error instanceof ApiError && error.status === 404) {
          return null;
        }
        throw error;
      }),
  });
}

/** Save a preference and re-read it, rather than patching the cache with what
 * was sent. The Hub mints the revision and the timestamps and may normalise the
 * types, so the screen has to show what was stored, not what was requested. */
export function useSavePreference() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (edit: PreferenceEdit) =>
      putJSON<NotificationPreference>("/notifications/preferences", edit),
    onSuccess: async (stored) => {
      await queries.invalidateQueries({
        queryKey: ["notifications", "preference", stored.resourceRid, stored.channel],
      });
    },
  });
}

/** A destination a team owns, matched to `domain.ContactKind`
 * (internal/notifications/domain/notifications.go) by
 * `scripts/verify-console-contract.mjs`. All three are creatable through the New
 * Team Contact dialog; PAGERDUTY is the odd one out at delivery time, which
 * `contactDeliverable` records rather than the console hiding a kind the Hub
 * accepts. */
export const TEAM_CONTACT_KINDS = ["EMAIL", "SLACK", "PAGERDUTY"] as const;

export type TeamContactKind = (typeof TEAM_CONTACT_KINDS)[number];

/** Whether the Hub delivers to a contact of this kind. A PAGERDUTY contact can
 * be created and receives nothing -- this mirrors `ContactKind.Deliverable`
 * (notifications.go), where the New Team Contact dialog offers PagerDuty while
 * the team-notifications page says it is not delivered to. Surfacing the
 * contradiction is the honest thing; resolving it by hiding the kind would
 * diverge from the Hub, which still stores it. */
export function contactDeliverable(kind: string): boolean {
  return kind === "EMAIL" || kind === "SLACK";
}

/** A stored team contact, mirroring `domain.Contact`'s json tags. One exists per
 * (owner, kind): saving a second of the same kind edits the first rather than
 * adding a destination. */
export interface TeamContact {
  id: string;
  owner: NotificationOwner;
  kind: string;
  address: string;
  optIntoFutureResources?: boolean;
  createdAt: string;
  updatedAt?: string;
}

/** The read envelope: `{owner, contacts}` from readContacts (httpapi.go). Go
 * encodes an empty slice as null, so `contacts` is nullable exactly like the
 * inbox's `entries`. */
interface TeamContactsResponse {
  owner: NotificationOwner;
  contacts?: TeamContact[] | null;
}

/** What a caller may set, as `contactRequest` (httpapi.go) declares it: the
 * identifier and the timestamps are minted by the application layer, and the
 * decoder sets DisallowUnknownFields, so sending one back fails the write. */
export interface TeamContactEdit {
  team: string;
  kind: TeamContactKind;
  address: string;
  optIntoFutureResources: boolean;
}

/** A team's contact methods, or an empty list when it has none.
 *
 * The route refuses an owner the caller does not belong to, so a 403 here is
 * "you are not a member of this team" rather than a broken console -- the panel
 * renders that as its own state. Any other status still throws. A team with no
 * contacts answers `{"contacts":null}`, which normalises to an empty array so
 * the first, most common state does not read as a failure. */
export function useTeamContacts(team: string) {
  return useQuery({
    queryKey: ["notifications", "contacts", team],
    enabled: team !== "",
    queryFn: ({ signal }) =>
      getJSON<TeamContactsResponse>("/notifications/contacts", {
        signal,
        query: { ownerKind: "TEAM", ownerId: team },
      }).then((response) => response.contacts ?? []),
  });
}

/** Create or replace one of a team's contacts, then re-read the list. The Hub
 * mints the identifier and timestamps and enforces one-per-(owner, kind), so the
 * panel shows what was stored rather than what was sent. */
export function useSaveTeamContact(team: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (edit: TeamContactEdit) =>
      putJSON<TeamContact>("/notifications/contacts", {
        owner: { kind: "TEAM", id: edit.team },
        kind: edit.kind,
        address: edit.address,
        optIntoFutureResources: edit.optIntoFutureResources,
      }),
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["notifications", "contacts", team] });
    },
  });
}

/** Remove one of a team's contacts by kind, then re-read the list. */
export function useDeleteTeamContact(team: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (kind: string) =>
      deleteJSON<{ deleted: boolean }>("/notifications/contacts", {
        query: { ownerKind: "TEAM", ownerId: team, kind },
      }),
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["notifications", "contacts", team] });
    },
  });
}
