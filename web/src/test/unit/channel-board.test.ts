import { describe, expect, it } from "vite-plus/test";

import { channelTable } from "../../console/operator/channel-board.js";

describe("channel subscriptions", () => {
  it("renders the version on each channel and an empty board as nothing", () => {
    expect(channelTable({ channels: [], rows: [] })).toBe("");
    const html = channelTable({
      channels: ["dev", "candidate", "stable"],
      rows: [
        {
          environment: "east",
          product: "ledger",
          subscribed: "stable",
          versions: { dev: "1.0.0", candidate: "1.1.0", stable: "1.2.0" },
          running: "1.1.0",
        },
      ],
    });
    expect(html).toContain("east");
    expect(html).toContain("1.2.0");
    expect(html).toContain("stable");
    expect(html).toContain("1.1.0");
  });
});
