import { expect, test } from "@playwright/test";

// Actual durable notification routes and mounted browser forms. The harness
// removes every outbound delivery setting; addresses below are metadata only.
if (process.env["CONDUIT_SIMULATED_NOTIFICATIONS"] !== "1")
  throw new Error(
    "Run simulate-console.sh --with-notifications for this fixture.",
  );

test("native notification settings preserve owner boundaries and authoritative revisions", async ({
  page,
}, testInfo) => {
  await page.goto("./notifications");
  const url = (path: string) => new URL(path, page.url()).toString();
  const read = async (path: string) => {
    const response = await page.request.get(url(path));
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  };
  const contactsPath = "/v1/notifications/contacts";
  const preferencesPath = "/v1/notifications/preferences";
  const owner = { kind: "USER", id: "operator" };
  const before = await read(contactsPath);
  expect(before).toEqual({ owner, contacts: [] });
  const writes: { method: string; path: string; body: unknown }[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (
      [contactsPath, preferencesPath].includes(path) &&
      ["PUT", "DELETE"].includes(request.method())
    )
      writes.push({
        method: request.method(),
        path,
        body: request.postData() ? request.postDataJSON() : null,
      });
  });
  await page.locator('.kit-subnav [data-ntf-view="contacts"]').click();
  const form = page.locator('[data-ntf-form="save-contact"]');
  await form.getByLabel("Kind", { exact: true }).selectOption("EMAIL");
  await expect(
    form.getByRole("button", { name: "Save contact", exact: true }),
  ).toBeDisabled();
  const address = "native-fixture@example.invalid";
  await form.getByLabel("Address", { exact: true }).fill(address);
  expect(writes).toEqual([]);
  const savedResponse = page.waitForResponse(
    (r) =>
      r.request().method() === "PUT" &&
      new URL(r.url()).pathname === contactsPath,
  );
  await form.getByRole("button", { name: "Save contact", exact: true }).click();
  const saved = await savedResponse;
  expect(saved.status()).toBe(200);
  const contact = await saved.json();
  expect(contact).toMatchObject({
    owner,
    kind: "EMAIL",
    address,
    optIntoFutureResources: false,
  });
  expect(contact.id).toBeTruthy();
  await expect(page.locator('[data-contact-row="EMAIL"]')).toContainText(
    address,
  );
  const storedContacts = await read(contactsPath);
  expect(storedContacts.contacts).toHaveLength(1);
  expect(storedContacts.contacts[0]).toMatchObject({
    id: contact.id,
    owner,
    kind: "EMAIL",
    address,
    optIntoFutureResources: false,
  });
  // PostgreSQL timestamps retain microseconds; JavaScript Date comparisons
  // use milliseconds. Compare instants without requiring identical fractions.
  expect(Date.parse(storedContacts.contacts[0].createdAt)).toBe(
    Date.parse(contact.createdAt),
  );
  expect(writes).toEqual([
    {
      method: "PUT",
      path: contactsPath,
      body: { kind: "EMAIL", address, optIntoFutureResources: false },
    },
  ]);

  // Even a bootstrap administrator cannot read, overwrite or remove another
  // user's contacts through an explicit owner selector.
  const peerHeaders = { "X-Platform-Subject": "proposer" };
  const namedOwner = "ownerKind=USER&ownerId=operator";
  const peerRead = await page.request.get(
    url(`${contactsPath}?${namedOwner}`),
    { headers: peerHeaders },
  );
  expect(peerRead.status()).toBe(403);
  const peerWrite = await page.request.put(url(contactsPath), {
    headers: peerHeaders,
    data: {
      owner,
      kind: "EMAIL",
      address: "other@example.invalid",
      optIntoFutureResources: false,
    },
  });
  expect(peerWrite.status()).toBe(403);
  const peerDelete = await page.request.delete(
    url(`${contactsPath}?${namedOwner}&kind=EMAIL`),
    { headers: peerHeaders },
  );
  expect(peerDelete.status()).toBe(403);
  expect(await read(contactsPath)).toEqual(storedContacts);
  const peerOwn = await page.request.get(url(contactsPath), {
    headers: peerHeaders,
  });
  expect((await peerOwn.json()).contacts).toEqual([]);

  await page.locator('.kit-subnav [data-ntf-view="preferences"]').click();
  const resourceRid = "environment/dev-use2";
  const preferencePath = `${preferencesPath}?resourceRid=${encodeURIComponent(resourceRid)}&channel=IN_APP`;
  await page.getByLabel("Resource", { exact: true }).fill(resourceRid);
  await page
    .getByRole("button", { name: "Load preference", exact: true })
    .click();
  await expect(page.locator("[data-preference-editor]")).toContainText(
    "You are not subscribed to",
  );
  await page.locator('[data-ntf-type="PLAN_FAILURES"]').check();
  const preferenceBody = {
    resourceRid,
    channel: "IN_APP",
    types: ["PLAN_FAILURES"],
    allFutureTypes: false,
  };
  const preferenceResponse = page.waitForResponse(
    (r) =>
      r.request().method() === "PUT" &&
      new URL(r.url()).pathname === preferencesPath,
  );
  await page
    .getByRole("button", { name: "Save preference", exact: true })
    .click();
  const preferenceSaved = await preferenceResponse;
  expect(preferenceSaved.status()).toBe(200);
  const preference = await preferenceSaved.json();
  expect(preference).toMatchObject({
    ...preferenceBody,
    owner,
    typeAvailabilityScope: "ALL_KNOWN",
  });
  expect(preference.revision).toBeGreaterThan(0);
  await expect(page.locator("[data-preference-saved]")).toContainText(
    `stored revision ${preference.revision}`,
  );
  const storedPreference = await read(preferencePath);
  expect(storedPreference).toMatchObject({
    ...preferenceBody,
    owner,
    id: preference.id,
    revision: preference.revision,
    typeAvailabilityScope: "ALL_KNOWN",
  });
  expect(Date.parse(storedPreference.updatedAt)).toBe(
    Date.parse(preference.updatedAt),
  );
  const peerPreference = await page.request.put(url(preferencesPath), {
    headers: peerHeaders,
    data: { ...preferenceBody, owner, types: [] },
  });
  expect(peerPreference.status()).toBe(403);
  expect(await read(preferencePath)).toEqual(storedPreference);

  // A real intervening owner update changes the native revision. The UI's
  // final re-read must refuse this stale draft without another browser PUT.
  await page.locator('[data-ntf-type="PRODUCT_RECALLS"]').check();
  const advancedResponse = await page.request.put(url(preferencesPath), {
    data: preferenceBody,
  });
  expect(advancedResponse.status()).toBe(200);
  const advancedReceipt = await advancedResponse.json();
  const advanced = await read(preferencePath);
  expect(advanced).toMatchObject({
    ...preferenceBody,
    owner,
    id: preference.id,
    revision: advancedReceipt.revision,
  });
  expect(advanced.revision).toBeGreaterThan(preference.revision);
  const writeCount = writes.length;
  await page
    .getByRole("button", { name: "Save preference", exact: true })
    .click();
  await expect(page.locator("[data-preference-error]")).toContainText(
    "changed on the Hub after you loaded it",
  );
  expect(writes).toHaveLength(writeCount);
  expect(await read(preferencePath)).toEqual(advanced);
  await page.reload();
  await page.locator('.kit-subnav [data-ntf-view="preferences"]').click();
  await page.getByLabel("Resource", { exact: true }).fill(resourceRid);
  await page
    .getByRole("button", { name: "Load preference", exact: true })
    .click();
  await expect(page.locator('[data-ntf-type="PLAN_FAILURES"]')).toBeChecked();
  await expect(
    page.locator('[data-ntf-type="PRODUCT_RECALLS"]'),
  ).not.toBeChecked();

  await page.locator('.kit-subnav [data-ntf-view="contacts"]').click();
  const row = page.locator('[data-contact-row="EMAIL"]');
  await expect(row).toContainText(address);
  const beforeDelete = writes.length;
  await row.getByRole("button", { name: "Delete", exact: true }).click();
  expect(writes).toHaveLength(beforeDelete);
  await row.getByRole("button", { name: "Keep", exact: true }).click();
  expect(await read(contactsPath)).toEqual(storedContacts);
  await row.getByRole("button", { name: "Delete", exact: true }).click();
  const deletedResponse = page.waitForResponse(
    (r) =>
      r.request().method() === "DELETE" &&
      new URL(r.url()).pathname === contactsPath,
  );
  await row
    .getByRole("button", { name: "Confirm delete", exact: true })
    .click();
  const deleted = await deletedResponse;
  expect(deleted.status()).toBe(200);
  expect(await deleted.json()).toEqual({ owner, kind: "EMAIL", deleted: true });
  await expect(row).toHaveCount(0);
  expect(await read(contactsPath)).toEqual({ owner, contacts: [] });
  expect(writes.map(({ method, path }) => ({ method, path }))).toEqual([
    { method: "PUT", path: contactsPath },
    { method: "PUT", path: preferencesPath },
    { method: "DELETE", path: contactsPath },
  ]);
  await testInfo.attach("native-notification-settings-receipts", {
    contentType: "application/json",
    body: Buffer.from(
      JSON.stringify(
        {
          delivery: "disabled by fixture",
          contact,
          preference,
          advanced,
          writes,
        },
        null,
        2,
      ),
    ),
  });
});
