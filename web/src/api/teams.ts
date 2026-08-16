import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";

/** Team transport. Read off http.go: GET /v1/teams returns a named array
 * ({"teams": [...]}), and GET /v1/teams/{teamName} returns a bare Team. The
 * field names are the governance.Team Go struct's -- adminGroups and
 * memberGroups are always present, the user lists and labels are omitempty. */

export interface Team {
  name: string;
  displayName: string;
  description?: string;
  adminUsers?: string[];
  adminGroups: string[];
  memberUsers?: string[];
  memberGroups: string[];
  labels?: Record<string, string>;
  createdAt: string;
  updatedAt: string;
  revision: number;
}

export function useTeams() {
  return useQuery({
    queryKey: ["teams"],
    queryFn: ({ signal }) => getJSON<{ teams: Team[] }>("/teams", { signal }).then((body) => body.teams),
  });
}

export function useTeam(teamName: string) {
  return useQuery({
    queryKey: ["team", teamName],
    queryFn: ({ signal }) => getJSON<Team>(`/teams/${encodeURIComponent(teamName)}`, { signal }),
  });
}

/** The number of distinct principals a team names in a role, users and groups
 * counted together, so the index can show reach without listing everyone. */
export function principalCount(users: string[] | undefined, groups: string[]): number {
  return (users?.length ?? 0) + groups.length;
}
