import { describe, expect, it } from "vite-plus/test";
import {
  forwardOrthographic,
  inverseOrthographic,
  paintDisc,
} from "../../console/operator/globe-texture.js";

const ROT: [number, number, number] = [96, -37, 0];

describe("globe texture projection", () => {
  it("inverts the projection the globe draws with", () => {
    for (const [lng, lat] of [
      [-77.4, 38.9],
      [-122.3, 47.6],
      [-147.7, 64.8],
      [-80.2, 25.8],
    ]) {
      const p = forwardOrthographic(lng, lat, ROT);
      expect(p).not.toBeNull();
      const back = inverseOrthographic(p![0], p![1], ROT);
      expect(back![0]).toBeCloseTo(lng, 6);
      expect(back![1]).toBeCloseTo(lat, 6);
    }
  });

  it("returns nothing outside the disc", () => {
    expect(inverseOrthographic(0.9, 0.9, ROT)).toBeNull();
  });

  it("paints the texel under each pixel and leaves the corners clear", () => {
    // 4x2 texture: western half red, eastern half blue.
    const width = 4;
    const height = 2;
    const data = new Uint8ClampedArray(width * height * 4);
    for (let i = 0; i < width * height; i++) {
      const east = i % width >= width / 2;
      data.set(east ? [0, 0, 255, 255] : [255, 0, 0, 255], i * 4);
    }
    const size = 8;
    const out = new Uint8ClampedArray(size * size * 4);
    // Centered on lng 90 east: the middle of the disc is the eastern half.
    paintDisc(out, size, { data, width, height }, [-90, 0, 0]);
    const at = (x: number, y: number) =>
      out.slice((y * size + x) * 4, (y * size + x) * 4 + 4);
    expect(at(0, 0)[3]).toBe(0);
    const middle = at(4, 4);
    expect(middle[3]).toBe(255);
    expect(middle[2]).toBeGreaterThan(150);
    expect(middle[0]).toBe(0);
  });
});
