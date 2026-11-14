// Resolve the closed token graph before notation normalization. This supports
// simple CSS var(--bp-name) substitution, including aliases inside gradients;
// it is not a CSS cascade or fallback evaluator. Unknown references, cycles and
// unsupported var() expressions fail rather than becoming matching literals.
export function resolveDesignTokenAliases(tokens) {
  const resolved = new Map();
  const active = new Set();
  function resolve(name) {
    if (resolved.has(name)) return resolved.get(name);
    if (!tokens.has(name)) throw new Error(`undefined design token ${name}`);
    if (active.has(name)) throw new Error(`cyclic design token alias ${name}`);
    active.add(name);
    const value = tokens.get(name).replace(/var\(\s*(--bp-[a-z0-9-]+)\s*\)/gi, (_, target) => resolve(target));
    if (/\bvar\s*\(/i.test(value)) throw new Error(`unsupported var() expression in ${name}`);
    active.delete(name);
    resolved.set(name, value);
    return value;
  }
  for (const name of tokens.keys()) resolve(name);
  return resolved;
}
