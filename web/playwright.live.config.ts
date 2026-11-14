import { defineConfig, devices } from "@playwright/test";

// The live suite. Unlike playwright.config.ts, which builds the console with
// Vite, serves it from `preview`, and intercepts every /v1 call, this one runs
// no server at all: scripts/simulate-console.sh starts a real Hub, seeds it
// through the Hub's own API, and passes the URL of the console that Hub serves
// from its embedded bundle. Nothing here stubs anything.
//
// It is a separate config rather than a project inside the main one so that
// `pnpm test` -- which the console CI gate runs -- cannot come to depend on a
// Hub being up.
const baseURL = process.env["CONDUIT_HUB_URL"];
if (baseURL === undefined || baseURL === "") {
  throw new Error(
    "CONDUIT_HUB_URL is required; run scripts/simulate-console.sh rather than this config directly",
  );
}

export default defineConfig({
  testDir: "./src/test/live",
  // Same reasoning as the stubbed suite: a retry would hide the races worth
  // seeing, and against a real Hub those are the interesting failures.
  retries: 0,
  // One Hub, one story. The specs share seeded state and one of them approves a
  // Change Request, which is exactly the kind of write that must not land
  // underneath another spec's assertion about what has not been approved yet.
  fullyParallel: false,
  workers: 1,
  // The stubbed suite answers instantly; this one waits on a real Agent that
  // polls for work and a console that refetches every 30 seconds. Playwright's
  // default 30s per-test timeout caps a longer expect timeout rather than
  // deferring to it, so it is raised here instead of per assertion.
  timeout: 120_000,
  reporter: "list",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    // The console has no identity provider in a simulation, so the operator it
    // acts as is named here. The Hub accepts the header only because the
    // harness starts it with ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS.
    //
    // This is not decoration. Without it the Hub resolves an anonymous
    // "local-developer" who holds no grants, every list is
    // authorization-filtered to empty, and the suite goes green against a
    // console that is rendering nothing. That happened while this was being
    // written and read exactly like a Hub bug.
    extraHTTPHeaders: { "X-Platform-Subject": "operator" },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
