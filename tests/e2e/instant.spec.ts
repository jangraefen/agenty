import { instant } from "@next/playwright";
import { expect, type Page, test } from "@playwright/test";
import { loginAtMockIdp, uniqueIdpUser } from "./support/mock-idp";

// Instant navigation: inside `instant()`, a page shows only its static shell (initial load) or the
// prefetched UI (client navigation); request-time content waits until the callback ends. A page
// that reads request-time data outside a Suspense boundary of its own (its loading.tsx) blocks the
// navigation, so the URL wait in the callback times out. Needs a build with the testing API
// (`task build` sets EXPOSE_TESTING_API=1, see next.config.ts).

const sidebar = (page: Page) => page.getByRole("navigation", { name: "Main", exact: true });
const workspaceNav = (page: Page) =>
  page.getByRole("navigation", { name: "Workspace", exact: true });
const heading = (page: Page, name: string) =>
  page.getByRole("heading", { level: 1, name, exact: true });
const homeCard = (page: Page) =>
  page.getByRole("main").getByText("Agents arrive in the next milestone");

/** A blocked client navigation never commits inside `instant()`: fail fast with a clear message. */
const navigationTimeout = 5_000;
const workspaceUrl = /\/w\/[0-9a-f-]{36}$/;
let personalUrl = "";
let teamUrl = "";
let team = "";
let users = 0;

test.beforeEach(async ({ page }) => {
  const user = uniqueIdpUser(`instant${++users}`, "Ina Instant");
  await page.goto("/sign-in");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await loginAtMockIdp(page, user);
  await expect(page).toHaveURL(workspaceUrl);
  await expect(homeCard(page)).toBeVisible();
  personalUrl = new URL(page.url()).pathname;
  // A shared workspace: the personal one has no settings page.
  team = `Instant ${user.sub}`;
  await page.goto("/workspaces");
  await page.getByLabel("Name", { exact: true }).fill(team);
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(heading(page, team)).toBeVisible();
  teamUrl = new URL(page.url()).pathname;
});

test.describe("initial page loads show the static shell first", () => {
  for (const [path, title] of [
    ["/workspaces", "Workspaces · Agenty"],
    ["team", "Agenty"],
    ["team/settings", "Workspace settings · Agenty"],
  ] as const) {
    test(path, async ({ page, baseURL }) => {
      const url = path.replace("team", teamUrl);
      let shellX: number | undefined;
      await instant(
        page,
        async () => {
          await page.goto(url);
          await expect(page).toHaveTitle(title);
          await expect(page.getByRole("main")).toBeAttached();
          // The sidebar frame is in the static shell, so it reserves its space from the start...
          await expect(sidebar(page)).toBeVisible();
          shellX = (await page.getByRole("main").boundingBox())?.x;
          // ...while request-time content (session, workspace) is held back.
          await expect(sidebar(page).getByRole("button")).toHaveCount(0);
          await expect(page.getByRole("main").getByRole("heading")).toBeHidden();
        },
        { baseURL },
      );
      await expect(sidebar(page).getByRole("button", { name: /^Workspace\b/ })).toBeVisible();
      await expect(page.getByRole("main").getByRole("heading").first()).toBeVisible();
      // No layout shift: the content stays where the static shell put it.
      expect(shellX).toBeGreaterThan(0);
      expect((await page.getByRole("main").boundingBox())?.x).toBe(shellX);
    });
  }

  test("/ (redirects to the personal workspace once request-time data streams)", async ({
    page,
    baseURL,
  }) => {
    await instant(
      page,
      async () => {
        await page.goto("/");
        await expect(page).toHaveTitle("Agenty");
        await expect(page.getByRole("main")).toBeAttached();
        await expect(page.getByRole("main").getByRole("heading")).toBeHidden();
      },
      { baseURL },
    );
    await expect(page).toHaveURL(personalUrl);
    await expect(homeCard(page)).toBeVisible();
  });
});

