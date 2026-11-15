import assert from "node:assert/strict";
import { createServer } from "vite";
const server = await createServer({
  server: { middlewareMode: true },
  appType: "custom",
});
try {
  const { PlanningAgent } = await server.ssrLoadModule(
    "/src/console/operator/planning-agent.js",
  );
  const origin = process.env.CONDUIT_AGENT_FIXTURE_ORIGIN;
  assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/u);
  const fetcher = (path, init = {}) =>
    fetch(new URL(path, origin), {
      ...init,
      headers: { ...init.headers, "X-Platform-Subject": "alice" },
    });
  const created = await fetcher("/v1/planning-sessions", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      repoUrl: "https://github.com/org/app",
      ref: "main",
    }),
  });
  assert.equal(created.status, 201);
  const session = await created.json();
  const agent = new PlanningAgent(session.id, fetcher);
  const drafts = new Map();
  const binding = {
    fields: () =>
      new Map([["question:port", { type: "string", readOnly: false }]]),
    dirty: () => false,
    set: (field, value) => drafts.set(field, value),
    focus() {},
    confirm: async () => false,
  };
  let view;
  const update = (run) => {
    view = run.state;
  };
  const first = await agent.run(
    { message: "inspect the fixture" },
    binding,
    update,
  );
  assert.equal(first.outcome.type, "interrupt");
  assert.equal(agent.interrupt.reason, "needs_input");
  assert.equal(drafts.get("question:port"), "8080");
  assert.deepEqual(view.session.answers, []); // Prefills are not native answers.
  assert.equal(view.session.binding, null);
  const ids = agent.messages.map((item) => item.id);
  await agent.run({ recover: true }, binding, update);
  assert.deepEqual(
    agent.messages.map((item) => item.id),
    ids,
  );
  assert.deepEqual(view.session.answers, []);
  const surfaceID = [...agent.forms.surfaces.keys()].at(-1);
  agent.forms.input(surfaceID, "question:port", "9090");
  const action = agent.forms.action(surfaceID, "submit").message;
  assert.equal(action.action.context["question:port"], "9090");
  const final = await agent.run({ formAction: action }, binding, update);
  assert.equal(final.outcome.type, "success");
  assert.equal(final.forms.surfaces.size, 0);
  assert.equal(view.session.status, "ready");
  assert.equal(view.session.answers[0].value, "9090");
  assert.equal(view.session.plan.text, "Build the reviewed port 9090.");
  assert.equal(view.session.binding, null);
  console.log(
    "PASS mounted Hub / JavaScript planning client: draft prefill, ACK, interrupt, snapshot recovery, human override and unaccepted plan",
  );
} finally {
  await server.close();
}
