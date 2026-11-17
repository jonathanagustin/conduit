// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vite-plus/test";

import {
  ARTIFACT_KINDS,
  artifactMarkup,
  artifactSummary,
  shortDigest,
} from "../../console/operator/artifact-display.js";
import { planInspectorMarkup } from "../../console/operator/release-plan-inspector.js";
import {
  productRows,
  productsMarkup,
} from "../../console/operator/release-products.js";

const repo = (path: string) =>
  readFileSync(new URL(`../../../../${path}`, import.meta.url), "utf8");

const digest = `sha256:${"ab12".repeat(16)}`;

// #833: artifact kind, digest and architectures
// ---------------------------------------------------------------------------

describe("artifact display", () => {
  it("names the kinds the Hub declares", () => {
    const go = repo("internal/catalog/domain/release.go");
    const kinds = [...go.matchAll(/ArtifactKind\w+\s+= "([a-z-]+)"/g)].map(
      (match) => match[1]!,
    );
    expect(kinds.sort()).toEqual(Object.keys(ARTIFACT_KINDS).sort());
  });

  it("treats an absent kind as a Helm chart", () => {
    expect(
      artifactSummary({ repository: "r/api", tag: "1.0.0", digest }),
    ).toEqual({
      kind: "helm-chart",
      kindLabel: "Helm chart",
      digest,
      shortDigest: "sha256:ab12ab12ab12…",
      architectures: [],
      packageName: "",
    });
  });

  it("reads kind, architectures and package name as the Hub returns them", () => {
    expect(
      artifactSummary({
        digest,
        kind: "zarf-package",
        architectures: ["amd64", "arm64"],
        packageName: "podinfo",
      }),
    ).toMatchObject({
      kind: "zarf-package",
      kindLabel: "Zarf package",
      architectures: ["amd64", "arm64"],
      packageName: "podinfo",
    });
    expect(artifactSummary({ digest, kind: "uds-bundle" })?.kindLabel).toBe(
      "UDS bundle",
    );
  });

  it("shows a kind it does not know as the Hub wrote it", () => {
    expect(artifactSummary({ digest, kind: "oci-thing" })?.kindLabel).toBe(
      "oci-thing",
    );
  });

  it("shows nothing when the Hub returned no artifact", () => {
    expect(artifactSummary(undefined)).toBeNull();
    expect(artifactSummary({})).toBeNull();
    expect(artifactSummary({ repository: "r/api", tag: "1" })).toBeNull();
    expect(artifactMarkup(null)).toBe("");
  });

  it("shortens a digest and leaves anything else alone", () => {
    expect(shortDigest(digest)).toBe("sha256:ab12ab12ab12…");
    expect(shortDigest("sha256:c")).toBe("sha256:c");
    expect(shortDigest("")).toBe("");
  });

  it("renders the short digest with the full one on hover and behind Copy", () => {
    const html = artifactMarkup({
      digest,
      kind: "uds-bundle",
      architectures: ["arm64"],
    });
    expect(html).toContain('data-artifact-kind="uds-bundle"');
    expect(html).toContain(">UDS bundle<");
    expect(html).toContain(`title="${digest}">sha256:ab12ab12ab12…<`);
    expect(html).toContain(`data-copy-text="${digest}"`);
    expect(html).toMatch(/data-artifact-architectures[^>]*>arm64</);
  });

  it("escapes what the Hub returned", () => {
    const html = artifactMarkup({ kind: "<b>", packageName: '"x"' });
    expect(html).not.toContain("<b>");
    expect(html).toContain("&lt;b&gt;");
    expect(html).toContain("&quot;x&quot;");
  });

  it("shows each plan step's artifact in the plan inspector", () => {
    const step = (position: number, chart?: unknown) => ({
      position,
      action: "apply",
      installationId: `api-${position}`,
      product: "service",
      version: "1",
      ...(chart ? { chart } : {}),
    });
    const html = planInspectorMarkup([
      {
        id: "plan-a",
        status: "AWAITING_APPROVAL",
        plan: {
          environment: "prod",
          steps: [
            step(1, { digest, kind: "zarf-package", architectures: ["amd64"] }),
            step(2, { digest }),
            step(3),
          ],
        },
      },
    ]);
    const items = html.match(/<li>[\s\S]*?<\/li>/g) ?? [];
    expect(items).toHaveLength(3);
    expect(items[0]).toContain("Zarf package");
    expect(items[0]).toContain("amd64");
    expect(items[0]).toContain(`title="${digest}"`);
    expect(items[1]).toContain("Helm chart");
    expect(items[2]).not.toContain("data-artifact");
  });

  it("shows the newest release's artifact in the products table", () => {
    const rows = productRows(
      [
        {
          product: "podinfo",
          releases: [
            { version: "1.0.0", chart: { digest: "sha256:c" } },
            {
              version: "2.0.0",
              chart: { digest, kind: "uds-bundle", architectures: ["arm64"] },
            },
          ],
        },
        { product: "bare", releases: [] },
      ],
      [],
    );
    const html = productsMarkup(rows);
    expect(html).toContain("<th>Artifact</th>");
    expect(html).toContain('data-artifact-kind="uds-bundle"');
    expect(html).toContain("arm64");
    expect((html.match(/data-artifact-kind/g) ?? []).length).toBe(1);
  });
});

// ---------------------------------------------------------------------------
