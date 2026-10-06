import type { Schemas } from "./server";

export function run(overrides: Partial<Schemas["Run"]> = {}): Schemas["Run"] {
  return {
    id: "run-1",
    harness_version_id: 7,
    harness: "notes",
    harness_version: 3,
    started_by: "demo",
    input: "tidy my notes",
    status: "succeeded",
    output: "Done.",
    steps: 2,
    created_at: "2026-10-06T10:00:00Z",
    finished_at: "2026-10-06T10:01:30Z",
    ...overrides,
  };
}

export function harnessVersion(name: string): Schemas["HarnessVersion"] {
  return {
    id: 1,
    version: 1,
    created_at: "2026-10-01T09:00:00Z",
    harness: {
      name,
      instructions: "Help.",
      model: { provider: "anthropic", name: "a-model" },
      limits: { max_steps: 10, max_tool_calls: 10 },
    },
  };
}
