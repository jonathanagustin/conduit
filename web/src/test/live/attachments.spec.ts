import { createHash, randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";

type Attachment = { name: string; kind: string; size: number; sha256: string };

test("attachment uploads retain exact bytes, owner visibility and native refusals on a real Hub", async ({
  page,
}) => {
  await page.goto("./");
  await expect(page.locator("#operator-name")).toHaveText("operator");
  await page.getByTitle("Toggle right chat panel").click();
  await expect(page.getByLabel("Message input")).toBeVisible();
  const rail = page.locator("#right-chat-rail");
  const endpoint = new URL("/v1/intake-attachments", page.url()).toString();
  const list = async (headers?: Record<string, string>) => {
    const response = await page.request.get(endpoint, { headers });
    expect(response.ok(), await response.text()).toBe(true);
    return ((await response.json()) as { items: Attachment[] }).items;
  };
  const initial = await list();
  const stamp = randomUUID();
  const name = `reviewed-<b>-${stamp}.txt`;
  const content = `Reviewed configuration α ${stamp}\n`;
  const bytes = Buffer.from(content, "utf8");
  const digest = createHash("sha256").update(bytes).digest("hex");
  const expected = { name, kind: "Text", size: bytes.length, sha256: digest };
  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (request.method() === "POST" && request.url() === endpoint)
      writes.push(request.postDataJSON());
  });
  const input = () => rail.locator('[data-attach-input="assistant"]');
  const row = () =>
    rail.locator("[data-attachment-row]").filter({ hasText: digest });
  // Both accepted uploads go through the mounted browser file control and real
  // Hub. The second must retain one authoritative digest rather than two rows.
  for (let upload = 0; upload < 2; upload++) {
    const completed = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" && response.url() === endpoint,
    );
    const file = { name, mimeType: "text/plain", buffer: bytes };
    if (upload === 0) {
      const opened = page.waitForEvent("filechooser");
      await rail.locator('[data-attach-button="assistant"]').click();
      const chooser = await opened;
      // Trigger actual chrome replacement while the browser owns a picker.
      // A detached original input cannot deliver this selection to the Hub.
      await rail.locator('[data-set-mode="ontology"]').click();
      expect(await chooser.element().evaluate((node) => node.isConnected)).toBe(
        true,
      );
      await chooser.setFiles(file);
    } else {
      await input().setInputFiles(file);
    }
    const response = await completed;
    expect(response.status(), await response.text()).toBe(201);
    expect(await response.json()).toMatchObject(expected);
    await expect(row()).toHaveCount(1);
    await expect(row()).toContainText(name);
    await expect(row().locator("b")).toHaveCount(0);
    const stored = await list();
    expect(stored).toHaveLength(initial.length + 1);
    expect(stored.filter((item) => item.sha256 === digest)).toEqual([expected]);
  }
  expect(writes).toEqual([
    { filename: name, content },
    { filename: name, content },
  ]);
  const readback = await page.request.get(`${endpoint}/${digest}`);
  expect(readback.ok(), await readback.text()).toBe(true);
  const recorded = await readback.json();
  expect(recorded).toMatchObject({ ...expected, content });
  expect(recorded).not.toHaveProperty("owners");

  // A client-only refusal makes no native write. A text/encoding refusal must
  // reach the Hub, display its actual reason, and leave the catalog unchanged.
  await input().setInputFiles({
    name: "refused.exe",
    mimeType: "application/octet-stream",
    buffer: Buffer.from("fixture"),
  });
  await expect(rail.locator("[data-attach-reason]")).toContainText(
    "is not an allowed type",
  );
  expect(writes).toHaveLength(2);
  const invalid = Buffer.from([0x61, 0, 0x62]);
  const invalidDigest = createHash("sha256").update(invalid).digest("hex");
  const refused = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" && response.url() === endpoint,
  );
  await input().setInputFiles({
    name: "binary-canary.txt",
    mimeType: "text/plain",
    buffer: invalid,
  });
  const refusal = await refused;
  expect(refusal.status()).toBe(422);
  const error = (await refusal.json()) as { error: string };
  expect(error.error).toContain("is not text");
  await expect(rail.locator("[data-attach-reason]")).toHaveText(error.error);
  expect(writes).toHaveLength(3);
  const afterRefusal = await list();
  expect(afterRefusal).toHaveLength(initial.length + 1);
  expect(afterRefusal.some((item) => item.sha256 === invalidDigest)).toBe(
    false,
  );
  expect(afterRefusal.filter((item) => item.sha256 === digest)).toEqual([
    expected,
  ]);
  expect(afterRefusal.filter((item) => item.sha256 !== digest)).toEqual(
    initial,
  );

  const other = { "X-Platform-Subject": `attachment-other-${stamp}` };
  expect((await list(other)).some((item) => item.sha256 === digest)).toBe(
    false,
  );
  const foreign = await page.request.get(`${endpoint}/${digest}`, {
    headers: other,
  });
  const missing = await page.request.get(`${endpoint}/${"0".repeat(64)}`, {
    headers: other,
  });
  expect(foreign.status()).toBe(404);
  expect(missing.status()).toBe(404);
  expect(await foreign.json()).toEqual(await missing.json());
  await page.reload();
  await page.getByTitle("Toggle right chat panel").click();
  await expect(page.getByLabel("Message input")).toBeVisible();
  await expect(row()).toHaveCount(1);
  await expect(row()).toContainText(name);
  await expect(row().locator("b")).toHaveCount(0);
  expect(writes).toHaveLength(3);
});
