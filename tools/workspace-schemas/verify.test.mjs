import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createHash } from 'node:crypto';
import { inputs, verify } from './verify.mjs';
test('pinned pack verifies', () => { const {manifest,settings,files}=inputs(); verify(manifest,settings,files); });
test('changed schema bytes fail checksum', () => {
 const {manifest,settings,files}=inputs(); files['plan.schema.json'] += ' ';
 assert.throws(() => verify(manifest,settings,files), /checksum/);
});
test('missing association fails', () => {
 const {manifest,settings,files}=inputs(); delete settings['yaml.schemas']['file:///opt/conduit/workspace-schemas/v1/zarf.schema.json'];
 assert.throws(() => verify(manifest,settings,files), /associations/);
});
test('schema-store discovery fails', () => {
 const {manifest,settings,files}=inputs(); settings['yaml.schemaStore.enable']=true;
 assert.throws(() => verify(manifest,settings,files));
});
test('remote reference fails even with refreshed checksum', () => {
 const {manifest,settings,files}=inputs(); files['plan.schema.json']=JSON.stringify({$ref:'https://example.invalid/schema.json'});
 manifest.schemas[0].sha256=createHash('sha256').update(files['plan.schema.json']).digest('hex');
 assert.throws(() => verify(manifest,settings,files), /external schema reference/);
});
