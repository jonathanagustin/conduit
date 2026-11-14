import { ONTOLOGY, type ActionName, type ActionParams } from "./generated.js";
import type { Transport } from "./transport.js";

export type ActionRisk = "low" | "high";

export const ACTION_RISK: Record<string, ActionRisk> = {
  fork: "low",
  upgrade: "low",
  archive: "high",
  delete: "high",
  createChangeRequest: "high",
};

/** Unknown action names are treated as high risk. */
export function riskOf(name: string): ActionRisk {
  return Object.prototype.hasOwnProperty.call(ACTION_RISK, name) ? ACTION_RISK[name]! : "high";
}

export type ActionOutcome =
  | { kind: "invalid"; messages: Record<string, string>; submission?: string }
  | { kind: "confirm"; action: string; risk: "low" }
  | { kind: "applied"; action: string }
  | { kind: "change-request"; action: string; changeRequestId?: string; stored: boolean };

export type ActionParamsFor<N extends string> = N extends ActionName
  ? ActionParams[N]
  : Record<string, string>;

export interface Actions {
  /** Checks the parameters on the Hub without executing anything. */
  validate<N extends string>(name: N, params: ActionParamsFor<N>): Promise<ActionOutcome>;
  /**
   * Low-risk actions run only when `opts.confirmed` is true. High-risk actions are
   * never executed directly: they are proposed as a change request that a
   * different person must approve. This client never approves.
   */
  apply<N extends string>(
    name: N,
    params: ActionParamsFor<N>,
    opts?: { confirmed?: boolean },
  ): Promise<ActionOutcome>;
}

type Mode = "VALIDATE_ONLY" | "VALIDATE_AND_EXECUTE";

interface ApplyResponse {
  validation?: {
    result?: string;
    parameters?: Record<string, { result?: string; evaluatedConstraints?: { message?: string }[] }>;
    submissionCriteria?: { configuredFailureMessage?: string }[];
  };
  edits?: { edits?: { type?: string; objectType?: string; primaryKey?: string }[] };
}

function parseInvalid(response: ApplyResponse): ActionOutcome & { kind: "invalid" } {
  const messages: Record<string, string> = {};
  for (const [param, check] of Object.entries(response.validation?.parameters ?? {})) {
    if (check.result === "VALID") continue;
    const text = (check.evaluatedConstraints ?? [])
      .map((constraint) => constraint.message)
      .filter((message): message is string => !!message)
      .join("; ");
    messages[param] = text || check.result || "INVALID";
  }
  const submission = (response.validation?.submissionCriteria ?? [])
    .map((criterion) => criterion.configuredFailureMessage)
    .filter((message): message is string => !!message)
    .join("; ");
  return submission ? { kind: "invalid", messages, submission } : { kind: "invalid", messages };
}

function createdChangeRequest(response: ApplyResponse): string | undefined {
  const edit = (response.edits?.edits ?? []).find(
    (candidate) => candidate.type === "addObject" && candidate.objectType === "ChangeRequest",
  );
  return edit?.primaryKey || undefined;
}

export function createActions(transport: Transport): Actions {
  async function send(name: string, params: object, mode: Mode): Promise<ApplyResponse> {
    const response = await transport(
      `/api/v2/ontologies/${ONTOLOGY}/actions/${encodeURIComponent(name)}/apply`,
      { method: "POST", body: { parameters: params, options: { mode } } },
    );
    return (response ?? {}) as ApplyResponse;
  }

  async function validateAction(name: string, params: object): Promise<ActionOutcome> {
    const response = await send(name, params, "VALIDATE_ONLY");
    if (response.validation?.result !== "VALID") return parseInvalid(response);
    if (riskOf(name) === "low") return { kind: "confirm", action: name, risk: "low" };
    return { kind: "change-request", action: name, stored: false };
  }

  async function applyAction(
    name: string,
    params: object,
    opts: { confirmed?: boolean } = {},
  ): Promise<ActionOutcome> {
    if (riskOf(name) === "low") {
      // Without explicit confirmation, only validate; nothing is executed.
      if (opts.confirmed !== true) return validateAction(name, params);
      const response = await send(name, params, "VALIDATE_AND_EXECUTE");
      if (response.validation?.result === "INVALID") return parseInvalid(response);
      return { kind: "applied", action: name };
    }

    // High risk: propose a change request instead of executing the action.
    const proposal =
      name === "createChangeRequest"
        ? params
        : { ...params, changeType: (params as Record<string, unknown>).changeType ?? name };
    const response = await send("createChangeRequest", proposal, "VALIDATE_AND_EXECUTE");
    if (response.validation?.result === "INVALID") return parseInvalid(response);
    const changeRequestId = createdChangeRequest(response);
    return changeRequestId === undefined
      ? { kind: "change-request", action: name, stored: false }
      : { kind: "change-request", action: name, changeRequestId, stored: true };
  }

  return {
    validate: (name, params) => validateAction(name, params),
    apply: (name, params, opts) => applyAction(name, params, opts),
  };
}
