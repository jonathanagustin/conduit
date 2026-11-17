import { create, toBinary } from "@bufbuild/protobuf";
import { createServer, type Server } from "node:http";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  planApprovalTransport,
  submitPlanApproval,
} from "../../api/plan-approval.js";
import { PlanApprovalOutcomeSchema } from "../../api/gen/conduit/plans/v1/plan_approval_pb.js";
import { approvePlan } from "../../console/data/hub.js";

const origin = "http://hub.local";
const path = "/conduit.plans.v1.PlanApprovalService/ApprovePlan";
const id = "plan /+=<native>";
const at = "2026-11-16T12:00:00.123456789Z";
const body = () => ({
  plan: {
    id,
    environment: "dev",
    status: "FUTURE_NATIVE_STATUS",
    stepCount: "2",
    approvalCount: "1",
    requiredApprovals: "2",
    createdAt: at,
    updatedAt: at,
    revision: "18446744073709551615",
  },
  approvals: [{ subject: "reviewer", rationale: "reviewed", createdAt: at }],
  changeRequestId: "cr-native",
});
const reply = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json" },
  });
function install(respond: (request: Request) => Response | Promise<Response>) {
  vi.stubGlobal("window", { location: { origin, pathname: "/console/" } });
  const calls: Request[] = [];
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    expect(input).toBeInstanceOf(Request);
    const request = input as Request;
    calls.push(request);
    expect(request.url).toBe(origin + path);
    expect(request.method).toBe("POST");
    expect(request.credentials).toBe("same-origin");
    expect(request.redirect).toBe("error");
    expect(
      Number(request.headers.get("Connect-Timeout-Ms")),
    ).toBeLessThanOrEqual(10000);
    return respond(request);
  });
  return calls;
}
function detail(planId = id, approvalRecorded = true, handoffFailed = true) {
  return {
    type: PlanApprovalOutcomeSchema.typeName,
    value: Buffer.from(
      toBinary(
        PlanApprovalOutcomeSchema,
        create(PlanApprovalOutcomeSchema, {
          planId,
          approvalRecorded,
          handoffFailed,
        }),
      ),
    ).toString("base64"),
  };
}
afterEach(() => vi.unstubAllGlobals());

