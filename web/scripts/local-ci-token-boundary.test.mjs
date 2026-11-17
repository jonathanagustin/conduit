import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";

test("candidate imports and act both receive no caller GitHub tokens", () => {
  const scratch = mkdtempSync(join(tmpdir(), "conduit-local-ci-token-"));
  try {
    const candidate = join(scratch, "candidate");
    const reports = join(scratch, "reports");
    mkdirSync(join(candidate, "scripts"), { recursive: true });
    mkdirSync(join(candidate, "web/scripts"), { recursive: true });
    mkdirSync(reports);
    const wrapper = join(candidate, "scripts/run-local-ci.sh");
    writeFileSync(
      wrapper,
      readFileSync(new URL("../../scripts/run-local-ci.sh", import.meta.url)),
    );
    const presence = `({ghTokenPresent: Object.hasOwn(process.env, "GH_TOKEN"), githubTokenPresent: Object.hasOwn(process.env, "GITHUB_TOKEN")})`;
    writeFileSync(
      join(candidate, "web/scripts/local-ci-browser-profile.mjs"),
      `import {writeFileSync} from "node:fs";
writeFileSync(process.env.TOKEN_BOUNDARY_REPORT + "/helper.json", JSON.stringify(${presence}));
export function localCiBrowserLaunchOptions() { return {args: ["--no-zygote"]}; }
`,
    );
    for (const name of ["playwright.config.ts", "playwright.live.config.ts"]) {
      writeFileSync(join(candidate, "web", name), "// controlled fixture\n");
    }
    const act = join(scratch, "act");
    writeFileSync(
      act,
      `#!/usr/bin/env node
const {writeFileSync} = require("node:fs");
if (process.argv[2] === "--version") {
  console.log("act version 0.2.97");
} else {
  writeFileSync(process.env.TOKEN_BOUNDARY_REPORT + "/act.json", JSON.stringify(${presence}));
  console.log("CONTROLLED_ACT_STOP_WITHOUT_RUNNING_CI");
  process.exit(23);
}
`,
    );
    chmodSync(act, 0o755);
    // Whitelist the child environment: these are fixed synthetic sentinels,
    // never values read from the caller's real credential environment.
    const environment = {
      PATH: `${dirname(process.execPath)}:${process.env.PATH ?? ""}`,
      TMPDIR: reports,
      GIT_CONFIG_NOSYSTEM: "1",
      GIT_CONFIG_GLOBAL: "/dev/null",
      GIT_TERMINAL_PROMPT: "0",
      ACT_BIN: act,
      TOKEN_BOUNDARY_REPORT: reports,
      CONDUIT_LOCAL_CI_BROWSER_PROFILE: "qemu-x86_64-no-zygote",
      GH_TOKEN: "CONDUIT_SYNTHETIC_GH_SENTINEL",
      GITHUB_TOKEN: "CONDUIT_SYNTHETIC_GITHUB_SENTINEL",
    };
    const git = (...args) =>
      execFileSync("git", args, {
        cwd: candidate,
        env: environment,
        stdio: "pipe",
      });
    git("init", "--quiet");
    git("add", ".");
    git(
      "-c",
      "user.name=Conduit test",
      "-c",
      "user.email=test@example.invalid",
      "-c",
      "commit.gpgsign=false",
      "-c",
      "core.hooksPath=/dev/null",
      "commit",
      "--quiet",
      "-m",
      "test: controlled local CI fixture",
    );
    const result = spawnSync("bash", [wrapper], {
      cwd: candidate,
      env: environment,
      encoding: "utf8",
      timeout: 30_000,
    });
    assert.equal(result.status, 23, result.stdout + result.stderr);
    assert.match(result.stdout, /^CONTROLLED_ACT_STOP_WITHOUT_RUNNING_CI$/m);
    for (const stage of ["helper", "act"]) {
      assert.deepEqual(
        JSON.parse(readFileSync(join(reports, stage + ".json"), "utf8")),
        { ghTokenPresent: false, githubTokenPresent: false },
        `${stage} must not receive either token variable`,
      );
    }
    const runDirectories = readdirSync(reports).filter((name) =>
      name.startsWith("conduit-local-ci."),
    );
    assert.equal(runDirectories.length, 1);
    assert.ok(
      !readdirSync(join(reports, runDirectories[0])).includes("receipt.json"),
      "the controlled failed act invocation cannot write a success receipt",
    );
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});
