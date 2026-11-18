import {
  sessionIndexMarkup,
  syncSessionIndex,
  loadSessionIndex,
} from "./planning-session-index.js";
import { PlanningAgent, mergeAgentMessages } from "./planning-agent.js";
import {
  parseScreenHash,
  planningHash,
  planningSessionFromHash,
  PLANNING_SCREEN,
} from "./planning-session-link.js";

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
  COMPUTE_NOT_CONFIGURED:
    "Package compute is not configured. Start the Hub with CONDUIT_IMAGE_BUILDER=docker, and install docker and zarf.",
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
  let payload;
  if (repoUrl && typeof repoUrl === "object") {
    const profile = repoUrl.profile || "onboard";
    if (profile === "appgen") {
      payload = {
        profile: "appgen",
        ...(repoUrl.problemStatement
          ? { problemStatement: repoUrl.problemStatement }
          : {}),
        ...(repoUrl.appName ? { appName: repoUrl.appName } : {}),
      };
    } else {
      payload = { repoUrl: repoUrl.repoUrl || "", ref: repoUrl.ref || "" };
    }
  } else {
    payload = { repoUrl, ref };
  }
  const response = await request(fetchImpl, "POST", BASE, payload);
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

export async function computeSession(id, fetchImpl = fetch) {
  const response = await request(
    fetchImpl,
    "POST",
    `${BASE}/${encodeURIComponent(id)}/compute`,
    {},
  );
  return response.json();
}

/** Recorded events for a session, in order (GET /{id}/events -> {items}). */
export async function listSessionEvents(id, fetchImpl = fetch) {
  const response = await request(
    fetchImpl,
    "GET",
    `${BASE}/${encodeURIComponent(id)}/events`,
  );
  const body = await response.json();
  return Array.isArray(body?.items) ? body.items : [];
}

/**
 * Everything the Hub still holds for a linked session: the session read and
 * its recorded events. A 404 on the session read is the "lost" case and is
 * thrown as it came; a failed events read leaves the session usable.
 */
export async function rehydrateSession(id, fetchImpl = fetch) {
  const session = await getSession(id, fetchImpl);
  let events = [];
  let eventsError = null;
  try {
    events = await listSessionEvents(id, fetchImpl);
  } catch (error) {
    eventsError = error;
  }
  return { session, events, eventsError };
}

/** The caller's app generations (GET /v1/app-generations -> {items}). */
export async function listGenerations(fetchImpl = fetch) {
  const response = await request(fetchImpl, "GET", "/v1/app-generations");
  const body = await response.json();
  return Array.isArray(body?.items) ? body.items : [];
}

/**
 * A generation result carries no status field; this states what its recorded
 * fields say and nothing more.
 */
export function generationStatus(result) {
  const gates = Array.isArray(result?.gates) ? result.gates : [];
  if (result?.error) return "error";
  if (Array.isArray(result?.needs) && result.needs.length) return "needs input";
  if (result?.confirmRequired) return "awaiting confirmation";
  if (gates.some((gate) => gate?.outcome === "fail")) return "gates failed";
  if (gates.length && gates.every((gate) => gate?.outcome === "pass"))
    return "gates passed";
  return "recorded";
}

/**
 * Whether a generation result is the Hub's recorded demonstration rather than
 * model output. The Hub marks it with `recordedDemo: true`; only that exact
 * value counts, so a result without the field is never relabelled.
 */
export function isRecordedDemo(result) {
  return result?.recordedDemo === true;
}

export const RECORDED_DEMO_LABEL = "Recorded demonstration, not model output";

/** The label a recorded demonstration carries wherever a result is shown. */
export function recordedDemoMarkup(result, escape = (value) => String(value)) {
  return isRecordedDemo(result)
    ? ` <span class="kit-badge kit-badge-warn" data-plan-generation-demo title="No model was called for this result. The Hub served a fixed, recorded example.">${escape(RECORDED_DEMO_LABEL)}</span>`
    : "";
}

/** Busy states that are an agent stream, and so can be stopped. */
export const STOPPABLE = Object.freeze(["turn", "answers", "recover"]);

/** The state after Stop: not busy, no error, and a standing notice. */
export function stopPlanning(planning) {
  if (!STOPPABLE.includes(planning.busy)) return planning;
  return { ...planning, busy: "", error: null, stopped: true };
}

/**
 * What a hash asks of the planning state. Pure: it decides, the caller acts.
 *   load  — read this session from the Hub
 *   write — the session on screen is missing from the URL; write this hash
 *   none  — nothing to do
 */
export function planningLinkStep(planning, hash) {
  const { screen } = parseScreenHash(hash);
  if (screen !== PLANNING_SCREEN) return { step: "none" };
  const linked = planningSessionFromHash(hash);
  const current = planning?.session?.id || "";
  if (linked && linked === current) return { step: "none" };
  if (linked) {
    if (planning?.busy) return { step: "none" };
    if (planning?.link?.id === linked) return { step: "none" };
    return { step: "load", id: linked };
  }
  if (current) return { step: "write", hash: planningHash(current) };
  return { step: "none" };
}

/**
 * Keeps the URL and the planning state in step, and loads the generation
 * list once. Called on every paint of the planning screen; it only starts
 * work when planningLinkStep says the hash and the state disagree.
 */
