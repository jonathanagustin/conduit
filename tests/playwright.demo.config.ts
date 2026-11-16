import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./demo",
  testMatch: /.*\.demo\.ts/,
  timeout: 50 * 60 * 1000,
  expect: { timeout: 60_000 },
  reporter: "line",
  use: {
    ...(process.env.DEMO_BROWSER_CHANNEL ? { channel: process.env.DEMO_BROWSER_CHANNEL } : {}),
    headless: false,
    viewport: { width: 1680, height: 980 },
    launchOptions: { slowMo: 40 },
  },
});
