import { expect, test } from "@playwright/test";
import { installPackagingReadStub, hydrate } from "./console-fixture";

installPackagingReadStub();

test("Work Intake uses agent form tools and resumes with the human's answer", async ({
  page,
}) => {
  await hydrate(page);
  const session = {
    id: "ps_browser",
    status: "needs_input",
    intake: { repoUrl: "https://github.com/org/app", ref: "main" },
    profile: { name: "onboard", version: "1", hash: "b".repeat(64) },
    needsInput: [
      {
        id: "port",
        name: "port",
        label: "Which port?",
        type: "string",
        required: true,
      },
    ],
    answers: [],
    messages: [],
    events: [],
    plan: null,
    binding: null,
    resolvedCommit: "a".repeat(40),
  };
  await page.route("**/v1/planning-sessions", (route) =>
    route.fulfill({ status: 201, json: session }),
  );
  const requests: Record<string, any>[] = [];
  await page.route("**/v1/agent-runs", async (route) => {
    const input = route.request().postDataJSON();
    requests.push(input);
    const first = requests.length === 1;
    const resumed = Boolean(input.resume);
    const rejected = requests.length === 3;
    const current = rejected
      ? {
          ...session,
          needsInput: [
            {
              ...session.needsInput[0],
              validationError: "Choose another port.",
            },
          ],
        }
      : resumed && !rejected
        ? {
            ...session,
            status: "ready",
            answers: [
              { id: "port", value: input.resume[0].payload["question:port"] },
            ],
            plan: { text: "Build reviewed input.", sha256: "c".repeat(64) },
          }
        : session;
    const events: unknown[] = [
      { type: "RUN_STARTED", threadId: input.threadId, runId: input.runId },
    ];
    if (first)
      events.push(
        { type: "TEXT_MESSAGE_START", messageId: "request", role: "assistant" },
        {
          type: "TOOL_CALL_START",
          toolCallId: "prefill",
          toolCallName: "fill_field",
          parentMessageId: "request",
        },
        {
          type: "TOOL_CALL_ARGS",
          toolCallId: "prefill",
          delta: '{"field":"question:port","value":"8080"}',
        },
        { type: "TOOL_CALL_END", toolCallId: "prefill" },
        { type: "TEXT_MESSAGE_END", messageId: "request" },
      );
    events.push(
      {
        type: "STATE_SNAPSHOT",
        snapshot: {
          session: current,
          toolCalls: [],
          pendingClientTools: first
            ? [
                {
                  id: "prefill",
                  name: "fill_field",
                  arguments: { field: "question:port", value: "8080" },
                  eligible: false,
                },
              ]
            : [],
        },
      },
      {
        type: "MESSAGES_SNAPSHOT",
        messages: [{ id: "user", role: "user", content: "Inspect" }],
      },
      {
        type: "RUN_FINISHED",
        threadId: input.threadId,
        runId: input.runId,
        outcome: first
          ? { type: "success", pendingToolCallIds: ["prefill"] }
          : resumed && !rejected
            ? { type: "success" }
            : {
                type: "interrupt",
                interrupts: [{ id: "form-current", reason: "needs_input" }],
              },
      },
    );
    if (!resumed || rejected)
      events.splice(events.length - 1, 0, {
        type: "CUSTOM",
        name: "a2ui",
        value: {
          version: "1.0",
          createSurface: {
            surfaceId: "surface-" + input.runId,
            catalogId: "conduit:planning-forms/v1",
            dataModel: { answers: { port: "" } },
            components: [
              {
                id: "root",
                component: "Column",
                children: ["question:port", "submit"],
              },
              {
                id: "question:port",
                component: "TextField",
                label:
                  'Which port? <img src=x onerror="window.formInjected=true">',
                ...(rejected ? { error: "Choose another port." } : {}),
                value: { "@path": "/answers/port" },
                "@checks": [
                  {
                    condition: {
                      "@call": "required",
                      args: { value: { "@path": "/answers/port" } },
                    },
                    message: "Enter the port.",
                  },
                ],
              },
              {
                id: "submit",
                component: "Button",
                label: "Submit answers",
                action: {
                  event: {
                    name: "submit_needs_input",
                    context: { "question:port": { "@path": "/answers/port" } },
                  },
                },
              },
            ],
          },
        },
      });
    await route.fulfill({
      contentType: "text/event-stream",
      body: events
        .map((event) => `data: ${JSON.stringify(event)}\n\n`)
        .join(""),
    });
  });
  await page.goto("/console/#s01-intake");
  await page.getByLabel("Repository URL").fill("https://github.com/org/app");
  await page.getByLabel("Ref", { exact: true }).fill("main");
  await page.evaluate(() => Reflect.get(window, "refreshHub")());
  await expect(page.getByLabel("Ref", { exact: true })).toBeFocused();
  await expect(page.getByLabel("Ref", { exact: true })).toHaveValue("main");
  await page.getByLabel("Message", { exact: true }).fill("Inspect");
  await page.locator("[data-plan-send]").click();
  await expect(page.locator('[data-plan-answer="port"]')).toHaveValue("8080");
  await expect(page.locator("[data-plan-submit-answers]")).toBeEnabled();
  await expect(page.locator("[data-plan-needs-input] img")).toHaveCount(0);
  expect(
    await page.evaluate(() => Reflect.get(window, "formInjected")),
  ).toBeUndefined();
  await page.locator('[data-plan-answer="port"]').fill("");
  await page.locator("[data-plan-submit-answers]").click();
  await expect(page.locator('[data-plan-answer="port"]')).toBeFocused();
  await expect(page.locator('[data-plan-answer="port"]')).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  expect(requests).toHaveLength(2);
  await page.locator('[data-plan-answer="port"]').fill("9090");
  await page.locator("[data-plan-submit-answers]").click();
  await expect(page.locator("[data-plan-needs-input]")).toContainText(
    "Choose another port.",
  );
  await page.locator("[data-plan-submit-answers]").click();
  expect(requests).toHaveLength(3);
  await page.locator('[data-plan-answer="port"]').fill("9091");
  await page.locator("[data-plan-submit-answers]").click();
  await expect(page.locator("[data-plan-review]")).toContainText(
    "Build reviewed input.",
  );
  expect(requests).toHaveLength(4);
  expect(requests[3].resume[0].payload).toEqual({ "question:port": "9091" });
  expect(requests[1].messages.at(-1)).toMatchObject({
    role: "tool",
    toolCallId: "prefill",
  });
  expect(requests[2].forwardedProps.conduit.a2uiAction.action).toMatchObject({
    name: "submit_needs_input",
    sourceComponentId: "submit",
    context: { "question:port": "9090" },
  });
  expect(requests[2].resume).toEqual([
    {
      interruptId: "form-current",
      status: "resolved",
      payload: { "question:port": "9090" },
    },
  ]);
  await expect(page.locator("[data-plan-binding]")).toHaveCount(0);
});
