import { createBridge } from "./http.ts";

const port = Number.parseInt(
  process.env["ASSISTANT_BRIDGE_PORT"] ?? "8787",
  10,
);
const bridgeToken = process.env["CONDUIT_BRIDGE_TOKEN"];
if (
  process.env["CONDUIT_BRIDGE_REQUIRE_TOKEN"] === "1" &&
  (!bridgeToken || bridgeToken.trim().length === 0)
) {
  throw new Error(
    "CONDUIT_BRIDGE_TOKEN is required when CONDUIT_BRIDGE_REQUIRE_TOKEN=1",
  );
}
const bridge = await createBridge({
  port,
  ...(bridgeToken !== undefined ? { bridgeToken } : {}),
  offlineBundle: process.env["CONDUIT_OFFLINE_BUNDLE"] === "1",
});
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
