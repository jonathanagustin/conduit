import { describe, expect, it } from "vite-plus/test";
import { servedModelLabel } from "../../console/model-identity";

describe("serving model identity", () => {
  it("keeps requested and provider-reported ids distinguishable", () => {
    expect(
      servedModelLabel({
        configuredModel: "alias",
        providerModel: "served-v2",
      }),
    ).toBe("Model: served-v2 (configured: alias)");
    expect(
      servedModelLabel({ configuredModel: "same", providerModel: "same" }),
    ).toBe("Model: same");
  });
  it("discloses an absent provider id and keeps old responses readable", () => {
    expect(servedModelLabel({ configuredModel: "cli-model" })).toBe(
      "Model: cli-model (operator configured; provider id unavailable)",
    );
    expect(servedModelLabel({ model: "legacy" })).toBe("Model: legacy");
    expect(servedModelLabel()).toBe("Model identity unavailable");
  });
});
