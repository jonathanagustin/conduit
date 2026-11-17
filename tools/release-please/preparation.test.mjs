import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import { test } from "node:test";
import { Manifest } from "release-please";

const root = fileURLToPath(new URL("../../", import.meta.url));
const config = JSON.parse(readFileSync(resolve(root, "release-please-config.json"), "utf8"));
const manifest = {".": "0.17.5"}; // Immutable bootstrap semantics, even after future releases.
const versionFiles = ["VERSION", "deploy/helm/platform-hub/Chart.yaml", "deploy/helm/platform-spoke/Chart.yaml", ".release-please-manifest.json"];

// The actual pinned Release Please engine plans and renders the PR. Only the
// GitHub persistence boundary is replaced; unexpected network writes fail.
class FixtureGitHub {
  repository = {owner: "jonathanagustin", repo: "conduit", defaultBranch: "main"};
  commits = [];
  prs = [];
  creates = 0;
  updates = 0;
  files = new Map();
  async getFileJson(path) { if (path === ".release-please-manifest.json") return manifest; return JSON.parse(readFileSync(resolve(root, path), "utf8")); }
  async *releaseIterator() { yield {tagName: `v${manifest["."]}`, sha: config["bootstrap-sha"], notes: "Earlier release"}; }
  async *mergeCommitIterator() {
    yield* this.commits;
    yield {sha: config["bootstrap-sha"], message: `chore: release ${manifest["."]}`, files: ["VERSION"]};
    throw new Error("history before the published release must not be replayed");
  }
  async *pullRequestIterator(_branch, state) { if (state === "OPEN") yield* this.prs; }
  async createPullRequest(pr, _branch, message, updates) {
    this.creates++;
    assert.equal(pr.headBranchName, "release-please--branches--main--components--conduit");
    assert.match(message, /^chore\(main\): release \d+\.\d+\.\d+$/);
    assert.equal(this.prs.length, 0, "must not open duplicate release PRs");
    this.render(updates);
    const stored = {...pr, number: 1}; this.prs.push(stored); return stored;
  }
  async updatePullRequest(number, candidate) {
    this.updates++; this.render(candidate.updates);
    const stored = {...this.prs[0], number, title: candidate.title.toString(), body: candidate.body.toString()};
    this.prs[0] = stored; return stored;
  }
  render(updates) {
    this.files = new Map();
    for (const update of updates) {
      let content;
      try { content = readFileSync(resolve(root, update.path), "utf8"); } catch (error) { if (error.code !== "ENOENT" || !update.createIfMissing) throw error; }
      this.files.set(update.path, update.updater.updateContent(content));
    }
  }
  createRelease() { throw new Error("release preparation may not create releases"); }
}

function commit(message, index = 1) { return {sha: String(index).padStart(40, "0"), message, files: ["internal/example.go"]}; }
async function engine(github) { return Manifest.fromManifest(github, "main"); }

for (const [message, expected] of [["feat: add a capability", "0.18.0"], ["fix: repair a defect", "0.17.6"], ["feat!: change the contract\n\nBREAKING CHANGE: old clients need migration", "0.18.0"]]) {
 test(`${message.split("\n")[0]} prepares ${expected}`, async () => {
  const github = new FixtureGitHub(); github.commits = [commit(message)];
  const prs = await (await engine(github)).createPullRequests();
  assert.equal(prs.length, 1);
  assert.equal(github.files.get("VERSION"), `${expected}\n`);
  assert.equal(JSON.parse(github.files.get(".release-please-manifest.json"))["."], expected);
  for (const chart of versionFiles.filter(path => path.endsWith("Chart.yaml"))) {
   assert.match(github.files.get(chart), new RegExp(`^version: ${expected.replaceAll(".", "\\.")}$`, "m"));
   assert.match(github.files.get(chart), new RegExp(`^appVersion: ["']?${expected.replaceAll(".", "\\.")}["']?$`, "m"));
  }
  // scripts/verify-zarf-package.rb requires the UDS bundle to equal VERSION, so
  // a release PR that leaves it behind fails Trunk on its own head. The bundle
  // uses the generic updater: the HCL source requires the generic version marker updater.
  const bundle = github.files.get("deploy/zarf/bundle.uds.hcl");
  const escaped = expected.replaceAll(".", "\\.");
  assert.match(bundle, new RegExp(`^  version = "${escaped}" # x-release-please-version$`, "m"));
  assert.match(bundle, /version = local\.version/);
  assert.match(bundle, /zarf-package-platform-runtime-\$\{sys\.arch\}-\$\{local\.version\}\.tar\.zst/);
  assert.match(bundle, /values_files = \["runtime-values\.yaml"\]/);
  assert.match(bundle, /certificate_identity = "https:\/\/github\.com\/jonathanagustin\/conduit\/\.github\/workflows\/platform-images\.yml@refs\/tags\/v\$\{local\.version\}"/);
  assert.match(github.files.get("CHANGELOG.md"), new RegExp(expected.replaceAll(".", "\\.")));
  assert.doesNotMatch(prs[0].body, /generated with|beep|co-authored-by|openai|claude|codex/i);
  execFileSync(resolve(root, "scripts/verify-pr-fidelity.sh"), {input: prs[0].body});
 });
}

test("non-release commits are a no-op", async () => {
 const github = new FixtureGitHub(); github.commits = [commit("docs: clarify setup"), commit("chore(ci): maintain tooling", 2)];
 assert.deepEqual(await (await engine(github)).createPullRequests(), []);
 assert.equal(github.creates, 0); assert.equal(github.updates, 0);
});

test("repeated runs update one PR and unchanged runs do not write", async () => {
 const github = new FixtureGitHub(); github.commits = [commit("fix: repair a defect")];
 await (await engine(github)).createPullRequests();
 assert.deepEqual(await (await engine(github)).createPullRequests(), []);
 github.commits.unshift(commit("feat: add a capability", 2));
 await (await engine(github)).createPullRequests();
 assert.equal(github.creates, 1); assert.equal(github.updates, 1); assert.equal(github.prs.length, 1);
 assert.equal(github.files.get("VERSION"), "0.18.0\n");
});
