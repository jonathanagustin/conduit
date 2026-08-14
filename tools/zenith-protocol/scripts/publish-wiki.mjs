#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const pluginRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(pluginRoot, "../..");
const owner = process.env.ZENITH_WIKI_OWNER ?? "drengskapur";
const repository = process.env.ZENITH_WIKI_REPOSITORY ?? "agentdriver";
const remote = `https://github.com/${owner}/${repository}.wiki.git`;
const checkOnly = process.argv.includes("--check");
const sources = [
  [join(pluginRoot, "skills/optimize-evidence/references/theory-to-practice.md"), "Whole-System-Optimization.md"],
  [join(pluginRoot, "skills/optimize-evidence/references/evidence-contract.md"), "Optimization-Evidence-Contract.md"],
  [join(pluginRoot, "skills/optimize-evidence/references/experiments-and-bitemporal-twins.md"), "Experiments-and-Bitemporal-Twins.md"],
  [join(pluginRoot, "skills/optimize-evidence/references/dora-in-system-optimization.md"), "DORA-in-System-Optimization.md"],
  [join(pluginRoot, "skills/optimize-evidence/references/value-engineering-and-sensitivity.md"), "Value-Engineering-and-Sensitivity.md"],
];

const run = (command, args, options = {}) => execFileSync(command, args, {
  cwd: options.cwd ?? repoRoot,
  encoding: "utf8",
  stdio: options.stdio ?? ["ignore", "pipe", "pipe"],
  env: process.env,
});
const gitAuth = [
  "-c", "credential.helper=",
  "-c", "credential.helper=!f() { echo username=x-access-token; echo password=$GITHUB_TOKEN; }; f",
];

for (const [source] of sources) readFileSync(source, "utf8");
if (checkOnly) {
  process.stdout.write(`wiki sources valid: ${sources.length} article(s)\n`);
  process.exit(0);
}
if (!process.env.GITHUB_TOKEN) throw new Error("GITHUB_TOKEN is required to enable and publish the wiki");

const response = await fetch(`https://api.github.com/repos/${owner}/${repository}`, {
  method: "PATCH",
  headers: {
    Authorization: `Bearer ${process.env.GITHUB_TOKEN}`,
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "Content-Type": "application/json",
  },
  body: JSON.stringify({ has_wiki: true }),
});
if (!response.ok) throw new Error(`enable wiki: GitHub API ${response.status}`);

try {
  run("git", [...gitAuth, "ls-remote", remote]);
} catch {
  throw new Error(`wiki is enabled but uninitialized; create its first Home page once at https://github.com/${owner}/${repository}/wiki, then rerun`);
}

const work = mkdtempSync(join(tmpdir(), "zenith-wiki-"));
try {
  run("git", [...gitAuth, "clone", "--depth=1", remote, work]);
  const sourceCommit = run("git", ["rev-parse", "HEAD"]).trim();
  for (const [source, target] of sources) cpSync(source, join(work, target));
  writeFileSync(join(work, "Home.md"), `# AgentDriver Zenith Protocol\n\n` +
    `These articles describe the reusable evidence-driven operating model maintained in ` +
    `[${repository}](https://github.com/${owner}/${repository}/tree/${sourceCommit}).\n\n` +
    `- [[Whole-System Optimization]]\n- [[Optimization Evidence Contract]]\n- [[Experiments and Bitemporal Twins]]\n- [[DORA in System Optimization]]\n- [[Value Engineering and Sensitivity]]\n\n` +
    `Published from source commit ${sourceCommit}; repository sources and executable gates remain authoritative.\n`);
  writeFileSync(join(work, "_Sidebar.md"), "- [[Home]]\n- [[Whole-System Optimization]]\n- [[Optimization Evidence Contract]]\n- [[Experiments and Bitemporal Twins]]\n- [[DORA in System Optimization]]\n- [[Value Engineering and Sensitivity]]\n");
  const changed = run("git", ["status", "--porcelain"], { cwd: work }).trim();
  if (!changed) {
    process.stdout.write("wiki already matches canonical sources\n");
  } else {
    run("git", ["add", "Home.md", "_Sidebar.md", ...sources.map(([, target]) => basename(target))], { cwd: work });
    run("git", ["-c", "user.name=agentdriver", "-c", "user.email=actions@users.noreply.github.com", "commit", "-m", `docs: publish Zenith articles from ${sourceCommit.slice(0, 12)}`], { cwd: work });
    run("git", [...gitAuth, "push", "origin", "HEAD"], { cwd: work });
    process.stdout.write(`published ${sources.length} article(s) from ${sourceCommit}\n`);
  }
} finally {
  rmSync(work, { recursive: true, force: true });
}
