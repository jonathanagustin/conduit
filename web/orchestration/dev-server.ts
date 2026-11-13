// Local rehearsal service, not a production Temporal deployment.
import { TestWorkflowEnvironment } from "@temporalio/testing";
import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const directory = fileURLToPath(
  new URL("../../.cache/temporal/", import.meta.url),
);
await mkdir(directory, { recursive: true });
const environment = await TestWorkflowEnvironment.createLocal({
  server: {
    ip: "127.0.0.1",
    port: 7233,
    ui: true,
    uiPort: 8233,
    dbFilename: `${directory}/rehearsal.sqlite`,
  },
});
console.log(
  "Temporal rehearsal: localhost:7233; UI http://localhost:8233; local SQLite state in .cache/temporal",
);
const keepAlive = setInterval(() => {}, 60_000);
let stopping = false;
const stop = async () => {
  if (stopping) return;
  stopping = true;
  clearInterval(keepAlive);
  await environment.teardown();
  process.exit(0);
};
process.on("SIGINT", () => void stop());
process.on("SIGTERM", () => void stop());
