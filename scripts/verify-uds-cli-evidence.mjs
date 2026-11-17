#!/usr/bin/env node
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

const pkg = 'github.com/jonathanagustin/conduit/internal/agent/adapters/uds';
const crypto = 'TestPinnedCLIRejectsUntrustedBundleAndIsolatesConfiguration';
const policy = 'TestWrongPublisherIdentityCannotApply';
export const requiredProofTests = Object.freeze([
  crypto, `${crypto}/matching_signer`, `${crypto}/wrong_signer`, `${crypto}/unsigned`,
  `${policy}/matching_publisher`, `${policy}/wrong_identity`, `${policy}/wrong_issuer`,
  'TestDefaultRunnerIsolatesConfigurationAndPreservesExplicitAccess',
  'TestPinnedCLINativeValuesPreserveReviewedTypes',
  'TestPinnedCLINativeBundleValuesPreserveReviewedTypes',
]);

// A zero process exit or a package-level PASS is not test execution evidence.
// Track exact package/test identities and require one ordered run/pass pair.
export function verifyProof(log) {
  const state = new Map(requiredProofTests.map((test) => [test, 0]));
  for (const line of log.split(/\r?\n/).filter((line) => line.trim())) {
    let event;
    try { event = JSON.parse(line); } catch { throw new Error('invalid Go test JSON output'); }
    if (!event || typeof event !== 'object' || Array.isArray(event)) throw new Error('invalid Go test event');
    if (event.Action === 'fail') throw new Error('Go test reported a failure');
    if (event.Package !== pkg || !state.has(event.Test)) continue;
    if (event.Action === 'run') {
      if (state.get(event.Test) !== 0) throw new Error('duplicate or out-of-order proof run');
      state.set(event.Test, 1);
    } else if (event.Action === 'pass') {
      if (state.get(event.Test) !== 1) throw new Error('proof PASS without exactly one preceding run');
      state.set(event.Test, 2);
    } else if (event.Action === 'skip') {
      throw new Error('required proof test was skipped');
    }
  }
  const missing = [...state].filter(([, status]) => status !== 2).map(([test]) => test);
  if (missing.length) throw new Error(`verification did not execute required proof tests: ${missing.join(', ')}`);
}

// Pinned UDS emits a NOTE before the bundle YAML. Match the diagnostic and its
// associated exact path, including line wrapping; a path elsewhere is no proof.
export function configWasLoaded(log, expected) {
  if (!expected || /[\r\n]/.test(expected)) return false;
  const lines = log.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, '').split(/\r?\n/);
  let found = false;
  for (let i = 0; i < lines.length; i++) {
    // Everything from the first YAML mapping/document boundary is bundle
    // data. In particular a description block may contain forged NOTE lines.
    if (/^(?:---(?:\s|$)|[A-Za-z_][A-Za-z0-9_.-]*\s*:(?:\s|$))/.test(lines[i].trim())) break;
    const marker = lines[i].trim().match(/^NOTE\s+Using config file(?:\s+(.*))?$/);
    if (!marker) continue;
    let observed = (marker[1] ?? '').trim();
    while (observed.length < expected.length && expected.startsWith(observed) && i + 1 < lines.length) {
      const next = lines[++i].trim();
      if (!next) break;
      observed += next;
    }
    if (observed !== expected) return false;
    found = true;
  }
  return found;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const [, , mode, file, expected] = process.argv;
    if (!file) throw new Error('evidence input is required');
    const log = readFileSync(file, 'utf8');
    if (mode === 'proof') {
      verifyProof(log);
      console.log(`PASS UDS executor: actual correct-key/wrong-key/unsigned crypto, injected certificate policy, child configuration isolation and native scalar values (${requiredProofTests.length} exact run+pass receipts)`);
    } else if (mode === 'config') {
      if (!configWasLoaded(log, expected)) throw new Error('expected UDS config-loaded evidence is missing or names another path');
    } else {
      throw new Error('unknown UDS evidence mode');
    }
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
