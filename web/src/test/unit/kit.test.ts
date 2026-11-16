import {
  areaObject,
  connectivityFromSpoke,
  diffRowsFromChanges,
  entityTitleMarkup,
  lineageFromHub,
  movePuck,
  objectHeader,
  sitesFromEnvironments,
  viewState,
  wavesFromRecords,
} from "../../console/operator/kit.js";
import { describe, expect, it } from "vite-plus/test";

describe("v3 kit records", () => {
  it("ignores a promotion that has no wave window", () => {
    expect(
      wavesFromRecords([
        { id: "promo-1", fromChannel: "DEV", toChannel: "STAGING" },
      ]),
    ).toEqual({ pucks: [], dependencies: [] });
  });

  it("keeps a recorded wave window and clamps a drag", () => {
    const waves = wavesFromRecords([
      {
        id: "wave-1",
        lane: "dev",
        label: "sample",
        startHourUtc: 2,
        durationHours: 2,
        windowStart: 0,
        windowEnd: 8,
        dependsOn: "wave-0",
      },
    ]);
    expect(waves.pucks[0].startHourUtc).toBe(2);
    expect(waves.dependencies).toEqual([["wave-0", "wave-1"]]);
    const moved = movePuck(waves.pucks, "wave-1", 20);
    expect(moved[0].startHourUtc).toBe(6);
  });

  it("does not invent connectivity for a blank spoke", () => {
    expect(connectivityFromSpoke("")).toBe("");
    expect(connectivityFromSpoke("offline")).toBe("Disconnected");
    const sites = sitesFromEnvironments([
      { environment: { id: "dev" }, spoke: { state: "" } },
    ]);
    expect(sites[0].connectivity).toBe("");
    expect(sites[0].health).toBe("not reported");
  });

  it("links only recorded channels", () => {
    const graph = lineageFromHub({
      promotions: [
        { fromChannel: "DEV", toChannel: "STAGING", status: "APPROVED" },
      ],
      packaging: [{ runId: "sample-app-1", phase: "review" }],
    });
    expect(graph.edges).toEqual([["channel:DEV", "channel:STAGING"]]);
    expect(graph.nodes.map((node) => node.id)).toContain("run:sample-app-1");
  });

  it("reads a stored field diff and leaves a missing diff empty", () => {
    expect(
      diffRowsFromChanges([
        {
          environment: "dev",
          author: { username: "local-developer" },
          changes: [{ diff: { memory: { before: "256Mi", after: "512Mi" } } }],
        },
      ]),
    ).toMatchObject([
      {
        path: "memory",
        before: "256Mi",
        after: "512Mi",
        actor: "local-developer",
      },
    ]);
    expect(diffRowsFromChanges([{ id: "crq", changes: [{}] }])).toEqual([]);
  });

  it("states an empty view without a fabricated cause", () => {
    const html = viewState({
      state: "empty",
      title: "Waves",
      detail: "No staged wave is recorded.",
    });
    expect(html).toContain("No staged wave is recorded.");
    expect(html).not.toContain("quorum");
  });
});

describe("packaging area selection", () => {
  const hub = {
    packaging: [
      { runId: "first", phase: "succeeded", outcome: "PASS" },
      { runId: "selected", phase: "review", outcome: "PASS" },
    ],
  };
  it("binds the shared area object to the selected visible run", () => {
    expect(
      areaObject(hub, "area-packaging", { packagingRunId: "selected" }),
    ).toMatchObject({ name: "selected", status: "review", tone: "warn" });
    expect(
      areaObject(hub, "area-packaging", { packagingRunId: "hidden" }),
    ).toBeNull();
    expect(areaObject(hub, "area-packaging")).toMatchObject({ name: "first" });
    expect(
      areaObject(
        {
          packaging: [
            {
              runId: "off",
              configured: false,
              phase: "succeeded",
              outcome: "PASS",
            },
          ],
        },
        "area-packaging",
      ),
    ).toMatchObject({ status: "blocked", tone: "warn" });
  });
});

