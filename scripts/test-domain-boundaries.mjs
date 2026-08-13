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
  write("cmd/platform-delivery/main.go", 'package main\nfunc main(){ composition.NewHubHandler() }\n');
  if (!run().includes("PASS domain boundaries")) throw new Error("positive fixture did not pass");

  const controls = [
    ["cmd/platform-delivery/main.go", 'package main\nfunc main(){ controlplane.NewHandler() }\n'],
    ["internal/reporting/domain/model.go", 'package domain\nimport "net/http"\n'],
    ["internal/hub/composition/composition.go", 'package composition\nimport "example/internal/agent"\n'],
  ];
  for (const [path, contents] of controls) {
    const original = path.includes("main.go")
      ? 'package main\nfunc main(){ composition.NewHubHandler() }\n'
      : path.includes("composition.go")
        ? 'package composition\nimport "example/internal/controlplane"\n'
        : null;
    write(path, contents);
    let rejected = false;
    try { run(); } catch { rejected = true; }
    if (!rejected) throw new Error(`planted boundary violation survived: ${path}`);
    if (original === null) rmSync(join(fixture, path));
    else write(path, original);
  }
  console.log("PASS domain boundary regression: positive=1 planted=3 caught=3");
} finally {
  rmSync(fixture, { recursive: true, force: true });
}
