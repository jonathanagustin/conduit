import { verifyProjection } from "./audit-proof.mjs";
// Audit projection display. Older responses support link checks only; exact
// returned payload inputs allow separate projection-digest recomputation.

export const CHAIN_VERSION = "conduit.authorization-audit-chain/v1";

const NOTE =
  "Link checks do not recompute payload digests. Returned-payload verification checks projection consistency only; stored ledger order, event truth and independent checkpoints are separate.";

function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/** SHA-256 of `${CHAIN_VERSION}:genesis`, as lowercase hex. */
export async function genesisDigest() {
  const bytes = new TextEncoder().encode(`${CHAIN_VERSION}:genesis`);
  const hash = await globalThis.crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(hash), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}

/**
 * Checks sequence continuity and previousDigest links. Entries are oldest first.
 * Returns { ok, checked, breaks: [{ sequence, reason }] }.
 */
export function checkChainLinks(entries, genesis) {
  const list = Array.isArray(entries) ? entries : [];
  const breaks = [];
  for (let i = 0; i < list.length; i += 1) {
    const entry = list[i];
    if (i === 0) {
      if (entry.sequence === 1 && entry.previousDigest !== genesis) {
        breaks.push({
          sequence: entry.sequence,
          reason: "The first entry does not link to the genesis checkpoint.",
        });
      }
      continue;
    }
    const prev = list[i - 1];
    if (entry.sequence !== prev.sequence + 1) {
      breaks.push({
        sequence: entry.sequence,
        reason: `Sequence jumps from ${prev.sequence} to ${entry.sequence}.`,
      });
    }
    if (entry.previousDigest !== prev.digest) {
      breaks.push({
        sequence: entry.sequence,
        reason: "The previous hash does not match the entry before it.",
      });
    }
  }
  return { ok: breaks.length === 0, checked: list.length, breaks };
}

/** Filters by free-text query (actor, action, id) and decision. */
export function filterEntries(entries, { query = "", decision = "all" } = {}) {
  const list = Array.isArray(entries) ? entries : [];
  const q = String(query).trim().toLowerCase();
  const want = String(decision).toLowerCase();
  return list.filter((entry) => {
    if (want === "allow" || want === "deny") {
      if (String(entry.decision).toLowerCase() !== want) return false;
    }
    if (!q) return true;
    return [
      entry.actor,
      entry.action,
      entry.id,
      ...exchangeSearchFields(entry),
    ].some((field) =>
      String(field ?? "")
        .toLowerCase()
        .includes(q),
    );
  });
}

function shortHash(value, escape) {
  const full = String(value ?? "");
  return `<code class="code-id" title="${escape(full)}">${escape(full.slice(0, 12))}</code>`;
}

function contextOf(entry) {
  if (entry && Object.hasOwn(entry, "payload")) {
    try {
      if (typeof entry.payload !== "string" || entry.payload.length > 2_800_000)
        return {};
      const binary = atob(entry.payload);
      const payload = JSON.parse(
        new TextDecoder("utf-8", { fatal: true }).decode(
          Uint8Array.from(binary, (char) => char.charCodeAt(0)),
        ),
      );
      return payload?.context && typeof payload.context === "object"
        ? payload.context
        : {};
    } catch {
      return {};
    }
  }
  return entry && entry.context && typeof entry.context === "object"
    ? entry.context
    : {};
}

function firstPresent(ctx, keys) {
  for (const key of keys) {
    const value = ctx[key];
    if (value === undefined || value === null || value === "") continue;
    return value;
  }
  return "";
}

/** Prompt, response, skill hash, and tool names recorded on the event, if any. */
export function exchangeFields(entry) {
  const ctx = contextOf(entry);
  const tools = firstPresent(ctx, ["toolNames", "mcpTools", "tools"]);
  let toolText = "";
  if (Array.isArray(tools)) {
    toolText = tools
      .map((item) =>
        item && typeof item === "object"
          ? item.name || item.id || ""
          : String(item),
      )
      .filter(Boolean)
      .join(", ");
  } else if (tools) {
    toolText = String(tools);
  }
  return {
    promptSha256: String(firstPresent(ctx, ["promptSha256"]) || ""),
    responseSha256: String(firstPresent(ctx, ["responseSha256"]) || ""),
    skillSha256: String(
      firstPresent(ctx, ["skillSha256", "skillHash", "packagingSkillSha256"]) ||
        "",
    ),
    tools: toolText,
  };
}

function exchangeSearchFields(entry) {
  const fields = exchangeFields(entry);
  return [
    fields.promptSha256,
    fields.responseSha256,
    fields.skillSha256,
    fields.tools,
  ];
}

