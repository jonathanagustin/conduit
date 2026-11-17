#!/usr/bin/env bash
# Run the complete CI gate locally, bound to one clean commit. No GitHub writes.
set -euo pipefail
# Match the hosted runner creation mask; permission-negative tests set modes explicitly.
umask 022

runner_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="$(git rev-parse --show-toplevel)"
cd "${root}"
act_bin="${ACT_BIN:-act}"
if [[ "$("${act_bin}" --version)" != 'act version 0.2.97' ]]; then
  echo 'install nektos/act v0.2.97 or set ACT_BIN to that version' >&2
  exit 1
fi
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  echo 'local CI requires a clean checkout, including untracked files' >&2
  exit 1
fi
if [[ -n "$(git -C "${runner_root}" status --porcelain --untracked-files=normal)" ]]; then
  echo 'local CI tooling must also be committed and clean' >&2
  exit 1
fi
expected_tooling_sha="$(git -C "${runner_root}" rev-parse HEAD)"
expected_sha="${1:-$(git rev-parse HEAD)}"
if [[ ! "${expected_sha}" =~ ^[a-f0-9]{40}$ ]] || [[ "$(git rev-parse HEAD)" != "${expected_sha}" ]]; then
  echo 'expected SHA must be the exact checked-out commit' >&2
  exit 1
fi
# Keep durable reports separate from disposable Linux-native fixture scratch.
report_root="${CONDUIT_LOCAL_CI_REPORT_ROOT:-${TMPDIR:-/tmp}}"
if [[ "${report_root}" != /* || ! -d "${report_root}" ]]; then
  echo 'local CI report root must be an existing absolute directory' >&2
  exit 1
fi
report_dir="$(mktemp -d "${report_root%/}/conduit-local-ci.XXXXXXXX")"
export CONDUIT_LOCAL_CI_REPORT_DIR="${report_dir}"
printf 'Local CI commit: %s\nEvidence directory: %s\n' "${expected_sha}" "${report_dir}"
# A nondefault profile must be supported by the candidate, even when this
# wrapper is newer tooling from another checkout. Refuse before the full run.
env -u GH_TOKEN -u GITHUB_TOKEN node --input-type=module - "${root}" "${report_dir}" <<'JS'
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
const [candidate, directory] = process.argv.slice(2);
const profile = process.env.CONDUIT_LOCAL_CI_BROWSER_PROFILE || 'default';
const evidence = {profile, helperSha256: null, configurationSha256: {}};
if (process.env.CONDUIT_LOCAL_CI_BROWSER_PROFILE) {
  const helper = join(candidate, 'web/scripts/local-ci-browser-profile.mjs');
  const { localCiBrowserLaunchOptions } = await import(pathToFileURL(helper));
  localCiBrowserLaunchOptions({...process.env, ACT: 'true'});
  const hash = path => createHash('sha256').update(readFileSync(path)).digest('hex');
  evidence.helperSha256 = hash(helper);
  for (const name of ['playwright.config.ts', 'playwright.live.config.ts']) {
    evidence.configurationSha256[name] = hash(join(candidate, 'web', name));
  }
}
writeFileSync(join(directory, 'browser-profile.json'), JSON.stringify(evidence, null, 2) + '\n', {mode: 0o600});
JS
# Never load act's implicit .env/.secrets files or pass a GitHub write token.
# The host executor uses the same pinned toolchain as verify-local.sh. Docker
# remains necessary for its PostgreSQL and isolated fixture checks.
env -u GH_TOKEN -u GITHUB_TOKEN "${act_bin}" workflow_dispatch \
  --workflows "${runner_root}/.github/workflows/local-ci.yml" --directory "${root}" \
  --job verify --platform conduit-local=-self-hosted \
  --env "CONDUIT_LOCAL_CI_WORKTREE=${root}" \
  --env "CONDUIT_LOCAL_CI_BROWSER_PROFILE=${CONDUIT_LOCAL_CI_BROWSER_PROFILE:-}" \
  --env-file /dev/null --secret-file /dev/null \
  2>&1 | tee "${report_dir}/act.log"
if [[ "$(git rev-parse HEAD)" != "${expected_sha}" ]] || [[ -n "$(git status --porcelain --untracked-files=normal)" ]] ||
   [[ "$(git -C "${runner_root}" rev-parse HEAD)" != "${expected_tooling_sha}" ]] ||
   [[ -n "$(git -C "${runner_root}" status --porcelain --untracked-files=normal)" ]]; then
  echo 'candidate or tooling checkout changed during local CI; no success receipt was written' >&2
  exit 1
fi
node --input-type=module - "${expected_sha}" "${report_dir}" "${runner_root}" <<'JS'
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
const [commit, directory, tooling] = process.argv.slice(2);
const hash = path => createHash('sha256').update(readFileSync(path)).digest('hex');
const command = (name, args) => execFileSync(name, args, {encoding: 'utf8'}).trim();
const optionalCommand = (name, args) => {
  try { return execFileSync(name, args, {encoding: 'utf8', timeout: 5000, stdio: ['ignore', 'pipe', 'ignore']}).trim() || null; }
  catch { return null; }
};
const browserProfile = JSON.parse(readFileSync(join(directory, 'browser-profile.json'), 'utf8'));
const receipt = {
  schemaVersion: 1, kind: 'CONDUIT_LOCAL_CI', result: 'PASS', commit,
  tree: command('git', ['rev-parse', 'HEAD^{tree}']),
  completedAt: new Date().toISOString(),
  executor: {name: 'nektos/act', version: '0.2.97', mode: 'host'},
  execution: {
    reportedUnameSystem: command('uname', ['-s']),
    reportedUnameMachine: command('uname', ['-m']),
    nodePlatform: process.platform, nodeArchitecture: process.arch,
    go: JSON.parse(command('go', ['env', '-json', 'GOHOSTOS', 'GOHOSTARCH', 'GOOS', 'GOARCH', 'CGO_ENABLED'])),
    dockerDaemonArchitecture: optionalCommand('docker', ['info', '--format', '{{.Architecture}}']),
    qemuGuestBase: process.env.QEMU_GUEST_BASE || null,
    chromiumProfile: browserProfile.profile,
    candidateBrowserProfileHelperSha256: browserProfile.helperSha256,
    candidateBrowserConfigurationSha256: browserProfile.configurationSha256,
  },
  runnerCommit: command('git', ['-C', tooling, 'rev-parse', 'HEAD']),
  workflowSha256: hash(join(tooling, '.github/workflows/local-ci.yml')),
  verifierSha256: hash('scripts/verify-local.sh'),
  runnerSha256: hash(join(tooling, 'scripts/run-local-ci.sh')),
  logSha256: hash(join(directory, 'act.log')),
  tools: {go: command('go', ['version']), node: process.version,
    ruby: command('ruby', ['--version']), docker: command('docker', ['--version'])},
  hostedRun: false, publicationAuthorized: false,
};
writeFileSync(join(directory, 'receipt.json'), JSON.stringify(receipt, null, 2) + '\n', {mode: 0o600});
console.log(`PASS local CI: ${commit}; receipt ${join(directory, 'receipt.json')}`);
JS
