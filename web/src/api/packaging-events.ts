import { create, toJson } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { createClient, type Transport } from "@connectrpc/connect";
import {
  ListPackagingRunEventsRequestSchema,
  PackagingRunService,
  type ListPackagingRunEventsRequest,
} from "./gen/conduit/packaging/v1/packaging_run_pb.js";
import { hubConnectError } from "./hub-connect.js";
import { HubError } from "./hub-error.js";
export { hubConnectTransport as packagingEventsTransport } from "./hub-connect.js";

/** One authorized history page; cursors and counts remain exact. */
export async function fetchPackagingEvents(
  transport: Transport,
  signal: AbortSignal | undefined,
  query: Pick<ListPackagingRunEventsRequest, "runId" | "cursor">,
) {
  try {
    const page = await createClient(
      PackagingRunService,
      transport,
    ).listPackagingRunEvents(
      create(ListPackagingRunEventsRequestSchema, query),
      {
        signal,
        timeoutMs: 10000,
      },
    );
    if (
      (page.configured && page.runId !== query.runId) ||
      (!page.configured &&
        (page.runId !== "" ||
          page.items.length !== 0 ||
          page.total !== BigInt(0))) ||
      page.pageSize !== page.items.length ||
      page.total < BigInt(page.items.length) ||
      page.limit < 1 ||
      page.limit > 20 ||
      page.items.length > page.limit
    )
      throw new HubError(502, "Packaging history page is unavailable.");
    return {
      ...page,
      items: page.items.map((event) => {
        // Stage adjacency uses safe integer arithmetic. Never round evidence.
        if (
          event.sequence < BigInt(1) ||
          event.sequence > BigInt(Number.MAX_SAFE_INTEGER) ||
          !event.at
        )
          throw new HubError(502, "Packaging history evidence is unavailable.");
        const at = toJson(TimestampSchema, event.at);
        if (typeof at !== "string")
          throw new HubError(502, "Packaging history timestamp is invalid.");
        return { ...event, sequence: Number(event.sequence), at };
      }),
    };
  } catch (error) {
    if (error instanceof HubError) throw error;
    throw hubConnectError(error);
  }
}
