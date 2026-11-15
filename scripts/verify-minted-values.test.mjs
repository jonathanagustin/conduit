#!/usr/bin/env node
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, copyFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const paths = {
 sdl:'internal/apollographql/schema/apollo.interface.graphql',
 minted:'scripts/apollo-graphql/conduit-minted-values.json',
 native:'scripts/apollo-graphql/conduit-native.json',
 register:'docs/evidence/conduit-inventions.json',
 observed:'docs/reference-platform/observed/reference-graphql-observed.json',
 manifest:'docs/reference-platform/public-documentation-manifest.tsv',
};
function fixture(mutate, expected) {
 const temporary = mkdtempSync(join(tmpdir(),'conduit-minted-test-'));
 try {
  const inputs = {};
  for (const [key,path] of Object.entries(paths)) inputs[key]=readFileSync(join(root,path),'utf8');
  mutate(inputs);
  for (const [key,path] of Object.entries(paths)) { mkdirSync(dirname(join(temporary,path)),{recursive:true});writeFileSync(join(temporary,path),inputs[key]); }
  const script=join(temporary,'scripts/verify-minted-values.mjs');copyFileSync(join(root,'scripts/verify-minted-values.mjs'),script);
  const result=spawnSync(process.execPath,[script],{encoding:'utf8'});
  assert.equal(result.error,undefined);
  assert.equal(result.status===0,expected,result.stdout+result.stderr);
 } finally { rmSync(temporary,{recursive:true,force:true}); }
}
function edit(inputs,key,change) { const value=JSON.parse(inputs[key]);change(value);inputs[key]=JSON.stringify(value); }
const coordinates=['Query.me','ConduitCurrentUser.id','ConduitCurrentUser.fullName'];
test('accepts real recovered and explicitly documented native-value disclosures',()=>fixture(()=>{},true));
test('a documentation note is equivalent',()=>fixture(i=>edit(i,'minted',v=>{v.note='Review note without contract changes';}),true));
test('public entry needs its explicit matching citation',()=>fixture(i=>edit(i,'minted',v=>{delete v.coordinates['Query.me'].documentedBy;}),false));
test('a different valid public URL cannot substitute for the declared source',()=>fixture(i=>edit(i,'minted',v=>{v.coordinates['Query.me'].documentedBy='https://www.palantir.com/docs/apollo/core/plans-and-constraints/';}),false));
test('an unknown URL cannot become authority by agreeing in two declarations',()=>fixture(i=>{
 const url='https://untrusted.invalid/document';
 edit(i,'minted',v=>{for(const c of coordinates)v.coordinates[c].documentedBy=url;});
 edit(i,'native',v=>{v.types.Query.fields.find(f=>f.name==='me').documentedBy=url;for(const f of v.types.ConduitCurrentUser.fields)f.documentedBy=url;});
},false));
test('missing declared public coordinate is rejected',()=>fixture(i=>edit(i,'native',v=>{delete v.types.Query;}),false));
test('public citation does not permit pretending a coordinate was observed',()=>fixture(i=>{i.sdl=i.sdl.replace('me: ConduitCurrentUser @provenance(tier: DOCUMENTED','me: ConduitCurrentUser @provenance(tier: OBSERVED');},false));
test('unknown minted coordinate is rejected',()=>fixture(i=>edit(i,'minted',v=>{v.coordinates['Query.unknown']=v.coordinates['Query.me'];}),false));
test('removing a value disclosure is rejected',()=>fixture(i=>{i.sdl=i.sdl.replace('me: ConduitCurrentUser @provenance(tier: DOCUMENTED, valueTier: CONDUIT_NATIVE)','me: ConduitCurrentUser @provenance(tier: DOCUMENTED)');},false));
test('a named invention must remain registered',()=>fixture(i=>edit(i,'register',v=>{v.inventions=v.inventions.filter(x=>x.id!=='apollographql.CurrentUserProjection');}),false));
test('empty recovery cannot disable source validation',()=>fixture(i=>edit(i,'observed',v=>{v.types=[];}),false));
