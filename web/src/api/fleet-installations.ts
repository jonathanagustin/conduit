import { create, toJson } from "@bufbuild/protobuf";
import { TimestampSchema, type Timestamp } from "@bufbuild/protobuf/wkt";
import { createClient, type Transport } from "@connectrpc/connect";
import {
  FleetReadService,
  ListFleetInstallationsRequestSchema,
} from "./gen/conduit/fleet/v1/fleet_pb.js";
import { hubConnectError } from "./hub-connect.js";
import { HubError } from "./hub-error.js";
export { hubConnectTransport as fleetInstallationsTransport } from "./hub-connect.js";
const strings = [
  "environment",
  "installationId",
  "product",
  "health",
  "rolloutStatus",
  "configStatus",
  "lifecycle",
  "search",
  "cursor",
  "spaceId",
] as const;
function invalid(): never {
  throw new HubError(502, "Fleet installation page is unavailable.");
}
function stamp(
  value: Timestamp | undefined,
  required = false,
): string | undefined {
  if (!value) {
    if (required) invalid();
    return undefined;
  }
  try {
    const result = toJson(TimestampSchema, value);
    if (typeof result !== "string") invalid();
    return result;
  } catch {
    return invalid();
  }
}
function safe(value: bigint): number {
  if (value < BigInt(0) || value > BigInt(Number.MAX_SAFE_INTEGER)) invalid();
  return Number(value);
}
/** One native Fleet page. Counts stay exact, and missing observations stay absent. */
export async function fetchFleetInstallations(
  transport: Transport,
  signal: AbortSignal | undefined,
  path: string,
) {
  try {
    const url = new URL(path, "http://hub.local");
    if (url.pathname !== "/v1/fleet/installations") invalid();
    const request = create(ListFleetInstallationsRequestSchema);
    const allowed = new Set<string>([...strings, "limit", "maxAgeSeconds"]);
    for (const [key] of url.searchParams)
      if (!allowed.has(key) || url.searchParams.getAll(key).length !== 1)
        invalid();
    for (const key of strings) request[key] = url.searchParams.get(key) ?? "";
    for (const key of ["limit", "maxAgeSeconds"] as const) {
      const raw = url.searchParams.get(key);
      if (raw !== null) {
        if (!/^[1-9][0-9]*$/.test(raw)) invalid();
        const n = Number(raw);
        if (!Number.isSafeInteger(n) || n > (key === "limit" ? 200 : 86400))
          invalid();
        request[key] = n;
      }
    }
    const page = await createClient(
      FleetReadService,
      transport,
    ).listFleetInstallations(request, { signal, timeoutMs: 10000 });
    if (
      !page.summary ||
      !page.facets ||
      page.limit < 1 ||
      page.limit > 200 ||
      page.pageSize !== page.installations.length ||
      page.installations.length > page.limit ||
      page.summary.total < BigInt(page.installations.length) ||
      page.summary.stale < BigInt(0) ||
      page.summary.stale > page.summary.total ||
      page.maxResultAgeSeconds < BigInt(0)
    )
      invalid();
    for (const cursor of [page.nextCursor, page.previousCursor])
      if (new TextEncoder().encode(cursor).length > 2048) invalid();
    for (const map of [
      page.summary.health,
      page.summary.rollout,
      page.summary.config,
      page.summary.lifecycle,
      page.facets.environment,
      page.facets.product,
      page.facets.health,
      page.facets.rolloutStatus,
      page.facets.configStatus,
      page.facets.lifecycle,
    ])
      if (Object.values(map).some((n) => n < BigInt(0))) invalid();
    const generatedAt = stamp(page.generatedAt, true);
    const seen = new Set<string>();
    const installations = page.installations.map((row) => {
      const key = JSON.stringify([row.environment, row.installationId]);
      if (
        !row.environment ||
        !row.installationId ||
        seen.has(key) ||
        (request.environment && row.environment !== request.environment) ||
        (request.installationId &&
          row.installationId !== request.installationId)
      )
        invalid();
      seen.add(key);
      return {
        environment: row.environment,
        installationId: row.installationId,
        product: row.product,
        namespace: row.namespace,
        desiredVersion: row.desiredVersion,
        observedVersion: row.observedVersion,
        desiredReleaseDigest: row.desiredReleaseDigest,
        observedReleaseDigest: row.observedReleaseDigest,
        desiredConfigDigest: row.desiredConfigDigest,
        observedConfigDigest: row.observedConfigDigest,
        health: row.health,
        rolloutStatus: row.rolloutStatus,
        configStatus: row.configStatus,
        lifecycle: row.lifecycle,
        stale: row.stale,
        observedAt: stamp(row.observedAt),
        observationAgeSeconds: safe(row.observationAgeSeconds),
      };
    });
    return {
      ...page,
      generatedAt,
      maxResultAgeSeconds: safe(page.maxResultAgeSeconds),
      installations,
    };
  } catch (error) {
    if (error instanceof HubError) throw error;
    throw hubConnectError(error);
  }
}
