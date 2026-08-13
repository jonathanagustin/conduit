import { defineConfig, devices } from "@playwright/test";

// The console is served under /console/ in production, and the router carries a
// matching basename, so the tests drive it at that path rather than at the root.
// Testing it at "/" would pass against a build that cannot be embedded.
const PORT = 4173;

export default defineConfig({
  testDir: "./src/test/e2e",
  // A failing assertion in a poll-driven UI is usually a race, not a defect, so
  // retries would hide exactly the flakiness worth seeing. Kept at zero.
  retries: 0,
  fullyParallel: true,
  reporter: process.env["CI"] === undefined ? "list" : [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: `http://127.0.0.1:${PORT}/console/`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    // `preview` serves the production build, so the tests exercise what ships
    // rather than what the dev server rewrites.
    // vite preview binds localhost only, which on this machine resolves to ::1
    // and leaves 127.0.0.1 refused. Bind explicitly so the URL below is reachable
    // however the host resolves names.
    command: `pnpm build && pnpm preview --port ${PORT} --strictPort --host 127.0.0.1`,
    url: `http://127.0.0.1:${PORT}/console/`,
    reuseExistingServer: process.env["CI"] === undefined,
    // tripled: this machine runs a Go mutation sweep alongside the suite, and a
    // contended build was timing the web server out before it could serve.
    timeout: 540_000,
  },
});
