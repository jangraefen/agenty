import type { Schemas } from "@/api/client";

type Harness = Schemas["Harness"];
type PolicyModule = Schemas["PolicyModule"];

/** What the harness form edits: a harness, with its tools one per line. */
export interface HarnessValues {
  name: string;
  instructions: string;
  provider: string;
  model: string;
  tools: string;
  maxSteps: number;
  maxToolCalls: number;
  policy: PolicyModule[];
}

export function emptyHarnessValues(): HarnessValues {
  return {
    name: "",
    instructions: "",
    provider: "anthropic",
    model: "",
    tools: "",
    maxSteps: 10,
    maxToolCalls: 20,
    policy: [],
  };
}

export function fromHarness(harness: Harness): HarnessValues {
  return {
    name: harness.name,
    instructions: harness.instructions,
    provider: harness.model.provider,
    model: harness.model.name,
    tools: (harness.tools ?? []).join("\n"),
    maxSteps: harness.limits.max_steps,
    maxToolCalls: harness.limits.max_tool_calls,
    policy: harness.policy ?? [],
  };
}

export function toHarness(values: HarnessValues): Harness {
  const tools = toolLines(values.tools);
  return {
    name: values.name.trim(),
    instructions: values.instructions,
    model: { provider: values.provider.trim(), name: values.model.trim() },
    ...(tools.length === 0 ? {} : { tools }),
    limits: { max_steps: values.maxSteps, max_tool_calls: values.maxToolCalls },
    ...(values.policy.length === 0 ? {} : { policy: values.policy }),
  };
}

function toolLines(tools: string): string[] {
  return tools
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");
}

// The validators mirror the server's (internal/harness and toolgateway), so
// the form says what is wrong before it sends; the server checks again, and
// compiles the policy, which only it can.

const slug = /^[a-z0-9]+(-[a-z0-9]+)*$/;
const toolName = /^[a-zA-Z0-9_-]{1,64}$/;

export function validateName(name: string): string | undefined {
  const trimmed = name.trim();
  if (trimmed === "") {
    return "A name is required.";
  }
  return slug.test(trimmed) ? undefined : "Use lowercase letters, digits and single hyphens.";
}

export function validateRequired(value: string, label: string): string | undefined {
  return value.trim() === ""
    ? `${label} ${label.endsWith("s") ? "are" : "is"} required.`
    : undefined;
}

export function validateTools(tools: string): string | undefined {
  const seen = new Set<string>();
  for (const tool of toolLines(tools)) {
    if (!toolName.test(tool)) {
      return `${tool}: use 1 to 64 letters, digits, underscores or hyphens.`;
    }
    if (seen.has(tool)) {
      return `${tool} is granted twice.`;
    }
    seen.add(tool);
  }
  return undefined;
}

export function validateLimit(limit: number): string | undefined {
  if (!Number.isInteger(limit)) {
    return "Must be a whole number.";
  }
  return limit < 1 ? "Must be at least 1." : undefined;
}

export function validateModuleName(policy: PolicyModule[], index: number): string | undefined {
  const name = policy[index]?.name.trim() ?? "";
  if (name === "") {
    return "A name is required.";
  }
  const twice = policy.some((module, i) => i !== index && module.name.trim() === name);
  return twice ? "Another module has this name." : undefined;
}
