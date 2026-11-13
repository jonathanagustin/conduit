#!/usr/bin/env node
// Only PR labels and the existing Trunk workflow may be written here. Tags,
// Releases and artifact publication remain owned by the manual release path.
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const pending = "autorelease: pending";
const tagged = "autorelease: tagged";
export const releaseBranch = "release-please--branches--main";
export function assertPreparationWrite(method, path) {
  if (method === "GET") return;
  const prefix = "repos/[\\w.-]+/[\\w.-]+/";
  if (method === "POST" && new RegExp(`^${prefix}(issues/[1-9][0-9]*/labels|actions/workflows/platform-ci\\.yml/dispatches)$`).test(path)) return;
  if (method === "DELETE" && new RegExp(`^${prefix}issues/[1-9][0-9]*/labels/autorelease%3A%20pending$`).test(path)) return;
  throw new Error("release preparation cannot write this API endpoint");
}
const semver = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;

async function pages(api, path) {
  const result = [];
  for (let page = 1; ; page++) {
    const batch = await api("GET", `${path}&per_page=100&page=${page}`);
    result.push(...batch);
    if (batch.length < 100) return result;
  }
}

async function preparations(api, repository, state) {
  const issues = await pages(api, `repos/${repository}/issues?state=${state}&labels=${encodeURIComponent(pending)}`);
  const result = [];
  for (const issue of issues) {
    if (!issue.pull_request) continue;
    const pr = await api("GET", `repos/${repository}/pulls/${issue.number}`);
    if (pr.base.ref === "main" && pr.head.ref === releaseBranch && pr.head.repo?.full_name === repository) result.push(pr);
  }
  return result;
}

function decoded(file) {
  if (file.encoding !== "base64" || typeof file.content !== "string") throw new Error("expected a base64 repository file");
  return Buffer.from(file.content, "base64").toString("utf8");
}

export async function reconcilePublished(api, repository) {
  const prs = (await preparations(api, repository, "closed")).filter(pr => pr.merged_at);
  if (!prs.length) return;
  const releases = await pages(api, `repos/${repository}/releases?`);
  for (const pr of prs) {
    const manifest = JSON.parse(decoded(await api("GET", `repos/${repository}/contents/.release-please-manifest.json?ref=${pr.merge_commit_sha}`)));
    const version = manifest["."];
    if (!semver.test(version)) throw new Error("release preparation has an invalid version");
    const tag = `v${version}`;
    if (!releases.some(release => release.tag_name === tag && !release.draft && release.published_at)) continue;
    const ref = await api("GET", `repos/${repository}/git/ref/tags/${tag}`);
    // The manual release workflow creates lightweight tags. Unexpected tag
    // forms fail closed instead of silently changing the tag-owner contract.
    if (ref.object.type !== "commit") throw new Error("manual release tag must point directly at a commit");
    const comparison = await api("GET", `repos/${repository}/compare/${pr.merge_commit_sha}...${ref.object.sha}`);
    if (!["ahead", "identical"].includes(comparison.status)) throw new Error("published tag does not contain the release preparation");
    const publishedVersion = decoded(await api("GET", `repos/${repository}/contents/VERSION?ref=${ref.object.sha}`)).trim();
    if (publishedVersion !== version) throw new Error("published tag and preparation versions disagree");
    await api("POST", `repos/${repository}/issues/${pr.number}/labels`, { labels: [tagged] });
    await api("DELETE", `repos/${repository}/issues/${pr.number}/labels/${encodeURIComponent(pending)}`);
    console.log(`Marked release preparation #${pr.number} published as ${tag}`);
  }
}

export async function ensureTrunk(api, repository) {
  const prs = await preparations(api, repository, "open");
  if (prs.length > 1) throw new Error("multiple pending release preparations; reconcile before dispatching");
  for (const pr of prs) {
    if (pr.state !== "open" || !/^[a-f0-9]{40}$/.test(pr.head.sha)) throw new Error("invalid release preparation head");
    const statuses = await api("GET", `repos/${repository}/commits/${pr.head.sha}/status`);
    if (statuses.statuses.find(status => status.context === "Trunk gate")?.state === "success") continue;
    const runs = await api("GET", `repos/${repository}/actions/workflows/platform-ci.yml/runs?head_sha=${pr.head.sha}&per_page=100`);
    if (runs.workflow_runs.some(run => run.head_sha === pr.head.sha && run.status !== "completed")) continue;
    await api("POST", `repos/${repository}/actions/workflows/platform-ci.yml/dispatches`, { ref: pr.head.ref, inputs: { pull_request: String(pr.number) } });
    console.log(`Dispatched Trunk for release preparation #${pr.number} at ${pr.head.sha}`);
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const repository = process.env.GITHUB_REPOSITORY;
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository ?? "")) throw new Error("GITHUB_REPOSITORY is required");
  const api = async (method, path, body) => {
    assertPreparationWrite(method, path);
    const args = ["api", "--method", method, path];
    if (body) args.push("--input", "-");
    const output = execFileSync(fileURLToPath(new URL("./retry-github-api.sh", import.meta.url)), ["gh", ...args], { encoding: "utf8", input: body ? JSON.stringify(body) : undefined });
    return output.trim() ? JSON.parse(output) : undefined;
  };
  if (process.argv[2] === "reconcile") await reconcilePublished(api, repository);
  else if (process.argv[2] === "verify") await ensureTrunk(api, repository);
  else throw new Error("expected reconcile or verify");
}