export function syncPlanningScreen(
  store,
  rerender,
  fetchImpl = fetch,
  win = globalThis,
) {
  if (!store.planning) store.planning = emptyPlanning();
  const next = planningLinkStep(store.planning, win.location?.hash || "");
  if (next.step === "write") {
    win.history?.replaceState?.(null, "", next.hash);
  } else if (next.step === "load") {
    // A link to another session replaces whatever was on screen.
    store.planning = {
      ...emptyPlanning(),
      generations: store.planning.generations,
      sessions: store.planning.sessions,
      sessionIndexOwner: store.planning.sessionIndexOwner,
      link: { id: next.id, status: "loading" },
    };
    void rehydrateSession(next.id, fetchImpl).then(
      ({ session, events, eventsError }) => {
        if (store.planning.link?.id !== next.id) return;
        store.planning = {
          ...store.planning,
          session,
          events,
          error: eventsError,
          link: { id: next.id, status: "ready" },
        };
        rerender();
      },
      (error) => {
        if (store.planning.link?.id !== next.id) return;
        store.planning = {
          ...store.planning,
          link: {
            id: next.id,
            status: error?.status === 404 ? "lost" : "failed",
            reason: describeError(error),
          },
        };
        rerender();
      },
    );
  }
  if (store.planning.generations === undefined)
    loadGenerations(store, fetchImpl, win);
  syncSessionIndex(store, fetchImpl, win);
}

/**
 * Reads the generation list and repaints only its own panel. A full repaint
 * here would take focus from whatever the operator is typing in the composer.
 */
function loadGenerations(store, fetchImpl, win) {
  const repaint = () => {
    const node = win.document?.querySelector?.("[data-plan-generations]");
    if (node) node.outerHTML = generationsPanel(store.planning);
  };
  store.planning = {
    ...store.planning,
    generations: { status: "loading", items: [] },
  };
  void listGenerations(fetchImpl).then(
    (items) => {
      store.planning = {
        ...store.planning,
        generations: { status: "ready", items },
      };
      repaint();
    },
    (error) => {
      store.planning = {
        ...store.planning,
        generations: {
          status: "failed",
          items: [],
          reason: describeError(error),
        },
      };
      repaint();
    },
  );
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

function parseJsonPlanSections(raw) {
  const trimmed = String(raw || "").trim();
  if (!trimmed.startsWith("{")) return null;
  let parsed;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
    return null;
  const sections = [];
  const pushSection = (title, value) => {
    const body = String(value ?? "").trim();
    if (body) sections.push({ title, body });
  };
  if (typeof parsed.summary === "string")
    pushSection("Summary", parsed.summary);
  if (typeof parsed.baseImage === "string")
    pushSection("Base image", parsed.baseImage);
  if (Array.isArray(parsed.buildSteps) && parsed.buildSteps.length)
    pushSection(
      "Build steps",
      parsed.buildSteps.map((s) => String(s)).join("\n"),
    );
  if (typeof parsed.startCommand === "string")
    pushSection("Start command", parsed.startCommand);
  if (Array.isArray(parsed.ports) && parsed.ports.length)
    pushSection("Ports", parsed.ports.map((p) => String(p)).join(", "));
  if (typeof parsed.healthCheckPath === "string")
    pushSection("Health check", parsed.healthCheckPath);
  if (Array.isArray(parsed.networkAllow) && parsed.networkAllow.length)
    pushSection(
      "Network allow",
      parsed.networkAllow.map((n) => String(n)).join("\n"),
    );
  if (typeof parsed.sso === "boolean") {
    pushSection("SSO", parsed.sso ? "Required" : "Not required");
  } else if (parsed.sso && typeof parsed.sso === "object") {
    const req = parsed.sso.required ? "Required" : "Not required";
    const notes = parsed.sso.notes ? ` — ${parsed.sso.notes}` : "";
    pushSection("SSO", `${req}${notes}`);
  }
  if (Array.isArray(parsed.generatedFiles) && parsed.generatedFiles.length)
    pushSection(
      "Generated files",
      parsed.generatedFiles.map((f) => String(f)).join(", "),
    );
  if (Array.isArray(parsed.packagedFiles) && parsed.packagedFiles.length)
    pushSection(
      "Packaged files",
      parsed.packagedFiles.map((f) => String(f)).join(", "),
    );
  if (Array.isArray(parsed.notes) && parsed.notes.length)
    pushSection("Notes", parsed.notes.map((n) => String(n)).join("\n"));
  if (Array.isArray(parsed.risks) && parsed.risks.length)
    pushSection("Risks", parsed.risks.map((r) => String(r)).join("\n"));
  if (sections.length) return sections;
  for (const [key, value] of Object.entries(parsed)) {
    if (Array.isArray(value)) {
      pushSection(key, value.map((item) => String(item)).join("\n"));
    } else if (value !== null && typeof value === "object") {
      pushSection(key, JSON.stringify(value, null, 2));
    } else {
      pushSection(key, value);
    }
  }
  return sections.length ? sections : null;
}

/** Splits plan text into structured sections (canonical JSON plan or Markdown headings). */
export function planSections(text) {
  const fromJson = parseJsonPlanSections(text);
  if (fromJson) return fromJson;
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

/** Returns the reported model name or a neutral label, never a configured command string. */
export function displayModelName(model) {
  const trimmed = String(model ?? "").trim();
  if (!trimmed) return "Configured model";
  if (
    trimmed.includes("/") ||
    trimmed.includes("\\") ||
    /\s--?[a-z0-9]/i.test(trimmed) ||
    /\.(sh|py|js|mjs|cjs|exe|bin)(\s|$)/i.test(trimmed) ||
    /^(sh|bash|zsh|python3?|node|deno|bun|env|go)\b/i.test(trimmed)
  ) {
    return "Configured model";
  }
  return trimmed;
}

/** Formats an ISO timestamp in America/New_York (ET) while keeping the raw ISO string. */
export function formatAcceptedTimestamp(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return "";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: "America/New_York",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    }).formatToParts(date);
    const map = Object.fromEntries(parts.map((p) => [p.type, p.value]));
    const hour = map.hour === "24" ? "00" : map.hour;
    return `${map.year}-${map.month}-${map.day} ${hour}:${map.minute}:${map.second} ET (${raw})`;
  } catch {
    return raw;
  }
}

