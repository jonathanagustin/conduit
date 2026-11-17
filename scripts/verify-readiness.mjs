#!/usr/bin/env node
// Offline evidence freshness and generated-page check. --online additionally
// verifies every active tracker through GitHub; it never writes a hosted status.
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const recordPath = 'docs/operations/readiness-evidence.json';
const documentPath = 'docs/operations/PRODUCTION-READINESS.md';
const sitePath = 'docs-site/src/content/docs/readiness.md';
const roadmapPath = 'docs-site/src/content/docs/roadmap.md';
const day = 86400000;
const issueLink = (number) => `[#${number}](https://github.com/jonathanagustin/conduit/issues/${number})`;
const links = (numbers) => numbers.length ? numbers.map(issueLink).join(', ') : 'No implementation tracker';
const escape = (value) => String(value).replaceAll('|', '\\|').replaceAll('\n', ' ');

export function renderReadiness(record, coverage, site = false) {
  const source = (path) => site
    ? `[${path}](https://github.com/jonathanagustin/conduit/blob/main/${path})`
    : `[${path}](${path.startsWith('docs/operations/') ? path.slice('docs/operations/'.length) : '../../' + path})`;
  let out = site
    ? '---\ntitle: Readiness and limitations\ndescription: The reviewed merged baseline, evidence limits and open production prerequisites.\n---\n\n'
    : '# Production readiness\n\n';
  out += `Status date: ${record.reviewedAt}; reviewed merged baseline \`${record.baselineCommit}\`.\n\n`;
  out += '**Overall status: production readiness is not established by this record.** This is a source and issue review, not a current-tree gate receipt or deployment authorization. Open pull requests are candidates and do not count as merged capabilities.\n\n';
  out += `${record.verification}\n\n`;
  out += `Hosted verification currently cannot allocate runners because of the confirmed account billing/spending refusal (${issueLink(record.hostedBlocker)}). The maintainer selected the complete exact-head local act verifier for review. Retain its receipt and log, compare its head with the PR, and preserve all server-required checks; a skipped or unallocated job is not a PASS. Publication and tenant deployment remain separate manual actions.\n\n`;
  out += '## Verified by gates\n\nThe repository contains the following executable controls. Their presence and prior bounded evidence do not establish a complete PASS at the reviewed baseline.\n\n';
  out += `| Area | Existing control and limit | Source |\n| --- | --- | --- |\n`;
  for (const [area, claim, paths] of [
    ['Correctness', 'Complete local verifier reconciles its gate list with Trunk; no complete baseline receipt is asserted.', ['scripts/verify-local.sh', '.github/workflows/platform-ci.yml']],
    ['Replica durability', 'Transactional PostgreSQL stores, leases and crash/restore tests exist; load, infrastructure-failure and capacity qualification remain bounded by gap 3.', ['scripts/test-postgres-repository.sh']],
    ['Identity and authority', 'OIDC and Agent mTLS/SPIFFE, explicit insecure opt-ins, fail-closed route register and negative authorization tests; interactive human sign-in remains open.', ['internal/hub/composition/authorization.go', 'docs/operations/route-authorization.md']],
    ['Release and supply chain', 'Digest-addressed artifacts, signature policy and release evidence tooling exist; successful publication and target installation are separate evidence.', ['.github/workflows/platform-images.yml', 'docs/architecture/artifacts-and-releases.md']],
    ['Recovery and audit', 'Forward migrations, backup/restore drill and independently anchored append-only audit verification exist; deployment custody and retention remain gap 4.', ['docs/operations/backup-and-restore.md', 'docs/operations/authorization-audit-integrity.md']],
    ['Offline installation', 'Package/schema/chart checks validate definitions; they do not establish disconnected recall operation, gateway success or production capacity.', ['scripts/verify-zarf-package.rb', 'scripts/verify-zarf-chart-equivalence.rb']],
    ['Frontend', 'Native console and browser checks exist; parser/render success does not establish complete operator journeys or accessibility qualification.', ['scripts/test-console.sh']],
  ]) out += `| ${area} | ${claim} | ${paths.map(source).join(', ')} |\n`;
  out += '\n## Blocking gaps\n\nThe nine historical gap numbers are preserved below. Closed historical issues are source history, not active trackers. Each remaining task links to an open issue snapshot; decisions stay decisions.\n\n';
  out += '| Gap | Disposition at baseline | What exists and what remains | Active tracker | Evidence |\n| --- | --- | --- | --- | --- |\n';
  for (const gap of record.gaps) out += `| ${gap.number}. ${escape(gap.area)} | ${gap.disposition} | ${escape(gap.current)} | ${links(gap.issues)} | ${gap.sources.map(source).join(', ')} |\n`;
  out += '\n## Other product completeness work\n\nThese issue groups are additional prerequisites, not assertions that the original issue title still describes all current code. Read their latest implementation comments and linked PRs.\n\n';
  for (const group of record.relatedGroups) out += `- **${group.area}:** ${links(group.issues)}.\n`;
  out += `\n## Contract coverage\n\nThe generated reference contains ${coverage.cliCommands} shipped CLI parser commands, ${coverage.nativeOpenAPIOperations} native OpenAPI operations and ${coverage.protoContracts} protobuf source files. Only ${coverage.routesWithNativeOpenAPI} of ${coverage.registeredRoutes} registered Hub route patterns match a committed native OpenAPI operation. The rest are explicitly uncontracted in the generated route register; schema completion depends on ${issueLink(record.contractPrerequisite)}. No request/response type is invented to fill that count. Protobuf sources are a separate bounded migration surface, not schemas for every REST route.\n\n`;
  out += '## Test types\n\nUnit, race, PostgreSQL concurrency, rollback, contract, authorization, mutation, old-data and version-skew harnesses are present. Actual success is scoped to a recorded command/tree. Property/invariant depth, load/soak, infrastructure failure, real issuer sign-in, cluster gateway/lease, published-package lifecycle and real model-quality evidence remain in the active trackers above. No performance or production-capacity bound is inferred from small fixture counts.\n\n';
  out += '## Historical reconciliation\n\n';
  out += '- Historical #128 is closed; CI and the local verifier now reconcile inner contract commands as well as step names. The old correctness qualification is retired, not repeated as a live defect.\n';
  out += '- Historical #261 and #263 are closed: audit integrity, populated-data and version-skew rehearsals landed. Export, retention decisions, capacity and deployment recovery evidence remain separately scoped above.\n';
  out += '- Historical #264 is closed: the namespaced Spoke RBAC floor shipped. Effective tenant grants, credential lifecycle and independent boundary assurance remain gap 8.\n';
  out += '- Historical #54 is closed: merged PR #277 explicitly fixed freshness only and retained evidence-trust limitations. Gap 9 records that remaining trust-policy decision; do not restore always-scan or confuse external suppression references with Plan approvals.\n';
  out += '- Historical #616 is closed: the isolated workspace boundary exists. A recorded complete build/package/deploy run is separate work.\n\n';
  out += '- #833 is closed through PR #885: declared package artifact display and target-architecture admission shipped. Multiarchitecture declarations and OCI indexes are refused; no mixed-platform package execution is claimed.\n\n';
  out += '## Explicitly out of production scope\n\nNo private upstream Hub-to-Agent compatibility, universal application packaging, mixed-architecture package execution, unreviewed optimizer candidate admission, FIPS certification or independent accreditation is established here. Licensing/visibility decisions remain owner-deferred; this refresh changes none of those statements or distribution rights.\n\n';
  out += '## Residual items owned by a human operator\n\nProvision and rotate deployment credentials and signing trust; select and exercise database backups, retention, recovery and independent checkpoint custody; declare target namespaces and actual Agent grants; authorize tenant access; verify the target network and gateway; and explicitly authorize release/publication/deployment. Repository administration and billing are distinct responsibilities. A green gate supplies evidence only.\n\n';
  out += `## Freshness and issue-state boundary\n\nIssue states were read at \`${record.issueSnapshotAt}\`. This committed snapshot is historical evidence, not a live GitHub response. Offline verification measures the checked tree: it fails after ${record.maximumAgeDays} days, after more than ${record.maximumCommitsBehind} commits beyond the reviewed baseline, for missing/shallow baseline history, or when an active tracker is recorded closed. The scheduled online check additionally reads GitHub's current main SHA and active issue states. It fails when main exceeds the commit bound, its history is unavailable locally, an active tracker is closed, or GitHub cannot supply authoritative state. It never silently refreshes the snapshot or rewrites evidence.\n`;
  if (!site) out += '\nThe [previous readiness record](PRODUCTION-READINESS-HISTORICAL.md) and [external audit](EXTERNAL-AUDIT-2026-08-18.md) preserve the original reasoning and correction history; their historical status claims are superseded by this baseline.\n';
  return out;
}

