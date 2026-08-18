import { type FormEvent, useEffect, useState } from "react";
import {
  Button,
  Callout,
  Checkbox,
  H2,
  HTMLSelect,
  HTMLTable,
  InputGroup,
  Section,
  SectionCard,
  Switch,
  Tag,
} from "@blueprintjs/core";
import { useSearchParams } from "react-router-dom";

import {
  NOTIFICATION_CHANNELS,
  NOTIFICATION_EVENT_TYPES,
  useInbox,
  usePreference,
  useSavePreference,
  type InboxEntry,
  type NotificationChannel,
  type NotificationOwner,
  type NotificationPreference,
} from "../../api/notifications.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import "./notifications.css";

/** Notifications -- what the Hub recorded for the operator reading this, and
 * the one setting that changes what it records next.
 *
 * Two questions, in the order an operator asks them. "What was I told?" is the
 * inbox. "Why was I told that, and how do I stop / start being told it?" is the
 * preference for the resource an entry names. The second is reachable from a row
 * because that is where the resource coordinate is; it is also reachable by
 * typing a coordinate, because an operator who was told *nothing* has no row to
 * click and is exactly the person who needs to subscribe.
 *
 * Both the channel and the selected resource live in the URL. The inbox route
 * requires a channel -- `requestedChannel` refuses an absent one rather than
 * choosing for you -- so the screen has to state one, and a state the screen has
 * to state is a state a link has to be able to carry.
 *
 * There is no unread affordance and no "mark as read", and no column for one.
 * `InboxEntry.ReadAt` exists in the domain and no route writes it, so a control
 * would silently do nothing and a column would be empty for every row of every
 * inbox -- which reads as a missing feature rather than as an unused field. */
