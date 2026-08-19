import { useState } from "react";
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
  Tab,
  Tabs,
  Tag,
} from "@blueprintjs/core";
import { Link, useParams } from "react-router-dom";

import { principalCount, useTeam, useTeams, type Team } from "../../api/teams.js";
import { ApiError } from "../../api/errors.js";
import {
  NOTIFICATION_CHANNELS,
  TEAM_CONTACT_KINDS,
  contactDeliverable,
  useDeleteTeamContact,
  useSaveTeamContact,
  useTeamContacts,
  useTeamInbox,
  type InboxEntry,
  type NotificationChannel,
  type TeamContact,
  type TeamContactKind,
} from "../../api/notifications.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./teams.css";

/** Team index and detail. GET /v1/teams returns the whole set as one bounded
 * named array -- there is no server cursor for teams -- so the search filters
 * the loaded set locally, the same "page-local filtering until a server search
 * contract exists" rule the rail's Search omission records. */
export function TeamsPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = useTeams();
  const term = (index.query.search ?? "").toLowerCase();
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Teams"
      onRetry={() => void refetch()}
    >
      {(teams) => {
        const visible = term === "" ? teams : teams.filter((team) => teamMatches(team, term));
        return (
          <div data-testid="teams-page">
            <ObjectHeader icon="people" kind="Resource collection" title="Teams" />
            <Section className="console-index" compact icon="people" title="Visible Teams" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search Teams"
                    placeholder="Search team, display name, or member"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {visible.length === 0 ? (
                  <p className="console-empty">
                    {index.query.search === undefined
                      ? "No teams are defined."
                      : `No team matches "${index.query.search}".`}
                  </p>
                ) : (
                  <HTMLTable className="console-teams__table console-index-table" interactive striped>
                    <thead>
                      <tr>
                        <th scope="col">Team</th>
                        <th scope="col">Admins</th>
                        <th scope="col">Members</th>
                      </tr>
                    </thead>
                    <tbody>
                      {visible.map((team) => (
                        <tr key={team.name}>
                          <td>
                            <Link to={`/teams/${encodeURIComponent(team.name)}`}>
                              {team.displayName === "" ? team.name : team.displayName}
                            </Link>
                            {team.displayName !== "" && team.displayName !== team.name ? (
                              <span className="console-teams__coordinate"> {team.name}</span>
                            ) : null}
                          </td>
                          <td>{principalCount(team.adminUsers, team.adminGroups)}</td>
                          <td>{principalCount(team.memberUsers, team.memberGroups)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </HTMLTable>
                )}
              </SectionCard>
            </Section>
          </div>
        );
      }}
    </QueryState>
  );
}

function teamMatches(team: Team, term: string): boolean {
  const haystacks = [
    team.name,
    team.displayName,
    ...(team.adminUsers ?? []),
    ...team.adminGroups,
    ...(team.memberUsers ?? []),
    ...team.memberGroups,
  ];
  return haystacks.some((value) => value.toLowerCase().includes(term));
}

export function TeamDetailPage() {
  const { teamName = "" } = useParams();
  const { data, error, isPending, refetch } = useTeam(teamName);
  const [tab, setTab] = useState<string>("overview");
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject={`Team ${teamName}`}
      onRetry={() => void refetch()}
    >
      {(team) => (
        <div className="console-teams" data-testid="team-detail">
          <ObjectHeader
            icon="people"
            kind="Team"
            title={team.displayName === "" ? team.name : team.displayName}
            subtitle={team.displayName !== "" && team.displayName !== team.name ? team.name : undefined}
          />
          <Tabs id="team-tabs" selectedTabId={tab} onChange={(next) => setTab(String(next))} renderActiveTabPanelOnly>
            <Tab id="overview" title="Overview" panel={<TeamOverview team={team} />} />
            {/* Contact methods live where the team lives, matching Apollo's
                Teams Contact Management tab. The panel handles the not-a-member
                refusal itself, so the tab is always offered. */}
            <Tab id="contacts" title="Contacts" panel={<TeamContactsPanel team={team.name} />} />
            {/* The team's Notifications tab -- what has reached the team inbox.
                Same member-only refusal as Contacts. */}
            <Tab id="notifications" title="Notifications" panel={<TeamNotificationsPanel team={team.name} />} />
          </Tabs>
        </div>
      )}
    </QueryState>
  );
}

function TeamOverview({ team }: { team: Team }) {
  return (
    <div className="console-teams__overview">
      {team.description !== undefined && team.description !== "" ? (
        <p className="console-teams__description">{team.description}</p>
      ) : null}
      <RoleList label="Administrators" users={team.adminUsers} groups={team.adminGroups} testId="team-admins" />
      <RoleList label="Members" users={team.memberUsers} groups={team.memberGroups} testId="team-members" />
      {team.labels !== undefined && Object.keys(team.labels).length > 0 ? (
        <section className="console-teams__labels" aria-labelledby="team-labels-title">
          <h2 id="team-labels-title">Labels</h2>
          <div>
            {Object.entries(team.labels).map(([key, value]) => (
              <Tag key={key} minimal round>
                {key}: {value}
              </Tag>
            ))}
          </div>
        </section>
      ) : null}
      <dl className="console-teams__meta">
        <div>
          <dt>Revision</dt>
          <dd>{team.revision}</dd>
        </div>
        <div>
          <dt>Created</dt>
          <dd>
            <Timestamp value={team.createdAt} />
          </dd>
        </div>
        <div>
          <dt>Updated</dt>
          <dd>
            <Timestamp value={team.updatedAt} />
          </dd>
        </div>
      </dl>
    </div>
  );
}

/** The team's contact methods -- Apollo's Teams Contact Management tab. The
 * address book is per-owner and readable only by the team's members, so a 403
 * is "you are not on this team", not a broken console, and is rendered as its
 * own explanatory state rather than through the failure path. */
function TeamContactsPanel({ team }: { team: string }) {
  const contacts = useTeamContacts(team);
  const save = useSaveTeamContact(team);
  const remove = useDeleteTeamContact(team);

  if (contacts.isPending) {
    return <p className="console-empty">Loading contact methods…</p>;
  }
  if (contacts.error !== null) {
    const forbidden = contacts.error instanceof ApiError && contacts.error.status === 403;
    return (
      <Callout
        intent={forbidden ? "primary" : "danger"}
        icon={forbidden ? "lock" : "error"}
        data-testid="team-contacts-error"
      >
        {forbidden
          ? "Only members of this team can view or manage its contact methods."
          : contacts.error.message}
      </Callout>
    );
  }

  return (
    <section className="console-teams__contacts" data-testid="team-contacts">
      {contacts.data.length === 0 ? (
        <p className="console-empty">This team has no contact methods.</p>
      ) : (
        <HTMLTable className="console-teams__contacts-table" striped>
          <thead>
            <tr>
              <th scope="col">Kind</th>
              <th scope="col">Address</th>
              <th scope="col">Future resources</th>
              <th scope="col" aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {contacts.data.map((contact) => (
              <ContactRow
                key={contact.kind}
                contact={contact}
                onDelete={() => remove.mutate(contact.kind)}
                deleting={remove.isPending && remove.variables === contact.kind}
              />
            ))}
          </tbody>
        </HTMLTable>
      )}
      <NewContactForm
        team={team}
        onSave={(edit) => save.mutate(edit)}
        saving={save.isPending}
        error={save.error instanceof Error ? save.error.message : undefined}
      />
    </section>
  );
}

function ContactRow({
  contact,
  onDelete,
  deleting,
}: {
  contact: TeamContact;
  onDelete: () => void;
  deleting: boolean;
}) {
  return (
    <tr data-testid={`team-contact-${contact.kind}`}>
      <td>
        <Tag minimal round>
          {contact.kind}
        </Tag>
        {contactDeliverable(contact.kind) ? null : (
          <Tag minimal round intent="warning" title="The Hub does not deliver to this kind of contact.">
            not delivered
          </Tag>
        )}
      </td>
      <td>{contact.address}</td>
      <td>{contact.optIntoFutureResources === true ? "Auto-subscribed" : "—"}</td>
      <td>
        <Button
          minimal
          small
          icon="trash"
          intent="danger"
          text="Remove"
          loading={deleting}
          onClick={onDelete}
          aria-label={`Remove ${contact.kind} contact`}
        />
      </td>
    </tr>
  );
}

function NewContactForm({
  team,
  onSave,
  saving,
  error,
}: {
  team: string;
  onSave: (edit: { team: string; kind: TeamContactKind; address: string; optIntoFutureResources: boolean }) => void;
  saving: boolean;
  error: string | undefined;
}) {
  const [kind, setKind] = useState<TeamContactKind>("EMAIL");
  const [address, setAddress] = useState<string>("");
  const [optIn, setOptIn] = useState<boolean>(false);
  const trimmed = address.trim();
  return (
    <form
      className="console-teams__contact-form"
      data-testid="team-contact-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (trimmed === "") {
          return;
        }
        onSave({ team, kind, address: trimmed, optIntoFutureResources: optIn });
        setAddress("");
      }}
    >
      <h2>Add a contact method</h2>
      {/* PAGERDUTY is offered because the Hub accepts it; the row it creates is
          then marked "not delivered", which is Apollo's own contradiction. */}
      <HTMLSelect
        aria-label="Contact kind"
        value={kind}
        onChange={(event) => setKind(event.currentTarget.value as TeamContactKind)}
        options={TEAM_CONTACT_KINDS.map((value) => ({ value, label: value }))}
      />
      <InputGroup
        aria-label="Contact address"
        placeholder={kind === "SLACK" ? "#channel" : "address"}
        value={address}
        onValueChange={setAddress}
      />
      <Checkbox
        label="Opt in to future team resources"
        checked={optIn}
        onChange={(event) => setOptIn(event.currentTarget.checked)}
      />
      <Button type="submit" icon="add" text="Save contact" intent="primary" loading={saving} disabled={trimmed === ""} />
      {error !== undefined ? (
        <Callout intent="danger" icon="error" compact data-testid="team-contact-save-error">
          {error}
        </Callout>
      ) : null}
    </form>
  );
}

