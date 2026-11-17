import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("ontology explorer scopes pages, drops stale reads and validates without execution", async ({
  page,
}) => {
  const metadata = JSON.parse(
    readFileSync(
      new URL(
        "../../../../testdata/ontology/full-metadata.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  let releaseA!: () => void;
  const heldA = new Promise<void>((resolve) => {
    releaseA = resolve;
  });
  let startedA = false;
  const actions: any[] = [];
  await page.route("**/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route("**/api/v2/ontologies/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/fullMetadata"))
      return route.fulfill({ json: metadata });
    const body = route.request().postDataJSON();
    if (path.endsWith("/objectSets/loadObjects")) {
      expect(body.pageSize).toBe(25);
      if (body.objectSet.objectType === "Environment") {
        startedA = true;
        await heldA;
        return route.fulfill({
          json: { data: [{ id: "foreign-environment" }] },
        });
      }
      if (body.pageToken === "second")
        return route.fulfill({
          status: 403,
          json: { error: "sample forbidden" },
        });
      return route.fulfill({
        json: {
          data: [
            {
              id: "plan-b",
              __primaryKey: "plan-b",
              secret: "hidden-value",
              title: "<img src=x onerror=alert(1)>",
            },
          ],
          nextPageToken: "second",
        },
      });
    }
    if (path.includes("/actions/")) {
      actions.push(body);
      return route.fulfill({ json: { validation: { result: "VALID" } } });
    }
    return route.fulfill({ status: 404, json: { error: "unknown" } });
  });
  await hydrate(page);
  await page.goto("/console/#s19-ontology");
  const picker = page.getByLabel("Ontology object type");
  await expect(picker.locator('option[value="Environment"]')).toHaveCount(1);
  await picker.selectOption("Environment");
  await expect.poll(() => startedA).toBe(true);
  await picker.selectOption("Plan");
  await expect(page.locator("[data-ontology-samples]")).toContainText("plan-b");
  const staleResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/objectSets/loadObjects") &&
      response.status() === 200,
  );
  releaseA();
  await staleResponse;
  await page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
  await expect(page.locator("[data-ontology-samples]")).not.toContainText(
    "foreign-environment",
  );
  await expect(page.locator("[data-ontology-samples]")).not.toContainText(
    "hidden-value",
  );
  await expect(page.locator("[data-ontology-samples] img")).toHaveCount(0);
  await page.getByLabel("Ontology action").selectOption("createChangeRequest");
  await page
    .getByLabel("Ontology validation parameters")
    .fill('{"title":"reviewed draft"}');
  await page
    .getByRole("button", { name: "Validate only", exact: true })
    .click();
  await expect(page.locator("[data-ont-validation-result]")).toContainText(
    "Validation accepted",
  );
  expect(actions).toEqual([
    {
      parameters: { title: "reviewed draft" },
      options: { mode: "VALIDATE_ONLY" },
    },
  ]);
  await page.getByRole("button", { name: "Next page", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("sample forbidden");
  await expect(page.locator("[data-ontology-samples]")).not.toContainText(
    "plan-b",
  );
});

test("ontology graph distinguishes an empty catalog from denied metadata", async ({
  page,
}) => {
  let denied = false;
  let populated = false;
  let objectReads = 0;
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({
        json: {
          actor: { username: "alice" },
          effective: { username: "alice" },
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await page.route("**/api/v2/ontologies/**", async (route) => {
    if (route.request().url().endsWith("/fullMetadata"))
      return denied
        ? route.fulfill({ status: 403, json: { error: "forbidden" } })
        : route.fulfill({
            json: populated
              ? JSON.parse(
                  readFileSync(
                    new URL(
                      "../../../../testdata/ontology/full-metadata.json",
                      import.meta.url,
                    ),
                    "utf8",
                  ),
                )
              : { objectTypes: {}, actionTypes: {} },
          });
    objectReads++;
    return route.fulfill({ status: 403, json: { error: "forbidden" } });
  });
  await hydrate(page);
  await page.goto("/console/#og01-ontology-graph");
  await expect(
    page.getByText("No object types returned", { exact: true }),
  ).toBeVisible();
  await expect(page.locator('svg[aria-label="Ontology graph"] g')).toHaveCount(
    0,
  );
  expect(objectReads).toBe(0);
  denied = true;
  await page.evaluate(() => Reflect.get(window, "reloadOntologyGraph")());
  await expect(
    page.getByText("Metadata unavailable; no example data substituted", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.locator("#main-workspace")).not.toContainText(
    "edge-west-02",
  );
  await expect(page.locator('svg[aria-label="Ontology graph"] g')).toHaveCount(
    0,
  );
  expect(objectReads).toBe(0);
  denied = false;
  populated = true;
  await page.evaluate(() => Reflect.get(window, "reloadOntologyGraph")());
  await expect(
    page.getByText("Native metadata · first 256 types", { exact: true }),
  ).toBeVisible();
  await expect(page.locator('svg[aria-label="Ontology graph"] g')).toHaveCount(
    7,
  );
  await page
    .locator('svg[aria-label="Ontology graph"] g')
    .filter({ hasText: "Plan" })
    .click();
  await expect(
    page
      .getByText("Object page unavailable; no zero-count inference.", {
        exact: true,
      })
      .first(),
  ).toBeVisible();
});
