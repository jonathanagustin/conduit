import { describe, it, expect } from "vitest";
import {
  verifiedSelection,
  validHandoff,
} from "../../console/operator/offline-bundle-flow.mjs";
const item = { id: "bundle-a", manifestDigest: "sha256:a" };
const envelope = { manifest: { id: "bundle-a" }, manifestDigest: "sha256:a" };
const preview = {
  bundleId: "bundle-a",
  manifestDigest: "sha256:a",
  signature: { status: "verified" },
};
describe("offline bundle receipt binding", () => {
  it("requires the selected envelope, catalog and verified preview to agree", () => {
    expect(verifiedSelection(item, envelope, preview)).toBe(true);
    expect(
      verifiedSelection(item, envelope, { ...preview, bundleId: "bundle-b" }),
    ).toBe(false);
    expect(
      verifiedSelection(
        item,
        { ...envelope, manifestDigest: "sha256:other" },
        preview,
      ),
    ).toBe(false);
    expect(
      verifiedSelection(item, envelope, {
        ...preview,
        signature: { status: "unknown" },
      }),
    ).toBe(false);
  });
  it("rejects a handoff receipt from another site, actor, digest or bundle", () => {
    const handoff = {
      id: "handoff-a",
      bundleId: "bundle-a",
      digest: "sha256:a",
      site: "east",
      takenBy: "alice",
      status: "handed_off",
    };
    expect(validHandoff({ handoff }, item, "east", "alice")).toBe(true);
    for (const mutation of [
      { site: "west" },
      { takenBy: "bob" },
      { digest: "sha256:b" },
      { bundleId: "bundle-b" },
      { status: "installed" },
    ])
      expect(
        validHandoff(
          { handoff: { ...handoff, ...mutation } },
          item,
          "east",
          "alice",
        ),
      ).toBe(false);
  });
});
