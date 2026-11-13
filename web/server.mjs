import { createServer } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import next from "next";
import { attachBridge } from "./server/bridge.mjs";

const root = path.dirname(fileURLToPath(import.meta.url));
const port = Number(process.env.PORT ?? 3000);
const workspace = path.resolve(
  process.env.CONDUIT_AGENT_WORKSPACE ?? path.join(root, ".."),
);
const dev = process.argv.includes("--dev");
const demo = process.env.CONDUIT_AGENT_MODE === "demo";
const temporalDemo = process.env.CONDUIT_AGENT_MODE === "temporal-demo";
const app = next({ dev, dir: root, hostname: "127.0.0.1", port });
await app.prepare();
const server = createServer(app.getRequestHandler());
attachBridge(server, {
  workspace,
  port,
  command:
    demo || temporalDemo
      ? process.execPath
      : path.join(root, "node_modules", ".bin", "codex-acp"),
  args: temporalDemo
    ? [
        "--import",
        import.meta.resolve("tsx"),
        path.join(root, "server", "temporal-agent.ts"),
      ]
    : demo
      ? [path.join(root, "server", "demo-agent.mjs")]
      : [],
});
const nextUpgrade = app.getUpgradeHandler();
server.on("upgrade", (req, socket, head) => {
  if (req.url !== "/console/acp") void nextUpgrade(req, socket, head);
});
server.listen(port, "127.0.0.1", () =>
  console.log(
    `Conduit: http://localhost:${port}/console/ (${demo || temporalDemo ? "SIMULATION" : "local read-only Codex preview"})`,
  ),
);
