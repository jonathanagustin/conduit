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

test("workspace review submission requires owner confirmation and opens only a separate human review", async ({
  page,
}) => {
  await page.goto("./");
  const title = `Live review metadata ${Date.now().toString(16)}`;
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
  const path = `/v1/dev-workspaces/${original.workspaceId}`;
  // These are explicitly reviewed synthetic metadata coordinates, not a claim
  // that a file tree was upgraded or an artifact was built/signed.
  const upgraded = await page.request.post(
    new URL(`${path}/upgrade`, page.url()).toString(),
    {
      data: {
        sourceDigest: "sha256:" + "b".repeat(64),
        templateVersion: "review-metadata-v1",
        toolVersions: "recorded-tools-v1",
      },
    },
  );
  expect(upgraded.status()).toBe(200);
  const workspace = (await read(page, path)) as Workspace;
  expect(workspace).toMatchObject({
    owner: "operator",
    sourceDigest: "sha256:" + "b".repeat(64),
    status: "stopped",
  });
  expect(workspace.proposalRevision).not.toBe(original.proposalRevision);
  const listChanges = async () =>
    (await read(page, "/v1/change-requests?limit=200")).changeRequests as {
      id: string;
    }[];
  const changeIDs = async () =>
    (await listChanges()).map((row) => row.id).sort();
  const planIDs = async () => {
    const ids: string[] = [];
    let cursor = "";
    const seen = new Set<string>();
    do {
      const listed = await read(
        page,
        `/v1/plans?limit=20&cursor=${encodeURIComponent(cursor)}`,
      );
      ids.push(...(listed.plans as { id: string }[]).map((row) => row.id));
      cursor = listed.nextCursor || "";
      if (cursor) {
        expect(seen.has(cursor), "Plan cursor must advance").toBe(false);
        seen.add(cursor);
        expect(seen.size).toBeLessThan(20);
      }
    } while (cursor);
    return ids.sort();
  };
  const beforeChanges = await changeIDs();
  const beforePlans = await planIDs();
  const body = {
    title: "Review recorded workspace metadata",
    description: "A separate human review; no approval or execution.",
    approve: false,
    reviewedRevision: workspace.proposalRevision,
    reviewedSourceDigest: workspace.sourceDigest,
  };
  const nonOwner = await page.request.post(
    new URL(`${path}/submit`, page.url()).toString(),
    {
      headers: { "X-Platform-Subject": "proposer" },
      data: body,
    },
  );
  expect(nonOwner.status()).toBe(403);
  const selfApproval = await page.request.post(
    new URL(`${path}/submit`, page.url()).toString(),
    { data: { ...body, approve: true } },
  );
  expect(selfApproval.status()).toBe(403);
  expect(await changeIDs()).toEqual(beforeChanges);
  expect(await planIDs()).toEqual(beforePlans);
  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === `${path}/submit`
    )
      writes.push(request.postDataJSON());
  });
  await page.goto("./#s12-lifecycle");
  await page
    .getByRole("button", { name: `${title} · stopped`, exact: true })
    .click();
  await page
    .getByRole("button", { name: "Stage separate review", exact: true })
    .click();
  const form = page.locator("[data-ws-confirm]");
  await expect(form).toContainText(workspace.workspaceId);
  await expect(form).toContainText(workspace.owner);
  await expect(form).toContainText(workspace.proposalRevision);
  await expect(form).toContainText(workspace.sourceDigest!);
  await form.getByLabel("Title", { exact: true }).fill(body.title);
  await form
    .getByLabel("Review description", { exact: true })
    .fill("Discarded description");
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
  expect(writes).toEqual([]);
  expect(await changeIDs()).toEqual(beforeChanges);
  await confirm.check();
  await form
    .getByLabel("Review description", { exact: true })
    .fill(body.description);
  await expect(confirm).not.toBeChecked();
  await expect(submit).toBeDisabled();
  expect(writes).toEqual([]);
  await confirm.check();
  const response = page.waitForResponse(
    (reply) =>
      reply.request().method() === "POST" &&
      new URL(reply.url()).pathname === `${path}/submit`,
  );
  await submit.click();
  const answer = await response;
  expect(answer.status()).toBe(202);
  const receipt = (await answer.json()) as {
    changeRequestId: string;
    status: string;
  };
  expect(receipt.status).toBe("PENDING_APPROVALS");
  expect(receipt.changeRequestId).toBeTruthy();
  expect(writes).toEqual([body]);
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    `Review request ${receipt.changeRequestId} · PENDING_APPROVALS`,
  );
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "No approval or Plan execution was granted.",
  );
  const request = await read(
    page,
    `/v1/change-requests/${receipt.changeRequestId}`,
  );
  expect(request).toMatchObject({
    id: receipt.changeRequestId,
    targetKind: "WORKSPACE",
    environment: workspace.environmentId,
    title: body.title,
    description: body.description,
    status: "PENDING_APPROVALS",
    author: { username: "operator" },
    requirementsSatisfied: false,
  });
  expect(request.targetId).toMatch(
    new RegExp(`^${workspace.workspaceId}@${workspace.proposalRevision}:`),
  );
  expect(request.changes[0].request.config).toEqual({
    workspaceId: workspace.workspaceId,
    revision: workspace.proposalRevision,
    sourceDigest: workspace.sourceDigest,
  });
  expect(request.requirements.length).toBeGreaterThan(0);
  expect(request.reviews ?? []).toEqual([]);
  expect(request.planId || "").toBe("");
  expect(await changeIDs()).toEqual(
    [...beforeChanges, receipt.changeRequestId].sort(),
  );
  expect(await planIDs()).toEqual(beforePlans);
  expect(await read(page, path)).toEqual(workspace);
  await page.reload();
  expect(
    (await read(page, `/v1/change-requests/${receipt.changeRequestId}`)).status,
  ).toBe("PENDING_APPROVALS");
});

