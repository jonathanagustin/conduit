import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getJSON, postJSON } from "./client.js";
import type { BundleImport } from "./imports.js";
import type { CatalogQuery, PageMetadata } from "./types.js";

/** A Hub list field that is null when empty, read as the empty list.
 *
 * Go marshals a nil slice as `null`, and several control-plane structs tag
 * their slices without `omitempty` -- so the key is always present and its
 * value is sometimes null. This is not defensive coding against a hypothetical:
 * a real Hub crashed the Change Requests screen with "Cannot read properties of
 * null (reading 'filter')" the first time a Change Request carried no reviewer
 * requirement, because every fixture in the test suite used `[]`.
 *
 * Deliberately narrow: it takes the Hub's own shape rather than `unknown`, so
 * it cannot be used to paper over a field whose type is actually wrong. */
export function listOf<T>(value: T[] | null | undefined): T[] {
  return value ?? [];
}

/** Transport types and hooks for the resources beyond the fleet.
 *
 * Envelopes are not uniform and are not guessed: list endpoints wrap their
 * results in a named key ({"plans": []}, {"environments": []},
 * {"changeRequests": []}) while detail endpoints return the record bare. Each
 * one below was read off internal/controlplane/http.go. */

export interface Environment {
  id: string;
  generation?: string;
  connectionSetting?: string;
  defaultReleaseChannel?: string;
  // Which Hub owns this Environment's settings: EDIT_ON_THIS_HUB (authoritative),
  // EDIT_ON_ANOTHER_HUB (imported, break-glass only), COPY_OF_ANOTHER_HUB
  // (read-only mirror). See internal/environments/domain/federation.go.
  declaredStateSource?: string;
  config?: Record<string, unknown>;
}

export interface ReportedEntity {
  installationId: string;
  product: string;
  version: string;
  namespace?: string;
  health?: string;
  rolloutStatus?: string;
  configStatus?: string;
  lifecycle?: string;
  observedAt?: string;
}

export interface EnvironmentReport {
  environment: string;
  agentId?: string;
  observedAt?: string;
  entities?: ReportedEntity[];
}

export interface EnvironmentCatalogRecord {
  environment: Environment;
  reported?: EnvironmentReport;
}

export interface EnvironmentCatalogSummary {
  environment: Environment;
  reported?: {
    environment: string;
    agentId?: string;
    observedAt?: string;
    entityCount: number;
  };
}

export interface EnvironmentCatalogPage extends PageMetadata {
  environments: EnvironmentCatalogSummary[];
  total: number;
}

export interface PlanStep {
  position: number;
  action: string;
  installationId: string;
  entity: string;
  product: string;
  version: string;
  releaseChannel?: string;
  namespace: string;
}

export interface PlanExecutionTask {
  sequence: number;
  phase: string;
  status: string;
  installationId?: string;
  namespace?: string;
  failure?: string;
  agentOutput?: string;
  resources?: ResourceDiagnostic[];
  startedAt?: string;
  finishedAt?: string;
}

/** Bounded, redacted resource evidence projected verbatim by the Hub. */
export interface ResourceDiagnostic {
  apiVersion?: string;
  kind: string;
  namespace?: string;
  name: string;
  reason?: string;
  message?: string;
}

export interface PlanExecution {
  startedAt?: string;
  finishedAt?: string;
  tasks?: PlanExecutionTask[];
}

export interface Approval {
  subject: string;
  rationale?: string;
  createdAt?: string;
}

export interface PlanRecord {
  id: string;
  desiredStateDigest: string;
  changeRequestId?: string;
  plan: { environment: string; generation: string; steps: PlanStep[] };
  status: string;
  requiredApprovals: number;
  approvals?: Approval[];
  failure?: string;
  failedInstallationId?: string;
  execution?: PlanExecution;
  cancellation?: { subject: string; reason: string; createdAt?: string };
  createdAt: string;
  updatedAt: string;
  revision: number;
}

export interface PlanSummary {
  id: string;
  environment: string;
  status: string;
  stepCount: number;
  approvalCount: number;
  requiredApprovals: number;
  createdAt: string;
  updatedAt: string;
  revision: number;
}

export interface PlanSummaryPage extends PageMetadata {
  plans: PlanSummary[];
  total: number;
}

