#!/usr/bin/env node
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const directory = mkdtempSync(join(tmpdir(), "review-writing-test-"));
try {
  const fixture = join(directory, "fixture.md");
  writeFileSync(fixture, "In today's world, experts argue this groundbreaking tool plays a pivotal role.\n\n- **Impact:** It is not just fast, but transformative.\n");
  const script = join(fileURLToPath(new URL(".", import.meta.url)), "review-writing.mjs");
  const report = JSON.parse(execFileSync(process.execPath, [script, fixture, "--json"], { encoding: "utf8" }));
  assert.equal(report.confidence, "HIGH");
  assert.equal(report.confidenceMeaning, "pattern density only; not evidence of authorship");
  assert.ok(report.categories.includes("vocabulary"));
  assert.ok(report.categories.includes("phrase"));
  assert.ok(report.categories.includes("structure"));
  assert.ok(report.findings.every((finding) => finding.line === null || finding.line > 0));

  writeFileSync(fixture, "The request completed in 42 ms. The trace records revision r7.\n");
  const clean = JSON.parse(execFileSync(process.execPath, [script, fixture, "--json"], { encoding: "utf8" }));
  assert.equal(clean.patternCount, 0);
  assert.equal(clean.confidence, "LOW");
  process.stdout.write("review-writing: 2 fixtures passed\n");
} finally {
  rmSync(directory, { recursive: true, force: true });
}
