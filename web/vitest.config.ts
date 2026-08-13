import { defineConfig } from "vitest/config";

// Unit tests only. The Playwright specs live under src/test/e2e and are driven
// by their own runner; including them here would start a browser inside vitest.
export default defineConfig({
  test: {
    include: ["src/test/unit/**/*.test.ts"],
    environment: "node",
  },
});
