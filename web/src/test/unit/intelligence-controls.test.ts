import { expect, it } from "vite-plus/test";
import {
  intelligenceTargets,
  canStageCourse,
  expectationSummary,
  verifyCourseReceipt,
  evalMarkup,
} from "../../console/operator/intelligence.js";
it("offers only visible desired targets and never treats a missing gate flag as permission", () => {
  const target = {
    environment: "prod",
    installationId: "api",
    product: "service",
    desiredVersion: "1",
  };
  expect(
    intelligenceTargets([
      target,
      target,
      { ...target, desiredVersion: "" },
      null,
    ]),
  ).toHaveLength(1);
  expect(
    canStageCourse({ value: "512Mi", outcome: "PASS", blocked: false }),
  ).toBe(true);
  expect(
    canStageCourse({ value: "8Gi", outcome: "FAIL_REQUIRED", blocked: true }),
  ).toBe(false);
  expect(canStageCourse({ value: "512Mi", outcome: "PASS" })).toBe(false);
});
it("counts expected failures as matched evaluations, not gate passes, and leaves unrun cases unscored", () => {
  const payload = {
    results: [
      {
        case: { id: "expected-failure", expectedOutcome: "FAIL_REQUIRED" },
        columns: [
          { adapter: "recorded", outcome: "FAIL_REQUIRED", passed: true },
        ],
      },
      {
        case: { id: "pass" },
        columns: [{ adapter: "recorded", outcome: "PASS", passed: true }],
      },
    ],
  };
  expect(expectationSummary(payload)).toEqual([
    { adapter: "recorded", matched: 2, total: 2 },
  ]);
  expect(expectationSummary({ cases: [{ expectedOutcome: "PASS" }] })).toEqual(
    [],
  );
  expect(evalMarkup(payload)).toContain("2 / 2 expectations matched");
  expect(evalMarkup({ cases: [{ id: "<case>" }] })).toContain("&lt;case&gt;");
  expect(evalMarkup(payload)).toContain("not a deployment gate pass");
});
it("requires the proposal receipt to match every staged coordinate and value", () => {
  const body = {
    Value: "512Mi",
    Environment: "prod",
    InstallationID: "api",
    ProductID: "service",
  };
  const result = {
    valueTier: "CONDUIT_NATIVE",
    environment: "prod",
    installationId: "api",
    productId: "service",
    value: "512Mi",
    changeRequestId: "crq_native",
    status: "PENDING_APPROVALS",
  };
  expect(verifyCourseReceipt(body, result)).toBe(result);
  for (const patch of [
    { environment: "dev" },
    { installationId: "other" },
    { productId: "other" },
    { value: "8Gi" },
    { changeRequestId: "" },
    { valueTier: "EXAMPLE" },
  ])
    expect(() => verifyCourseReceipt(body, { ...result, ...patch })).toThrow();
});
