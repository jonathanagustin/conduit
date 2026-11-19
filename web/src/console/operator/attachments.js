// Intake attachments for the Work Intake screen and the assistant panel.
// The Hub stores each file by sha256 and returns the reason when it refuses one.
// Nothing here invents a digest or a size.

export const LIMITS = {
  maxFileBytes: 256 * 1024,
  maxSpecBytes: 16 * 1024,
  maxTotalBytes: 1024 * 1024,
  maxCount: 8,
};

const EXTENSIONS = [".yaml", ".yml", ".json", ".txt", ".md"];

function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

export function formatSize(bytes) {
  const size = Number(bytes) || 0;
  if (size < 1024) return `${size} B`;
  return `${(size / 1024).toFixed(1)} KB`;
}

// precheck mirrors the Hub's type and size limits so an obvious refusal does not
// need a round trip. The Hub still decides; it returns the reason of record.
export function precheck(name, size, limits = LIMITS) {
  const file = String(name || "").trim();
  if (!file) return "File name is required.";
  const lower = file.toLowerCase();
  const allowed =
    file === "Dockerfile" || EXTENSIONS.some((ext) => lower.endsWith(ext));
  if (!allowed) {
    return `${file} is not an allowed type; use Dockerfile, .yaml, .yml, .json, .txt, or a short .md spec.`;
  }
  const cap = lower.endsWith(".md") ? limits.maxSpecBytes : limits.maxFileBytes;
  if (size > cap) {
    return `${file} is ${size} bytes and the limit for this type is ${cap}.`;
  }
  if (size === 0) return `${file} is empty.`;
  return "";
}

export async function loadAttachments(fetchImpl = fetch) {
  const response = await fetchImpl("/v1/intake-attachments", {
    headers: { accept: "application/json" },
  });
  if (!response.ok) {
    return { items: [], limits: LIMITS, error: "Attachments are unavailable." };
  }
  const body = await response.json();
  return {
    items: Array.isArray(body.items) ? body.items : [],
    limits: body.limits || LIMITS,
    error: "",
  };
}

// uploadAttachment sends one file as a JSON object. On refusal it returns the
// Hub's reason and nothing was stored.
export async function uploadAttachment(file, fetchImpl = fetch) {
  const early = precheck(file.name, file.size);
  if (early) return { ok: false, reason: early };
  const content = await file.text();
  let response;
  try {
    response = await fetchImpl("/v1/intake-attachments", {
      method: "POST",
      headers: {
        "content-type": "application/json",
        accept: "application/json",
      },
      body: JSON.stringify({ filename: file.name, content }),
    });
  } catch {
    return { ok: false, reason: "The upload did not reach the Hub." };
  }
  let body = {};
  try {
    body = await response.json();
  } catch {
    body = {};
  }
  if (!response.ok) {
    return {
      ok: false,
      reason: body.error || `The Hub refused the upload (${response.status}).`,
    };
  }
  return { ok: true, item: body };
}

export function mergeItem(items, item) {
  if ((items || []).some((row) => row.sha256 === item.sha256)) return items;
  return [...(items || []), item];
}

// attachmentList is the read-only view of stored files.
export function attachmentList(items) {
  if (!items || items.length === 0) {
    return `<div class="text-[11px] text-[var(--text-2)]" data-attachment-empty>No attachments stored.</div>`;
  }
  return `<div class="flex flex-col gap-1.5" data-attachment-list>${items
    .map(
      (
        item,
      ) => `<div class="px-2.5 py-1.5 rounded-[5px] bg-[var(--bg-1)] border border-[var(--border-subtle)] text-[11px]" data-attachment-row>
        <div class="flex items-center gap-1.5"><span class="code-id text-[var(--text-1)]">${esc(item.name)}</span><span class="text-[10px] text-[var(--text-2)]">(${esc(item.kind)} · ${esc(formatSize(item.size))})</span></div>
        <div class="code-id text-[10px] text-[var(--text-2)] break-all" title="sha256">sha256 ${esc(item.sha256)}</div>
      </div>`,
    )
    .join("")}</div>`;
}

// attachControl is the Attach button, its hidden file input, and the slot that
// shows why an upload failed.
export function attachControl(reason, id = "intake") {
  return `<span class="inline-flex flex-col gap-1">
    <button type="button" class="so-btn so-btn-sm" data-attach-button="${esc(id)}">Attach</button>
    <input type="file" class="hidden" data-attach-input="${esc(id)}" accept=".yaml,.yml,.json,.txt,.md,Dockerfile" />
    <span class="text-[10.5px] text-[var(--err-text)]" data-attach-reason role="alert">${esc(reason || "")}</span>
  </span>`;
}

const chromeSnapshots = new WeakMap();

// A file chooser retains its input node while open. Replacing that node during
// an unrelated chrome refresh loses the selection before its change can bubble.
// Identity or input-constraint changes deliberately invalidate that context.
export function updateAttachmentChrome(root, markup, scope) {
  if (!root) return;
  const previous = chromeSnapshots.get(root);
  if (previous && previous.markup === markup && previous.scope === scope)
    return;
  const inputs =
    previous && previous.scope === scope
      ? [...root.querySelectorAll('input[type="file"][data-attach-input]')]
      : [];
  root.innerHTML = markup;
  for (const input of inputs) {
    const replacement = [
      ...root.querySelectorAll('input[type="file"][data-attach-input]'),
    ].find(
      (candidate) =>
        candidate.getAttribute("data-attach-input") ===
        input.getAttribute("data-attach-input"),
    );
    if (
      replacement &&
      replacement.accept === input.accept &&
      replacement.multiple === input.multiple &&
      replacement.disabled === input.disabled
    )
      replacement.replaceWith(input);
  }
  chromeSnapshots.set(root, { markup, scope });
}

// bindAttach wires every Attach control under root. state.attachments holds
// { items, error, reason }. onChange re-renders after a load or an upload.
export function bindAttach(root, state, onChange, fetchImpl = fetch) {
  if (!root || root.dataset.attachBound === "1") return;
  root.dataset.attachBound = "1";
  root.addEventListener("click", (event) => {
    const button =
      event.target instanceof Element
        ? event.target.closest("[data-attach-button]")
        : null;
    if (!button) return;
    const input = root.querySelector(
      `[data-attach-input="${button.getAttribute("data-attach-button")}"]`,
    );
    if (input) input.click();
  });
  root.addEventListener("change", async (event) => {
    const input = event.target;
    if (
      !(input instanceof HTMLInputElement) ||
      !input.matches("[data-attach-input]")
    )
      return;
    const file = input.files && input.files[0];
    input.value = "";
    if (!file) return;
    const result = await uploadAttachment(file, fetchImpl);
    const current = state.attachments || { items: [] };
    state.attachments = result.ok
      ? { ...current, items: mergeItem(current.items, result.item), reason: "" }
      : { ...current, reason: result.reason };
    onChange();
  });
}

// ensureLoaded fetches the stored list once per page load, then re-renders.
export function ensureLoaded(state, onChange, fetchImpl = fetch) {
  if (state.attachments?.loaded || state.attachments?.loading) return;
  state.attachments = {
    ...(state.attachments || {}),
    items: [],
    loading: true,
  };
  loadAttachments(fetchImpl)
    .then((loaded) => {
      state.attachments = {
        items: loaded.items,
        limits: loaded.limits,
        reason: loaded.error,
        loaded: true,
      };
    })
    .catch(() => {
      state.attachments = {
        items: [],
        reason: "Attachments are unavailable.",
        loaded: true,
      };
    })
    .finally(onChange);
}
