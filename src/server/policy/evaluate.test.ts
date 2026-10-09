import { describe, expect, it } from "vitest";
import { evaluatePolicy, parseResultSet } from "./evaluate";

describe("evaluatePolicy (built policy.wasm)", () => {
  it("denies by default", async () => {
    await expect(evaluatePolicy({})).resolves.toEqual({
      kind: "deny",
      reason: "no policy matches",
    });
  });

  it("denies a tool call", async () => {
    const outcome = await evaluatePolicy({ tool: { name: "http.get" }, args: { url: "x" } });
    expect(outcome.kind).toBe("deny");
  });
});

describe("parseResultSet", () => {
  const decision = (allow: boolean, require_approval: boolean) => [
    { result: { allow, reason: "because", require_approval } },
  ];

  it("maps allow without approval to allow", () => {
    expect(parseResultSet(decision(true, false))).toEqual({ kind: "allow", reason: "because" });
  });

  it("maps allow with approval to require_approval", () => {
    expect(parseResultSet(decision(true, true))).toEqual({
      kind: "require_approval",
      reason: "because",
    });
  });

  it("maps a denial to deny", () => {
    expect(parseResultSet(decision(false, false))).toEqual({ kind: "deny", reason: "because" });
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

  it("rejects the contradictory decision: denied but requiring approval", () => {
    expect(() =>
      parseResultSet([{ result: { allow: false, reason: "x", require_approval: true } }]),
    ).toThrow(/require_approval/);
  });
});
