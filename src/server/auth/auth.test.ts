import { describe, expect, it, vi } from "vitest";
import { memoizeUntil } from "./auth";

function fakeClock(start = 1_000) {
  let time = start;
  return {
    now: () => time,
    advance: (ms: number) => {
      time += ms;
    },
  };
}

describe("memoizeUntil", () => {
  it("shares one pending creation between concurrent callers", async () => {
    const create = vi.fn(async () => "auth");
    const get = memoizeUntil(create, async () => true, 30_000);

    const [a, b] = await Promise.all([get(), get()]);

    expect(a).toBe("auth");
    expect(b).toBe("auth");
    expect(create).toHaveBeenCalledTimes(1);
  });

  it("creates a usable value only once", async () => {
    const clock = fakeClock();
    const create = vi.fn(async () => ({}));
    const get = memoizeUntil(create, async () => true, 30_000, clock.now);

    const first = await get();
    clock.advance(10 * 60 * 1000);
    const second = await get();

    expect(second).toBe(first);
    expect(create).toHaveBeenCalledTimes(1);
  });

  it("returns an unusable value, keeps it for retryAfterMs, then recreates it", async () => {
    const clock = fakeClock();
    let usable = false;
    const create = vi.fn(async () => ({}));
    const get = memoizeUntil(create, async () => usable, 30_000, clock.now);

    const first = await get();
    clock.advance(30_000 - 1);
    expect(await get()).toBe(first);
    expect(create).toHaveBeenCalledTimes(1);

    usable = true;
    clock.advance(1);
    const second = await get();
    expect(second).not.toBe(first);
    expect(create).toHaveBeenCalledTimes(2);

    clock.advance(60_000);
    expect(await get()).toBe(second);
    expect(create).toHaveBeenCalledTimes(2);
  });

  it("retries a rejected creation on the next call", async () => {
    const create = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce("auth");
    const get = memoizeUntil(create, async () => true, 30_000);

    await expect(get()).rejects.toThrow("boom");
    await expect(get()).resolves.toBe("auth");
    expect(create).toHaveBeenCalledTimes(2);
  });

  it("retries a creation that throws synchronously", async () => {
    let fail = true;
    const create = vi.fn(() => {
      if (fail) throw new Error("sync");
      return Promise.resolve("auth");
    });
    const get = memoizeUntil(create, async () => true, 30_000);

    await expect(get()).rejects.toThrow("sync");
    fail = false;
    await expect(get()).resolves.toBe("auth");
  });
});
