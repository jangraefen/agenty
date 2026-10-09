import "server-only";
import { lookup as dnsLookup } from "node:dns/promises";
import { isIP } from "node:net";
import { isPublicRoutableHost } from "@better-auth/core/utils/host";
import { z } from "zod";
import type { ssoProvider } from "@/server/db/auth-schema";

/** member.role is plain text in the database: every read goes through this (fail closed). */
export const roleSchema = z.enum(["admin", "member"]);
export type Role = z.infer<typeof roleSchema>;
export type ProviderRow = typeof ssoProvider.$inferSelect;

const httpUrl = z.url({ protocol: /^https?$/ });

// Exactly the keys and values the spec accepts. Anything else (userInfoEndpoint, mapping,
// allowIdpInitiated, private_key_jwt, offline_access, ...) makes the provider unusable.
const oidcConfigSchema = z.strictObject({
  clientId: z.string().min(1),
  clientSecret: z.string().min(1),
  pkce: z.literal(true),
  scopes: z.tuple([z.literal("openid"), z.literal("email"), z.literal("profile")]),
  discoveryEndpoint: httpUrl.optional(),
  authorizationEndpoint: httpUrl.optional(),
  tokenEndpoint: httpUrl.optional(),
  jwksEndpoint: httpUrl.optional(),
});

export type OidcConfig = z.infer<typeof oidcConfigSchema>;

export type ValidProvider = {
  providerId: string;
  issuer: string;
  domains: string[];
  organizationClaim: string;
  roleClaim?: string;
  adminValues: string[];
  oidc: OidcConfig;
  hasEndpoints: boolean;
};

const domainName = /^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,62}$/;

/** Drizzle returns the JSON text, Better Auth's adapter may return it parsed: accept both. */
function parseJsonText(value: unknown): unknown {
  if (typeof value !== "string") return value;
  try {
    return JSON.parse(value);
  } catch {
    return undefined; // reported as an issue at `oidcConfig`
  }
}

const splitList = (value: string) => value.split(",").map((entry) => entry.trim());

const rowSchema = z.object({
  providerId: z.string().min(1),
  issuer: httpUrl,
  domain: z
    .string()
    .transform((value) => splitList(value.toLowerCase()))
    .pipe(z.array(z.string().regex(domainName)).min(1)),
  samlConfig: z.null().optional(),
  organizationId: z.null().optional(),
  organizationClaim: z.string().min(1),
  roleClaim: z.string().min(1).nullish(),
  adminValues: z
    .string()
    .nullish()
    .transform((value) => (value ? splitList(value) : []))
    .pipe(z.array(z.string().min(1))),
  oidcConfig: z.preprocess(parseJsonText, oidcConfigSchema),
});

/**
 * Validates a provider row (a Drizzle row or a Better Auth adapter row). On failure it returns the
 * paths of the invalid fields only, never their values (the config holds the client secret).
 */
export function parseProvider(
  row: unknown,
): { ok: true; provider: ValidProvider } | { ok: false; fields: string[] } {
  const result = rowSchema.safeParse(row);
  if (!result.success) {
    const fields = result.error.issues.map((issue) => issue.path.map(String).join(".") || "row");
    return { ok: false, fields: [...new Set(fields)] };
  }
  const { data } = result;
  const oidc = data.oidcConfig;
  return {
    ok: true,
    provider: {
      providerId: data.providerId,
      issuer: data.issuer,
      domains: data.domain,
      organizationClaim: data.organizationClaim,
      ...(data.roleClaim ? { roleClaim: data.roleClaim } : {}),
      adminValues: data.adminValues,
      oidc,
      hasEndpoints: Boolean(oidc.authorizationEndpoint && oidc.tokenEndpoint && oidc.jwksEndpoint),
    },
  };
}

/**
 * Whether an IdP URL may be fetched or redirected to: public hosts always, private and loopback
 * hosts only when their origin is trusted. Mirrors the SSO plugin's endpoint validation; the
 * plugin's own discovery would accept trusted origins only, i.e. no public IdP by default.
 */
export function isAllowedIdpUrl(url: string, isTrustedOrigin: (url: string) => boolean): boolean {
  let hostname: string;
  try {
    hostname = new URL(url).hostname;
  } catch {
    return false;
  }
  return isPublicRoutableHost(hostname) || isTrustedOrigin(url);
}

export type HostLookup = (host: string) => Promise<{ address: string }[]>;

const lookupAll: HostLookup = (host) => dnsLookup(host, { all: true });

/**
 * Whether an IdP URL's host resolves to public addresses only (or its origin is trusted, which
 * skips the lookup). The SSO plugin skips its own DNS check for every URL its `isTrustedOrigin`
 * accepts, and `isAllowedIdpUrl` accepts every public name, so discovery checks DNS here first.
 * A host that does not resolve is rejected.
 */
export async function idpHostResolvesPublic(
  url: string,
  isTrustedOrigin: (url: string) => boolean,
  lookup: HostLookup = lookupAll,
): Promise<boolean> {
  if (isTrustedOrigin(url)) return true;
  let host: string;
  try {
    host = new URL(url).hostname.replace(/^\[(.*)\]$/, "$1");
  } catch {
    return false;
  }
  if (isIP(host)) return isPublicRoutableHost(host);
  try {
    const addresses = await lookup(host);
    return addresses.length > 0 && addresses.every(({ address }) => isPublicRoutableHost(address));
  } catch {
    return false;
  }
}

/** The lower-cased domain of an email address with exactly one "@", else undefined. */
export function emailDomain(email: string): string | undefined {
  const parts = email.split("@");
  if (parts.length !== 2) return undefined;
  const [local, domain] = parts;
  if (!local || !domain) return undefined;
  return domain.toLowerCase();
}

export function domainMatches(emailDomainValue: string, domain: string): boolean {
  return emailDomainValue === domain || emailDomainValue.endsWith(`.${domain}`);
}

/** The provider for an email: an exact domain match first, then a subdomain match. */
export function selectProvider<T extends { domain: string; providerId: string }>(
  rows: T[],
  email: string,
): T | undefined {
  const domain = emailDomain(email);
  if (!domain) return undefined;
  const sorted = [...rows].sort((a, b) => (a.providerId < b.providerId ? -1 : 1));
  const domainsOf = (row: T) =>
    splitList(row.domain)
      .map((d) => d.toLowerCase())
      .filter(Boolean);
  return (
    sorted.find((row) => domainsOf(row).includes(domain)) ??
    sorted.find((row) => domainsOf(row).some((d) => domainMatches(domain, d)))
  );
}
