import { fetchPackagingRunDetail, packagingRunTransport } from "../../api/packaging-run.js";

export class HubError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "HubError";
    this.status = status;
  }
}

export type PackagingRun = {
  configured: boolean;
  tier?: string;
  phase?: string;
  outcome?: string;
  reason?: string;
  runId?: string;
  sourceDigest?: string;
  artifactDigest?: string;
  proposalRevision?: string;
  targetPlanRef?: string;
  planId?: string;
  handoffReconciled?: boolean;
  lastReview?: string;
};

export type PlanCard = {
  id: string;
  environment: string;
  status: string;
  changeRequestId?: string;
  stepCount: number;
  approvalCount: number;
  requiredApprovals: number;
  lease?: { attempt?: number; agentId?: string; expiresAt?: string };
  failure?: string;
  execution?: { attempt?: number; tasks?: { phase?: string; status?: string; failure?: string; agentOutput?: string }[] };
};

export type EnvironmentCard = {
  id: string;
  reported: boolean;
  connected: boolean;
};

export type ProductCard = { name: string };

async function readBody(response: Response): Promise<Record<string, unknown>> {
  try {
    const body = (await response.json()) as unknown;
    if (body !== null && typeof body === "object" && !Array.isArray(body)) {
      return body as Record<string, unknown>;
    }
  } catch {
    // The status is the fact when the body is not JSON.
  }
  return {};
}

async function send(path: string, init?: RequestInit): Promise<Record<string, unknown>> {
  const response = await fetch(path, {
    credentials: "same-origin",
    ...init,
    headers: { "content-type": "application/json", ...init?.headers },
  });
  const body = await readBody(response);
  if (!response.ok) {
    const message = typeof body["error"] === "string" ? body["error"] : `Hub returned ${response.status}`;
    throw new HubError(response.status, message);
  }
  return body;
}

export async function hubLive(): Promise<boolean> {
  const response = await fetch("/health/live", { credentials: "same-origin" });
  return response.ok;
}

export type StepEvent = {
  sequence: number;
  at: string;
  step: string;
  outcome: string;
  reason: string;
};

export type ControlBinding = {
  decisionId: string;
  sourceDigest: string;
  proposalRevision: string;
  targetPlanRef: string;
  templateVersion?: string;
  toolVersions?: string;
};

function runFrom(record: Record<string, unknown>): PackagingRun {
  return {
    configured: record["configured"] === true,
    ...(typeof record["tier"] === "string" ? { tier: record["tier"] } : {}),
    ...(typeof record["phase"] === "string" ? { phase: record["phase"] } : {}),
    ...(typeof record["outcome"] === "string" ? { outcome: record["outcome"] } : {}),
    ...(typeof record["reason"] === "string" ? { reason: record["reason"] } : {}),
    ...(typeof record["runId"] === "string" ? { runId: record["runId"] } : {}),
    ...(typeof record["sourceDigest"] === "string" ? { sourceDigest: record["sourceDigest"] } : {}),
    ...(typeof record["artifactDigest"] === "string" ? { artifactDigest: record["artifactDigest"] } : {}),
    ...(typeof record["proposalRevision"] === "string" ? { proposalRevision: record["proposalRevision"] } : {}),
    ...(typeof record["targetPlanRef"] === "string" ? { targetPlanRef: record["targetPlanRef"] } : {}),
    ...(typeof record["planId"] === "string" ? { planId: record["planId"] } : {}),
    ...(record["handoffReconciled"] === true ? { handoffReconciled: true } : {}),
    ...(typeof record["lastReview"] === "string" ? { lastReview: record["lastReview"] } : {}),
  };
}

export async function readPackagingRun(runId: string): Promise<PackagingRun> {
  const detail = await fetchPackagingRunDetail(runId, packagingRunTransport(window.location.origin));
  return {
    configured: detail.configured,
    tier: detail.tier,
    phase: detail.phase,
    outcome: detail.outcome,
    reason: detail.reason,
    runId: detail.runId,
    sourceDigest: detail.sourceDigest,
    artifactDigest: detail.artifactDigest,
    proposalRevision: detail.proposalRevision,
    targetPlanRef: detail.targetPlanRef,
    planId: detail.planId,
    handoffReconciled: detail.handoffReconciled,
    lastReview: detail.lastReview,
  };
}

