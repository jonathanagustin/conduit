const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const labels = {
  "packaging.step": "Packaging progress",
  "plan.changed": "Plan changed",
  "approval.changed": "Approval changed",
  "spoke.reported": "Spoke reported",
  "assistant.activity": "Assistant activity",
  "index.invalidated": "Authorized index refreshed",
};

// Observation-only presentation buffer. Business state is always read from Hub.
export function createLiveOperations() {
  let rows = [],
    stream = "",
    sequence = 0,
    omitted = 0;
  return {
    reset() {
      rows = [];
      stream = "";
      sequence = 0;
      omitted = 0;
    },
    accept(events) {
      for (const event of events) {
        if (stream !== event.streamID) {
          rows = [];
          sequence = 0;
          omitted = 0;
          stream = event.streamID;
        }
        if (event.sequence <= sequence) continue;
        sequence = event.sequence;
        // Never retain the opaque delivery cursor or arbitrary payload fields.
        rows.unshift(
          Object.freeze({
            sequence,
            occurredAt: event.occurredAt,
            label: labels[event.kind],
            resource: event.resource.type === "index" ? "" : event.resource.id,
            detail:
              event.kind === "index.invalidated"
                ? (event.data.areas || []).join(", ")
                : [
                    event.data.phase,
                    event.data.outcome,
                    event.data.status,
                    event.data.state,
                    event.data.activity,
                  ]
                    .filter(Boolean)
                    .join(" · "),
          }),
        );
        if (rows.length > 100) {
          rows.pop();
          omitted++;
        }
      }
    },
    markup(status) {
      return `<section data-live-operations data-value-tier="CONDUIT_NATIVE" class="so-panel p-3 space-y-2"><h2>Live operations</h2><p role="status" data-operations-state="${esc(status.state)}">${esc(status.message)}</p><p class="kit-muted">Latest 100 authorized updates received in this session after successful model reads, newest first. This is not a complete event history or the authorization audit. Reconnect catch-up does not invent past feed entries. Spoke events record reports, not network probes.</p>${omitted ? `<p>${omitted} earlier session updates omitted.</p>` : ""}${rows.length ? `<ol>${rows.map((row) => `<li data-operation-sequence="${row.sequence}"><time>${esc(row.occurredAt)}</time> · <strong>${esc(row.label)}</strong>${row.resource ? ` · ${esc(row.resource)}` : ""}${row.detail ? ` · ${esc(row.detail)}` : ""}</li>`).join("")}</ol>` : "<p>No session updates displayed. Missing access or an unavailable stream does not mean no events occurred.</p>"}</section>`;
    },
  };
}
