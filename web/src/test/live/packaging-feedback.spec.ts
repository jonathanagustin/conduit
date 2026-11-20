import { expect, test, type Page } from "@playwright/test";

// This suite needs the real recorded-fixture packaging workflow started by the
// opt-in harness. An empty run catalog is a failed setup, never qualification.
const runId = process.env["CONDUIT_SIMULATED_PACKAGING_RUN"];
if (!runId)
  throw new Error(
    "Run simulate-console.sh --with-packaging for packaging feedback qualification.",
  );
const path = `/v1/packaging-runs/${encodeURIComponent(runId)}`;
type ReviewNote = {
  id: string;
  runId: string;
  target: string;
  selector: string;
  note: string;
  author: string;
  approval: boolean;
  createdAt: string;
};
async function read<T>(page: Page, suffix = ""): Promise<T> {
  const response = await page.request.get(
    new URL(path + suffix, page.url()).toString(),
  );
  expect(response.ok(), await response.text()).toBe(true);
  return response.json() as Promise<T>;
}

test("packaging review notes retain exact input without approving the real waiting workflow", async ({
  page,
}, testInfo) => {
  await page.goto("./#s08-staged-run");
  const form = page.locator(`form[data-run="${runId}"]`);
  await expect(form).toBeVisible();
  const parent = await read<Record<string, unknown>>(page);
  expect(parent).toMatchObject({
    runId,
    phase: "review",
    targetPlanRef: "env-dev/sample-app",
  });
  expect(parent.sourceDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(parent.artifactDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(parent.proposalRevision).toBeTruthy();
  expect(parent.planId || "").toBe("");
  const history = await read<{ items: unknown[] }>(page, "/events");
  const initial = await read<{ items: ReviewNote[] }>(page, "/feedback");
  expect(initial.items).toEqual([]);
  const writes: unknown[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname === path + "/feedback"
    )
      writes.push(request.postDataJSON());
  });
  const notes: ReviewNote[] = [];
  const stamp = Date.now().toString(16);
  for (const target of ["file", "manifest", "step"]) {
    const body = {
      target,
      selector: `${target}.reviewed`,
      note: `Reviewed ${target} <script>literal</script> ${stamp}`,
    };
    await form.locator('[name="target"]').selectOption(target);
    await form.getByLabel("Selector", { exact: true }).fill(body.selector);
    await form.getByLabel("Note", { exact: true }).fill(body.note);
    // Force an ordinary read refresh after entry. It must not replace a
    // reviewed draft with the default empty form or record any feedback.
    await page.evaluate(async () => {
      await Reflect.get(window, "state").liveHub.refresh();
    });
    await expect(form.locator('[name="target"]')).toHaveValue(body.target);
    await expect(form.getByLabel("Selector", { exact: true })).toHaveValue(
      body.selector,
    );
    await expect(form.getByLabel("Note", { exact: true })).toHaveValue(
      body.note,
    );
    // Draft edits do not write; the labeled Record action is the commit point.
    expect(
      (await read<{ items: ReviewNote[] }>(page, "/feedback")).items,
    ).toEqual(notes);
    expect(writes).toHaveLength(notes.length);
    const response = page.waitForResponse(
      (value) =>
        value.request().method() === "POST" &&
        new URL(value.url()).pathname === path + "/feedback",
    );
    await form
      .getByRole("button", { name: "Record review note", exact: true })
      .click();
    const recorded = await response;
    expect(recorded.status()).toBe(201);
    const note = (await recorded.json()) as ReviewNote;
    expect(note).toMatchObject({
      ...body,
      runId,
      author: "operator",
      approval: false,
    });
    expect(note.id).toMatch(/^fb_[0-9a-f]+$/);
    expect(Number.isFinite(Date.parse(note.createdAt))).toBe(true);
    notes.push(note);
    expect(writes).toEqual(
      notes.map(({ target, selector, note }) => ({ target, selector, note })),
    );
    expect(
      (await read<{ items: ReviewNote[] }>(page, "/feedback")).items,
    ).toEqual(notes);
    const displayed = page.locator("li").filter({ hasText: body.note });
    await expect(displayed).toContainText(`${target} ${body.selector}`);
    await expect(displayed).toContainText("operator, not an approval");
    await expect(displayed.locator("script,img")).toHaveCount(0);
  }
  await page.reload();
  await expect(form).toBeVisible();
  for (const note of notes)
    await expect(
      page.locator("li").filter({ hasText: note.note }),
    ).toContainText(note.selector);

  await form.getByLabel("Selector", { exact: true }).fill("");
  await form
    .getByLabel("Note", { exact: true })
    .fill("Invalid selector must not be stored");
  const refused = page.waitForResponse(
    (value) =>
      value.request().method() === "POST" &&
      new URL(value.url()).pathname === path + "/feedback",
  );
  await form
    .getByRole("button", { name: "Record review note", exact: true })
    .click();
  expect((await refused).status()).toBe(422);
  await expect(form.locator("[data-error]")).toHaveText(
    "The note was not recorded.",
  );
  expect(
    (await read<{ items: ReviewNote[] }>(page, "/feedback")).items,
  ).toEqual(notes);
  const missing = await page.request.post(
    new URL(
      "/v1/packaging-runs/missing-feedback-parent/feedback",
      page.url(),
    ).toString(),
    {
      data: { target: "step", selector: "review", note: "must not be stored" },
    },
  );
  expect(missing.status()).toBe(404);

  // This existing route is operator-any, not owner-private or environment-grant
  // scoped. A second authenticated reader sees the same run's review input.
  const peer = await page.request.get(
    new URL(path + "/feedback", page.url()).toString(),
    { headers: { "X-Platform-Subject": "feedback-peer" } },
  );
  expect(peer.status()).toBe(200);
  expect((await peer.json()).items).toEqual(notes);
  expect(await read(page)).toEqual(parent);
  expect(await read(page, "/events")).toEqual(history);
  await testInfo.attach("packaging-feedback-receipts", {
    contentType: "application/json",
    body: Buffer.from(
      JSON.stringify({ parent, history, notes, writes }, null, 2),
    ),
  });
});