export interface ChangeDiffEntry {
  before?: unknown;
  after?: unknown;
}

export interface EntityChange {
  id: string;
  type: string;
  installationId: string;
  diff?: Record<string, ChangeDiffEntry>;
}

export interface PolicyEvaluation {
  changeId: string;
  policyId: string;
  outcome: string;
  reason?: string;
}

export interface ReviewerRequirement {
  id: string;
  team?: string;
  role?: string;
  operation?: string;
  reason?: string;
  satisfiedBy?: string[];
}

export interface ChangeReview {
  subject: string;
  decision: string;
  rationale?: string;
  createdAt?: string;
}

export interface ChangeRequest {
  id: string;
  planId: string;
  targetKind?: string;
  targetId?: string;
  environment: string;
  title: string;
  description?: string;
  author: { username: string };
  status: string;
  requirementsSatisfied: boolean;
  /** Null, not absent, when the Hub has none.
   *
   * governance.go tags these three without `omitempty`, so a nil Go slice is
   * serialized as `null` rather than omitted or emptied -- the key is always
   * present and its value is sometimes null. Declaring them as plain arrays was
   * a claim about the Hub that the Hub does not honour, and it crashed the
   * Change Requests screen outright on the first request whose policy raised no
   * requirement. Read them through `listOf`. */
  changes: EntityChange[] | null;
  evaluations: PolicyEvaluation[] | null;
  requirements: ReviewerRequirement[] | null;
  reviews?: ChangeReview[];
  error?: string;
  createdAt: string;
  updatedAt: string;
  revision: number;
}

export interface ChangeRequestPage extends PageMetadata {
  changeRequests: ChangeRequest[];
  total: number;
}

export function useEnvironments(query: CatalogQuery) {
  return useQuery({
    queryKey: ["environments", query],
    queryFn: ({ signal }) =>
      getJSON<EnvironmentCatalogPage>("/environments", {
        signal,
        query: { ...query, view: "summary" },
      }),
  });
}

export function usePlans(query: CatalogQuery = {}, environment?: string) {
  return useQuery({
    queryKey: ["plans", environment ?? null, query],
    queryFn: ({ signal }) =>
      getJSON<PlanSummaryPage>("/plans", {
        signal,
        query: { ...query, ...(environment === undefined ? {} : { environment }), view: "summary" },
      }),
    refetchInterval: 30_000,
  });
}

/** The Plan statuses nothing moves a Plan out of, read off the Hub's lifecycle.
 *
 * Every transition in internal/controlplane/lifecycle.go -- ApprovePlan,
 * LeaseNextPlan, StartPlan, FinishPlan, CancelPlan, supersedePendingPlans,
 * recordNonExecutingState -- guards on a status in {AWAITING_APPROVAL,
 * APPROVED, LEASED, RUNNING}, so a Plan that has reached SUCCEEDED, FAILED or
 * CANCELLED stays there. RECORDED belongs here too: it is a reviewed generation
 * for an Environment this Hub does not drive, and lifecycle.go says outright
 * that it "cannot be approved or leased".
 *
 * Stated as the terminal set rather than as the active one deliberately. The
 * first version of the poll below named the two statuses to keep polling
 * (RUNNING, LEASED), which left a Plan detail page opened at APPROVED frozen
 * for the whole rollout -- the Agent drives APPROVED -> LEASED -> RUNNING ->
 * SUCCEEDED/FAILED with no help from the console, and AWAITING_APPROVAL moves
 * on its own as soon as another reviewer approves. Naming the states that stop
 * the polling instead means every intermediate state the Hub grows in future is
 * polled by construction, and going quiet is a decision someone has to make
 * here rather than an omission. */
export const PLAN_TERMINAL_STATUSES: ReadonlySet<string> = new Set([
  "SUCCEEDED",
  "FAILED",
  "CANCELLED",
  "RECORDED",
]);

/** How often to refetch a Plan in `status`, or false to stop polling.
 *
 * `undefined` is the case where no record has loaded -- the first fetch, or one
 * that failed. React Query already retries that on its own schedule, and a Plan
 * id the Hub refuses outright would otherwise re-ask every five seconds
 * forever, so an absent status stops the poll rather than starting it. */
