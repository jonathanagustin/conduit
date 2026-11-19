#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { cpSync, mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const sourceRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const fixture = mkdtempSync(join(tmpdir(), "platform-domain-boundaries-"));
const run = () => execFileSync(process.execPath, [join(fixture, "scripts/verify-domain-boundaries.mjs")], { encoding: "utf8" });
const write = (path, contents) => {
  const target = join(fixture, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, contents);
};

try {
  mkdirSync(join(fixture, "scripts"), { recursive: true });
  cpSync(join(sourceRoot, "scripts/verify-domain-boundaries.mjs"), join(fixture, "scripts/verify-domain-boundaries.mjs"));
  write("internal/hub/composition/composition.go", 'package composition\nimport "example/internal/controlplane"\n');
  write("internal/controlplane/apollo_openapi_adapter.go", "package controlplane\n");
  write("internal/apollo/application/service.go", "package application\n");
  write("internal/apollo/workloadmetadata/metadata.go", 'package workloadmetadata\nimport "go.yaml.in/yaml/v4"\n');
  write("internal/delivery/reconciliation/application/worker.go", "package application\n");
  write("cmd/platform-delivery/main.go", 'package main\nfunc main(){ composition.NewHubHandler() }\n');
  if (!run().includes("PASS domain boundaries")) throw new Error("positive fixture did not pass");

  write("internal/catalog/ports/ports.go", 'package ports\nimport "example/internal/catalog/domain"\n');
  if (!run().includes("PASS domain boundaries")) throw new Error("native port-to-domain fixture did not pass");

  write("internal/environments/application/projection_reader.go", 'package application\nimport "example/internal/environments/domain"\n');
  if (!run().includes("PASS domain boundaries")) throw new Error("native Environment snapshot fixture did not pass");

  const controls = [
    ["internal/environments/application/projection_reader.go", 'package application\nimport "example/internal/controlplane"\n', 'package application\nimport "example/internal/environments/domain"\n'],
    ["internal/environments/application/reference.go", 'package application\nimport adapter "example/internal/apollo/application"\n', null],
    ["internal/environments/application/raw.go", 'package application\nimport `example/internal/apollographql`\n', null],
    ["internal/controlplane/environment_projection_source.go", 'package controlplane\nimport "example/internal/apollo"\n', null],
    ["internal/catalog/ports/ports.go", 'package ports\nimport "example/internal/catalog/adapters/postgres"\n', 'package ports\nimport "example/internal/catalog/domain"\n'],
    ["internal/catalog/application/direct.go", 'package application\nimport repo "example/internal/catalog/adapters"\n', null],
    ["internal/catalog/ports/raw.go", 'package ports\nimport (\n repo `example/internal/catalog/adapters/postgres`\n)\n', null],
    ["internal/catalog/application/raw.go", 'package application\nimport `example/internal/catalog/adapters`\n', null],
    ["internal/packagingworkflow/application/service.go", 'package application\nimport "example/internal/packagingworkflow/adapters/workspacegit"\n', null],
    ["internal/catalog/application/service.go", 'package application\nimport (\n  repo "example/internal/catalog/adapters/postgres"\n)\n', null],
    ["cmd/platform-delivery/main.go", 'package main\nfunc main(){ controlplane.NewHandler() }\n', 'package main\nfunc main(){ composition.NewHubHandler() }\n'],
    ["internal/reporting/domain/model.go", 'package domain\nimport "net/http"\n', null],
    ["internal/hub/composition/composition.go", 'package composition\nimport "example/internal/agent"\n', 'package composition\nimport "example/internal/controlplane"\n'],
    ["internal/apollo/application/service.go", 'package application\nimport "example/internal/controlplane"\n', "package application\n"],
    ["internal/apollo/workloadmetadata/metadata.go", 'package workloadmetadata\nimport "example/internal/apollo/application"\n', 'package workloadmetadata\nimport "go.yaml.in/yaml/v4"\n'],
    ["internal/apollo/workloadmetadata/metadata.go", 'package workloadmetadata\nimport "example.com/another/yaml"\n', 'package workloadmetadata\nimport "go.yaml.in/yaml/v4"\n'],
    ["internal/delivery/reconciliation/application/worker.go", 'package application\nimport "example/internal/controlplane"\n', "package application\n"],
    ["internal/controlplane/apollo_openapi_adapter.go", "package controlplane\n// invokeApolloFacade\n", "package controlplane\n"],
  ];
  for (const [path, contents, original] of controls) {
    write(path, contents);
    let rejected = false;
    try { run(); } catch { rejected = true; }
    if (!rejected) throw new Error(`planted boundary violation survived: ${path}`);
    if (original === null) rmSync(join(fixture, path));
    else write(path, original);
  }
  console.log(`PASS domain boundary regression: positive=3 planted=${controls.length} caught=${controls.length}`);
} finally {
  rmSync(fixture, { recursive: true, force: true });
}
