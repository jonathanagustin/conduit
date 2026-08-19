/** Transport types mirroring the Hub's JSON. These are the console's inbound
 * adapter: they follow the Go structs in internal/controlplane, field for field,
 * and are deliberately not shared with anything else. */

export interface FleetInstallation {
  environment: string;
  installationId: string;
  product: string;
  namespace: string;
  desiredVersion?: string;
  observedVersion?: string;
  desiredReleaseDigest?: string;
  observedReleaseDigest?: string;
  desiredConfigDigest?: string;
  observedConfigDigest?: string;
  health: string;
  rolloutStatus: string;
  configStatus: string;
  lifecycle: string;
  observedAt?: string;
  observationAgeSeconds?: number;
  stale: boolean;
}

export interface FleetSummary {
  total: number;
  health: Record<string, number>;
  rollout: Record<string, number>;
  config: Record<string, number>;
  lifecycle: Record<string, number>;
  stale: number;
}

export interface PageMetadata {
  pageSize: number;
  limit: number;
  nextCursor?: string;
  previousCursor?: string;
  hasPrevious: boolean;
}

/** Counts per value of each filterable dimension, each computed with its OWN
 * filter excluded -- so `health` still reports every health bucket after
 * `health=UNHEALTHY` has been applied, while `environment` narrows with it.
 *
 * The keys are the FleetQuery field names below, and that is the Hub's doing
 * (reporting.FleetFacets in internal/reporting/domain/fleet.go): a facet is
 * served under the name of the parameter that selects it, so a rail cannot
 * offer a filter the Hub would not accept. This differs from `summary`, which
 * describes the result and moves with every filter.
 * docs/ui/contracts/fleet-facets.md states which is which and why. */
export interface FleetFacets {
  environment: Record<string, number>;
  product: Record<string, number>;
  health: Record<string, number>;
  rolloutStatus: Record<string, number>;
  configStatus: Record<string, number>;
  lifecycle: Record<string, number>;
}

export interface FleetProjection extends PageMetadata {
  generatedAt: string;
  maxResultAgeSeconds: number;
  installations: FleetInstallation[];
  summary: FleetSummary;
  facets: FleetFacets;
}

export interface CatalogQuery {
  search?: string;
  cursor?: string;
  limit?: number;
}

export interface FleetQuery {
  environment?: string;
  product?: string;
  health?: string;
  rolloutStatus?: string;
  configStatus?: string;
  lifecycle?: string;
  maxAgeSeconds?: number;
  search?: string;
  cursor?: string;
  limit?: number;
}

/** Status vocabularies, read off internal/controlplane/lifecycle.go 163-187.
 *
 * These are filter options, so a wrong list does not merely look untidy: a
 * status the Hub emits and this list omits cannot be filtered for, and one this
 * list invents can be selected and will never match. An earlier version had both
 * faults -- it omitted IN_GRACE_PERIOD, BLOCKED and RECALLED, and offered
 * RELINQUISHED and PENDING, which the Hub does not emit. */
export const HEALTH = [
  "HEALTHY",
  "UNHEALTHY",
  "INDETERMINATE",
  "UNKNOWN",
  "REPAIRING",
  "WARNING",
  "ERROR",
  "TERMINAL",
] as const;
export const ROLLOUT = [
  "UP_TO_DATE",
  "IN_GRACE_PERIOD",
  "BLOCKED",
  "RECALLED",
  "OUT_OF_DATE",
] as const;
export const CONFIG = ["APPLIED", "DRIFTED", "UNAVAILABLE", "INDETERMINATE"] as const;
export const LIFECYCLE = ["INSTALLED", "UNINSTALLED", "UNKNOWN"] as const;
