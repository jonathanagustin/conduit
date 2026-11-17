import assert from 'node:assert/strict';
import test from 'node:test';
import { configWasLoaded, requiredProofTests, verifyProof } from './verify-uds-cli-evidence.mjs';

const pkg = 'github.com/jonathanagustin/conduit/internal/agent/adapters/uds';
const events = () => requiredProofTests.flatMap((Test) => [
  { Action: 'run', Package: pkg, Test }, { Action: 'pass', Package: pkg, Test },
]);
const log = (values) => values.map((value) => JSON.stringify(value)).join('\n');

test('accepts exact, executed proof tests', () => {
  assert.doesNotThrow(() => verifyProof(log(events())));
});

for (const [name, alter] of [
  ['zero exit without test events', () => [{ Action: 'pass', Package: pkg }]],
  ['PASS without run', (all) => all.filter((event) => event.Action !== 'run')],
  ['run without PASS', (all) => all.filter((event) => event.Action !== 'pass')],
  ['similar test suffix', (all) => all.map((event) => ({ ...event, Test: `${event.Test}/unrelated` }))],
  ['similar package suffix', (all) => all.map((event) => ({ ...event, Package: `${pkg}/unrelated` }))],
  ['nested marker strings', (all) => [{ Action: 'output', Package: pkg, Output: log(all) }]],
  ['skipped proof', (all) => all.map((event, index) => index === 1 ? { ...event, Action: 'skip' } : event)],
  ['failure after proof', (all) => [...all, { Action: 'fail', Package: pkg }]],
  ['duplicate PASS', (all) => [...all, all[1]]],
  ['reversed PASS/run order', (all) => all.toReversed()],
]) {
  test(`refuses ${name}`, () => assert.throws(() => verifyProof(log(alter(events())))));
}
test('refuses raw runtime failure output', () => assert.throws(() => verifyProof('FATAL: ThreadSanitizer stopped before main')));

const expected = '/tmp/package/uds-config.yaml';
test('accepts single-line, wrapped and colored config diagnostics', () => {
  for (const output of [
    ` NOTE Using config file ${expected}\nkind: UDSBundle`,
    ` NOTE Using config file\n      ${expected}\nkind: UDSBundle`,
    ' NOTE Using config file\n      /tmp/package/\n      uds-config.yaml\nkind: UDSBundle',
    `\x1b[96m NOTE Using config file\x1b[0m\n      ${expected}\nkind: UDSBundle`,
  ]) assert.equal(configWasLoaded(output, expected), true);
});
test('refuses omitted, unrelated, wrong or conflicting config evidence', () => {
  for (const output of [
    '', expected, 'NOTE Using config file',
    `unrelated message Using config file ${expected}`,
    `description: Using config file ${expected}`,
    `NOTE Using config file /wrong/uds-config.yaml\n${expected}`,
    `NOTE Using config file\n /wrong/uds-config.yaml\n${expected}`,
    `NOTE Using config file ${expected}.other`,
    `NOTE Using config file ${expected}\nNOTE Using config file /wrong/uds-config.yaml`,
  ]) assert.equal(configWasLoaded(output, expected), false, output);
});

test('refuses single-line and wrapped NOTE markers in Bundle YAML metadata', () => {
  for (const marker of [
    `    NOTE Using config file ${expected}`,
    `    NOTE Using config file\n      ${expected}`,
  ]) {
    const yaml = `kind: UDSBundle\nmetadata:\n  description: |\n${marker}\n`;
    assert.equal(configWasLoaded(yaml, expected), false);
    assert.equal(configWasLoaded(`---\n${yaml}`, expected), false);
    assert.equal(configWasLoaded(yaml.replace('kind: UDSBundle\n', ''), expected), false);
  }
});

test('Bundle YAML markers cannot override genuine diagnostic prefix evidence', () => {
  const log = `NOTE Using config file\n  ${expected}\nkind: UDSBundle\nmetadata:\n  description: |\n    NOTE Using config file /wrong/uds-config.yaml\n`;
  assert.equal(configWasLoaded(log, expected), true);
});
