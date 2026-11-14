import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { blueprintVariant, GATE_PRESET } from "../../console/components/base/variants.js";
import { columnOf, handoffChecklist, healthShares, planGate } from "../../console/logic.js";
import { CONDUIT_ACTIONS } from "../../console/ontology.js";

describe("blueprint variants", () => {
  it("maps emphasis onto Blueprint button props", () => {
    expect(blueprintVariant({ emphasis: "outlined", intent: "warning", size: "sm" })).toEqual({
      intent: "warning",
      size: "small",
      compact: false,
      minimal: true,
      outlined: true,
    });
    expect(blueprintVariant({ emphasis: "filled", intent: "none" }).intent).toBeUndefined();
  });

  it("presets the gate tags", () => {
    expect(GATE_PRESET.GO).toEqual({ intent: "success", emphasis: "filled" });
    expect(GATE_PRESET.CAUTION.emphasis).toBe("outlined");
    expect(GATE_PRESET["NO-GO"].intent).toBe("danger");
  });
});

describe("domain decisions stay in logic", () => {
  it("gates a plan and sorts the board", () => {
    expect(planGate("SUCCEEDED")).toBe("GO");
    expect(planGate("AWAITING_APPROVAL")).toBe("CAUTION");
    expect(planGate("FAILED")).toBe("NO-GO");
    expect(columnOf("AWAITING_APPROVAL")).toBe("approval");
  });

  it("explains each merge requirement", () => {
    const items = handoffChecklist({
      validationPassed: false,
      approvalCount: 0,
      requiredApprovals: 1,
      rejections: 1,
      phase: "review",
    });
    expect(items.find((item) => item.id === "validation")?.detail).toContain("not passed");
    expect(items.find((item) => item.id === "rejections")?.passed).toBe(false);
    expect(items.find((item) => item.id === "before-handoff")?.passed).toBe(true);
  });
});

describe("release composition", () => {
  it("splits installation health and keeps the approval block reason", () => {
    expect(healthShares([{ health: "HEALTHY" }, { health: "ERROR" }, { health: "UNKNOWN" }])).toEqual({
      ok: 1,
      bad: 1,
      other: 1,
    });
    expect(CONDUIT_ACTIONS.find((action) => action.actionId === "approve_plan")?.blockingRule).toContain("Requirement");
  });
});

describe("shell breakpoints", () => {
  it("collapses the rails below 1024px and scrolls tables", () => {
    const css = readFileSync(join(import.meta.dirname, "../../console/theme/tokens.css"), "utf8");
    expect(css).toContain("max-width: 1023px");
    expect(css).toContain("is-chat-open");
    expect(css).toContain("conduit-table-scroll");
  });
});

describe("base layer boundary", () => {
  it("does not import domain or hub data", () => {
    const dir = join(import.meta.dirname, "../../console/components/base");
    const files = readdirSync(dir).filter((name) => name.endsWith(".ts") || name.endsWith(".tsx"));
    for (const name of files) {
      const text = readFileSync(join(dir, name), "utf8");
      expect(text.includes("components/domain")).toBe(false);
      expect(text.includes("console/data")).toBe(false);
    }
  });
});
