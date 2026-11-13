#!/usr/bin/env node
// Validate visible commit/PR metadata; implementation references to tools are
// ordinary prose, not attribution. GitHub mode also pins the API response to
// the head that the existing authenticator approved.
import { execFileSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const conventional = /^[a-z][a-z0-9-]*(?:\([^\r\n()\x00-\x1f]+\))?!?: \S[^\r\n]*$/;
const automatedNames = /^(?:claude(?: (?:code|opus|sonnet|haiku)(?: [\d.]+)?)?|cursor ?agent|(?:openai )?codex(?: cli)?|openai|(?:github )?copilot)(?:\[bot\])?$/i;
const automatedEmails = /^(?:noreply|claude|cursoragent|codex|copilot)@(?:anthropic\.com|cursor\.com|openai\.com)$|^[0-9]+\+(?:claude|github-copilot)\[bot\]@users\.noreply\.github\.com$/i;
export const conventionalSubject = (value) => typeof value === "string" && conventional.test(value);
export const attributionIdentity = (value) => Boolean(value && (automatedNames.test(value.name?.trim() ?? "") || automatedEmails.test(value.email?.trim() ?? "")));

export function attributionFooter(message) {
  let fence = null;
  for (const raw of String(message).split(/\r?\n/)) {
    const marker = /^\s*(`{3,}|~{3,})/.exec(raw)?.[1];
    if (marker) {
      if (!fence) fence = marker;
      else if (marker[0] === fence[0] && marker.length >= fence.length) fence = null;
      continue;
    }
    if (fence || /^\s*>/.test(raw)) continue;
    const line = raw.replace(/^[\s_*🤖-]+/u, "").replace(/[_*\s]+$/u, "");
    if (/^(?:claude|codex|cursor)[ -]session:/i.test(line)) return true;
    if (/^(?:generated (?:by|with)|created with|made with)\b/i.test(line) && /claude|codex|copilot|cursor|openai|anthropic/i.test(line)) return true;
    const trailer = /^co-authored-by:\s*(.+)$/i.exec(line)?.[1];
    if (trailer) {
      const identity = /^(.*?)\s*<([^>]+)>$/.exec(trailer);
      if (attributionIdentity(identity ? { name: identity[1], email: identity[2] } : { name: trailer })) return true;
    }
  }
  return false;
}

export function commitDefects(record) {
  const value = record.commit ?? record;
  const defects = [];
  if (!/^[a-f0-9]{40}$/.test(record.sha ?? "")) defects.push("missing commit identity");
  if (!conventionalSubject(value.message?.split(/\r?\n/)[0])) defects.push("expected a Conventional Commit subject");
  for (const kind of ["author", "committer"]) {
    if (!value[kind]?.name || !value[kind]?.email) defects.push(`missing ${kind} metadata`);
    else if (attributionIdentity(value[kind])) defects.push(`forbidden attribution in ${kind} metadata`);
  }
  if (attributionFooter(value.message)) defects.push("forbidden attribution/session footer");
  return defects;
}

export function proposalDefects(proposal, commits, expectedSHA) {
  const defects = [];
  if (proposal.head?.sha !== expectedSHA || commits.at(-1)?.sha !== expectedSHA ||
      !Number.isSafeInteger(proposal.commits) || proposal.commits !== commits.length ||
      new Set(commits.map((record) => record.sha)).size !== commits.length) defects.push("pull request head moved or commit list is incomplete");
  if (!conventionalSubject(proposal.title)) defects.push("expected a Conventional Commit pull request title");
  if (attributionFooter(proposal.body ?? "")) defects.push("forbidden pull request attribution/session footer");
  for (const record of commits) for (const defect of commitDefects(record)) defects.push(`${record.sha?.slice(0, 12) ?? "unknown"}: ${defect}`);
  return defects;
}

function githubMetadata() {
  const repo = process.env.GITHUB_REPOSITORY ?? "";
  const number = process.env.PULL_REQUEST_NUMBER ?? "";
  const expected = process.env.GATE_SHA ?? "";
  if (!/^[\w.-]+\/[\w.-]+$/.test(repo) || !/^[1-9][0-9]*$/.test(number) || !/^[a-f0-9]{40}$/.test(expected)) throw new Error("authenticated repository, pull request number, and GATE_SHA are required");
  const retry = join(dirname(fileURLToPath(import.meta.url)), "retry-github-api.sh");
  const api = (path, paginate = false) => {
    try {
      // tojson emits one complete compact JSON value per page, including on
      // older gh clients that support pagination but not --slurp.
      const output = execFileSync(retry, ["gh", "api", path, ...(paginate ? ["--paginate", "--jq", "tojson"] : [])], { encoding: "utf8", maxBuffer: 64 << 20, stdio: ["ignore", "pipe", "pipe"] });
      return paginate ? output.trim().split(/\r?\n/).filter(Boolean).map((line) => JSON.parse(line)) : JSON.parse(output);
    } catch {
      throw new Error("unable to read authenticated commit metadata");
    }
  };
  const proposal = api(`repos/${repo}/pulls/${number}`);
  const pages = api(`repos/${repo}/pulls/${number}/commits`, true);
  if (!Array.isArray(pages) || pages.some((page) => !Array.isArray(page))) throw new Error("GitHub returned an invalid paginated commit list");
  const commits = pages.flat();
  const defects = proposalDefects(proposal, commits, expected);
  if (api(`repos/${repo}/pulls/${number}`).head?.sha !== expected) defects.push("pull request head moved while metadata was read");
  return { defects, count: commits.length };
}

function localMetadata() {
  const git = (args) => execFileSync("git", args, { encoding: "utf8", maxBuffer: 64 << 20 }).trim();
  const base = git(["merge-base", "HEAD", "origin/main"]);
  const head = git(["rev-parse", "HEAD"]);
  const range = base === head ? ["-1", "HEAD"] : [`${base}..HEAD`];
  const parts = git(["log", "--reverse", "--format=%H%x00%an%x00%ae%x00%cn%x00%ce%x00%B%x00", ...range]).split("\0");
  const commits = [];
  for (let index = 0; index + 5 < parts.length; index += 6) commits.push({ sha: parts[index].trim(), author: { name: parts[index + 1], email: parts[index + 2] }, committer: { name: parts[index + 3], email: parts[index + 4] }, message: parts[index + 5] });
  if (!commits.length) throw new Error("no local commit metadata was read");
  return { count: commits.length, defects: commits.flatMap((record) => commitDefects(record).map((defect) => `${record.sha.slice(0, 12)}: ${defect}`)) };
}

export function main() {
  try {
    const result = process.argv.includes("--github") ? githubMetadata() : localMetadata();
    if (result.defects.length) throw new Error(result.defects.join("\n"));
    console.log(`PASS commit metadata: ${result.count} commit(s), conventional subjects and clean attribution`);
  } catch (error) {
    // Never print API response bodies, subprocess environments, or full commit
    // messages merely to explain a metadata policy failure.
    console.error(error instanceof Error && !Object.hasOwn(error, "stderr") ? error.message : "unable to read authenticated commit metadata");
    process.exitCode = 1;
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
