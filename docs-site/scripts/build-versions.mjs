import { cp, mkdir, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const requiredPages = [
  'index',
  'getting-started',
  'how-it-works',
  'architecture',
  'packaging',
  'roadmap',
];

const demoFiles = [
  'conduit-excalidraw-demo.webp',
  'conduit-excalidraw-planning-only.webp',
];

async function pageSlugs(directory, prefix = '') {
  const pages = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const name = `${prefix}${entry.name}`;
    if (entry.isDirectory()) pages.push(...await pageSlugs(path.join(directory, entry.name), `${name}/`));
    else if (/\.mdx?$/.test(name) && name !== '404.md') pages.push(name.replace(/\.mdx?$/, ''));
  }
  return pages.sort();
}

export async function buildVersions(siteRoot = path.resolve(import.meta.dirname, '..')) {
  const repoRoot = path.resolve(siteRoot, '..');
  const docs = path.join(siteRoot, 'src/content/docs');
  const pages = await pageSlugs(docs);
  const missing = requiredPages.filter((slug) => !pages.includes(slug));
  if (missing.length) throw new Error(`Missing docs pages: ${missing.join(', ')}`);

  const demoOut = path.join(siteRoot, 'public/demo');
  await mkdir(demoOut, { recursive: true });
  for (const name of demoFiles) {
    await cp(path.join(repoRoot, 'docs/ui', name), path.join(demoOut, name));
  }

  const versions = [{
    id: 'current',
    prefix: '',
    ref: 'main',
    label: 'Current',
    pages,
  }];
  const generated = path.join(siteRoot, 'src/generated');
  await mkdir(generated, { recursive: true });
  await writeFile(path.join(generated, 'versions.json'), `${JSON.stringify(versions, null, 2)}\n`);
  console.log(`build-versions: current pages=${pages.join(',')}`);
  return versions;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await buildVersions();
}
