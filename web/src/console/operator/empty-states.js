// Empty states for Hub-backed views. The Hub's own message (or the default
// "No X is recorded.") stays the first line; the title names the state and
// the guidance says what makes the first record appear. Actions only
// navigate or re-read the Hub; nothing here writes.

const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

export const EMPTY = Object.freeze({
  packaging: {
    title: "No packaging runs yet",
    guide: "A run appears here once a packaging run is submitted to this Hub.",
    next: ["Open workspaces", "s02-workspaces"],
  },
  bundle: {
    title: "No signed bundle yet",
    guide:
      "A bundle preview appears once a packaging run produces a signed bundle.",
    next: ["Open packaging runs", "pkg-runs"],
  },
  changes: {
    title: "Change request queue is clear",
    guide:
      "A change request appears when a plan or a drift fix needs a second person's approval.",
    next: ["Open drift", "s14-env-detail"],
  },
  products: {
    title: "Product catalog is empty",
    guide: "A product appears once its first release is published to this Hub.",
    next: null,
  },
  promotion: {
    title: "No promotions recorded",
    guide:
      "A promotion appears once a release moves from one channel to the next.",
    next: ["Open channels", "rm-channels"],
  },
  channels: {
    title: "No channel subscriptions",
    guide:
      "Environments subscribe to a release channel; none has subscribed yet.",
    next: ["Open environments", "flt-environments"],
  },
  environments: {
    title: "No environments enrolled",
    guide:
      "An environment appears once its spoke agent joins this Hub and sends a report.",
    next: null,
  },
  recall: {
    title: "No recalls",
    guide:
      "A recall appears when a release is pulled back from its environments.",
    next: ["Open waves", "rm-waves"],
  },
  audit: {
    title: "Audit ledger is empty",
    guide:
      "Every action the Hub records lands here, hash-chained to the one before.",
    next: null,
  },
});

/** The empty-state card for one view; message is the Hub's own note. */
export function emptyState(kind, message, { esc = defaultEsc } = {}) {
  const spec = EMPTY[kind];
  const action = spec?.next
    ? `<button type="button" class="so-btn so-btn-sm" data-onclick="switchScreen('${esc(spec.next[1])}')">${esc(spec.next[0])}</button>`
    : "";
  return `<section class="kit-card kit-state kit-nonideal" role="status" data-state="empty"${kind ? ` data-empty="${esc(kind)}"` : ""}>
    <div class="kit-state-title">${esc(spec?.title || "Nothing recorded yet")}</div>
    <p>${esc(message)}</p>
    ${spec ? `<p class="kit-muted">${esc(spec.guide)}</p>` : ""}
    <div class="kit-nonideal-actions"><button type="button" class="so-btn so-btn-sm" data-onclick="refreshHub()">Check again</button>${action}</div>
  </section>`;
}

/**
 * Hub setup checklist for a Hub that has nothing in it yet. Each step is
 * done when the Hub returns at least one record of that kind; it renders
 * nothing once any product, environment or packaging run exists, or before
 * the Hub has answered at all.
 */
export function hubSetupMarkup(hub, { esc = defaultEsc } = {}) {
  // A Hub that failed to answer is not an empty Hub.
  if (!hub || hub.note || hub.productsNote || hub.packagingNote) return "";
  const count = (value) => (Array.isArray(value) ? value.length : 0);
  const products = count(hub.products);
  const environments = count(hub.environments);
  const packaging = count(hub.packaging);
  if (products || environments || packaging) return "";
  const steps = [
    ["Publish a product release", products, "rel-products"],
    ["Enroll a spoke environment", environments, "flt-environments"],
    ["Run packaging", packaging, "pkg-runs"],
    ["Review a change request", count(hub.changes), "rm07-change-requests"],
  ];
  const done = steps.filter(([, n]) => n > 0).length;
  return `<section class="so-panel kit-view-panel kit-setup kit-nonideal" data-hub-setup aria-label="Hub setup">
    <div class="kit-view-head"><h2>Hub setup</h2><span class="kit-muted">${done} of ${steps.length} done</span></div>
    <p class="kit-muted">This Hub has no products, environments or packaging runs yet. The panels below are example data until it does.</p>
    <ol class="kit-setup-steps">${steps
      .map(
        ([label, n, screen], i) =>
          `<li class="${n ? "is-done" : ""}"><span>${i + 1}. ${esc(label)}${n ? " &#10003;" : ""}</span>${n ? "" : `<button type="button" class="so-btn so-btn-sm" data-onclick="switchScreen('${esc(screen)}')">Open</button>`}</li>`,
      )
      .join("")}</ol>
  </section>`;
}