test("/w (where sign-in lands; redirects inside the signed-in layout)", async ({
  page,
  baseURL,
}) => {
  await instant(
    page,
    async () => {
      await page.goto("/w");
      // The sidebar frame is there from the first paint, before the redirect streams.
      await expect(sidebar(page)).toBeVisible();
    },
    { baseURL },
  );
  await expect(page).toHaveURL(personalUrl);
  await expect(homeCard(page)).toBeVisible();
});

test.describe("client navigations commit at once and keep the sidebar", () => {
  test("into /workspaces", async ({ page }) => {
    await page.goto(teamUrl);
    await expect(heading(page, team)).toBeVisible();
    await instant(page, async () => {
      await sidebar(page)
        .getByRole("button", { name: /^Workspace\b/ })
        .click();
      await page.getByRole("menuitem", { name: /^All workspaces & invitations/ }).click();
      await page.waitForURL((url) => url.pathname === "/workspaces", {
        timeout: navigationTimeout,
      });
      // The previous page is gone (a blocked navigation would keep showing it)...
      await expect(workspaceNav(page)).toBeHidden();
      // ...the sidebar stays, and request-time content is held back.
      await expect(sidebar(page)).toBeVisible();
      await expect(heading(page, "Workspaces")).toBeHidden();
    });
    await expect(heading(page, "Workspaces")).toBeVisible();
  });

  test("into /w/[workspaceId]", async ({ page }) => {
    await page.goto("/workspaces");
    await expect(heading(page, "Workspaces")).toBeVisible();
    await instant(page, async () => {
      await page.getByRole("link", { name: team, exact: true }).click();
      await page.waitForURL((url) => url.pathname === teamUrl, { timeout: navigationTimeout });
      await expect(heading(page, "Workspaces")).toBeHidden();
      await expect(sidebar(page)).toBeVisible();
      await expect(workspaceNav(page)).toBeHidden();
    });
    await expect(heading(page, team)).toBeVisible();
    await expect(homeCard(page)).toBeVisible();
  });

  test("into /w/[workspaceId] from its settings page (shared workspace layout)", async ({
    page,
  }) => {
    await page.goto(`${teamUrl}/settings`);
    await expect(page.getByRole("heading", { name: "Settings", exact: true })).toBeVisible();
    await instant(page, async () => {
      await workspaceNav(page).getByRole("link", { name: "Home", exact: true }).click();
      await page.waitForURL((url) => url.pathname === teamUrl, { timeout: navigationTimeout });
      await expect(page.getByRole("heading", { name: "Settings", exact: true })).toBeHidden();
      await expect(sidebar(page)).toBeVisible();
      await expect(heading(page, team)).toBeVisible();
      await expect(homeCard(page)).toBeHidden();
    });
    await expect(homeCard(page)).toBeVisible();
  });

  test("into /w/[workspaceId]/settings", async ({ page }) => {
    await page.goto(teamUrl);
    await expect(homeCard(page)).toBeVisible();
    await instant(page, async () => {
      await workspaceNav(page).getByRole("link", { name: "Settings", exact: true }).click();
      await page.waitForURL((url) => url.pathname === `${teamUrl}/settings`, {
        timeout: navigationTimeout,
      });
      await expect(homeCard(page)).toBeHidden();
      // The workspace layout is shared with the previous page and stays.
      await expect(sidebar(page)).toBeVisible();
      await expect(heading(page, team)).toBeVisible();
      await expect(page.getByRole("heading", { name: "Settings", exact: true })).toBeHidden();
    });
    await expect(page.getByRole("heading", { name: "Settings", exact: true })).toBeVisible();
  });

  test("into the personal workspace (the sidebar's app name)", async ({ page }) => {
    await page.goto("/workspaces");
    await expect(heading(page, "Workspaces")).toBeVisible();
    await instant(page, async () => {
      await sidebar(page).getByRole("link", { name: "Agenty", exact: true }).click();
      await page.waitForURL((url) => url.pathname === personalUrl, {
        timeout: navigationTimeout,
      });
      await expect(heading(page, "Workspaces")).toBeHidden();
      await expect(sidebar(page)).toBeVisible();
    });
    await expect(homeCard(page)).toBeVisible();
  });
});
