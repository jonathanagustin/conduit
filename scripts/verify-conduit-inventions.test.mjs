#!/usr/bin/env node
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { execFileSync, spawnSync } from "node:child_process";
const root=mkdtempSync(join(tmpdir(),"conduit-invention-source-"));
const source="// CONDUIT-INVENTED: fixture.NativeSource\n// Displaced-by: an approved replacement for this synthetic native source contract.\n";
const entry={id:"fixture.NativeSource",what:"A synthetic native source used by the verifier tests.",why:"A positive fixture must be admitted before any negative case is believed.",displacedBy:"an approved replacement for this synthetic native source contract.",riskIfWrong:"Only this disposable verifier fixture is affected by a wrong assumption."};
const register={inventions:[entry],displacementProcedure:["Reimplement against evidence in the same change and record what the invention got wrong."]};
try{
 mkdirSync(join(root,"scripts"));mkdirSync(join(root,"docs/evidence"),{recursive:true});
 writeFileSync(join(root,"scripts/verify-conduit-inventions.mjs"),readFileSync(new URL("./verify-conduit-inventions.mjs",import.meta.url)));
 const path=join(root,"docs/evidence/conduit-inventions.json");
 const setRegister=value=>writeFileSync(path,JSON.stringify(value));setRegister(register);
 const file=join(root,"source.mjs");writeFileSync(file,source);
 execFileSync("git",["init","--quiet"],{cwd:root});execFileSync("git",["add","."],{cwd:root});
 const run=()=>spawnSync(process.execPath,[join(root,"scripts/verify-conduit-inventions.mjs")],{cwd:root,encoding:"utf8"});
 assert.equal(run().status,0,"registered production JavaScript source must be admitted");
 setRegister({...register,inventions:[]});let result=run();assert.notEqual(result.status,0);assert.match(result.stderr,/which .*does not record/);
 setRegister(register);writeFileSync(file,source.split("\n")[0]+"\n");result=run();assert.notEqual(result.status,0);assert.match(result.stderr,/no Displaced-by/);
 writeFileSync(file,source);writeFileSync(join(root,"duplicate.go"),source+"package fixture\n");execFileSync("git",["add","."],{cwd:root});result=run();assert.notEqual(result.status,0);assert.match(result.stderr,/marked in two places/);
 rmSync(join(root,"duplicate.go"));execFileSync("git",["add","-u"],{cwd:root});
 writeFileSync(join(root,"noise.test.mjs"),"// CONDUIT-INVENTED: fixture.TestOnly\n// Displaced-by: test-only markers cannot register a production invention.\n");execFileSync("git",["add","."],{cwd:root});assert.equal(run().status,0,"test fixtures must not register production inventions");
 writeFileSync(file,"// Removed production implementation.\n");result=run();assert.notEqual(result.status,0);assert.match(result.stderr,/which no source marker claims/);
}finally{rmSync(root,{recursive:true,force:true});}
console.log("PASS invention sources: production JavaScript, registration, displacement, duplicate and stale-entry controls");
