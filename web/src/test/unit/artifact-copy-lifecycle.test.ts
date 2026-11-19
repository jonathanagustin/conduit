// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bindCopyButtons } from "../../console/operator/artifact-display.js";

function fixture() {
  const handlers = new Map<string, (event: unknown) => Promise<void>>();
  const root = {
    dataset: {} as Record<string, string>,
    addEventListener(name: string, fn: (event: unknown) => Promise<void>) {
      handlers.set(name, fn);
    },
    removeEventListener(name: string, fn: (event: unknown) => Promise<void>) {
      if (handlers.get(name) === fn) handlers.delete(name);
    },
  };
  const button = {
    textContent: "Copy",
    isConnected: true,
    getAttribute: () => "sha256:reviewed",
  };
  const clipboard = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", { clipboard: { writeText: clipboard } });
  vi.useFakeTimers();
  const dispose = bindCopyButtons(root);
  const click = () =>
    handlers.get("click")!({ target: { closest: () => button } });
  return { root, button, clipboard, handlers, dispose, click };
}

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("artifact copy feedback lifecycle", () => {
  it("restores the original label after repeated clicks during feedback", async () => {
    const f = fixture();
    await f.click();
    expect(f.button.textContent).toBe("Copied");
    await vi.advanceTimersByTimeAsync(500);
    await f.click();
    await vi.advanceTimersByTimeAsync(1500);
    expect(f.clipboard.mock.calls).toEqual([
      ["sha256:reviewed"],
      ["sha256:reviewed"],
    ]);
    expect(f.button.textContent).toBe("Copy");
  });

  it("ignores an older clipboard failure after the newer attempt succeeds", async () => {
    const f = fixture();
    let rejectOld!: (error: Error) => void;
    f.clipboard.mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectOld = reject;
        }),
    );
    const old = f.click();
    await f.click();
    rejectOld(new Error("older write failed"));
    await old;
    expect(f.button.textContent).toBe("Copied");
    await vi.advanceTimersByTimeAsync(1500);
    expect(f.button.textContent).toBe("Copy");
  });

  it("removes delegated listeners and feedback timers on unmount", async () => {
    const f = fixture();
    await f.click();
    expect(vi.getTimerCount()).toBe(1);
    f.dispose();
    expect(vi.getTimerCount()).toBe(0);
    expect(f.handlers.size).toBe(0);
    expect(f.root.dataset.copyBound).toBeUndefined();
    expect(f.button.textContent).toBe("Copy");
    const again = bindCopyButtons(f.root);
    expect(f.handlers.size).toBe(1);
    again();
    expect(f.handlers.size).toBe(0);
  });

  it("does not mutate feedback or schedule timers after an unsettled write is disposed", async () => {
    const f = fixture();
    let finish!: () => void;
    f.clipboard.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    const pending = f.click();
    f.dispose();
    finish();
    await pending;
    expect(f.button.textContent).toBe("Copy");
    expect(vi.getTimerCount()).toBe(0);
  });
});