export function NotificationsPage() {
  const [params, setParams] = useSearchParams();
  const channel = readChannel(params.get("channel"));
  const resourceRid = params.get("resourceRid") ?? "";
  const inbox = useInbox(channel);

  const move = (key: "channel" | "resourceRid", value: string) =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (value === "") next.delete(key);
      else next.set(key, value);
      return next;
    });

  return (
    <div data-testid="notifications-page">
      <ObjectHeader
        icon="notifications"
        kind="Operator"
        title="Notifications"
        subtitle={describeOwner(inbox.data?.owner)}
      />

      <Section
        className="console-index"
        compact
        icon="inbox"
        title="Inbox"
        titleRenderer={H2}
        rightElement={
          <HTMLSelect
            aria-label="Inbox channel"
            value={channel}
            onChange={(event) => move("channel", event.currentTarget.value)}
          >
            {NOTIFICATION_CHANNELS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </HTMLSelect>
        }
      >
        <SectionCard padded={false}>
          <QueryState
            isPending={inbox.isPending}
            error={inbox.error}
            data={inbox.data}
            subject={`your ${channel} inbox`}
            headingLevel={2}
            onRetry={() => void inbox.refetch()}
          >
            {(page) =>
              page.entries.length === 0 ? (
                <p className="console-empty" data-testid="inbox-empty">
                  Nothing has reached your {page.channel} inbox. An entry is recorded when an event
                  matches a notification preference you own.
                </p>
              ) : (
                <>
                  <HTMLTable className="console-notifications__table" interactive striped>
                    <thead>
                      <tr>
                        <th scope="col">Occurred</th>
                        <th scope="col">Type</th>
                        <th scope="col">Resource</th>
                        <th scope="col">Summary</th>
                        <th scope="col">Actor</th>
                      </tr>
                    </thead>
                    <tbody>
                      {page.entries.map((entry) => (
                        <tr
                          key={`${entry.event.id}-${entry.preferenceId}`}
                          data-testid={`inbox-entry-${entry.event.id}`}
                          aria-selected={entry.event.resourceRid === resourceRid}
                          onClick={() => move("resourceRid", entry.event.resourceRid)}
                        >
                          <td>
                            <Timestamp value={entry.event.occurredAt} />
                          </td>
                          <td>
                            <Tag minimal round>
                              {entry.event.type}
                            </Tag>
                          </td>
                          <td className="console-notifications__rid">
                            {entry.event.resourceRid}
                            {environmentOf(entry) === undefined ? null : (
                              <>
                                {" "}
                                <Tag minimal round>
                                  {environmentOf(entry)}
                                </Tag>
                              </>
                            )}
                          </td>
                          <td>{entry.event.summary}</td>
                          {/* An empty actor is reserved for events the system
                              raised on its own, which is a fact rather than a
                              missing value: "alice recalled this" and "a scanner
                              recalled this" are different things to act on. */}
                          <td>{actorOf(entry) ?? "the system"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </HTMLTable>
                  {page.bounded ? (
                    <Callout
                      compact
                      icon="stacked-chart"
                      intent="primary"
                      data-testid="inbox-bounded"
                    >
                      This is the most recent {page.limit} entries, which is as many as one read
                      returns. There may be older ones this page does not show.
                    </Callout>
                  ) : null}
                </>
              )
            }
          </QueryState>
        </SectionCard>
      </Section>

      <PreferenceSection
        channel={channel}
        resourceRid={resourceRid}
        onResourceChange={(value) => move("resourceRid", value)}
      />
    </div>
  );
}

/** The channel the URL asks for, or the default.
 *
 * IN_APP is the default because the inbox row *is* the in-app delivery --
 * `domain.NeedsTransport` says so, and it is the one channel that needs no
 * configured transport to have anything in it. An unrecognised value falls back
 * rather than being forwarded: the route refuses a channel it does not know with
 * a 400, so passing `?channel=SLACK` through would turn a bad link into what
 * looks like a broken console. */
export function readChannel(value: string | null): NotificationChannel {
  const known = NOTIFICATION_CHANNELS.find((channel) => channel === value);
  return known ?? "IN_APP";
}

/** Whose inbox this is, as the Hub resolved it. The console never names an
 * owner on a read -- an omitted owner defaults to the caller and cannot name the
 * wrong one -- so this reports the answer rather than restating the request. */
export function describeOwner(owner: NotificationOwner | undefined): string {
  if (owner === undefined || owner.id === "") {
    return "The notifications recorded for you";
  }
  return owner.kind === "TEAM" ? `Team ${owner.id}` : owner.id;
}

function actorOf(entry: InboxEntry): string | undefined {
  const actor = entry.event.actor;
  return actor === undefined || actor === "" ? undefined : actor;
}

function environmentOf(entry: InboxEntry): string | undefined {
  const environment = entry.event.environment;
  return environment === undefined || environment === "" ? undefined : environment;
}

/** The event types a preference enables, after a checkbox is toggled.
 *
 * Kept as a pure function so the ordering rule is testable: the list is sorted,
 * because the Hub stores what it is sent and two edits that enable the same
 * types should not produce two different stored documents. */
export function toggleType(types: readonly string[], type: string, enabled: boolean): string[] {
  const next = new Set(types);
  if (enabled) next.add(type);
  else next.delete(type);
  return [...next].sort();
}

/** Whether the Hub would accept this edit.
 *
 * `domain.Preference.Validate` refuses a preference that enables no types and
 * has not opted into future ones, because it could never deliver anything. The
 * screen refuses it first and says why, rather than offering a button whose only
 * outcome is a 422. */
export function deliversSomething(types: readonly string[], allFutureTypes: boolean): boolean {
  return allFutureTypes || types.length > 0;
}

function PreferenceSection({
  channel,
  resourceRid,
  onResourceChange,
}: {
  channel: NotificationChannel;
  resourceRid: string;
  onResourceChange: (value: string) => void;
}) {
  const [draft, setDraft] = useState(resourceRid);
  useEffect(() => setDraft(resourceRid), [resourceRid]);
  const preference = usePreference(resourceRid, channel);
  const apply = (event: FormEvent) => {
    event.preventDefault();
    onResourceChange(draft.trim());
  };

  return (
    <Section
      className="console-index"
      compact
      icon="cog"
      title="Your preference for a resource"
      titleRenderer={H2}
    >
      <SectionCard padded={false}>
        <form className="console-index-controls" role="search" onSubmit={apply}>
          <InputGroup
            leftIcon="search"
            aria-label="Resource RID"
            placeholder="ri.apollo.main.environment.…"
            value={draft}
            onValueChange={setDraft}
          />
          <Button type="submit" text="Load preference" />
        </form>
        {resourceRid === "" ? (
          <p className="console-empty" data-testid="preference-unselected">
            Choose an inbox entry, or name a resource, to see what you have asked to be told about
            it on {channel}.
          </p>
        ) : (
          <QueryState
            isPending={preference.isPending}
            error={preference.error}
            data={preference.data}
            subject={`your ${channel} preference for ${resourceRid}`}
            headingLevel={2}
            onRetry={() => void preference.refetch()}
          >
            {(stored) => (
              <PreferenceEditor
                key={`${resourceRid}-${channel}`}
                channel={channel}
                resourceRid={resourceRid}
                stored={stored}
              />
            )}
          </QueryState>
        )}
      </SectionCard>
    </Section>
  );
}

/** The editor for one (resource, channel) preference.
 *
 * The draft is re-seeded whenever the stored revision moves, so what is on
 * screen after a save is what the Hub stored rather than what was typed at it.
 * The Hub mints the revision and may normalise the types, and a screen that kept
 * its own draft would go on showing an edit that was normalised away. It also
 * means a preference changed elsewhere wins over an unsaved draft here, which is
 * the right way round: the Hub is the record.
 *
 * `owner` is deliberately never sent. An omitted owner defaults to the caller,
 * so this editor cannot name somebody else's subscription; a team's preference
 * additionally requires a contact, and nothing in this repository stores one. */
function PreferenceEditor({
  channel,
  resourceRid,
  stored,
}: {
  channel: NotificationChannel;
  resourceRid: string;
  stored: NotificationPreference | null;
}) {
  // Seeded twice on purpose. The initialiser is what the first render draws, so
  // an existing subscription never flashes as an empty form; the effect below is
  // what every later revision comes through. Removing either leaves a real hole:
  // without the initialiser the form blinks, without the effect a save shows the
  // draft rather than what the Hub stored.
  const [types, setTypes] = useState<string[]>(stored?.types ?? []);
  const [allFutureTypes, setAllFutureTypes] = useState(stored?.allFutureTypes ?? false);
  const revision = stored === null ? -1 : stored.revision;
  useEffect(() => {
    setTypes(stored?.types ?? []);
    setAllFutureTypes(stored?.allFutureTypes ?? false);
    // Keyed on the stored revision rather than on `stored` itself: react-query
    // hands back a fresh object on every refetch, so depending on the object
    // would discard a draft each time the window regained focus.
  }, [revision]);
  const save = useSavePreference();
  const deliverable = deliversSomething(types, allFutureTypes);

  return (
    <div className="console-notifications__preference" data-testid="preference-editor">
      {stored === null ? (
        <Callout compact icon="info-sign" data-testid="preference-absent">
          You have no {channel} preference for this resource, so nothing about it reaches your
          inbox. Enabling a type below records one.
        </Callout>
      ) : (
        <dl className="console-notifications__meta">
          <div>
            <dt>Preference</dt>
            <dd>{stored.id}</dd>
          </div>
          <div>
            <dt>Owner</dt>
            <dd>{describeOwner(stored.owner)}</dd>
          </div>
          <div>
            <dt>Last changed</dt>
            <dd>
              <Timestamp value={stored.updatedAt} /> (revision {stored.revision})
            </dd>
          </div>
        </dl>
      )}

      {save.error !== null ? (
        <Callout
          intent="danger"
          icon="error"
          title="That preference was not saved"
          data-testid="preference-save-failed"
        >
          {save.error.message}
        </Callout>
      ) : null}

      <Switch
        checked={allFutureTypes}
        data-testid="preference-all-future-types"
        label="Opt into every notification type added for this resource"
        onChange={(event) => setAllFutureTypes(event.currentTarget.checked)}
      />
      {/* The vocabulary is open, so the master toggle is not a shortcut for
          ticking every box: it admits types this build has never heard of. The
          checkboxes are disabled while it is on because the Hub ignores Types
          when AllFutureTypes is set, and a live checkbox that changes nothing
          is worse than a dimmed one. */}
      <fieldset className="console-notifications__types">
        <legend>Notification types</legend>
        {NOTIFICATION_EVENT_TYPES.map((type) => (
          <Checkbox
            key={type}
            checked={allFutureTypes || types.includes(type)}
            disabled={allFutureTypes}
            label={type}
            onChange={(event) => setTypes(toggleType(types, type, event.currentTarget.checked))}
          />
        ))}
      </fieldset>
      {unknownTypes(types).length > 0 ? (
        <Callout compact icon="help" data-testid="preference-unknown-types">
          This preference also enables {unknownTypes(types).join(", ")}, which this console build
          has no checkbox for. It is carried through a save unchanged.
        </Callout>
      ) : null}

      {deliverable ? null : (
        <p className="console-empty" data-testid="preference-delivers-nothing">
          A preference that enables no types and has not opted into future ones could never deliver
          anything, and the Hub refuses it.
        </p>
      )}
      <div className="console-notifications__actions">
        <Button
          intent="primary"
          text="Save preference"
          disabled={!deliverable || save.isPending}
          onClick={() =>
            save.mutate({
              resourceRid,
              channel,
              types,
              allFutureTypes,
            })
          }
        />
        {save.isSuccess ? (
          <span className="console-notifications__saved" role="status" data-testid="preference-saved">
            Saved.
          </span>
        ) : null}
      </div>
    </div>
  );
}

/** The enabled types this build has no checkbox for.
 *
 * Reported rather than dropped. Apollo's vocabulary is open and the Hub's is
 * wider than any one bundle's, so a stored preference may legitimately name a
 * type added after this console was built. The draft is seeded from the stored
 * list and the checkboxes only ever touch known types, so an unknown one
 * survives a save; saying so is what stops an operator reading its absence from
 * the form as "I am no longer subscribed to that". */
export function unknownTypes(drafted: readonly string[]): string[] {
  const known = new Set<string>(NOTIFICATION_EVENT_TYPES);
  return [...new Set(drafted)].filter((type) => !known.has(type)).sort();
}
