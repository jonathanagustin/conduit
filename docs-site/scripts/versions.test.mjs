import assert from 'node:assert/strict';
import test from 'node:test';
import { relativePage, versionForPath, versionHref } from '../src/lib/versions.mjs';

const versions = [
  { id: 'current', prefix: '', label: 'Current', pages: ['index', 'architecture', 'getting-started'] },
  { id: 'older', prefix: 'v1', label: '1', pages: ['index'] },
];

test('the current channel owns unprefixed routes', () => {
  assert.equal(versionForPath('/conduit/', versions).id, 'current');
  assert.equal(versionForPath('/conduit/architecture/', versions).id, 'current');
  assert.equal(versionForPath('/conduit/v1/architecture/', versions).id, 'older');
});

test('version links keep the page when that channel has it', () => {
  assert.equal(versionHref(versions[0], 'architecture'), '/conduit/architecture/');
  assert.equal(versionHref(versions[0], 'index'), '/conduit/');
  assert.equal(versionHref(versions[1], 'architecture'), '/conduit/v1/');
  assert.equal(relativePage('/conduit/architecture/', versions[0]), 'architecture');
  assert.equal(relativePage('/conduit/', versions[0]), 'index');
});
