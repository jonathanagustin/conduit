import { spawn } from "node:child_process";
import { WebSocketServer, WebSocket } from "ws";

// A loopback-only developer bridge. Each browser connection owns one process.
export function attachBridge(
  server,
  { workspace, port, command, args = [], env = process.env },
) {
  const sockets = new WebSocketServer({
    noServer: true,
    maxPayload: 1024 * 1024,
  });
  const origins = new Set([
    `http://localhost:${port}`,
    `http://127.0.0.1:${port}`,
  ]);
  server.on("upgrade", (request, socket, head) => {
    if (request.url !== "/console/acp") return;
    if (
      !origins.has(request.headers.origin) ||
      !["127.0.0.1", "::1", "::ffff:127.0.0.1"].includes(
        request.socket.remoteAddress,
      )
    ) {
      socket.end("HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n");
      return;
    }
    sockets.handleUpgrade(request, socket, head, (ws) =>
      sockets.emit("connection", ws),
    );
  });
  sockets.on("connection", (ws) => {
    const child = spawn(command, args, {
      cwd: workspace,
      env: { ...env, NO_BROWSER: "1", INITIAL_AGENT_MODE: "read-only" },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let buffer = "";
    let stopped = false;
    const stop = () => {
      if (stopped) return;
      stopped = true;
      child.stdin.destroy();
      child.kill("SIGTERM");
      const timer = setTimeout(() => child.kill("SIGKILL"), 2000);
      timer.unref();
      child.once("exit", () => clearTimeout(timer));
    };
    const send = (value) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(value));
    };
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      buffer += chunk;
      if (buffer.length > 4 * 1024 * 1024) {
        ws.close(1011, "Agent output exceeded limit");
        stop();
        return;
      }
      let index;
      while ((index = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, index);
        buffer = buffer.slice(index + 1);
        if (!line.trim()) continue;
        try {
          send(JSON.parse(line));
        } catch {
          ws.close(1011, "Invalid ACP agent output");
          stop();
          return;
        }
      }
    });
    // Drain diagnostics without exposing credentials or arbitrary agent logs.
    child.stderr.resume();
    child.stdin.on("error", () => ws.close(1011, "Agent input closed"));
    child.on("error", () => ws.close(1011, "Could not start Codex ACP"));
    child.on("exit", () => ws.close(1011, "Codex ACP exited"));
    ws.on("message", (bytes) => {
      try {
        const message = JSON.parse(bytes.toString());
        if (
          !message ||
          typeof message !== "object" ||
          Array.isArray(message) ||
          message.jsonrpc !== "2.0"
        )
          throw Error("Invalid JSON-RPC");
        // The server chooses the workspace. Browser-supplied MCP commands and
        // filesystem roots cannot select executable programs or other roots.
        if (["session/new", "session/load"].includes(message.method)) {
          message.params = {
            ...message.params,
            cwd: workspace,
            mcpServers: [],
          };
        }
        if (
          [
            "session/set_mode",
            "session/set_config_option",
            "authenticate",
          ].includes(message.method)
        ) {
          send({
            jsonrpc: "2.0",
            id: message.id,
            error: {
              code: -32601,
              message:
                "Configure authentication and permissions on the server; this preview is read-only.",
            },
          });
          return;
        }
        if (child.stdin.writableLength > 1024 * 1024)
          throw Error("Input queue exceeded");
        child.stdin.write(JSON.stringify(message) + "\n");
      } catch {
        ws.close(1008, "Invalid ACP request");
        stop();
      }
    });
    ws.on("close", stop);
    ws.on("error", stop);
  });
  return sockets;
}
