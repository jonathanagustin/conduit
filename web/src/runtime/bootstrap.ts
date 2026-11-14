export const BOOTSTRAP_SCHEMA_VERSION = "conduit.console.bootstrap/v1" as const;

const allowedIcons = ["application", "cloud", "dashboard"] as const;
const tenantIDPattern = /^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$/;
const accentPattern = /^#[0-9a-fA-F]{6}$/;

export type ConsoleIcon = (typeof allowedIcons)[number];
export type BootstrapClaimClass = "tenant-observation" | "conduit-extension";

export interface ConsoleBootstrap {
  schemaVersion: typeof BOOTSTRAP_SCHEMA_VERSION;
  tenant: {
    id: string;
    displayName: string;
  };
  brand: {
    title: string;
    subtitle: string;
    accent: string;
    icon: ConsoleIcon;
  };
  provenance: {
    claimClass: BootstrapClaimClass;
    evidenceRef: string;
  };
}

export function parseRuntimeBootstrap(input: unknown): ConsoleBootstrap {
  const root = record(input, "bootstrap");
  exactKeys(
    root,
    ["brand", "provenance", "schemaVersion", "tenant"],
    "bootstrap",
  );
  if (root.schemaVersion !== BOOTSTRAP_SCHEMA_VERSION) {
    throw new Error("unsupported console bootstrap schema");
  }

  const tenant = record(root.tenant, "tenant");
  exactKeys(tenant, ["displayName", "id"], "tenant");
  const tenantID = text(tenant.id, "tenant id", 63);
  if (!tenantIDPattern.test(tenantID)) {
    throw new Error("tenant id is not canonical");
  }

  const brand = record(root.brand, "brand");
  exactKeys(brand, ["accent", "icon", "subtitle", "title"], "brand");
  const accent = text(brand.accent, "brand accent", 7);
  if (!accentPattern.test(accent)) {
    throw new Error("brand accent is not a six-digit hexadecimal color");
  }
  const icon = text(brand.icon, "brand icon", 32);
  if (!(allowedIcons as readonly string[]).includes(icon)) {
    throw new Error("brand icon is not allowed");
  }

  const provenance = record(root.provenance, "provenance");
  exactKeys(provenance, ["claimClass", "evidenceRef"], "provenance");
  const claimClass = text(provenance.claimClass, "claim class", 32);
  if (
    claimClass !== "tenant-observation" &&
    claimClass !== "conduit-extension"
  ) {
    throw new Error("bootstrap claim class is not allowed");
  }
  const evidenceRef = text(provenance.evidenceRef, "evidence reference", 512);
  validateEvidenceRef(
    evidenceRef,
    claimClass === "tenant-observation" ? "evidence:" : "conduit:",
  );

  return {
    schemaVersion: BOOTSTRAP_SCHEMA_VERSION,
    tenant: {
      id: tenantID,
      displayName: text(tenant.displayName, "tenant display name", 80),
    },
    brand: {
      title: text(brand.title, "brand title", 80),
      subtitle: text(brand.subtitle, "brand subtitle", 120),
      accent,
      icon: icon as ConsoleIcon,
    },
    provenance: {
      claimClass,
      evidenceRef,
    },
  };
}

/** The meta the Hub injects into the application shell. Kept in step with
 * BootstrapMetaName in internal/consoleui/bootstrap.go. */
export const BOOTSTRAP_META_NAME = "x-conduit-console-bootstrap";

/** Read the tenant bootstrap the Hub rendered into this document.
 *
 * Synchronous, and it throws rather than degrading. The bundle is
 * tenant-agnostic and content-addressed; everything tenant-specific arrives in
 * the shell the Hub renders around it, so a document without the meta was not
 * served by a Hub -- and a console that carried on regardless would render
 * whatever branding it invented, on a screen whose whole claim is that its
 * branding is evidence-bound.
 *
 * The parsed value goes through parseRuntimeBootstrap unchanged. That validator
 * is a closed surface -- it refuses unknown and missing fields alike, which is
 * what stops tenant data granting a capability -- and it is deliberately not
 * relaxed to accommodate a new transport. */
/** The one thing loadRuntimeBootstrap needs from the document. Narrower than
 * ParentNode on purpose: it keeps the reader testable without a DOM, and it
 * states that reading one meta is the entire coupling to the page. */
export interface BootstrapShell {
  querySelector(
    selectors: string,
  ): { getAttribute(name: string): string | null } | null;
}

export function loadRuntimeBootstrap(
  shell: BootstrapShell = document,
): ConsoleBootstrap {
  const meta = shell.querySelector(`meta[name="${BOOTSTRAP_META_NAME}"]`);
  if (meta === null) {
    throw new Error("the console shell carries no tenant bootstrap");
  }
  const content = meta.getAttribute("content");
  if (content === null || content === "") {
    throw new Error("the console bootstrap meta is empty");
  }
  return parseRuntimeBootstrap(JSON.parse(content));
}

function record(value: unknown, name: string): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${name} must be an object`);
  }
  return value as Record<string, unknown>;
}

function exactKeys(
  value: Record<string, unknown>,
  expected: readonly string[],
  name: string,
): void {
  const actual = Object.keys(value).sort();
  const wanted = [...expected].sort();
  if (
    actual.length !== wanted.length ||
    actual.some((key, index) => key !== wanted[index])
  ) {
    throw new Error(`${name} has unknown or missing fields`);
  }
}

function text(value: unknown, name: string, maximum: number): string {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    value.trim() !== value
  ) {
    throw new Error(`${name} must be a non-empty canonical string`);
  }
  if ([...value].length > maximum) {
    throw new Error(`${name} is too long`);
  }
  const lower = value.toLowerCase();
  if (
    value.includes("<") ||
    value.includes(">") ||
    lower.startsWith("javascript:") ||
    lower.startsWith("data:") ||
    [...value].some((character) => /\p{Cc}/u.test(character))
  ) {
    throw new Error(
      `${name} contains executable markup, a URI scheme, or a control character`,
    );
  }
  return value;
}

function validateEvidenceRef(
  value: string,
  protocol: "evidence:" | "conduit:",
): void {
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("evidence reference is not a URL coordinate");
  }
  if (
    !value.startsWith(`${protocol}//`) ||
    parsed.protocol !== protocol ||
    parsed.host.length === 0 ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.search !== "" ||
    parsed.hash !== "" ||
    parsed.href !== value ||
    /\s/u.test(value)
  ) {
    throw new Error(
      `evidence reference must be a canonical ${protocol}// coordinate`,
    );
  }
}
