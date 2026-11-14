import assert from "node:assert/strict";
import test from "node:test";
import { resolveDesignTokenAliases } from "./resolve-design-token-aliases.mjs";

test("resolves forward and chained aliases inside compound values without changing literals", () => {
  const input = new Map([
    ["--bp-layer", "linear-gradient(var(--bp-intent) 0 0)"],
    ["--bp-intent", "var(--bp-palette)"],
    ["--bp-palette", "#2d72d2"],
    ["--bp-spacing", "4px"],
  ]);
  const result = resolveDesignTokenAliases(input);
  assert.equal(result.size, input.size);
  assert.equal(result.get("--bp-layer"), "linear-gradient(#2d72d2 0 0)");
  assert.equal(result.get("--bp-intent"), "#2d72d2");
  assert.equal(result.get("--bp-spacing"), "4px");
  assert.equal(input.get("--bp-intent"), "var(--bp-palette)");
  input.set("--bp-palette", "#2d72d3");
  assert.equal(resolveDesignTokenAliases(input).get("--bp-intent"), "#2d72d3");
  assert.notEqual(resolveDesignTokenAliases(input).get("--bp-layer"), result.get("--bp-layer"));
});

test("resolves repeated references and preserves case-sensitive property names", () => {
  const input = new Map([["--bp-a", "var( --bp-b ) var(--bp-b)"], ["--bp-b", "#fff"]]);
  assert.equal(resolveDesignTokenAliases(input).get("--bp-a"), "#fff #fff");
  assert.throws(() => resolveDesignTokenAliases(new Map([["--bp-a", "var(--bp-B)"], ["--bp-b", "#fff"]])), /undefined design token/);
});

test("rejects missing references, self cycles and indirect cycles", () => {
  for (const input of [
    new Map([["--bp-a", "var(--bp-missing)"]]),
    new Map([["--bp-a", "var(--bp-a)"]]),
    new Map([["--bp-a", "var(--bp-b)"], ["--bp-b", "var(--bp-a)"]]),
  ]) assert.throws(() => resolveDesignTokenAliases(input), /undefined design token|cyclic design token/);
});

test("rejects fallback or malformed expressions rather than guessing CSS semantics", () => {
  for (const value of ["var(--bp-missing, #fff)", "var(--bp-a", "var( )", "var(--other-token)"]) {
    assert.throws(() => resolveDesignTokenAliases(new Map([["--bp-a", value]])), /unsupported var\(\) expression/);
  }
});
