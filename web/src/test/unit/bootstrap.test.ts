import { describe, expect, it, vi } from "vitest";

import tenantA from "../../../../testdata/console-bootstrap/tenant-a.json";
import tenantB from "../../../../testdata/console-bootstrap/tenant-b.json";
import { loadRuntimeBootstrap, parseRuntimeBootstrap } from "../../runtime/bootstrap.js";

describe("tenant runtime bootstrap", () => {
  it("accepts two evidence-bound tenants through the same closed shape", () => {
    const alpha = parseRuntimeBootstrap(tenantA);
    const bravo = parseRuntimeBootstrap(tenantB);

    expect(alpha.tenant.id).toBe("synthetic-alpha");
    expect(bravo.tenant.id).toBe("synthetic-bravo");
    expect(alpha.brand).not.toEqual(bravo.brand);
    expect(Object.keys(alpha).sort()).toEqual(["brand", "provenance", "schemaVersion", "tenant"]);
    expect(Object.keys(alpha.brand).sort()).toEqual(["accent", "icon", "subtitle", "title"]);
    expect(JSON.stringify(alpha)).not.toMatch(/capabil|auth|script|csp|apiBase|feature/i);
    expect(JSON.stringify(bravo)).not.toMatch(/capabil|auth|script|csp|apiBase|feature/i);
  });

  it.each([
    ["top-level capabilities", { ...tenantA, capabilities: ["admin"] }],
    ["API origin", { ...tenantA, apiBaseUrl: "https://example.invalid" }],
    ["CSP", { ...tenantA, csp: "default-src *" }],
    ["authentication", { ...tenantA, authMode: "disabled" }],
    ["script", { ...tenantA, brand: { ...tenantA.brand, script: "alert(1)" } }],
    ["executable title", { ...tenantA, brand: { ...tenantA.brand, title: "javascript:alert(1)" } }],
    ["unproven evidence", { ...tenantA, provenance: { ...tenantA.provenance, evidenceRef: "conduit://console/default" } }],
  ])("rejects %s fields or values", (_name, document) => {
    expect(() => parseRuntimeBootstrap(document)).toThrow();
  });

  it("fetches only the fixed same-origin bootstrap route", async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify(tenantA), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    await expect(loadRuntimeBootstrap(fetcher)).resolves.toEqual(parseRuntimeBootstrap(tenantA));
    expect(fetcher).toHaveBeenCalledWith("/console/bootstrap.json", {
      method: "GET",
      headers: { Accept: "application/json" },
      credentials: "same-origin",
      cache: "no-store",
    });
  });
});
