import { afterEach, describe, expect, it, vi } from "vitest";
import { checkHealth } from "./health";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("checkHealth", () => {
  it("reports ok when the database answers", async () => {
    await expect(checkHealth(async () => [{ "?column?": 1 }])).resolves.toEqual({
      httpStatus: 200,
      body: { status: "ok", db: "ok" },
    });
  });

  it("reports 503 without leaking error details when the database fails", async () => {
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    const result = await checkHealth(async () => {
      throw new Error("password authentication failed for user agenty_app at 10.0.0.5");
    });
    expect(result).toEqual({ httpStatus: 503, body: { status: "error", db: "unavailable" } });
    expect(JSON.stringify(result)).not.toContain("password");
    expect(log).toHaveBeenCalledOnce();
  });
});
