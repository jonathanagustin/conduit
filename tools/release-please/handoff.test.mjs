import assert from "node:assert/strict";
import { test } from "node:test";
import { assertPreparationWrite, ensureTrunk, reconcilePublished, releaseBranch } from "../../scripts/release-preparation.mjs";

const repo = "owner/conduit";
const sha = "a".repeat(40);
const pr = {number: 42, state: "open", base: {ref: "main"}, head: {sha, ref: releaseBranch, repo: {full_name: repo}}};
const file = value => ({encoding: "base64", content: Buffer.from(value).toString("base64")});
function apiFixture({pull = pr, status = [], runs = [], releases = [], comparison = "ahead", version = "0.18.0", failure} = {}) {
 const writes = [];
 const api = async (method, path, body) => {
  assertPreparationWrite(method, path);
  if (failure && path.includes(failure)) throw new Error("injected API failure");
  if (method !== "GET") { writes.push({method, path, body}); return; }
  if (path.includes("/issues?")) return [{number: pull.number, pull_request: {}}];
  if (path.endsWith(`/pulls/${pull.number}`)) return pull;
  if (path.includes("/commits/") && path.endsWith("/status")) return {statuses: status};
  if (path.includes("/runs?")) return {workflow_runs: runs};
  if (path.includes("/releases?")) return releases;
  if (path.includes("/contents/.release-please-manifest.json")) return file('{".":"0.18.0"}');
  if (path.includes("/git/ref/tags/")) return {object: {type: "commit", sha}};
  if (path.includes("/compare/")) return {status: comparison};
  if (path.includes("/contents/VERSION")) return file(version);
  throw new Error(`unexpected API request ${method} ${path}`);
 };
 return {api, writes};
}

test("bot PR explicitly dispatches only the exact-head-authenticating Trunk workflow", async () => {
 const {api,writes} = apiFixture(); await ensureTrunk(api,repo);
 assert.deepEqual(writes,[{method:"POST",path:`repos/${repo}/actions/workflows/platform-ci.yml/dispatches`,body:{ref:releaseBranch,inputs:{pull_request:"42"}}}]);
});
for (const scenario of [
 {runs:[{head_sha:sha,status:"completed",conclusion:"success"}]},
 {runs:[{head_sha:sha,status:"in_progress"}]},
 {pull:{...pr,head:{...pr.head,repo:{full_name:"fork/conduit"}}}},
]) test(`no redundant or foreign-head dispatch ${JSON.stringify(scenario)}`,async()=>{
 const {api,writes}=apiFixture(scenario);await ensureTrunk(api,repo);assert.equal(writes.length,0);
});

test("failed/cancelled and stale-head runs do not strand the new release head",async()=>{
 const {api,writes}=apiFixture({status:[{context:"Trunk gate",state:"failure"}],runs:[{head_sha:sha,status:"completed"},{head_sha:"b".repeat(40),status:"in_progress"}]});
 await ensureTrunk(api,repo);assert.equal(writes.length,1);
});

test("read failure fails closed rather than reporting checks scheduled",async()=>{
 const {api,writes}=apiFixture({failure:"/runs?"});await assert.rejects(ensureTrunk(api,repo),/injected/);assert.equal(writes.length,0);
});

const merged={...pr,state:"closed",merged_at:"2026-11-13T00:00:00Z",merge_commit_sha:"b".repeat(40)};
test("release-PR merge alone never marks a release published",async()=>{
 const {api,writes}=apiFixture({pull:merged});await reconcilePublished(api,repo);assert.equal(writes.length,0);
});
test("only an actual published release containing the preparation clears the pending label",async()=>{
 const {api,writes}=apiFixture({pull:merged,releases:[{tag_name:"v0.18.0",draft:false,published_at:"2026-11-13T01:00:00Z"}]});
 await reconcilePublished(api,repo);
 assert.deepEqual(writes.map(write=>write.method),["POST","DELETE"]);
 assert.deepEqual(writes[0].body,{labels:["autorelease: tagged"]});
 assert.match(writes[1].path,/autorelease%3A%20pending$/);
});
for(const scenario of [{comparison:"diverged"},{version:"0.19.0"}]) test("mismatched publication cannot clear a pending preparation",async()=>{
 const {api,writes}=apiFixture({pull:merged,releases:[{tag_name:"v0.18.0",draft:false,published_at:"2026-11-13T01:00:00Z"}],...scenario});
 await assert.rejects(reconcilePublished(api,repo));assert.equal(writes.length,0);
});

test("ordinary preparation writes cannot create tags, Releases, or dispatch publication",()=>{
 for(const [method,path] of [["POST",`repos/${repo}/git/refs`],["POST",`repos/${repo}/releases`],["POST",`repos/${repo}/actions/workflows/platform-release.yml/dispatches`],["POST",`repos/${repo}/actions/workflows/platform-images.yml/dispatches`],["PATCH",`repos/${repo}/git/refs/tags/v0.18.0`]]) {
  assert.throws(()=>assertPreparationWrite(method,path),/cannot write/);
 }
});
