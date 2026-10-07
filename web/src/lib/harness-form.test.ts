import { describe, expect, test } from "vitest";
import type { Schemas } from "@/api/client";
import {
  emptyHarnessValues,
  fromHarness,
  type HarnessValues,
  toHarness,
  validateLimit,
  validateModuleName,
  validateName,
  validateRequired,
  validateTools,
} from "./harness-form";

type Harness = Schemas["Harness"];

const stored: Harness = {
  name: "notes",
  instructions: "Keep the notes tidy.",
  model: { provider: "anthropic", name: "a-model" },
  tools: ["files_read_text_file", "files_write_file"],
  limits: { max_steps: 10, max_tool_calls: 20 },
  policy: [{ name: "notes (inline policy)", source: "package agenty.tool\n" }],
};

describe("fromHarness and toHarness", () => {
  test("round-trip a stored harness", () => {
    expect(toHarness(fromHarness(stored))).toEqual(stored);
  });

  test("write tools one per line", () => {
    expect(fromHarness(stored).tools).toBe("files_read_text_file\nfiles_write_file");
  });

  test("read tools from lines, trimmed, without blank ones", () => {
    const values: HarnessValues = { ...fromHarness(stored), tools: "  a_b \n\n c-d\n" };

    expect(toHarness(values).tools).toEqual(["a_b", "c-d"]);
  });

  test("trim the name, provider and model", () => {
    const values: HarnessValues = {
      ...fromHarness(stored),
      name: " notes ",
      provider: " anthropic ",
      model: " a-model ",
    };

    expect(toHarness(values)).toMatchObject({
      name: "notes",
      model: { provider: "anthropic", name: "a-model" },
    });
  });

  test("leave out tools and policy when there are none", () => {
    const harness = toHarness({ ...fromHarness(stored), tools: "", policy: [] });

    expect(harness).not.toHaveProperty("tools");
    expect(harness).not.toHaveProperty("policy");
  });

  test("start a new harness on Anthropic with modest limits", () => {
    expect(emptyHarnessValues()).toEqual({
      name: "",
      instructions: "",
      provider: "anthropic",
      model: "",
      tools: "",
      maxSteps: 10,
      maxToolCalls: 20,
      policy: [],
    });
  });
});

describe("validation, as the server validates", () => {
  test.each([
    ["", "A name is required."],
    ["Notes", "Use lowercase letters, digits and single hyphens."],
    ["a--b", "Use lowercase letters, digits and single hyphens."],
    ["-a", "Use lowercase letters, digits and single hyphens."],
    ["notes-2", undefined],
  ])("name %j", (name, want) => {
    expect(validateName(name)).toBe(want);
  });

  test.each([
    ["", undefined],
    ["files_read\nfiles_write", undefined],
    ["has space", "has space: use 1 to 64 letters, digits, underscores or hyphens."],
    ["a".repeat(65), `${"a".repeat(65)}: use 1 to 64 letters, digits, underscores or hyphens.`],
    ["a\nb\na", "a is granted twice."],
  ])("tools %j", (tools, want) => {
    expect(validateTools(tools)).toBe(want);
  });

  test.each([
    [0, "Must be at least 1."],
    [-1, "Must be at least 1."],
    [1.5, "Must be a whole number."],
    [Number.NaN, "Must be a whole number."],
    [1, undefined],
  ])("limit %d", (limit, want) => {
    expect(validateLimit(limit)).toBe(want);
  });

  test("required text", () => {
    expect(validateRequired("  ", "Instructions")).toBe("Instructions are required.");
    expect(validateRequired("x", "Instructions")).toBeUndefined();
  });

  test("module names are required and unique", () => {
    const policy = [
      { name: "a", source: "" },
      { name: "a", source: "" },
      { name: " ", source: "" },
    ];

    expect(validateModuleName(policy, 0)).toBe("Another module has this name.");
    expect(validateModuleName(policy, 2)).toBe("A name is required.");
    expect(validateModuleName([{ name: "b", source: "" }], 0)).toBeUndefined();
  });
});
