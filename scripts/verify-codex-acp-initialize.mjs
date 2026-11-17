#!/usr/bin/env node
// Real bundled adapter smoke: one initialize request, no authentication or model turn.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const requireWeb = createRequire(join(root, "web/package.json"));
const packageFile = requireWeb.resolve("@agentclientprotocol/codex-acp/package.json");
const adapter = JSON.parse(await readFile(packageFile, "utf8"));
const codex = JSON.parse(await readFile(createRequire(packageFile).resolve("@openai/codex/package.json"), "utf8"));
const manifest = JSON.parse(await readFile(join(root, "web/package.json"), "utf8"));
assert.equal(adapter.version, manifest.dependencies[adapter.name], "install the locked web dependencies first");
const entry = resolve(dirname(packageFile), adapter.bin["codex-acp"]);
const protocolSource = await readFile(join(root, "web/src/api/protocol.ts"), "utf8");
const protocolVersion = Number(protocolSource.match(/PINNED_ACP_PROTOCOL_VERSION = (\d+) as const/)?.[1]);
assert.ok(Number.isSafeInteger(protocolVersion), "missing pinned ACP protocol version");
assert.notEqual(process.platform, "win32", "this smoke requires POSIX process-group cleanup");

const profile = await mkdtemp(join(tmpdir(), "conduit-acp-initialize-"));
const environment = {
  HOME: profile,
  CODEX_HOME: join(profile, "codex"),
  XDG_CONFIG_HOME: join(profile, "config"),
  XDG_DATA_HOME: join(profile, "data"),
  PATH: [dirname(process.execPath), join(root, "web/node_modules/.bin"), "/usr/bin", "/bin"].join(delimiter),
};
for (const directory of [environment.CODEX_HOME, environment.XDG_CONFIG_HOME, environment.XDG_DATA_HOME]) {
  await mkdir(directory, { recursive: true });
}
const request = {
  jsonrpc: "2.0", id: 1, method: "initialize",
  params: {
    protocolVersion,
    clientCapabilities: { fs: { readTextFile: true, writeTextFile: true }, terminal: true },
    clientInfo: { name: "conduit-acp-initialize-smoke", version: "1" },
  },
};
const started = Date.now();
const child = spawn(process.execPath, [entry], {
  cwd: profile, env: environment, detached: true, stdio: ["pipe", "pipe", "pipe"],
});
let response;
let failure;
let output = "";
let stderr = "";
let stopping = false;
function signalGroup(signal) {
  if (!child.pid) return;
  try { process.kill(-child.pid, signal); } catch (error) {
    if (error.code !== "ESRCH") failure ??= error;
  }
}
function stop(error) {
  failure ??= error;
  if (stopping) return;
  stopping = true;
  signalGroup("SIGTERM");
}
const timeout = setTimeout(() => stop(new Error("initialize exceeded 15 seconds")), 15_000);
const killTimeout = setTimeout(() => signalGroup("SIGKILL"), 17_000);
child.stdout.setEncoding("utf8");
child.stderr.setEncoding("utf8");
child.stdout.on("data", (chunk) => {
  output += chunk;
  if (output.length > 65_536) return stop(new Error("ACP stdout exceeded 64 KiB"));
  while (output.includes("\n")) {
    const newline = output.indexOf("\n");
    const line = output.slice(0, newline);
    output = output.slice(newline + 1);
    try {
      const message = JSON.parse(line);
      assert.equal(message.jsonrpc, "2.0");
      assert.equal(message.id, request.id);
      assert.equal(message.method, undefined, "initialize must not invoke a client tool");
      assert.equal(response, undefined, "unexpected additional ACP response");
      response = message;
      stop();
    } catch (error) { stop(error); }
  }
});
child.stderr.on("data", (chunk) => { stderr = (stderr + chunk).slice(0, 8192); });
child.stdin.on("error", (error) => stop(error));
const closed = new Promise((resolveClosed) => {
  child.once("error", (error) => { failure ??= error; });
  child.once("close", (code, signal) => resolveClosed({ code, signal }));
});
// Keep stdin open until the response: EOF makes the adapter shut down before
// the asynchronous Codex app-server initialization completes.
child.stdin.write(`${JSON.stringify(request)}\n`);
try {
  const exit = await closed;
  if (failure) throw failure;
  assert.ok(response, "adapter exited without an initialize response");
  assert.equal(response.error, undefined, "adapter returned an initialize error");
  assert.equal(response.result.protocolVersion, protocolVersion);
  assert.equal(response.result.agentInfo.name, adapter.name);
  assert.equal(response.result.agentInfo.version, adapter.version);
  console.log(JSON.stringify({
    schemaVersion: 1,
    scope: "initialize-only; no authentication, session, prompt, file, terminal or model call",
    recordedAt: new Date().toISOString(),
    command: "node scripts/verify-codex-acp-initialize.mjs",
    platform: process.platform, arch: process.arch, nodeVersion: process.version,
    adapter: { name: adapter.name, version: adapter.version, entrySha256: createHash("sha256").update(await readFile(entry)).digest("hex") },
    codexVersion: codex.version,
    lockSha256: createHash("sha256").update(await readFile(join(root, "web/pnpm-lock.yaml"))).digest("hex"),
    environment: "fresh temporary HOME/CODEX_HOME/XDG directories; no inherited credentials or configuration",
    request, response, elapsedMs: Date.now() - started, exit,
    stderr: stderr.replaceAll(profile, "$PROFILE"),
  }, null, 2));
} finally {
  clearTimeout(timeout);
  clearTimeout(killTimeout);
  signalGroup("SIGKILL");
  await rm(profile, { recursive: true, force: true });
}
