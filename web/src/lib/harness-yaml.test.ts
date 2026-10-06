import { expect, test } from "vitest";
import { parse } from "yaml";
import type { components } from "@/api/schema";
import { harnessYaml } from "./harness-yaml";

const harness: components["schemas"]["Harness"] = {
  name: "notes",
  instructions: "Keep the notes tidy.\nSort them.\n",
  model: { provider: "anthropic", name: "a-model" },
  tools: ["files_read_text_file", "files_write_file"],
  limits: { max_steps: 10, max_tool_calls: 20 },
  policy: [{ name: "notes (inline policy)", source: "package agenty.tool\n" }],
};

test("writes the harness file's fields, multi-line text as a block", () => {
  const { policy: _, ...withoutPolicy } = harness;
  expect(harnessYaml(withoutPolicy)).toBe(
    [
      "name: notes",
      "instructions: |",
      "  Keep the notes tidy.",
      "  Sort them.",
      "model:",
      "  provider: anthropic",
      "  name: a-model",
      "tools:",
      "  - files_read_text_file",
      "  - files_write_file",
      "limits:",
      "  max_steps: 10",
      "  max_tool_calls: 20",
      "",
    ].join("\n"),
  );
});

test("keeps the harness file's key order, whatever the API's", () => {
  const reordered = {
    limits: { max_tool_calls: 20, max_steps: 10 },
    model: { name: "a-model", provider: "anthropic" },
    instructions: "Help.",
    name: "notes",
  };

  expect(harnessYaml(reordered)).toBe(
    [
      "name: notes",
      "instructions: Help.",
      "model:",
      "  provider: anthropic",
      "  name: a-model",
      "limits:",
      "  max_steps: 10",
      "  max_tool_calls: 20",
      "",
    ].join("\n"),
  );
});

test("leaves out the policy, which is stored resolved, but warns of it", () => {
  const yaml = harnessYaml(harness);

  expect(parse(yaml).policy).toBeUndefined();
  expect(
    yaml.endsWith(
      "# policy: left out. This harness has 1 policy module, shown on its page as Rego;\n" +
        "# applying this YAML without it would drop its rules.\n",
    ),
  ).toBe(true);
});

test("quotes what YAML would read as something else", () => {
  const tricky = { ...harness, name: "yes", instructions: "key: value # not a comment" };

  const parsed = parse(harnessYaml(tricky));

  expect(parsed.name).toBe("yes");
  expect(parsed.instructions).toBe("key: value # not a comment");
});

test("writes a harness without tools without them", () => {
  const { tools: _, ...withoutTools } = harness;

  expect(harnessYaml(withoutTools)).not.toContain("tools");
});
