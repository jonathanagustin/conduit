// Audit ledger view: lists hub audit entries and checks the hash links
// between them. The API does not return the payload each digest covers, so
// digests cannot be recomputed here; only the previousDigest links are checked.

export const CHAIN_VERSION = "conduit.authorization-audit-chain/v1";

const NOTE =
  "Each entry stores the hash of the entry before it. This check confirms those links; the Hub verifies each entry's own hash.";

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
  return Array.from(new Uint8Array(hash), (b) => b.toString(16).padStart(2, "0")).join("");
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
    return [entry.actor, entry.action, entry.id].some((field) =>
      String(field ?? "").toLowerCase().includes(q),
    );
  });
}

function shortHash(value, escape) {
  const full = String(value ?? "");
  return `<code class="code-id" title="${escape(full)}">${escape(full.slice(0, 12))}</code>`;
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
  { query = "", decision = "all", page = 0, pageSize = 50, check = null, esc: escape = esc } = {},
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
    `</div>`;

  const result = checkMarkup(check, all, escape);
  const note = `<p class="so-muted">${escape(NOTE)}</p>`;

  if (total === 0) {
    return `<div class="audit-ledger">${toolbar}${result}${note}<p class="so-muted">No entry matches.</p></div>`;
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
        `</tr>`
      );
    })
    .join("");

  const table =
    `<table class="so-table"><thead><tr>` +
    `<th>#</th><th>Time</th><th>Actor</th><th>Action</th><th>Decision</th><th>Previous</th><th>Hash</th>` +
    `</tr></thead><tbody>${rows}</tbody></table>`;

  const from = start + 1;
  const to = start + slice.length;
  const pager =
    `<div class="ledger-pager">` +
    `<span>Showing ${from}–${to} of ${total}</span>` +
    `<button type="button" class="so-btn so-btn-sm" data-ledger-page="${current - 1}"${current <= 0 ? " disabled" : ""}>Previous</button>` +
    `<button type="button" class="so-btn so-btn-sm" data-ledger-page="${current + 1}"${current >= lastPage ? " disabled" : ""}>Next</button>` +
    `</div>`;

  return `<div class="audit-ledger">${toolbar}${result}${note}${table}${pager}</div>`;
}

/**
 * Stateful wrapper. `getEntries()` returns the entries (oldest first);
 * `rerender()` is called after state changes so the host can call render(root).
 */
export function createAuditLedger({ getEntries, rerender }) {
  const state = { query: "", decision: "all", page: 0, check: null };

  function render(root) {
    root.innerHTML = ledgerMarkup(getEntries(), { ...state, esc });
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

      if (target.closest("[data-ledger-check]")) {
        const genesis = await genesisDigest();
        state.check = checkChainLinks(getEntries(), genesis);
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
        if (typeof input.setSelectionRange === "function") input.setSelectionRange(pos, pos);
      }
    });
  }

  return { render, bind };
}
