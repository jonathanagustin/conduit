// A named workaround for Chromium's zygote under the local QEMU runner. Keep
// launch arguments closed: this must never become an arbitrary flags channel.
export function localCiBrowserLaunchOptions(
  environment = process.env,
  runtime = { platform: process.platform, arch: process.arch },
) {
  const profile = environment["CONDUIT_LOCAL_CI_BROWSER_PROFILE"];
  if (profile === undefined || profile === "") return undefined;
  if (profile !== "qemu-x86_64-no-zygote") {
    throw new Error(`Unknown local CI browser profile: ${profile}`);
  }
  if (
    environment["ACT"] !== "true" ||
    runtime.platform !== "linux" ||
    runtime.arch !== "x64"
  ) {
    throw new Error("qemu-x86_64-no-zygote requires local act on Linux x86_64");
  }
  return { args: ["--no-zygote"] };
}
