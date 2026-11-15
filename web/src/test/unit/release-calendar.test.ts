import { expect, it } from "vitest";
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
