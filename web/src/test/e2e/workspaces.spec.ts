import { expect, test } from "@playwright/test";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("workspace studio drops stale reads and confirms real metadata coordinates", async ({
  page,
}) => {
  const a = {
    workspaceId: "workspace-a",
    title: "Workspace A",
    owner: "alice",
    environmentId: "east",
    status: "STOPPED",
    proposalRevision: "rev-a",
    sourceDigest: "sha256:a",
  };
  let b = {
    ...a,
    workspaceId: "workspace-b",
    title: "Workspace B",
    proposalRevision: "rev-b",
    sourceDigest: "sha256:b",
    templateVersion: "v1",
    toolVersions: "tools-v1",
  };
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: [{ runId: "unrelated-packaging-run", phase: "review" }],
          total: "1",
          pageSize: 1,
          limit: 20,
        },
      }),
  );
  const writes: { path: string; body: any }[] = [];
  let releaseA!: () => void;
  const gateA = new Promise<void>((resolve) => {
    releaseA = resolve;
  });
  let aStarted = false;
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    if (path === "/v1/operator-identity")
      return route.fulfill({ json: { enabled: false, providers: [] } });
    if (path === "/v1/packaging-runs")
      return route.fulfill({
        json: {
          items: [{ runId: "unrelated-packaging-run", phase: "review" }],
        },
      });
    if (path === "/v1/dev-workspaces")
      return route.fulfill({ json: { items: [a, b] } });
    if (path === "/v1/dev-workspaces/workspace-a/tree") {
      aStarted = true;
      await gateA;
      return route.fulfill({
        json: {
          entries: [{ path: "foreign-a.md", kind: "file", digest: "sha256:a" }],
        },
      });
    }
    if (path === "/v1/dev-workspaces/workspace-b/tree")
      return route.fulfill({
        json: {
          entries: [{ path: "b.md", kind: "file", digest: "sha256:file-b" }],
        },
      });
    if (path === "/v1/dev-workspaces/workspace-b/file")
      return route.fulfill({
        json: {
          path: "b.md",
          digest: "sha256:file-b",
          content: "<img src=x onerror=alert(1)>",
        },
      });
    if (
      path === "/v1/dev-workspaces/workspace-b" &&
      route.request().method() === "GET"
    )
      return route.fulfill({ json: b });
    if (
      path === "/v1/dev-workspaces/workspace-b/runtime" &&
      route.request().method() === "GET"
    )
      return route.fulfill({
        json: {
          workspaceId: b.workspaceId,
          owner: "alice",
          status: b.status,
          origin: "recorded",
        },
      });
    if (path === "/v1/dev-workspaces/workspace-b/logs")
      return route.fulfill({
        json: { lines: [], origin: "recorded", status: b.status },
      });
    if (
      [
        "/v1/dev-workspaces/workspace-b/runtime",
        "/v1/dev-workspaces/workspace-b/runtime/stop",
      ].includes(path) &&
      route.request().method() === "POST"
    ) {
      writes.push({ path, body: route.request().postDataJSON() });
      b = { ...b, status: path.endsWith("/stop") ? "STOPPED" : "RUNNING" };
      return route.fulfill({
        json: {
          workspaceId: b.workspaceId,
          owner: "alice",
          status: b.status,
          origin: "recorded",
        },
      });
    }
    if (path === "/v1/dev-workspaces/workspace-b/upgrade") {
      const body = route.request().postDataJSON();
      writes.push({ path, body });
      b = { ...b, ...body, proposalRevision: "rev-upgraded" };
      return route.fulfill({ json: b });
    }
    if (path === "/v1/dev-workspaces/workspace-b/submit") {
      const body = route.request().postDataJSON();
      writes.push({ path, body });
      return route.fulfill({
        status: 202,
        json: {
          changeRequestId: "separate-workspace-review",
          status: "PENDING_APPROVALS",
        },
      });
    }
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await hydrate(page);
  await page.goto("/console/#s09-source");
  await page
    .getByRole("button", { name: "Workspace A · STOPPED", exact: true })
    .click();
  await expect.poll(() => aStarted).toBe(true);
  await page
    .getByRole("button", { name: "Workspace B · STOPPED", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(
    "workspace-b",
  );
  await expect(page.locator("[data-area-object-header]")).toContainText(
    "workspace-b",
  );
  await expect(page.locator("[data-area-object-header]")).not.toContainText(
    "unrelated-packaging-run",
  );
  await expect(page.locator("[data-workspace-studio]")).toContainText("b.md");
  const staleTreeResponse = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname ===
      "/v1/dev-workspaces/workspace-a/tree",
  );
  releaseA();
  await (await staleTreeResponse).finished();
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(
    page.getByRole("button", { name: "b.md · file", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "b.md · file", exact: true }).click();
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "<img src=x",
  );
  await expect(page.locator("[data-workspace-studio] img")).toHaveCount(0);
  await expect(page.locator("[data-workspace-studio]")).not.toContainText(
    "foreign-a.md",
  );
  await page
    .getByRole("button", { name: "Review and lifecycle", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Stage metadata upgrade", exact: true })
    .click();
  expect(writes).toEqual([]);
  const form = page.locator("[data-ws-confirm]");
  const digest = "sha256:" + "c".repeat(64);
  await form.getByLabel("Reviewed source digest").fill(digest);
  await form.getByLabel("Template version").fill("v2");
  await form.getByLabel("Tool versions").fill("tools-v2");
  await form.getByRole("checkbox").check();
  await form.getByLabel("Template version").fill("v3");
  await expect(
    form.getByRole("button", { name: "Confirm workspace action", exact: true }),
  ).toBeDisabled();
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(digest);
  expect(writes[0].body).toEqual({
    sourceDigest: digest,
    templateVersion: "v3",
    toolVersions: "tools-v2",
  });
  await expect(page.locator("[data-workspace-studio]")).not.toContainText(
    "<img src=x",
  );
  await page
    .getByRole("button", { name: "Stage separate review", exact: true })
    .click();
  await form
    .getByLabel("Title", { exact: true })
    .fill("Review upgraded workspace");
  await form.getByLabel("Review description").fill("Independent human review");
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "separate-workspace-review",
  );
  expect(writes[1].body).toEqual({
    title: "Review upgraded workspace",
    description: "Independent human review",
    approve: false,
    reviewedRevision: "rev-upgraded",
    reviewedSourceDigest: digest,
  });
  expect(writes).toHaveLength(2);
  await page
    .getByRole("button", { name: "Preview and logs", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Stage preview start", exact: true })
    .click();
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(
    "RUNNING",
  );
  await page
    .getByRole("button", { name: "Stage preview stop", exact: true })
    .click();
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(page.locator("[data-workspace-detail]")).toContainText(
    "STOPPED",
  );
  expect(writes.map((write) => write.path)).toEqual([
    "/v1/dev-workspaces/workspace-b/upgrade",
    "/v1/dev-workspaces/workspace-b/submit",
    "/v1/dev-workspaces/workspace-b/runtime",
    "/v1/dev-workspaces/workspace-b/runtime/stop",
  ]);
});

test("workspace history pins recorded commits and rejects late selection and failed reads", async ({
  page,
}, testInfo) => {
  const a = {
    workspaceId: "history-a",
    title: "History A",
    owner: "alice",
    environmentId: "east",
    status: "STOPPED",
    proposalRevision: "rev-a",
    sourceDigest: "sha256:a",
    branch: "workspace/alice/a",
    headCommit: "a".repeat(40),
  };
  let b = {
    ...a,
    workspaceId: "history-b",
    title: "History B",
    proposalRevision: "rev-b",
    sourceDigest: "sha256:b",
    branch: "workspace/alice/b",
    headCommit: "b".repeat(40),
  };
  let releaseA!: () => void;
  const gateA = new Promise<void>((resolve) => {
    releaseA = resolve;
  });
  let aStarted = false,
    refused = false;
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url()),
      path = url.pathname;
    if (
      path.startsWith("/v1/dev-workspaces") &&
      route.request().method() !== "GET"
    )
      writes.push(path);
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    if (path === "/v1/operator-identity")
      return route.fulfill({ json: { enabled: false, providers: [] } });
    if (path === "/v1/dev-workspaces")
      return route.fulfill({ json: { items: [a, b] } });
    if (path.endsWith("/tree")) return route.fulfill({ json: { entries: [] } });
    if (path.endsWith("/history")) {
      const item = path.includes("history-a/") ? a : b;
      expect(url.searchParams.get("revision")).toBe(item.proposalRevision);
      expect(url.searchParams.get("head")).toBe(item.headCommit);
      if (item.workspaceId === a.workspaceId) {
        aStarted = true;
        await gateA;
      }
      if (refused)
        return route.fulfill({
          status: 503,
          json: { error: "history unavailable" },
        });
      return route.fulfill({
        json: {
          ...item,
          current: true,
          valueTier: "CONDUIT_NATIVE",
          truncated: true,
          entries: [
            {
              commit: item.headCommit,
              committedAt: "2026-11-18T12:00:00Z",
              subject:
                item.workspaceId === a.workspaceId
                  ? "foreign late A"
                  : "fix: reviewed <script>literal</script>",
            },
          ],
        },
      });
    }
    return route.fulfill({ json: {} });
  });
  await hydrate(page);
  await page.goto("/console/#s09-source");
  await page
    .getByRole("button", { name: "History A · STOPPED", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Commit history", exact: true })
    .click();
  await expect.poll(() => aStarted).toBe(true);
  await page
    .getByRole("button", { name: "History B · STOPPED", exact: true })
    .click();
  const toggle = page.getByRole("button", {
    name: "Commit history",
    exact: true,
  });
  await toggle.click();
  await toggle.click();
  const commits = page.locator("[data-ws-commits]");
  await expect(commits).toContainText(b.headCommit);
  await expect(commits).toContainText("fix: reviewed <script>literal</script>");
  await expect(commits.locator("script")).toHaveCount(0);
  await expect(commits.locator("..")).toContainText(
    "more commits were omitted",
  );
  await testInfo.attach("recorded-workspace-history", {
    body: await commits.locator("..").screenshot(),
    contentType: "image/png",
  });
  const stale = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname ===
      "/v1/dev-workspaces/history-a/history",
  );
  releaseA();
  await (await stale).finished();
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(commits).not.toContainText("foreign late A");
  await expect(commits).toContainText(b.headCommit);
  b = { ...b, headCommit: "c".repeat(40) };
  refused = true;
  await page
    .getByRole("button", { name: "Refresh workspaces", exact: true })
    .click();
  await expect(commits).toHaveCount(0);
  await toggle.click();
  await toggle.click();
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "history unavailable",
  );
  await expect(commits).toHaveCount(0);
  expect(writes).toEqual([]);
});
