import { expect, it } from "vitest";
import { releaseWavesMarkup } from "../../console/operator/release-waves.js";
it("retains recorded wave order, gate, status and reasons without synthetic scheduling", () => {
  const html = releaseWavesMarkup([
    {
      id: "run",
      product: "service",
      version: "1",
      status: "paused",
      pausedBy: "operator",
      waves: [
        {
          name: "canary",
          gate: "health",
          status: "passed",
          environments: ["dev"],
        },
        {
          name: "fleet",
          gate: "approval",
          status: "blocked",
          reason: "Needs <approval>",
          environments: ["prod"],
        },
      ],
    },
  ]);
  expect(html.indexOf("canary")).toBeLessThan(html.indexOf("fleet"));
  expect(html).toContain("Needs &lt;approval&gt;");
  expect(html).toContain("Gate: approval");
  expect(html).toContain("blocked");
  expect(html).toContain("Paused by: operator");
  expect(html).toContain("scheduling is unavailable");
  expect(html).not.toContain("100%");
});
it("keeps unavailable wave records explicit and escapes all fields", () => {
  expect(releaseWavesMarkup([], "<offline>")).toContain("&lt;offline&gt;");
  expect(releaseWavesMarkup([])).toContain("No staged rollout is recorded");
  const html = releaseWavesMarkup([{ id: "<run>", waves: [] }]);
  expect(html).toContain("&lt;run&gt;");
  expect(html).toContain("No wave detail was recorded");
});
