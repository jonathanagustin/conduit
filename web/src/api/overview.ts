import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";

export type OverviewStatus = "CURRENT" | "UNAVAILABLE";

export interface OverviewPanel<T> {
  status: OverviewStatus;
  count: number;
  items: T[];
}

export interface OverviewProjection {
  generatedAt: string;
  staleAfterSeconds: number;
  awaitingApprovals: OverviewPanel<{ id: string; title: string; environment: string }>;
  failedPlans: OverviewPanel<{ id: string; environment: string }>;
  runningPlans: OverviewPanel<{ id: string; environment: string }>;
  unhealthyEntities: OverviewPanel<{
    environment: string;
    installationId: string;
    health: string;
  }>;
  staleReports: OverviewPanel<Record<string, never>>;
  blockedPromotions: OverviewPanel<{
    id: string;
    product: string;
    version: string;
    fromChannel: string;
    toChannel: string;
  }>;
}

export function useOverview() {
  return useQuery({
    queryKey: ["overview"],
    queryFn: ({ signal }) => getJSON<OverviewProjection>("/overview", { signal }),
    refetchInterval: 30_000,
  });
}
