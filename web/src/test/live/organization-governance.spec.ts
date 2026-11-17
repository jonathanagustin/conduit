import { expect, test, type Page } from "@playwright/test";

// Synthetic native records on a real Hub; no route or response is stubbed.
// Run against an isolated Hub with operator as a bootstrap administrator.
const stamp = Date.now().toString(16);
const policyID = `console-policy-${stamp}`;
const team = `console-reviewers-${stamp}`;
const resource = `environment/console-${stamp}`;
const variables = {
  text: { type: "STRING", string: "preserved text" },
  colors: { type: "STRING_LIST", list: ["red\nblue", "", "green"] },
};

async function read(page: Page, path: string) {
  const response = await page.request.get(new URL(path, page.url()).toString());
  expect(response.ok()).toBe(true);
  return response.json();
}
async function open(page: Page, tab: string) {
  await page.goto("./#adm02-org-control-panel");
  await page.getByRole("radio", { name: tab, exact: true }).click();
}

test("Organization confirms policy and grant edits, preserves revisions and shows a real restricted refusal", async ({
  page,
  browser,
  baseURL,
}) => {
  await page.goto("./");
  for (const [path, method, data] of [
    [
      `/v1/teams/${team}`,
      "PUT",
      {
        displayName: "Console reviewers",
        memberUsers: ["console-reviewer"],
        memberGroups: [],
        adminUsers: ["operator"],
        adminGroups: [],
      },
    ],
    [
      `/v1/approval-policies/${policyID}`,
      "PUT",
      {
        displayName: "Console review",
        expression: "no_op()",
        enabled: true,
        variables,
      },
    ],
    [
      "/v1/resource-grants",
      "POST",
      {
        resourceRid: resource,
        team,
        relationship: "CO_OWNER",
        roles: ["environment:viewer"],
        operations: ["resource:read"],
      },
    ],
  ] as const) {
    const response = await page.request.fetch(
      new URL(path, page.url()).toString(),
      { method, data },
    );
    expect(response.ok(), await response.text()).toBe(true);
  }
  await open(page, "Approval policies");
  await page
    .getByRole("button", { name: "Edit Console review", exact: true })
    .click();
  const editor = page.locator("[data-governance-editor]");
  await editor
    .getByLabel("Policy name", { exact: true })
    .fill("Reviewed console policy");
  await editor.getByLabel("Enabled", { exact: true }).uncheck();
  await editor
    .getByRole("button", { name: "Review changes", exact: true })
    .click();
  let stored = (
    await read(page, "/v1/approval-policies")
  ).approvalPolicies.find((row: any) => row.id === policyID);
  expect(stored.revision).toBe(1);
  await editor.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(
    (await read(page, "/v1/approval-policies")).approvalPolicies.find(
      (row: any) => row.id === policyID,
    ).revision,
  ).toBe(1);
  await editor
    .getByRole("button", { name: "Review changes", exact: true })
    .click();
  await editor
    .getByRole("button", { name: "Save policy", exact: true })
    .click();
  await expect(editor.locator("[data-governance-note]")).toContainText(
    "revision 2",
  );
  stored = (await read(page, "/v1/approval-policies")).approvalPolicies.find(
    (row: any) => row.id === policyID,
  );
  expect(stored).toMatchObject({
    displayName: "Reviewed console policy",
    enabled: false,
    variables,
    revision: 2,
  });

  // A concurrent writer changes the authoritative revision after this editor loaded.
  const concurrent = await page.request.put(
    new URL(`/v1/approval-policies/${policyID}`, page.url()).toString(),
    { data: { ...stored, displayName: "Concurrent writer" } },
  );
  expect(concurrent.ok()).toBe(true);
  await editor.getByLabel("Policy name", { exact: true }).fill("Stale draft");
  await editor
    .getByRole("button", { name: "Review changes", exact: true })
    .click();
  await editor
    .getByRole("button", { name: "Save policy", exact: true })
    .click();
  await expect(editor.locator("[data-governance-note]")).toContainText(
    "record changed on the Hub",
  );
  await expect(editor.getByLabel("Policy name", { exact: true })).toHaveValue(
    "Stale draft",
  );
  expect(
    (await read(page, "/v1/approval-policies")).approvalPolicies.find(
      (row: any) => row.id === policyID,
    ),
  ).toMatchObject({ displayName: "Concurrent writer", revision: 3 });
  await editor
    .getByRole("button", { name: "Reload and discard draft", exact: true })
    .click();
  await expect(editor.getByLabel("Policy name", { exact: true })).toHaveValue(
    "Concurrent writer",
  );
  await editor
    .getByRole("button", { name: "Close editor", exact: true })
    .click();

  await page
    .getByRole("radio", { name: "Teams and roles", exact: true })
    .click();
  await page
    .getByRole("button", { name: `Edit ${team} on ${resource}`, exact: true })
    .click();
  await expect(editor.getByLabel("Resource", { exact: true })).toHaveAttribute(
    "readonly",
    "",
  );
  await expect(editor.getByLabel("Team", { exact: true })).toHaveAttribute(
    "readonly",
    "",
  );
  await editor
    .getByLabel("Roles (one per line)", { exact: true })
    .fill("environment:viewer\nenvironment:operator");
  await editor
    .getByRole("button", { name: "Review changes", exact: true })
    .click();
  expect(
    (
      await read(
        page,
        `/v1/resource-grants?resourceRid=${encodeURIComponent(resource)}`,
      )
    ).resourceGrants[0].revision,
  ).toBe(1);
  await editor.getByRole("button", { name: "Save grant", exact: true }).click();
  await expect(editor.locator("[data-governance-note]")).toContainText(
    "revision 2",
  );
  const savedGrant = (
    await read(
      page,
      `/v1/resource-grants?resourceRid=${encodeURIComponent(resource)}`,
    )
  ).resourceGrants[0];
  expect(savedGrant).toMatchObject({
    team,
    resourceRid: resource,
    relationship: "CO_OWNER",
    roles: ["environment:operator", "environment:viewer"],
    operations: ["resource:read"],
    revision: 2,
  });

  const restricted = await browser.newContext({
    baseURL,
    extraHTTPHeaders: { "X-Platform-Subject": `console-reader-${stamp}` },
  });
  try {
    const reader = await restricted.newPage();
    await open(reader, "Approval policies");
    await expect(
      reader.getByRole("button", { name: "New approval policy", exact: true }),
    ).toBeDisabled();
    await reader
      .getByRole("radio", { name: "Teams and roles", exact: true })
      .click();
    await expect(
      reader.getByRole("button", {
        name: `Edit ${team} on ${resource}`,
        exact: true,
      }),
    ).toBeDisabled();
    await expect(
      reader.locator('[data-governance-kind="grant"]'),
    ).toContainText("does not hold permissions:manage");
    // Bypassing that presentation hint still reaches the real server authority.
    const refused = await reader.request.post(
      new URL("/v1/resource-grants", reader.url()).toString(),
      { data: { ...savedGrant, roles: ["environment:administrator"] } },
    );
    expect(refused.status()).toBe(403);
    expect(
      (
        await read(
          page,
          `/v1/resource-grants?resourceRid=${encodeURIComponent(resource)}`,
        )
      ).resourceGrants[0],
    ).toEqual(savedGrant);
  } finally {
    await restricted.close();
  }
});
