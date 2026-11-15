import assert from "node:assert/strict";
import test from "node:test";
import { verifyToolchain } from "./check-toolchain.mjs";

const matched = {
  core: "@voidzero-dev/vite-plus-core@1.6.0",
  expectedCore: "@voidzero-dev/vite-plus-core@1.6.0",
  vitest: "5.1.0",
  expectedVitest: "5.1.0",
  sameCore: true,
  sameVitest: true,
  packageManager: "pnpm@12.18.0",
  pnpmPin: "12.18.0",
};

await test("accepts the matched bundle and pinned package manager", () => {
  assert.doesNotThrow(() => verifyToolchain(matched));
});

await test("rejects a core updated independently of Vite+", () => {
  assert.throws(
    () =>
      verifyToolchain({
        ...matched,
        core: "@voidzero-dev/vite-plus-core@8.3.8000",
      }),
    /core must match/,
  );
});

await test("rejects a separate Vitest version or runner instance", () => {
  assert.throws(
    () => verifyToolchain({ ...matched, vitest: "5.2.0" }),
    /Vitest must match/,
  );
  assert.throws(
    () => verifyToolchain({ ...matched, sameVitest: false }),
    /same Vitest instance/,
  );
});

await test("rejects automatic package-manager drift", () => {
  assert.throws(
    () => verifyToolchain({ ...matched, packageManager: "pnpm@12.21.0" }),
    /match mise/,
  );
});
