#!/usr/bin/env node

import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const script = fileURLToPath(new URL("./render-runtime-source-descriptor.mjs", import.meta.url));
const digest = `sha256:${"a".repeat(64)}`;
const common = [
  "--reference", `ghcr.io/jonathanagustin/platform-runtime:1.2.3@${digest}`,
  "--digest", digest,
  "--version", "1.2.3",
  "--commit", "b".repeat(40),
  "--source-repository", "jonathanagustin/conduit",
  "--certificate-issuer", "https://token.actions.githubusercontent.com",
  "--certificate-identity", "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3",
];

const run = (arguments_) => spawnSync(process.execPath, [script, ...arguments_], { encoding: "utf8" });
const replace = (arguments_, name, value) => {
  const copy = [...arguments_];
  copy[copy.indexOf(name) + 1] = value;
  return copy;
};

const valid = run(common);
assert.equal(valid.status, 0, valid.stderr);
assert.deepEqual(JSON.parse(valid.stdout), {
  apiVersion: "conduit.delivery/runtime-source/v1",
  kind: "RuntimeSource",
  version: "1.2.3",
  source: { repository: "jonathanagustin/conduit", commit: "b".repeat(40) },
  artifact: {
    reference: `ghcr.io/jonathanagustin/platform-runtime:1.2.3@${digest}`,
    digest,
  },
  signaturePolicy: {
    provider: "cosign",
    certificateIssuer: "https://token.actions.githubusercontent.com",
    certificateIdentity: "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3",
  },
});

for (const [name, arguments_, message] of [
  ["digest mismatch", replace(common, "--digest", `sha256:${"c".repeat(64)}`), "reference must equal"],
  ["wrong signer", replace(common, "--certificate-identity", "https://github.com/other/repo/.github/workflows/publish.yml@refs/tags/v1.2.3"), "certificate identity must equal"],
  ["mutable version", replace(common, "--version", "latest"), "stable semantic version"],
]) {
  const result = run(arguments_);
  assert.notEqual(result.status, 0, `${name} unexpectedly passed`);
  assert.match(result.stderr, new RegExp(message), `${name}: ${result.stderr}`);
}

console.log("Gitless runtime source descriptor contract verified: 1 valid and 3 rejected cases");
