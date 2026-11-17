// @vitest-environment node
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";
import { AppShell, ConduitMark } from "../../console/components/base/index.tsx";
import { brandMarkMarkup } from "../../console/operator/kit.js";

const WEB = join(import.meta.dirname, "../../..");
const read = (path: string) => readFileSync(join(WEB, path), "utf8");

describe("Conduit brand mark", () => {
  it("renders three Obsidian Sapphire chevrons at a requested size", () => {
    const svg = brandMarkMarkup();
    for (const token of [
      'class="cnd-brand-mark"',
      'width="20"',
      'class="cnd-mark-c1"',
      'class="cnd-mark-c2"',
      'class="cnd-mark-c3"',
      'stroke="#215db0"',
      'stroke="#4c90f0"',
      'stroke="#8abbff"',
    ]) {
      expect(svg).toContain(token);
    }
    expect(brandMarkMarkup(32)).toContain('width="32"');
  });

  it("puts the mark beside the wordmark in AppShell", () => {
    expect(
      renderToStaticMarkup(React.createElement(ConduitMark, { size: 24 })),
    ).toContain('width="24"');
    const html = renderToStaticMarkup(
      React.createElement(AppShell, {
        kpi: React.createElement("div", null, "kpi"),
        sidebar: React.createElement("nav", null, "nav"),
        rail: React.createElement("aside", null, "rail"),
        children: React.createElement("main", null, "content"),
      }),
    );
    expect(html).toContain('class="cd-wordmark"');
    expect(html).toContain('class="cnd-mark-c1"');
    expect(html).toContain("Conduit");
  });

  it("flows colour rightward and honours reduced motion", () => {
    for (const path of [
      "src/console/operator/kit.css",
      "src/console/theme/tokens.css",
    ]) {
      const css = read(path);
      expect(css).toContain("@keyframes cnd-sapphire-flow");
      expect(css).toContain("-1.6s");
      expect(css).toContain("-0.8s");
      expect(css).toContain("@media (prefers-reduced-motion: reduce)");
    }
  });

  it("ships a matching animated squircle favicon linked from the shell", () => {
    expect(existsSync(join(WEB, "public/favicon.svg"))).toBe(true);
    const favicon = read("public/favicon.svg");
    expect(favicon).toContain("<svg");
    expect(favicon).toContain("#1c2127");
    expect(favicon).toContain("#0b1113");
    expect(favicon).toContain('values="#215db0;#8abbff;#4c90f0;#215db0"');
    expect(favicon).not.toContain("<style");
    expect(read("index.html")).toContain(
      '<link rel="icon" type="image/svg+xml" href="/favicon.svg" />',
    );
    expect(read("index.html")).toContain("<!--conduit-console-bootstrap-->");
  });
});
