import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { once } from "node:events";
import { WebSocket } from "ws";
import { attachBridge } from "./bridge.mjs";

const fixture = `
const readline = require('node:readline');
const write = m => process.stdout.write(JSON.stringify({jsonrpc:'2.0',...m})+'\\n');
readline.createInterface({input:process.stdin}).on('line', line => {
 const m=JSON.parse(line);
 if(m.method==='initialize') write({id:m.id,result:{protocolVersion:1,agentCapabilities:{}}});
 if(m.method==='session/new') write({id:m.id,result:{sessionId:'fixture',received:m.params}});
 if(m.method==='session/prompt') {
   write({method:'session/update',params:{sessionId:'fixture',update:{sessionUpdate:'agent_message_chunk',content:{type:'text',text:'streamed'}}}});
   write({id:'permission',method:'session/request_permission',params:{options:[{optionId:'deny',kind:'reject_once',name:'Deny'}]}});
 }
 if(m.id==='permission' && m.result) write({method:'test/permission',params:m.result});
 if(m.method==='session/cancel') write({method:'test/cancelled',params:{}});
});
`;

async function harness(t) {
  const server = createServer();
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = server.address().port;
  const sockets = attachBridge(server, {
    port,
    workspace: process.cwd(),
    command: process.execPath,
    args: ["-e", fixture],
  });
  t.after(() => {
    for (const ws of sockets.clients) ws.terminate();
    sockets.close();
    server.close();
  });
  return { port, url: `ws://127.0.0.1:${port}/console/acp` };
}

function inbox(ws) {
  const queue = [];
  const waiting = [];
  ws.on("message", (raw) => {
    const value = JSON.parse(raw.toString());
    if (waiting.length) waiting.shift()(value);
    else queue.push(value);
  });
  return () =>
    queue.length
      ? Promise.resolve(queue.shift())
      : new Promise((resolve) => waiting.push(resolve));
}

test(
  "bridge carries handshake, streams, permission answers and cancel; pins workspace",
  { timeout: 10000 },
  async (t) => {
    const { port, url } = await harness(t);
    const ws = new WebSocket(url, { origin: `http://localhost:${port}` });
    const receive = inbox(ws);
    await once(ws, "open");
    const send = (m) => ws.send(JSON.stringify({ jsonrpc: "2.0", ...m }));
    send({ id: 1, method: "initialize", params: { protocolVersion: 1 } });
    assert.equal((await receive()).result.protocolVersion, 1);
    send({
      id: 2,
      method: "session/new",
      params: { cwd: "/other", mcpServers: [{ command: "untrusted" }] },
    });
    const created = await receive();
    assert.equal(created.result.received.cwd, process.cwd());
    assert.deepEqual(created.result.received.mcpServers, []);
    send({ id: 3, method: "session/prompt", params: {} });
    assert.equal((await receive()).params.update.content.text, "streamed");
    assert.equal((await receive()).method, "session/request_permission");
    send({
      id: "permission",
      result: { outcome: { outcome: "selected", optionId: "deny" } },
    });
    assert.equal((await receive()).params.outcome.optionId, "deny");
    send({ method: "session/cancel", params: { sessionId: "fixture" } });
    assert.equal((await receive()).method, "test/cancelled");
    send({
      id: 4,
      method: "session/set_mode",
      params: { modeId: "agent-full-access" },
    });
    assert.equal((await receive()).error.code, -32601);
    ws.close();
    await once(ws, "close");
  },
);

test(
  "bridge rejects foreign browser origins",
  { timeout: 10000 },
  async (t) => {
    const { url } = await harness(t);
    const ws = new WebSocket(url, { origin: "https://example.com" });
    const [error] = await once(ws, "error");
    assert.match(error.message, /403/);
  },
);

test("bridge closes malformed messages", { timeout: 10000 }, async (t) => {
  const { url, port } = await harness(t);
  const ws = new WebSocket(url, { origin: `http://localhost:${port}` });
  await once(ws, "open");
  ws.send("not json");
  const [code] = await once(ws, "close");
  assert.equal(code, 1008);
});
