import { create } from "@bufbuild/protobuf";
import { createClient, type Transport } from "@connectrpc/connect";
import {
  ListPackagingRunsRequestSchema,
  PackagingRunService,
  type ListPackagingRunsRequest,
} from "./gen/conduit/packaging/v1/packaging_run_pb.js";
import { hubConnectError } from "./hub-connect.js";
import { HubError } from "./hub-error.js";
export { hubConnectTransport as packagingRunsTransport } from "./hub-connect.js";

type Filters = Partial<
  Pick<
    ListPackagingRunsRequest,
    "view" | "limit" | "cursor" | "phase" | "outcome" | "planId" | "search"
  >
>;

/** One native page. Callers retain the opaque cursors and exact int64 total. */
export async function fetchPackagingRuns(
  transport: Transport,
  signal?: AbortSignal,
  filters: Filters = {},
) {
  try {
    const page = await createClient(
      PackagingRunService,
      transport,
    ).listPackagingRuns(create(ListPackagingRunsRequestSchema, filters), {
      signal,
      timeoutMs: 10000,
    });
    const maximum = filters.view === "summary" ? 200 : 20;
    if (
      page.pageSize !== page.items.length ||
      page.total < BigInt(page.items.length) ||
      page.limit < 1 ||
      page.limit > maximum ||
      page.items.length > page.limit ||
      page.items.some((run) => !run.runId)
    )
      throw new HubError(502, "Packaging run index is unavailable.");
    return page;
  } catch (error) {
    if (error instanceof HubError) throw error;
    throw hubConnectError(error);
  }
}
