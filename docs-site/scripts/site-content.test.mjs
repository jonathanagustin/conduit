import assert from 'node:assert/strict';
import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';

const siteRoot = path.resolve(import.meta.dirname, '..');
const tagline = 'Self-hosted delivery control plane that takes an upstream app from source to a governed UDS package. Every step is planned, accepted, validated, and receipted.';
const banned = /\b(palantir|apollo|openai|anthropic|claude|gemini|grok|codex|keycloak)\b/i;

async function sourceFiles(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await sourceFiles(full));
    else if (/\.(md|mdx|astro|mjs|css|svg)$/.test(entry.name)) files.push(full);
  }
  return files;
}

test('the landing page carries the tagline and the demo media', async () => {
  const home = await readFile(path.join(siteRoot, 'src/content/docs/index.mdx'), 'utf8');
  assert.match(home, new RegExp(tagline.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  assert.match(home, /conduit-excalidraw-demo\.webp/);
  assert.match(home, /conduit-excalidraw-planning-only\.webp/);
  assert.match(home, /packaging-v1/);
  assert.match(home, /appgen-v1/);
  assert.match(home, /COMPUTE_BEFORE_ACCEPT/);
});

test('curated product copy does not name unrelated products', async () => {
  const files = await sourceFiles(path.join(siteRoot, 'src'));
  const hits = [];
  for (const file of files) {
    // Generated technical references retain actual shipped command names,
    // protocol paths and native contract text. They are not marketing copy.
    if (file.startsWith(path.join(siteRoot, 'src/content/docs/reference') + path.sep)) continue;
    const text = await readFile(file, 'utf8');
    if (banned.test(text)) hits.push(path.relative(siteRoot, file));
  }
  assert.deepEqual(hits, []);
});

test('the favicon and wordmarks use the console sapphire marks', async () => {
  const favicon = await readFile(path.join(siteRoot, 'public/favicon.svg'), 'utf8');
  for (const color of ['#1c2127', '#0b1113', '#215db0', '#4c90f0', '#8abbff']) {
    assert.match(favicon, new RegExp(color, 'i'));
  }
  const dark = await readFile(path.join(siteRoot, 'src/assets/conduit-wordmark-dark.svg'), 'utf8');
  assert.match(dark, /#215db0/);
  assert.match(dark, /#4c90f0/);
  assert.match(dark, /#8abbff/);
  assert.match(dark, /Conduit/);
  const light = await readFile(path.join(siteRoot, 'src/assets/conduit-wordmark-light.svg'), 'utf8');
  assert.match(light, /#184a90/);
  assert.match(light, /#1c2127/);
});

test('the generated roadmap follows the reviewed open trackers', async () => {
  const roadmap = await readFile(path.join(siteRoot, 'src/content/docs/roadmap.md'), 'utf8');
  const record = JSON.parse(await readFile(path.join(siteRoot,'../docs/operations/readiness-evidence.json'),'utf8'));
  const issues = [...roadmap.matchAll(/github\.com\/jonathanagustin\/conduit\/issues\/(\d+)/g)].map(match => Number(match[1]));
  assert.ok(issues.length > 0);
  for (const number of issues) {
    assert.equal(record.issues.find(issue => issue.number === number)?.state, 'open', `stale roadmap tracker #${number}`);
  }
  assert.ok(!issues.includes(616));
});

test('generated native references copy no private reference corpus', async () => {
  const files = await sourceFiles(path.join(siteRoot,'src/content/docs/reference'));
  for (const file of files) {
    const text = await readFile(file,'utf8');
    assert.doesNotMatch(text, /docs\/reference-platform\/|ri\.apollo\.main\./);
  }
});
