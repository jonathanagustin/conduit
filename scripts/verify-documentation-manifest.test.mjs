import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const offline = 'data:text/javascript,' + encodeURIComponent('globalThis.fetch = async () => { throw new Error("fixture network outage"); };');
for(const strict of [false,true]){
  test(`public crawl ${strict?'fails closed when required':'reports optional offline skip'}`,()=>{
    const args=['--import',offline,'scripts/verify-documentation-manifest.mjs'];
    if(strict)args.push('--require-network');
    const result=spawnSync(process.execPath,args,{encoding:'utf8',timeout:10000});
    const output=result.stdout+result.stderr;
    assert.equal(result.status,strict?1:0,output);
    assert.match(output,strict?/no current-corpus PASS/:/SKIP/);
    if(strict)assert.doesNotMatch(output,/SKIP/);
  });
}
