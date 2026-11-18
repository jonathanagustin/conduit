import { expect, type Page, test } from "@playwright/test";

// The assistant against a real Hub: its turn loop, its tool, its audit, its
// thread store and the event stream it writes. The only stand-in is the model,
// scripts/simulate/fake-model.mjs, which calls query_ontology_objects for
// every Environment and then answers. Nothing in this file is stubbed.

const QUESTION = "which environments exist?";
const ANSWER = "Simulated answer: read 1 tool result(s) from the Hub.";

const rail = (page: Page) => page.locator("#right-chat-rail");
const answers = (page: Page) =>
  rail(page).locator('[data-slot="aui_assistant-message-content"]');
const card = (page: Page) =>
  rail(page).locator('[data-assistant-tool="query_ontology_objects"]');

async function openAssistant(page: Page) {
  await page.getByTitle("Toggle right chat panel").click();
  await expect(page.getByLabel("Message input")).toBeVisible();
}

async function ask(page: Page, text: string) {
  await page.getByLabel("Message input").fill(text);
  await page.getByRole("button", { name: "Send message", exact: true }).click();
}

type Thread = { id: string; title: string; messageCount: number };

async function threads(page: Page): Promise<Thread[]> {
  const response = await page.request.get(
    new URL("/v1/assistant/threads", page.url()).toString(),
  );
  expect(response.ok(), "GET /v1/assistant/threads").toBe(true);
  return ((await response.json()) as { threads: Thread[] }).threads;
}

test("a turn is streamed from the Hub with its tool call, result and answer", async ({
  page,
}) => {
  await page.goto("./");
  await openAssistant(page);
  const sent = page.waitForRequest(
    (request) =>
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/assistant/turns",
  );
  await ask(page, QUESTION);
  const request = await sent;
  expect(request.headers()["accept"]).toContain("text/event-stream");
  expect(request.headers()["x-conduit-csrf"]).toBe("1");
  const body = request.postDataJSON() as { threadId?: string; message: string };
  expect(body.message).toBe(QUESTION);
  expect(body.threadId).toMatch(/^ath_[0-9a-f]{32}$/);
  const response = await request.response();
  expect(response?.status()).toBe(200);
  expect(response?.headers()["content-type"]).toContain("text/event-stream");

  // The tool ran on the Hub: its arguments are the ones the Hub executed and
  // its result names the environment the simulation seeded.
  await expect(card(page)).toHaveAttribute("data-tool-state", "ok");
  await expect(card(page).locator("[data-tool-args]")).toContainText(
    "Environment",
  );
  await expect(card(page).locator("[data-tool-outcome]")).toHaveText(
    /^[1-9]\d* matched$/,
  );
  await expect(card(page).locator("[data-tool-result]")).toContainText(
    "dev-use2",
  );
  await expect(answers(page)).toContainText(ANSWER);
  const meta = rail(page).locator("[data-assistant-turn-meta]");
  await expect(meta).toContainText("Live model call");
  await expect(meta.locator("[data-assistant-model]")).toHaveAttribute(
    "data-assistant-model",
    "reported",
  );
  await expect(meta.locator("[data-assistant-model]")).toHaveText(
    "Model: simulated-model",
  );
  // The adapter identity remains in the Hub result; the footer names the
  // provider-reported model instead of presenting an adapter as a model id.
  const events = (await response!.text())
    .split(/\r?\n/)
    .filter((line) => line.startsWith("data:"))
    .map((line) => JSON.parse(line.slice(5)));
  const result = events.find((event) => event.type === "RUN_FINISHED")?.result;
  expect(result?.model).toMatchObject({
    kind: "openai-http",
    configuredModel: "simulated-model",
    providerModel: "simulated-model",
    model: "simulated-model",
  });
  await expect(rail(page).locator(".aui-message-error-root")).toHaveCount(0);

  // The Hub stored the turn in the thread the console opened for it.
  const stored = (await threads(page)).find((row) => row.id === body.threadId);
  expect(stored).toMatchObject({ title: QUESTION, messageCount: 2 });
});

test("the open thread survives a reload, and deleting it removes it from the Hub", async ({
  page,
}) => {
  await page.goto("./");
  await openAssistant(page);
  const sent = page.waitForRequest(
    (request) =>
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/v1/assistant/turns",
  );
  await ask(page, QUESTION);
  const { threadId } = (await sent).postDataJSON() as { threadId: string };
  await expect(answers(page)).toContainText(ANSWER);

  await page.reload();
  await openAssistant(page);
  // Everything shown now was read back from the Hub's thread store.
  await expect(rail(page).locator('[data-role="user"]')).toContainText(
    QUESTION,
  );
  await expect(answers(page)).toContainText(ANSWER);
  await expect(card(page).locator("[data-tool-args]")).toContainText(
    "Environment",
  );
  await expect(card(page).locator("[data-tool-outcome]")).toHaveText(
    /^[1-9]\d* matched$/,
  );
  await expect(rail(page).locator("[data-assistant-thread-title]")).toHaveText(
    QUESTION,
  );

  await rail(page)
    .getByRole("button", { name: /^Conversations \(/ })
    .click();
  const row = rail(page).locator(`[data-assistant-thread="${threadId}"]`);
  await row.getByRole("button", { name: `Delete ${QUESTION}` }).click();
  await row.getByRole("button", { name: "Delete conversation" }).click();
  await expect(row).toHaveCount(0);
  await expect(answers(page)).toHaveCount(0);
  expect((await threads(page)).map((thread) => thread.id)).not.toContain(
    threadId,
  );
});