export async function listPackagingRuns(): Promise<{ configured: boolean; items: PackagingRun[] }> {
  const body = await send("/v1/packaging-runs?limit=20");
  const items = Array.isArray(body["items"]) ? body["items"] : [];
  return {
    configured: body["configured"] === true,
    items: items.flatMap((item) => (item !== null && typeof item === "object" ? [runFrom(item as Record<string, unknown>)] : [])),
  };
}

export async function listPackagingEvents(runId: string): Promise<StepEvent[]> {
  const body = await send(`/v1/packaging-runs/${encodeURIComponent(runId)}/events?limit=20`);
  const items = Array.isArray(body["items"]) ? body["items"] : [];
  return items.flatMap((item) => {
    if (item === null || typeof item !== "object") return [];
    const record = item as Record<string, unknown>;
    return [
      {
        sequence: typeof record["sequence"] === "number" ? record["sequence"] : 0,
        at: typeof record["at"] === "string" ? record["at"] : "",
        step: typeof record["step"] === "string" ? record["step"] : "",
        outcome: typeof record["outcome"] === "string" ? record["outcome"] : "",
        reason: typeof record["reason"] === "string" ? record["reason"] : "",
      },
    ];
  });
}

export async function cancelPackagingRun(runId: string, binding: ControlBinding): Promise<void> {
  await send(`/v1/packaging-runs/${encodeURIComponent(runId)}/cancel`, {
    method: "POST",
    body: JSON.stringify(binding),
  });
}

export async function rerunPackagingRun(runId: string, binding: ControlBinding): Promise<void> {
  await send(`/v1/packaging-runs/${encodeURIComponent(runId)}/rerun`, {
    method: "POST",
    body: JSON.stringify(binding),
  });
}

function planOf(value: unknown): PlanCard | undefined {
  if (value === null || typeof value !== "object") return undefined;
  const record = value as Record<string, unknown>;
  const plan = record["plan"];
  const nested = plan !== null && typeof plan === "object" ? (plan as Record<string, unknown>) : {};
  const id = typeof record["id"] === "string" ? record["id"] : "";
  if (!id) return undefined;
  const lease = record["lease"];
  const execution = record["execution"];
  const steps = Array.isArray(nested["steps"]) ? nested["steps"].length : 0;
  return {
    id,
    environment: typeof nested["environment"] === "string" ? nested["environment"] : "",
    status: typeof record["status"] === "string" ? record["status"] : "",
    ...(typeof record["changeRequestId"] === "string" ? { changeRequestId: record["changeRequestId"] } : {}),
    stepCount: steps,
    approvalCount: Array.isArray(record["approvals"]) ? record["approvals"].length : 0,
    requiredApprovals: typeof record["requiredApprovals"] === "number" ? record["requiredApprovals"] : 0,
    ...(typeof record["failure"] === "string" ? { failure: record["failure"] } : {}),
    ...(lease !== null && typeof lease === "object"
      ? {
          lease: {
            ...(typeof (lease as Record<string, unknown>)["attempt"] === "number"
              ? { attempt: (lease as Record<string, unknown>)["attempt"] as number }
              : {}),
            ...(typeof (lease as Record<string, unknown>)["agentId"] === "string"
              ? { agentId: (lease as Record<string, unknown>)["agentId"] as string }
              : {}),
            ...(typeof (lease as Record<string, unknown>)["expiresAt"] === "string"
              ? { expiresAt: (lease as Record<string, unknown>)["expiresAt"] as string }
              : {}),
          },
        }
      : {}),
    ...(execution !== null && typeof execution === "object"
      ? {
          execution: {
            ...(typeof (execution as Record<string, unknown>)["attempt"] === "number"
              ? { attempt: (execution as Record<string, unknown>)["attempt"] as number }
              : {}),
            ...(Array.isArray((execution as Record<string, unknown>)["tasks"])
              ? {
                  tasks: ((execution as Record<string, unknown>)["tasks"] as unknown[]).flatMap((task) => {
                    if (task === null || typeof task !== "object") return [];
                    const item = task as Record<string, unknown>;
                    return [
                      {
                        ...(typeof item["phase"] === "string" ? { phase: item["phase"] } : {}),
                        ...(typeof item["status"] === "string" ? { status: item["status"] } : {}),
                        ...(typeof item["failure"] === "string" ? { failure: item["failure"] } : {}),
                        ...(typeof item["agentOutput"] === "string" ? { agentOutput: item["agentOutput"] } : {}),
                      },
                    ];
                  }),
                }
              : {}),
          },
        }
      : {}),
  };
}

