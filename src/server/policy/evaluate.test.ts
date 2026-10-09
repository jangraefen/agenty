import { describe, expect, it } from "vitest";
import { evaluatePolicy, parseResultSet } from "./evaluate";

describe("evaluatePolicy (built policy.wasm)", () => {
  it("denies by default", async () => {
    await expect(evaluatePolicy({})).resolves.toEqual({
      allow: false,
      reason: "no policy matches",
      require_approval: false,
    });
  });

  it("denies a tool call", async () => {
    const decision = await evaluatePolicy({ tool: { name: "http.get" }, args: { url: "x" } });
    expect(decision.allow).toBe(false);
  });
});

describe("parseResultSet (fail closed)", () => {
  it("rejects an empty result set (decision undefined)", () => {
    expect(() => parseResultSet([])).toThrow();
  });

  it("rejects a result with wrong types", () => {
    expect(() =>
      parseResultSet([{ result: { allow: "yes", reason: "x", require_approval: false } }]),
    ).toThrow();
  });

  it("rejects more than one result", () => {
    const result = { allow: true, reason: "ok", require_approval: false };
    expect(() => parseResultSet([{ result }, { result }])).toThrow();
  });

  it("accepts exactly one well-formed decision", () => {
    const result = { allow: true, reason: "ok", require_approval: true };
    expect(parseResultSet([{ result }])).toEqual(result);
  });
});
