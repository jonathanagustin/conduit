import { test as base, type Page, type Route } from "@playwright/test";

import { projection, MIXED_FLEET } from "../fixtures.js";
import type { FleetInstallation, FleetProjection } from "../../api/types.js";

/** Browser helpers.
 *
 * Every test intercepts /v1 rather than running a Hub. That is a deliberate
 * limit: these tests establish that the console renders and behaves correctly
 * given a response, not that the Hub produces that response. The transport
 * types are checked against the Go structs at the type level, and the Hub's own
 * behaviour has its own suite. What this cannot catch is a field the Hub
 * renamed -- so a fixture must be corrected from the Go source, never from what
 * makes a test pass. */

/** The Conduit-native default, transcribed from consoleui.DefaultBootstrap in
 * internal/consoleui/bootstrap.go. The Hub renders this into the shell when no
 * tenant file is configured, so it is what an unconfigured deployment serves --
 * and correcting it belongs in the Go source, never here. */
/** Kept in step with bootstrapMarker in internal/consoleui/handler.go. */
const BOOTSTRAP_MARKER = "<!--conduit-console-bootstrap-->";
const BOOTSTRAP_META_NAME = "x-conduit-console-bootstrap";

/** The same escaping the Hub applies with html.EscapeString. Present so a
 * tenant name containing a quote is exercised here the way it will be served,
 * rather than only in Go. */
function escapeAttribute(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&#34;")
    .replaceAll("'", "&#39;");
}

export const DEFAULT_CONSOLE_BOOTSTRAP = {
  schemaVersion: "conduit.console.bootstrap/v1",
  tenant: { id: "conduit", displayName: "Conduit" },
  brand: { title: "Conduit", subtitle: "Control plane", accent: "#2d72d2", icon: "dashboard" },
  provenance: { claimClass: "conduit-extension", evidenceRef: "conduit://console/default" },
} as const;

export interface HubStub {
  /** Render a tenant bootstrap into the application shell, the way the Hub
   * does. `vite preview` serves the built shell with its marker unrendered, so
   * without this the console refuses to start -- which is the correct
   * behaviour, and why the stub has to do the Hub's job rather than skip it. */
  tenant(document: unknown): void;
  /** Serve a body for any GET whose path ends with the given suffix. */
  get(pathSuffix: string, body: unknown, options?: { cursor?: string }): void;
  /** Fail any GET matching the suffix with a status and Hub-shaped error body. */
  fails(pathSuffix: string, status: number, error: string, options?: { cursor?: string }): void;
  /** Serve a body for a POST, and record that it happened. */
  post(pathSuffix: string, body: unknown): void;
  /** Convenience for the fleet endpoint, which needs its summary counted. */
  fleet(
    installations: FleetInstallation[],
    pagination?: Partial<Pick<FleetProjection, "nextCursor" | "previousCursor" | "hasPrevious" | "limit">> & {
      cursor?: string;
      total?: number;
    },
  ): void;
  /** Every request the console made to /v1, in order, as "METHOD path?query". */
  requests(): string[];
  /** The parsed JSON body of each POST, in order. Recording only method and
   * path is how a wrong request body survives a passing test. */
  bodies(): unknown[];
}

interface Stubbed {
  suffix: string;
  method: "GET" | "POST";
  status: number;
  body: unknown;
  cursor?: string;
}

