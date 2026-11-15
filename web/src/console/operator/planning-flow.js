import { PlanningAgent } from "./planning-agent.js";

// Planning flow for the Work Intake screen: composer, inspection rows, Needs
// Input card, plan review, Accept, and the binding panel. Every value shown
// here comes from the planning-session API (api/planning-sessions.yaml). When
// there is no session the screen shows the empty composer and nothing else.

const BASE = "/v1/planning-sessions";

function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/** What the operator reads for each stable error code the API returns. */
export const ERROR_TEXT = {
  INVALID_INTAKE: "The repository URL or ref was refused.",
  SESSION_NOT_FOUND: "This planning session no longer exists.",
  MODEL_NOT_CONFIGURED: "No planning model is configured on this Hub.",
  PLAN_REQUIRED: "There is no plan to accept yet. Send a message to get one.",
  COMMIT_NOT_PINNED:
    "The source commit is not pinned, so the plan cannot be bound. Run another turn.",
  NEEDS_INPUT_PENDING: "Answer every required question before Accept.",
  TURN_IN_PROGRESS:
    "A planning turn is already running. Wait for it before changing answers or accepting.",
  ALREADY_ACCEPTED: "This plan is already accepted. Its binding is below.",
  NOT_ACCEPTED: "This session has not been accepted.",
  COMPUTE_BEFORE_ACCEPT: "Compute is refused until the plan is accepted.",
  COMPUTE_NOT_CONFIGURED: "Package compute is not configured on this Hub.",
  NO_PENDING_QUESTIONS: "There are no questions waiting for an answer.",
  UNKNOWN_QUESTION: "An answer did not match a pending question.",
  INVALID_ANSWER: "An answer is not valid for its question.",
};

export class PlanningError extends Error {
  constructor(status, code, message) {
    super(message);
    this.name = "PlanningError";
    this.status = status;
    this.code = code || "";
  }
}

/** Text for an error: the code's wording, plus the Hub's own reason. */
export function describeError(error) {
  if (!error) return "";
  const code = error.code || "";
  const known = ERROR_TEXT[code];
  const reason = error.message || "";
  if (known && reason && !known.includes(reason)) return `${known} ${reason}`;
  if (known) return known;
  return reason || "The planning request failed.";
}

