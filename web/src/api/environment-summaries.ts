import { create, toJson } from "@bufbuild/protobuf";
import { TimestampSchema, type Timestamp } from "@bufbuild/protobuf/wkt";
import { createClient, type Transport } from "@connectrpc/connect";
import {
  EnvironmentReadService,
  ListEnvironmentSummariesRequestSchema,
} from "./gen/conduit/environments/v1/environment_summary_pb.js";
import { hubConnectError } from "./hub-connect.js";
import { HubError } from "./hub-error.js";
export { hubConnectTransport as environmentSummariesTransport } from "./hub-connect.js";

function invalid(): never {
  throw new HubError(502, "Environment summary page is unavailable.");
}
function stamp(value: Timestamp | undefined): string | undefined {
  if (!value) return undefined;
  try {
    const result = toJson(TimestampSchema, value);
    if (typeof result !== "string") invalid();
    return result;
  } catch {
    return invalid();
  }
}
function count(value: bigint): number {
  if (value < BigInt(0) || value > BigInt(Number.MAX_SAFE_INTEGER)) invalid();
  return Number(value);
}

/** Native summary only. Full records and linked detail remain separate reads. */
export async function fetchEnvironmentSummaries(
  transport: Transport,
  signal: AbortSignal | undefined,
  path: string,
) {
  try {
    signal?.throwIfAborted();
    const url = new URL(path, "http://hub.local");
    if (url.pathname !== "/v1/environments") invalid();
    const allowed = new Set(["view", "limit", "search", "cursor", "spaceId"]);
    for (const [key] of url.searchParams)
      if (!allowed.has(key) || url.searchParams.getAll(key).length !== 1)
        invalid();
    if (url.searchParams.get("view") !== "summary") invalid();
    const request = create(ListEnvironmentSummariesRequestSchema, {
      search: url.searchParams.get("search") ?? "",
      cursor: url.searchParams.get("cursor") ?? "",
      spaceId: url.searchParams.get("spaceId") ?? "",
    });
    const limit = url.searchParams.get("limit");
    if (limit !== null) {
      if (!/^[1-9][0-9]*$/.test(limit)) invalid();
      request.limit = Number(limit);
      if (!Number.isSafeInteger(request.limit) || request.limit > 200)
        invalid();
    }
    const page = await createClient(
      EnvironmentReadService,
      transport,
    ).listEnvironmentSummaries(request, { signal, timeoutMs: 10000 });
    if (
      page.limit !== (request.limit ?? 100) ||
      page.pageSize !== page.environments.length ||
      page.pageSize > page.limit ||
      page.total < BigInt(page.environments.length)
    )
      invalid();
    for (const cursor of [page.nextCursor, page.previousCursor])
      if (new TextEncoder().encode(cursor).length > 2048) invalid();
    const seen = new Set<string>();
    const environments = page.environments.map((row) => {
      if (!row.environment?.id || !row.spoke?.state) invalid();
      if (seen.has(row.environment.id)) invalid();
      seen.add(row.environment.id);
      if (row.reported && row.reported.environment !== row.environment.id)
        invalid();
      return {
        environment: {
          id: row.environment.id,
          generation: row.environment.generation,
        },
        spoke: {
          agentId: row.spoke.agentId,
          state: row.spoke.state,
          observedAt: stamp(row.spoke.observedAt),
        },
        ...(row.reported
          ? {
              reported: {
                environment: row.reported.environment,
                agentId: row.reported.agentId,
                entityCount: count(row.reported.entityCount),
                observedAt: stamp(row.reported.observedAt),
              },
            }
          : {}),
      };
    });
    return {
      environments,
      total: page.total,
      pageSize: page.pageSize,
      limit: page.limit,
      nextCursor: page.nextCursor,
      previousCursor: page.previousCursor,
      hasPrevious: page.hasPrevious,
    };
  } catch (error) {
    if (error instanceof HubError) throw error;
    throw hubConnectError(error);
  }
}
