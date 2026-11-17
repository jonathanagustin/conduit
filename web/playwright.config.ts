import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import { defineConfig, devices } from "@playwright/test";
import { localCiBrowserLaunchOptions } from "./scripts/local-ci-browser-profile.mjs";

// Port is derived from this checkout so two worktrees do not share a preview
// server. See issue #270.
const derivedPort =
  20_000 +
  (createHash("sha256")
    .update(fileURLToPath(import.meta.url))
    .digest()
    .readUInt32BE(0) %
    10_000);
const PORT = Number.parseInt(
  process.env["CONSOLE_TEST_PORT"] ?? String(derivedPort),
  10,
);

export default defineConfig({
  testDir: "./src/test/e2e",
  globalSetup: "./src/test/e2e/global-setup.ts",
  retries: 0,
  fullyParallel: true,
  reporter:
    process.env["CI"] === undefined
      ? "list"
      : [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: `http://127.0.0.1:${PORT}/console/`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: localCiBrowserLaunchOptions(),
      },
    },
  ],
  webServer: {
    command: `pnpm build && pnpm preview --port ${PORT} --strictPort --host 127.0.0.1`,
    url: `http://127.0.0.1:${PORT}/console/`,
    reuseExistingServer: process.env["CI"] === undefined,
    timeout: 540_000,
  },
});
