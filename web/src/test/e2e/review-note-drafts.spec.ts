import { expect, test, type Page } from "@playwright/test";
import { session, stubHub } from "./hub-stub";

async function setup(page: Page, holdRecord = false) {
  let owner = "reviewer-a";
  const writes: Record<string, unknown>[] = [];
  const notes: Record<string, unknown>[] = [];
  let release!: () => void;
  let started!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  const writing = new Promise<void>((resolve) => {
    started = resolve;
  });
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session")
      return {
        json: session({
          actor: { username: owner, teams: [] },
          effective: { username: owner, teams: [] },
        }),
      };
    if (
      url.pathname === "/v1/packaging-runs/draft-run/feedback" &&
      request.method() === "GET"
    )
      return { json: { items: notes } };
    return { json: {} };
  });
  await page.route("**/v1/packaging-runs/draft-run/feedback", async (route) => {
    if (route.request().method() === "GET")
      return route.fulfill({ json: { items: notes } });
    const body = route.request().postDataJSON();
    writes.push(body);
    started();
    const author = owner;
    if (holdRecord) await pending;
    const recorded = {
      ...body,
      id: `fb_${notes.length + 1}`,
      runId: "draft-run",
      author,
      approval: false,
      createdAt: "2026-11-20T01:00:00Z",
    };
    notes.push(recorded);
    return route.fulfill({ status: 201, json: recorded });
  });
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRuns",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          items: [
            {
              runId: "draft-run",
              phase: "review",
              outcome: "WAITING",
              targetPlanRef: "dev/app",
              sourceDigest: `sha256:${"a".repeat(64)}`,
              artifactDigest: `sha256:${"b".repeat(64)}`,
              proposalRevision: "revision-1",
            },
          ],
          total: "1",
          pageSize: 1,
          limit: 20,
        },
      }),
  );
  await page.route(
    "**/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents",
    (route) =>
      route.fulfill({
        json: {
          configured: true,
          runId: route.request().postDataJSON().runId,
          items: [],
          total: "0",
          pageSize: 0,
          limit: 20,
        },
      }),
  );
  await page.goto("/console/#s08-staged-run");
  const form = page.locator('form[data-run="draft-run"]');
  await expect(form).toBeVisible();
  return {
    form,
    writes,
    notes,
    writing,
    release,
    setOwner(value: string) {
      owner = value;
    },
  };
}
async function refresh(page: Page) {
  await page.evaluate(async () => {
    await Reflect.get(window, "state").liveHub.refresh();
  });
}

test("review-note drafts survive read refresh, remain literal and clear at native identity changes", async ({
  page,
}) => {
  const f = await setup(page);
  await f.form.locator('[name="target"]').selectOption("file");
  await f.form
    .getByLabel("Selector", { exact: true })
    .fill('manifest["reviewed"]');
  const note = '<img src=x onerror="throw 1"> literal review, not approval';
  await f.form.getByLabel("Note", { exact: true }).fill(note);
  await refresh(page);
  await expect(f.form.locator('[name="target"]')).toHaveValue("file");
  await expect(f.form.getByLabel("Selector", { exact: true })).toHaveValue(
    'manifest["reviewed"]',
  );
  await expect(f.form.getByLabel("Note", { exact: true })).toHaveValue(note);
  await expect(f.form.locator("script,img")).toHaveCount(0);
  expect(f.writes).toEqual([]);
  f.setOwner("reviewer-b");
  await refresh(page);
  await expect(page.locator("#operator-name")).toHaveText("reviewer-b");
  await expect(f.form.locator('[name="target"]')).toHaveValue("step");
  await expect(f.form.getByLabel("Selector", { exact: true })).toHaveValue("");
  await expect(f.form.getByLabel("Note", { exact: true })).toHaveValue("");
  expect(f.writes).toEqual([]);
});

test("acknowledging one review note does not clear a newer unsent draft", async ({
  page,
}) => {
  const f = await setup(page, true);
  // Settle initial reads; this case controls the acknowledgement itself.
  await refresh(page);
  try {
    await f.form.getByLabel("Selector", { exact: true }).fill("first.selector");
    await f.form
      .getByLabel("Note", { exact: true })
      .fill("First explicit record");
    await f.form
      .getByRole("button", { name: "Record review note", exact: true })
      .click();
    await f.writing;
    await f.form.locator('[name="target"]').selectOption("manifest");
    await f.form.getByLabel("Selector", { exact: true }).fill("later.selector");
    await f.form.getByLabel("Note", { exact: true }).fill("Later unsent draft");
    await f.form.getByLabel("Note", { exact: true }).blur();
    f.release();
    await expect(
      page.locator("li").filter({ hasText: "First explicit record" }),
    ).toContainText("reviewer-a, not an approval");
    await expect(f.form.locator('[name="target"]')).toHaveValue("manifest");
    await expect(f.form.getByLabel("Selector", { exact: true })).toHaveValue(
      "later.selector",
    );
    await expect(f.form.getByLabel("Note", { exact: true })).toHaveValue(
      "Later unsent draft",
    );
    expect(f.writes).toEqual([
      {
        target: "step",
        selector: "first.selector",
        note: "First explicit record",
      },
    ]);
    expect(f.notes).toHaveLength(1);
  } finally {
    f.release();
  }
});
