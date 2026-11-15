import { describe, expect, it } from "vite-plus/test";

import {
  compareVersions,
  productRows,
  productsMarkup,
} from "../../console/operator/release-products.js";

const products = [
  {
    product: "app.example:sample-app",
    releases: [
      { version: "0.1.0", channels: ["DEV"], chart: { digest: "sha256:a" } },
      {
        version: "0.10.0",
        channels: ["DEV", "CANDIDATE"],
        chart: { digest: "sha256:c" },
      },
      { version: "0.2.0", channels: ["STABLE"], chart: { digest: "sha256:b" } },
    ],
  },
];
const installations = [
  { product: "app.example:sample-app", observedVersion: "0.10.0" },
  { product: "app.example:sample-app", observedVersion: "0.2.0" },
  { product: "other", observedVersion: "1.0.0" },
];

describe("release products", () => {
  it("orders versions numerically", () => {
    expect(compareVersions("0.10.0", "0.2.0")).toBe(1);
    expect(compareVersions("1.0.0", "1.0.0")).toBe(0);
  });

  it("finds the newest release per channel and counts installations on it", () => {
    const [row] = productRows(products, installations);
    expect(row).toMatchObject({
      latest: "0.10.0",
      latestDigest: "sha256:c",
      channels: { DEV: "0.10.0", CANDIDATE: "0.10.0", STABLE: "0.2.0" },
      releases: 3,
      installations: 2,
      onLatest: 1,
    });
  });

  it("renders an empty note and escapes product names", () => {
    expect(
      productsMarkup([], { note: "Hub did not return the product catalog." }),
    ).toContain("Hub did not return the product catalog.");
    const html = productsMarkup(
      productRows([{ product: "<x>", releases: [] }], []),
    );
    expect(html).toContain("&lt;x&gt;");
    expect(html).not.toContain("<x>");
  });
});
