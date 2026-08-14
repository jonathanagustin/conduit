import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";
import type { EnvironmentCatalogRecord, ReportedEntity } from "./resources.js";
import type { CatalogQuery, PageMetadata } from "./types.js";

/** Catalog and promotion transport. As elsewhere, envelopes were read off
 * http.go: paged Products and Promotions use named arrays plus page metadata,
 * a Product's Releases use a bounded named array, and Environment Commands are
 * scoped records returned as a bare array. */

export interface Artifact {
  repository: string;
  tag?: string;
  digest: string;
}

export interface VulnerabilityAssessment {
  provider?: string;
  runId?: string;
  status?: string;
  outcome?: string;
  activeVulnerabilities?: number;
  suppressedFindings?: number;
}

export interface Release {
  product: string;
  version: string;
  chart: Artifact;
  images: Artifact[];
  channels?: string[];
  labels?: Record<string, string>;
  vulnerabilityAssessment?: VulnerabilityAssessment;
  rolloutStrategy?: string;
}

export interface ProductCatalogRecord {
  product: string;
  releases: Release[];
}

export interface ProductCatalogSummary {
  product: string;
  releaseCount: number;
  latestVersion?: string;
}

export interface ProductCatalogPage extends PageMetadata {
  products: ProductCatalogSummary[];
  total: number;
}

export interface PromotionBlocker {
  kind: string;
  message: string;
  subjects?: string[];
}

export interface PromotionRecord {
  id: string;
  product: string;
  version: string;
  fromChannel: string;
  toChannel: string;
  status: string;
  blockers?: PromotionBlocker[];
  reasons?: string[];
  evaluationStartedAt?: string;
  evaluatedAt?: string;
}

export interface PromotionPage extends PageMetadata {
  promotions: PromotionRecord[];
  total: number;
}

export interface PromotionQuery extends CatalogQuery {
  product?: string;
  status?: string;
}

/** GET /v1/environments/{id}/commands returns PlanRecord values whose Command
 * is set -- http.go 1392-1398 -- not a flat command list. The command itself is
 * nested, and the Plan around it carries the status. Decoding this as a flat
 * shape produced a table of dashes with the Plan's status in the Type column. */
export interface EntityCommand {
  type: string;
  installationId: string;
  resourceKind?: string;
  resourceName?: string;
  targetVersion?: string;
  issuedBy: string;
  rationale: string;
  issuedAt: string;
}

export interface CommandPlan {
  id: string;
  status: string;
  command?: EntityCommand;
  createdAt: string;
}

export function useEnvironment(environmentId: string) {
  return useQuery({
    queryKey: ["environment", environmentId],
    queryFn: ({ signal }) =>
      getJSON<EnvironmentCatalogRecord>(`/environments/${encodeURIComponent(environmentId)}`, {
        signal,
      }),
  });
}

export function useEnvironmentCommands(environmentId: string) {
  return useQuery({
    queryKey: ["environment-commands", environmentId],
    queryFn: ({ signal }) =>
      getJSON<CommandPlan[]>(`/environments/${encodeURIComponent(environmentId)}/commands`, {
        signal,
      }),
  });
}

export function useProducts(query: CatalogQuery) {
  return useQuery({
    queryKey: ["products", query],
    queryFn: ({ signal }) =>
      getJSON<ProductCatalogPage>("/products", {
        signal,
        query: { ...query, view: "summary" },
      }),
  });
}

export function useProductReleases(product: string) {
  return useQuery({
    queryKey: ["product-releases", product],
    queryFn: ({ signal }) =>
      getJSON<{ releases: Release[] }>(`/products/${encodeURIComponent(product)}/releases`, {
        signal,
      }).then((body) => body.releases),
  });
}

export function usePromotions(query: PromotionQuery = {}) {
  return useQuery({
    queryKey: ["promotions", query],
    queryFn: ({ signal }) =>
      getJSON<PromotionPage>("/promotions", {
        signal,
        query: { ...query },
      }),
    refetchInterval: 30_000,
  });
}

export type { EnvironmentCatalogRecord, ReportedEntity };
