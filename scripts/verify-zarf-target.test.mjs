import assert from 'node:assert/strict';
import { test } from 'node:test';
import { inputs, verifyZarfTargets } from './verify-zarf-target.mjs';
test('all qualified Zarf targets agree', () => verifyZarfTargets(inputs()));
for (const [name, mutate] of [
 ['developer drift', value => { value.mise = value.mise.replace(/^zarf = "[^"]+"/m, 'zarf = "0.0.0"'); }],
 ['lock drift', value => { value.lock = value.lock.replace(/(\[\[tools\.zarf\]\]\s*version = ")[^"]+/, (_match, prefix) => prefix + '0.0.0'); }],
 ['scaffold drift', value => { value.scaffold = value.scaffold.replace(/const ZarfVersion = "[^"]+"/, 'const ZarfVersion = "0.0.0"'); }],
 ['validator schema drift', value => { value.validator = value.validator.replace(/zarf-v[0-9.]+\.json/, 'zarf-v0.0.0.json'); }],
 ['schema drift', value => { value.manifest.schemas.find(item => item.file === 'zarf.schema.json').version = '0.0.0'; }],
 ['one stale download among correct jobs', value => { value.workflows['platform-ci.yml'] = value.workflows['platform-ci.yml'].replace(/\/zarf\/releases\/download\/v[^/]+\//, '/zarf/releases/download/v0.0.0/'); }],
 ['one stale checksum among correct jobs', value => { value.workflows['platform-ci.yml'] = value.workflows['platform-ci.yml'].replace(/(echo ['"])[a-f0-9]{64}(\s+\/tmp\/zarf)/, (_match, before, after) => before + '0'.repeat(64) + after); }],
]) test(name, () => { const value = inputs(); mutate(value); assert.throws(() => verifyZarfTargets(value)); });
