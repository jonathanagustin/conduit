import {
  fetchNotificationEvents,
  notificationEventsTransport,
} from "../../api/notification-events.js";
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const mounts = new WeakMap();
export function visibleAuditResources(entries) {
  return [
    ...new Set(
      (Array.isArray(entries) ? entries : [])
        .map((entry) => entry?.resourceRid)
        .filter((rid) => typeof rid === "string" && rid !== ""),
    ),
  ]
    .sort()
    .slice(0, 256);
}
export function scopedNotificationEvents(body, resource) {
  if (
    body?.resourceRid !== resource ||
    !Array.isArray(body.events) ||
    body.events.some(
      (event) =>
        !event ||
        event.resourceRid !== resource ||
        typeof event.id !== "string",
    )
  )
    throw new Error("Notification events did not match the selected resource.");
  return [
    ...new Map(
      body.events.slice(0, 100).map((event) => [event.id, event]),
    ).values(),
  ];
}
export function createAuditEventStream({ getEntries, getIdentity }) {
  let active = null;
  function mount(root) {
    if (active?.reuse(root)) return;
    active?.dispose();
    const token = {};
    mounts.set(root, token);
    const identity = JSON.stringify(getIdentity());
    const current = () => mounts.get(root) === token && root.isConnected;
    let resource = "",
      rows = [],
      paused = true,
      busy = false,
      note =
        "Choose a visible audit resource. This is polling, not a push stream.",
      timer = null,
      controller = null,
      generation = 0;
    const stop = () => {
      paused = true;
      generation++;
      clearTimeout(timer);
      controller?.abort();
    };
    const resources = () => visibleAuditResources(getEntries());
    const sameIdentity = () =>
      Boolean(getIdentity()?.actor) &&
      JSON.stringify(getIdentity()) === identity;
    const paint = () => {
      if (!current()) return;
      root.innerHTML = `<section data-audit-event-stream class="so-panel p-3 space-y-2"><h3>Notification events · polled</h3><p>Resource-authorized latest 100 notification events, not the complete authorization audit or a durable push stream. Up to 256 visible audit resources are selectable. Polls every 10 seconds while active and visible.</p><label>Resource <select aria-label="Audit event resource" data-audit-resource><option value="">Choose a visible resource</option>${resources()
        .map(
          (rid) =>
            `<option value="${esc(rid)}"${rid === resource ? " selected" : ""}>${esc(rid)}</option>`,
        )
        .join(
          "",
        )}</select></label><button type="button" class="so-btn" data-audit-poll${!resource || !sameIdentity() ? " disabled" : ""}>${paused ? "Start polling" : "Pause polling"}</button><button type="button" class="so-btn" data-audit-refresh${busy || !resource || !sameIdentity() ? " disabled" : ""}>Refresh events</button><p role="status">${esc(note)}</p>${rows.length ? `<ul>${rows.map((event) => `<li>${esc(event.occurredAt || "time unavailable")} · ${esc(event.type || "type unavailable")} · ${esc(event.actor || "system or unavailable")} · ${esc(event.summary || "summary unavailable")}</li>`).join("")}</ul>` : "<p>No events are displayed. Missing access or an unavailable read does not mean no events occurred.</p>"}</section>`;
    };
    const schedule = () => {
      clearTimeout(timer);
      if (!paused && current())
        timer = setTimeout(() => {
          if (!current()) {
            stop();
            return;
          }
          if (document.hidden) {
            schedule();
            return;
          }
          void load();
        }, 10000);
    };
    const load = async () => {
      if (
        !current() ||
        !sameIdentity() ||
        !resource ||
        !resources().includes(resource)
      ) {
        stop();
        note = "Identity or visible resource changed; select again.";
        paint();
        return;
      }
      if (busy) return;
      const selected = resource,
        stamp = generation;
      controller = new AbortController();
      const request = controller;
      const timeout = setTimeout(() => request.abort(), 10000);
      busy = true;
      paint();
      try {
        const body = await fetchNotificationEvents(
          selected,
          notificationEventsTransport(window.location.origin),
          request.signal,
          BigInt(100),
        );
        if (
          !current() ||
          stamp !== generation ||
          !sameIdentity() ||
          !resources().includes(selected)
        )
          return;
        rows = scopedNotificationEvents(body, selected);
        note = `${rows.length} returned notification events for ${selected}. Polling ${paused ? "paused" : "active"}.`;
      } catch (error) {
        if (current() && stamp === generation) {
          rows = [];
          stop();
          note = `${error.message} Polling paused; no event-free result is inferred.`;
        }
      } finally {
        clearTimeout(timeout);
        if (controller === request) {
          busy = false;
          controller = null;
        }
        if (current()) {
          paint();
          schedule();
        }
      }
    };
    const bind = () => {
      root.onchange = (event) => {
        if (!event.target?.matches?.("[data-audit-resource]")) return;
        stop();
        resource = event.target.value;
        rows = [];
        busy = false;
        note = "Selection changed; polling is paused.";
        paint();
      };
      root.onclick = (event) => {
        const button = event.target?.closest?.("button");
        if (!button || button.disabled) return;
        if (button.matches("[data-audit-poll]")) {
          if (!paused) {
            stop();
            busy = false;
            note = "Polling paused.";
            paint();
          } else {
            paused = false;
            generation++;
            void load();
          }
        }
        if (button.matches("[data-audit-refresh]")) void load();
      };
    };
    const session = {
      reuse(nextRoot) {
        if (!sameIdentity()) return false;
        root.onchange = null;
        root.onclick = null;
        mounts.delete(root);
        root = nextRoot;
        mounts.set(root, token);
        if (resource && !resources().includes(resource)) {
          stop();
          resource = "";
          rows = [];
          busy = false;
          note = "Visible resource changed; select again.";
        }
        bind();
        paint();
        return true;
      },
      dispose() {
        stop();
        rows = [];
        resource = "";
        root.onchange = null;
        root.onclick = null;
        mounts.delete(root);
      },
    };
    active = session;
    bind();
    paint();
  }
  mount.dispose = () => {
    active?.dispose();
    active = null;
  };
  return mount;
}