export async function listPlans(): Promise<PlanCard[]> {
  const body = await send("/v1/plans?limit=20");
  const plans = Array.isArray(body["plans"]) ? body["plans"] : [];
  return plans.flatMap((plan) => {
    const card = planOf(plan);
    return card ? [card] : [];
  });
}

export async function listEnvironments(): Promise<EnvironmentCard[]> {
  const body = await send("/v1/environments?view=summary&limit=20");
  const rows = Array.isArray(body["environments"]) ? body["environments"] : [];
  return rows.flatMap((row) => {
    if (row === null || typeof row !== "object") return [];
    const record = row as Record<string, unknown>;
    const environment = record["environment"];
    if (environment === null || typeof environment !== "object") return [];
    const env = environment as Record<string, unknown>;
    const id = typeof env["id"] === "string" ? env["id"] : "";
    if (!id) return [];
    return [
      {
        id,
        reported: record["reported"] != null,
        connected: typeof env["connectionSetting"] === "string" && env["connectionSetting"].length > 0,
      },
    ];
  });
}

export async function listProducts(): Promise<ProductCard[]> {
  const body = await send("/v1/products?view=summary&limit=20");
  const rows = Array.isArray(body["products"]) ? body["products"] : [];
  return rows.flatMap((row) => {
    if (row === null || typeof row !== "object") return [];
    const name = (row as Record<string, unknown>)["product"];
    return typeof name === "string" && name.length > 0 ? [{ name }] : [];
  });
}

export async function approvePlan(id: string, rationale: string): Promise<void> {
  await send(`/v1/plans/${encodeURIComponent(id)}/approvals`, {
    method: "POST",
    body: JSON.stringify({ rationale }),
  });
}

export type ChangeRequestCard = {
  id: string;
  title: string;
  status: string;
  environment: string;
  author: string;
  requirementsSatisfied: boolean;
  reviews: { decision?: string; rationale?: string }[];
  diffBefore: string;
  diffAfter: string;
};

export type FleetRow = {
  id: string;
  environment: string;
  product: string;
  health: string;
};

function textOf(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (value === null || value === undefined) return "";
  try {
    return JSON.stringify(value);
  } catch {
    return "";
  }
}

function diffEnds(record: Record<string, unknown>): { diffBefore: string; diffAfter: string } {
  const changes = Array.isArray(record["changes"]) ? record["changes"] : [];
  for (const change of changes) {
    if (change === null || typeof change !== "object") continue;
    const diff = (change as Record<string, unknown>)["diff"];
    if (diff === null || typeof diff !== "object") continue;
    const entries = Object.values(diff as Record<string, unknown>);
    for (const entry of entries) {
      if (entry === null || typeof entry !== "object") continue;
      const item = entry as Record<string, unknown>;
      return { diffBefore: textOf(item["before"]), diffAfter: textOf(item["after"]) };
    }
  }
  return { diffBefore: "", diffAfter: "" };
}

function pairText(value: unknown): string {
  if (value === null || typeof value !== "object") return "";
  return Object.entries(value as Record<string, unknown>)
    .map(([key, item]) => `${key}=${textOf(item)}`)
    .join(", ");
}

