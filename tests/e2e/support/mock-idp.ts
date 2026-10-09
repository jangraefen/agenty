import type { Page } from "@playwright/test";

/** Completes the mock IdP's login form (no labels; fields by name). */
export async function loginAtMockIdp(
  page: Page,
  claims: { email: string; name: string; org?: string; groups?: string[] },
) {
  await page.locator("input[name=username]").fill(claims.email);
  await page
    .locator("textarea[name=claims]")
    .fill(JSON.stringify({ ...claims, email_verified: true }));
  await page.getByRole("button", { name: "Sign-in" }).click();
}

const run = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
/** Per-run names, so repeated runs against the same dev database never collide. */
export const uniqueEmail = (prefix: string, domain: string) => `${prefix}-${run}@${domain}`;
export const uniqueOrg = (prefix: string) => `${prefix}-${run}`;