export function modelProvenance(entry) {
  const context = contextOf(entry);
  const live =
    typeof context.liveModelCall === "boolean"
      ? context.liveModelCall
        ? "yes"
        : "no"
      : context.adapter === "recorded"
        ? "no"
        : "unknown";
  const digest = (value) =>
    typeof value === "string" && /^[0-9a-f]{64}$/.test(value)
      ? value
      : "unavailable";
  return {
    live,
    input: digest(context.promptSha256),
    output: digest(context.responseSha256),
    adapter:
      typeof context.adapter === "string" ? context.adapter : "unavailable",
  };
}
function provenanceMarkup(entry, escape) {
  const provenance = modelProvenance(entry);
  return `<details data-audit-provenance><summary>Object and model provenance</summary><p>Target: ${escape(entry.resourceRid || "unavailable")}</p><p>Live model call: ${escape(provenance.live)} · adapter: ${escape(provenance.adapter)}</p><p>Input SHA-256: ${escape(provenance.input)}</p><p>Output SHA-256: ${escape(provenance.output)}</p><p>Recorded metadata; an adapter name or a hash alone does not prove a live provider call.</p></details>`;
}

function exchangeMarkup(entry, escape) {
  const fields = exchangeFields(entry);
  const parts = [];
  if (fields.promptSha256) {
    parts.push(`<span>prompt ${shortHash(fields.promptSha256, escape)}</span>`);
  }
  if (fields.responseSha256) {
    parts.push(
      `<span>response ${shortHash(fields.responseSha256, escape)}</span>`,
    );
  }
  if (fields.skillSha256) {
    parts.push(`<span>skill ${shortHash(fields.skillSha256, escape)}</span>`);
  }
  if (fields.tools) {
    parts.push(`<span>tools ${escape(fields.tools)}</span>`);
  }
  if (!parts.length) {
    return `<span class="so-muted">—</span>`;
  }
  return `<div class="ledger-exchange">${parts.join(" ")}</div>`;
}

function checkMarkup(check, entries, escape) {
  if (!check) return "";
  if (check.ok) {
    const first = entries.length ? entries[0].sequence : 0;
    const last = entries.length ? entries[entries.length - 1].sequence : 0;
    const from = first === 1 ? "the genesis checkpoint" : `entry ${first}`;
    return `<div class="kit-card kit-bar-ok">All ${escape(check.checked)} links match, from ${from} to entry ${escape(last)}.</div>`;
  }
  const items = check.breaks
    .map((b) => `<li>Entry ${escape(b.sequence)}: ${escape(b.reason)}</li>`)
    .join("");
  return `<div class="kit-card kit-bar-err"><p>${escape(check.breaks.length)} link problem(s) found.</p><ul>${items}</ul></div>`;
}

/**
 * Builds the ledger HTML. `entries` are oldest first; the table shows newest first.
 * @param {any[]} entries
 * @param {{ query?: string, decision?: string, page?: number, pageSize?: number, check?: any, esc?: (v: unknown) => string }} [options]
 */
