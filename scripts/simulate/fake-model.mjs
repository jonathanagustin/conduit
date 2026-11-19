// A loopback stand-in for an OpenAI-compatible model, for the console
// simulation only (scripts/simulate-console.sh). It is not a model and proves
// nothing about one: it exists so the Hub's assistant loop, its audit, its
// thread store and its event stream run for real underneath the live specs.
//
// It answers POST <base>/chat/completions with one fixed, stateless script:
// asked for the first time in a turn it calls query_ontology_objects for every
// Environment; once the prompt carries that round's tool results it answers in
// text. The reply is the assistant's own reply object, as message content.
import http from "node:http";

const port = Number.parseInt(process.argv[2] ?? "", 10);
if (!Number.isInteger(port) || port <= 0) {
  console.error("usage: fake-model.mjs <port>");
  process.exit(2);
}

const MODEL = "simulated-model";
const ROUND = /Round \d+ of this turn: your reply and the tool results/;
// Only the opt-in planning fixture can hold a reply. These counters witness
// a real cancelled HTTP call; the production model transport is unchanged.
const planningRepo = process.env.CONDUIT_SIMULATION_PLANNING_REPO || "";
const planning = { started: 0, active: 0, disconnected: 0, expired: 0 };
const packagingRun = process.env.CONDUIT_SIMULATION_PACKAGING_RUN || "";
const packaging = { calls: 0 };

function reply(prompt) {
  if (!ROUND.test(prompt))
    return {
      text: "",
      toolCalls: [
        {
          name: "query_ontology_objects",
          arguments: { objectType: "Environment" },
        },
      ],
    };
  const results = prompt.match(/^result \S+ \S+: /gm) ?? [];
  return {
    text: `Simulated answer: read ${results.length} tool result(s) from the Hub.`,
    toolCalls: [],
  };
}

const server = http.createServer((request, response) => {
  const chunks = [];
  request.on("data", (chunk) => chunks.push(chunk));
  request.on("end", () => {
    const send = (status, body) => {
      response.writeHead(status, { "content-type": "application/json" });
      response.end(JSON.stringify(body));
    };
    if (
      planningRepo &&
      request.method === "GET" &&
      request.url === "/planning-fixture"
    )
      return send(200, planning);
    if (
      packagingRun &&
      request.method === "GET" &&
      request.url === "/packaging-fixture"
    )
      return send(200, packaging);
    if (
      request.method !== "POST" ||
      !request.url?.endsWith("/chat/completions")
    )
      return send(404, { error: { message: "not found" } });
    let prompt = "";
    try {
      const body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
      prompt = (body.messages ?? [])
        .map((message) => String(message?.content ?? ""))
        .join("\n");
    } catch {
      return send(400, { error: { message: "body is not JSON" } });
    }
    if (planningRepo && prompt.includes(`Repository: ${planningRepo}\n`)) {
      planning.started++;
      planning.active++;
      const deadline = setTimeout(() => {
        planning.expired++;
        send(504, {
          error: { message: "Planning cancellation fixture expired." },
        });
      }, 60_000);
      response.once("close", () => {
        clearTimeout(deadline);
        planning.active--;
        if (!response.writableEnded) planning.disconnected++;
      });
      return;
    }
    let proposal;
    if (
      packagingRun &&
      prompt.includes(`Packaging run: ${packagingRun}\n`) &&
      prompt.includes("memory limit must not exceed 1Gi")
    ) {
      packaging.calls++;
      if (packaging.calls >= 3)
        return send(503, {
          error: { message: "Fixture provider unavailable" },
        });
      proposal = {
        field: "memory",
        value: packaging.calls === 1 ? "512Mi" : "2Gi",
        rationale:
          "Recorded fixture proposal; separate human review is required.",
      };
    }
    // Long enough that a browser sees the tool call before the answer.
    setTimeout(
      () =>
        send(200, {
          id: "chatcmpl-simulated",
          object: "chat.completion",
          model: MODEL,
          choices: [
            {
              index: 0,
              finish_reason: "stop",
              message: {
                role: "assistant",
                content: JSON.stringify(proposal || reply(prompt)),
              },
            },
          ],
          usage: { prompt_tokens: 1, completion_tokens: 1 },
        }),
      250,
    );
  });
});

server.listen(port, "127.0.0.1", () => {
  console.log(`fake-model: listening on 127.0.0.1:${port}`);
});
