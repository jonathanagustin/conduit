import { create } from "@bufbuild/protobuf";
import {
  Code,
  ConnectError,
  createClient,
  type Transport,
} from "@connectrpc/connect";

export type { Transport };
import { createConnectTransport } from "@connectrpc/connect-web";
import { useEffect, useState } from "react";

import {
  GetPackagingRunRequestSchema,
  PackagingRunService,
  type GetPackagingRunResponse,
} from "./gen/conduit/packaging/v1/packaging_run_pb.js";

// Packaging-run detail is the one console read on Connect.
// Other console calls stay in client.ts. Replacing fetchPackagingRunDetail
// puts this read back on REST without touching the view.

export class PackagingRunError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "PackagingRunError";
    this.status = status;
  }
}

export type PackagingRunDetail = {
  tier: string;
  configured: boolean;
  authority: string;
  planAuthority: string;
  approvalAuthority: string;
  runId: string;
  phase: string;
  outcome: string;
  reason: string;
  sourceDigest: string;
  artifactDigest: string;
  proposalRevision: string;
  targetPlanRef: string;
  planId: string;
  handoffReconciled: boolean;
  lastReview: string;
  duplicateReviews: number;
  templateVersion: string;
  toolVersions: string;
};

export type PackagingRunState =
  | { status: "loading" }
  | { status: "ready"; detail: PackagingRunDetail }
  | { status: "error"; error: PackagingRunError };

export function packagingRunTransport(
  baseUrl: string,
  fetchImpl?: typeof fetch,
): Transport {
  return createConnectTransport({
    baseUrl,
    useBinaryFormat: false,
    fetch: fetchImpl,
  });
}

export async function fetchPackagingRunDetail(
  runId: string,
  transport: Transport,
  signal?: AbortSignal,
): Promise<PackagingRunDetail> {
  const client = createClient(PackagingRunService, transport);
  try {
    const response = await client.getPackagingRun(
      create(GetPackagingRunRequestSchema, { runId }),
      {
        signal,
      },
    );
    return toDetail(response);
  } catch (error) {
    throw asPackagingRunError(error);
  }
}

export function usePackagingRunDetail(
  runId: string,
  transport?: Transport,
): PackagingRunState {
  const [state, setState] = useState<PackagingRunState>({ status: "loading" });
  useEffect(() => {
    const active = transport ?? packagingRunTransport(window.location.origin);
    const controller = new AbortController();
    let cancelled = false;
    fetchPackagingRunDetail(runId, active, controller.signal).then(
      (detail) => {
        if (!cancelled) setState({ status: "ready", detail });
      },
      (error: unknown) => {
        if (cancelled) return;
        setState({ status: "error", error: asPackagingRunError(error) });
      },
    );
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [runId, transport]);
  return state;
}

function toDetail(response: GetPackagingRunResponse): PackagingRunDetail {
  return {
    tier: response.tier,
    configured: response.configured,
    authority: response.authority,
    planAuthority: response.planAuthority,
    approvalAuthority: response.approvalAuthority,
    runId: response.runId,
    phase: response.phase,
    outcome: response.outcome,
    reason: response.reason,
    sourceDigest: response.sourceDigest,
    artifactDigest: response.artifactDigest,
    proposalRevision: response.proposalRevision,
    targetPlanRef: response.targetPlanRef,
    planId: response.planId,
    handoffReconciled: response.handoffReconciled,
    lastReview: response.lastReview,
    duplicateReviews: response.duplicateReviews,
    templateVersion: response.templateVersion,
    toolVersions: response.toolVersions,
  };
}

export function asPackagingRunError(error: unknown): PackagingRunError {
  if (error instanceof PackagingRunError) return error;
  if (error instanceof ConnectError)
    return new PackagingRunError(statusForCode(error.code), error.message);
  if (error instanceof Error && error.name === "AbortError") {
    return new PackagingRunError(499, error.message);
  }
  if (error instanceof Error) return new PackagingRunError(500, error.message);
  return new PackagingRunError(500, "packaging run status is unavailable");
}

function statusForCode(code: Code): number {
  switch (code) {
    case Code.Canceled:
      return 499;
    case Code.Unknown:
      return 500;
    case Code.InvalidArgument:
      return 400;
    case Code.DeadlineExceeded:
      return 504;
    case Code.NotFound:
      return 404;
    case Code.AlreadyExists:
      return 409;
    case Code.PermissionDenied:
      return 403;
    case Code.ResourceExhausted:
      return 429;
    case Code.FailedPrecondition:
      return 400;
    case Code.Aborted:
      return 409;
    case Code.OutOfRange:
      return 400;
    case Code.Unimplemented:
      return 501;
    case Code.Internal:
      return 500;
    case Code.Unavailable:
      return 503;
    case Code.DataLoss:
      return 500;
    case Code.Unauthenticated:
      return 401;
    default: {
      const unexpected: never = code;
      return unexpected;
    }
  }
}
