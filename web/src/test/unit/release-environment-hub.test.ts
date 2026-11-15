import { expect, it } from "vitest";
import { areaObject } from "../../console/operator/kit.js";
import {
  environmentHubMarkup,
  maintenanceMarkup,
  selectedEnvironment,
} from "../../console/operator/release-environment-hub.js";
const hub = {
  environments: [
    { environment: { id: "env-a" } },
    {
      environment: {
        id: "env-b",
        maintenance: {
          downtime: { timeZone: "UTC", intervals: ["Monday 02:00-04:00"] },
        },
      },
    },
  ],
  installations: [
    { environment: "env-a", product: "other-service" },
    { environment: "env-b", product: "selected-service" },
  ],
};
it("binds environment and shared header to the selected visible identity", () => {
  expect(selectedEnvironment(hub.environments, "missing")).toBeNull();
  expect(
    areaObject(hub, "area-releases", { releaseEnvironmentId: "env-b" })?.name,
  ).toBe("env-b");
  expect(
    areaObject(hub, "area-releases", { releaseEnvironmentId: "missing" }),
  ).toBeNull();
  const html = environmentHubMarkup(hub, "env-b", {
    blackout: { environment: "env-a", blocked: false },
  });
  expect(html).toContain("selected-service");
  expect(html).not.toContain("other-service");
  expect(html).toContain("Blackout: unavailable");
  expect(html).toContain("Monday 02:00-04:00");
});
it("distinguishes unavailable blackout evidence and displays rules without scheduling authority", () => {
  expect(
    environmentHubMarkup(hub, "env-b", {
      blackout: { environment: "env-b", blocked: true, reason: "<review>" },
    }),
  ).toContain("blocked · &lt;review&gt;");
  expect(
    environmentHubMarkup(hub, "env-b", {
      blackout: { environment: "env-b", blocked: false },
    }),
  ).toContain("not blocked at the Hub evaluation time");
  expect(maintenanceMarkup({})).toContain(
    "Product and installation constraints can still apply",
  );
  expect(maintenanceMarkup({})).toContain("No calendar drag, snap or Save");
  expect(environmentHubMarkup(hub, "missing")).toContain(
    "No selected environment",
  );
});