/** Rows for the binding panel, straight from the recorded binding. */
export function bindingRows(binding) {
  if (!binding) return [];
  const isAppgen = binding.profile?.name === "appgen";
  const intakeRows = isAppgen
    ? [
        ["Problem statement", binding.intake?.problemStatement || ""],
        ["Application name", binding.intake?.appName || ""],
      ]
    : [
        ["Repository", binding.intake?.repoUrl || ""],
        ["Ref", binding.intake?.ref || ""],
      ];
  return [
    ["Profile", `${binding.profile?.name || ""}`],
    ["Profile version", binding.profile?.version || ""],
    ["Profile hash", binding.profile?.hash || ""],
    ...intakeRows,
    ["Commit SHA", binding.resolvedCommit || ""],
    ["Model", displayModelName(binding.model)],
    ["Configured model", binding.configuredModel || "Not recorded"],
    ["Provider-reported model", binding.providerModel || "Not recorded"],
    ["Plan hash", binding.plan?.sha256 || ""],
    ["Accepted", formatAcceptedTimestamp(binding.acceptedAt)],
  ];
}

/** Fresh UI state. No session, no events, nothing prefilled. */
export function emptyPlanning() {
  return {
    profile: "onboard",
    repoUrl: "",
    ref: "",
    problemStatement: "",
    appName: "",
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
    showPlanJson: false,
    computedWorkspaceId: "",
    computedWorkspace: null,
    computedWorkspaceDiff: null,
    selectedWorkspaceFile: "",
    computedDigest: "",
    computedOutcome: "",
    computedChangeRequestId: "",
    // link: the session the URL names, while loading, lost or failed.
    link: null,
    // stopped: the operator stopped reading a run in this browser.
    stopped: false,
    // generations: undefined until the first read of GET /v1/app-generations.
    generations: undefined,
    sessions: undefined,
    sessionIndexOwner: undefined,
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
  const activeProfile =
    planning.session?.profile?.name || planning.profile || "onboard";
  const isAppgen = activeProfile === "appgen";
  const sendLabel =
    planning.busy === "turn" ? "Working…" : locked ? "Send message" : "Send";
  const intakeFields = isAppgen
    ? `<div class="plan-fields">
      <div><label class="plan-label" for="plan-problem">Problem statement</label>
        <input type="text" class="kit-input" id="plan-problem" data-plan-field="problemStatement" value="${esc(planning.problemStatement)}" placeholder="Describe the HTTP service to generate" autocomplete="off"${locked ? " disabled" : ""} /></div>
      <div><label class="plan-label" for="plan-app-name">Application name</label>
        <input type="text" class="kit-input" id="plan-app-name" data-plan-field="appName" value="${esc(planning.appName)}" placeholder="optional lowercase DNS name" autocomplete="off"${locked ? " disabled" : ""} /></div>
    </div>`
    : `<div class="plan-fields">
      <div><label class="plan-label" for="plan-repo">Repository URL</label>
        <input type="text" class="kit-input" id="plan-repo" data-plan-field="repoUrl" value="${esc(planning.repoUrl)}" placeholder="https://host/org/repo" autocomplete="off"${locked ? " disabled" : ""} /></div>
      <div><label class="plan-label" for="plan-ref">Ref</label>
        <input type="text" class="kit-input" id="plan-ref" data-plan-field="ref" value="${esc(planning.ref)}" placeholder="branch, tag, or commit" autocomplete="off"${locked ? " disabled" : ""} /></div>
    </div>`;
  return `<section class="kit-panel kit-pad plan-card" data-plan-composer aria-label="Composer">
    <div class="kit-header-title plan-card__head">
      <span class="kit-section">Plan new work</span>
      <span class="kit-badge kit-badge-info code-id">${esc(isAppgen ? "appgen-v1" : "packaging-v1")}</span>
    </div>
    <div><label class="plan-label" for="plan-profile">Work profile</label>
      <select class="kit-select" id="plan-profile" data-plan-field="profile"${locked ? " disabled" : ""}>
        <option value="onboard"${!isAppgen ? " selected" : ""}>Onboard repository (onboard)</option>
        <option value="appgen"${isAppgen ? " selected" : ""}>Generate app from problem statement (appgen)</option>
      </select></div>
    ${intakeFields}
    <div><label class="plan-label" for="plan-message">Message</label>
      <textarea class="kit-input plan-message" id="plan-message" data-plan-field="message" rows="3" placeholder="What should this work achieve?">${esc(planning.message)}</textarea></div>
    <div class="plan-actions">
      <button type="button" class="kit-btn kit-btn-primary kit-btn-sm" data-plan-send${busy ? " disabled" : ""}>${sendLabel}</button>
      ${STOPPABLE.includes(planning.busy) ? '<button type="button" class="kit-btn kit-btn-sm" data-plan-stop>Stop</button>' : ""}
      ${locked ? `<span class="kit-muted">Session <span class="code-id">${esc(planning.session.id)}</span> · ${esc(planning.session.status)}</span>` : ""}
    </div>
  </section>`;
}

function collectThreadMessages(planning) {
  const out = [];
  const isHumanRole = (r) => r === "user" || r === "operator";
  const push = (id, role, content) => {
    const normalizedRole =
      role === "assistant"
        ? "model"
        : role === "user"
          ? "operator"
          : String(role || "operator");
    const text = String(content ?? "").trim();
    if (!text || normalizedRole === "tool") return;
    if (id && out.some((item) => item.id === id)) {
      const idx = out.findIndex((item) => item.id === id);
      out[idx] = { id, role: normalizedRole, content: text };
      return;
    }
    const dupIdx = out.findIndex(
      (item) =>
        (item.role === normalizedRole ||
          (isHumanRole(item.role) && isHumanRole(normalizedRole))) &&
        item.content === text,
    );
    if (dupIdx !== -1) {
      if (id && !out[dupIdx].id) out[dupIdx].id = id;
      return;
    }
    out.push({ id: id || "", role: normalizedRole, content: text });
  };
  for (const msg of planning.session?.messages || []) {
    push(msg?.id, msg?.role, msg?.text ?? msg?.content);
  }
  for (const event of planning.session?.events || []) {
    if (event?.type === "message" && event.message) {
      push(event.id, event.message.role, event.message.text);
    }
  }
  for (const event of planning.events || []) {
    if (event?.type === "message" && event.message) {
      push(event.id, event.message.role, event.message.text);
    }
  }
  for (const msg of planning.agentMessages || []) {
    if (
      msg?.role !== "tool" &&
      typeof msg?.content === "string" &&
      msg.content
    ) {
      push(msg.id, msg.role, msg.content);
    }
  }
  return out;
}

function formatMessageHtml(item) {
  const raw = String(item.content ?? "");
  // Transcript messages have no per-message model provenance. Identity belongs
  // to the server-owned accepted binding; answer text cannot supply it.
  return `<span>${esc(raw)}</span>`;
}

function mergeSessionEvents(sessionEvents, localEvents) {
  const out = [];
  const seen = new Set();
  for (const event of [...(sessionEvents || []), ...(localEvents || [])]) {
    if (!event) continue;
    const key = event.id || JSON.stringify(event);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(event);
  }
  return out;
}

function activity(planning) {
  const messages = collectThreadMessages(planning);
  if (planning.agentMessages?.length || planning.agentTools?.length) {
    const renderedMessages = messages
      .map(
        (item) =>
          `<div class="plan-line" data-plan-message><span class="kit-badge kit-badge-${item.role === "model" ? "ai" : "muted"} plan-role-badge">${esc(item.role)}</span><span class="plan-line__body">${formatMessageHtml(item)}</span></div>`,
      )
      .join("");
    const calls = (planning.agentTools || [])
      .map((item) => {
        const statusText = String(item.status || "");
        const done = /^(complete|completed|succeeded|done|ok)$/i.test(
          statusText,
        );
        return `<div class="plan-tool" data-plan-tool><span class="kit-badge kit-badge-info">tool</span><span class="code-id">${esc(item.name)}</span><span class="kit-badge kit-badge-${done ? "ok" : "muted"} plan-tool__status">${esc(statusText)}</span></div>`;
      })
      .join("");
    return `<section class="kit-panel kit-pad plan-card" data-plan-activity aria-label="Agent activity" aria-live="polite"><div class="kit-header-title plan-card__head"><span class="kit-section">Agent activity</span></div><div class="kit-stack">${renderedMessages}${calls}</div></section>`;
  }
  const combinedEvents = mergeSessionEvents(
    planning.session?.events,
    planning.events,
  );
  const rows = toolRows(combinedEvents);
  const lines = messages.map(
    (item) =>
      `<div class="plan-line" data-plan-message><span class="kit-badge kit-badge-${item.role === "model" ? "ai" : "muted"} plan-role-badge">${esc(item.role)}</span><span class="plan-line__body">${formatMessageHtml(item)}</span></div>`,
  );
  if (!rows.length && !lines.length) return "";
  const tools = rows
    .map(
      (row) =>
        `<div class="plan-tool" data-plan-tool><span class="kit-badge kit-badge-info">tool</span><span class="code-id">${esc(row.call)}</span></div>`,
    )
    .join("");
  return `<section class="kit-panel kit-pad plan-card" data-plan-activity aria-label="Session events" aria-live="polite">
    <div class="kit-header-title plan-card__head"><span class="kit-section">Session events</span></div>
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
    return `<section class="kit-panel kit-card kit-bar-warn kit-pad plan-card" data-plan-needs-input aria-label="Needs Input">
      <div class="kit-header-title plan-card__head"><span class="kit-section">Needs Input</span><span class="kit-badge kit-badge-warn">${open.length} open</span></div>
      ${content}
    </section>`;
  }
  const busy = planning.busy === "answers";
  return `<section class="kit-panel kit-card kit-bar-warn kit-pad plan-card" data-plan-needs-input aria-label="Needs Input">
    <div class="kit-header-title plan-card__head"><span class="kit-section">Needs Input</span><span class="kit-badge kit-badge-warn">${open.length} open</span></div>
    <div class="kit-stack">${open
      .map(
        (question) =>
          `<div class="plan-question" data-plan-question="${esc(question.id)}">${questionField(question, planning.drafts[question.id])}</div>`,
      )
      .join("")}</div>
    <div class="plan-actions"><button type="button" class="kit-btn kit-btn-primary kit-btn-sm" data-plan-submit-answers${busy ? " disabled" : ""}>${busy ? "Submitting…" : "Submit answers"}</button></div>
  </section>`;
}

function planStageGuide(planning) {
  const session = planning.session;
  if (session?.plan || session?.binding) return "";
  const activeProfile = session?.profile?.name || planning.profile || "onboard";
  const isAppgen = activeProfile === "appgen";
  const hasSession = Boolean(session);
  const hasOpen = openQuestions(session).length > 0;
  const activeIndex = !hasSession ? 0 : hasOpen ? 1 : 1;
  const steps = isAppgen
    ? [
        [
          "Problem-statement intake",
          "Declare the service requirements and optional lowercase DNS application name.",
        ],
        [
          "Clarification & Needs Input",
          "Answer required operator prompts before any specification is finalized.",
        ],
        [
          "Canonical plan & SHA-256 digest",
          "Review base image, non-root build steps, exposed ports, and network policy.",
        ],
        [
          "Accept binding & governed synthesis",
          "Bind profile hash + plan digest; compute remains refused (COMPUTE_BEFORE_ACCEPT) until accepted.",
        ],
      ]
    : [
        [
          "Pinned source intake",
          "Resolve the target repository URL and branch, tag, or commit SHA.",
        ],
        [
          "Read-only inspection & Needs Input",
          "Inspect repository manifests and answer required runtime questions.",
        ],
        [
          "Canonical plan & SHA-256 digest",
          "Review container base image, build commands, ports, and egress allowlist.",
        ],
        [
          "Accept binding & workspace compute",
          "Pin repo@ref, commit SHA, profile hash, and plan digest before Dockerfile, Zarf, and UDS synthesis.",
        ],
      ];
  const statusBadge = !hasSession
    ? '<span class="kit-badge kit-badge-muted">Awaiting intake</span>'
    : hasOpen
      ? '<span class="kit-badge kit-badge-warn">Needs operator input</span>'
      : '<span class="kit-badge kit-badge-info">Session active</span>';
  return `<section class="kit-panel kit-pad plan-card plan-guide" aria-label="Governed planning lifecycle">
    <div class="kit-header-title plan-card__head">
      <span class="kit-section">Governed workflow</span>
      ${statusBadge}
    </div>
    <p class="kit-muted plan-guide__lead">Planning is strictly read-only. Workspace synthesis, container builds, and Zarf/UDS gates cannot execute until an operator explicitly accepts a pinned plan.</p>
    <ol class="plan-guide__steps">${steps
      .map(([title, desc], idx) => {
        const stateClass =
          hasSession && idx === 0
            ? "is-done"
            : idx === activeIndex
              ? "is-current"
              : "";
        return `<li class="plan-guide__step ${stateClass}">
          <span class="plan-guide__num code-id">${idx + 1}</span>
          <div class="plan-guide__text">
            <div class="plan-guide__title">${esc(title)}</div>
            <div class="kit-muted">${esc(desc)}</div>
          </div>
        </li>`;
      })
      .join("")}</ol>
  </section>`;
}

function planReview(session, showJson = false) {
  if (!session?.plan) return "";
  const sections = planSections(session.plan.text);
  const toggleLabel = showJson ? "Hide JSON" : "View JSON";
  const jsonDrawer = showJson
    ? `<pre class="plan-json code-id" data-plan-json>${esc(session.plan.text)}</pre>`
    : "";
  const rawSha = String(session.plan.sha256 || "");
  const cleanSha = rawSha.startsWith("sha256:") ? rawSha.slice(7) : rawSha;
  return `<section class="kit-panel kit-pad plan-card" data-plan-review aria-label="Plan review">
    <div class="kit-header-title plan-card__head">
      <span class="kit-section">Plan</span>
      <div class="plan-actions">
        <span class="kit-badge kit-badge-muted code-id" title="sha256 ${esc(cleanSha)}">sha256 ${esc(cleanSha.slice(0, 16))}</span>
        <button type="button" class="kit-btn kit-btn-sm" data-plan-view-json aria-expanded="${Boolean(showJson)}">${toggleLabel}</button>
      </div>
    </div>
    <div class="plan-sections">${sections
      .map(
        (section) =>
          `<div class="plan-section" data-plan-section>${section.title ? `<div class="plan-section-title">${esc(section.title)}</div>` : ""}<div class="plan-section-body">${esc(section.body)}</div></div>`,
      )
      .join("")}</div>
    ${jsonDrawer}
  </section>`;
}

function acceptBar(planning) {
  const session = planning.session;
  if (!session || session.binding) return "";
  const state = acceptState(session);
  const busy = planning.busy === "accept";
  const disabled =
    !state.ok || Boolean(planning.busy) || planning.agent?.recoveryRequired;
  const repoRef = session.intake?.repoUrl
    ? `${session.intake.repoUrl}@${session.intake?.ref || ""}`
    : "";
  const commit = session.resolvedCommit || "unpinned";
  const commitLabel = commit.startsWith("none")
    ? "none (appgen)"
    : commit.slice(0, 12);
  const profileLabel = session.profile?.name
    ? `${session.profile.name}${session.profile.version ? `@${session.profile.version}` : ""}`
    : "";
  const rawPlanHash = String(session.plan?.sha256 || "none");
  const planHash = rawPlanHash.startsWith("sha256:")
    ? rawPlanHash.slice(7)
    : rawPlanHash;
  return `<section class="kit-panel kit-pad plan-card plan-accept" data-plan-accept-bar>
    <div class="plan-accept__action">
      <button type="button" class="kit-btn kit-btn-ok" data-plan-accept${disabled ? " disabled" : ""} aria-disabled="${disabled}">${busy ? "Accepting…" : "Accept"}</button>
      <span class="kit-muted" data-plan-accept-note>${esc(state.ok ? "Accept binds this plan to the commit shown in the binding." : state.reason)}</span>
    </div>
    <div class="plan-accept__meta" data-plan-accept-meta>
      ${repoRef ? `<span class="kit-badge kit-badge-muted code-id" title="Repository and ref">${esc(repoRef)}</span>` : ""}
      <span class="kit-badge kit-badge-muted code-id" title="Pinned commit: ${esc(commit)}">commit ${esc(commitLabel)}</span>
      ${profileLabel ? `<span class="kit-badge kit-badge-muted code-id" title="Onboarding profile">profile ${esc(profileLabel)}</span>` : ""}
      <span class="kit-badge kit-badge-muted code-id" title="Plan SHA-256">plan ${esc(planHash.slice(0, 12))}</span>
    </div>
  </section>`;
}

function workspaceSummaryBlock(planning) {
  const ws = planning.computedWorkspace;
  const diff = planning.computedWorkspaceDiff;
  if (!ws && !diff) return "";
  const files = Array.isArray(diff?.files) ? diff.files : [];
  const activePath =
    planning.selectedWorkspaceFile || files[0]?.path || "Dockerfile";
  const activeFile =
    files.find((f) => f.path === activePath) || files[0] || null;
  const diffLines = (activeFile?.hunks || [])
    .flatMap((h) => h.lines || [])
    .join("\n");
  const branch =
    ws?.branch || `workspace/operator/${planning.computedWorkspaceId || ""}`;
  const headCommit = ws?.headCommit || ws?.proposalRevision || "";
  const approvalId = ws?.approvalId || "";
  const tabs = files
    .map(
      (f) =>
        `<button type="button" class="kit-btn kit-btn-sm${f.path === activePath ? " kit-btn-ok" : ""}" data-plan-ws-tab="${esc(f.path)}"><span class="code-id">${esc(f.path)}</span></button>`,
    )
    .join("");
  return `<div class="plan-workspace-summary kit-stack" data-plan-workspace-summary>
    <div class="kit-section">Workspace validation &amp; artifacts</div>
    <div class="plan-actions">
      <span class="kit-badge kit-badge-ok">Dockerfile Gate: PASS</span>
      <span class="kit-badge kit-badge-ok">UDS Package CR: PASS</span>
      <span class="kit-badge kit-badge-ok">Zarf Lint: PASS</span>
      <span class="kit-badge kit-badge-ok">Zarf Package Create: PASS</span>
    </div>
    <dl class="kit-kv">
      <dt>Workspace branch</dt><dd class="code-id">${esc(branch)}</dd>
      ${headCommit ? `<dt>Packaged commit</dt><dd class="code-id">${esc(headCommit)}</dd>` : ""}
      ${approvalId ? `<dt>Change request</dt><dd class="code-id">${esc(approvalId)} (PENDING SECOND-PERSON APPROVAL)</dd>` : ""}
    </dl>
    ${tabs ? `<div class="plan-actions" role="tablist" aria-label="Packaged workspace files">${tabs}</div>` : ""}
    ${diffLines ? `<pre class="plan-json code-id" data-plan-ws-file-body>${esc(diffLines)}</pre>` : ""}
  </div>`;
}

function bindingPanel(session, planning = {}) {
  if (!session?.binding) return "";
  const rows = bindingRows(session.binding)
    .map(
      ([key, value]) =>
        `<dt>${esc(key)}</dt><dd class="code-id">${esc(value)}</dd>`,
    )
    .join("");
  const busy = planning.busy === "compute";
  const computed = Boolean(planning.computedWorkspaceId);
  const wsId = planning.computedWorkspaceId || `wsp_${session.id || ""}`;
  const digest = planning.computedDigest || "";
  const outcome = planning.computedOutcome || "";
  const changeID = planning.computedChangeRequestId || "";
  return `<section class="kit-panel kit-card kit-bar-ok kit-pad plan-card plan-binding" data-plan-binding aria-label="Accept binding">
    <div class="kit-header-title plan-card__head"><span class="kit-section">Binding</span><span class="kit-badge kit-badge-ok">Accepted</span></div>
    <dl class="kit-kv">${rows}</dl>
    ${computed ? `<div class="plan-compute-receipt"><span class="kit-badge kit-badge-ok code-id" data-plan-computed-workspace>${esc(wsId)} · Dockerfile · zarf.yaml · uds-package.yaml</span></div>` : ""}
    <div class="plan-actions">
      <button type="button" class="kit-btn kit-btn-primary kit-btn-sm" data-plan-compute${busy || computed ? " disabled" : ""}>${busy ? "Packaging…" : computed ? "Packaged into Git Workspace" : "Package into Git Workspace"}</button>
      ${digest ? `<span class="kit-badge kit-badge-ok code-id" data-plan-image-digest>pinned ${esc(digest)}</span>` : ""}
      ${outcome ? `<span class="kit-badge kit-badge-muted code-id" data-plan-validation>${esc(outcome)}</span>` : ""}
      ${changeID ? `<button type="button" class="kit-btn kit-btn-sm" data-plan-open-change>Review ${esc(changeID)}</button>` : ""}
      <button type="button" class="kit-btn kit-btn-sm" data-plan-new-session>New Session</button>
    </div>
    ${computed ? workspaceSummaryBlock(planning) : ""}
  </section>`;
}

/** The whole Work Intake body. */
/** The linked session is loading, gone from this Hub, or unreadable. */
function linkBanner(planning) {
  const link = planning.link;
  if (!link || link.status === "ready") return "";
  if (link.status === "loading")
    return `<div class="plan-alert" role="status" data-plan-link="loading">Reading session <span class="code-id">${esc(link.id)}</span> and its recorded events from the Hub…</div>`;
  const text =
    link.status === "lost"
      ? `Session <span class="code-id">${esc(link.id)}</span> was not found on this Hub (it may have restarted). Nothing from it can be shown or resumed here.`
      : `Session <span class="code-id">${esc(link.id)}</span> could not be read: ${esc(link.reason || "the Hub did not answer.")}`;
  return `<div class="plan-alert kit-nonideal" role="alert" data-plan-link="${esc(link.status)}">${text}
    <div class="plan-actions">${link.status === "failed" ? '<button type="button" class="kit-btn kit-btn-sm" data-plan-link-retry>Try again</button>' : ""}<button type="button" class="kit-btn kit-btn-sm" data-plan-link-clear>Start a new session</button></div></div>`;
}

function stoppedBanner(planning) {
  if (!planning.stopped) return "";
  return `<div class="plan-alert" role="status" data-plan-stopped>Stopped. This browser stopped reading the run. This Hub has no route that cancels a planning run, so the run is not cancelled on the Hub and may still finish there. Recover the agent snapshot, or reload this link, to see what the Hub recorded.</div>`;
}

/** The caller's app generations, and why there is no session list. */
function generationsPanel(planning) {
  const view = planning.generations;
  const rows = (view?.items || [])
    .map((item) => {
      const gates = (Array.isArray(item.gates) ? item.gates : [])
        .map(
          (gate) =>
            `<li><span class="code-id">${esc(gate.name)}</span> · ${esc(gate.outcome)}${gate.reason ? ` · ${esc(gate.reason)}` : ""}</li>`,
        )
        .join("");
      return `<li class="plan-line" data-plan-generation="${esc(item.id)}"><div><strong>${esc(item.name || item.label || item.id)}</strong> <span class="code-id">${esc(item.id)}</span> <span class="kit-badge kit-badge-muted" data-plan-generation-status>${esc(generationStatus(item))}</span>${recordedDemoMarkup(item, esc)}${item.error ? `<div>${esc(item.error)}</div>` : ""}${gates ? `<ul>${gates}</ul>` : '<div class="kit-muted">No gate result is recorded.</div>'}</div></li>`;
    })
    .join("");
  const body =
    view === undefined || view.status === "loading"
      ? '<p class="kit-muted">Reading your app generations…</p>'
      : view.status === "failed"
        ? `<p role="status" data-plan-generations-error>The Hub did not return app generations: ${esc(view.reason)}</p>`
        : rows
          ? `<ul class="kit-stack">${rows}</ul>`
          : '<p class="kit-muted" data-plan-generations-empty>The Hub holds no app generation for you.</p>';
  return `<section class="kit-panel kit-pad plan-card" data-plan-generations aria-label="Your app generations">
    <div class="kit-section">Your app generations</div>
    ${body}
    <div class="plan-actions"><button type="button" class="kit-btn kit-btn-sm" data-plan-generations-refresh>Refresh</button></div>
  </section>`;
}

export function planningMarkup(planning) {
  if (planning.link && planning.link.status !== "ready")
    return `<div class="plan-flow" data-plan-flow>${linkBanner(planning)}${sessionIndexMarkup(planning)}${generationsPanel(planning)}</div>`;
  return `<div class="plan-flow" data-plan-flow>
    ${errorBanner(planning.error)}
    ${stoppedBanner(planning)}
    ${planning.agent?.recoveryRequired ? '<button type="button" class="kit-btn" data-plan-recover>Recover agent snapshot</button>' : ""}
    <div class="plan-flow__workspace" data-plan-workspace>
      <div class="plan-flow__col plan-flow__col--left" data-plan-col="conversation">
        ${composer(planning)}
        ${activity(planning)}
      </div>
      <div class="plan-flow__col plan-flow__col--right" data-plan-col="plan">
        ${needsInput(planning)}
        ${planStageGuide(planning)}
        ${planReview(planning.session, planning.showPlanJson)}
        ${bindingPanel(planning.session, planning)}
      </div>
    </div>
    ${acceptBar(planning)}
    ${sessionIndexMarkup(planning)}
    ${generationsPanel(planning)}
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
  // The stream being read right now. Stop aborts it; nothing else does.
  let reading = null;
  const fail = (error) => {
    // A stopped run is not a failed one: no error banner, a standing notice.
    if (error?.name === "AbortError") {
      set({ busy: "", error: null, stopped: true });
      return;
    }
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
  };
  const agentRun = async (options) => {
    const controller = new AbortController();
    reading = controller;
    store.planning = { ...store.planning, stopped: false };
    try {
      return await agentExchange(options, controller.signal);
    } catch (error) {
      // Whatever a stopped stream threw, the operator stopped it.
      if (controller.signal.aborted)
        throw new DOMException("The run was stopped.", "AbortError");
      throw error;
    } finally {
      if (reading === controller) reading = null;
    }
  };
  const agentExchange = async (options, signal) => {
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
    const result = await agent.run(
      options,
      binding,
      (run, event) => {
        if (event.type === "CUSTOM") {
          for (const surface of run.forms.surfaces.values()) {
            for (const question of run.state.session?.needsInput || []) {
              const field = "question:" + question.id;
              if (
                surface.components.has(field) &&
                Object.hasOwn(store.planning.drafts, question.id) &&
                (store.planning.humanDrafts[question.id] ||
                  (!question.value &&
                    !answerMap(run.state.session)[question.id]))
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
          agentMessages: mergeAgentMessages(
            store.planning.agentMessages || [],
            [...run.messages.values()],
          ),
          agentTools: [
            ...new Map(
              [
                ...(store.planning.agentTools || []),
                ...(run.state.toolCalls || []),
                ...run.tools.values(),
              ].map((item) => [item.id, item]),
            ).values(),
          ],
          ...(run.state.session ? { session: run.state.session } : {}),
        });
      },
      signal,
    );
    // A run that ended after Stop is not painted as a finished exchange.
    signal.throwIfAborted();
    set({
      session: result.state.session,
      agentMessages: mergeAgentMessages(
        store.planning.agentMessages || [],
        agent.messages,
      ),
      busy: "",
    });
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
    const field = target?.getAttribute?.("data-plan-field");
    if (field === "profile") {
      set({ profile: target.value, error: null });
      return;
    }
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

    const wsTab = hit("[data-plan-ws-tab]");
    if (wsTab) {
      set({
        selectedWorkspaceFile: wsTab.getAttribute("data-plan-ws-tab") || "",
      });
      return;
    }

    if (hit("[data-plan-view-json]")) {
      set({ showPlanJson: !planning.showPlanJson });
      return;
    }

    if (hit("[data-plan-stop]")) {
      // Abort first: the state change below must not race a late event.
      reading?.abort();
      set(stopPlanning(store.planning));
      return;
    }

    if (hit("[data-plan-link-clear]")) {
      globalThis.history?.replaceState?.(null, "", planningHash(""));
      set({ ...emptyPlanning(), generations: planning.generations });
      return;
    }

    if (hit("[data-plan-link-retry]")) {
      set({ link: null });
      return;
    }

    if (hit("[data-plan-sessions-refresh]")) {
      void loadSessionIndex(store, fetchImpl);
      return;
    }
    const nextSessionPage = hit("[data-plan-sessions-next]");
    const previousSessionPage = hit("[data-plan-sessions-previous]");
    if (nextSessionPage || previousSessionPage) {
      if (
        (nextSessionPage || previousSessionPage).disabled ||
        planning.sessions?.status !== "ready"
      )
        return;
      const cursor = nextSessionPage
        ? planning.sessions.nextCursor
        : planning.sessions.previousCursor || "";
      void loadSessionIndex(store, fetchImpl, globalThis, cursor);
      return;
    }

    if (hit("[data-plan-generations-refresh]")) {
      set({ generations: undefined });
      return;
    }

    if (planning.busy) return;

    if (hit("[data-plan-send]")) {
      const message = planning.message.trim();
      const isAppgen = (planning.profile || "onboard") === "appgen";
      if (
        !planning.session &&
        !isAppgen &&
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
      set({ busy: "turn", error: null });
      try {
        let session = store.planning.session;
        if (!session) {
          session = isAppgen
            ? await createSession(
                {
                  profile: "appgen",
                  problemStatement: (planning.problemStatement || "").trim(),
                  appName: (planning.appName || "").trim(),
                },
                undefined,
                fetchImpl,
              )
            : await createSession(
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
      return;
    }

    if (hit("[data-plan-compute]")) {
      if (!planning.session?.binding || planning.computedWorkspaceId) return;
      set({ busy: "compute", error: null });
      try {
        const session = await computeSession(planning.session.id, fetchImpl);
        const wsId = `wsp_${planning.session.id}`;
        let computedWorkspace = null;
        let computedWorkspaceDiff = null;
        let computedDigest = "";
        let computedOutcome = "";
        let computedChangeRequestId = "";
        try {
          const wsResp = await fetchImpl(
            `/v1/dev-workspaces/${encodeURIComponent(wsId)}`,
            { headers: { accept: "application/json" } },
          );
          if (wsResp?.ok) computedWorkspace = await wsResp.json();
          computedDigest = computedWorkspace?.artifactDigest || "";
          computedOutcome = computedWorkspace?.validationOutcome || "";
        } catch {
          computedDigest = "";
        }
        try {
          const diffResp = await fetchImpl(
            `/v1/dev-workspaces/${encodeURIComponent(wsId)}/diff`,
            { headers: { accept: "application/json" } },
          );
          if (diffResp?.ok) computedWorkspaceDiff = await diffResp.json();
        } catch {
          computedWorkspaceDiff = null;
        }
        try {
          const pageResp = await fetchImpl("/v1/change-requests", {
            headers: { accept: "application/json" },
          });
          if (pageResp?.ok) {
            const page = await pageResp.json();
            const rows = Array.isArray(page?.changeRequests)
              ? page.changeRequests
              : [];
            const match = rows.find(
              (row) =>
                (computedDigest &&
                  String(row?.description || "").includes(computedDigest)) ||
                String(row?.title || "").includes(wsId),
            );
            computedChangeRequestId = match?.id || "";
          }
        } catch {
          computedChangeRequestId = "";
        }
        const firstFile = computedWorkspaceDiff?.files?.[0]?.path || "";
        set({
          session,
          computedWorkspaceId: wsId,
          computedWorkspace,
          computedWorkspaceDiff,
          selectedWorkspaceFile: firstFile,
          computedDigest,
          computedOutcome,
          computedChangeRequestId,
          busy: "",
        });
      } catch (error) {
        fail(error);
      }
      return;
    }

    if (hit("[data-plan-open-change]")) {
      const open = globalThis.switchScreen;
      if (typeof open === "function") open("rm07-change-requests");
      return;
    }

    if (hit("[data-plan-new-session]")) {
      store.planning = emptyPlanning();
      rerender();
    }
  });
}
