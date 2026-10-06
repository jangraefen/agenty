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

export function auditRecord(
  overrides: Partial<Schemas["AuditRecord"]> = {},
): Schemas["AuditRecord"] {
  return {
    run_id: "run-1",
    call_id: "call-1",
    event: "decision",
    tool: "files_read_file",
    args: { path: "notes.md" },
    decision: "allow",
    recorded_at: "2026-10-06T10:00:05Z",
    ...overrides,
  };
}

export function approvalRequest(
  overrides: Partial<Schemas["ApprovalRequest"]> = {},
): Schemas["ApprovalRequest"] {
  return {
    id: "approval-1",
    run_id: "run-1",
    harness: "notes",
    tool: "files_write_file",
    args: { path: "notes.md", content: "tidy" },
    reasons: ["file changes need approval"],
    created_at: "2026-10-06T10:00:10Z",
    expires_at: "2026-10-06T11:00:10Z",
    ...overrides,
  };
}
