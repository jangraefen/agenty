import { describe, expect, it } from "vitest";
import { checkHealth } from "./health";

describe("checkHealth against Postgres", () => {
  it("reaches the database as the runtime role", async () => {
    await expect(checkHealth()).resolves.toEqual({
      httpStatus: 200,
      body: { status: "ok", db: "ok" },
    });
  });
});
