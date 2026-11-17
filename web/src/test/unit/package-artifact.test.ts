import { describe, expect, it } from "vite-plus/test";
import { artifactMarkup } from "../../console/operator/package-artifact.js";
import { planInspectorMarkup } from "../../console/operator/release-plan-inspector.js";
import {
  productRows,
  productsMarkup,
} from "../../console/operator/release-products.js";

const artifact = {
  kind: "zarf-package",
  repository: "registry.example/packages/service",
  digest: `sha256:${"a".repeat(64)}`,
  architectures: ["arm64"],
  packageName: "service",
};

describe("declared package artifacts", () => {
  it("shows the complete approved coordinates in the Plan inspector", () => {
    for (const kind of ["zarf-package", "uds-bundle"]) {
      const html = planInspectorMarkup([
        {
          id: "plan-1",
          status: "PENDING_APPROVAL",
          plan: {
            steps: [{ chart: { ...artifact, kind }, product: "service" }],
          },
        },
      ]);
      for (const value of [
        kind,
        artifact.repository,
        artifact.digest,
        artifact.architectures[0],
        "Package name",
        "Declared artifact",
      ])
        expect(html).toContain(value);
      expect(html).toContain("Instructions, not per-step execution receipts");
    }
  });

  it("shows the newest release artifact rather than an older catalog entry", () => {
    const rows = productRows(
      [
        {
          product: "service",
          releases: [
            { version: "1.0.0", chart: { digest: "old-artifact" } },
            { version: "2.0.0", chart: artifact },
          ],
        },
      ],
      [],
    );
    const html = productsMarkup(rows);
    expect(html).toContain(artifact.repository);
    expect(html).toContain(artifact.digest);
    expect(html).toContain("arm64");
    expect(html).toContain("zarf-package");
    expect(html).not.toContain("old-artifact");
  });

  it("preserves the legacy Helm default and distinguishes absent evidence", () => {
    expect(artifactMarkup({ repository: "charts.example/api" })).toContain(
      "helm-chart",
    );
    expect(artifactMarkup(null)).toContain("Artifact unavailable");
    expect(artifactMarkup(null)).not.toContain("helm-chart");
    expect(artifactMarkup({ kind: "uds-bundle" })).toContain("unavailable");
  });

  it("escapes every artifact field without constructing executable links", () => {
    const html = artifactMarkup({
      ...artifact,
      repository: '<img src=x onerror="alert(1)">',
      digest: "<digest>",
      architectures: ["<architecture>"],
      packageName: "<package>",
      tag: "<tag>",
    });
    for (const value of ["digest", "architecture", "package", "tag"])
      expect(html).toContain(`&lt;${value}&gt;`);
    expect(html).toContain("&lt;img");
    expect(html).not.toContain("<img");
    expect(html).not.toContain("href=");
  });
});
