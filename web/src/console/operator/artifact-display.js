/*
 * How a Release's or a Plan step's deployable artifact is shown.
 *
 * The Hub's field is `chart` on both (internal/catalog/domain/release.go
 * Artifact, internal/delivery/plan/plan.go Step.Chart): {repository, tag,
 * digest, kind?, architectures?, packageName?}. An absent kind is a Helm
 * chart, which is what every Release written before kind existed is.
 */

/** internal/catalog/domain/release.go — the ArtifactKind constants. */
export const ARTIFACT_KINDS = Object.freeze({
  "helm-chart": "Helm chart",
  "zarf-package": "Zarf package",
  "uds-bundle": "UDS bundle",
});

const DEFAULT_KIND = "helm-chart";

const text = (value) => (typeof value === "string" ? value.trim() : "");

const defaultEsc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

/** `sha256:` and the first twelve hex characters; anything else unchanged. */
export function shortDigest(digest) {
  const value = text(digest);
  const match = /^([a-z0-9]+):([a-f0-9]{13,})$/.exec(value);
  return match ? `${match[1]}:${match[2].slice(0, 12)}…` : value;
}

/**
 * What the Hub returned about an artifact, or null when it returned none.
 * A kind the console does not know is shown as the Hub wrote it.
 * @returns {null | {kind: string, kindLabel: string, digest: string,
 *   shortDigest: string, architectures: string[], packageName: string}}
 */
export function artifactSummary(artifact) {
  if (!artifact || typeof artifact !== "object") return null;
  const digest = text(artifact.digest);
  const declared = text(artifact.kind);
  const architectures = Array.isArray(artifact.architectures)
    ? artifact.architectures.map(text).filter(Boolean)
    : [];
  const packageName = text(artifact.packageName);
  if (!digest && !declared && !architectures.length && !packageName)
    return null;
  const kind = declared || DEFAULT_KIND;
  return {
    kind,
    kindLabel: ARTIFACT_KINDS[kind] || kind,
    digest,
    shortDigest: shortDigest(digest),
    architectures,
    packageName,
  };
}

/**
 * The artifact as inline markup: its kind, its digest shortened with the full
 * value on hover and behind Copy, and its architectures when declared.
 */
export function artifactMarkup(artifact, esc = defaultEsc) {
  const summary = artifactSummary(artifact);
  if (!summary) return "";
  const digest = summary.digest
    ? ` <code class="code-id" data-artifact-digest title="${esc(summary.digest)}">${esc(summary.shortDigest)}</code> <button type="button" class="kit-btn kit-btn-sm" data-copy-text="${esc(summary.digest)}" aria-label="Copy digest ${esc(summary.digest)}">Copy</button>`
    : "";
  const architectures = summary.architectures.length
    ? ` <span data-artifact-architectures title="Architectures this artifact was built for">${esc(summary.architectures.join(", "))}</span>`
    : "";
  const packageName = summary.packageName
    ? ` <span class="kit-muted" data-artifact-package title="Package name">${esc(summary.packageName)}</span>`
    : "";
  return `<span data-artifact data-artifact-kind="${esc(summary.kind)}"><span class="kit-badge kit-badge-muted">${esc(summary.kindLabel)}</span>${digest}${architectures}${packageName}</span>`;
}

/**
 * One delegated click handler for every `data-copy-text` button under `root`.
 * It says what happened on the button itself and restores the label.
 */
export function bindCopyButtons(root) {
  if (!root || root.dataset?.copyBound === "1") return () => {};
  if (root.dataset) root.dataset.copyBound = "1";
  let disposed = false;
  const feedback = new Map();
  const onClick = async (event) => {
    const button = event.target?.closest?.("[data-copy-text]");
    if (!button || disposed) return;
    const previous = feedback.get(button);
    if (previous) clearTimeout(previous.timer);
    const attempt = {
      label: previous?.label ?? button.textContent,
      timer: undefined,
    };
    feedback.set(button, attempt);
    let message;
    try {
      await navigator.clipboard.writeText(
        button.getAttribute("data-copy-text") || "",
      );
      message = "Copied";
    } catch {
      message = "Copy failed";
    }
    if (disposed || feedback.get(button) !== attempt) return;
    if (!button.isConnected) {
      feedback.delete(button);
      return;
    }
    button.textContent = message;
    attempt.timer = setTimeout(() => {
      if (feedback.get(button) !== attempt) return;
      if (button.isConnected) button.textContent = attempt.label;
      feedback.delete(button);
    }, 1500);
  };
  root.addEventListener("click", onClick);
  return () => {
    if (disposed) return;
    disposed = true;
    root.removeEventListener("click", onClick);
    if (root.dataset) delete root.dataset.copyBound;
    for (const [button, attempt] of feedback) {
      clearTimeout(attempt.timer);
      if (button.isConnected) button.textContent = attempt.label;
    }
    feedback.clear();
  };
}