export function planPollInterval(status: string | undefined): number | false {
  if (status === undefined) return false;
  return PLAN_TERMINAL_STATUSES.has(status) ? false : 5_000;
}

export function usePlan(planId: string) {
  return useQuery({
    // A Change Request can govern no Plan, and asking for /plans/ with an empty
    // id is a request the Hub will refuse -- noise in its logs that the screen
    // then hides behind a callout.
    enabled: planId !== "",
    queryKey: ["plan", planId],
    queryFn: ({ signal }) => getJSON<PlanRecord>(`/plans/${encodeURIComponent(planId)}`, { signal }),
    // A Plan the Agent can still move changes on its own; a terminal one does
    // not. See planPollInterval for why the rule is stated that way round.
    refetchInterval: (query) => planPollInterval(query.state.data?.status),
  });
}

export function useChangeRequests(query: CatalogQuery = {}) {
  return useQuery({
    queryKey: ["change-requests", query],
    queryFn: ({ signal }) =>
      getJSON<ChangeRequestPage>("/change-requests", { signal, query: { ...query } }),
    refetchInterval: 30_000,
  });
}

export function useChangeRequest(id: string) {
  return useQuery({
    queryKey: ["change-request", id],
    queryFn: ({ signal }) =>
      getJSON<ChangeRequest>(`/change-requests/${encodeURIComponent(id)}`, { signal }),
  });
}

export type ReviewAction = "approve" | "reject" | "cancel" | "reopen";

/** The body each review endpoint actually accepts.
 *
 * Reopen returns nothing rather than `{}`: requireEmptyBody in http.go reads
 * bytes, not JSON fields, and rejects the two-byte document an empty object
 * serialises to. */
export function reviewBody(action: ReviewAction, rationale: string): Record<string, string> | undefined {
  switch (action) {
    case "approve":
    case "reject":
      return { rationale };
    case "cancel":
      return { reason: rationale };
    case "reopen":
      return undefined;
  }
}

/** What the four review endpoints actually return. The Hub wraps the updated
 * ChangeRequest in an envelope and adds the Plan it produced, and a
 * BundleImport when an import was admitted -- it never returns a bare
 * ChangeRequest (http.go /approve, /reject, /reopen, /cancel). The console
 * discards this and refetches, so the shape was never read; typing it as a bare
 * ChangeRequest was a latent lie that would have handed `undefined` to the
 * first caller that consumed the result. */
export interface ChangeReviewResult {
  changeRequest: ChangeRequest;
  plan?: PlanRecord;
  bundleImport?: BundleImport;
}

/** The query keys a review makes stale, given the reviewed Change Request's id.
 *
 * `["plan"]` is a prefix rather than `["plan", planId]` on purpose: this hook
 * knows the Change Request's id, and that is not the Plan's. A review both
 * moves the governed Plan (approve leaves it APPROVED, cancel leaves it
 * CANCELLED) and can produce a new one, and the ids of neither are in hand
 * here, so the prefix invalidates whichever Plan detail the operator is looking
 * at. Without it the Plans index refreshed and the Plan detail page did not,
 * and the two screens disagreed about the Plan just acted on. */
export function reviewInvalidationKeys(id: string): string[][] {
  return [["change-request", id], ["change-requests"], ["plans"], ["plan"]];
}

/** Reviewing is the console's only write path so far, and it is deliberately
 * explicit: the caller names the action, and the affected queries are
 * invalidated rather than patched, so what the screen shows afterwards is what
 * the Hub says rather than what the console assumed. */
export function useReviewChangeRequest(id: string) {
  const queries = useQueryClient();
  return useMutation({
    // Each endpoint takes a different body and the Hub sets DisallowUnknownFields,
    // so sending a union of the fields fails all four with 400. approve and
    // reject take {rationale}; cancel takes {reason}; reopen requires a body
    // with no bytes -- requireEmptyBody, not an empty JSON object.
    mutationFn: ({ action, rationale }: { action: ReviewAction; rationale: string }) =>
      postJSON<ChangeReviewResult>(
        `/change-requests/${encodeURIComponent(id)}/${action}`,
        reviewBody(action, rationale),
      ),
    onSuccess: async () => {
      await Promise.all(
        reviewInvalidationKeys(id).map((queryKey) => queries.invalidateQueries({ queryKey })),
      );
    },
  });
}
