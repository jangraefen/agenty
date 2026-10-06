import { describe, expect, test } from "vitest";
import { formatDuration } from "./format";

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
