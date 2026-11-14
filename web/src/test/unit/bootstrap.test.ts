import { describe, expect, it } from "vitest";

import tenantA from "../../../../testdata/console-bootstrap/tenant-a.json";
import tenantB from "../../../../testdata/console-bootstrap/tenant-b.json";
import {
  BOOTSTRAP_META_NAME,
  type BootstrapShell,
  loadRuntimeBootstrap,
  parseRuntimeBootstrap,
} from "../../runtime/bootstrap.js";

describe("tenant runtime bootstrap", () => {
  it("accepts two evidence-bound tenants through the same closed shape", () => {
    const alpha = parseRuntimeBootstrap(tenantA);
    const bravo = parseRuntimeBootstrap(tenantB);

    expect(alpha.tenant.id).toBe("synthetic-alpha");
    expect(bravo.tenant.id).toBe("synthetic-bravo");
    expect(alpha.brand).not.toEqual(bravo.brand);
    expect(Object.keys(alpha).sort()).toEqual([
      "brand",
      "provenance",
      "schemaVersion",
      "tenant",
    ]);
    expect(Object.keys(alpha.brand).sort()).toEqual([
      "accent",
      "icon",
      "subtitle",
      "title",
    ]);
    expect(JSON.stringify(alpha)).not.toMatch(
      /capabil|auth|script|csp|apiBase|feature/i,
    );
    expect(JSON.stringify(bravo)).not.toMatch(
      /capabil|auth|script|csp|apiBase|feature/i,
    );
  });

  it.each([
    ["top-level capabilities", { ...tenantA, capabilities: ["admin"] }],
    ["API origin", { ...tenantA, apiBaseUrl: "https://example.invalid" }],
    ["CSP", { ...tenantA, csp: "default-src *" }],
    ["authentication", { ...tenantA, authMode: "disabled" }],
    ["script", { ...tenantA, brand: { ...tenantA.brand, script: "alert(1)" } }],
    [
      "executable title",
      { ...tenantA, brand: { ...tenantA.brand, title: "javascript:alert(1)" } },
    ],
    [
      "unproven evidence",
      {
        ...tenantA,
        provenance: {
          ...tenantA.provenance,
          evidenceRef: "conduit://console/default",
        },
      },
    ],
  ])("rejects %s fields or values", (_name, document) => {
    expect(() => parseRuntimeBootstrap(document)).toThrow();
  });

  // The bootstrap arrives in the document the Hub rendered, not from a request
  // the console makes. These cover the transport; parseRuntimeBootstrap's own
  // refusals are covered above and are deliberately unchanged by the move.
  const shellWith = (content: string | null): BootstrapShell => ({
    querySelector: (selectors: string) =>
      selectors === `meta[name="${BOOTSTRAP_META_NAME}"]` && content !== null
        ? { getAttribute: () => content }
        : null,
  });

  it("reads the tenant bootstrap the Hub rendered into the shell", () => {
    expect(loadRuntimeBootstrap(shellWith(JSON.stringify(tenantA)))).toEqual(
      parseRuntimeBootstrap(tenantA),
    );
  });

  // A document without the meta was not served by a Hub. Refusing is the whole
  // point: carrying on would render invented branding on a screen whose claim
  // is that its branding is evidence-bound.
  it.each([
    ["no meta at all", null],
    ["an empty meta", ""],
    ["a meta that is not JSON", "not json"],
  ])("refuses %s", (_name, content) => {
    expect(() => loadRuntimeBootstrap(shellWith(content))).toThrow();
  });

  // The closed surface has to survive the change of transport. This is the
  // property that makes tenant data unable to grant a capability, and reading
  // from a meta makes "ignore the extra" the easy mistake.
  it("still refuses a document carrying an unknown field", () => {
    const withCapability = JSON.stringify({
      ...tenantA,
      capabilities: ["admin"],
    });
    expect(() => loadRuntimeBootstrap(shellWith(withCapability))).toThrow();
  });

  // Escaping is the Hub's job, and a quote in a tenant name is legal today --
  // the validator bans < and > but not ". If the attribute round-trips wrong
  // the console must fail rather than render a half-parsed document.
  it("round-trips a tenant name containing a double quote", () => {
    const quoted = {
      ...tenantA,
      tenant: { ...tenantA.tenant, displayName: 'Alpha "Ops' },
    };
    expect(
      loadRuntimeBootstrap(shellWith(JSON.stringify(quoted))).tenant
        .displayName,
    ).toBe('Alpha "Ops');
  });
});
