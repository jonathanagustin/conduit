#!/usr/bin/env node
import assert from "node:assert/strict";
import { readFileSync, mkdtempSync, writeFileSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { prepareSelfProducts } from "./prepare-self-products.mjs";
import { selfProductFixture } from "./fixtures/self-products.mjs";

const fixture = selfProductFixture(), result = prepareSelfProducts(fixture);
assert.equal(JSON.stringify(result, null, 2)+"\n",readFileSync(new URL("../testdata/self-product/synthetic-prepared.json",import.meta.url),"utf8"));
assert.equal(result.publicationState,"NOT_PUBLISHED");
assert.equal(result.products.length,2);assert.equal(result.cli.assets.length,6);
assert.equal(result.products[0].mavenCoordinate,"io.github.jonathanagustin.conduit:hub:0.17.5");
assert.equal(result.products[0].publication.body.manifest.extensions.helmChart.helmRepositoryUrl,`oci://ghcr.io/jonathanagustin/conduit/platform-hub:0.17.5@sha256:${"5".repeat(64)}`);
assert.deepEqual(JSON.parse(Buffer.from(result.products[0].publication.body.defaultConfig,"base64")),{image:`ghcr.io/jonathanagustin/conduit:hub-0.17.5@sha256:${"2".repeat(64)}`});
assert.equal(result.cli.cataloguePublication,"UNSUPPORTED_BY_CURRENT_SERVICE_PRODUCT_API");
assert.equal(result.verification.signatures,"NOT_VERIFIED");
assert.deepEqual(result,prepareSelfProducts(selfProductFixture()),"preparation must be deterministic");
for (const [name, mutate] of [
  ["mismatched version",x=>x.helm.version="0.18.0"],
  ["mismatched commit",x=>x.helm.commit="a".repeat(40)],
  ["mutable image",x=>x.oci.images.hub.reference="ghcr.io/jonathanagustin/conduit:latest"],
  ["foreign repository",x=>x.oci.images.hub.image="ghcr.io/other/owner"],
  ["missing image",x=>delete x.oci.images["spoke-agent"]],
  ["duplicate chart",x=>x.helm.charts[1]=x.helm.charts[0]],
  ["wrong chart digest",x=>x.helm.charts[0].digest=`sha256:${"e".repeat(64)}`],
  ["missing architecture",x=>x.oci.images.hub.sboms.pop()],
  ["cross-component receipt",x=>x.oci.images.hub.sboms[0].file="spoke-agent-amd64.spdx.json"],
  ["bad archive bytes",x=>x.archives.set(x.archives.keys().next().value,Buffer.from("tampered"))],
  ["missing archive",x=>x.checksums=x.checksums.trim().split("\n").slice(1).join("\n")],
  ["duplicate checksum",x=>x.checksums+=x.checksums.split("\n")[0]+"\n"],
  ["traversal checksum",x=>x.checksums=x.checksums.replace("conduit_0.17.5_darwin_amd64.tar.gz","../conduit_0.17.5_darwin_amd64.tar.gz")],
  ["wrong target provenance",x=>x.provenance.values().next().value.buildDefinition.externalParameters.target="linux/amd64"],
  ["wrong provenance commit",x=>x.provenance.values().next().value.buildDefinition.resolvedDependencies[0].digest.gitCommit="b".repeat(40)],
  ["wrong builder",x=>x.provenance.values().next().value.runDetails.builder.id="https://untrusted.example/builder"],
]) { const input=selfProductFixture();mutate(input);assert.throws(()=>prepareSelfProducts(input),undefined,name); }
const secretInput=selfProductFixture();secretInput.oci.images.hub.unexpectedCredential="fixture-secret";
assert.ok(!JSON.stringify(prepareSelfProducts(secretInput)).includes("fixture-secret"),"opaque metadata must not leak into descriptors");
const directory=mkdtempSync(join(tmpdir(),"conduit-self-products-"));
try {
  for(const sub of ["archives","provenance"])mkdirSync(join(directory,sub));
  for(const [name,value] of fixture.archives)writeFileSync(join(directory,"archives",name),value);
  for(const [name,value] of fixture.provenance)writeFileSync(join(directory,"provenance",name),JSON.stringify(value));
  for(const [file,value] of [["oci.json",fixture.oci],["helm.json",fixture.helm]])writeFileSync(join(directory,file),JSON.stringify(value));
  writeFileSync(join(directory,"SHA256SUMS"),fixture.checksums);
  const script=fileURLToPath(new URL("./prepare-self-products.mjs",import.meta.url));
  const args=[script,"--repository",fixture.repository,"--version",fixture.version,"--oci-manifest",join(directory,"oci.json"),"--helm-manifest",join(directory,"helm.json"),"--cli-checksums",join(directory,"SHA256SUMS"),"--cli-archives",join(directory,"archives"),"--cli-provenance",join(directory,"provenance")];
  const cli=spawnSync(process.execPath,args,{encoding:"utf8"});assert.equal(cli.status,0,cli.stderr);assert.deepEqual(JSON.parse(cli.stdout),result);
  writeFileSync(join(directory,"SHA256SUMS"),"invalid");
  const failed=spawnSync(process.execPath,args,{encoding:"utf8"});assert.notEqual(failed.status,0);assert.equal(failed.stdout,"","failed preparation must not emit partial publication inputs");
  writeFileSync(join(directory,"SHA256SUMS"),fixture.checksums);
  writeFileSync(join(directory,"oci.json"),"PVT193");
  const invalid=spawnSync(process.execPath,args,{encoding:"utf8"});assert.notEqual(invalid.status,0);assert.equal(invalid.stdout,"");assert.ok(!invalid.stderr.includes("PVT193"));assert.match(invalid.stderr,/invalid OCI manifest JSON input/);
  writeFileSync(join(directory,"oci.json"),JSON.stringify(fixture.oci));
  writeFileSync(join(directory,"provenance",fixture.provenance.keys().next().value),"PVTCLI");
  const invalidProvenance=spawnSync(process.execPath,args,{encoding:"utf8"});assert.notEqual(invalidProvenance.status,0);assert.equal(invalidProvenance.stdout,"");assert.ok(!invalidProvenance.stderr.includes("PVTCLI"));assert.match(invalidProvenance.stderr,/invalid CLI provenance JSON input/);

} finally {rmSync(directory,{recursive:true,force:true});}
console.log("PASS self-Product preparation: exact inputs, six CLI byte checks, metadata binding, native output and no partial output");
