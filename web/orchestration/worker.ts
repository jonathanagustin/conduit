import { NativeConnection, Worker } from "@temporalio/worker";
import { fileURLToPath } from "node:url";
import * as activities from "./activities";
import { TASK_QUEUE } from "./contracts";

const connection = await NativeConnection.connect({
  address: process.env.TEMPORAL_ADDRESS ?? "localhost:7233",
});
try {
  const worker = await Worker.create({
    connection,
    namespace: process.env.TEMPORAL_NAMESPACE ?? "default",
    taskQueue: TASK_QUEUE,
    workflowsPath: fileURLToPath(new URL("./workflow.ts", import.meta.url)),
    activities,
  });
  await worker.run();
} finally {
  await connection.close();
}
