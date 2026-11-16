import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";

import {
  fetchPackagingRunDetail,
  packagingRunTransport,
  PackagingRunError,
} from "../../api/packaging-run.js";
import {
  derivePackagingSteps,
  PackagingRunDetailView,
} from "../../features/packaging/PackagingRunPage.js";
import { ViewStateBoundary } from "../../components/index.js";

const detailBody = {
  tier: "CONDUIT_NATIVE",
  configured: true,
  authority: "packaging-run",
  planAuthority: "conduit-plan",
  approvalAuthority: "conduit-governance",
  runId: "sample-app-1",
  phase: "review",
  outcome: "REJECTED",
  reason: "needs a person",
  sourceDigest: "sha256:source",
  artifactDigest: "sha256:artifact",
  proposalRevision: "rev-2",
  targetPlanRef: "plan-ref",
  planId: "plan_abc",
  handoffReconciled: true,
  lastReview: "accepted",
  duplicateReviews: 2,
};

describe("packaging-run detail view", () => {
  it("loads the read through the Connect client and renders it", async () => {
    const calls: { method: string; url: string; body: string }[] = [];
    const transport = packagingRunTransport(
      "http://hub.local",
      async (input, init) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        const body = await request.text();
        calls.push({ method: request.method, url: request.url, body });
        return new Response(JSON.stringify(detailBody), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      },
    );

    const detail = await fetchPackagingRunDetail("sample-app-1", transport);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe(
      "http://hub.local/conduit.packaging.v1.PackagingRunService/GetPackagingRun",
    );
    expect(calls[0]?.body).toContain("sample-app-1");
    expect(detail).toMatchObject(detailBody);

    const html = renderToStaticMarkup(
      createElement(PackagingRunDetailView, { detail }),
    );
    expect(html).toContain('data-testid="packaging-run-detail"');
    expect(html).toContain("sample-app-1");
    expect(html).toContain("review");
    expect(html).toContain("packaging-run");
    expect(html).toContain("conduit-plan");
    expect(html).toContain("conduit-governance");
    expect(html).toContain("plan_abc");
    expect(html).toContain("sha256:artifact");
  });

  it("surfaces an unauthorized Connect refusal as 401", async () => {
    const transport = packagingRunTransport("http://hub.local", async () => {
      return new Response(
        JSON.stringify({ code: "unauthenticated", message: "unauthorized" }),
        {
          status: 401,
          headers: { "content-type": "application/json" },
        },
      );
    });
    await expect(
      fetchPackagingRunDetail("sample-app-1", transport),
    ).rejects.toMatchObject({
      name: "PackagingRunError",
      status: 401,
    });
    await expect(
      fetchPackagingRunDetail("sample-app-1", transport),
    ).rejects.toBeInstanceOf(PackagingRunError);
  });

  it("derives pipeline stepper stages with rejection and handoff states", () => {
    const rejectedSteps = derivePackagingSteps(detailBody);
    expect(rejectedSteps).toEqual([
      { id: "assess", label: "Assess", state: "done", detail: undefined },
      { id: "build", label: "Build", state: "done", detail: undefined },
      { id: "verify", label: "Verify", state: "done", detail: undefined },
      { id: "review", label: "Review", state: "failed", detail: "REJECTED" },
      { id: "handoff", label: "Handoff", state: "done", detail: undefined },
    ]);

    const activeSteps = derivePackagingSteps({
      ...detailBody,
      phase: "build",
      outcome: "PENDING",
      handoffReconciled: false,
    });
    expect(activeSteps).toEqual([
      { id: "assess", label: "Assess", state: "done", detail: undefined },
      { id: "build", label: "Build", state: "active", detail: "PENDING" },
      { id: "verify", label: "Verify", state: "waiting", detail: undefined },
      { id: "review", label: "Review", state: "waiting", detail: undefined },
      { id: "handoff", label: "Handoff", state: "waiting", detail: undefined },
    ]);
  });

  it("renders loading and error view state boundary contracts correctly", () => {
    const loadingHtml = renderToStaticMarkup(
      createElement(ViewStateBoundary, {
        state: "loading",
        testId: "packaging-run-loading",
        loadingLabel: "Loading packaging run",
      }),
    );
    expect(loadingHtml).toContain('data-testid="packaging-run-loading"');
    expect(loadingHtml).toContain("Loading packaging run");
    expect(loadingHtml).toContain('role="status"');
    expect(loadingHtml).toContain('aria-busy="true"');

    const errorHtml = renderToStaticMarkup(
      createElement(ViewStateBoundary, {
        state: "disconnected",
        testId: "packaging-run-error",
        title: "Packaging run error",
        description: "Network timeout communicating with Hub",
      }),
    );
    expect(errorHtml).toContain('data-testid="packaging-run-error"');
    expect(errorHtml).toContain('role="alert"');
    expect(errorHtml).toContain("Network timeout communicating with Hub");
  });
});
