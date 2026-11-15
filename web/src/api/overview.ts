import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Transport,
} from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import {
  GetOverviewRequestSchema,
  ReportingService,
} from "./gen/conduit/reporting/v1/overview_pb.js";
import { HubError } from "./hub-error.js";
export { HubError };

export function overviewTransport(
  baseUrl: string,
  fetchImpl: typeof fetch = fetch,
): Transport {
  return createConnectTransport({
    baseUrl,
    useBinaryFormat: false,
    fetch: (input, init) =>
      fetchImpl(new Request(input, { ...init, credentials: "same-origin" })),
  });
}

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
    const failure = ConnectError.from(error);
    const status: Partial<Record<Code, number>> = {
      [Code.Canceled]: 499,
      [Code.Unauthenticated]: 401,
      [Code.PermissionDenied]: 403,
      [Code.NotFound]: 404,
      [Code.InvalidArgument]: 422,
      [Code.OutOfRange]: 422,
      [Code.FailedPrecondition]: 409,
      [Code.AlreadyExists]: 409,
      [Code.Aborted]: 409,
      [Code.ResourceExhausted]: 429,
      [Code.DeadlineExceeded]: 504,
      [Code.Unavailable]: 503,
      [Code.Unimplemented]: 501,
      [Code.Internal]: 500,
      [Code.DataLoss]: 500,
    };
    throw new HubError(
      status[failure.code] ?? 502,
      failure.rawMessage || "Overview unavailable",
    );
  }
}
