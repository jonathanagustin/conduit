#!/usr/bin/env node
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const failures = [];
function filesBelow(directory) {
  const result = [];
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) result.push(...filesBelow(path));
    else if (entry.endsWith(".go") && !entry.endsWith("_test.go")) result.push(path);
  }
  return result;
}
function imports(path) {
  const source = readFileSync(path, "utf8");
  const blocks = [...source.matchAll(/import\s*\(([^]*?)\)/g)].map((match) => match[1]);
  const singles = [...source.matchAll(/\bimport\s*(?:(?:[A-Za-z_]\w*|\.)\s+)?(?:"([^"\n]+)"|`([^`]+)`)/g)].map((match) => match[1] ?? match[2]);
  return [...singles, ...blocks.flatMap((block) => [...block.matchAll(/(?:"([^"\n]+)"|`([^`]+)`)/g)].map((match) => match[1] ?? match[2]))];
}
for (const path of filesBelow(join(root, "internal"))) {
  if (path.startsWith(join(root, "internal/apollo/workloadmetadata/"))) {
    for (const dependency of imports(path)) {
      if (dependency !== "go.yaml.in/yaml/v4" && (dependency.includes("/internal/") || dependency.split("/")[0].includes("."))) {
        failures.push(`${relative(root, path)}: workload metadata leaf imports ${dependency}`);
      }
    }
  }
  const local = relative(root, path).replaceAll("\\", "/");
  const dependencies = imports(path);
  if (["internal/controlplane/fleet_index_read.go", "internal/controlplane/fleet.go", "internal/controlplane/fleet_read_visibility.go", "internal/controlplane/artifact_registry_catalog.go", "internal/controlplane/publication_asset_projection.go", "internal/controlplane/catalog_projection.go", "internal/controlplane/release_lifecycle_projection.go", "internal/controlplane/catalog_publication_projection.go", "internal/controlplane/environment_inventory_projection.go", "internal/controlplane/environment_reporting_projection.go", "internal/controlplane/environment_projection_source.go"].includes(local)) {
    for (const dependency of dependencies) {
      if (/\/internal\/(apollo|apollographql)(?:\/|$)/.test(dependency)) {
        failures.push(`${local}: native repository projection imports reference adapter ${dependency}`);
      }
    }
  }
  if (local.startsWith("internal/environments/application/")) {
    for (const dependency of dependencies) {
      if (/\/internal\/(controlplane|apollo|apollographql)(?:\/|$)/.test(dependency)) {
        failures.push(`${local}: native Environment application imports legacy or reference adapter ${dependency}`);
      }
    }
  }
  const coreKind = local.includes("/application/") ? "application" : local.includes("/ports/") ? "port" : null;
  if (coreKind) {
    for (const dependency of dependencies) {
      if (/\/internal\/.*\/adapters(?:\/|$)/.test(dependency)) {
        failures.push(`${local}: ${coreKind} imports adapter ${dependency}`);
      }
    }
  }
  if (local.includes("/domain/")) {
    for (const dependency of dependencies) {
      if (dependency.includes("/internal/") || dependency === "net/http" || dependency.includes("k8s.io/")) {
        failures.push(`${local}: domain imports forbidden dependency ${dependency}`);
      }
    }
  }
  if (local.startsWith("internal/hub/composition/")) {
    for (const dependency of dependencies) {
      if (dependency.includes("/internal/agent") || dependency.includes("/internal/optimization") || dependency.includes("/internal/foundryinterop")) {
        failures.push(`${local}: composition imports non-Hub adapter ${dependency}`);
      }
    }
  }
  if (local.startsWith("internal/apollo/")) {
    for (const dependency of dependencies) {
      if (dependency.includes("/internal/controlplane")) {
        failures.push(`${local}: Apollo bounded context imports legacy control plane ${dependency}`);
      }
    }
  }
  if (local.startsWith("internal/delivery/reconciliation/") &&
      !local.startsWith("internal/delivery/reconciliation/adapters/controlplane/")) {
    for (const dependency of dependencies) {
      if (dependency.includes("/internal/controlplane")) {
        failures.push(`${local}: reconciliation core imports legacy control plane ${dependency}`);
      }
    }
  }
}
const apolloAdapter = readFileSync(join(root, "internal/controlplane/apollo_openapi_adapter.go"), "utf8");
if (apolloAdapter.includes("invokeApolloFacade")) {
  failures.push("Apollo generated adapter re-enters the legacy HTTP facade");
}
const delivery = readFileSync(join(root, "cmd/platform-delivery/main.go"), "utf8");
if (!delivery.includes("composition.NewHubHandler(")) failures.push("platform-delivery bypasses the Hub composition root");
if (delivery.includes("controlplane.NewHandler(")) failures.push("platform-delivery constructs the legacy HTTP facade directly");
if (failures.length) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  process.exit(1);
}
console.log("PASS domain boundaries: Hub composition is mandatory; Apollo and reconciliation core have no control-plane backedge; domain infrastructure imports are rejected");
