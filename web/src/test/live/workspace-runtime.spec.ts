import { expect, test } from "@playwright/test";

// Native browser/Hub/governance transport with the existing recorded runtime.
// There is no process, preview health, container or isolation claim here.
if (process.env["CONDUIT_SIMULATED_WORKSPACE_RUNTIME"] !== "recorded")
  throw new Error(
    "Run simulate-console.sh --with-workspace-recorded for this fixture.",
  );

test("recorded workspace runtime actions require owner confirmation and retain native receipts", async ({
  page,
}, testInfo) => {
  await page.goto("./");
  const url = (path: string) => new URL(path, page.url()).toString();
  const read = async (path: string) => {
    const response = await page.request.get(url(path));
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  };
  const title = `Recorded runtime ${Date.now().toString(16)}`;
  const created = await page.request.post(url("/v1/dev-workspaces"), {
    data: {
      profileId: "live-recorded-runtime",
      environmentId: "dev-use2",
      title,
    },
  });
  expect(created.status()).toBe(201);
  const original = await created.json();
  expect(original).toMatchObject({
    owner: "operator",
    environmentId: "dev-use2",
    status: "stopped",
    proposalRevision: "rev-1",
  });
  const path = `/v1/dev-workspaces/${original.workspaceId}`;
  // proposer is also a bootstrap administrator. Resource authority does not
  // replace ownership for these runtime writes.
  for (const suffix of ["/runtime", "/runtime/stop", "/logs/clear"]) {
    const denied = await page.request.post(url(path + suffix), {
      headers: { "X-Platform-Subject": "proposer" },
      data: {},
    });
    expect(denied.status()).toBe(403);
    expect(await read(path)).toEqual(original);
  }
  const writes: { path: string; body: unknown }[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname.startsWith(path + "/")
    )
      writes.push({
        path: new URL(request.url()).pathname,
        body: request.postDataJSON(),
      });
  });
  await page.goto("./#s12-lifecycle");
  const studio = page.locator("[data-workspace-studio]");
  await studio
    .getByRole("button", { name: `${title} · stopped`, exact: true })
    .click();
  await studio
    .getByRole("button", { name: "Preview and logs", exact: true })
    .click();
  await expect(studio).toContainText("Returned runtime status: stopped");
  const receipts: unknown[] = [];
  for (const action of [
    {
      name: "start",
      label: "Stage preview start",
      suffix: "/runtime",
      status: "running",
    },
    {
      name: "stop",
      label: "Stage preview stop",
      suffix: "/runtime/stop",
      status: "stopped",
    },
    {
      name: "clear-logs",
      label: "Stage log clear",
      suffix: "/logs/clear",
      status: "stopped",
    },
  ]) {
    const count = writes.length;
    await studio
      .getByRole("button", { name: action.label, exact: true })
      .click();
    const form = studio.locator("[data-ws-confirm]");
    await expect(form).toContainText(
      `Review ${action.name} for ${original.workspaceId}`,
    );
    await expect(form).toContainText(original.proposalRevision);
    await expect(
      form.getByRole("button", {
        name: "Confirm workspace action",
        exact: true,
      }),
    ).toBeDisabled();
    expect(writes).toHaveLength(count);
    await form.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(form).toHaveCount(0);
    expect(writes).toHaveLength(count);
    await studio
      .getByRole("button", { name: action.label, exact: true })
      .click();
    await form.getByRole("checkbox").check();
    const response = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === path + action.suffix,
    );
    await form
      .getByRole("button", { name: "Confirm workspace action", exact: true })
      .click();
    const recorded = await response;
    expect(recorded.status()).toBe(action.name === "clear-logs" ? 204 : 200);
    if (action.name === "clear-logs") {
      expect(await recorded.text()).toBe("");
      await expect(studio).toContainText(
        "Log clear request acknowledged; other audit records are unchanged.",
      );
    } else {
      const receipt = await recorded.json();
      expect(receipt).toMatchObject({
        workspaceId: original.workspaceId,
        status: action.status,
        origin: "local-runtime",
      });
      if (action.name === "start")
        expect(receipt.proposalRevision).toBe(original.proposalRevision);
      receipts.push(receipt);
      await expect(studio).toContainText(
        `Returned preview status ${action.status} · origin local-runtime.`,
      );
    }
    await expect(form).toHaveCount(0);
    expect(writes).toEqual(
      ["/runtime", "/runtime/stop", "/logs/clear"]
        .slice(0, count + 1)
        .map((suffix) => ({ path: path + suffix, body: {} })),
    );
    const updated = await read(path);
    expect(updated.status).toBe(action.status);
    for (const field of [
      "workspaceId",
      "owner",
      "environmentId",
      "proposalRevision",
      "sourceDigest",
      "artifactDigest",
    ])
      expect(updated[field]).toEqual(original[field]);
    expect(await read(path + "/runtime")).toMatchObject({
      workspaceId: original.workspaceId,
      status: action.status,
      origin: "local-runtime",
    });
  }
  // This fixture starts with an empty buffer: qualify the 204 transport and
  // empty readback, not the erasure of real process output.
  const logs = await read(path + "/logs?limit=100");
  expect(logs).toMatchObject({
    lines: [],
    status: "stopped",
    origin: "local-runtime",
    valueTier: "CONDUIT_NATIVE",
  });
  await studio
    .getByRole("button", { name: "Stage log clear", exact: true })
    .click();
  const form = studio.locator("[data-ws-confirm]");
  await form.getByRole("checkbox").check();
  await form
    .getByRole("button", { name: "Confirm workspace action", exact: true })
    .click();
  await expect(studio).toContainText("This exact write was already attempted.");
  expect(writes).toHaveLength(3);
  const final = await read(path);
  expect(final.status).toBe("stopped");
  await testInfo.attach("recorded-workspace-runtime-receipts", {
    contentType: "application/json",
    body: Buffer.from(
      JSON.stringify(
        {
          fixture: "recorded runtime; no process execution",
          original,
          final,
          receipts,
          writes,
          logs,
        },
        null,
        2,
      ),
    ),
  });
});