export function ledgerMarkup(
  entries,
  {
    query = "",
    decision = "all",
    page = 0,
    pageSize = 50,
    check = null,
    proof = null,
    checking = false,
    esc: escape = esc,
  } = {},
) {
  const all = Array.isArray(entries) ? entries : [];
  const filtered = filterEntries(all, { query, decision }).slice().reverse();
  const total = filtered.length;
  const size = Math.max(1, pageSize | 0);
  const lastPage = Math.max(0, Math.ceil(total / size) - 1);
  const current = Math.min(Math.max(0, page | 0), lastPage);
  const start = current * size;
  const slice = filtered.slice(start, start + size);

  const chips = ["all", "allow", "deny"]
    .map((d) => {
      const label = d === "all" ? "All" : d === "allow" ? "Allow" : "Deny";
      const active = d === decision ? " is-active" : "";
      return `<button type="button" class="kit-tab${active}" data-ledger-decision="${d}">${label}</button>`;
    })
    .join("");

  const toolbar =
    `<div class="ledger-toolbar">` +
    `<input type="search" data-ledger-query value="${escape(query)}" placeholder="Search actor, action or id" aria-label="Search audit entries">` +
    chips +
    `<button type="button" class="so-btn so-btn-sm" data-ledger-check>Check chain links</button>` +
    `<button type="button" class="so-btn so-btn-sm" data-ledger-proof${checking ? " disabled" : ""}>Verify returned payload digests</button>` +
    `</div>`;

  const result =
    checkMarkup(check, all, escape) +
    (proof
      ? `<p role="status" class="kit-card ${proof.ok ? "kit-bar-ok" : "kit-bar-err"}" data-audit-proof>${escape(proof.reason)} Checked ${escape(proof.checked)} returned entries.</p>`
      : "");
  const note = `<p class="so-muted">${escape(NOTE)}</p>`;

  if (total === 0) {
    return `<div class="audit-ledger" data-value-tier="CONDUIT_NATIVE">${toolbar}${result}${note}<p class="so-muted">No entry matches.</p></div>`;
  }

  const rows = slice
    .map((e) => {
      const ok = String(e.decision).toUpperCase() === "ALLOW";
      const badge = `<span class="kit-badge ${ok ? "kit-badge-ok" : "kit-badge-err"}">${escape(e.decision)}</span>`;
      return (
        `<tr>` +
        `<td>${escape(e.sequence)}</td>` +
        `<td>${escape(e.createdAt)}</td>` +
        `<td>${escape(e.actor)}</td>` +
        `<td>${escape(e.action)}</td>` +
        `<td>${badge}</td>` +
        `<td>${shortHash(e.previousDigest, escape)}</td>` +
        `<td>${shortHash(e.digest, escape)}</td>` +
        `<td>${exchangeMarkup(e, escape)}${provenanceMarkup(e, escape)}</td>` +
        `</tr>`
      );
    })
    .join("");

  const table =
    `<table class="so-table"><thead><tr>` +
    `<th>#</th><th>Time</th><th>Actor</th><th>Action</th><th>Decision</th><th>Previous</th><th>Hash</th><th>Model exchange</th>` +
    `</tr></thead><tbody>${rows}</tbody></table>`;

  const from = start + 1;
  const to = start + slice.length;
  const pager =
    `<div class="ledger-pager">` +
    `<span>Showing ${from}–${to} of ${total}</span>` +
    `<button type="button" class="so-btn so-btn-sm" data-ledger-page="${current - 1}"${current <= 0 ? " disabled" : ""}>Previous</button>` +
    `<button type="button" class="so-btn so-btn-sm" data-ledger-page="${current + 1}"${current >= lastPage ? " disabled" : ""}>Next</button>` +
    `</div>`;

  return `<div class="audit-ledger" data-value-tier="CONDUIT_NATIVE">${toolbar}${result}${note}${table}${pager}</div>`;
}

/**
 * Stateful wrapper. `getEntries()` returns the entries (oldest first);
 * `rerender()` is called after state changes so the host can call render(root).
 */
export function createAuditLedger({ getEntries, rerender }) {
  const state = {
    query: "",
    decision: "all",
    page: 0,
    check: null,
    proof: null,
    checking: false,
    checkedEntries: null,
  };

  function render(root) {
    const entries = getEntries();
    root.innerHTML = ledgerMarkup(entries, {
      ...state,
      check: state.checkedEntries === entries ? state.check : null,
      proof: state.checkedEntries === entries ? state.proof : null,
      esc,
    });
  }

  function bind(root) {
    if (root.dataset.ledgerBound === "1") return;
    root.dataset.ledgerBound = "1";

    root.addEventListener("click", async (event) => {
      const target = event.target;
      if (!target || typeof target.closest !== "function") return;

      const chip = target.closest("[data-ledger-decision]");
      if (chip) {
        state.decision = chip.dataset.ledgerDecision;
        state.page = 0;
        rerender();
        return;
      }

      const pageBtn = target.closest("[data-ledger-page]");
      if (pageBtn) {
        if (pageBtn.disabled) return;
        const next = Number(pageBtn.dataset.ledgerPage);
        if (Number.isFinite(next) && next >= 0) {
          state.page = next;
          rerender();
        }
        return;
      }

      if (target.closest("[data-ledger-proof]")) {
        if (state.checking) return;
        const entries = getEntries();
        state.checking = true;
        state.proof = null;
        rerender();
        try {
          const proof = await verifyProjection(entries);
          if (getEntries() === entries) {
            state.proof = proof;
            state.checkedEntries = entries;
          }
        } catch {
          state.proof = null;
        } finally {
          state.checking = false;
          rerender();
        }
        return;
      }

      if (target.closest("[data-ledger-check]")) {
        const entries = getEntries();
        const genesis = await genesisDigest();
        if (getEntries() !== entries) return;
        state.check = checkChainLinks(entries, genesis);
        state.checkedEntries = entries;
        rerender();
      }
    });

    root.addEventListener("input", (event) => {
      const target = event.target;
      if (!target || typeof target.matches !== "function") return;
      if (!target.matches("[data-ledger-query]")) return;
      const caret = target.selectionStart;
      state.query = target.value;
      state.page = 0;
      rerender();
      const input = root.querySelector("[data-ledger-query]");
      if (input) {
        input.focus();
        const pos = typeof caret === "number" ? caret : input.value.length;
        if (typeof input.setSelectionRange === "function")
          input.setSelectionRange(pos, pos);
      }
    });
  }

  return { render, bind };
}
