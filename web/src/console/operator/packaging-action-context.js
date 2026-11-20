// Resolve one action target independently of the visible, bounded Hub indexes.
// These reads are context only; they never replace index rows or their totals.
import {
  fetchFleetInstallations,
  fleetInstallationsTransport,
} from "../../api/fleet-installations.js";
import { activeSpaceId, spaceScopedPath } from "./space-scope.js";

const pending = new WeakMap();
const bindingKeys = [
  "runId",
  "targetPlanRef",
  "sourceDigest",
  "proposalRevision",
  "templateVersion",
  "toolVersions",
  "phase",
  "outcome",
];
const binding = (run) =>
  JSON.stringify(bindingKeys.map((key) => run?.[key] || ""));
const scope = (state) =>
  JSON.stringify([
    state.screen,
    state.detailScreen,
    state.packagingRunId,
    state.assistantMode,
    state.spaceId,
    state.operatorName,
    state.reviewOperatorActor,
    state.assistantRemoved || [],
    state.assistantAdded || [],
  ]);

async function read(path) {
  const response = await fetch(path, {
    method: "GET",
    headers: { accept: "application/json" },
    credentials: "same-origin",
    signal: AbortSignal.timeout(10_000),
  });
  if (!response.ok)
    throw new Error(
      "The selected packaging context is unavailable. Refresh the run before proposing an action.",
    );
  return response.json();
}

export async function resolvePackagingActionContext(state, runID) {
  const hub = state.hub;
  const selected = hub?.packaging?.find((run) => run.runId === runID);
  const original = binding(selected);
  const originalScope = scope(state);
  const token = {};
  pending.set(state, token);
  const current = () =>
    pending.get(state) === token &&
    state.hub === hub &&
    scope(state) === originalScope &&
    binding(state.hub?.packaging?.find((run) => run.runId === runID)) ===
      original;
  const changed = () =>
    new Error(
      "The selected packaging context changed. Refresh the run before proposing an action.",
    );
  if (!selected) throw changed();
  const run = await read(`/v1/packaging-runs/${encodeURIComponent(runID)}`);
  if (
    !current() ||
    run.configured !== true ||
    run.runId !== runID ||
    binding(run) !== original
  )
    throw changed();
  const target = String(run.targetPlanRef || "");
  const split = target.indexOf("/");
  if (split < 1 || split === target.length - 1) throw changed();
  const environment = target.slice(0, split);
  const installation = target.slice(split + 1);
  const query = new URLSearchParams({
    environment,
    installationId: installation,
    limit: "1",
  });
  const page = await fetchFleetInstallations(
    fleetInstallationsTransport(window.location.origin),
    AbortSignal.timeout(10_000),
    spaceScopedPath(
      `/v1/fleet/installations?${query}`,
      activeSpaceId(state.spaceId),
    ),
  );
  if (!current()) throw changed();
  const rows = page.installations;
  const row = Array.isArray(rows) && rows.length === 1 ? rows[0] : null;
  if (
    !row ||
    row.environment !== environment ||
    row.installationId !== installation ||
    typeof row.product !== "string" ||
    !row.product.trim() ||
    typeof row.desiredVersion !== "string" ||
    !row.desiredVersion.trim() ||
    page.nextCursor ||
    page.hasPrevious ||
    page.summary?.total !== BigInt(1) ||
    page.pageSize !== 1 ||
    page.limit !== 1
  )
    throw new Error(
      "The exact packaging installation is unavailable. Refresh the run before proposing an action.",
    );
  return {
    environment,
    installation,
    product: row.product,
    isCurrent: current,
  };
}
