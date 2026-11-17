import { expect, test } from "@playwright/test";
import { session, stubHub } from "./hub-stub";

test("course staging follows the selected installation grant", async ({
  page,
}) => {
  const writes: unknown[] = [];
  await stubHub(page, (request, url) => {
    if (url.pathname === "/v1/session")
      return {
        json: session({
          resources: [
            {
              resourceRid: "installation/dev/denied",
              operations: ["resource:read"],
            },
            {
              resourceRid: "installation/dev/allowed",
              operations: ["change:propose"],
            },
          ],
        }),
      };
    if (url.pathname === "/v1/fleet/installations")
      return {
        json: {
          installations: ["denied", "allowed"].map((installationId) => ({
            environment: "dev",
            installationId,
            product: "test-app",
            desiredVersion: "1",
          })),
        },
      };
    if (url.pathname === "/v1/intelligence/logic")
      return { json: { adapter: "recorded", gateRule: "memory <= 1Gi" } };
    if (url.pathname === "/v1/intelligence/courses")
      return {
        json: {
          options: [
            { id: "small", value: "512Mi", outcome: "PASS", blocked: false },
          ],
        },
      };
    if (url.pathname === "/v1/intelligence/courses/select") {
      const body = request.postDataJSON();
      writes.push(body);
      return {
        json: {
          valueTier: "CONDUIT_NATIVE",
          changeRequestId: "reviewed-course",
          status: "PENDING_APPROVALS",
          environment: body.Environment,
          installationId: body.InstallationID,
          productId: body.ProductID,
          value: body.Value,
        },
      };
    }
  });
  await page.goto("/console/");
  await page.getByRole("button", { name: "Intelligence", exact: true }).click();
  const form = page.locator("[data-intel-stage]");
  await expect(form.locator("[data-intel-capability]")).toHaveText(
    "Your session does not hold change:propose on installation/dev/denied.",
  );
  await form.locator("[data-intel-confirm]").check();
  await expect(form.locator("[data-intel-create]")).toBeDisabled();
  expect(writes).toHaveLength(0);
  await form
    .locator('[name="target"]')
    .selectOption(JSON.stringify(["dev", "allowed", "test-app"]));
  await expect(form.locator("[data-intel-capability]")).toBeEmpty();
  await expect(form.locator("[data-intel-confirm]")).not.toBeChecked();
  await form.locator("[data-intel-confirm]").check();
  await form.locator("[data-intel-create]").click();
  await expect(form.locator("[data-intel-note]")).toContainText(
    "reviewed-course",
  );
  expect(writes).toEqual([
    {
      Value: "512Mi",
      Environment: "dev",
      InstallationID: "allowed",
      ProductID: "test-app",
    },
  ]);
});
