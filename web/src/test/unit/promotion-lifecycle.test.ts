import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { PromotionRecord } from "../../api/catalog.js";
import { latestForChannel, ReleaseChannelGraph } from "../../components/ReleaseChannelGraph.js";
import { TransitionDetail } from "../../components/TransitionDetail.js";

const base: PromotionRecord = {
  id: "evaluation", product: "group:product", version: "0.2.0", fromChannel: "DEV", toChannel: "PRODUCTION", status: "BLOCKED",
};

describe("promotion lifecycle rendering", () => {
  it.each(["RUNNING", "SUCCEEDED", "ABORTED", "CANCELLED"] as const)("shows %s consistently in graph and detail", (lifecycle) => {
    const promotion: PromotionRecord = {...base, lifecycle, status: lifecycle === "SUCCEEDED" ? "PASSED" : "BLOCKED", ...(lifecycle === "CANCELLED" ? {lifecycleReason: "Superseded by the approved fix"} : {}), valueTier: "CONDUIT_NATIVE"};
    const graph = renderToStaticMarkup(createElement(ReleaseChannelGraph, {promotions:[promotion], selectedId:undefined, onSelect:()=>{}}));
    const detail = renderToStaticMarkup(createElement(TransitionDetail, {promotion}));
    expect(graph).toContain(lifecycle);
    expect(detail).toContain(lifecycle);
    expect(detail).toContain("Conduit lifecycle");
    if (promotion.lifecycleReason) {
      expect(graph).toContain(promotion.lifecycleReason);
      expect(detail).toContain(promotion.lifecycleReason);
    }
  });
  it("preserves the legacy eligibility view when no lifecycle was reported", () => {
    const detail = renderToStaticMarkup(createElement(TransitionDetail, {promotion:base}));
    expect(detail).toContain("BLOCKED");
    expect(detail).not.toContain("Promotion in progress.");
  });
});


it("keeps the terminal state when one run's observations share a timestamp", () => {
 const running: PromotionRecord = {...base, runId:"run", lifecycle:"RUNNING", evaluatedAt:"2026-11-13T12:00:00Z"};
 const cancelled: PromotionRecord = {...running, id:"cancelled", previousEvaluationId:running.id, lifecycle:"CANCELLED", lifecycleReason:"Superseded"};
 expect(latestForChannel([running,cancelled],"DEV")?.id).toBe("cancelled");
 expect(latestForChannel([cancelled,running],"DEV")?.id).toBe("cancelled");
});