describe("entityTitleMarkup & objectHeader", () => {
  it("renders blueprint v6 entity title structure", () => {
    const html = entityTitleMarkup({
      title: "Test Entity",
      subtitle: "Entity subtitle",
      headingTag: "h2",
      iconSvg: '<svg class="test-icon"></svg>',
      tagsHtml: '<span class="test-tag">TAG</span>',
      fill: true,
      wrap: true,
    });
    expect(html).toContain("bp6-entity-title");
    expect(html).toContain("bp6-entity-title-has-icon");
    expect(html).toContain("bp6-fill");
    expect(html).toContain("bp6-entity-title-wrap");
    expect(html).toContain(
      '<span class="bp6-entity-title-icon-container" aria-hidden="true"><svg class="test-icon"></svg></span>',
    );
    expect(html).toContain(
      '<h2 class="bp6-entity-title-title">Test Entity</h2>',
    );
    expect(html).toContain(
      '<span class="bp6-entity-title-tags"><span class="test-tag">TAG</span></span>',
    );
    expect(html).toContain(
      '<div class="bp6-entity-title-subtitle kit-muted">Entity subtitle</div>',
    );
  });

  it("omits icon container when iconSvg is not provided", () => {
    const html = entityTitleMarkup({ title: "No Icon" });
    expect(html).not.toContain("bp6-entity-title-has-icon");
    expect(html).not.toContain("bp6-entity-title-icon-container");
    expect(html).toContain('<div class="bp6-entity-title-title">No Icon</div>');
  });

  it("renders objectHeader using entityTitleMarkup and preserves header attributes", () => {
    const html = objectHeader({
      name: "prod-cluster",
      objectType: "Environment",
      status: "healthy",
      tone: "ok",
      owner: "infra-team",
      subtitle: "Region us-east-1",
      links: [{ screen: "s14-env-detail", label: "Details" }],
    });
    expect(html).toContain("data-object-header");
    expect(html).toContain('class="kit-card kit-bar-ok kit-header"');
    expect(html).toContain("bp6-entity-title");
    expect(html).toContain(
      '<h1 class="bp6-entity-title-title">prod-cluster</h1>',
    );
    expect(html).toContain(
      "Owner: <strong>infra-team</strong> · Region us-east-1",
    );
    expect(html).toContain("data-onclick=\"switchScreen('s14-env-detail')\"");
  });
});

describe("viewState 7 states & retry", () => {
  it("renders loading state with skeleton and aria-busy", () => {
    const html = viewState({ state: "loading", title: "Overview" });
    expect(html).toContain('data-state="loading"');
    expect(html).toContain('aria-busy="true"');
    expect(html).toContain("kit-bar-info");
    expect(html).toContain("Reading the Hub.");
    expect(html).toContain("kit-skeleton-stack");
    expect(html).toContain("kit-skeleton-line");
    expect(html).toContain("kit-skeleton-short");
  });

  it("renders empty state without tone bar", () => {
    const html = viewState({ state: "empty", title: "Overview" });
    expect(html).toContain('data-state="empty"');
    expect(html).toContain('class="kit-card kit-state"');
    expect(html).toContain("No records match this view.");
  });

  it("renders stale, partial, and partial-failure with warn tone", () => {
    const staleHtml = viewState({ state: "stale", title: "Overview" });
    expect(staleHtml).toContain("kit-bar-warn");
    expect(staleHtml).toContain(
      "Showing the last recorded Hub snapshot while refresh catches up.",
    );

    const partialHtml = viewState({ state: "partial", title: "Overview" });
    expect(partialHtml).toContain("kit-bar-warn");
    expect(partialHtml).toContain(
      "Some Hub endpoints did not respond; showing recorded rows only.",
    );

    const partialFailHtml = viewState({
      state: "partial-failure",
      title: "Overview",
    });
    expect(partialFailHtml).toContain("kit-bar-warn");
    expect(partialFailHtml).toContain(
      "Some Hub endpoints did not respond; showing recorded rows only.",
    );
  });

  it("renders error, disconnected, and denied with err tone", () => {
    const errHtml = viewState({ state: "error", title: "Overview" });
    expect(errHtml).toContain("kit-bar-err");
    expect(errHtml).toContain("The Hub did not answer. Nothing was changed.");

    const discHtml = viewState({ state: "disconnected", title: "Overview" });
    expect(discHtml).toContain("kit-bar-err");
    expect(discHtml).toContain("The Hub did not answer. Nothing was changed.");

    const deniedHtml = viewState({
      state: "denied",
      title: "Overview",
      operator: "Viewer",
    });
    expect(deniedHtml).toContain("kit-bar-err");
    expect(deniedHtml).toContain("Viewer does not have a role for this view.");
  });

  it("renders retryOnClick on error, disconnected, and stale", () => {
    const errWithRetry = viewState({
      state: "error",
      title: "Overview",
      retryOnClick: "refreshHub()",
    });
    expect(errWithRetry).toContain('data-onclick="refreshHub()"');
    expect(errWithRetry).toContain("Retry");

    const staleWithRetry = viewState({
      state: "stale",
      title: "Overview",
      retryOnClick: "refreshHub()",
    });
    expect(staleWithRetry).toContain('data-onclick="refreshHub()"');

    const emptyWithRetry = viewState({
      state: "empty",
      title: "Overview",
      retryOnClick: "refreshHub()",
    });
    expect(emptyWithRetry).not.toContain("refreshHub()");
  });
});
