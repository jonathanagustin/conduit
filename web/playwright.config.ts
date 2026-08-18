import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import { defineConfig, devices } from "@playwright/test";

// The console is served under /console/ in production, and the router carries a
// matching basename, so the tests drive it at that path rather than at the root.
// Testing it at "/" would pass against a build that cannot be embedded.

// The default port is derived from this checkout's own path rather than fixed.
//
// It was 4173 for everybody, and `reuseExistingServer` below is on outside CI,
// so two worktrees verifying two branches on one machine did not collide
// loudly: the second attached to the first's preview server and ran its whole
// suite against another tree's build. When the first run finished and took its
// server down, the second died mid-suite with eighty `ECONNREFUSED`s naming a
// port and nothing else. That is issue #270, and it cost two full local
// verifications before the cause was found. Concurrent worktrees are how this
// repository is worked on, so that is the normal case rather than an exotic one.
//
// Derived rather than random, because reuse has to keep working: the same
// checkout must land on the same port every run or `reuseExistingServer` can
// never find the server it left behind. Different checkouts land on different
// ports, which is the property that was missing.
//
// Below 32768 to stay clear of Linux's default ephemeral range
// (`ip_local_port_range` is 32768-60999), so the port cannot already be held by
// an outbound socket some other process opened.
//
// A hash still collides eventually, and `global-setup.ts` is what makes that
// case loud rather than silent: it refuses to run against a server serving a
// build other than this checkout's.
const derivedPort =
  20_000 +
  (createHash("sha256").update(fileURLToPath(import.meta.url)).digest().readUInt32BE(0) % 10_000);
const PORT = Number.parseInt(process.env["CONSOLE_TEST_PORT"] ?? String(derivedPort), 10);
if (!Number.isInteger(PORT) || PORT < 1 || PORT > 65_535) {
  throw new Error("CONSOLE_TEST_PORT must be a valid TCP port");
}

export default defineConfig({
  testDir: "./src/test/e2e",
  // Runs before the suite and refuses a server serving somebody else's build.
  // Reuse is safe exactly when the bytes match; this is what checks that.
  globalSetup: "./src/test/e2e/global-setup.ts",
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
