import { describe, expect, test } from "vitest";
import { formatDuration, formatRemaining, formatUsage, formatUsageExactly } from "./format";

describe("formatDuration", () => {
  test.each([
    ["2026-10-06T10:00:00Z", "2026-10-06T10:00:00.400Z", "0s"],
    ["2026-10-06T10:00:00Z", "2026-10-06T10:00:42Z", "42s"],
    ["2026-10-06T10:00:00Z", "2026-10-06T10:01:30Z", "1m 30s"],
    ["2026-10-06T10:00:00Z", "2026-10-06T12:05:00Z", "2h 5m"],
  ])("from %s to %s is %s", (from, to, want) => {
    expect(formatDuration(from, to)).toBe(want);
  });
});

describe("formatRemaining", () => {
  const now = Date.parse("2026-10-06T10:00:00Z");

  test.each([
    ["2026-10-06T10:00:42Z", "42s left"],
    ["2026-10-06T10:01:30Z", "1m 30s left"],
    ["2026-10-06T11:00:00Z", "1h 0m left"],
    ["2026-10-06T10:00:00Z", "expired"],
    ["2026-10-06T09:00:00Z", "expired"],
  ])("until %s is %s", (until, want) => {
    expect(formatRemaining(until, now)).toBe(want);
  });
});

describe("formatUsage", () => {
  const usage = { input_tokens: 0, output_tokens: 0, cache_write_tokens: 0, cache_read_tokens: 0 };

  test.each([
    [{ ...usage, input_tokens: 950, output_tokens: 40 }, "950 tokens in, 40 out"],
    [{ ...usage, input_tokens: 12_345, output_tokens: 1_000 }, "12.3k tokens in, 1k out"],
    [
      {
        input_tokens: 500,
        output_tokens: 200,
        cache_write_tokens: 1_500,
        cache_read_tokens: 8_000,
      },
      "10k tokens in, 200 out, 80% from the cache",
    ],
    [{ ...usage, input_tokens: 2_500_000, output_tokens: 3 }, "2.5M tokens in, 3 out"],
    [{ ...usage, input_tokens: 999_950, output_tokens: 999 }, "1M tokens in, 999 out"],
    [
      { ...usage, input_tokens: 4, cache_read_tokens: 996 },
      "1k tokens in, 0 out, 99% from the cache",
    ],
  ])("%o is %s", (u, want) => {
    expect(formatUsage(u)).toBe(want);
  });
});

test("formatUsageExactly", () => {
  expect(
    formatUsageExactly({
      input_tokens: 1_234,
      output_tokens: 5,
      cache_write_tokens: 0,
      cache_read_tokens: 80_000,
    }),
  ).toBe("1,234 input tokens, 0 written to and 80,000 read from the cache; 5 output tokens");
});
