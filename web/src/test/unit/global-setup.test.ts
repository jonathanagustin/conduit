import { createServer, type Server } from "node:http";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import type { FullConfig } from "@playwright/test";
import { afterEach, describe, expect, it } from "vitest";

import globalSetup from "../e2e/global-setup.js";

// The refusal this exercises is the one that keeps a green console gate from
// being reported about a build the gate never tested. Playwright's
// `reuseExistingServer` attaches to whatever answers the URL, and before
// #270 that could be another worktree's preview server. Removing the
// `globalSetup` line from playwright.config.ts and running the suite against
// such a server passes, cleanly, in 1.5 seconds -- which is why the assertions
// below are about the refusal rather than about the happy path alone.

const servers: Server[] = [];

afterEach(() => {
  for (const server of servers.splice(0)) {
    server.close();
  }
});

// Serves one body at /console/index.html on an ephemeral port, and answers
// everything else 404, so a check reading some other path fails rather than
// passing by accident.
async function serve(body: string, status = 200): Promise<number> {
  const server = createServer((request, response) => {
    if (request.url === "/console/index.html") {
      response.writeHead(status, { "content-type": "text/html" });
      response.end(body);
      return;
    }
    response.writeHead(404).end();
  });
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("expected a TCP address from the stub server");
  }
  return address.port;
}

// PLATFORM_CONSOLE_OUT_DIR is an absolute path here, which is what
// test-console.sh passes; global-setup resolves it against the package
// directory, and path.resolve leaves an absolute path alone.
async function buildDir(indexHtml?: string): Promise<string> {
  const directory = await mkdtemp(path.join(tmpdir(), "console-build-"));
  if (indexHtml !== undefined) {
    await writeFile(path.join(directory, "index.html"), indexHtml);
  }
  return directory;
}

function configFor(port: number): FullConfig {
  return {
    projects: [{ use: { baseURL: `http://127.0.0.1:${port}/console/` } }],
  } as unknown as FullConfig;
}

async function run(outDir: string, port: number): Promise<void> {
  const previous = process.env["PLATFORM_CONSOLE_OUT_DIR"];
  process.env["PLATFORM_CONSOLE_OUT_DIR"] = outDir;
  try {
    await globalSetup(configFor(port));
  } finally {
    if (previous === undefined) {
      delete process.env["PLATFORM_CONSOLE_OUT_DIR"];
    } else {
      process.env["PLATFORM_CONSOLE_OUT_DIR"] = previous;
    }
  }
}

describe("console preview build identity", () => {
  const build = "<!doctype html><title>Conduit</title><script src=/console/assets/main-abc123.js></script>";

  it("accepts a server serving exactly the build this checkout produced", async () => {
    const port = await serve(build);
    await expect(run(await buildDir(build), port)).resolves.toBeUndefined();
  });

  it("refuses a server serving a different build, naming the port and the way out", async () => {
    // One hashed asset name apart, which is what two checkouts of different
    // source actually differ by -- not something obviously foreign.
    const other = build.replace("main-abc123.js", "main-def456.js");
    const port = await serve(other);
    await expect(run(await buildDir(build), port)).rejects.toThrow(
      /serving a different build.*CONSOLE_TEST_PORT/s,
    );
  });

  it("refuses when this run built nothing, because that means it reused a server it did not start", async () => {
    const port = await serve(build);
    await expect(run(await buildDir(), port)).rejects.toThrow(
      /holds no build.*reused it/s,
    );
  });

  it("refuses a server that answers, but not with the entry document", async () => {
    const port = await serve(build, 500);
    await expect(run(await buildDir(build), port)).rejects.toThrow(/answered 500/);
  });

  it("refuses when nothing is listening rather than reporting a match", async () => {
    // A port nobody holds: opened, its number read, then closed.
    const port = await serve(build);
    servers.splice(0).forEach((server) => server.close());
    await expect(run(await buildDir(build), port)).rejects.toThrow(/could not read/);
  });
});
