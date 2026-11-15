import { hubConnectError } from "./hub-connect.js";
import { create } from "@bufbuild/protobuf";
import { createClient, type Transport } from "@connectrpc/connect";
import {
  GetOverviewRequestSchema,
  ReportingService,
} from "./gen/conduit/reporting/v1/overview_pb.js";
import { HubError } from "./hub-error.js";
export { HubError };

export { hubConnectTransport as overviewTransport } from "./hub-connect.js";

export async function fetchOverview(
  transport: Transport,
  signal?: AbortSignal,
) {
  try {
    return await createClient(ReportingService, transport).getOverview(
      create(GetOverviewRequestSchema),
      { signal, timeoutMs: 15000 },
    );
  } catch (error) {
    throw hubConnectError(error);
  }
}