test("workspace review refuses metadata changed after the final browser preflight", async ({
  page,
}, testInfo) => {
  await page.goto("./");
  const title = `Review race metadata ${Date.now().toString(16)}`;
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
  const path = `/v1/dev-workspaces/${original.workspaceId}`;
  const upgrade = async (digit: string, version: string) => {
    const reply = await page.request.post(
      new URL(`${path}/upgrade`, page.url()).toString(),
      {
        data: {
          sourceDigest: "sha256:" + digit.repeat(64),
          templateVersion: version,
          toolVersions: "recorded-tools-v1",
        },
      },
    );
    expect(reply.status()).toBe(200);
    return (await read(page, path)) as Workspace;
  };
  const reviewed = await upgrade("c", "reviewed-metadata-a");
  const before = (
    (await read(page, "/v1/change-requests?limit=200")).changeRequests as {
      id: string;
    }[]
  )
    .map((row) => row.id)
    .sort();
  await page.goto("./#s12-lifecycle");
  await page
    .getByRole("button", { name: `${title} · stopped`, exact: true })
    .click();
  await page
    .getByRole("button", { name: "Stage separate review", exact: true })
    .click();
  const form = page.locator("[data-ws-confirm]");
  await expect(form).toContainText(reviewed.proposalRevision);
  await expect(form).toContainText(reviewed.sourceDigest!);
  await form
    .getByLabel("Title", { exact: true })
    .fill("Review only displayed metadata A");
  await form
    .getByLabel("Review description", { exact: true })
    .fill("The confirmed revision and source must not change in transit.");
  await form.getByRole("checkbox").check();
  const preflight = page
    .waitForResponse(
      (reply) =>
        reply.request().method() === "GET" &&
        new URL(reply.url()).pathname === path,
    )
    .then((reply) => reply.json() as Promise<Workspace>);
  let changed: Workspace | undefined;
  let submitted: unknown;
  await page.route(`**${path}/submit`, async (route) => {
    submitted = route.request().postDataJSON();
    expect(await preflight).toMatchObject({
      proposalRevision: reviewed.proposalRevision,
      sourceDigest: reviewed.sourceDigest,
    });
    changed = await upgrade("d", "unreviewed-metadata-b");
    expect(changed.proposalRevision).not.toBe(reviewed.proposalRevision);
    // Continue the actual browser request to the actual Hub. No response is faked.
    await route.continue();
  });
  const responsePromise = page.waitForResponse(
    (reply) =>
      reply.request().method() === "POST" &&
      new URL(reply.url()).pathname === `${path}/submit`,
  );
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  const response = await responsePromise;
  const receipt = await response.json();
  const admitted = receipt.changeRequestId
    ? await read(page, `/v1/change-requests/${receipt.changeRequestId}`)
    : null;
  await testInfo.attach("workspace-submission-interleaving", {
    contentType: "application/json",
    body: Buffer.from(
      JSON.stringify(
        {
          reviewed,
          preflight: await preflight,
          changed,
          submitted,
          status: response.status(),
          receipt,
          admitted,
        },
        null,
        2,
      ),
    ),
  });
  expect(response.status()).toBe(409);
  await expect(page.locator("[data-workspace-studio]")).toContainText(
    "reviewed workspace metadata changed",
  );
  const after = (
    (await read(page, "/v1/change-requests?limit=200")).changeRequests as {
      id: string;
    }[]
  )
    .map((row) => row.id)
    .sort();
  expect(after).toEqual(before);
});
