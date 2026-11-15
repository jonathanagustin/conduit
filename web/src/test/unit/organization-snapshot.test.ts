import { afterEach, describe, it, expect, vi } from "vitest";
import { createSettings } from "../../console/operator/settings.js";
function harness(overrides: Record<string, any> = {}) {
  const state: any = {
    reviewOperatorActor: "alice",
    reviewOperatorIdentities: ["alice"],
  };
  const calls: { path: string; method: string }[] = [];
  const render = vi.fn();
  const settings = createSettings({ state, render });
  settings.extendState();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: any, options: any = {}) => {
      const path = String(input);
      calls.push({ path, method: options.method || "GET" });
      const defaults: Record<string, any> = {
        "/v1/operator-identity": { enabled: false, providers: [] },
        "/v1/teams": { teams: [] },
        "/v1/resource-grants": { resourceGrants: [] },
        "/v1/approval-policies": { approvalPolicies: [] },
        "/health/ready": { status: "ok" },
        "/v1/session": {
          actor: { username: state.reviewOperatorActor },
          effective: { username: state.reviewOperatorActor, teams: [] },
          applications: [],
        },
      };
      const value = Object.hasOwn(overrides, path)
        ? overrides[path]
        : defaults[path];
      if (typeof value === "function") return value();
      return new Response(JSON.stringify(value || { error: "unavailable" }), {
        status: value ? 200 : 404,
      });
    }),
  );
  const root = {
    innerHTML: "",
    get textContent() {
      return this.innerHTML.replace(/<[^>]*>/g, "");
    },
  };
  return {
    state,
    settings,
    calls,
    root,
    render,
    paint: () => settings.renderers["adm02-org-control-panel"](root),
  };
}
afterEach(() => vi.unstubAllGlobals());
describe("native read-only organization snapshots", () => {
  it("keeps valid empty directories empty and separates user/group memberships", async () => {
    const h = harness();
    h.settings.ensureLoaded();
    await vi.waitFor(() => expect(h.state.settingsLoaded).toBe(true));
    h.state.orgTab = "teams";
    h.paint();
    expect(h.root.textContent).toContain("0 stored teams returned");
    expect(h.root.textContent).toContain("0 stored grants returned");
    expect(h.root.textContent).not.toContain("release-managers");
    h.state.orgTab = "org";
    h.paint();
    expect(h.root.innerHTML).not.toMatch(/<(input|textarea|select)\b/);
    expect(h.calls.every((call) => call.method === "GET")).toBe(true);
  });
  it("clears stale records on a denied read instead of showing examples or zero records", async () => {
    const h = harness({
      "/v1/teams": () => new Response("{}", { status: 403 }),
    });
    h.state.orgTeams = [{ name: "stale-private-team", users: 99 }];
    h.settings.ensureLoaded();
    await vi.waitFor(() => expect(h.state.settingsLoaded).toBe(true));
    h.state.orgTab = "teams";
    h.paint();
    expect(h.root.textContent).toContain("Team directory unavailable");
    expect(h.root.textContent).not.toContain("stale-private-team");
    expect(h.root.textContent).not.toContain("0 stored teams returned");
  });
  it("displays the native session identity and leaves failed identity metadata unavailable", async () => {
    const h = harness({
      "/v1/operator-identity": () => new Response("{}", { status: 503 }),
    });
    h.state.operatorName = "edited-display-name";
    h.settings.ensureLoaded();
    await vi.waitFor(() => expect(h.state.settingsLoaded).toBe(true));
    h.state.orgTab = "health";
    h.paint();
    expect(h.root.textContent).toContain("Signed in as alice");
    expect(h.root.textContent).not.toContain("edited-display-name");
    h.state.orgTab = "identity";
    h.paint();
    expect(h.root.textContent).toContain("metadata is unavailable");
  });
  it("discards a snapshot if the observed native identity changes while reading", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    let first = true;
    let h: ReturnType<typeof harness>;
    h = harness({
      "/v1/teams": async () => {
        if (first) {
          first = false;
          await gate;
          return new Response(
            JSON.stringify({ teams: [{ name: "alice-private-team" }] }),
          );
        }
        return new Response(JSON.stringify({ teams: [{ name: "bob-team" }] }));
      },
    });
    h.settings.ensureLoaded();
    await vi.waitFor(() => expect(first).toBe(false));
    h.state.reviewOperatorActor = "bob";
    h.state.reviewOperatorIdentities = ["bob"];
    h.state.orgTab = "teams";
    h.paint();
    expect(h.root.textContent).not.toContain("alice-private-team");
    release();
    await vi.waitFor(() => expect(h.state.settingsLoading).toBe(false));
    expect(h.state.settingsLoaded).toBe(false);
    expect(h.state.orgTeams).toEqual([]);
    h.paint();
    await vi.waitFor(() => expect(h.state.settingsLoaded).toBe(true));
    h.paint();
    expect(h.root.textContent).toContain("bob-team");
    expect(h.root.textContent).not.toContain("alice-private-team");
  });
});
