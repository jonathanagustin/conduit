import { expect, it } from "vitest";
import {
  approvalColumns,
  approvalsMarkup,
  constraintGateMarkup,
} from "../../console/operator/change-request-views.js";
import {
  changeDiffMarkup,
  diffValue,
} from "../../console/operator/change-diff.js";
import {
  bindReviewContext,
  reviewMarkup,
  reviewState,
} from "../../console/operator/change-review.js";
import { areaObject } from "../../console/operator/kit.js";
const request = {
  id: "cr-a",
  title: "A",
  revision: 1,
  status: "PENDING_APPROVALS",
  author: { username: "writer" },
  changes: [
    {
      id: "change-a",
      diff: {
        "limits.memory": { before: "256Mi", after: "512Mi" },
        password: { before: "hidden-old", after: "hidden-new" },
      },
    },
  ],
  requirements: [{ kind: "TEAM", team: "reviewers", reason: "review changes" }],
};
it("keeps recorded-only and unknown Plan statuses out of executable/complete columns", () => {
  const columns = approvalColumns([
    { id: "a", status: "AWAITING_APPROVAL" },
    { id: "b", status: "RECORDED" },
    { id: "c", status: "NEW_UNKNOWN" },
  ]);
  expect(
    columns
      .find((row) => row.label === "Recorded only")!
      .rows.map((row) => row.id),
  ).toEqual(["b"]);
  expect(columns.find((row) => row.label === "Succeeded")!.rows).toEqual([]);
  expect(
    columns
      .find((row) => row.label === "Other / unavailable")!
      .rows.map((row) => row.id),
  ).toEqual(["c"]);
  expect(approvalsMarkup([{ id: "<plan>", status: "RECORDED" }])).toContain(
    "&lt;plan&gt;",
  );
});
it("shows only recorded side-by-side fields, with bounded redacted JSON display lines", () => {
  const html = changeDiffMarkup(request);
  expect(html).toContain("256Mi");
  expect(html).toContain("512Mi");
  expect(html).toContain("[redacted]");
  expect(html).not.toContain("hidden-old");
  expect(html).not.toContain("hidden-new");
  expect(html).toContain("not source-file lines");
  expect(
    diffValue({ accessToken: "hidden", value: "<img>" }, "config"),
  ).not.toContain("hidden");
  expect(
    changeDiffMarkup({
      changes: [{ id: "a", diff: { x: { after: "<img>" } } }],
    }),
  ).toContain("&lt;img&gt;");
  expect(changeDiffMarkup({})).toContain("No field-level diff");
  expect(diffValue("x".repeat(10000), "config")).toContain(
    "[display truncated]",
  );
});
it("binds drafts to the selected request revision and actual operator identity", () => {
  const review: any = {
    filter: "all",
    selectedId: "cr-a",
    draft: { decision: "", rationale: "" },
    error: "",
  };
  bindReviewContext([request], review, "reviewer");
  review.draft = { decision: "approve", rationale: "checked" };
  bindReviewContext([request], review, "reviewer");
  expect(review.draft.decision).toBe("approve");
  bindReviewContext([{ ...request, revision: 2 }], review, "reviewer");
  expect(review.draft).toEqual({ decision: "", rationale: "" });
  review.draft = { decision: "approve", rationale: "checked" };
  bindReviewContext([{ ...request, revision: 2 }], review, "other-reviewer");
  expect(review.draft.decision).toBe("");
  review.selectedId = "missing";
  review.draft = { decision: "approve", rationale: "stale" };
  expect(bindReviewContext([request], review, "reviewer")).toBeNull();
  expect(review.draft.decision).toBe("");
  const html = reviewMarkup([request], {
    selectedId: "missing",
    operatorName: "reviewer",
  });
  expect(html).toContain("no longer in the visible");
  expect(html).not.toContain("data-cr-confirm");
  expect(
    areaObject(
      { changes: [request, { ...request, id: "cr-b", title: "B" }] },
      "area-changes",
      { changeRequestId: "cr-b" },
    )?.name,
  ).toBe("B");
  expect(
    areaObject({ changes: [request] }, "area-changes", {
      changeRequestId: "missing",
    }),
  ).toBeNull();
});
it("keeps unknown identity and self-review closed in both review and gate", () => {
  expect(reviewState(request, "").canReview).toBe(false);
  expect(reviewState(request, "writer").canReview).toBe(false);
  const html = constraintGateMarkup(request, "writer");
  expect(html).toContain("author cannot approve");
  expect(html).toContain("reviewers");
  expect(html).not.toContain("data-cr-decide");
});
