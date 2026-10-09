/**
 * The model behind the harness form: conversion between a stored harness and
 * the form's values, and the form's field validators.
 *
 * The new-harness and edit pages start components/harness-form.tsx, a
 * TanStack Form on HarnessValues, from emptyHarnessValues or fromHarness;
 * hooks/use-save-harness.ts turns the submitted values back into a Harness
 * with toHarness and stores it. Kept apart from the components so the
 * conversion and validation are tested without rendering.
 */
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

/** The new-harness form's starting values: blank fields, Anthropic, 10 steps and 20 tool calls. */
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

/** The form's values for a stored harness, for editing it: tools one per line. */
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

/**
 * The harness the form's values describe, as the API stores it. Names are
 * trimmed, as the validators check them trimmed; tools and policy are left
 * out when empty, as a harness file would leave them out.
 */
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

/** The tool names of the tools field: one per line, blank lines ignored. */
function toolLines(tools: string): string[] {
  return tools
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");
}

// The validators mirror the server's (internal/harness and toolgateway), so
// the form says what is wrong before it sends; the server checks again, and
// compiles the policy, which only it can.

/** A harness name: lowercase words of letters and digits joined by single hyphens. */
const slug = /^[a-z0-9]+(-[a-z0-9]+)*$/;
/** A tool name, as model provider APIs accept it (toolgateway.ValidateToolName). */
const toolName = /^[a-zA-Z0-9_-]{1,64}$/;

/** The error of a harness name, or undefined when it is valid. */
export function validateName(name: string): string | undefined {
  const trimmed = name.trim();
  if (trimmed === "") {
    return "A name is required.";
  }
  return slug.test(trimmed) ? undefined : "Use lowercase letters, digits and single hyphens.";
}

/**
 * The error of a required field left blank, worded for label: "are" for a
 * plural label such as "Instructions", "is" otherwise.
 */
export function validateRequired(value: string, label: string): string | undefined {
  return value.trim() === ""
    ? `${label} ${label.endsWith("s") ? "are" : "is"} required.`
    : undefined;
}

/**
 * The error of the tools field: the first malformed or repeated tool, so the
 * user fixes one line at a time.
 */
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

/** The error of a step or tool-call limit: a whole number of at least 1. */
export function validateLimit(limit: number): string | undefined {
  if (!Number.isInteger(limit)) {
    return "Must be a whole number.";
  }
  return limit < 1 ? "Must be at least 1." : undefined;
}

/** The error of the name of policy module index: required, and unique within the harness. */
export function validateModuleName(policy: PolicyModule[], index: number): string | undefined {
  const name = policy[index]?.name.trim() ?? "";
  if (name === "") {
    return "A name is required.";
  }
  const twice = policy.some((module, i) => i !== index && module.name.trim() === name);
  return twice ? "Another module has this name." : undefined;
}
