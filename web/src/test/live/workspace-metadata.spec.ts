import { expect, test, type Page } from "@playwright/test";

type Workspace = {
  workspaceId: string;
  owner: string;
  environmentId: string;
  proposalRevision: string;
  sourceDigest?: string;
  status: string;
};
async function read(page: Page, path: string) {
  const response = await page.request.get(new URL(path, page.url()).toString());
  expect(response.ok(), await response.text()).toBe(true);
  return response.json();
}

test("workspace metadata upgrades and forks require reviewed owner coordinates on a real Hub", async ({
  page,
}) => {
  await page.goto("./");
  const title = `Live metadata ${Date.now().toString(16)}`;
  const created = await page.request.post(
    new URL("/v1/dev-workspaces", page.url()).toString(),
    {
      data: {
        profileId: "live-metadata-only",
        environmentId: "dev-use2",
        title,
      },
    },
  );
  expect(created.status()).toBe(201);
  const original = (await created.json()) as Workspace;
  expect(original).toMatchObject({
    owner: "operator",
    environmentId: "dev-use2",
    status: "stopped",
    proposalRevision: "rev-1",
  });
  const path = `/v1/dev-workspaces/${original.workspaceId}`;
  const writes: { path: string; body: unknown }[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (request.method() === "POST" && url.pathname.startsWith(path + "/")) {
      writes.push({ path: url.pathname, body: request.postDataJSON() });
    }
  });
  await page.goto("./#s12-lifecycle");
  await page
    .getByRole("button", { name: `${title} · stopped`, exact: true })
    .click();
  await page
    .getByRole("button", { name: "Stage metadata upgrade", exact: true })
    .click();
  const form = page.locator("[data-ws-confirm]");
  const expected = {
    sourceDigest: "sha256:" + "a".repeat(64),
    templateVersion: "reviewed-v2",
    toolVersions: "tools-v2",
  };
  await form
    .getByLabel("Reviewed source digest", { exact: true })
    .fill(expected.sourceDigest);
  await form
    .getByLabel("Template version", { exact: true })
    .fill(expected.templateVersion);
  await form.getByLabel("Tool versions", { exact: true }).fill("tools-v1");
  await expect(form).toContainText("does not prove files were upgraded");
  const confirm = form.getByRole("checkbox");
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
  expect(await read(page, path)).toEqual(original);
  expect(writes).toEqual([]);
  await confirm.check();
  await form
    .getByLabel("Tool versions", { exact: true })
    .fill(expected.toolVersions);
  await expect(confirm).not.toBeChecked();
  await expect(submit).toBeDisabled();
  expect(await read(page, path)).toEqual(original);
  await confirm.check();
  const upgradedResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === path + "/upgrade",
  );
  await submit.click();
  const upgradedHTTP = await upgradedResponse;
  expect(upgradedHTTP.status()).toBe(200);
  const upgraded = (await upgradedHTTP.json()) as Workspace;
  expect(upgraded).toMatchObject({
    ...expected,
    workspaceId: original.workspaceId,
    owner: original.owner,
    environmentId: original.environmentId,
    status: "stopped",
  });
  expect(upgraded.proposalRevision).not.toBe(original.proposalRevision);
  expect(await read(page, path)).toEqual(upgraded);
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "Workspace metadata updated; file upgrades and deployment are not inferred.",
  );
  expect(writes).toEqual([{ path: path + "/upgrade", body: expected }]);

  await page.getByRole("button", { name: "Stage fork", exact: true }).click();
  const childTitle = title + " child";
  await form.getByLabel("Title", { exact: true }).fill(childTitle);
  await expect(submit).toBeDisabled();
  await confirm.check();
  const forkedResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === path + "/fork",
  );
  await submit.click();
  const forkedHTTP = await forkedResponse;
  expect(forkedHTTP.status()).toBe(201);
  const child = (await forkedHTTP.json()) as Workspace;
  expect(child.workspaceId).not.toBe(original.workspaceId);
  expect(child).toMatchObject({
    ...expected,
    title: childTitle,
    parentId: original.workspaceId,
    owner: original.owner,
    environmentId: original.environmentId,
    proposalRevision: upgraded.proposalRevision,
    status: "stopped",
  });
  expect(await read(page, `/v1/dev-workspaces/${child.workspaceId}`)).toEqual(
    child,
  );
  expect(await read(page, path)).toEqual(upgraded);
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    `Fork recorded: ${child.workspaceId}.`,
  );
  expect(writes).toEqual([
    { path: path + "/upgrade", body: expected },
    { path: path + "/fork", body: { title: childTitle } },
  ]);
  const forbidden = await page.request.post(
    new URL(path + "/upgrade", page.url()).toString(),
    {
      headers: { "X-Platform-Subject": "workspace-ungranted" },
      data: { ...expected, sourceDigest: "sha256:" + "b".repeat(64) },
    },
  );
  expect(forbidden.status()).toBe(403);
  expect(await read(page, path)).toEqual(upgraded);
});
