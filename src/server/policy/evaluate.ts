import "server-only";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { loadPolicy } from "@open-policy-agent/opa-wasm";
import { z } from "zod";

/** The decision shape OPA returns (package agenty.authz, rule `decision`). */
export const policyDecisionSchema = z
  .object({
    allow: z.boolean(),
    reason: z.string(),
    require_approval: z.boolean(),
  })
  .refine((decision) => decision.allow || !decision.require_approval, {
    message: "require_approval is only valid together with allow",
    path: ["require_approval"],
  });

export type PolicyDecision = z.infer<typeof policyDecisionSchema>;

/** What a caller must do with a tool call. Handle every `kind`; there is no implicit default. */
export type PolicyOutcome =
  | { kind: "allow"; reason: string }
  | { kind: "require_approval"; reason: string }
  | { kind: "deny"; reason: string };

function toOutcome({ allow, reason, require_approval }: PolicyDecision): PolicyOutcome {
  if (!allow) return { kind: "deny", reason };
  return { kind: require_approval ? "require_approval" : "allow", reason };
}

const resultSetSchema = z.tuple([z.object({ result: policyDecisionSchema })]);

/**
 * Parses OPA's result set. Anything but exactly one well-formed, consistent decision throws
 * (fail closed): callers must treat a thrown error as a denial.
 */
export function parseResultSet(raw: unknown): PolicyOutcome {
  const [entry] = resultSetSchema.parse(raw);
  return toOutcome(entry.result);
}

type LoadedPolicy = Awaited<ReturnType<typeof loadPolicy>>;

let policy: Promise<LoadedPolicy> | undefined;

/** The standalone server chdirs to its own directory, where the build traces this file to. */
function policyWasmPath(): string {
  return path.join(process.cwd(), "build/policy/policy.wasm");
}

function getPolicy(): Promise<LoadedPolicy> {
  policy ??= readFile(policyWasmPath())
    .then((wasm) => loadPolicy(wasm))
    .catch((error: unknown) => {
      policy = undefined; // retry on the next call instead of caching the failure
      throw error;
    });
  return policy;
}

export async function evaluatePolicy(input: unknown): Promise<PolicyOutcome> {
  const loaded = await getPolicy();
  return parseResultSet(loaded.evaluate(input));
}