/** What has reached the team's inbox -- Apollo's team Notifications tab. This is
 * a read: the personal Notifications screen owns per-resource subscription
 * editing, and a team's preference additionally needs a contact, so this panel
 * shows what the team was told rather than duplicating that editor. The channel
 * toggles between the two the route accepts; a 403 is the same not-a-member
 * refusal the Contacts tab renders. */
function TeamNotificationsPanel({ team }: { team: string }) {
  const [channel, setChannel] = useState<NotificationChannel>("IN_APP");
  const inbox = useTeamInbox(team, channel);

  return (
    <section className="console-teams__notifications" data-testid="team-notifications">
      <div className="console-teams__notifications-controls">
        <HTMLSelect
          aria-label="Inbox channel"
          value={channel}
          onChange={(event) => setChannel(event.currentTarget.value as NotificationChannel)}
          options={NOTIFICATION_CHANNELS.map((value) => ({ value, label: value }))}
        />
      </div>
      {inbox.isPending ? (
        <p className="console-empty">Loading the team inbox…</p>
      ) : inbox.error !== null ? (
        (() => {
          const forbidden = inbox.error instanceof ApiError && inbox.error.status === 403;
          return (
            <Callout
              intent={forbidden ? "primary" : "danger"}
              icon={forbidden ? "lock" : "error"}
              data-testid="team-notifications-error"
            >
              {forbidden
                ? "Only members of this team can read its notifications."
                : inbox.error.message}
            </Callout>
          );
        })()
      ) : inbox.data.entries.length === 0 ? (
        <p className="console-empty" data-testid="team-notifications-empty">
          Nothing has reached this team's {channel} inbox. An entry is recorded when an event matches
          a notification preference the team owns.
        </p>
      ) : (
        <>
          <HTMLTable className="console-teams__notifications-table" striped>
            <thead>
              <tr>
                <th scope="col">Occurred</th>
                <th scope="col">Type</th>
                <th scope="col">Resource</th>
                <th scope="col">Summary</th>
              </tr>
            </thead>
            <tbody>
              {inbox.data.entries.map((entry: InboxEntry) => (
                <tr key={`${entry.event.id}-${entry.preferenceId}`} data-testid={`team-inbox-entry-${entry.event.id}`}>
                  <td>
                    <Timestamp value={entry.event.occurredAt} />
                  </td>
                  <td>
                    <Tag minimal round>
                      {entry.event.type}
                    </Tag>
                  </td>
                  <td className="console-teams__notifications-rid">{entry.event.resourceRid}</td>
                  <td>{entry.event.summary}</td>
                </tr>
              ))}
            </tbody>
          </HTMLTable>
          {inbox.data.bounded ? (
            <Callout compact icon="stacked-chart" intent="primary" data-testid="team-inbox-bounded">
              This is the most recent {inbox.data.limit} entries, which is as many as one read
              returns. There may be older ones this page does not show.
            </Callout>
          ) : null}
        </>
      )}
    </section>
  );
}

function RoleList({
  label,
  users,
  groups,
  testId,
}: {
  label: string;
  users: string[] | undefined;
  groups: string[];
  testId: string;
}) {
  const titleId = `${testId}-title`;
  const userList = users ?? [];
  return (
    <section className="console-teams__role" aria-labelledby={titleId} data-testid={testId}>
      <h2 id={titleId}>{label}</h2>
      {userList.length === 0 && groups.length === 0 ? (
        <p className="console-empty">No {label.toLowerCase()} are named.</p>
      ) : (
        <ul className="console-teams__principals">
          {userList.map((user) => (
            <li key={`user:${user}`}>
              <Tag minimal round icon="person">
                {user}
              </Tag>
            </li>
          ))}
          {groups.map((group) => (
            <li key={`group:${group}`}>
              <Tag minimal round icon="people">
                {group}
              </Tag>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
