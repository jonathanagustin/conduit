import { expect, it } from "vitest";
import {
  scheduleTargets,
  scheduleBody,
  adjustSchedule,
  verifyScheduleReceipt,
} from "../../console/operator/window-scheduler.js";
const now = Date.parse("2026-11-15T00:00:00Z");
const targets = [{ entity: "api", installationId: "api-1" }];
const values = {
  entity: "api",
  start: "2026-11-15T02:00",
  minutes: 120,
  reason: "reviewed maintenance",
  ticket: "CAB-1",
  downtime: false,
};
it("binds the proposal to a visible environment Entity and future bounded interval", () => {
  expect(
    scheduleTargets(
      [
        {
          plan: {
            environment: "prod",
            steps: [{ entity: "api" }, { entity: "api" }],
          },
        },
        { plan: { environment: "other", steps: [{ entity: "foreign" }] } },
      ],
      "prod",
    ),
  ).toHaveLength(1);
  expect(scheduleBody("prod", targets, values, now)).toMatchObject({
    entities: [{ environment: "prod", entity: "api" }],
    startsAt: "2026-11-15T02:00:00.000Z",
    duration: "120m",
  });
  for (const patch of [
    { entity: "foreign" },
    { start: "2026-11-14T00:00" },
    { minutes: 216000 },
    { minutes: 0 },
    { reason: "" },
  ])
    expect(() =>
      scheduleBody("prod", targets, { ...values, ...patch }, now),
    ).toThrow();
  expect(adjustSchedule(values, 15).start).toBe("2026-11-15T02:15");
  expect(adjustSchedule(values, -200, true).minutes).toBe(1);
});
it("requires receipts to preserve reviewed coordinates, interval, type, phase and identity", () => {
  const body = scheduleBody("prod", targets, values, now);
  const row = {
    changeRequestId: "cr-1",
    environment: "prod",
    entity: "api",
    startsAt: "2026-11-15T02:00:00Z",
    endsAt: "2026-11-15T04:00:00Z",
    type: "NO_DOWNTIME",
    status: "PENDING_APPROVALS",
  };
  const result = { valueTier: "CONDUIT_NATIVE", dryRun: true, requests: [row] };
  expect(verifyScheduleReceipt(body, result, true)).toBe(row);
  for (const patch of [
    { environment: "other" },
    { entity: "foreign" },
    { startsAt: "2026-11-15T03:00:00Z" },
    { endsAt: "2026-11-15T05:00:00Z" },
    { type: "DOWNTIME" },
    { changeRequestId: "" },
  ])
    expect(() =>
      verifyScheduleReceipt(
        body,
        { ...result, requests: [{ ...row, ...patch }] },
        true,
      ),
    ).toThrow();
  expect(() => verifyScheduleReceipt(body, result, false)).toThrow();
  expect(() =>
    verifyScheduleReceipt(body, result, true, "another-preview"),
  ).toThrow();
});
