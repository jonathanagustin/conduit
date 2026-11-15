// @vitest-environment node
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vite-plus/test";

const css = readFileSync(
  join(import.meta.dirname, "../../console/operator/tokens.css"),
  "utf8",
);

function block(selector: string): Record<string, string> {
  const start = css.indexOf(`${selector} {`);
  expect(start, selector).toBeGreaterThanOrEqual(0);
  const body = css.slice(start, css.indexOf("}", start));
  const tokens: Record<string, string> = {};
  for (const match of body.matchAll(/(--[a-z0-9-]+):\s*(#[0-9a-f]{6});/gi)) {
    tokens[match[1]] = match[2].toLowerCase();
  }
  return tokens;
}

function luminance(hex: string): number {
  const channel = (offset: number) => {
    const value = parseInt(hex.slice(offset, offset + 2), 16) / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5);
}

function ratio(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

const dark = block(".operator-root");
const light = { ...dark, ...block('.operator-root[data-theme="light"]') };
const SURFACES = ["--bg-0", "--bg-1", "--bg-2", "--bg-3"];
const STATUS = ["ok", "warn", "err", "info", "ai", "neutral"];

describe.each([
  ["dark", dark],
  ["light", light],
])("%s token sheet", (_name, t) => {
  it("keeps body and secondary text at 4.5:1 on every surface", () => {
    for (const surface of SURFACES) {
      expect(ratio(t["--text-1"], t[surface])).toBeGreaterThanOrEqual(4.5);
      expect(ratio(t["--text-2"], t[surface])).toBeGreaterThanOrEqual(4.5);
    }
  });

  it("keeps control boundaries and focus at 3:1", () => {
    for (const surface of ["--bg-1", "--bg-2"]) {
      expect(ratio(t["--border-control"], t[surface])).toBeGreaterThanOrEqual(
        3,
      );
      expect(ratio(t["--border-strong"], t[surface])).toBeGreaterThanOrEqual(3);
    }
    expect(ratio(t["--accent-focus"], t["--bg-1"])).toBeGreaterThanOrEqual(3);
    expect(ratio(t["--accent-sel-border"], t["--bg-1"])).toBeGreaterThanOrEqual(
      3,
    );
  });

  it("keeps the primary action label at 4.5:1", () => {
    for (const fill of ["--accent-fill", "--accent-hover", "--accent-active"]) {
      expect(ratio(t["--accent-text"], t[fill])).toBeGreaterThanOrEqual(4.5);
    }
    expect(ratio(t["--text-1"], t["--accent-sel-bg"])).toBeGreaterThanOrEqual(
      4.5,
    );
  });

  it("keeps status text at 4.5:1 and status borders at 3:1", () => {
    for (const tone of STATUS) {
      const fill = t[`--${tone}-fill`];
      expect(ratio(t[`--${tone}-text`], fill), tone).toBeGreaterThanOrEqual(
        4.5,
      );
      expect(
        ratio(t[`--${tone}-text`], t["--bg-1"]),
        tone,
      ).toBeGreaterThanOrEqual(4.5);
      expect(ratio(t[`--${tone}-border`], fill), tone).toBeGreaterThanOrEqual(
        3,
      );
      expect(
        ratio(t[`--${tone}-border`], t["--bg-1"]),
        tone,
      ).toBeGreaterThanOrEqual(3);
    }
  });

  it("avoids pure white body text in the dark theme", () => {
    if (t === dark) expect(t["--text-1"]).not.toBe("#ffffff");
  });
});
