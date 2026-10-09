import "server-only";
import { z } from "zod";
import { domainMatches, emailDomain, type Role, type ValidProvider } from "./providers";

export type SignInDecision = { organizationSlug: string; role: Role };
export type RejectCode = "email_domain_mismatch" | "organization_claim_missing";

const slugSchema = z.string().regex(/^[a-z0-9][a-z0-9-]{1,62}$/);

/** Admin when the role claim (a string or an array) contains an admin value; else member. */
export function deriveRole(
  claims: Record<string, unknown>,
  roleClaim: string | undefined,
  adminValues: string[],
): Role {
  if (!roleClaim) return "member";
  const value = claims[roleClaim];
  const values: unknown[] = typeof value === "string" ? [value] : Array.isArray(value) ? value : [];
  return values.some((v) => typeof v === "string" && adminValues.includes(v)) ? "admin" : "member";
}

/** Decides organization and role for a verified sign-in. Never throws on odd claim values. */
export function decideSignIn({
  provider,
  email,
  claims,
}: {
  provider: ValidProvider;
  email: string;
  claims: Record<string, unknown>;
}): { ok: true; decision: SignInDecision } | { ok: false; code: RejectCode } {
  const domain = emailDomain(email);
  if (!domain || !provider.domains.some((d) => domainMatches(domain, d))) {
    return { ok: false, code: "email_domain_mismatch" };
  }
  const slug = slugSchema.safeParse(claims[provider.organizationClaim]);
  if (!slug.success) return { ok: false, code: "organization_claim_missing" };
  return {
    ok: true,
    decision: {
      organizationSlug: slug.data,
      role: deriveRole(claims, provider.roleClaim, provider.adminValues),
    },
  };
}
