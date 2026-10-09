import { describe, expect, it } from "vitest";
import { evaluatePolicy, parseResultSet } from "./evaluate";

describe("evaluatePolicy (built policy.wasm)", () => {
  it("denies by default", async () => {
    await expect(evaluatePolicy({})).resolves.toEqual({
      outcome: "deny",
      reason: "no policy matches",
    });
  });

  it("denies a tool call", async () => {
    const decision = await evaluatePolicy({ tool: { name: "http.get" }, args: { url: "x" } });
    expect(decision.outcome).toBe("deny");
  });
});

describe("parseResultSet", () => {
  it.each(["allow", "require_approval", "deny"] as const)("accepts outcome %s", (outcome) => {
    const result = { outcome, reason: "because" };
    expect(parseResultSet([{ result }])).toEqual(result);
  });
});

describe("parseResultSet (fail closed)", () => {
  it("rejects an empty result set (decision undefined)", () => {
    expect(() => parseResultSet([])).toThrow();
  });

  it("rejects an unknown outcome", () => {
    expect(() => parseResultSet([{ result: { outcome: "maybe", reason: "x" } }])).toThrow();
  });

  it("rejects a decision without a reason", () => {
    expect(() => parseResultSet([{ result: { outcome: "allow" } }])).toThrow();
  });

  it("rejects the old boolean shape", () => {
    expect(() =>
      parseResultSet([{ result: { allow: true, reason: "x", require_approval: false } }]),
    ).toThrow();
  });

  it("rejects more than one result", () => {
    const result = { outcome: "allow", reason: "ok" };
    expect(() => parseResultSet([{ result }, { result }])).toThrow();
  });
});