function stub(page: Page): Promise<HubStub> {
  const seen: string[] = [];
  const sent: unknown[] = [];
  const stubs: Stubbed[] = [];
  let tenant: unknown = DEFAULT_CONSOLE_BOOTSTRAP;

  // The Hub renders the tenant bootstrap into the shell before serving it.
  // `vite preview` serves the built shell with its marker unrendered, so this
  // does the Hub's job -- skipping it would leave every test running against a
  // console that correctly refuses to start.
  const hydrate = page.route("**/console/**", async (route: Route) => {
    if (route.request().resourceType() !== "document") {
      await route.fallback();
      return;
    }
    const response = await route.fetch();
    const meta = `<meta name="${BOOTSTRAP_META_NAME}" content="${escapeAttribute(JSON.stringify(tenant))}" />`;
    await route.fulfill({
      response,
      body: (await response.text()).replace(BOOTSTRAP_MARKER, meta),
    });
  });

  const install = page.route("**/v1/**", async (route: Route) => {
    const requested = new URL(route.request().url());
    const method = route.request().method() as "GET" | "POST";
    // Path segments are percent-encoded by the client -- a Product coordinate
    // carries a colon -- so match on the decoded path. Stubs are written the way
    // the endpoint reads in http.go, not the way the URL is escaped.
    const path = decodeURIComponent(requested.pathname);
    seen.push(`${method} ${path}${requested.search}`);
    if (method === "POST") {
      const raw = route.request().postData();
      sent.push(raw === null ? null : JSON.parse(raw));
    }
    // Last registration wins, so a test can override a default set in a helper.
    const requestedCursor = requested.searchParams.get("cursor") ?? undefined;
    const match = [...stubs]
      .reverse()
      .find(
        (entry) =>
          entry.method === method &&
          path.endsWith(entry.suffix) &&
          entry.cursor === requestedCursor,
      );
    if (match === undefined) {
      // An unstubbed call is a test defect, not a 404: failing loudly here stops
      // a screen quietly rendering an empty state that nobody asked for.
      await route.fulfill({
        status: 501,
        json: { error: `no stub for ${method} ${path}` },
      });
      return;
    }
    await route.fulfill({ status: match.status, json: match.body });
  });

  const api: HubStub = {
    tenant(document) {
      tenant = document;
    },
    get(suffix, body, options = {}) {
      stubs.push({ suffix, method: "GET", status: 200, body, ...options });
    },
    fails(suffix, status, error, options = {}) {
      stubs.push({ suffix, method: "GET", status, body: { error }, ...options });
    },
    post(suffix, body) {
      stubs.push({ suffix, method: "POST", status: 200, body });
    },
    fleet(installations, pagination = {}) {
      const { cursor, ...response } = pagination;
      stubs.push({
        suffix: "/fleet/installations",
        method: "GET",
        status: 200,
        body: projection(installations, response),
        ...(cursor === undefined ? {} : { cursor }),
      });
    },
    requests: () => seen,
    bodies: () => sent,
  };
  api.fleet(MIXED_FLEET);
  api.get("/session", {
    actor: {
      username: "browser-operator",
      teams: [],
      teamsTruncated: false,
      serviceAccount: false,
    },
    effective: {
      username: "browser-operator",
      teams: [],
      teamsTruncated: false,
      serviceAccount: false,
    },
    delegated: false,
    // All eight applications the Hub's session projection serves
    // (internal/controlplane/session.go), enabled as they are for a principal
    // with resource:read at platform/root. The stub used to carry the first
    // four, which is the vocabulary an older projection had: the menu under
    // test was then a strict subset of the menu production renders, so Export,
    // Import, Risk Management and Teams appeared in no browser test at all.
    applications: [
      { id: "change-requests", enabled: true },
      { id: "fleet", enabled: true },
      { id: "plans", enabled: true },
      { id: "promotions", enabled: true },
      { id: "teams", enabled: true },
      { id: "risk", enabled: true },
      { id: "export", enabled: true },
      { id: "import", enabled: true },
    ],
    globalOperations: ["resource:read"],
    resources: [],
  });
  return Promise.all([install, hydrate]).then(() => api);
}

export const test = base.extend<{ hub: HubStub }>({
  hub: async ({ page }, use) => {
    await use(await stub(page));
  },
});

export { expect } from "@playwright/test";

export function indexPage<T>(
  key: "changeRequests" | "promotions",
  values: T[],
  pagination: Partial<{
    total: number;
    pageSize: number;
    limit: number;
    hasPrevious: boolean;
    nextCursor: string;
    previousCursor: string;
  }> = {},
) {
  return {
    [key]: values,
    total: values.length,
    pageSize: values.length,
    limit: 100,
    hasPrevious: false,
    ...pagination,
  };
}
