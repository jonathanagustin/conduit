const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );

// These are the declared coordinates, never evidence that a package deployed.
// An omitted kind on an existing artifact is the native Helm default.
export function artifactMarkup(artifact) {
  if (!artifact || typeof artifact !== "object" || Array.isArray(artifact))
    return '<p class="kit-muted">Artifact unavailable</p>';
  const kind = artifact.kind || "helm-chart";
  const packaged = kind === "zarf-package" || kind === "uds-bundle";
  const architectures = Array.isArray(artifact.architectures)
    ? artifact.architectures.join(", ")
    : "";
  return `<dl class="text-xs space-y-1 break-all" data-declared-artifact>
    <dt>Declared artifact</dt><dd>${esc(kind)}</dd>
    <dt>Repository</dt><dd class="code-id">${esc(artifact.repository || "unavailable")}</dd>
    <dt>Artifact digest</dt><dd class="code-id">${esc(artifact.digest || "unavailable")}</dd>
    ${artifact.tag ? `<dt>Tag</dt><dd>${esc(artifact.tag)}</dd>` : ""}
    ${packaged ? `<dt>Architecture</dt><dd>${esc(architectures || "unavailable")}</dd><dt>Package name</dt><dd>${esc(artifact.packageName || "unavailable")}</dd>` : ""}
  </dl>`;
}
