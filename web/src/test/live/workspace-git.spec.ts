import { expect, test, type Page } from "@playwright/test";

type Workspace = {
  workspaceId: string;
  branch: string;
  headCommit: string;
  proposalRevision: string;
  sourceDigest: string;
};
async function read(page: Page, path: string) {
  const response = await page.request.get(new URL(path, page.url()).toString());
  expect(response.ok(), await response.text()).toBe(true);
  return response.json();
}

test("Git workspace history and confirmed fork retain authoritative branch and source identity on a real Hub", async ({
  page,
}, testInfo) => {
  await page.goto("./");
  const title = `Live Git ${Date.now().toString(16)}`;
  const created = await page.request.post(
    new URL("/v1/dev-workspaces", page.url()).toString(),
    {
      data: { profileId: "live-local-git", environmentId: "dev-use2", title },
    },
  );
  expect(created.status()).toBe(201);
  const receipt = await created.json();
  const path = `/v1/dev-workspaces/${receipt.workspaceId}`;
  // Creation currently returns pre-adapter metadata. Assert the authoritative
  // persisted Git coordinates explicitly rather than fabricating them locally.
  const parent = (await read(page, path)) as Workspace;
  expect(parent).toMatchObject({
    workspaceId: receipt.workspaceId,
    owner: "operator",
    environmentId: "dev-use2",
    status: "stopped",
  });
  expect(parent.branch).toBe(`workspace/operator/${parent.workspaceId}`);
  expect(parent.headCommit).toMatch(/^[a-f0-9]{40}$/);
  expect(parent.proposalRevision).toBe(parent.headCommit);
  expect(parent.sourceDigest).toBe(`git:${parent.headCommit}`);
  const historyPath = (workspace: Workspace) =>
    `/v1/dev-workspaces/${workspace.workspaceId}/history?${new URLSearchParams({ revision: workspace.proposalRevision, head: workspace.headCommit })}`;
  const before = await read(page, historyPath(parent));
  expect(before).toMatchObject({
    workspaceId: parent.workspaceId,
    branch: parent.branch,
    headCommit: parent.headCommit,
    proposalRevision: parent.proposalRevision,
    sourceDigest: parent.sourceDigest,
    current: true,
    valueTier: "CONDUIT_NATIVE",
    truncated: false,
  });
  expect(before.entries).toHaveLength(1);
  expect(before.entries[0]).toMatchObject({
    commit: parent.headCommit,
    subject: "chore(workspace): initialize branch",
  });
  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === path + "/fork"
    )
      writes.push(request.postDataJSON());
  });
  await page.goto("./#s09-source");
  await page
    .getByRole("button", { name: `${title} · stopped`, exact: true })
    .click();
  await page
    .getByRole("button", { name: "Commit history", exact: true })
    .click();
  const commits = page.locator("[data-ws-commits]");
  await expect(commits.locator("li")).toHaveCount(1);
  await expect(commits).toContainText(parent.headCommit);
  await expect(commits).toContainText(before.entries[0].subject);
  await expect(commits.locator("..")).toContainText(parent.branch);
  await page
    .getByRole("button", { name: "Review and lifecycle", exact: true })
    .click();
  await page.getByRole("button", { name: "Stage fork", exact: true }).click();
  const form = page.locator("[data-ws-confirm]");
  const childTitle = title + " child";
  await form.getByLabel("Title", { exact: true }).fill(childTitle);
  const submit = form.getByRole("button", {
    name: "Confirm workspace action",
    exact: true,
  });
  await expect(submit).toBeDisabled();
  await form.evaluate((node) =>
    node.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    ),
  );
  expect(writes).toEqual([]);
  expect(await read(page, path)).toEqual(parent);
  await form.getByRole("checkbox").check();
  const response = page.waitForResponse(
    (row) =>
      row.request().method() === "POST" &&
      new URL(row.url()).pathname === path + "/fork",
  );
  await submit.click();
  const forked = await response;
  expect(forked.status()).toBe(201);
  const child = (await forked.json()) as Workspace;
  expect(child.workspaceId).not.toBe(parent.workspaceId);
  expect(child.branch).toBe(`workspace/operator/${child.workspaceId}`);
  expect(child.branch).not.toBe(parent.branch);
  expect(child).toMatchObject({
    parentId: parent.workspaceId,
    title: childTitle,
    owner: "operator",
    environmentId: "dev-use2",
    headCommit: parent.headCommit,
    sourceDigest: parent.sourceDigest,
    proposalRevision: parent.proposalRevision,
    status: "stopped",
  });
  expect(await read(page, `/v1/dev-workspaces/${child.workspaceId}`)).toEqual(
    child,
  );
  expect(writes).toEqual([{ title: childTitle }]);
  const childHistory = await read(page, historyPath(child));
  expect(childHistory).toMatchObject({
    workspaceId: child.workspaceId,
    branch: child.branch,
    headCommit: parent.headCommit,
    current: true,
  });
  expect(childHistory.entries).toEqual(before.entries);
  expect(await read(page, path)).toEqual(parent);
  expect(await read(page, historyPath(parent))).toEqual(before);
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    `Fork recorded: ${child.workspaceId}.`,
  );
  await page.reload();
  await page
    .getByRole("button", { name: `${childTitle} · stopped`, exact: true })
    .click();
  const toggle = page.getByRole("button", {
    name: "Commit history",
    exact: true,
  });
  if ((await toggle.getAttribute("aria-expanded")) !== "true")
    await toggle.click();
  await expect(commits).toContainText(child.headCommit);
  await expect(commits.locator("..")).toContainText(child.branch);
  await expect(commits.locator("..")).not.toContainText(parent.branch);
  await testInfo.attach("real-git-workspace-fork-history", {
    body: await commits.locator("..").screenshot(),
    contentType: "image/png",
  });
});