function keyText(value: unknown): string {
  if (value === null || typeof value !== "object") return "";
  return Object.keys(value as Record<string, unknown>).join(", ");
}

function windowText(value: unknown): string {
  if (value === null || typeof value !== "object") return "";
  const record = value as Record<string, unknown>;
  const parts = ["downtime", "noDowntime"].flatMap((key) => {
    const window = record[key];
    if (window === null || typeof window !== "object") return [];
    const intervals = (window as Record<string, unknown>)["intervals"];
    const zone = (window as Record<string, unknown>)["timeZone"];
    const when = Array.isArray(intervals) ? intervals.filter((item) => typeof item === "string").join(", ") : "";
    return [`${key} ${typeof zone === "string" ? zone : ""} ${when}`.trim()];
  });
  return parts.join("; ");
}

export async function listChangeRequests(): Promise<ChangeRequestCard[]> {
  const body = await send("/v1/change-requests?limit=20");
  const rows = Array.isArray(body["changeRequests"]) ? body["changeRequests"] : [];
  return rows.flatMap((row) => {
    if (row === null || typeof row !== "object") return [];
    const record = row as Record<string, unknown>;
    const id = typeof record["id"] === "string" ? record["id"] : "";
    if (!id) return [];
    const author = record["author"];
    const reviews = Array.isArray(record["reviews"]) ? record["reviews"] : [];
    return [
      {
        id,
        title: typeof record["title"] === "string" ? record["title"] : id,
        status: typeof record["status"] === "string" ? record["status"] : "",
        environment: typeof record["environment"] === "string" ? record["environment"] : "",
        author:
          author !== null && typeof author === "object" && typeof (author as Record<string, unknown>)["username"] === "string"
            ? ((author as Record<string, unknown>)["username"] as string)
            : "",
        requirementsSatisfied: record["requirementsSatisfied"] === true,
        ...diffEnds(record),
        reviews: reviews.flatMap((review) => {
          if (review === null || typeof review !== "object") return [];
          const item = review as Record<string, unknown>;
          return [
            {
              ...(typeof item["decision"] === "string" ? { decision: item["decision"] } : {}),
              ...(typeof item["rationale"] === "string" ? { rationale: item["rationale"] } : {}),
            },
          ];
        }),
      },
    ];
  });
}

export type EnvironmentDetail = {
  id: string;
  generation: string;
  connection: string;
  reported: boolean;
  agentId: string;
  observedAt: string;
  entityCount: number;
  releaseChannel: string;
  labels: string;
  maintenance: string;
  configKeys: string;
};

export type OverviewCounts = {
  awaiting: number;
  failed: number;
  running: number;
  unhealthy: number;
};

export async function readEnvironment(id: string): Promise<EnvironmentDetail> {
  const [catalog, reported] = await Promise.all([
    send(`/v1/environments/${encodeURIComponent(id)}`),
    send(`/v1/environments/${encodeURIComponent(id)}/reported-state`).catch(() => ({}) as Record<string, unknown>),
  ]);
  const environment =
    catalog["environment"] !== null && typeof catalog["environment"] === "object"
      ? (catalog["environment"] as Record<string, unknown>)
      : catalog;
  const entities = Array.isArray(reported["entities"]) ? reported["entities"] : [];
  return {
    id: typeof environment["id"] === "string" ? environment["id"] : id,
    generation: typeof environment["generation"] === "string" ? environment["generation"] : "",
    connection: typeof environment["connectionSetting"] === "string" ? environment["connectionSetting"] : "",
    reported: Object.keys(reported).length > 0,
    agentId: typeof reported["agentId"] === "string" ? reported["agentId"] : "",
    observedAt: typeof reported["observedAt"] === "string" ? reported["observedAt"] : "",
    entityCount: entities.length,
    releaseChannel: typeof environment["defaultReleaseChannel"] === "string" ? environment["defaultReleaseChannel"] : "",
    labels: pairText(environment["labels"]),
    maintenance: windowText(environment["maintenance"]),
    configKeys: keyText(environment["config"]),
  };
}

