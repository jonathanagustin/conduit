// @vitest-environment node
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it } from "vite-plus/test";

import {
  bindDeliveryCapabilities,
  capabilityBlocks,
  deliveryCapabilityHook,
  idleAction,
  planReviewState,
} from "../../console/operator/delivery-actions.js";
import {
  decisionMarkup,
  proposalMarkup,
} from "../../console/operator/delivery-journey.js";
import { HUB_SESSION_OPERATIONS } from "../../console/operator/hub-vocabulary.js";
import {
  capabilitiesFromSession,
  capabilitiesUnavailable,
  decideOperation,
  screenVisible,
} from "../../console/operator/session-capabilities.js";

const repo = (path: string) =>
  readFileSync(new URL(`../../../../${path}`, import.meta.url), "utf8");

const digest = `sha256:${"ab12".repeat(16)}`;

const session = (over: Record<string, unknown> = {}) => ({
  applications: [
    { id: "change-requests", enabled: true },
    { id: "fleet", enabled: false },
    { id: "plans", enabled: true },
    { id: "promotions", enabled: false },
    { id: "teams", enabled: true },
  ],
  globalOperations: ["resource:read"],
  resources: [
    { resourceRid: "environment/dev", operations: ["resource:read"] },
    {
      resourceRid: "environment/stage",
      operations: ["resource:read", "change:propose"],
    },
  ],
  ...over,
});

const buttons = (html: string, attribute: string) =>
  [...html.matchAll(new RegExp(`<button[^>]*${attribute}[^>]*>`, "g"))].map(
    (match) => match[0],
  );

// ---------------------------------------------------------------------------
// #824: write controls ask the session
// ---------------------------------------------------------------------------

describe("deliveryCapabilityHook", () => {
  afterEach(() => bindDeliveryCapabilities(null));
  const capabilities = capabilitiesFromSession(session());
  const canPerform = (operation: string, resourceRid: string) =>
    decideOperation(capabilities, operation, resourceRid);

  it("does not decide before the session capabilities are bound", () => {
    expect(deliveryCapabilityHook("change:propose", "environment/dev")).toEqual(
      { allowed: null, reason: "" },
    );
  });

  it("asks canPerform with the operation and the resource", () => {
    const asked: string[][] = [];
    bindDeliveryCapabilities((operation, resourceRid) => {
      asked.push([operation, resourceRid]);
      return { allowed: true, reason: "" };
    });
    expect(
      deliveryCapabilityHook("change:propose", "environment/stage"),
    ).toEqual({ allowed: true, reason: "" });
    expect(asked).toEqual([["change:propose", "environment/stage"]]);
  });

  it("blocks only on a stated refusal, and carries its reason", () => {
    bindDeliveryCapabilities(canPerform);
    const refused = deliveryCapabilityHook("change:propose", "environment/dev");
    expect(refused).toEqual({
      allowed: false,
      reason: "Your session does not hold change:propose on environment/dev.",
    });
    expect(capabilityBlocks(refused)).toBe(true);
    const held = deliveryCapabilityHook("change:propose", "environment/stage");
    expect(held.allowed).toBe(true);
    expect(capabilityBlocks(held)).toBe(false);
  });

  it("leaves the control live when the session did not report the resource", () => {
    bindDeliveryCapabilities(canPerform);
    const unknown = deliveryCapabilityHook("change:propose", "environment/qa");
    expect(unknown).toEqual({ allowed: null, reason: "" });
    expect(capabilityBlocks(unknown)).toBe(false);
  });

  it("does not block an operation the session never lists", () => {
    // Who may approve is decided by approval policy; the session answer never
    // contains it, so its absence is not a refusal.
    bindDeliveryCapabilities(canPerform);
    expect(
      deliveryCapabilityHook("change:review", "environment/dev").allowed,
    ).toBeNull();
  });
});

