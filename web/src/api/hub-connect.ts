import { Code, ConnectError, type Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { HubError } from "./hub-error.js";
export function hubConnectTransport(
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

export function hubConnectError(
  error: unknown,
  invalidArgumentStatus = 422,
): HubError {
  const failure = ConnectError.from(error);
  const status: Partial<Record<Code, number>> = {
    [Code.Canceled]: 499,
    [Code.Unauthenticated]: 401,
    [Code.PermissionDenied]: 403,
    [Code.NotFound]: 404,
    [Code.InvalidArgument]: invalidArgumentStatus,
    [Code.OutOfRange]: invalidArgumentStatus,
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
  return new HubError(
    status[failure.code] ?? 502,
    failure.rawMessage || "Overview unavailable",
  );
}
