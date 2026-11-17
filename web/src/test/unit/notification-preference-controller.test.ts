// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { createNotifications } from "../../console/operator/notifications.js";

class Control {
  value = "";
  checked = false;
  constructor(private attributes: Record<string, string>) {}
  getAttribute(name: string) {
    return this.attributes[name] ?? null;
  }
  hasAttribute(name: string) {
    return name in this.attributes;
  }
  closest(selector: string) {
    return this.hasAttribute(selector.slice(1, -1)) ? this : null;
  }
}

const coverage = {
  typeAvailabilityScope: "ALL_KNOWN",
  typesNotYetEmitted: ["RBAC_CHANGE"],
};
const stored = (resourceRid = "product/A", channel = "IN_APP") => ({
  resourceRid,
  channel,
  owner: { kind: "USER", id: "alice" },
  types: ["PLAN_FAILURES"],
  revision: 3,
  ...coverage,
});
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const missing = () => response({ error: "absent", ...coverage }, 404);
const settle = async () => {
  for (let index = 0; index < 15; index++) await Promise.resolve();
};

function harness() {
  const listeners: Record<string, (event: any) => void> = {};
  const root = {
    innerHTML: "",
    contains: () => false,
    querySelector: () => null,
    firstElementChild: {
      addEventListener: (type: string, handler: (event: any) => void) => {
        listeners[type] = handler;
      },
    },
  };
  vi.stubGlobal("Element", Control);
  vi.stubGlobal("document", {
    activeElement: null,
    getElementById: () => null,
  });
  const state: any = {
    screen: "ntf-inbox",
    hub: {},
    reviewOperatorActor: "alice",
    reviewOperatorName: "alice",
    reviewOperatorIdentities: ["alice"],
  };
  const controller = createNotifications({
    state,
    render: () => controller.renderers["ntf-inbox"](root),
    capabilities: { watch() {}, canPerform: () => ({ allowed: true }) },
  });
  state.notifications.view = "preferences";
  const pref = state.notifications.pref;
  Object.assign(pref, {
    ridDraft: "product/A",
    resourceRid: "product/A",
    status: "ready",
    stored: null,
    availabilityKnown: true,
    notYetEmitted: ["RBAC_CHANGE"],
    draft: { types: ["PLAN_FAILURES"], allFutureTypes: false, contactId: "" },
  });
  controller.renderers["ntf-inbox"](root);
  const writes: any[] = [];
  const reads: { path: string; signal: AbortSignal }[] = [];
  let release!: (reply: Response) => void;
  let releaseWrite: ((reply: Response) => void) | undefined;
  let holdWrite = false;
  vi.stubGlobal("fetch", async (path: string, init: RequestInit) => {
    if (!path.includes("/notifications/preferences"))
      return response({ entries: [], owner: { kind: "USER", id: "alice" } });
    if (init.method === "GET") {
      reads.push({ path, signal: init.signal as AbortSignal });
      if (reads.length === 1)
        // Deliberately ignore cancellation: the generation fence must also
        // reject replies delivered by a transport after it was aborted.
        return new Promise<Response>((resolve) => {
          release = resolve;
        });
      return missing();
    }
    const body = JSON.parse(init.body as string);
    writes.push(body);
    if (holdWrite)
      return new Promise<Response>((resolve) => {
        releaseWrite = resolve;
      });
    return response({ ...stored(body.resourceRid, body.channel), ...body });
  });
  const event = (type: string, attrs: Record<string, string>, value = "") => {
    const target = new Control(attrs);
    target.value = value;
    target.checked = true;
    listeners[type]!({ target, preventDefault() {} });
  };
  return {
    state,
    pref,
    controller,
    writes,
    reads,
    event,
    save: () => event("click", { "data-ntf-action": "save-preference" }),
    load: (rid: string) => {
      pref.ridDraft = rid;
      event("click", { "data-ntf-action": "reload-preference" });
    },
    release: (reply = missing()) => release(reply),
    holdWrite: () => {
      holdWrite = true;
    },
    releaseWrite: () => releaseWrite!(response(stored())),
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("notification preference save scope", () => {
  it("saves explicit off consent after the same reviewed-target preflight", async () => {
    const h = harness();
    h.pref.stored = stored();
    h.pref.draft = {
      types: [],
      allFutureTypes: false,
      contactId: "selected-contact",
    };
    h.save();
    expect(h.reads).toHaveLength(1);
    expect(h.writes).toEqual([]);
    h.release(response(stored()));
    await settle();
    expect(h.writes).toEqual([
      {
        resourceRid: "product/A",
        channel: "IN_APP",
        types: [],
        allFutureTypes: false,
        contactId: "selected-contact",
      },
    ]);
    expect(h.pref.saved).toContain("Saved.");
  });

  it.each(["resource", "channel", "identity", "navigation", "dispose"])(
    "aborts and refuses the old preflight after %s changes",
    async (change) => {
      const h = harness();
      h.save();
      expect(h.reads).toHaveLength(1);
      if (change === "resource") {
        h.load("product/B");
        await settle();
        h.pref.draft.types = ["PRODUCT_RECALLS"];
      } else if (change === "channel") {
        h.event("change", { "data-ntf-channel": "" }, "EMAIL");
        await settle();
        h.pref.draft.types = ["PRODUCT_RECALLS"];
      } else if (change === "identity") {
        h.state.reviewOperatorActor = "bob";
        h.state.reviewOperatorName = "bob";
        h.controller.sync();
      } else if (change === "navigation") {
        h.state.screen = "s13-fleet-overview";
        h.controller.sync();
      } else (h.controller as any).dispose?.();
      expect(h.reads[0]!.signal.aborted).toBe(true);
      h.release();
      await settle();
      expect(h.writes).toEqual([]);
      expect(h.reads[0]!.signal.aborted).toBe(true);
      expect(h.pref.saved).toBe("");
    },
  );

  it("preserves an edit made during preflight for a later explicit save", async () => {
    const h = harness();
    h.save();
    h.event("change", { "data-ntf-type": "PRODUCT_RECALLS" });
    expect(h.reads[0]!.signal.aborted).toBe(true);
    h.release();
    await settle();
    expect(h.writes).toEqual([]);
    expect(h.reads[0]!.signal.aborted).toBe(true);
    expect(h.pref.draft.types).toEqual(["PLAN_FAILURES", "PRODUCT_RECALLS"]);
    expect(h.pref.saving).toBe(false);
  });

  it("also fences draft mutations that did not go through an input handler", async () => {
    const h = harness();
    h.save();
    h.pref.draft.types = ["PRODUCT_RECALLS"];
    h.release();
    await settle();
    expect(h.writes).toEqual([]);
    expect(h.pref.draft.types).toEqual(["PRODUCT_RECALLS"]);
    expect(h.pref.saving).toBe(false);
  });

  it("fences a direct target mutation without leaving the editor saving", async () => {
    const h = harness();
    h.save();
    h.pref.resourceRid = "product/B";
    h.release();
    await settle();
    expect(h.writes).toEqual([]);
    expect(h.reads[0]!.signal.aborted).toBe(true);
    expect(h.pref.saving).toBe(false);
  });

  it.each(["revision", "resource", "channel", "read failure"])(
    "refuses a preflight with a conflicting %s",
    async (mismatch) => {
      const h = harness();
      h.pref.stored = stored();
      h.save();
      const body = stored();
      if (mismatch === "revision") body.revision = 4;
      if (mismatch === "resource") body.resourceRid = "product/B";
      if (mismatch === "channel") body.channel = "EMAIL";
      h.release(response(body, mismatch === "read failure" ? 503 : 200));
      await settle();
      expect(h.writes).toEqual([]);
      expect(h.pref.error).not.toBe("");
      expect(h.pref.saving).toBe(false);
    },
  );

  it("sends one unchanged reviewed payload after its matching preflight", async () => {
    const h = harness();
    h.pref.stored = stored();
    h.pref.draft.types = ["PRODUCT_RECALLS"];
    h.save();
    h.save();
    expect(h.reads).toHaveLength(1);
    expect(h.writes).toEqual([]);
    h.release(response(stored()));
    await settle();
    expect(h.writes).toEqual([
      {
        resourceRid: "product/A",
        channel: "IN_APP",
        types: ["PRODUCT_RECALLS"],
        allFutureTypes: false,
      },
    ]);
    expect(h.pref.saved).toContain("Saved.");
    expect(h.pref.saving).toBe(false);
  });

  it("does not apply a late PUT receipt to a replacement editor", async () => {
    const h = harness();
    h.holdWrite();
    h.save();
    h.release();
    await settle();
    expect(h.writes).toHaveLength(1);
    h.load("product/B");
    await settle();
    h.pref.draft.types = ["PRODUCT_RECALLS"];
    h.releaseWrite();
    await settle();
    expect(h.pref.resourceRid).toBe("product/B");
    expect(h.pref.stored).toBe(null);
    expect(h.pref.draft.types).toEqual(["PRODUCT_RECALLS"]);
    expect(h.pref.saved).toBe("");
  });

  it("preserves draft edits made after PUT starts without replay or a stale receipt", async () => {
    const h = harness();
    h.holdWrite();
    h.save();
    h.release();
    await settle();
    expect(h.writes).toHaveLength(1);
    h.event("change", { "data-ntf-type": "PRODUCT_RECALLS" });
    h.releaseWrite();
    await settle();
    expect(h.writes).toHaveLength(1);
    expect(h.writes[0].types).toEqual(["PLAN_FAILURES"]);
    expect(h.pref.draft.types).toEqual(["PLAN_FAILURES", "PRODUCT_RECALLS"]);
    expect(h.pref.saved).toBe("");
    expect(h.pref.error).toContain("may have completed");
    h.save();
    await settle();
    expect(h.writes).toHaveLength(1);
    expect(h.reads).toHaveLength(1);
  });

  it("does not dispatch a queued Save after disposal", async () => {
    const h = harness();
    h.controller.dispose();
    h.save();
    await settle();
    expect(h.reads).toEqual([]);
    expect(h.writes).toEqual([]);
  });
});