describe("delivery controls under a capability", () => {
  afterEach(() => bindDeliveryCapabilities(null));
  const release = (version: string) => ({
    product: "app.example:api",
    version,
    chart: { repository: "r/api", tag: version, digest },
    images: [],
  });
  const hub = {
    plans: [
      {
        id: "pln_1",
        status: "SUCCEEDED",
        createdAt: "2026-01-01T00:00:00Z",
        plan: { environment: "dev" },
        desiredState: {
          environment: { id: "dev", generation: "gen-1" },
          releases: [release("0.1.0")],
          entities: [
            { name: "api", product: "app.example:api", version: "0.1.0" },
          ],
        },
      },
    ],
    products: [
      {
        product: "app.example:api",
        releases: [release("0.1.0"), release("0.2.0")],
      },
    ],
  };
  const panel = {
    action: idleAction(),
    draft: { generation: "gen-2", rationale: "roll" },
  };

  it("disables Review proposal on a refusal, with the reason as tooltip and note", () => {
    bindDeliveryCapabilities(() => ({
      allowed: false,
      reason: "Your session does not hold change:propose on environment/dev.",
    }));
    const html = proposalMarkup(hub, "dev", panel);
    const [button] = buttons(html, "data-dj-review");
    expect(button).toContain("disabled");
    expect(button).toContain(
      'title="Your session does not hold change:propose on environment/dev."',
    );
    expect(html).toMatch(
      /data-dj-capability>Your session does not hold change:propose/,
    );
  });

  it("keeps the control live on an unknown answer and says nothing", () => {
    bindDeliveryCapabilities(() => ({
      allowed: null,
      reason: "The session read did not report environment/dev.",
    }));
    const html = proposalMarkup(hub, "dev", panel);
    expect(buttons(html, "data-dj-review")[0]).not.toContain("disabled");
    expect(html).not.toContain("data-dj-capability");
  });

  it("does not disable Approve because the session omits a review operation", () => {
    const capabilities = capabilitiesFromSession(session());
    bindDeliveryCapabilities((operation, resourceRid) =>
      decideOperation(capabilities, operation, resourceRid),
    );
    const plan = {
      id: "pln_9",
      status: "AWAITING_APPROVAL",
      changeRequestId: "crq_9",
      plan: { environment: "dev" },
    };
    const review = planReviewState(
      plan,
      [{ id: "crq_9", status: "PENDING_APPROVALS", author: { username: "a" } }],
      "bob",
      ["bob"],
    );
    const ready = buttons(
      decisionMarkup(plan, review, {
        action: idleAction(),
        draft: { rationale: "reviewed" },
      }),
      "data-dj-review",
    );
    expect(ready).toHaveLength(2);
    expect(ready.some((button) => button.includes("disabled"))).toBe(false);
  });
});

describe("session operation vocabulary", () => {
  it("is the Hub's OperationVocabulary, in both directions", () => {
    const go = repo("internal/governance/domain/authorization.go");
    const block = /var OperationVocabulary = \[\]string\{([\s\S]*?)\n\}/.exec(
      go,
    )?.[1];
    const hub = [...(block ?? "").matchAll(/\/\/ ([a-z:-]+)$/gm)].map(
      (match) => match[1]!,
    );
    expect(hub.length).toBeGreaterThan(5);
    expect([...HUB_SESSION_OPERATIONS]).toEqual(hub);
  });

  it("does not answer for an operation outside it", () => {
    const capabilities = capabilitiesFromSession(session());
    expect(
      decideOperation(
        capabilities,
        "environment:approve-change",
        "environment/dev",
      ),
    ).toMatchObject({ allowed: null });
    expect(
      decideOperation(capabilities, "resource:operate", "environment/dev")
        .allowed,
    ).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// #824: the palette lists what the rail lists
// ---------------------------------------------------------------------------

describe("screenVisible", () => {
  const AREAS = {
    "area-releases": {
      label: "Releases",
      tabs: [
        { id: "products", label: "Products" },
        { id: "promotion", label: "Promotion" },
      ],
    },
    "area-fleet": {
      label: "Fleet",
      tabs: [{ id: "environments", label: "Environments" }],
    },
    "area-intelligence": {
      label: "Intelligence",
      tabs: [{ id: "logic", label: "Logic" }],
    },
  };
  const PLACEMENT: Record<string, { area: string; tab: string }> = {
    "flt-environments": { area: "area-fleet", tab: "environments" },
    "rel-products": { area: "area-releases", tab: "products" },
    "rm06-promotion-pipe": { area: "area-releases", tab: "promotion" },
  };
  const placement = (id: string) => PLACEMENT[id] ?? null;
  const restricted = capabilitiesFromSession(session());
  const visible = (id: string, capabilities = restricted) =>
    screenVisible(id, AREAS, placement, capabilities);

  it("withholds an area the session does not enable, and its screens", () => {
    expect(visible("area-fleet")).toBe(false);
    expect(visible("flt-environments")).toBe(false);
  });

  it("withholds one tab's screen while its area stays", () => {
    expect(visible("area-releases")).toBe(true);
    expect(visible("rel-products")).toBe(true);
    expect(visible("rm06-promotion-pipe")).toBe(false);
  });

  it("keeps entries no application gates, and a gated rail screen follows its id", () => {
    expect(visible("area-intelligence")).toBe(true);
    expect(visible("s19-ontology")).toBe(true);
    expect(visible("adm02-org-control-panel")).toBe(true);
    expect(
      visible(
        "adm02-org-control-panel",
        capabilitiesFromSession(session({ applications: [] })),
      ),
    ).toBe(false);
  });

  it("withholds mapped entries while capabilities are unavailable", () => {
    const unavailable = capabilitiesUnavailable("");
    expect(visible("flt-environments", unavailable)).toBe(false);
    expect(visible("area-intelligence", unavailable)).toBe(true);
  });

  it("shows everything to a fully enabled session", () => {
    const full = capabilitiesFromSession(
      session({
        applications: ["change-requests", "fleet", "plans", "promotions"].map(
          (id) => ({ id, enabled: true }),
        ),
      }),
    );
    for (const id of ["area-fleet", "flt-environments", "rm06-promotion-pipe"])
      expect(visible(id, full)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
