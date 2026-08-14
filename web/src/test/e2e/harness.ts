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

export interface HubStub {
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
    applications: [
      { id: "change-requests", enabled: true },
      { id: "fleet", enabled: true },
      { id: "plans", enabled: true },
      { id: "promotions", enabled: true },
    ],
    globalOperations: ["resource:read"],
    resources: [],
  });
  return install.then(() => api);
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
