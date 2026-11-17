#!/usr/bin/env node
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const root = new URL('../', import.meta.url);
export function inputs() {
  const read = path => readFileSync(new URL(path, root), 'utf8');
  return {
    manifest: JSON.parse(read('workspace-schemas/v1/manifest.json')),
    mise: read('mise.toml'), lock: read('mise.lock'),
    scaffold: read('internal/packaging/domain/scaffold.go'), validator: read('scripts/verify-packaging-scaffold.py'),
    workflows: Object.fromEntries(['platform-ci.yml', 'platform-images.yml', 'platform-uds-lifecycle.yml', 'demo.yaml'].map(name => [name, read(`.github/workflows/${name}`)])),
  };
}
export function verifyZarfTargets({manifest, mise, lock, workflows, scaffold, validator}) {
  const version = manifest.schemas.find(item => item.file === 'zarf.schema.json').version;
  assert.equal(/^zarf\s*=\s*"([^"]+)"/m.exec(mise)?.[1], version, 'developer Zarf target differs from schema');
  assert.equal(/\[\[tools\.zarf\]\]\s*version = "([^"]+)"/.exec(lock)?.[1], version, 'locked Zarf target differs from schema');
  const platform = /\[tools\.zarf\."platforms\.linux-x64"\]([\s\S]*?)(?=\n\[|$)/.exec(lock)?.[1] || '';
  const checksum = /checksum = "sha256:([a-f0-9]{64})"/.exec(platform)?.[1];
  assert(checksum, 'missing locked Linux Zarf checksum');
  const asset = `https://github.com/zarf-dev/zarf/releases/download/v${version}/zarf_v${version}_Linux_amd64`;
  assert(platform.includes(`url = "${asset}"`), 'locked Zarf asset differs');
  assert.equal(/const ZarfVersion = "([^"]+)"/.exec(scaffold)?.[1], version, 'scaffold Zarf target differs');
  assert(validator.includes(`"zarf-v${version}.json": "${manifest.schemas.find(item => item.file === 'zarf.schema.json').sha256}"`), 'structural validator Zarf schema differs');
  for (const [name, text] of Object.entries(workflows)) {
    const downloads = [...text.matchAll(/https:\/\/github\.com\/zarf-dev\/zarf\/releases\/download\/[^\s\\]+/g)];
    assert(downloads.length, `${name}: missing qualified Zarf download`);
    for (const download of downloads) {
      assert.equal(download[0], asset, `${name}: Zarf download target differs`);
      const following = text.slice(download.index + download[0].length, download.index + download[0].length + 450);
      assert.equal(/echo ['"]([a-f0-9]{64})\s+\/tmp\/zarf/.exec(following)?.[1], checksum, `${name}: Zarf checksum differs`);
    }
  }
  return version;
}
if (process.argv[1] === fileURLToPath(import.meta.url)) console.log(`PASS Zarf developer, lock, workflow and schema targets: ${verifyZarfTargets(inputs())}`);
