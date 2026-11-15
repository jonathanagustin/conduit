import { describe, expect, it, vi } from "vite-plus/test";
import {
  pipelineBuilderMarkup,
  stagedRunMarkup,
  stageChunks,
  selectedPackagingRun,
  readPackagingHistory,
} from "../../console/operator/packaging-pipeline.js";
const event = (sequence: number, step: string, outcome = "PASS") => ({
  sequence,
  step,
  outcome,
  at: "2026-11-15T09:00:00Z",
  reason: `${step} recorded output`,
});
const runs = [
  {
    runId: "r1",
    configured: true,
    phase: "review",
    outcome: "WAITING",
    sourceDigest: "sha256:source",
    artifactDigest: "sha256:run-artifact",
  },
  { runId: "r2", configured: false, phase: "blocked" },
];
describe("recorded packaging pipeline", () => {
  it("preserves contiguous visits, revisited steps and source order without mutating history", () => {
    const events = [
      event(1, "assess", "WAITING"),
      event(2, "assess"),
      event(3, "build", "FAIL_REQUIRED"),
      event(4, "assess", "WAITING"),
    ];
    const original = structuredClone(events);
    const view = stageChunks(events);
    expect(view.ordered).toBe(true);
    expect(
      view.chunks.map((chunk) => [chunk.step, chunk.events.at(-1).outcome]),
    ).toEqual([
      ["assess", "PASS"],
      ["build", "FAIL_REQUIRED"],
      ["assess", "WAITING"],
    ]);
    expect(events).toEqual(original);
    expect(stageChunks([event(2, "build")]).ordered).toBe(false);
    expect(stageChunks([event(1, "build"), event(1, "build")]).ordered).toBe(
      false,
    );
  });
  it("selects only visible runs and never substitutes another run for an absent selection", () => {
    expect(selectedPackagingRun(runs, "r2")).toEqual(runs[1]);
    expect(selectedPackagingRun(runs, "hidden")).toBeNull();
    const html = pipelineBuilderMarkup(runs, [event(1, "secret-step")], {
      runId: "hidden",
    });
    expect(html).toContain("No selected packaging run is available");
    expect(html).not.toContain("secret-step");
  });
  it("renders recorded stages and references without invented success, signatures or future steps", () => {
    const html = pipelineBuilderMarkup(
      runs,
      [event(1, "inspect", "WAITING"), event(2, "check", "FAIL_REQUIRED")],
      { runId: "r1", stageId: "0" },
    );
    expect(html).toContain("Recorded stage graph");
    expect(html).toContain("inspect recorded output");
    expect(html).toContain("WAITING");
    expect(html).not.toContain("Stage 5 of 6");
    expect(html).not.toContain("RUN-8844");
    expect(html).not.toContain("hsm://");
    expect(html).not.toContain("78%");
    const detail = stagedRunMarkup(
      runs,
      [event(1, "check", "FAIL_REQUIRED"), event(2, "review", "WAITING")],
      { runId: "r1" },
    );
    expect(detail.match(/data-stage-chunk=/gu)).toHaveLength(2);
    expect(detail).toContain("review recorded output");
    const artifact = stagedRunMarkup(runs, [], {
      runId: "r1",
      section: "artifacts",
    });
    expect(artifact).toContain("No downloadable artifact reference");
    expect(artifact).not.toContain('href="');
  });
  it("escapes recorded output and reports missing history", () => {
    const html = pipelineBuilderMarkup(
      [{ runId: "<r1>" }],
      [{ ...event(1, "<script>"), reason: '<img src=x onerror="evil()">' }],
      { runId: "<r1>", history: { note: "<unavailable>" } },
    );
    expect(html).not.toContain("<img");
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;img");
    expect(html).toContain("&lt;unavailable&gt;");
    expect(pipelineBuilderMarkup(runs, [], { runId: "r1" })).toContain(
      "No packaging step has been recorded",
    );
  });
  it("follows opaque history cursors and refuses loops with a bounded partial result", async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce({
        items: [event(1, "assess")],
        total: 2,
        nextCursor: "opaque/+=",
      })
      .mockResolvedValueOnce({
        items: [event(2, "review", "WAITING")],
        total: 2,
      });
    const history = await readPackagingHistory("r/1", read);
    expect(history.complete).toBe(true);
    expect(history.items).toHaveLength(2);
    expect(read.mock.calls.map(([path]) => path)).toEqual([
      "/v1/packaging-runs/r%2F1/events",
      "/v1/packaging-runs/r%2F1/events?cursor=opaque%2F%2B%3D",
    ]);
    const looping = vi.fn().mockResolvedValue({
      items: [event(1, "assess")],
      total: 20,
      nextCursor: "same",
    });
    expect((await readPackagingHistory("r1", looping)).complete).toBe(false);
    expect(looping).toHaveBeenCalledTimes(2);
  });
  it("preserves a read prefix on a later failure and refuses malformed pages", async () => {
    const read = vi
      .fn()
      .mockResolvedValueOnce({
        items: [event(1, "inspect")],
        total: 2,
        nextCursor: "next",
      })
      .mockRejectedValueOnce(new Error("offline"));
    const history = await readPackagingHistory("r1", read);
    expect(history.items).toHaveLength(1);
    expect(history.complete).toBe(false);
    expect(history.note).toContain("not be read completely");
    expect((await readPackagingHistory("r1", async () => null)).complete).toBe(
      false,
    );
  });
});
