import { createBridge } from "./http.ts";

const port = Number.parseInt(process.env["ASSISTANT_BRIDGE_PORT"] ?? "8787", 10);
const bridge = await createBridge({ port });
console.log(`assistant bridge listening on http://127.0.0.1:${bridge.port}`);
console.log(`workspace ${bridge.workspace}`);

async function shutdown(): Promise<void> {
  await bridge.close();
  process.exit(0);
}

process.on("SIGINT", () => {
  void shutdown();
});
process.on("SIGTERM", () => {
  void shutdown();
});
