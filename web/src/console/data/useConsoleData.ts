"use client";

import { useCallback, useEffect, useState } from "react";

import {
  approvePlan,
  hubLive,
  listChangeRequests,
  listEnvironments,
  listFleet,
  listPackagingEvents,
  listPackagingRuns,
  listPlans,
  listProductReleases,
  listProducts,
  listPromotions,
  cancelPackagingRun,
  rerunPackagingRun,
  readEnvironment,
  readOverview,
  readPackagingRun,
  rejectPlan,
  type ChangeRequestCard,
  type EnvironmentCard,
  type EnvironmentDetail,
  type FleetRow,
  type OverviewCounts,
  type PackagingRun,
  type StepEvent,
  type PlanCard,
  type ProductCard,
  type ProductRelease,
  type PromotionEdge,
} from "./hub";

export function useConsoleData(runId: string) {
  const [live, setLive] = useState<boolean | null>(null);
  const [error, setError] = useState("");
  const [environments, setEnvironments] = useState<EnvironmentCard[]>([]);
  const [products, setProducts] = useState<ProductCard[]>([]);
  const [plans, setPlans] = useState<PlanCard[]>([]);
  const [requests, setRequests] = useState<ChangeRequestCard[]>([]);
  const [fleet, setFleet] = useState<FleetRow[]>([]);
  const [releases, setReleases] = useState<ProductRelease[]>([]);
  const [promotions, setPromotions] = useState<PromotionEdge[]>([]);
  const [overview, setOverview] = useState<OverviewCounts | null>(null);
  const [environmentDetail, setEnvironmentDetail] =
    useState<EnvironmentDetail | null>(null);
  const [run, setRun] = useState<PackagingRun | null>(null);
  const [runs, setRuns] = useState<PackagingRun[]>([]);
  const [events, setEvents] = useState<StepEvent[]>([]);
  const [runError, setRunError] = useState("");

  const refresh = useCallback(async () => {
    const [
      health,
      catalog,
      profileRows,
      planRows,
      requestRows,
      fleetRows,
      overviewRow,
      releaseRows,
      promotionRows,
    ] = await Promise.allSettled([
      hubLive(),
      listEnvironments(),
      listProducts(),
      listPlans(),
      listChangeRequests(),
      listFleet(),
      readOverview(),
      listProductReleases(),
      listPromotions(),
    ]);
    setLive(health.status === "fulfilled" ? health.value : false);
    if (catalog.status === "fulfilled") {
      setEnvironments(catalog.value);
      setError("");
    } else {
      setEnvironments([]);
      setError(
        catalog.reason instanceof Error
          ? catalog.reason.message
          : "Hub is not answering",
      );
    }
    setProducts(profileRows.status === "fulfilled" ? profileRows.value : []);
    setPlans(planRows.status === "fulfilled" ? planRows.value : []);
    setRequests(requestRows.status === "fulfilled" ? requestRows.value : []);
    setFleet(fleetRows.status === "fulfilled" ? fleetRows.value : []);
    setOverview(overviewRow.status === "fulfilled" ? overviewRow.value : null);
    setReleases(releaseRows.status === "fulfilled" ? releaseRows.value : []);
    setPromotions(
      promotionRows.status === "fulfilled" ? promotionRows.value : [],
    );
    const index = await listPackagingRuns().catch(() => ({
      configured: false,
      items: [] as PackagingRun[],
    }));
    setRuns(index.items);
  }, []);

  const loadEnvironment = useCallback(async (id: string) => {
    try {
      setEnvironmentDetail(await readEnvironment(id));
    } catch {
      setEnvironmentDetail(null);
    }
  }, []);

  const loadRun = useCallback(async (id: string) => {
    setRunError("");
    try {
      setRun(await readPackagingRun(id));
      setEvents(await listPackagingEvents(id).catch(() => []));
    } catch (reason) {
      setRun(null);
      setRunError(
        reason instanceof Error
          ? reason.message
          : "Packaging run is unavailable",
      );
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (runId.trim().length === 0) return;
    void loadRun(runId);
  }, [runId, loadRun]);

  const decide = useCallback(
    async (
      plan: PlanCard,
      verdict: "approve" | "reject",
      rationale: string,
    ) => {
      if (verdict === "approve") await approvePlan(plan.id, rationale);
      else await rejectPlan(plan, rationale);
      await refresh();
    },
    [refresh],
  );

  return {
    live,
    error,
    environments,
    products,
    plans,
    requests,
    fleet,
    releases,
    promotions,
    overview,
    environmentDetail,
    run,
    runs,
    events,
    runError,
    refresh,
    loadRun,
    cancelRun: cancelPackagingRun,
    rerunRun: rerunPackagingRun,
    loadEnvironment,
    decide,
  };
}
