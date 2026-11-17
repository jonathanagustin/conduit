import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, resolve } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { verifyReadiness } from './verify-readiness.mjs';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const recordPath = 'docs/operations/readiness-evidence.json';
const today = '2026-11-17';

async function fixture(t) {
  const root = mkdtempSync(resolve(tmpdir(),'conduit-readiness-test-'));
  t.after(() => rmSync(root,{ recursive:true, force:true }));
  const git = (...args) => execFileSync('git',args,{ cwd:root, encoding:'utf8',stdio:['ignore','pipe','pipe'] }).trim();
  git('init','-q');git('config','user.name','Fixture');git('config','user.email','fixture@example.invalid');
  git('commit','--allow-empty','-qm','docs: fixture baseline');
  const record = JSON.parse(readFileSync(resolve(repo,recordPath),'utf8'));
  record.baselineCommit = git('rev-parse','HEAD');
  record.reviewedAt=today;record.issueSnapshotAt=today+'T00:00:00Z';
  for(const issue of record.issues)issue.observedAt=record.issueSnapshotAt;
  for(const gap of record.gaps)for(const path of gap.sources){mkdirSync(dirname(resolve(root,path)),{recursive:true});writeFileSync(resolve(root,path),'fixture\n');}
  mkdirSync(resolve(root,'docs/operations'),{recursive:true});
  mkdirSync(resolve(root,'docs-site/src/content/docs'),{recursive:true});
  copyFileSync(resolve(repo,'docs/operations/native-reference-coverage.json'),resolve(root,'docs/operations/native-reference-coverage.json'));
  const save = () => writeFileSync(resolve(root,recordPath),JSON.stringify(record));
  save();
  await verifyReadiness({root,today,write:true});
  return {root,record,save,git};
}

test('offline result explicitly reports the committed snapshot boundary',async t => {
  const {root}=await fixture(t);
  assert.match(await verifyReadiness({root,today}),/committed issue snapshot only \(offline\)/);
});

test('stale dates, forged bounds and closed tracker snapshots fail',async t => {
  for(const [mutate,reason] of [
    [record=>{record.reviewedAt='2026-08-17';},/readiness review is stale or future-dated/],
    [record=>{record.reviewedAt='2026-02-31';},/review dates must be real/],
    [record=>{record.maximumAgeDays='forever';},/maximumAgeDays must be a bounded positive integer/],
    [record=>{record.issueSnapshotAt='2026-08-17T00:00:00Z';},/committed issue-state snapshot is stale or invalid/],
    [record=>{record.issues[0].state='closed';},/active tracker #[0-9]+ is absent or closed/],
    [record=>{record.gaps.pop();},/each historical gap number 1 through 9/],
  ]){
    const {root,record,save}=await fixture(t);mutate(record);save();
    await assert.rejects(verifyReadiness({root,today}),reason);
  }
});

test('the commit-distance bound fails after real additional commits',async t => {
  const {root,record,save,git}=await fixture(t);
  record.maximumCommitsBehind=1;save();
  git('commit','--allow-empty','-qm','docs: first newer commit');
  git('commit','--allow-empty','-qm','docs: second newer commit');
  await assert.rejects(verifyReadiness({root,today}),/lags HEAD by 2 commits/);
});

test('missing or divergent history fails explicitly rather than assuming freshness',async t => {
  const {root,record,save,git}=await fixture(t);
  record.baselineCommit='f'.repeat(40);save();
  await assert.rejects(verifyReadiness({root,today}),/history is unavailable, shallow, or not an ancestor/);
  record.baselineCommit=git('rev-parse','HEAD');save();
  git('checkout','--orphan','unrelated');git('commit','--allow-empty','-qm','docs: unrelated history');
  await assert.rejects(verifyReadiness({root,today}),/not an ancestor/);
});

test('live closure and unavailable API state cannot produce a current-state pass',async t => {
  const {root}=await fixture(t);
  await assert.rejects(verifyReadiness({root,today,online:true,fetchIssue:async number=>({number,state:'closed'})}),/GitHub reports active tracker/);
  await assert.rejects(verifyReadiness({root,today,online:true,fetchIssue:async()=>{throw new Error('offline');}}),/live issue state unavailable/);
  assert.match(await verifyReadiness({root,today,online:true,fetchIssue:async number=>({number,state:'open'})}),/live issue states verified/);
});

test('hand-edited status pages and absent evidence sources are rejected',async t => {
  const {root,record,save}=await fixture(t);
  writeFileSync(resolve(root,'docs-site/src/content/docs/readiness.md'),'Everything passes!\n');
  await assert.rejects(verifyReadiness({root,today}),/is stale/);
  record.gaps[0].sources.push('missing-evidence');save();
  await assert.rejects(verifyReadiness({root,today}),/missing evidence source/);
});

test('scheduled verification requires live evidence and Pages publication stays manual', () => {
  const drift = readFileSync(resolve(repo,'.github/workflows/documentation-drift.yml'),'utf8');
  assert.match(drift,/\n  schedule:\n    - cron:/);
  assert.match(drift,/node scripts\/verify-readiness\.mjs --online/);
  assert.match(drift,/run: node scripts\/verify-documentation-manifest\.mjs --require-network/);
  assert.doesNotMatch(drift,/^\s+(?:run:.*|node .*|go .*|git .*)--write/m);
  assert.match(drift,/permissions:\n  contents: read\n  issues: read/);
  const docs = readFileSync(resolve(repo,'.github/workflows/docs.yaml'),'utf8');
  const manual = "github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main'";
  assert.ok(docs.includes('name: Upload Pages artifact\n        if: '+manual));
  assert.ok(docs.includes('  deploy:\n    timeout-minutes: 10\n    if: '+manual));
});
