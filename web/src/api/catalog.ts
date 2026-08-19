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

/** One active CVE behind the assessment's count. Fields are
 * catalog.VulnerabilityFinding's Go `json` tags.
 *
 * `cvss` is authoritative and `severity` is the fallback a scanner supplies
 * when it published no numeric score -- the Hub's own `findingScore` prefers
 * `cvss` whenever it is above zero. Ordering or colouring by `severity` would
 * therefore rank findings differently from how the Hub scores the same Release,
 * and `severity` is an unconstrained string with no Go const set behind it, so
 * `verify-console-contract.mjs` could not guard a map keyed on it. Rank by
 * `cvss`; render `severity` as the provider's own text. */
export interface VulnerabilityFinding {
  id: string;
  cvss?: number;
  severity?: string;
}

/** The provenance that makes a suppressed finding trustworthy. Fields are
 * catalog.VulnerabilitySuppressionProvenance's Go `json` tags. A suppressed
 * count on its own is not evidence: the Hub refuses to let a suppression turn a
 * vulnerability outcome into PASS unless it carries an approver, a Change
 * Request, and an unexpired window, so those are the fields worth showing. */
export interface VulnerabilitySuppressionProvenance {
  providerId?: string;
  scope?: string;
  scopeValue?: string;
  category?: string;
  rationale?: string;
  requestedBy?: string;
  approvedBy?: string;
  changeRequestId?: string;
  createdAt?: string;
  approvedAt?: string;
  validUntil?: string;
}

export interface SuppressedVulnerabilityFinding {
  findingId: string;
  vulnerabilityId: string;
  artifactDigest: string;
  suppression?: VulnerabilitySuppressionProvenance;
}

export interface VulnerabilityAssessment {
  provider?: string;
  runId?: string;
  status?: string;
  outcome?: string;
  artifactDigests?: string[];
  activeVulnerabilities?: number;
  suppressedFindings?: number;
  suppressedFindingDetails?: SuppressedVulnerabilityFinding[];
  gracePeriodFindings?: number;
  activeFindings?: VulnerabilityFinding[];
  assessedAt?: string;
}

/** The Hub's own tiers for a finding that reported no CVSS, mirrored from
 * `findingScore` in internal/controlplane/risk.go. The unknown tier is
 * deliberately equal to HIGH there -- "a finding whose severity nobody could
 * parse must not make a target look cleaner than it is" -- and that intent is
 * lost if the console quietly treats unparsed as harmless. */
const CVSS_MAXIMUM = 10;
const CVSS_CRITICAL = 9;
const CVSS_HIGH = 7;
const CVSS_MEDIUM = 4;
const CVSS_LOW = 0.1;
const CVSS_UNKNOWN_TIER = CVSS_HIGH;

/** What the Hub scores this finding at, mirroring `findingScore`
 * (internal/controlplane/risk.go). CVSS wins whenever it is above zero;
 * otherwise the severity string is mapped to a representative score, including
 * the MODERATE / INFORMATIONAL / INFO aliases that exist because providers
 * disagree.
 *
 * An earlier revision of this file ordered on CVSS alone and sent every
 * severity-only finding to the bottom, which inverted the Hub for exactly the
 * case that matters: a CRITICAL finding with no CVSS scores 9.0 at the Hub and
 * rendered below a finding scoring 0.5. The comment claiming Hub-consistent
 * order was true of half the function. Mirroring the whole of `findingScore` is
 * the only way the claim holds.
 *
 * This duplicates five numbers across the language boundary, which is a real
 * cost. It is accepted because the alternative -- ordering differently from the
 * Hub while saying otherwise -- is worse, and because these are numeric tiers
 * rather than a status vocabulary, so `verify-console-contract.mjs` (which
 * extracts value SETS from named Go consts) is not the mechanism that would
 * guard them. `logic.test.ts` pins each tier instead. */
export function findingScore(finding: VulnerabilityFinding): number {
  if (finding.cvss !== undefined && finding.cvss > 0) {
    return Math.min(finding.cvss, CVSS_MAXIMUM);
  }
  switch ((finding.severity ?? "").trim().toUpperCase()) {
    case "CRITICAL":
      return CVSS_CRITICAL;
    case "HIGH":
      return CVSS_HIGH;
    case "MEDIUM":
    case "MODERATE":
      return CVSS_MEDIUM;
    case "LOW":
      return CVSS_LOW;
    case "NONE":
    case "INFORMATIONAL":
    case "INFO":
      return 0;
    default:
      return CVSS_UNKNOWN_TIER;
  }
}

/** Findings worst-first by the score the Hub would give them, so the panel's
 * order is the order `ReleaseRiskScore` is built from. Ties keep CVE id order
 * so the list is stable between renders. */
export function orderFindingsByHubScore(findings: VulnerabilityFinding[]): VulnerabilityFinding[] {
  return [...findings].sort((left, right) => {
    const difference = findingScore(right) - findingScore(left);
    return difference !== 0 ? difference : left.id.localeCompare(right.id);
  });
}

/** True when a timestamp is Go's zero `time.Time`. `omitempty` does not omit a
 * struct, so an unset `assessedAt` or `validUntil` arrives as year 1 rather
 * than as absent, and an undefined-check alone renders it as a date ~740,000
 * days ago. */
export function isZeroTime(value: string | undefined): boolean {
  return value === undefined || value.startsWith("0001-01-01T00:00:00");
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

/** The operator-authored display metadata for an Environment: a human name, a
 * description, and a Markdown README. Mirrors internal/environments/domain
 * Profile. Most Environments have none, so the endpoint answers 404 and the
 * query is not retried -- an absent profile is a normal state, not a failure. */
export interface EnvironmentProfile {
  environmentId: string;
  displayName?: string;
  description?: string;
  readme?: string;
  ownerTeam?: string;
}

export function useEnvironmentProfile(environmentId: string) {
  return useQuery({
    queryKey: ["environment-profile", environmentId],
    queryFn: ({ signal }) =>
      getJSON<EnvironmentProfile>(`/environments/${encodeURIComponent(environmentId)}/profile`, {
        signal,
      }),
    // An Environment with no profile answers 404; that is not worth retrying and
    // not worth surfacing as an error. The detail page renders nothing then.
    retry: false,
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
