import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import {
  operatorIdentity,
  paintOperatorIdentity,
} from "../../console/operator/shell.js";
import { createLiveHub } from "../../console/operator/live-hub.js";

function nodes() {
  const make = () => ({
    textContent: "",
    title: "",
    attributes: new Map<string, string>(),
    setAttribute(name: string, value: string) {
      this.attributes.set(name, value);
    },
    set innerHTML(_value: string) {
      throw new Error("Identity must use textContent");
    },
  });
  const label = make(),
    mark = make(),
    chip = make(),
    settings = make();
  const byId: Record<string, ReturnType<typeof make>> = {
    "operator-name": label,
    "operator-initials": mark,
    "operator-chip": chip,
    "settings-operator-identity": settings,
  };
  vi.stubGlobal("document", {
    getElementById: (id: string) => byId[id] ?? null,
  });
  return { label, mark, chip, settings };
}
afterEach(() => vi.unstubAllGlobals());
describe("operator identity presentation", () => {
  it.each([undefined, null, "", "  ", 42, {}])(
    "shows unavailable for %j rather than a fabricated principal",
    (name) => {
      expect(operatorIdentity(name)).toEqual({
        label: "Identity unavailable",
        initials: "?",
        title: "Operator identity unavailable",
        settingsLabel: "Native operator identity is unavailable.",
      });
    },
  );
  it("retains real names including Operator and non-Latin identities", () => {
    expect(operatorIdentity(" alice.smith ")).toEqual({
      label: "alice.smith",
      initials: "AS",
      title: "Operator: alice.smith",
      settingsLabel: "Signed in as alice.smith. The Hub session sets this.",
    });
    expect(operatorIdentity("Operator").label).toBe("Operator");
    expect(operatorIdentity("用户")).toMatchObject({
      label: "用户",
      initials: "用",
    });
  });
  it("writes hostile names as text and updates the accessible badge", () => {
    const { label, mark, chip, settings } = nodes();
    const name = '<img src=x onerror="attack()">';
    paintOperatorIdentity(name);
    expect(label.textContent).toBe(name);
    expect(settings.textContent).toBe(
      `Signed in as ${name}. The Hub session sets this.`,
    );
    expect(chip.title).toBe(`Operator: ${name}`);
    expect(chip.attributes.get("aria-label")).toBe(chip.title);
    paintOperatorIdentity(undefined);
    expect(label.textContent).toBe("Identity unavailable");
    expect(mark.textContent).toBe("?");
    expect(settings.textContent).toBe(
      "Native operator identity is unavailable.",
    );
    expect(chip.attributes.get("aria-label")).toBe(
      "Operator identity unavailable",
    );
  });
});

describe("native operator display boundaries", () => {
  function setup() {
    const dom = nodes();
    vi.stubGlobal("window", { location: { origin: "http://hub.local" } });
    let session: unknown = {
      actor: { username: "robot" },
      effective: { username: "alice.smith" },
    };
    let refused = false;
    let hold: Promise<void> | undefined;
    let ready: (() => void) | undefined;
    let sessionReads = 0;
    const calls: { path: string; method: string }[] = [];
    vi.stubGlobal(
      "fetch",
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path =
          input instanceof Request
            ? new URL(input.url).pathname
            : String(input);
        const method =
          input instanceof Request ? input.method : (init?.method ?? "GET");
        calls.push({ path, method });
        if (path === "/v1/session") {
          sessionReads++;
          return new Response(
            JSON.stringify(refused ? { error: "not signed in" } : session),
            { status: refused ? 401 : 200 },
          );
        }
        if (path.startsWith("/v1/environments") && hold) {
          ready?.();
          await hold;
        }
        if (path.endsWith("/ListPackagingRuns"))
          return new Response(
            JSON.stringify({
              configured: true,
              items: [],
              limit: 20,
              pageSize: 0,
              total: "0",
            }),
            { headers: { "content-type": "application/json" } },
          );
        return new Response(
          JSON.stringify({
            environments: [],
            installations: [],
            products: [],
            changeRequests: [],
            plans: [],
            promotions: [],
            items: [],
          }),
        );
      },
    );
    const state: Record<string, any> = { operatorName: "cached-old-user" };
    const hub = createLiveHub({ state, render: () => {} });
    return {
      ...dom,
      state,
      hub,
      calls,
      reads: () => sessionReads,
      setSession: (value: unknown) => {
        session = value;
      },
      refuse: () => {
        refused = true;
      },
      holdEnvironment: () => {
        let release!: () => void;
        hold = new Promise<void>((resolve) => {
          release = resolve;
        });
        const started = new Promise<void>((resolve) => {
          ready = resolve;
        });
        return { release, started };
      },
    };
  }
  it("displays the native effective identity without adding writes or session requests", async () => {
    const f = setup();
    expect(f.state.operatorName).toBe("");
    await f.hub.refresh();
    expect(f.label.textContent).toBe("alice.smith");
    expect(f.mark.textContent).toBe("AS");
    expect(f.reads()).toBe(1);
    expect(
      f.calls.every(
        (c) => c.method === "GET" || c.path.endsWith("/ListPackagingRuns"),
      ),
    ).toBe(true);
    f.hub.stop();
  });
  it.each(["missing", "actor-only", "refused"])(
    "clears stale identity for %s sessions",
    async (mode) => {
      const f = setup();
      await f.hub.refresh();
      if (mode === "missing") f.setSession({});
      else if (mode === "actor-only")
        f.setSession({ actor: { username: "unverified" } });
      else f.refuse();
      await f.hub.refresh();
      expect(f.state.operatorName).toBe("");
      expect(f.label.textContent).toBe("Identity unavailable");
      expect(f.mark.textContent).toBe("?");
      f.hub.stop();
    },
  );
  it("clears the old badge while a different caller's resources are still loading", async () => {
    const f = setup();
    await f.hub.refresh();
    f.setSession({
      actor: { username: "bob" },
      effective: { username: "bob" },
    });
    const gate = f.holdEnvironment();
    const pending = f.hub.refresh();
    await gate.started;
    const during = { label: f.label.textContent, name: f.state.operatorName };
    gate.release();
    await pending;
    expect(during).toEqual({ label: "Identity unavailable", name: "" });
    expect(f.label.textContent).toBe("bob");
    expect(f.chip.attributes.get("aria-label")).toBe("Operator: bob");
    expect(f.reads()).toBe(2);
    f.hub.stop();
  });
});
