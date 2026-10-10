import type { Page } from "@playwright/test";

/** Completes the mock IdP's login form (no labels; fields by name). `sub` is the username. */
export async function loginAtMockIdp(
  page: Page,
  { sub, email, name }: { sub: string; email: string; name: string },
) {
  await page.locator("input[name=username]").fill(sub);
  await page
    .locator("textarea[name=claims]")
    .fill(JSON.stringify({ email, name, email_verified: true }));
  await page.getByRole("button", { name: "Sign-in" }).click();
}

const run = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
/** A per-run IdP user, so repeated runs against the same dev database never collide. */
export const uniqueIdpUser = (prefix: string, name: string) => ({
  sub: `${prefix}-${run}`,
  email: `${prefix}-${run}@corp.test`,
  name,
});
