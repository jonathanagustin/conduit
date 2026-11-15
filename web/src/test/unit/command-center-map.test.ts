// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  LOCATION_LABELS,
  recordedSites,
} from "../../console/operator/command-center-map.js";
import {
  activeWorkspaceProfiles,
  governanceOpportunities,
  overviewMarkup,
} from "../../console/operator/command-center.js";
import { appendAssistantDraft } from "../../console/assistant-draft";

const environment = (id: string, latitude?: string, longitude?: string) => ({
  environment: {
    id,
    labels: {
      [LOCATION_LABELS.latitude]: latitude,
      [LOCATION_LABELS.longitude]: longitude,
    },
  },
  spoke: { state: "live" },
});
const hub = {
  environments: [
    environment("recorded-east", "0", "0"),
    environment("recorded-west", "35", "-105"),
    environment("unmapped"),
  ],
  installations: [
    { environment: "recorded-east", observedVersion: "1", desiredVersion: "2" },
    { environment: "unmapped", observedVersion: "1" },
  ],
  changes: [
    { id: "cr-1", title: "Review capacity", status: "PENDING_APPROVALS" },
  ],
  plans: [],
  packaging: [],
  audit: [{ sequence: 1, actor: "operator", action: "plan:approve" }],
  devWorkspaces: [
    { workspaceId: "w-1", profileId: "packaging", status: "running" },
    { workspaceId: "w-2", profileId: "packaging", status: "starting" },
    { workspaceId: "w-3", profileId: "idle", status: "stopped" },
  ],
};

describe("recorded Command Center data", () => {
  it("plots only explicit valid coordinates and retains genuine zero", () => {
    expect(recordedSites(hub.environments).map((x) => x.location)).toEqual([
      [0, 0],
      [-105, 35],
      null,
    ]);
    for (const value of ["", " ", "not-a-coordinate", "Infinity", "0x10", "91"])
      expect(
        recordedSites([environment("invalid", value, "0")])[0]?.location,
      ).toBeNull();
    expect(
      recordedSites([environment("invalid", "0", "181")])[0]?.location,
    ).toBeNull();
  });
  it("groups only recorded active profiles and distinguishes missing data", () => {
    expect(activeWorkspaceProfiles(hub)).toEqual({
      available: true,
      groups: [["packaging", 2]],
      unknown: 0,
    });
    expect(activeWorkspaceProfiles({ devWorkspaces: [] }).available).toBe(true);
    expect(activeWorkspaceProfiles({}).available).toBe(false);
    expect(
      activeWorkspaceProfiles({ devWorkspaces: [{ status: "unrecognized" }] })
        .unknown,
    ).toBe(1);
  });
  it("does not infer drift when the desired version is unknown", () => {
    expect(
      governanceOpportunities(hub).map((x) => [x.objectType, x.id]),
    ).toEqual([
      ["ChangeRequest", "cr-1"],
      ["Environment", "recorded-east"],
    ]);
    expect(
      governanceOpportunities({
        ...hub,
        changeNote: "unavailable",
        installationNote: "unavailable",
      }),
    ).toEqual([]);
  });
  it("labels unmeasured links, simulation entry and recorded audit counts honestly", () => {
    const html = overviewMarkup(hub);
    expect(html).toContain("3 recorded");
    expect(html).toContain("2 with declared coordinates");
    expect(html).toContain("link endpoints are unavailable");
    expect(html).toContain("Control tower · Example");
    expect(html).toContain("1 loaded");
    expect(html).not.toContain("LIVE OPERATIONS");
  });
  it("escapes record content and never interpolates it as a handler", () => {
    const id = `env'\"><script>alert(1)</script>`;
    const html = overviewMarkup({
      ...hub,
      environments: [environment(id, "0", "0")],
    });
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
  });
});

describe("assistant draft handoff", () => {
  it("preserves existing work, appends once, and rejects invalid or oversized context", () => {
    expect(
      appendAssistantDraft("My existing draft", "Recorded environment east"),
    ).toBe("My existing draft\n\nRecorded environment east");
    expect(
      appendAssistantDraft(
        "My existing draft\n\nRecorded environment east",
        "Recorded environment east",
      ),
    ).toBe("My existing draft\n\nRecorded environment east");
    expect(appendAssistantDraft("keep", null)).toBeNull();
    expect(appendAssistantDraft("keep", " ")).toBeNull();
    expect(appendAssistantDraft("x".repeat(4000), "more")).toBeNull();
  });
});
