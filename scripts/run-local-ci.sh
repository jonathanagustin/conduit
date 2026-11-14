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
report_dir="$(mktemp -d "${TMPDIR:-/tmp}/conduit-local-ci.XXXXXXXX")"
export CONDUIT_LOCAL_CI_REPORT_DIR="${report_dir}"
printf 'Local CI commit: %s\nEvidence directory: %s\n' "${expected_sha}" "${report_dir}"
# Never load act's implicit .env/.secrets files or pass a GitHub write token.
# The host executor uses the same pinned toolchain as verify-local.sh. Docker
# remains necessary for its PostgreSQL and isolated fixture checks.
env -u GH_TOKEN -u GITHUB_TOKEN "${act_bin}" workflow_dispatch \
  --workflows "${runner_root}/.github/workflows/local-ci.yml" --directory "${root}" \
  --job verify --platform conduit-local=-self-hosted \
  --env "CONDUIT_LOCAL_CI_WORKTREE=${root}" \
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
const receipt = {
  schemaVersion: 1, kind: 'CONDUIT_LOCAL_CI', result: 'PASS', commit,
  tree: command('git', ['rev-parse', 'HEAD^{tree}']),
  completedAt: new Date().toISOString(),
  executor: {name: 'nektos/act', version: '0.2.97', mode: 'host'},
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