export function renderRoadmap(record) {
  let out = '---\ntitle: Roadmap\ndescription: Open native product work from the reviewed issue snapshot, with explicit readiness and contract dependencies.\nsidebar:\n  label: Roadmap\n  order: 5\n---\n\n';
  out += `Generated from the same reviewed issue evidence as [Readiness](/conduit/readiness/), observed at \`${record.issueSnapshotAt}\`. GitHub issues are the current tracker; this is a dated index, not a project board or a release promise.\n\n`;
  for (const group of record.relatedGroups) out += `## ${group.area}\n\n${links(group.issues)}.\n\n`;
  out += '## Decisions and deployment evidence\n\n';
  for (const gap of record.gaps) out += `- ${gap.area}: ${links(gap.issues)}.\n`;
  out += '\nA closed workspace-isolation task is not retained as an open follow-on. Recorded application packaging, cluster delivery and recovery evidence remain separate tasks. No issue, candidate branch or passing test authorizes publication or a tenant deployment.\n';
  return out;
}

function realDate(value) {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return NaN;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) && new Date(parsed).toISOString().slice(0, 10) === value ? parsed : NaN;
}

export async function verifyReadiness({ root = ROOT, mainRef = 'HEAD', today = new Date().toISOString().slice(0, 10), online = false, write = false, fetchIssue = githubIssue, fetchMain = githubMain } = {}) {
  const record = JSON.parse(readFileSync(resolve(root, recordPath), 'utf8'));
  const coverage = JSON.parse(readFileSync(resolve(root, 'docs/operations/native-reference-coverage.json'), 'utf8'));
  const failures = [];
  if (record.schemaVersion !== 1 || record.gaps.length !== 9 || new Set(record.gaps.map(gap => gap.number)).size !== 9 || record.gaps.some(gap => !Number.isInteger(gap.number) || gap.number < 1 || gap.number > 9 || !gap.current || !gap.sources.length)) failures.push('the reviewed record must reconcile each historical gap number 1 through 9 with current evidence');
  const now = realDate(today), reviewed = realDate(record.reviewedAt);
  if (!Number.isFinite(now) || !Number.isFinite(reviewed)) failures.push('review dates must be real YYYY-MM-DD dates');
  for (const field of ['maximumAgeDays', 'maximumCommitsBehind', 'maximumIssueSnapshotAgeDays']) {
    if (!Number.isInteger(record[field]) || record[field] < 1 || record[field] > (field === 'maximumCommitsBehind' ? 200 : 90)) failures.push(`${field} must be a bounded positive integer`);
  }
  if (Number.isFinite(now) && Number.isFinite(reviewed)) {
    const age = (now - reviewed) / day;
    if (age < 0 || age > record.maximumAgeDays) failures.push(`readiness review is stale or future-dated (${record.reviewedAt})`);
  }
  const snapshot = realDate(String(record.issueSnapshotAt).slice(0, 10));
  if (!Number.isFinite(snapshot) || snapshot > now || (now - snapshot) / day > record.maximumIssueSnapshotAgeDays) failures.push('committed issue-state snapshot is stale or invalid; a live check does not refresh it');
  const states = new Map();
  for (const issue of record.issues ?? []) {
    if (!Number.isInteger(issue.number) || issue.number < 1 || states.has(issue.number) || !['open','closed'].includes(issue.state)) failures.push('issue snapshot contains invalid, repeated or unknown issue state');
    if (issue.observedAt !== record.issueSnapshotAt) failures.push(`issue #${issue.number} has a different observation time`);
    states.set(issue.number, issue);
  }
  const tracked = new Set([record.hostedBlocker, record.contractPrerequisite, ...record.gaps.flatMap(gap => gap.issues), ...record.relatedGroups.flatMap(group => group.issues)]);
  for (const number of tracked) if (states.get(number)?.state !== 'open') failures.push(`active tracker #${number} is absent or closed in the committed snapshot`);
  for (const gap of record.gaps) for (const path of gap.sources) if (path.includes('..') || !existsSync(resolve(root, path))) failures.push(`gap ${gap.number} has missing evidence source ${path}`);
  const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8', stdio: ['ignore','pipe','pipe'] }).trim();
  const checkDistance = (ref, label = ref) => {
    try {
      git('cat-file', '-e', `${record.baselineCommit}^{commit}`);
      git('merge-base', '--is-ancestor', record.baselineCommit, ref);
      const behind = Number(git('rev-list', '--count', `${record.baselineCommit}..${ref}`));
      if (!Number.isSafeInteger(behind) || behind > record.maximumCommitsBehind) failures.push(`baseline lags ${label} by ${behind} commits (maximum ${record.maximumCommitsBehind})`);
    } catch {
      failures.push(`baseline history is unavailable, shallow, or not an ancestor of ${label}; fetch complete history before checking readiness`);
    }
  };
  if (!/^[a-f0-9]{40}$/.test(record.baselineCommit)) failures.push('baselineCommit must be an exact commit SHA');
  else checkDistance(mainRef);
  if (online) {
    try {
      const liveMain = await fetchMain();
      if (typeof liveMain !== 'string' || !/^[a-f0-9]{40}$/.test(liveMain)) failures.push('GitHub did not return an exact current-main SHA');
      else checkDistance(liveMain, 'live main');
    } catch { failures.push('live main state unavailable; no current-main freshness PASS is claimed'); }
    const numbers = [...tracked];
    for (let index = 0; index < numbers.length; index += 4) await Promise.all(numbers.slice(index,index + 4).map(async number => {
      try {
        const issue = await fetchIssue(number);
        if (issue.number !== number || issue.state !== 'open') failures.push(`GitHub reports active tracker #${number} closed or invalid; reconcile the gap before updating evidence`);
      } catch { failures.push(`live issue state unavailable for #${number}; no current issue-state PASS is claimed`); }
    }));
  }
  if (failures.length) throw new Error(failures.join('\n'));
  for (const [path, site] of [[documentPath,false],[sitePath,true]]) {
    const generated = renderReadiness(record, coverage, site);
    if (write) writeFileSync(resolve(root,path), generated);
    else if (readFileSync(resolve(root,path),'utf8') !== generated) throw new Error(`${path} is stale; review evidence then run node scripts/verify-readiness.mjs --write`);
  }
  const roadmap = renderRoadmap(record);
  if (write) writeFileSync(resolve(root,roadmapPath),roadmap);
  else if (readFileSync(resolve(root,roadmapPath),'utf8') !== roadmap) throw new Error(`${roadmapPath} is stale; regenerate the reviewed issue index`);
  return `PASS readiness: baseline ${record.baselineCommit}, ${tracked.size} active trackers; ${online ? 'live issue states verified' : 'committed issue snapshot only (offline)'}`;
}

async function githubResource(path) {
  const token = process.env.GH_TOKEN || process.env.GITHUB_TOKEN;
  const headers = { Accept: 'application/vnd.github+json', 'User-Agent': 'conduit-readiness-verification' };
  if (token) headers.Authorization = `Bearer ${token}`;
  const response = await fetch(`https://api.github.com/repos/jonathanagustin/conduit/${path}`, { headers, signal: AbortSignal.timeout(15000) });
  if (!response.ok) throw new Error(`GitHub lookup failed (${response.status})`);
  return response.json();
}

async function githubIssue(number) { return githubResource(`issues/${number}`); }
async function githubMain() { return (await githubResource('git/ref/heads/main')).object?.sha; }

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    console.log(await verifyReadiness({ write: process.argv.includes('--write'), online: process.argv.includes('--online'), mainRef: process.env.CONDUIT_READINESS_MAIN_REF || 'HEAD', today: process.env.CONDUIT_READINESS_TODAY || new Date().toISOString().slice(0,10) }));
  } catch (error) { console.error(`FAIL readiness: ${error.message}`); process.exitCode = 1; }
}
