import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";

export interface SessionIdentity {
  username: string;
  teams: string[];
  teamsTruncated: boolean;
  serviceAccount: boolean;
}

export interface SessionApplication {
  id: string;
  enabled: boolean;
}

export interface SessionResourceCapabilities {
  resourceRid: string;
  operations: string[];
}

export interface SessionProjection {
  actor: SessionIdentity;
  effective: SessionIdentity;
  delegated: boolean;
  applications: SessionApplication[];
  globalOperations: string[];
  resources: SessionResourceCapabilities[];
}

export function useSession() {
  return useQuery({
    queryKey: ["session"],
    queryFn: ({ signal }) => getJSON<SessionProjection>("/session", { signal }),
    staleTime: 30_000,
  });
}