async function request(
  fetchImpl,
  method,
  path,
  body,
  accept = "application/json",
) {
  let response;
  try {
    response = await fetchImpl(path, {
      method,
      headers: {
        accept,
        ...(body === undefined ? {} : { "content-type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new PlanningError(0, "", "The request did not reach the Hub.");
  }
  if (!response.ok) {
    let detail = {};
    try {
      detail = await response.json();
    } catch {
      detail = {};
    }
    throw new PlanningError(
      response.status,
      detail.code,
      detail.error || `The Hub answered ${response.status}.`,
    );
  }
  return response;
}

export async function createSession(repoUrl, ref, fetchImpl = fetch) {
  const response = await request(fetchImpl, "POST", BASE, { repoUrl, ref });
  return response.json();
}

export async function getSession(id, fetchImpl = fetch) {
  const response = await request(
    fetchImpl,
    "GET",
    `${BASE}/${encodeURIComponent(id)}`,
  );
  return response.json();
}

export async function submitAnswers(id, answers, fetchImpl = fetch) {
  const response = await request(
    fetchImpl,
    "POST",
    `${BASE}/${encodeURIComponent(id)}/answers`,
    { answers },
  );
  return response.json();
}

export async function acceptSession(id, fetchImpl = fetch) {
  const response = await request(
    fetchImpl,
    "POST",
    `${BASE}/${encodeURIComponent(id)}/accept`,
    {},
  );
  return response.json();
}

/**
 * Splits buffered server-sent text into complete events. Returns the parsed
 * events and the unfinished remainder to prepend to the next chunk.
 */
export function parseSSE(buffer) {
  const events = [];
  const frames = buffer.replace(/\r\n/g, "\n").split("\n\n");
  const rest = frames.pop() ?? "";
  for (const frame of frames) {
    const data = frame
      .split("\n")
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).replace(/^ /, ""))
      .join("\n");
    if (!data) continue;
    try {
      events.push(JSON.parse(data));
    } catch {
      // A frame that is not JSON is not an event of this contract.
    }
  }
  return { events, rest };
}

/**
 * Runs one turn and calls onEvent for each streamed event as it arrives. A
 * JSON reply (no stream) is returned as the session instead.
 */
export async function startTurn(id, message, onEvent, fetchImpl = fetch) {
  const response = await request(
    fetchImpl,
    "POST",
    `${BASE}/${encodeURIComponent(id)}/turns`,
    { message },
    "text/event-stream",
  );
  const type = response.headers?.get?.("content-type") || "";
  if (!type.includes("text/event-stream") || !response.body) {
    return { session: await response.json(), events: [] };
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const seen = [];
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
    const parsed = parseSSE(done ? `${buffer}\n\n` : buffer);
    buffer = parsed.rest;
    for (const event of parsed.events) {
      seen.push(event);
      onEvent?.(event);
    }
    if (done) break;
  }
  return { session: null, events: seen };
}

/** One compact row per recorded tool call, in event order. */
export function toolRows(events) {
  const rows = [];
  for (const event of Array.isArray(events) ? events : []) {
    if (event?.type !== "inspection" || !event.inspection) continue;
    for (const call of event.inspection.toolCalls || []) {
      rows.push({ id: `${event.id}:${rows.length}`, call: String(call) });
    }
  }
  return rows;
}

/** Splits plan text at Markdown headings. Text before a heading is its own section. */
export function planSections(text) {
  const sections = [];
  let current = null;
  for (const line of String(text || "").split(/\r?\n/)) {
    const heading = /^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$/.exec(line);
    if (heading) {
      current = { title: heading[1], lines: [] };
      sections.push(current);
    } else {
      if (!current) {
        current = { title: "", lines: [] };
        sections.push(current);
      }
      current.lines.push(line);
    }
  }
  return sections
    .map((section) => ({
      title: section.title,
      body: section.lines.join("\n").trim(),
    }))
    .filter((section) => section.title || section.body);
}

/** Answered means the session holds a non-blank answer for that question id. */
export function answerMap(session) {
  const map = {};
  for (const answer of session?.answers || []) {
    if (String(answer.value ?? "").trim()) map[answer.id] = answer.value;
  }
  return map;
}

/** Questions waiting for an answer that the session has not recorded. */
export function openQuestions(session) {
  const answered = answerMap(session);
  return (session?.needsInput || []).filter((q) => !answered[q.id]);
}

/** Accept needs a plan, no unanswered required question, and no prior Accept. */
export function acceptState(session) {
  if (!session) return { ok: false, reason: "Start a session first." };
  if (session.binding || session.status === "accepted") {
    return { ok: false, reason: "Already accepted." };
  }
  if (!session.plan || !String(session.plan.text || "").trim()) {
    return { ok: false, reason: "No plan has been written yet." };
  }
  if (openQuestions(session).some((q) => q.required || q.validationError)) {
    return { ok: false, reason: "Answer every required question." };
  }
  return { ok: true, reason: "" };
}

/** Rows for the binding panel, straight from the recorded binding. */
export function bindingRows(binding) {
  if (!binding) return [];
  return [
    ["Profile", `${binding.profile?.name || ""}`],
    ["Profile version", binding.profile?.version || ""],
    ["Profile hash", binding.profile?.hash || ""],
    ["Repository", binding.intake?.repoUrl || ""],
    ["Ref", binding.intake?.ref || ""],
    ["Commit SHA", binding.resolvedCommit || ""],
    ["Model", binding.model || ""],
    ["Plan hash", binding.plan?.sha256 || ""],
    ["Accepted", binding.acceptedAt || ""],
  ];
}

/** Fresh UI state. No session, no events, nothing prefilled. */
export function emptyPlanning() {
  return {
    repoUrl: "",
    ref: "",
    message: "",
    session: null,
    events: [],
    drafts: {},
    busy: "",
    error: null,
    agent: null,
    agentState: null,
    agentMessages: [],
    agentTools: [],
  };
}

/** Folds one streamed event into the state. */
export function applyEvent(planning, event) {
  if (!event || event.type === "done") return planning;
  const events = [...planning.events, event];
  const next = { ...planning, events };
  if (event.type === "error") {
    next.error = new PlanningError(0, event.code, event.error);
  }
  return next;
}

function questionField(question, draft) {
  const id = esc(question.id);
  const label = `<label class="plan-label" for="plan-q-${id}">${esc(question.label || question.name)}${question.required ? ' <span class="plan-req" title="Required">*</span>' : ""}</label>`;
  const value = draft ?? question.value ?? "";
  if (question.type === "select" && Array.isArray(question.options)) {
    const options = [`<option value="">Choose one</option>`]
      .concat(
        question.options.map(
          (option) =>
            `<option value="${esc(option)}"${option === value ? " selected" : ""}>${esc(option)}</option>`,
        ),
      )
      .join("");
    return `${label}<select class="kit-select" id="plan-q-${id}" data-plan-answer="${id}">${options}</select>`;
  }
  if (question.type === "boolean") {
    const choices = ["true", "false"]
      .map(
        (option) =>
          `<option value="${option}"${option === value ? " selected" : ""}>${option === "true" ? "Yes" : "No"}</option>`,
      )
      .join("");
    return `${label}<select class="kit-select" id="plan-q-${id}" data-plan-answer="${id}"><option value="">Choose one</option>${choices}</select>`;
  }
  return `${label}<input type="text" class="kit-input" id="plan-q-${id}" data-plan-answer="${id}" value="${esc(value)}" autocomplete="off" />`;
}

function errorBanner(error) {
  if (!error) return "";
  const code = error.code
    ? `<span class="code-id">${esc(error.code)}</span> `
    : "";
  return `<div class="plan-alert kit-nonideal" role="alert" data-plan-error>${code}${esc(describeError(error))}</div>`;
}

function composer(planning) {
  const busy = Boolean(planning.busy);
  const locked = Boolean(planning.session);
  const sendLabel =
    planning.busy === "turn" ? "Working…" : locked ? "Send message" : "Send";
  return `<section class="kit-panel kit-pad plan-card" data-plan-composer aria-label="Composer">
    <div class="kit-section">Plan new work</div>
    <div class="plan-fields">
      <div><label class="plan-label" for="plan-repo">Repository URL</label>
        <input type="text" class="kit-input" id="plan-repo" data-plan-field="repoUrl" value="${esc(planning.repoUrl)}" placeholder="https://host/org/repo" autocomplete="off"${locked ? " disabled" : ""} /></div>
      <div><label class="plan-label" for="plan-ref">Ref</label>
        <input type="text" class="kit-input" id="plan-ref" data-plan-field="ref" value="${esc(planning.ref)}" placeholder="branch, tag, or commit" autocomplete="off"${locked ? " disabled" : ""} /></div>
    </div>
    <label class="plan-label" for="plan-message">Message</label>
    <textarea class="kit-input plan-message" id="plan-message" data-plan-field="message" rows="3" placeholder="What should this work achieve?">${esc(planning.message)}</textarea>
    <div class="plan-actions">
      <button type="button" class="kit-btn kit-btn-sm" data-plan-send${busy ? " disabled" : ""}>${sendLabel}</button>
      ${locked ? `<span class="kit-muted">Session <span class="code-id">${esc(planning.session.id)}</span> · ${esc(planning.session.status)}</span>` : ""}
    </div>
  </section>`;
}

function activity(planning) {
  if (planning.agentMessages?.length || planning.agentTools?.length) {
    const messages = planning.agentMessages
      .filter(
        (item) =>
          item.role !== "tool" &&
          typeof item.content === "string" &&
          item.content,
      )
      .map(
        (item) =>
          `<div class="plan-line" data-plan-message><span class="kit-badge">${esc(item.role)}</span><span>${esc(item.content)}</span></div>`,
      )
      .join("");
    const calls = (planning.agentTools || [])
      .map(
        (item) =>
          `<div class="plan-tool" data-plan-tool><span class="code-id">${esc(item.name)}</span><span>${esc(item.status)}</span></div>`,
      )
      .join("");
    return `<section class="kit-panel kit-pad plan-card" data-plan-activity aria-label="Agent activity" aria-live="polite"><div class="kit-section">Agent activity</div>${messages}${calls}</section>`;
  }
  const rows = toolRows(
    planning.events.length ? planning.events : planning.session?.events,
  );
  const lines = (
    planning.events.length ? planning.events : planning.session?.events || []
  )
    .filter((event) => event.type === "message" && event.message)
    .map(
      (event) =>
        `<div class="plan-line" data-plan-message><span class="kit-badge kit-badge-${event.message.role === "model" ? "ai" : "muted"}">${esc(event.message.role)}</span><span>${esc(event.message.text)}</span></div>`,
    );
  if (!rows.length && !lines.length) return "";
  const tools = rows
    .map(
      (row) =>
        `<div class="plan-tool" data-plan-tool><span class="kit-badge kit-badge-info">tool</span><span class="code-id">${esc(row.call)}</span></div>`,
    )
    .join("");
  return `<section class="kit-panel kit-pad plan-card" data-plan-activity aria-label="Session events" aria-live="polite">
    <div class="kit-section">Session events</div>
    <div class="kit-stack">${lines.join("")}${tools}</div>
  </section>`;
}

function needsInput(planning) {
  const session = planning.session;
  const open = openQuestions(session);
  if (!open.length) return "";
  const forms = planning.agent?.forms;
  if (forms?.surfaces.size) {
    const content = [...forms.surfaces.keys()]
      .map((id) =>
        forms.render(id, { planning: true, disabled: Boolean(planning.busy) }),
      )
      .join("");
    return `<section class="kit-panel kit-card kit-pad plan-card" data-plan-needs-input aria-label="Needs Input"><div class="kit-section">Needs Input</div>${content}</section>`;
  }
  const busy = planning.busy === "answers";
  return `<section class="kit-panel kit-card kit-bar-warn kit-pad plan-card" data-plan-needs-input aria-label="Needs Input">
    <div class="kit-header-title"><span class="kit-section">Needs Input</span><span class="kit-badge kit-badge-warn">${open.length} open</span></div>
    <div class="kit-stack">${open
      .map(
        (question) =>
          `<div class="plan-question" data-plan-question="${esc(question.id)}">${questionField(question, planning.drafts[question.id])}</div>`,
      )
      .join("")}</div>
    <div class="plan-actions"><button type="button" class="kit-btn kit-btn-sm" data-plan-submit-answers${busy ? " disabled" : ""}>${busy ? "Submitting…" : "Submit answers"}</button></div>
  </section>`;
}

function planReview(session) {
  if (!session?.plan) return "";
  const sections = planSections(session.plan.text);
  return `<section class="kit-panel kit-pad plan-card" data-plan-review aria-label="Plan review">
    <div class="kit-header-title"><span class="kit-section">Plan</span><span class="code-id kit-muted" title="sha256">sha256 ${esc(session.plan.sha256)}</span></div>
    <div class="kit-stack">${sections
      .map(
        (section) =>
          `<div class="plan-section" data-plan-section>${section.title ? `<div class="plan-section-title">${esc(section.title)}</div>` : ""}<div class="plan-section-body">${esc(section.body)}</div></div>`,
      )
      .join("")}</div>
  </section>`;
}

function acceptBar(planning) {
  const session = planning.session;
  if (!session || session.binding) return "";
  const state = acceptState(session);
  const busy = planning.busy === "accept";
  const disabled =
    !state.ok || Boolean(planning.busy) || planning.agent?.recoveryRequired;
  return `<section class="kit-panel kit-pad plan-card plan-accept" data-plan-accept-bar>
    <button type="button" class="kit-btn kit-btn-ok" data-plan-accept${disabled ? " disabled" : ""} aria-disabled="${disabled}">${busy ? "Accepting…" : "Accept"}</button>
    <span class="kit-muted" data-plan-accept-note>${esc(state.ok ? "Accept binds this plan to the commit shown in the binding." : state.reason)}</span>
  </section>`;
}

function bindingPanel(session) {
  if (!session?.binding) return "";
  const rows = bindingRows(session.binding)
    .map(
      ([key, value]) =>
        `<dt>${esc(key)}</dt><dd class="code-id">${esc(value)}</dd>`,
    )
    .join("");
  return `<section class="kit-panel kit-card kit-bar-ok kit-pad plan-card" data-plan-binding aria-label="Accept binding">
    <div class="kit-header-title"><span class="kit-section">Binding</span><span class="kit-badge kit-badge-ok">Accepted</span></div>
    <dl class="kit-kv">${rows}</dl>
  </section>`;
}

/** The whole Work Intake body. */
export function planningMarkup(planning) {
  return `<div class="plan-flow" data-plan-flow>
    ${errorBanner(planning.error)}
    ${planning.agent?.recoveryRequired ? '<button type="button" class="kit-btn" data-plan-recover>Recover agent snapshot</button>' : ""}
    ${composer(planning)}
    ${activity(planning)}
    ${needsInput(planning)}
    ${planReview(planning.session)}
    ${acceptBar(planning)}
    ${bindingPanel(planning.session)}
  </div>`;
}

/**
 * Wires the flow under root. `store.planning` is the state; `rerender` redraws
 * after a change. Typing updates the state without a redraw so focus stays.
 */
export function bindPlanning(root, store, rerender, fetchImpl = fetch) {
  if (!root) return;
  const set = (patch) => {
    store.planning = { ...store.planning, ...patch };
    rerender();
  };
  const fail = (error) =>
    set({
      busy: "",
      error:
        error instanceof PlanningError
          ? error
          : new PlanningError(
              error?.status || 0,
              error?.code || "",
              error?.message || "The planning request failed.",
            ),
    });
  const agentRun = async (options) => {
    const id = store.planning.session.id;
    let agent = store.planning.agent;
    if (!agent || agent.threadId !== id) {
      agent = new PlanningAgent(id, fetchImpl);
      store.planning = { ...store.planning, agent };
    }
    if (!store.planning.humanDrafts)
      store.planning.humanDrafts = Object.fromEntries(
        Object.keys(store.planning.drafts).map((id) => [id, true]),
      );
    const questionId = (field) =>
      field.startsWith("question:") ? field.slice(9) : null;
    const binding = {
      fields: () => {
        const current = store.planning.session;
        const pending = openQuestions(current).some(
          (question) => question.required || question.validationError,
        );
        return new Map([
          ["repoUrl", { type: "string", readOnly: true }],
          ["ref", { type: "string", readOnly: true }],
          ...(current.needsInput || []).map((question) => [
            "question:" + question.id,
            {
              type: question.type === "boolean" ? "boolean" : "string",
              choices: question.options || [],
              readOnly: !pending,
            },
          ]),
        ]);
      },
      dirty: (field) =>
        Object.hasOwn(store.planning.humanDrafts, questionId(field)),
      set: (field, value) => {
        for (const surface of agent.forms?.surfaces.values() || []) {
          if (surface.components.has(field)) {
            const item = surface.components.get(field);
            agent.forms.input(
              surface.id,
              field,
              item.component === "CheckBox" ? value === "true" : value,
              false,
            );
          }
        }
        return set({
          drafts: { ...store.planning.drafts, [questionId(field)]: value },
        });
      },
      focus: (field) => {
        const id = questionId(field);
        const target = [...root.querySelectorAll("[data-plan-answer]")].find(
          (item) => item.getAttribute("data-plan-answer") === id,
        );
        target?.focus();
      },
      confirm: async (message) => window.confirm(message),
    };
    const result = await agent.run(options, binding, (run, event) => {
      if (event.type === "CUSTOM") {
        for (const surface of run.forms.surfaces.values()) {
          for (const question of run.state.session?.needsInput || []) {
            const field = "question:" + question.id;
            if (
              surface.components.has(field) &&
              Object.hasOwn(store.planning.drafts, question.id) &&
              (store.planning.humanDrafts[question.id] ||
                (!question.value && !answerMap(run.state.session)[question.id]))
            ) {
              const item = surface.components.get(field);
              run.forms.input(
                surface.id,
                field,
                item.component === "CheckBox"
                  ? store.planning.drafts[question.id] === "true"
                  : store.planning.drafts[question.id],
                Boolean(store.planning.humanDrafts[question.id]),
                false,
              );
            }
          }
        }
      }
      set({
        agentState: run.state,
        agentMessages: [...run.messages.values()],
        agentTools: [
          ...new Map(
            [...(run.state.toolCalls || []), ...run.tools.values()].map(
              (item) => [item.id, item],
            ),
          ).values(),
        ],
        ...(run.state.session ? { session: run.state.session } : {}),
      });
    });
    set({ session: result.state.session, busy: "" });
    return result;
  };

  root.addEventListener("input", (event) => {
    const target = event.target;
    const field = target?.getAttribute?.("data-plan-field");
    if (field) {
      store.planning = { ...store.planning, [field]: target.value };
      return;
    }
    const surface = target?.getAttribute?.("data-a2ui-surface");
    const component = target?.getAttribute?.("data-a2ui-component");
    if (surface && component && target.type !== "radio")
      store.planning.agent.forms.input(
        surface,
        component,
        target.type === "checkbox" ? target.checked : target.value,
      );
    const answer = target?.getAttribute?.("data-plan-answer");
    if (answer) {
      store.planning = {
        ...store.planning,
        drafts: {
          ...store.planning.drafts,
          [answer]:
            target.type === "checkbox" ? String(target.checked) : target.value,
        },
        humanDrafts: { ...store.planning.humanDrafts, [answer]: true },
      };
    }
  });
  root.addEventListener("change", (event) => {
    const target = event.target;
    const surface = target?.getAttribute?.("data-a2ui-surface");
    const component = target?.getAttribute?.("data-a2ui-component");
    if (surface && component)
      store.planning.agent.forms.input(
        surface,
        component,
        target.type === "checkbox" ? target.checked : target.value,
      );
    const answer = event.target?.getAttribute?.("data-plan-answer");
    if (answer) {
      store.planning = {
        ...store.planning,
        drafts: {
          ...store.planning.drafts,
          [answer]:
            target.type === "checkbox" ? String(target.checked) : target.value,
        },
        humanDrafts: { ...store.planning.humanDrafts, [answer]: true },
      };
    }
  });

  root.addEventListener("click", async (event) => {
    const hit = (selector) =>
      event.target instanceof Element ? event.target.closest(selector) : null;
    const planning = store.planning;
    if (planning.busy) return;

    if (hit("[data-plan-send]")) {
      const message = planning.message.trim();
      if (
        !planning.session &&
        (!planning.repoUrl.trim() || !planning.ref.trim())
      ) {
        set({
          error: new PlanningError(
            0,
            "INVALID_INTAKE",
            "Enter a repository URL and a ref.",
          ),
        });
        return;
      }
      set({ busy: "turn", error: null, events: [] });
      try {
        let session = store.planning.session;
        if (!session) {
          session = await createSession(
            planning.repoUrl.trim(),
            planning.ref.trim(),
            fetchImpl,
          );
          store.planning = { ...store.planning, session };
        }
        await agentRun({ message });
        set({ message: "" });
      } catch (error) {
        fail(error);
      }
      return;
    }

    if (hit("[data-plan-submit-answers]")) {
      const recorded = answerMap(planning.session);
      const answers = (planning.session.needsInput || [])
        .map((q) => ({
          id: q.id,
          value: String(
            planning.drafts[q.id] ?? recorded[q.id] ?? q.value ?? "",
          ),
        }))
        .filter((a) => a.value.trim());
      if (!answers.length && !planning.agent?.forms?.surfaces.size) {
        set({
          error: new PlanningError(
            0,
            "INVALID_ANSWER",
            "Enter an answer first.",
          ),
        });
        return;
      }
      set({ busy: "answers", error: null });
      try {
        let formAction;
        const forms = planning.agent?.forms;
        if (forms?.surfaces.size) {
          const surfaceID = [...forms.surfaces.keys()].at(-1);
          const result = forms.action(surfaceID, "submit");
          if (!result.message) {
            set({ busy: "", error: null });
            const first = Object.keys(result.errors)[0];
            const element = [
              ...root.querySelectorAll("[data-a2ui-component]"),
            ].find(
              (item) => item.getAttribute("data-a2ui-component") === first,
            );
            (element?.querySelector?.("input") || element)?.focus();
            return;
          }
          formAction = result.message;
        }
        await agentRun(formAction ? { formAction } : { answers });
        set({ busy: "" });
      } catch (error) {
        fail(error);
      }
      return;
    }

    if (hit("[data-plan-recover]")) {
      set({ busy: "recover", error: null });
      try {
        await agentRun({ recover: true });
      } catch (error) {
        fail(error);
      }
      return;
    }

    if (hit("[data-plan-accept]")) {
      if (!acceptState(planning.session).ok || planning.agent?.recoveryRequired)
        return;
      set({ busy: "accept", error: null });
      try {
        const session = await acceptSession(planning.session.id, fetchImpl);
        set({ session, busy: "" });
      } catch (error) {
        // A 409 means the session moved; read it again so the screen is true.
        try {
          const session = await getSession(planning.session.id, fetchImpl);
          store.planning = { ...store.planning, session };
        } catch {
          // Keep the session already on screen.
        }
        fail(error);
      }
    }
  });
}
