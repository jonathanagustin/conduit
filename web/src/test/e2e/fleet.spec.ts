import { expect, test } from "@playwright/test";
import { APPLICATIONS } from "./hub-stub";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("spoke operations uses recorded workloads and opens the exact environment", async ({
  page,
}) => {
  const writes: string[] = [];
  await page.route("**/v1/**", async (route) => {
    if (new URL(route.request().url()).pathname === "/v1/session")
      return route.fulfill({ json: { applications: APPLICATIONS } });
    const url = new URL(route.request().url());
    if (route.request().method() !== "GET") writes.push(url.pathname);
    if (url.pathname === "/v1/environments")
      return route.fulfill({
        json: {
          environments: [
            {
              environment: { id: "east" },
              spoke: { state: "stale", agentId: "agent-east" },
              reported: {
                agentId: "agent-east",
                entities: [
                  {
                    installationId: "east-app",
                    product: "east-product",
                    namespace: "east-ns",
                    version: "1",
                    health: "UNHEALTHY",
                  },
                ],
              },
            },
            {
              environment: {
                id: "west",
                namespaces: [
                  {
                    name: "west-declared",
                    rid: "namespace/west",
                    deletionProtected: true,
                  },
                ],
              },
              spoke: { state: "live", agentId: "agent-west" },
              reported: {
                agentId: "agent-west",
                entities: [
                  {
                    installationId: "west-app",
                    product: "west-product",
                    namespace: "west-ns",
                    version: "2",
                    health: "HEALTHY",
                    healthDetails: {
                      nodes: [
                        {
                          nodeId: "west-pod-observed",
                          result: "HEALTHY",
                          containers: [
                            {
                              containerId: "west-container",
                              liveness: true,
                              readiness: false,
                            },
                          ],
                        },
                      ],
                    },
                  },
                ],
              },
            },
          ],
        },
      });
    if (url.pathname === "/v1/fleet/installations")
      return route.fulfill({ json: { installations: [] } });
    if (url.pathname.endsWith("/timeline"))
      return route.fulfill({ json: { timeline: [] } });
    if (url.pathname === "/v1/blackouts")
      return route.fulfill({
        json: {
          environment: url.searchParams.get("environment"),
          blocked: false,
        },
      });
    return route.fulfill({ status: 404, json: { error: "not configured" } });
  });
  await hydrate(page);
  await page.goto("/console/#s17-spokes-audit");
  await expect(page.locator("[data-spoke-operations]")).toContainText(
    "east-product",
  );
  await expect(page.locator("[data-spoke-operations]")).toContainText(
    "Spoke: stale",
  );
  await expect(page.locator("[data-spoke-operations]")).not.toContainText(
    "Optical Diode",
  );
  await page.locator('[data-spoke-environment="west"]').click();
  await expect(
    page.locator("#main-workspace [data-spoke-workloads]"),
  ).toContainText("west-product");
  await expect(
    page.locator("#main-workspace [data-spoke-workloads]"),
  ).not.toContainText("east-product");
  await expect(
    page.locator("#main-workspace [data-spoke-namespaces]"),
  ).toContainText("west-declared");
  await expect(
    page.locator("#main-workspace [data-spoke-instances]"),
  ).toContainText("west-pod-observed");
  await expect(
    page.locator("#main-workspace [data-spoke-instances]"),
  ).toContainText("readiness no");
  expect(writes).toEqual([]);
});
