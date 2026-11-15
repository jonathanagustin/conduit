import assert from 'node:assert/strict';
import { readFile, readdir } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { networkInterfaces } from 'node:os';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createMessageConnection, StreamMessageReader, StreamMessageWriter } from 'vscode-jsonrpc/node';

// This is the real stdio server and its own filesystem/HTTP loader, not a mock
// schema resolver. The outer Docker gate supplies a network-none namespace.
assert.deepEqual(Object.keys(networkInterfaces()), ['lo'], 'run through the network-none gate');
const root = '/opt/conduit/workspace-schemas/v1';
const settings = JSON.parse(await readFile(`${root}/settings.json`, 'utf8'));
assert.equal(settings['yaml.schemaStore.enable'], false);
const server = spawn(process.execPath, [fileURLToPath(new URL('./node_modules/yaml-language-server/bin/yaml-language-server', import.meta.url)), '--stdio'], { stdio: ['pipe', 'pipe', 'inherit'] });
const rpc = createMessageConnection(new StreamMessageReader(server.stdout), new StreamMessageWriter(server.stdin));
const pending = new Map();
let configured;
const configuration = new Promise(resolve => { configured = resolve; });
rpc.onRequest('workspace/configuration', ({ items }) => {
  configured();
  const yaml = { validate: settings['yaml.validate'], completion: settings['yaml.completion'], hover: settings['yaml.hover'], schemas: settings['yaml.schemas'], schemaStore: { enable: false, url: '' } };
  return items.map(item => item.section === 'yaml' ? yaml : {});
});
rpc.onRequest('client/registerCapability', () => null);
rpc.onRequest('workspace/codeLens/refresh', () => null);
rpc.onNotification('textDocument/publishDiagnostics', result => pending.get(result.uri)?.(result.diagnostics));
rpc.listen();
try {
  await rpc.sendRequest('initialize', { processId: process.pid, rootUri: 'file:///fixtures', capabilities: { workspace: { configuration: true } } });
  await rpc.sendNotification('initialized', {});
  await Promise.race([configuration, new Promise((_, reject) => setTimeout(() => reject(Error('no settings request')), 10000))]);
  for (const name of (await readdir(`${root}/fixtures`)).sort()) {
    const valid = name.startsWith('valid.');
    const basename = name.includes('.plan.') ? 'example.plan.yaml' : name.includes('.zarf.') ? 'zarf.yaml' : 'uds-package.yaml';
    const uri = pathToFileURL(`/fixtures/${name}/${basename}`).href;
    const text = await readFile(`${root}/fixtures/${name}`, 'utf8');
    const diagnostics = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(Error(`diagnostic timeout: ${name}`)), 15000);
      pending.set(uri, value => { clearTimeout(timer); pending.delete(uri); resolve(value); });
      rpc.sendNotification('textDocument/didOpen', { textDocument: { uri, languageId: 'yaml', version: 1, text } });
    });
    assert(!diagnostics.some(d => /Unable to load|Unable to resolve|ENOENT|ECONN|ENETUNREACH/i.test(d.message)), `${name}: schema resolution failure: ${JSON.stringify(diagnostics)}`);
    if (valid) assert.deepEqual(diagnostics, [], name);
    else {
      assert(diagnostics.length > 0, `${name}: missing schema diagnostic`);
      const schemaName = name.includes('.plan.') ? 'plan' : name.includes('.zarf.') ? 'zarf' : 'uds-package';
      assert(diagnostics.some(d => d.source?.startsWith('yaml-schema:') && d.severity === 1 && d.data?.schemaUri?.includes(`file://${root}/${schemaName}.schema.json`)), `${name}: not local YAML schema diagnostics: ${JSON.stringify(diagnostics)}`);
    }
    console.log(`${name}: ${diagnostics.length} diagnostics${valid ? ' (valid)' : ''}`);
    await rpc.sendNotification('textDocument/didClose', { textDocument: { uri } });
  }
  await rpc.sendRequest('shutdown');
  await rpc.sendNotification('exit');
} finally {
  rpc.dispose(); server.kill();
}
