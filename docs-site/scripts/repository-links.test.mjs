import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';

const siteRoot = path.resolve(import.meta.dirname, '..');
const repoRoot = path.resolve(siteRoot, '..');
const blob = /https:\/\/github\.com\/jonathanagustin\/conduit\/blob\/main\/([^\s)#]+)/g;

async function pages(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await pages(full));
    else if (/\.mdx?$/.test(entry.name)) files.push(full);
  }
  return files;
}

test('GitHub blob links point at files in this repository', async () => {
  const missing = [];
  for (const file of await pages(path.join(siteRoot, 'src/content/docs'))) {
    const text = await readFile(file, 'utf8');
    for (const match of text.matchAll(blob)) {
      const relative = decodeURIComponent(match[1]);
      if (!existsSync(path.join(repoRoot, relative))) {
        missing.push(`${path.relative(siteRoot, file)} -> ${relative}`);
      }
    }
  }
  assert.deepEqual(missing, []);
});
