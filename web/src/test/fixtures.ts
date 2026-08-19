import type { FleetFacets, FleetInstallation, FleetProjection } from "../api/types.js";

/** Deterministic API bodies for the browser tests.
 *
 * These are shaped by the Go structs in internal/controlplane, not invented: the
 * field names and the status vocabularies are the ones the Hub emits. Where a
 * test needs a specific condition -- a stale report, a drifted config -- it is
 * built by changing one field of a known-good installation, so the difference
 * under test is the only difference present. */

export function installation(overrides: Partial<FleetInstallation> = {}): FleetInstallation {
  return {
    environment: "prod-use1",
    installationId: "payments",
    product: "app.example:payments",
    namespace: "payments",
    desiredVersion: "2.4.1",
    observedVersion: "2.4.1",
    health: "HEALTHY",
    rolloutStatus: "UP_TO_DATE",
    configStatus: "APPLIED",
    lifecycle: "INSTALLED",
    observedAt: "2026-08-07T12:00:00Z",
    observationAgeSeconds: 30,
    stale: false,
    ...overrides,
  };
}

/** Summaries are counted from the installations rather than written by hand, so
 * a fixture cannot claim a total its rows contradict. */
export function projection(
  installations: FleetInstallation[],
  pagination: Partial<Pick<FleetProjection, "nextCursor" | "previousCursor" | "hasPrevious" | "limit">> & {
    total?: number;
    /** Facet counts that differ from the rows, which the Hub's near-miss walk
     * produces whenever a filter is applied: a dimension excludes its own
     * filter, so it counts installations the response does not carry. Nothing
     * derivable from `installations` can express that, and a fixture that
     * always derived it could never exercise the case the rail exists for. */
    facets?: Partial<FleetFacets>;
  } = {},
): FleetProjection {
  const count = (pick: (item: FleetInstallation) => string) => {
    const tally: Record<string, number> = {};
    for (const item of installations) {
      const key = pick(item);
      tally[key] = (tally[key] ?? 0) + 1;
    }
    return tally;
  };
  return {
    generatedAt: "2026-08-07T12:00:30Z",
    maxResultAgeSeconds: Math.max(0, ...installations.map((item) => item.observationAgeSeconds ?? 0)),
    installations,
    pageSize: installations.length,
    limit: pagination.limit ?? 100,
    hasPrevious: pagination.hasPrevious ?? false,
    ...(pagination.nextCursor === undefined ? {} : { nextCursor: pagination.nextCursor }),
    ...(pagination.previousCursor === undefined
      ? {}
      : { previousCursor: pagination.previousCursor }),
    summary: {
      total: pagination.total ?? installations.length,
      health: count((item) => item.health),
      rollout: count((item) => item.rolloutStatus),
      config: count((item) => item.configStatus),
      lifecycle: count((item) => item.lifecycle),
      stale: installations.filter((item) => item.stale).length,
    },
    facets: {
      environment: count((item) => item.environment),
      product: count((item) => item.product),
      health: count((item) => item.health),
      rolloutStatus: count((item) => item.rolloutStatus),
      configStatus: count((item) => item.configStatus),
      lifecycle: count((item) => item.lifecycle),
      ...pagination.facets,
    },
  };
}

/** A fleet with one of everything worth showing: healthy, unhealthy, drifted,
 * out of date, and one that has stopped reporting. */
export const MIXED_FLEET: FleetInstallation[] = [
  installation(),
  installation({
    environment: "prod-use1",
    installationId: "search",
    product: "app.example:search",
    desiredVersion: "8.1.0",
    observedVersion: "8.1.0",
    configStatus: "DRIFTED",
  }),
  installation({
    environment: "prod-euw1",
    installationId: "ledger",
    product: "app.example:ledger",
    desiredVersion: "1.9.0",
    observedVersion: "1.8.2",
    health: "UNHEALTHY",
    rolloutStatus: "OUT_OF_DATE",
  }),
  installation({
    environment: "dev-use2",
    installationId: "reporting",
    product: "app.example:reporting",
    desiredVersion: "0.4.0",
    observedVersion: "0.3.0",
    health: "UNKNOWN",
    rolloutStatus: "OUT_OF_DATE",
    observationAgeSeconds: 7200,
    stale: true,
  }),
];
