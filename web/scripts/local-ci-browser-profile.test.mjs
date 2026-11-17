import assert from "node:assert/strict";
import test from "node:test";
import { localCiBrowserLaunchOptions } from "./local-ci-browser-profile.mjs";

const linuxX64 = { platform: "linux", arch: "x64" };
const selected = {
  ACT: "true",
  CONDUIT_LOCAL_CI_BROWSER_PROFILE: "qemu-x86_64-no-zygote",
};

test("ordinary hosted and developer launches retain Playwright defaults", () => {
  for (const environment of [{}, { CI: "true" }, { ACT: "true" }]) {
    assert.equal(localCiBrowserLaunchOptions(environment, linuxX64), undefined);
  }
  assert.equal(
    localCiBrowserLaunchOptions(
      { ...selected, CONDUIT_LOCAL_CI_BROWSER_PROFILE: "" },
      linuxX64,
    ),
    undefined,
  );
});

test("the explicit local profile selects exactly one Chromium argument", () => {
  assert.deepEqual(localCiBrowserLaunchOptions(selected, linuxX64), {
    args: ["--no-zygote"],
  });
});

test("unknown profiles and arbitrary argument strings fail closed", () => {
  for (const profile of [
    "default",
    "--single-process",
    "qemu-x86_64-no-zygote --disable-gpu",
    "qemu-x86_64-no-zygote\n--no-sandbox",
  ]) {
    assert.throws(
      () =>
        localCiBrowserLaunchOptions(
          { ...selected, CONDUIT_LOCAL_CI_BROWSER_PROFILE: profile },
          linuxX64,
        ),
      /Unknown local CI browser profile/,
    );
  }
});

test("the opt-in rejects hosted and non-x86 Linux runtimes", () => {
  for (const ACT of [undefined, "false", "1", "TRUE"]) {
    assert.throws(
      () => localCiBrowserLaunchOptions({ ...selected, ACT }, linuxX64),
      /requires local act on Linux x86_64/,
    );
  }
  for (const runtime of [
    { platform: "darwin", arch: "x64" },
    { platform: "darwin", arch: "arm64" },
    { platform: "linux", arch: "arm64" },
    { platform: "win32", arch: "x64" },
  ]) {
    assert.throws(
      () => localCiBrowserLaunchOptions(selected, runtime),
      /requires local act on Linux x86_64/,
    );
  }
});
