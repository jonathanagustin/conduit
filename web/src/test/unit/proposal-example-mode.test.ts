import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createReleaseGates } from "../../console/operator/release-gates.js";

function harness(hub: any = {}) {
  const state: any = { hub, screen: "pt01-proposal-trace" };
  const audit = vi.fn();
  const ctx: any = { state, render: vi.fn(), recordAudit: audit };
  for (const key of [
    "rmApiStrip",
    "pill",
    "ontChip",
    "toolChip",
    "agentChip",
    "ic",
  ])
    ctx[key] = () => "";
  const gates = createReleaseGates(ctx);
  gates.extendState();
  const actions: any = {};
  gates.bind(actions);
  const root = { innerHTML: "", querySelectorAll: () => [] };
  const paint = (
    screen:
      | "pt01-proposal-trace"
      | "pt02-proposals-evals" = "pt01-proposal-trace",
  ) => {
    gates.renderers[screen](root);
    return root.innerHTML;
  };
  return { state, gates, actions, paint, audit };
}
afterEach(() => vi.unstubAllGlobals());

describe("explicit proposal simulation", () => {
  it.each([
    {},
    { packaging: [] },
    { packaging: [{}] },
    { packaging: [], packagingNote: "Forbidden" },
  ])("never substitutes samples for absent native evidence: %j", (hub) => {
    const h = harness(hub);
    expect(h.paint()).toContain("No recorded packaging proposal");
    expect(h.paint()).toContain("Show proposal example data");
    expect(h.paint()).not.toContain("PLN-2026-0416");
    expect(h.paint("pt02-proposals-evals")).not.toContain("PRP-901");
    expect(h.paint("pt02-proposals-evals")).not.toContain("EV-01");
  });
  it("labels opt-in simulations, discards edits on exit and makes no native or audit calls", () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const h = harness();
    h.actions.setProposalExampleMode(true);
    expect(h.paint()).toContain("only simulate results");
    expect(h.paint()).toContain("PLN-2026-0416");
    h.actions.runProposalTrace();
    const id = h.state.proposals[0].id;
    expect(id).not.toBe("PRP-901");
    h.actions.setProposalStatus(id, "Applied");
    h.actions.runExampleEvaluations();
    expect(h.state.evalBoost).toBe(true);
    h.actions.setProposalExampleMode(false);
    expect(h.state.proposals.some((row: any) => row.id === id)).toBe(false);
    expect(h.state.traceBanner).toBe("");
    expect(h.state.evalBoost).toBe(false);
    expect(h.paint()).not.toContain("PLN-2026-0416");
    expect(fetch).not.toHaveBeenCalled();
    expect(h.audit).not.toHaveBeenCalled();
  });
  it("guards direct and keyboard mutations while closed", () => {
    const h = harness();
    const original = structuredClone(h.state.proposals);
    h.actions.runProposalTrace();
    h.actions.setProposalStatus("PRP-901", "Rejected");
    h.actions.runExampleEvaluations();
    h.gates.onKey({ ctrlKey: true, key: "Enter", preventDefault: vi.fn() });
    expect(h.state.proposals).toEqual(original);
    expect(h.state.traceRuns).toBe(1);
    expect(h.state.evalBoost).toBe(false);
    expect(h.audit).not.toHaveBeenCalled();
  });
  it("keeps recorded gate results visible with simulation off", () => {
    const h = harness({
      packaging: [{ runId: "native-run" }],
      packagingEventsByRun: {
        "native-run": [
          {
            step: "schema",
            outcome: "PASS",
            reason: "validated native schema",
          },
        ],
      },
    });
    const html = h.paint("pt02-proposals-evals");
    expect(html).toContain("Recorded gate results");
    expect(html).toContain("validated native schema");
    expect(html).not.toContain("EV-01");
    expect(html).not.toContain("PRP-901");
  });
  it("prefers recorded proposals, closes simulation and does not revive it when native rows disappear", () => {
    const h = harness();
    h.actions.setProposalExampleMode(true);
    h.state.hub.packaging = [
      { runId: "native-run", phase: "review", outcome: "WAITING" },
    ];
    expect(h.paint()).toContain("Recorded proposal");
    expect(h.paint()).not.toContain("PLN-2026-0416");
    expect(h.state.proposalExamples).toBe(false);
    h.state.hub.packaging = [];
    expect(h.paint()).toContain("Show proposal example data");
    expect(h.paint()).not.toContain("PLN-2026-0416");
  });
});
