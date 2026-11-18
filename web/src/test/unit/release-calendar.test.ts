import { expect, it } from "vite-plus/test";
import {
  recordedIntervals,
  releaseCalendarMarkup,
} from "../../console/operator/release-calendar.js";
it("draws only distinct actual increasing maintenance intervals", () => {
  const row = {
    type: "DOWNTIME",
    startsAt: "2026-11-15T01:00:00Z",
    endsAt: "2026-11-15T02:00:00Z",
    reason: "<reviewed>",
    approvedBy: "reviewer",
  };
  expect(
    recordedIntervals([
      row,
      row,
      { ...row, endsAt: "invalid" },
      { ...row, endsAt: row.startsAt },
    ]),
  ).toHaveLength(1);
  expect(releaseCalendarMarkup([row])).toContain("&lt;reviewed&gt;");
  expect(releaseCalendarMarkup([row])).toContain("width:100%");
  expect(releaseCalendarMarkup([row])).toContain(
    "not rollout execution appointments",
  );
  expect(releaseCalendarMarkup([])).toContain(
    "Resolved scheduling data is unavailable",
  );
});

it("draws only bounded native exact-environment blackout intervals and discloses custody", () => {
  const snapshot = {
    environment: "east",
    valueTier: "CONDUIT_NATIVE",
    persistence: "process-local",
    evaluatedAt: "2026-11-18T12:00:00Z",
    truncated: true,
    intervals: [
      {
        environment: "east",
        author: "<operator>",
        reason: "<freeze>",
        startsAt: "2026-11-18T11:00:00Z",
        endsAt: "2026-11-18T13:00:00Z",
      },
    ],
  };
  const markup = releaseCalendarMarkup([], snapshot);
  expect(markup).toContain("repeating-linear-gradient");
  expect(markup).toContain('aria-label="Recorded blackout interval"');
  expect(markup).toContain("&lt;freeze&gt;");
  expect(markup).toContain("&lt;operator&gt;");
  expect(markup).toContain("Process-local blackout snapshot");
  expect(markup).toContain("omitted intervals can still block");
  for (const invalid of [
    { ...snapshot, valueTier: "unknown" },
    { ...snapshot, persistence: "durable" },
    {
      ...snapshot,
      intervals: [{ ...snapshot.intervals[0], environment: "west" }],
    },
    {
      ...snapshot,
      intervals: [{ ...snapshot.intervals[0], endsAt: "invalid" }],
    },
    {
      ...snapshot,
      intervals: Array.from({ length: 65 }, () => snapshot.intervals[0]),
    },
    {
      environment: "east",
      blocked: true,
      reason: "a reason is not coordinates",
    },
  ]) {
    const absent = releaseCalendarMarkup([], invalid);
    expect(absent).toContain("Blackout interval evidence is unavailable");
    expect(absent).not.toContain("data-blackout-interval");
  }
  expect(
    releaseCalendarMarkup([], { ...snapshot, truncated: false, intervals: [] }),
  ).toContain("No stored blackout interval");
});
