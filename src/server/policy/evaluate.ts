import "server-only";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { loadPolicy } from "@open-policy-agent/opa-wasm";
import { z } from "zod";

export const policyDecisionSchema = z.object({
  allow: z.boolean(),
  reason: z.string(),
  require_approval: z.boolean(),
});

export type PolicyDecision = z.infer<typeof policyDecisionSchema>;

const resultSetSchema = z.tuple([z.object({ result: policyDecisionSchema })]);

/** Parses OPA's result set. Anything but exactly one well-formed decision throws (fail closed). */
export function parseResultSet(raw: unknown): PolicyDecision {
  const [entry] = resultSetSchema.parse(raw);
  return entry.result;
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

export async function evaluatePolicy(input: unknown): Promise<PolicyDecision> {
  const loaded = await getPolicy();
  return parseResultSet(loaded.evaluate(input));
}
