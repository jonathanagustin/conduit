import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { FullConfig } from "@playwright/test";

// Refuses to run the suite against a preview server that is serving somebody
// else's build.
//
// `playwright.config.ts` sets `reuseExistingServer` outside CI, which is a real
// convenience while iterating on one checkout: the second `pnpm test:e2e` skips
// a rebuild and attaches to the server the first one left running. What it also
// did, before this existed, was attach to a server started by a *different
// worktree* that happened to hold the same port, and then report that tree's
// results as this one's.
//
// Both halves of that are bad and the quiet half is worse. A run that outlives
// the other worktree's server dies mid-suite with eighty `ECONNREFUSED`s naming
// a port and nothing else -- that is issue #270, and it cost two full local
// verifications. A run that finishes first reports a **green console gate that
// never tested this tree**, and nothing anywhere says so.
//
// The port default is now derived from the checkout (see playwright.config.ts),
// so two worktrees no longer meet by accident. This is the check that makes the
// remaining cases loud: reuse is permitted exactly when the server is serving
// the same bytes this checkout just built, which is the condition that made
// reuse safe in the first place.
export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0]?.use.baseURL;
  if (baseURL === undefined) {
    throw new Error(
      "global setup expected a baseURL to check the served build against",
    );
  }

  // The same default vite.config.ts resolves, from the same directory, so the
  // file read here is the one `vite preview` is serving.
  const outDir = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../../..",
    process.env["PLATFORM_CONSOLE_OUT_DIR"] ?? "../internal/consoleui/dist",
  );
  let built: string;
  try {
    built = await readFile(path.join(outDir, "index.html"), "utf8");
  } catch (cause) {
    // Nothing to compare against, and under `reuseExistingServer` that is
    // itself the answer. Playwright only runs the `webServer` command when the
    // URL does not already respond, so a missing build here means this run
    // attached to a server it did not start. `test-console.sh` points
    // PLATFORM_CONSOLE_OUT_DIR at a fresh temporary directory every time, so
    // under the gate that is the only way this branch is reached.
    throw new Error(
      [
        `${outDir} holds no build, so this run did not build one --`,
        `something was already answering at ${baseURL} and Playwright reused it.`,
        "Whatever that server is serving, it is not this checkout.",
        "Set CONSOLE_TEST_PORT to a free port, or stop the other server.",
      ].join(" "),
      { cause },
    );
  }

  const url = new URL("index.html", baseURL);
  let served: string;
  try {
    const response = await fetch(url, { cache: "no-store" });
    if (!response.ok) {
      throw new Error(`the preview server answered ${response.status}`);
    }
    served = await response.text();
  } catch (cause) {
    throw new Error(
      `could not read ${url} to check which build is being served: ${String(cause)}`,
      { cause },
    );
  }

  // Byte comparison of the entry document, which names every hashed asset the
  // build produced. Two identical trees produce identical hashes, so a genuine
  // reuse of this checkout's own server passes; anything built from different
  // source does not.
  if (served !== built) {
    throw new Error(
      [
        `the server at ${url.origin} is serving a different build than ${outDir}.`,
        "Another checkout is almost certainly using this port, and reusing its",
        "server would report that tree's results as this one's.",
        "Set CONSOLE_TEST_PORT to a free port, or stop the other server.",
      ].join(" "),
    );
  }
}
