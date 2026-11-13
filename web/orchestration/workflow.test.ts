import test from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { randomUUID } from "node:crypto";
import { TestWorkflowEnvironment } from "@temporalio/testing";
import {
  bundleWorkflowCode,
  Worker,
  Runtime,
  DefaultLogger,
} from "@temporalio/worker";
import * as activities from "./activities";
import {
  APPROVAL_SIGNAL,
  STATE_QUERY,
  TASK_QUEUE,
  type DeliveryState,
  type Proposal,
} from "./contracts";

Runtime.install({ logger: new DefaultLogger("ERROR") });

test("Temporal delivery rehearsal", { timeout: 180000 }, async (t) => {
  const environment = await TestWorkflowEnvironment.createTimeSkipping();
  try {
    const workflowBundle = await bundleWorkflowCode({
      workflowsPath: fileURLToPath(new URL("./workflow.ts", import.meta.url)),
    });
    const worker = (overrides = {}) =>
      Worker.create({
        connection: environment.nativeConnection,
        taskQueue: TASK_QUEUE,
        workflowBundle,
        maxCachedWorkflows: 0,
        shutdownGraceTime: "1 second",
        shutdownForceTime: "5 seconds",
        activities: { ...activities, ...overrides },
      });
    const start = (failValidation = false) =>
      environment.client.workflow.start<
        (proposal: Proposal) => Promise<DeliveryState>
      >("deliveryRehearsal", {
        workflowId: `rehearsal-test-${randomUUID()}`,
        taskQueue: TASK_QUEUE,
        args: [{ summary: "Dependency update", failValidation }],
      });
    const waitApproval = async (handle: Awaited<ReturnType<typeof start>>) => {
      for (let attempt = 0; attempt < 300; attempt++) {
        if (
          (await handle.query<DeliveryState>(STATE_QUERY)).status ===
          "awaiting_approval"
        )
          return;
        await new Promise((resolve) => setTimeout(resolve, 10));
      }
      throw new Error("Workflow did not reach approval gate");
    };

    await t.test(
      "worker restart retains evidence and pending approval; duplicate decisions do not redeploy",
      async () => {
        let validations = 0,
          deployments = 0;
        const adapters = {
          validateProposal: async (
            input: Parameters<typeof activities.validateProposal>[0],
          ) => {
            validations++;
            return activities.validateProposal(input);
          },
          simulateDeployment: async (
            input: Parameters<typeof activities.simulateDeployment>[0],
          ) => {
            deployments++;
            return activities.simulateDeployment(input);
          },
        };
        let handle!: Awaited<ReturnType<typeof start>>;
        await (
          await worker(adapters)
        ).runUntil(async () => {
          handle = await start();
          await waitApproval(handle);
        });
        await (
          await worker(adapters)
        ).runUntil(async () => {
          assert.equal(
            (await handle.query<DeliveryState>(STATE_QUERY)).evidence?.passed,
            true,
          );
          await handle.signal(APPROVAL_SIGNAL, true);
          await handle.signal(APPROVAL_SIGNAL, false);
          const result = await handle.result();
          assert.equal(result.status, "completed");
          assert.equal(result.receipt?.deployed, false);
          assert.equal(result.receipt?.simulation, true);
        });
        assert.equal(validations, 1);
        assert.equal(deployments, 1);
      },
    );
    await t.test("denial never schedules deployment", async () => {
      let deployments = 0;
      await (
        await worker({
          simulateDeployment: async () => {
            deployments++;
            throw Error("must not deploy");
          },
        })
      ).runUntil(async () => {
        const handle = await start();
        await waitApproval(handle);
        await handle.signal(APPROVAL_SIGNAL, false);
        assert.equal((await handle.result()).status, "denied");
      });
      assert.equal(deployments, 0);
    });
    await t.test(
      "failed validation never requests approval or deploys",
      async () => {
        await (
          await worker()
        ).runUntil(async () => {
          const result = await (await start(true)).result();
          assert.equal(result.status, "failed");
          assert.equal(result.receipt, undefined);
        });
      },
    );
    await t.test(
      "transient activity failures retry with the same step identity",
      async () => {
        const stepIds: string[] = [];
        await (
          await worker({
            validateProposal: async (
              input: Parameters<typeof activities.validateProposal>[0],
            ) => {
              stepIds.push(input.stepId);
              if (stepIds.length < 3) throw Error("transient fixture fault");
              return activities.validateProposal(input);
            },
          })
        ).runUntil(async () => {
          const handle = await start();
          await waitApproval(handle);
          await handle.signal(APPROVAL_SIGNAL, false);
          assert.equal((await handle.result()).status, "denied");
        });
        assert.equal(stepIds.length, 3);
        assert.equal(new Set(stepIds).size, 1);
      },
    );
    await t.test(
      "permanent activity failure stops at three attempts",
      async () => {
        let attempts = 0;
        await (
          await worker({
            validateProposal: async () => {
              attempts++;
              throw Error("persistent fixture fault");
            },
          })
        ).runUntil(async () => {
          const result = await (await start()).result();
          assert.equal(result.status, "failed");
          assert.equal(result.receipt, undefined);
        });
        assert.equal(attempts, 3);
      },
    );
    await t.test(
      "cancellation while waiting for approval stops without deployment",
      async () => {
        await (
          await worker()
        ).runUntil(async () => {
          const handle = await start();
          await waitApproval(handle);
          await handle.cancel();
          const result = await handle.result();
          assert.equal(result.status, "cancelled");
          assert.equal(result.receipt, undefined);
        });
      },
    );
    await t.test("approval expiry does not become success", async () => {
      await (
        await worker()
      ).runUntil(async () => {
        const handle = await start();
        await waitApproval(handle);
        const result = await handle.result();
        assert.equal(result.status, "expired");
        assert.equal(result.receipt, undefined);
      });
    });
  } finally {
    await environment.teardown();
  }
});
