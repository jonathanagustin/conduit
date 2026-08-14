#!/usr/bin/env node

const names = [
  "reference",
  "digest",
  "version",
  "commit",
  "source-repository",
  "certificate-issuer",
  "certificate-identity",
];

const values = new Map();
for (let index = 2; index < process.argv.length; index += 2) {
  const argument = process.argv[index];
  const value = process.argv[index + 1];
  if (!argument?.startsWith("--") || value === undefined) {
    throw new Error(`arguments must be --name value pairs; invalid argument ${argument ?? "<missing>"}`);
  }
  const name = argument.slice(2);
  if (!names.includes(name) || values.has(name)) {
    throw new Error(`unknown or duplicate argument --${name}`);
  }
  values.set(name, value);
}

for (const name of names) {
  if (!values.get(name)) throw new Error(`missing --${name}`);
}

const version = values.get("version");
const digest = values.get("digest");
const reference = values.get("reference");
const commit = values.get("commit");
const sourceRepository = values.get("source-repository");
const certificateIssuer = values.get("certificate-issuer");
const certificateIdentity = values.get("certificate-identity");

if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(version)) {
  throw new Error("version must be a stable semantic version without a v prefix");
}
if (!/^sha256:[a-f0-9]{64}$/.test(digest)) {
  throw new Error("digest must be an exact lowercase SHA-256 coordinate");
}
if (!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\/[a-z0-9._-]+$/.test(sourceRepository)) {
  throw new Error("source repository must be an owner/name coordinate");
}

const expectedReference = `ghcr.io/${sourceRepository.split("/")[0]}/platform-runtime:${version}@${digest}`;
if (reference !== expectedReference) {
  throw new Error(`reference must equal ${expectedReference}`);
}
if (!/^[a-f0-9]{40}$/.test(commit)) {
  throw new Error("commit must be a full lowercase Git SHA-1");
}
if (certificateIssuer !== "https://token.actions.githubusercontent.com") {
  throw new Error("certificate issuer must be GitHub Actions OIDC");
}

const expectedIdentity = `https://github.com/${sourceRepository}/.github/workflows/platform-images.yml@refs/tags/v${version}`;
if (certificateIdentity !== expectedIdentity) {
  throw new Error(`certificate identity must equal ${expectedIdentity}`);
}

const descriptor = {
  apiVersion: "conduit.delivery/runtime-source/v1",
  kind: "RuntimeSource",
  version,
  source: {
    repository: sourceRepository,
    commit,
  },
  artifact: {
    reference,
    digest,
  },
  signaturePolicy: {
    provider: "cosign",
    certificateIssuer,
    certificateIdentity,
  },
};

process.stdout.write(`${JSON.stringify(descriptor, null, 2)}\n`);
