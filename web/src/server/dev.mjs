import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../..", import.meta.url));
const children = [];

function start(command, args) {
  const child = spawn(command, args, { cwd: root, stdio: "inherit" });
  children.push(child);
  child.on("exit", (code) => {
    for (const other of children) {
      if (other !== child && other.exitCode === null) other.kill();
    }
    process.exit(code ?? 1);
  });
}

// The Hub spawns and governs workspace ACP bridges behind Hub endpoints; the
// browser development server never exposes a direct route to the agent process.
start("pnpm", ["exec", "vp", "dev"]);