describe("native Plan approval generated console client", () => {
  it("sends one exact command and keeps native bigint/nanosecond evidence", async () => {
    const calls = install(async (request) => {
      expect(await request.json()).toEqual({
        planId: id,
        rationale: "  reviewed target  ",
      });
      return reply(body());
    });
    const receipt = await submitPlanApproval(
      planApprovalTransport(origin),
      id,
      "  reviewed target  ",
    );
    expect(receipt.plan?.revision).toBe(BigInt("18446744073709551615"));
    expect(receipt.plan?.createdAt?.nanos).toBe(123456789);
    expect(receipt.plan?.requiredApprovals).toBe(BigInt(2));
    expect(receipt.plan?.status).toBe("FUTURE_NATIVE_STATUS");
    expect(receipt.approvals[0]?.subject).toBe("reviewer");
    expect(receipt.changeRequestId).toBe("cr-native");
    expect(calls).toHaveLength(1);
  });

  it("keeps the legacy void contract without a follow-up read or REST replay", async () => {
    const calls = install(() => reply(body()));
    expect(await approvePlan(id, "reviewed")).toBeUndefined();
    expect(calls).toHaveLength(1);
  });

  it.each([
    {},
    { plan: { ...body().plan, id: "wrong-plan" } },
    { plan: { ...body().plan, status: "" } },
    { plan: { ...body().plan, stepCount: "-1" } },
    { plan: { ...body().plan, requiredApprovals: "-1" } },
    { plan: { ...body().plan, approvalCount: "0" } },
    { plan: { ...body().plan, approvalCount: "2" } },
    { plan: { ...body().plan, createdAt: "invalid" } },
    { plan: { ...body().plan, updatedAt: undefined } },
    { approvals: [] },
    { approvals: [{ subject: "", createdAt: at }] },
    { approvals: [{ subject: "reviewer" }] },
  ])("treats malformed success as unknown without replay %j", async (patch) => {
    const value = Object.keys(patch).length ? { ...body(), ...patch } : {};
    const calls = install(() => reply(value));
    await expect(approvePlan(id, "reviewed")).rejects.toMatchObject({
      name: "PlanApprovalError",
      outcomeKnown: false,
      approvalRecorded: false,
      handoffFailed: false,
    });
    expect(calls).toHaveLength(1);
  });

  it.each([
    [401, "unauthenticated", 401],
    [403, "permission_denied", 403],
    [400, "invalid_argument", 422],
    [404, "not_found", 404],
    [409, "failed_precondition", 409],
    [501, "unimplemented", 501],
    [503, "unavailable", 503],
  ] as const)(
    "retains refusal status %i without exposing diagnostics or replay",
    async (status, code, expected) => {
      const calls = install(() =>
        reply({ code, message: "PRIVATE_BACKEND_DIAGNOSTIC" }, status),
      );
      const error = await approvePlan(id, "reviewed").catch(
        (error: unknown) => error,
      );
      expect(error).toMatchObject({ status: expected, outcomeKnown: false });
      expect((error as Error).message).not.toContain(
        "PRIVATE_BACKEND_DIAGNOSTIC",
      );
      expect(calls).toHaveLength(1);
    },
  );

  it.each([
    { details: [detail()], known: true, handoff: true },
    { details: [detail(id, true, false)], known: true, handoff: false },
    { details: [], known: false, handoff: false },
    { details: [detail("wrong-plan")], known: false, handoff: false },
    { details: [detail(id, false)], known: false, handoff: false },
    {
      details: [detail(), detail("conflicting-plan")],
      known: false,
      handoff: false,
    },
    {
      details: [{ type: PlanApprovalOutcomeSchema.typeName, value: "////" }],
      known: false,
      handoff: false,
    },
  ])(
    "distinguishes native recorded outcome from uncertainty %j",
    async ({ details, known, handoff }) => {
      const calls = install(() =>
        reply({ code: "unavailable", message: "PRIVATE", details }, 503),
      );
      const error = await approvePlan(id, "reviewed").catch(
        (error: unknown) => error,
      );
      expect(error).toMatchObject({
        status: 503,
        outcomeKnown: known,
        approvalRecorded: known,
        handoffFailed: handoff,
      });
      expect((error as Error).message).not.toContain("PRIVATE");
      expect(calls).toHaveLength(1);
    },
  );

  it("does not replay a lost reply after the fixture has recorded the review", async () => {
    let recorded = 0;
    const calls = install(() => {
      recorded++;
      throw new TypeError("connection lost");
    });
    await expect(approvePlan(id, "reviewed")).rejects.toMatchObject({
      outcomeKnown: false,
    });
    expect(recorded).toBe(1);
    expect(calls).toHaveLength(1);
  });

  it.each([
    ["", "reviewed"],
    [id, "  "],
  ])(
    "preflights empty identity/rationale without a request",
    async (planId, rationale) => {
      const calls = install(() => reply(body()));
      await expect(approvePlan(planId, rationale)).rejects.toMatchObject({
        status: 422,
      });
      expect(calls).toHaveLength(0);
    },
  );

  it("cancels the generated request without replay", async () => {
    let started!: () => void;
    const ready = new Promise<void>((resolve) => {
      started = resolve;
    });
    const calls = install((request) => {
      started();
      return new Promise<Response>((_, reject) =>
        request.signal.addEventListener(
          "abort",
          () => reject(new DOMException("Aborted", "AbortError")),
          { once: true },
        ),
      );
    });
    const controller = new AbortController();
    const pending = expect(
      approvePlan(id, "reviewed", controller.signal),
    ).rejects.toMatchObject({ status: 499, outcomeKnown: false });
    await ready;
    controller.abort();
    await pending;
    expect(calls).toHaveLength(1);
  });

  it.each([302, 307, 308])(
    "refuses actual HTTP %i command redirects before contacting the target",
    async (status) => {
      let targets = 0;
      const server: Server = createServer((request, response) => {
        if (request.url === path) {
          response.writeHead(status, { location: "/target" });
          response.end();
        } else {
          targets++;
          response.writeHead(200, { "content-type": "application/json" });
          response.end(JSON.stringify(body()));
        }
      });
      await new Promise<void>((resolve) =>
        server.listen(0, "127.0.0.1", resolve),
      );
      try {
        const address = server.address();
        if (!address || typeof address === "string")
          throw new Error("fixture address unavailable");
        await expect(
          submitPlanApproval(
            planApprovalTransport(`http://127.0.0.1:${address.port}`),
            id,
            "reviewed",
          ),
        ).rejects.toMatchObject({ outcomeKnown: false });
        expect(targets).toBe(0);
      } finally {
        server.closeAllConnections();
        await new Promise<void>((resolve, reject) =>
          server.close((error) => (error ? reject(error) : resolve())),
        );
      }
    },
  );
});
