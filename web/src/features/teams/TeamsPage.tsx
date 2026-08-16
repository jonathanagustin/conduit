import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";
import { Link, useParams } from "react-router-dom";

import { principalCount, useTeam, useTeams, type Team } from "../../api/teams.js";
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
      )}
    </QueryState>
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
