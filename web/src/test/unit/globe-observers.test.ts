import { expect, it, vi } from "vite-plus/test";
import { observeGlobe } from "../../console/operator/globe-observers.js";

it("disconnects observers and cancels pending work without reviving a disposed globe", () => {
  const callbacks: (() => void)[] = [];
  const observers: { disconnect: ReturnType<typeof vi.fn> }[] = [];
  class Observer {
    disconnect = vi.fn();
    observe = vi.fn();
    constructor(callback: () => void) {
      callbacks.push(callback);
      observers.push(this);
    }
  }
  let queued: () => void = () => {};
  const platform = {
    ResizeObserver: Observer,
    MutationObserver: Observer,
    requestAnimationFrame: vi.fn((callback: () => void) => {
      queued = callback;
      return 0;
    }),
    cancelAnimationFrame: vi.fn(),
  };
  const onResize = vi.fn(),
    onTheme = vi.fn();
  const dispose = observeGlobe({
    wrap: {},
    themeRoot: {},
    onResize,
    onTheme,
    platform: platform as unknown as typeof window,
  });
  callbacks[0]();
  expect(platform.requestAnimationFrame).toHaveBeenCalledTimes(1);
  dispose();
  expect(platform.cancelAnimationFrame).toHaveBeenCalledWith(0);
  for (const observer of observers)
    expect(observer.disconnect).toHaveBeenCalledTimes(1);
  queued();
  callbacks[0]();
  callbacks[1]();
  expect(onResize).not.toHaveBeenCalled();
  expect(onTheme).not.toHaveBeenCalled();
  expect(platform.requestAnimationFrame).toHaveBeenCalledTimes(1);
});

it("coalesces live resize work and preserves theme redraws until disposal", () => {
  const callbacks: (() => void)[] = [];
  const frames = new Map<number, () => void>();
  let nextID = 0;
  class Observer {
    observe() {}
    disconnect() {}
    constructor(callback: () => void) {
      callbacks.push(callback);
    }
  }
  const platform = {
    ResizeObserver: Observer,
    MutationObserver: Observer,
    requestAnimationFrame(callback: () => void) {
      const id = nextID++;
      frames.set(id, callback);
      return id;
    },
    cancelAnimationFrame(id: number) {
      frames.delete(id);
    },
  };
  const onResize = vi.fn(),
    onTheme = vi.fn();
  const dispose = observeGlobe({
    wrap: {},
    themeRoot: {},
    onResize,
    onTheme,
    platform: platform as unknown as typeof window,
  });
  callbacks[0]();
  callbacks[0]();
  expect(frames.size).toBe(1);
  frames.get(1)!();
  frames.delete(1);
  expect(onResize).toHaveBeenCalledTimes(1);
  callbacks[1]();
  expect(onTheme).toHaveBeenCalledTimes(1);
  dispose();
  callbacks[1]();
  expect(onTheme).toHaveBeenCalledTimes(1);
});
