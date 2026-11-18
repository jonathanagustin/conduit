import { areaObject } from "../../console/operator/kit.js";
import { describe, it, expect } from "vite-plus/test";
import {
  WORKSPACE_ISOLATION_NOTICE,
  WORKSPACE_HOST_PREVIEW_NOTICE,
  workspaceScope,
  currentWorkspaceDiff,
  currentWorkspaceHistory,
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
  it("states container isolation and the host preview limit", () => {
    expect(WORKSPACE_ISOLATION_NOTICE).toBe(
      "container isolation now, stronger isolation planned",
    );
    expect(WORKSPACE_HOST_PREVIEW_NOTICE).toBe(
      "A local process preview runs on the host and is not that boundary.",
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

describe("workspace commit history", () => {
  const item = {
    ...row,
    branch: "workspace/alice/a",
    headCommit: "a".repeat(40),
  };
  const history = {
    ...item,
    current: true,
    valueTier: "CONDUIT_NATIVE",
    truncated: false,
    entries: [
      {
        commit: item.headCommit,
        committedAt: "2026-11-18T12:00:00Z",
        subject: "fix: recorded <literal>",
      },
    ],
  };
  it("requires exact head, branch and source context with bounded real commit metadata", () => {
    expect(currentWorkspaceHistory(history, item)).toBe(true);
    for (const changed of [
      { current: false },
      { valueTier: "unknown" },
      { headCommit: "b".repeat(40) },
      {
        headCommit: "b".repeat(40),
        entries: [{ ...history.entries[0], commit: "b".repeat(40) }],
      },
      { branch: "other" },
      { proposalRevision: "old" },
      { sourceDigest: "changed" },
      { workspaceId: "other" },
      { entries: [] },
      { entries: [{ ...history.entries[0], commit: "HEAD" }] },
      { entries: [{ ...history.entries[0], committedAt: "invalid" }] },
      { entries: Array.from({ length: 21 }, () => history.entries[0]) },
    ])
      expect(currentWorkspaceHistory({ ...history, ...changed }, item)).toBe(
        false,
      );
  });
  it("invalidates an earlier scope when only the recorded branch or head changes", () => {
    expect(
      workspaceScope({ ...item, headCommit: "b".repeat(40) }, "alice"),
    ).not.toBe(workspaceScope(item, "alice"));
    expect(workspaceScope({ ...item, branch: "other" }, "alice")).not.toBe(
      workspaceScope(item, "alice"),
    );
  });
});
