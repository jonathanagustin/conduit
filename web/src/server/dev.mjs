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

start(process.execPath, ["--experimental-strip-types", "src/server/main.ts"]);
start("pnpm", ["exec", "vite"]);