export async function readOverview(): Promise<OverviewCounts> {
  const body = await send("/v1/overview");
  const count = (key: string) => {
    const panel = body[key];
    if (panel !== null && typeof panel === "object" && typeof (panel as Record<string, unknown>)["count"] === "number") {
      return (panel as Record<string, unknown>)["count"] as number;
    }
    return 0;
  };
  return {
    awaiting: count("awaitingApprovals"),
    failed: count("failedPlans"),
    running: count("runningPlans"),
    unhealthy: count("unhealthyEntities"),
  };
}

export async function listFleet(): Promise<FleetRow[]> {
  const body = await send("/v1/fleet/installations?limit=20");
  const rows = Array.isArray(body["installations"]) ? body["installations"] : [];
  return rows.flatMap((row) => {
    if (row === null || typeof row !== "object") return [];
    const record = row as Record<string, unknown>;
    const id = typeof record["installationId"] === "string" ? record["installationId"] : "";
    if (!id) return [];
    return [
      {
        id,
        environment: typeof record["environment"] === "string" ? record["environment"] : "",
        product: typeof record["product"] === "string" ? record["product"] : "",
        health: typeof record["health"] === "string" ? record["health"] : "UNKNOWN",
      },
    ];
  });
}

export async function rejectPlan(plan: PlanCard, rationale: string): Promise<void> {
  if (plan.changeRequestId) {
    await send(`/v1/change-requests/${encodeURIComponent(plan.changeRequestId)}/reject`, {
      method: "POST",
      body: JSON.stringify({ rationale }),
    });
    return;
  }
  await send(`/v1/plans/${encodeURIComponent(plan.id)}/cancel`, {
    method: "POST",
    body: JSON.stringify({ reason: rationale }),
  });
}

export type ProductRelease = {
  product: string;
  version: string;
  channels: string[];
};

export type PromotionEdge = {
  id: string;
  product: string;
  version: string;
  fromChannel: string;
  toChannel: string;
  status: string;
  reason: string;
};

export async function listProductReleases(): Promise<ProductRelease[]> {
  const body = await send("/v1/products?view=full&limit=20");
  const rows = Array.isArray(body["products"]) ? body["products"] : [];
  return rows.flatMap((row) => {
    if (row === null || typeof row !== "object") return [];
    const record = row as Record<string, unknown>;
    const product = typeof record["product"] === "string" ? record["product"] : "";
    const releases = Array.isArray(record["releases"]) ? record["releases"] : [];
    return releases.flatMap((release) => {
      if (release === null || typeof release !== "object") return [];
      const item = release as Record<string, unknown>;
      const version = typeof item["version"] === "string" ? item["version"] : "";
      if (!product || !version) return [];
      const channels = Array.isArray(item["channels"]) ? item["channels"].filter((channel) => typeof channel === "string") : [];
      return [{ product, version, channels: channels as string[] }];
    });
  });
}

export async function listPromotions(): Promise<PromotionEdge[]> {
  const body = await send("/v1/promotions?limit=20");
  const rows = Array.isArray(body["promotions"]) ? body["promotions"] : Array.isArray(body["items"]) ? body["items"] : [];
  return rows.flatMap((row) => {
    if (row === null || typeof row !== "object") return [];
    const record = row as Record<string, unknown>;
    const id = typeof record["id"] === "string" ? record["id"] : "";
    const fromChannel = typeof record["fromChannel"] === "string" ? record["fromChannel"] : "";
    const toChannel = typeof record["toChannel"] === "string" ? record["toChannel"] : "";
    if (!id || !fromChannel || !toChannel) return [];
    const reasons = Array.isArray(record["reasons"]) ? record["reasons"].filter((reason) => typeof reason === "string") : [];
    return [
      {
        id,
        product: typeof record["product"] === "string" ? record["product"] : "",
        version: typeof record["version"] === "string" ? record["version"] : "",
        fromChannel,
        toChannel,
        status: typeof record["status"] === "string" ? record["status"] : "",
        reason: (reasons as string[]).join("; "),
      },
    ];
  });
}
