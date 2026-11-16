import { areaObject } from "../../console/operator/kit.js";
import { describe, it, expect } from "vite-plus/test";
import {
  WORKSPACE_ISOLATION_NOTICE,
  workspaceScope,
  currentWorkspaceDiff,
  safeWorkspaceDownload,
} from "../../console/operator/dev-workspaces.js";
const row = {
  workspaceId: "workspace-a",
  owner: "alice",
  environmentId: "east",
  proposalRevision: "rev-a",
  sourceDigest: "sha256:a",
};
describe("workspace studio binding", () => {
  it("states the local preview isolation limit", () => {
    expect(WORKSPACE_ISOLATION_NOTICE).toBe(
      "Workspace isolation must be configured; local previews run on the host.",
    );
  });
  it("binds drafts to native identity, owner and immutable source coordinates", () => {
    const scope = workspaceScope(row, "alice");
    expect(
      workspaceScope({ ...row, proposalRevision: "rev-b" }, "alice"),
    ).not.toBe(scope);
    expect(
      workspaceScope({ ...row, sourceDigest: "sha256:b" }, "alice"),
    ).not.toBe(scope);
    expect(workspaceScope(row, "bob")).not.toBe(scope);
    expect(workspaceScope({ ...row, status: "RUNNING" }, "alice")).not.toBe(
      scope,
    );
  });
  it("requires a current diff with matching workspace, revision and source", () => {
    const diff = { ...row, current: true };
    expect(currentWorkspaceDiff(diff, row)).toBe(true);
    for (const changed of [
      { current: false },
      { workspaceId: "workspace-b" },
      { proposalRevision: "rev-b" },
      { sourceDigest: "sha256:b" },
    ])
      expect(currentWorkspaceDiff({ ...diff, ...changed }, row)).toBe(false);
  });
  it("does not follow untrusted file download destinations", () => {
    expect(safeWorkspaceDownload("javascript:alert(1)", row.workspaceId)).toBe(
      "",
    );
    expect(
      safeWorkspaceDownload(
        "https://other.example/v1/dev-workspaces/workspace-a/file",
        row.workspaceId,
      ),
    ).toBe("");
    expect(
      safeWorkspaceDownload(
        "/v1/dev-workspaces/workspace-b/file",
        row.workspaceId,
      ),
    ).toBe("");
    expect(
      safeWorkspaceDownload(
        "/v1/dev-workspaces/workspace-a/file?path=README.md",
        row.workspaceId,
      ),
    ).toBe("/v1/dev-workspaces/workspace-a/file?path=README.md");
  });
});

it("keeps the studio header on the selected workspace instead of a packaging run", () => {
  const hub = { packaging: [{ runId: "unrelated-run" }] };
  expect(
    areaObject(hub, "area-packaging", { workspaceView: true, workspace: row }),
  ).toMatchObject({
    name: "workspace-a",
    objectType: "Workspace",
    owner: "alice",
  });
  expect(
    areaObject(hub, "area-packaging", { workspaceView: true, workspace: null }),
  ).toBeNull();
});
