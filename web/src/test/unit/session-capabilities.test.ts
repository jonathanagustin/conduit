// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vite-plus/test";

import { AREAS as MOUNTED_AREAS } from "../../console/operator/navigation.js";
import { HUB_APPLICATIONS } from "../../console/operator/hub-vocabulary.js";
import { escapeHtml as esc } from "../../console/operator/palette.js";
import {
  CAPABILITIES_LOADING,
  NAV_APPLICATIONS,
  blockedMarkup,
  capabilitiesFromSession,
  capabilitiesUnavailable,
  capabilityTableMarkup,
  createCapabilities,
  decideOperation,
  deriveNavigation,
  filterNavItems,
  grantNotice,
  navDecision,
  navNoticeMarkup,
  sessionPath,
} from "../../console/operator/session-capabilities.js";

const repo = (path: string) =>
  readFileSync(new URL(`../../../../${path}`, import.meta.url), "utf8");

/** The application ids the Hub's session handler actually returns. */
function hubVocabulary(): string[] {
  const go = repo("internal/controlplane/session.go");
  return [...go.matchAll(/\{ID: "([^"]+)", Enabled: /g)].map((m) => m[1]!);
}

// The areas and tabs the mapping names, shaped like the engine's AREAS.
const AREAS = {
  "area-command": {
    label: "Command Center",
    tabs: [
      { id: "command", label: "Overview" },
      { id: "tower", label: "Release Control Tower" },
    ],
  },
  "area-packaging": {
    label: "Packaging",
    tabs: [
      { id: "runs", label: "Runs" },
      { id: "bundle", label: "Bundle" },
    ],
  },
  "area-releases": {
    label: "Releases",
    tabs: [
      { id: "environments", label: "Environments" },
      { id: "plans", label: "Plans" },
      { id: "promotion", label: "Promotion" },
      { id: "waves", label: "Waves" },
    ],
  },
  "area-changes": {
    label: "Change requests",
    tabs: [{ id: "list", label: "Requests" }],
  },
  "area-intelligence": {
    label: "Intelligence",
    tabs: [{ id: "logic", label: "Logic" }],
  },
  "area-fleet": {
    label: "Fleet",
    tabs: [{ id: "environments", label: "Environments" }],
  },
};

/** What the Hub returns for an operator without resource:read at the root. */
const restricted = {
  actor: { username: "riley" },
  effective: { username: "riley" },
  applications: [
    { id: "change-requests", enabled: true },
    { id: "fleet", enabled: false },
    { id: "plans", enabled: true },
    { id: "promotions", enabled: false },
    { id: "teams", enabled: true },
    { id: "risk", enabled: false },
    { id: "export", enabled: false },
    { id: "import", enabled: false },
  ],
  globalOperations: [],
  resources: [
    { resourceRid: "product/payments", operations: ["resource:read"] },
  ],
};
const full = {
  ...restricted,
  applications: restricted.applications.map((app) => ({
    ...app,
    enabled: true,
  })),
  globalOperations: ["resource:read", "resource:operate"],
};

describe("application vocabulary", () => {
  it("is the list the Hub's session handler returns", () => {
    const hub = hubVocabulary();
    expect(hub.length).toBeGreaterThan(0);
    expect([...hub].sort()).toEqual([...HUB_APPLICATIONS].sort());
  });

  it("maps navigation only to application ids the Hub returns", () => {
    const hub = new Set(hubVocabulary());
    const mapped = Object.values(NAV_APPLICATIONS).flat();
    expect(mapped.length).toBeGreaterThan(0);
    expect(mapped.filter((id) => !hub.has(id))).toEqual([]);
  });

  it("maps only navigation entries the console has", () => {
    const settings = readFileSync(
      new URL("../../console/operator/settings.js", import.meta.url),
      "utf8",
    );
    for (const key of Object.keys(NAV_APPLICATIONS)) {
      const [area, tab] = key.split("/");
      if (!area!.startsWith("area-")) {
        expect(settings, key).toContain(`id: "${key}"`);
        continue;
      }
      const areaSpec = MOUNTED_AREAS[area as keyof typeof MOUNTED_AREAS];
      expect(areaSpec, key).toBeDefined();
      if (tab)
        expect(
          areaSpec.tabs.some((item) => item.id === tab),
          key,
        ).toBe(true);
    }
  });
});

describe("navigation from enabled applications", () => {
  it("hides a mapped area and tab whose application is off", () => {
    const capabilities = capabilitiesFromSession(restricted);
    const { areas, hidden } = deriveNavigation(AREAS, capabilities);
    expect(Object.keys(areas)).toEqual([
      "area-command",
      "area-packaging",
      "area-releases",
      "area-changes",
      "area-intelligence",
    ]);
    expect(areas["area-releases"]!.tabs.map((tab) => tab.id)).toEqual([
      "environments",
      "plans",
    ]);
    expect(areas["area-packaging"]!.tabs.map((tab) => tab.id)).toEqual([
      "runs",
    ]);
    expect(hidden.map((item) => item.id)).toEqual([
      "area-packaging/bundle",
      "area-releases/promotion",
      "area-releases/waves",
      "area-fleet",
    ]);
    expect(hidden.every((item) => item.state === "disabled")).toBe(true);
  });

  it("shows everything the Hub enabled", () => {
    const { areas, hidden } = deriveNavigation(
      AREAS,
      capabilitiesFromSession(full),
    );
    expect(Object.keys(areas)).toEqual(Object.keys(AREAS));
    expect(hidden).toEqual([]);
  });

  it("keeps an unmapped entry visible and closes a mapped one", () => {
    const none = capabilitiesFromSession({ ...restricted, applications: [] });
    expect(navDecision(none, "area-intelligence")).toMatchObject({
      visible: true,
      state: "unmapped",
    });
    expect(navDecision(none, "area-changes")).toMatchObject({
      visible: false,
      state: "disabled",
    });
  });

  it("filters rail items for areas and for mapped screens", () => {
    const items = [
      { id: "area-fleet" },
      { id: "area-changes" },
      { id: "adm02-org-control-panel" },
      { id: "s19-ontology" },
    ];
    const capabilities = capabilitiesFromSession({
      ...restricted,
      applications: restricted.applications.map((app) =>
        app.id === "teams" ? { ...app, enabled: false } : app,
      ),
    });
    expect(
      filterNavItems(items, AREAS, capabilities).map((item) => item.id),
    ).toEqual(["area-changes", "s19-ontology"]);
  });

  it("says how many entries a restricted session lacks", () => {
    const capabilities = capabilitiesFromSession(restricted);
    const html = navNoticeMarkup(
      capabilities,
      deriveNavigation(AREAS, capabilities).hidden,
      esc,
    );
    expect(html).toContain('data-capabilities="restricted"');
    expect(html).toContain("4 entries are not enabled for this session");
    expect(
      blockedMarkup(
        capabilities,
        "Fleet",
        navDecision(capabilities, "area-fleet"),
        esc,
      ),
    ).toContain("missing grant, not missing data");
  });
});

describe("a failed session read", () => {
  it.each([
    ["no answer", null],
    ["an answer without applications", { actor: { username: "riley" } }],
  ])("is unavailable for %s, never a default set", (_name, session) => {
    const capabilities = capabilitiesFromSession(session);
    expect(capabilities.status).toBe("unavailable");
    expect(capabilities.enabled).toEqual([]);
    const { areas, hidden } = deriveNavigation(AREAS, capabilities);
    // Mapped entries are withheld; unmapped ones are not hidden with them.
    expect(Object.keys(areas)).toEqual([
      "area-command",
      "area-packaging",
      "area-releases",
      "area-intelligence",
    ]);
    expect(hidden.length).toBeGreaterThan(0);
    expect(hidden.every((item) => item.state !== "disabled")).toBe(true);
    const html = navNoticeMarkup(capabilities, hidden, esc);
    expect(html).toContain('role="alert"');
    expect(html).toContain("Capabilities unavailable");
    expect(html).toContain("retryCapabilities()");
    expect(capabilityTableMarkup(capabilities, esc)).toContain(
      "does not substitute a default set",
    );
  });

  it("is distinguished from still loading", () => {
    expect(navNoticeMarkup(CAPABILITIES_LOADING, [], esc)).toContain(
      'data-capabilities="loading"',
    );
    expect(navDecision(CAPABILITIES_LOADING, "area-fleet").state).toBe(
      "loading",
    );
  });

  it("replaces a good answer when a later read fails", async () => {
    const state: Record<string, any> = { hub: { loading: true } };
    let fail = false;
    let renders = 0;
    let reads = 0;
    const capabilities = createCapabilities({
      state,
      render: () => void (renders += 1),
      read: async () => {
        reads += 1;
        if (fail) throw new Error("The Hub answered 503 to the session read.");
        return full;
      },
    });
    expect(state.capabilities.status).toBe("loading");
    capabilities.sync();
    await Promise.resolve();
    await Promise.resolve();
    expect(state.capabilities.status).toBe("ready");
    expect(renders).toBe(1);
    // The same Hub read model is not a new refresh cycle.
    capabilities.sync();
    await Promise.resolve();
    expect(reads).toBe(1);
    expect(renders).toBe(1);
    fail = true;
    state.hub = { loading: false };
    capabilities.sync();
    await Promise.resolve();
    await Promise.resolve();
    expect(state.capabilities.status).toBe("unavailable");
    expect(state.capabilities.reason).toContain("503");
    expect(state.capabilities.enabled).toEqual([]);
    expect(renders).toBe(2);
  });
});

describe("canPerform", () => {
  const capabilities = capabilitiesFromSession(restricted);

  it("answers from the operations the session reported", () => {
    expect(
      decideOperation(capabilities, "resource:read", "product/payments"),
    ).toEqual({ allowed: true, reason: "" });
    expect(
      decideOperation(capabilities, "resource:operate", "product/payments"),
    ).toEqual({
      allowed: false,
      reason:
        "Your session does not hold resource:operate on product/payments.",
    });
    expect(decideOperation(capabilities, "resource:read")).toMatchObject({
      allowed: false,
    });
    expect(
      decideOperation(capabilitiesFromSession(full), "resource:read"),
    ).toMatchObject({ allowed: true });
  });

  it("does not guess about a resource the session did not report", () => {
    expect(
      decideOperation(capabilities, "resource:read", "product/unknown").allowed,
    ).toBeNull();
    expect(
      decideOperation(
        capabilitiesUnavailable(""),
        "resource:read",
        "product/payments",
      ).allowed,
    ).toBeNull();
  });

  it("asks the Hub about an unreported resource on the next read", async () => {
    const paths: string[] = [];
    const state: Record<string, any> = { hub: {} };
    const capabilities = createCapabilities({
      state,
      render: () => {},
      read: async (path: string) => {
        paths.push(path);
        return restricted;
      },
    });
    capabilities.sync();
    await Promise.resolve();
    await Promise.resolve();
    expect(
      capabilities.canPerform("resource:read", "product/new").allowed,
    ).toBeNull();
    expect(paths).toEqual([
      "/v1/session",
      "/v1/session?resourceRid=product%2Fnew",
    ]);
  });
});

describe("sessionPath", () => {
  it("sends bounded, canonical, de-duplicated coordinates", () => {
    expect(sessionPath([])).toBe("/v1/session");
    expect(
      sessionPath(["b/2", "a/1", "b/2", "not a rid", "platform/root"]),
    ).toBe("/v1/session?resourceRid=a%2F1&resourceRid=b%2F2");
    const many = Array.from({ length: 40 }, (_, index) => `r/${index}`);
    expect(sessionPath(many).split("resourceRid=").length - 1).toBe(32);
  });
});

describe("no grants versus no data", () => {
  const hub = { loading: false, environments: [], plans: [{ id: "p" }] };

  it("names an empty index as filtered for a session without root read", () => {
    const notice = grantNotice(capabilitiesFromSession(restricted), hub, [
      "environments",
      "plans",
    ]);
    expect(notice).toContain("You have no grants here");
    expect(notice).toContain("no environments it is entitled to read");
    expect(notice).not.toContain("plans");
  });

  it("stays silent when the session holds root read, so empty means empty", () => {
    expect(
      grantNotice(capabilitiesFromSession(full), hub, ["environments"]),
    ).toBe("");
  });

  it("stays silent when it cannot know", () => {
    const restrictedState = capabilitiesFromSession(restricted);
    expect(
      grantNotice(capabilitiesUnavailable(""), hub, ["environments"]),
    ).toBe("");
    expect(
      grantNotice(restrictedState, { ...hub, loading: true }, ["environments"]),
    ).toBe("");
    // A failed read is not a filtered one.
    expect(
      grantNotice(
        restrictedState,
        { ...hub, note: "Hub did not return environments." },
        ["environments"],
      ),
    ).toBe("");
  });
});
