import "server-only";
import { getCurrentAuthEndpointContext } from "@better-auth/core/context";
import type { SSOUserResolution, SSOUserResolutionInput } from "@better-auth/sso";
import type { DBTransactionAdapter } from "better-auth";
import { APIError } from "better-auth/api";
import { decideSignIn, type SignInDecision } from "./claims";
import { parseProvider } from "./providers";

export type ProvisioningDecision = SignInDecision & { providerId: string };

// resolveUser decides, session.create.before writes: both run in the same endpoint call, whose
// context object carries the decision from one to the other. Weak, so nothing outlives a request.
const decisions = new WeakMap<object, ProvisioningDecision>();

export function rememberDecision(endpointContext: object, decision: ProvisioningDecision): void {
  decisions.set(endpointContext, decision);
}

/** The remembered decision of this endpoint call, at most once. */
export function takeDecision(
  endpointContext: object | null | undefined,
): ProvisioningDecision | undefined {
  if (!endpointContext) return undefined;
  const decision = decisions.get(endpointContext);
  decisions.delete(endpointContext);
  return decision;
}

type OrganizationRow = { id: string; slug: string; providerId: string | null };
type MemberRow = { id: string; userId: string; organizationId: string; role: string };

/** Whether the error, or one of its causes, is a Postgres unique violation. */
function isUniqueViolation(error: unknown): boolean {
  let current: unknown = error;
  for (let depth = 0; depth < 5 && typeof current === "object" && current !== null; depth++) {
    if ((current as { code?: unknown }).code === "23505") return true;
    current = (current as { cause?: unknown }).cause;
  }
  return false;
}

/**
 * Creates the organization of the decision (owned by its provider) if absent, and the user's
 * membership in it, or updates the role of an existing membership. Runs inside the sign-in
 * transaction, so a throw here leaves no user, account or session behind.
 */
export async function provisionMembership(
  adapter: DBTransactionAdapter,
  userId: string,
  decision: ProvisioningDecision,
): Promise<void> {
  try {
    const now = new Date();
    let organization = await adapter.findOne<OrganizationRow>({
      model: "organization",
      where: [{ field: "slug", value: decision.organizationSlug }],
    });
    if (!organization) {
      organization = await adapter.create<Omit<OrganizationRow, "id">, OrganizationRow>({
        model: "organization",
        data: {
          slug: decision.organizationSlug,
          providerId: decision.providerId,
          createdAt: now,
          updatedAt: now,
        } as Omit<OrganizationRow, "id">,
      });
    } else if (organization.providerId !== decision.providerId) {
      throw new APIError("FORBIDDEN", { code: "organization_owned_by_other_provider" });
    }

    const membership = await adapter.findOne<MemberRow>({
      model: "member",
      where: [{ field: "userId", value: userId }],
    });
    if (!membership) {
      await adapter.create({
        model: "member",
        data: {
          userId,
          organizationId: organization.id,
          role: decision.role,
          createdAt: now,
          updatedAt: now,
        },
      });
    } else if (membership.organizationId !== organization.id) {
      throw new APIError("FORBIDDEN", { code: "organization_changed" });
    } else {
      await adapter.update({
        model: "member",
        where: [{ field: "id", value: membership.id }],
        update: { role: decision.role, updatedAt: now },
      });
    }
  } catch (error) {
    if (error instanceof APIError) throw error;
    if (isUniqueViolation(error)) {
      // A concurrent first sign-in created the same row: the user may simply retry.
      throw new APIError("CONFLICT", { code: "try_again" });
    }
    console.error(
      `Provisioning failed: ${error instanceof Error ? error.constructor.name : typeof error}`,
    );
    throw new APIError("INTERNAL_SERVER_ERROR", { code: "provisioning_failed" });
  }
}

/**
 * The SSO plugin's resolveUser: validates the provider row, decides organization and role from
 * the verified ID token, refuses an email bound to another provider, and hands the decision to
 * session.create.before.
 */
export async function resolveSignIn(
  input: SSOUserResolutionInput,
  database: DBTransactionAdapter,
): Promise<SSOUserResolution> {
  if (input.protocol !== "oidc") return { action: "reject", code: "idp_unavailable" };

  const row = await database.findOne({
    model: "ssoProvider",
    where: [{ field: "providerId", value: input.providerId }],
  });
  const parsed = parseProvider(row);
  if (!parsed.ok) {
    console.error(
      `Identity provider "${input.providerId}" is misconfigured: ${parsed.fields.join(", ")}`,
    );
    return { action: "reject", code: "idp_unavailable" };
  }

  const result = decideSignIn({
    provider: parsed.provider,
    email: input.providerUser.email,
    claims: input.verifiedIdTokenClaims,
  });
  if (!result.ok) return { action: "reject", code: result.code };

  // Better Auth stores emails lower-cased.
  const user = await database.findOne<{ id: string }>({
    model: "user",
    where: [{ field: "email", value: input.providerUser.email.toLowerCase() }],
  });
  if (user) {
    const accounts = await database.findMany<{ providerId: string }>({
      model: "account",
      where: [{ field: "userId", value: user.id }],
    });
    if (accounts.some((account) => account.providerId !== input.providerId)) {
      return { action: "reject", code: "account_bound_to_other_provider" };
    }
  }

  rememberDecision(getCurrentAuthEndpointContext(), {
    ...result.decision,
    providerId: input.providerId,
  });
  return { action: "continue" };
}
