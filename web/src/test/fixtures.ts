import type { Schemas } from "./server";

export function run(overrides: Partial<Schemas["Run"]> = {}): Schemas["Run"] {
  return {
    id: "run-1",
    conversation_id: "run-1",
    harness: "notes",
    harness_version: 3,
    started_by: "demo",
    input: "tidy my notes",
    status: "succeeded",
    output: "Done.",
    steps: 2,
    usage: { input_tokens: 0, output_tokens: 0, cache_write_tokens: 0, cache_read_tokens: 0 },
    created_at: "2026-10-06T10:00:00Z",
    finished_at: "2026-10-06T10:01:30Z",
    ...overrides,
  };
}

export function auditRecord(
  overrides: Partial<Schemas["AuditRecord"]> = {},
): Schemas["AuditRecord"] {
  return {
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

// A stored version of a harness, by default version 1 of notes.
export function storedHarness(
  harness: Partial<Schemas["Harness"]> = {},
  version: Partial<Omit<Schemas["HarnessVersion"], "harness">> = {},
): Schemas["HarnessVersion"] {
  return {
    version: 1,
    created_at: "2026-10-01T09:00:00Z",
    ...version,
    harness: {
      name: "notes",
      instructions: "Help.",
      model: { provider: "anthropic", name: "a-model" },
      limits: { max_steps: 10, max_tool_calls: 10 },
      ...harness,
    },
  };
}

export function conversation(
  overrides: Partial<Schemas["ConversationSummary"]> = {},
): Schemas["ConversationSummary"] {
  return {
    id: "run-1",
    workspace: "notes",
    harness: "notes",
    title: "tidy my notes",
    status: "succeeded",
    ...overrides,
  };
}

export function auditRun(overrides: Partial<Schemas["AuditRun"]> = {}): Schemas["AuditRun"] {
  return {
    id: "run-1",
    workspace: "notes",
    harness: "notes",
    harness_version: 3,
    started_by: "demo",
    status: "succeeded",
    steps: 2,
    usage: { input_tokens: 0, output_tokens: 0, cache_write_tokens: 0, cache_read_tokens: 0 },
    created_at: "2026-10-06T10:00:00Z",
    finished_at: "2026-10-06T10:01:30Z",
    ...overrides,
  };
}

export function logEvent(
  overrides: Partial<Schemas["AuditLogEvent"]> = {},
): Schemas["AuditLogEvent"] {
  return {
    id: 1,
    recorded_at: "2026-10-08T12:00:00Z",
    actor: "demo",
    action: "harness.changed",
    workspace: "notes",
    run_id: "",
    target: "notes",
    details: { version: 2 },
    prev_hash: "0".repeat(64),
    hash: "1".repeat(64),
    ...overrides,
  };
}
