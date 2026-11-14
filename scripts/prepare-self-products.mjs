#!/usr/bin/env node
// CONDUIT-INVENTED: releaseprep.SelfProductDescriptors
// Displaced-by: an owner-approved self-Product publication contract defining coordinates, artifact inputs and the supported CLI asset catalogue.
import { createHash } from "node:crypto";
import { readFileSync, lstatSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const targets = ["darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"];
const sha = value => createHash("sha256").update(value).digest("hex");
const requireValue = (condition, message) => { if (!condition) throw new Error(message); };
const exactDigest = value => typeof value === "string" && /^sha256:[a-f0-9]{64}$/.test(value);
const bareDigest = value => typeof value === "string" && /^[a-f0-9]{64}$/.test(value);
const jsonBytes = value => Buffer.from(JSON.stringify(value)).toString("base64");

// This prepares documents only. It performs no registry, Hub, release, signing,
// cluster or network operation. Input consistency is not signature verification.
export function prepareSelfProducts({ repository, version, oci, helm, checksums, readCLI, readProvenance }) {
  requireValue(/^[a-z0-9][a-z0-9-]*\/[a-z0-9][a-z0-9._-]*$/.test(repository ?? ""), "source repository must be owner/name");
  requireValue(/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(version ?? ""), "expected a stable semantic version");
  requireValue(oci?.apiVersion === "platform.delivery/oci-release/v2" && helm?.apiVersion === "platform.delivery/helm-release/v1", "unsupported release manifest version");
  requireValue(oci.version === version && helm.version === version, "release manifest versions differ from the selected version");
  requireValue(/^[a-f0-9]{40}$/.test(oci.commit ?? "") && helm.commit === oci.commit, "release manifests must share one full source commit");
  requireValue(JSON.stringify(Object.keys(oci.images ?? {}).sort()) === JSON.stringify(["hub", "spoke-agent"]), "expected exactly Hub and Spoke image descriptors");
  requireValue(Array.isArray(helm.charts) && helm.charts.length === 2 && new Set(helm.charts.map(x => x.component)).size === 2, "expected exactly two unique Helm chart descriptors");
  requireValue(typeof readCLI === "function" && typeof readProvenance === "function", "CLI archives and provenance readers are required");
  const imageRepository = `ghcr.io/${repository}`;
  const group = "io.github.jonathanagustin.conduit";
  // Product names are an explicit native first slice, not recovered Apollo names.
  requireValue(repository === "jonathanagustin/conduit", "this preparer is scoped to the Conduit release producer");
  const products = ["hub", "spoke-agent"].map(component => {
    const image = oci.images[component];
    requireValue(image?.image === imageRepository && exactDigest(image.digest) && image.reference === `${imageRepository}@${image.digest}`, `invalid ${component} immutable image descriptor`);
    for (const [key, extension] of [["sboms", "spdx.json"], ["vulnerabilityReports", "grype.json"]]) {
      const records = image[key];
      requireValue(Array.isArray(records) && records.length === 2 && JSON.stringify(records.map(x => x.architecture).sort()) === '["amd64","arm64"]' && records.every(x => bareDigest(x.sha256) && typeof x.file === "string" && x.file === `${component}-${x.architecture}.${extension}`), `invalid ${component} ${key} receipts`);
    }
    const chartComponent = component === "hub" ? "hub" : "spoke";
    const chartName = `platform-${chartComponent}`;
    const chart = helm.charts.find(x => x.component === chartComponent);
    requireValue(chart?.version === version && chart.commit === oci.commit && exactDigest(chart.digest) && chart.reference === `${imageRepository}/${chartName}:${version}@${chart.digest}`, `invalid ${component} immutable Helm descriptor`);
    // The release producer publishes component-version display tags. The digest
    // is authoritative; no registry tag lookup or tag/digest equivalence is claimed.
    const imageReference = `${imageRepository}:${component}-${version}@${image.digest}`;
    const manifest = {
      manifestVersion: "1.0", productGroup: group, productName: component, productVersion: version, productType: "helm-chart.v1",
      extensions: {
        helmChart: { helmChartName: chartName, helmChartVersion: version, helmRepositoryUrl: `oci://${chart.reference}` },
        artifacts: [{ type: "oci", uri: imageReference }],
      },
    };
    return {
      component, mavenCoordinate: `${group}:${component}:${version}`, channels: ["DEV", "RELEASE_CANDIDATE", "RELEASE"],
      publication: { method: "POST", path: "/v1/product-publications/helm-chart", body: {
        manifest, defaultConfig: jsonBytes({ image: imageReference }),
        configSchema: jsonBytes({ type: "object", properties: { image: { const: imageReference } }, required: ["image"] }),
      } },
      sourceImage: { image: image.image, digest: image.digest, reference: image.reference, sboms: image.sboms.map(({architecture,file,sha256})=>({architecture,file,sha256})), vulnerabilityReports: image.vulnerabilityReports.map(({architecture,file,sha256})=>({architecture,file,sha256})) },
      consumerConfigurationResponsibilities: component === "hub" ? ["repository.postgres.secretName", "tls.serverSecretName", "bundleSigningSecretName", "bootstrapAdministrators"] : ["environmentId", "agentId", "controlPlaneURL", "tls.clientSecretName", "rbac.managedNamespaces"],
    };
  });
  const expected = new Map(targets.map(target => {
    const [os, architecture] = target.split("/");
    return [`conduit_${version}_${os}_${architecture}.${os === "windows" ? "zip" : "tar.gz"}`, target];
  }));
  const parsed = new Map();
  for (const line of String(checksums).trim().split(/\r?\n/)) {
    const match = /^([a-f0-9]{64})  (conduit_[a-zA-Z0-9_.-]+)$/.exec(line);
    requireValue(match && expected.has(match[2]) && !parsed.has(match[2]), "CLI checksums must contain each expected archive exactly once and no other path");
    parsed.set(match[2], match[1]);
  }
  requireValue(parsed.size === expected.size, "CLI checksums omit a supported target");
  const assets = [...expected].map(([file, target]) => {
    const digest = parsed.get(file);
    requireValue(sha(readCLI(file)) === digest, `CLI archive checksum mismatch: ${file}`);
    const stem = file.replace(/\.(?:tar\.gz|zip)$/, "");
    const provenance = readProvenance(`${stem}.slsa.json`);
    const definition = provenance?.buildDefinition;
    const dependencies = definition?.resolvedDependencies;
    const builder = `https://github.com/${repository}/.github/workflows/platform-images.yml@refs/tags/v${version}`;
    requireValue(definition?.buildType === "https://platform.delivery/buildtypes/conduit-cli/v1" && definition.externalParameters?.target === target && definition.externalParameters?.version === version && Array.isArray(dependencies) && dependencies.length === 1 && dependencies[0].uri === `git+https://github.com/${repository}@refs/tags/v${version}` && dependencies[0].digest?.gitCommit === oci.commit && provenance.runDetails?.builder?.id === builder, `CLI provenance metadata mismatch: ${file}`);
    return { target, file, sha256: digest, url: `https://github.com/${repository}/releases/download/v${version}/${file}`, provenanceFile: `${stem}.slsa.json` };
  });
  return {
    apiVersion: "conduit.delivery/self-product-preparation/v1", kind: "SelfProductPreparation", valueTier: "CONDUIT_NATIVE",
    publicationState: "NOT_PUBLISHED", source: { repository, version, commit: oci.commit },
    verification: { scope: "INPUT_METADATA_AND_CLI_CHECKSUMS", signatures: "NOT_VERIFIED", registryAvailability: "NOT_CHECKED", deployment: "NOT_PERFORMED" },
    products,
    cli: { disposition: "DOWNLOAD_INVENTORY_ONLY", cataloguePublication: "UNSUPPORTED_BY_CURRENT_SERVICE_PRODUCT_API", assets },
  };
}

export function main(args = process.argv.slice(2)) {
  const allowed = ["repository", "version", "oci-manifest", "helm-manifest", "cli-checksums", "cli-archives", "cli-provenance"];
  const options = {};
  for (let i = 0; i < args.length; i += 2) {
    const key = args[i]?.slice(2);
    requireValue(args[i]?.startsWith("--") && allowed.includes(key) && !Object.hasOwn(options, key) && args[i + 1], "arguments must be unique known --name value pairs");
    options[key] = args[i + 1];
  }
  requireValue(allowed.every(key => options[key]), `required arguments: ${allowed.map(key => `--${key}`).join(" ")}`);
  const readRegular = (directory, file) => {
    const path = join(resolve(directory), file);
    const stat = lstatSync(path);
    requireValue(stat.isFile() && !stat.isSymbolicLink(), `expected a regular release file: ${file}`);
    return readFileSync(path);
  };
  const result = prepareSelfProducts({ repository: options.repository, version: options.version,
    oci: JSON.parse(readFileSync(options["oci-manifest"], "utf8")), helm: JSON.parse(readFileSync(options["helm-manifest"], "utf8")),
    checksums: readFileSync(options["cli-checksums"], "utf8"), readCLI: file => readRegular(options["cli-archives"], file),
    readProvenance: file => JSON.parse(readRegular(options["cli-provenance"], file)),
  });
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { main(); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
