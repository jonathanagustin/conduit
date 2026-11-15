import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const pack = new URL('../../workspace-schemas/v1/', import.meta.url);
export function verify(manifest, settings, files) {
  assert.equal(manifest.packVersion, 1);
  assert.equal(manifest.installPath, '/opt/conduit/workspace-schemas/v1');
  assert.deepEqual(manifest.schemas.map(s => s.file).sort(), ['plan.schema.json', 'uds-package.schema.json', 'zarf.schema.json']);
  const associations = {};
  for (const item of manifest.schemas) {
    assert.equal(createHash('sha256').update(files[item.file]).digest('hex'), item.sha256, `checksum: ${item.file}`);
    if (item.license) assert.equal(createHash('sha256').update(files[item.license]).digest('hex'), item.licenseSha256, `license: ${item.file}`);
    const visit = value => {
      if (!value || typeof value !== 'object') return;
      for (const [key, child] of Object.entries(value)) {
        if (key === '$ref' || key === '$dynamicRef' || key === '$recursiveRef') assert(child.startsWith('#'), `external schema reference: ${child}`);
        visit(child);
      }
    };
    visit(JSON.parse(files[item.file]));
    const name = item.file.split('.')[0];
    const names = name === 'plan' ? ['**/*.plan.yaml', '**/*.plan.yml'] : [`**/${name === 'uds-package' ? 'uds-package' : 'zarf'}.yaml`, `**/${name === 'uds-package' ? 'uds-package' : 'zarf'}.yml`];
    associations[`file://${manifest.installPath}/${item.file}`] = names;
  }
  assert.equal(settings['yaml.schemaStore.enable'], false);
  assert.equal(settings['yaml.schemaStore.url'], '');
  for (const key of ['validate', 'completion', 'hover']) assert.equal(settings[`yaml.${key}`], true);
  assert.deepEqual(settings['yaml.schemas'], associations, 'local file associations drifted');
}
export function inputs() {
  const manifest = JSON.parse(readFileSync(new URL('manifest.json', pack)));
  const settings = JSON.parse(readFileSync(new URL('settings.json', pack)));
  const files = {};
  for (const item of manifest.schemas) for (const name of [item.file, item.license].filter(Boolean)) files[name] = readFileSync(new URL(name, pack), 'utf8');
  return { manifest, settings, files };
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const { manifest, settings, files } = inputs(); verify(manifest, settings, files);
  const workflow = readFileSync(new URL('../../.github/workflows/platform-ci.yml', import.meta.url), 'utf8');
  assert(workflow.includes(`/zarf/releases/download/v${manifest.schemas.find(s => s.file === 'zarf.schema.json').version}/`), 'schema differs from CI-validated Zarf target');
  const uds = readFileSync(new URL('../../scripts/uds-up.sh', import.meta.url), 'utf8');
  assert(uds.includes(manifest.schemas.find(s => s.file === 'uds-package.schema.json').version), 'schema differs from UDS default target');
  console.log('schema checksums, contained references, target versions and associations verified');
}
