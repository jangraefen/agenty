import {
  type Browser,
  type BrowserContext,
  type BrowserContextOptions,
  expect,
  type Page,
  test,
} from "@playwright/test";
import { loginAtMockIdp, uniqueIdpUser } from "./support/mock-idp";

type IdpUser = ReturnType<typeof uniqueIdpUser>;

const contexts: BrowserContext[] = [];
test.afterEach(async () => {
  await Promise.all(contexts.splice(0).map((context) => context.close()));
});

/** A fresh browser session signed in as `user`, on their personal workspace home. */
async function signedIn(
  browser: Browser,
  user: IdpUser,
  options: BrowserContextOptions = {},
): Promise<Page> {
  const context = await browser.newContext(options);
  contexts.push(context);
  const page = await context.newPage();
  await page.goto("/sign-in");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await loginAtMockIdp(page, user);
  await expect(page).toHaveURL(/\/w\/[0-9a-f-]{36}$/);
  // Scoped to <main>: streamed content briefly sits in a hidden copy at the end of <body>.
  await expect(
    page.getByRole("main").getByText("Agents arrive in the next milestone"),
  ).toBeVisible();
  return page;
}

const alertWith = (page: Page, text: string) => page.getByRole("alert").filter({ hasText: text });
const settingsLink = (page: Page) =>
  page
    .getByRole("navigation", { name: "Workspace" })
    .getByRole("link", { name: "Settings", exact: true });

test("the first sign-in lands in a personal workspace that can't be changed", async ({
  browser,
}) => {
  const page = await signedIn(browser, uniqueIdpUser("wspers", "Pat Personal"));
  const home = page.url();
  await expect(
    page.getByRole("heading", { level: 1, name: "Personal", exact: true }),
  ).toBeVisible();

  // Settings is a disabled link whose tooltip explains why, on hover and on keyboard focus.
  const settings = settingsLink(page);
  await expect(settings).toHaveAttribute("aria-disabled", "true");
  await expect(settings).not.toHaveAttribute("href");
  const reason = page.getByText("Your personal workspace can't be changed.", { exact: true });
  // Retried: an interaction before hydration opens no tooltip.
  await expect(async () => {
    await settings.hover();
    await expect(reason).toBeVisible({ timeout: 1_000 });
  }).toPass();
  await page.mouse.move(0, 0);
  await expect(reason).toHaveCount(0);
  await settings.focus();
  await expect(reason).toBeVisible();

  // A direct visit to its settings goes back to the workspace home.
  await page.goto(`${home}/settings`);
  await expect(page).toHaveURL(home);
  await expect(page.getByRole("button", { name: "Delete workspace", exact: true })).toHaveCount(0);
  await expect(page.getByLabel("Search users", { exact: true })).toHaveCount(0);
  await expect(page.getByLabel("Workspace name", { exact: true })).toHaveCount(0);
});

test("admins share a workspace with an invited user", async ({ browser }) => {
  const ada = uniqueIdpUser("wsada", "Ada Admin");
  const bob = uniqueIdpUser("wsbob", "Bob Member");
  const bobPage = await signedIn(browser, bob); // Bob needs an account before he can be invited.
  const adaPage = await signedIn(browser, ada);
  const team = `Team ${ada.sub}`;

  // Ada creates a workspace, renames it and invites Bob.
  await adaPage.goto("/workspaces");
  await adaPage.getByLabel("Name", { exact: true }).fill(`Draft ${ada.sub}`);
  await adaPage.getByRole("button", { name: "Create", exact: true }).click();
  await expect(
    adaPage.getByRole("heading", { level: 1, name: `Draft ${ada.sub}`, exact: true }),
  ).toBeVisible();
  const teamUrl = adaPage.url();
  await settingsLink(adaPage).click();
  await adaPage.getByLabel("Workspace name", { exact: true }).fill(team);
  await adaPage.getByRole("button", { name: "Rename", exact: true }).click();
  await expect(adaPage.getByRole("heading", { level: 1, name: team, exact: true })).toBeVisible();
  // The search runs as Ada types: Bob can be invited, Ada herself is listed as a member.
  const search = adaPage.getByLabel("Search users", { exact: true });
  const results = adaPage.getByRole("list", { name: "Search results" });
  await search.fill(ada.email);
  const adaResult = results.getByRole("listitem").filter({ hasText: ada.email });
  await expect(adaResult).toContainText("Member");
  await expect(adaResult.getByRole("button", { name: "Invite" })).toHaveCount(0);
  await search.fill(bob.email);
  const bobResult = results.getByRole("listitem").filter({ hasText: bob.email });
  await bobResult.getByRole("button", { name: "Invite", exact: true }).click();
  await expect(
    adaPage.getByRole("list", { name: "Pending invitations" }).getByText(bob.email),
  ).toBeVisible();
  await expect(bobResult).toContainText("Invited");
  await expect(bobResult.getByRole("button", { name: "Invite" })).toHaveCount(0);

  // Bob accepts and sees the workspace as a member.
  await bobPage.goto("/workspaces");
  const invitation = bobPage.getByRole("listitem").filter({ hasText: team });
  await expect(invitation).toContainText("invited by Ada Admin");
  await invitation.getByRole("button", { name: "Accept", exact: true }).click();
  await expect(bobPage.getByRole("heading", { level: 1, name: team, exact: true })).toBeVisible();
  await expect(bobPage.getByText("You are a member of this workspace.")).toBeVisible();
  await settingsLink(bobPage).click();
  await expect(bobPage.getByRole("button", { name: "Leave workspace", exact: true })).toBeVisible();
  const members = bobPage.getByRole("table", { name: "Members" });
  await expect(members.getByRole("row").filter({ hasText: ada.email })).toContainText("Admin");
  await expect(members.getByRole("button")).toHaveCount(0);
  await expect(members.getByRole("combobox")).toHaveCount(0);
  await expect(bobPage.getByRole("button", { name: "Delete workspace", exact: true })).toHaveCount(
    0,
  );
  await expect(bobPage.getByLabel("Workspace name", { exact: true })).toHaveCount(0);
  await expect(bobPage.getByLabel("Search users", { exact: true })).toHaveCount(0);

  // Ada makes Bob an admin and leaves.
  await adaPage.goto(`${teamUrl}/settings`);
  const bobRow = adaPage
    .getByRole("table", { name: "Members" })
    .getByRole("row")
    .filter({ hasText: bob.email });
  await expect(bobRow.getByRole("button", { name: "Remove", exact: true })).toBeVisible();
  // Choosing a role saves it at once; there is no save button.
  await expect(bobRow.getByRole("button", { name: /^Save/ })).toHaveCount(0);
  const bobRole = bobRow.getByLabel("Role of Bob Member", { exact: true });
  // The page looks the same afterwards, so wait for the server action's response.
  const saved = adaPage.waitForResponse((response) => response.request().method() === "POST");
  await bobRole.click();
  await adaPage.getByRole("option", { name: "Admin", exact: true }).click();
  await saved;
  await expect(bobRole).toContainText("Admin");
  await expect(bobRole).toBeEnabled();
  // Bob's own view proves the change was saved (the select alone shows what the test chose).
  await bobPage.reload();
  await expect(bobPage.getByLabel("Search users", { exact: true })).toBeVisible();
  await adaPage.getByRole("button", { name: "Leave workspace", exact: true }).click();
  await expect(adaPage).toHaveURL(/\/workspaces$/);
  await expect(adaPage.getByRole("link", { name: team, exact: true })).toHaveCount(0);

  // Ada no longer gets in.
  await adaPage.goto(teamUrl);
  await expect(notFoundHeading(adaPage)).toBeVisible();

  // Bob, now the only admin, can't demote himself or leave; deleting needs the exact name.
  await bobPage.goto(`${teamUrl}/settings`);
  const ownRole = bobPage
    .getByRole("table", { name: "Members" })
    .getByLabel("Role of Bob Member", { exact: true });
  await ownRole.click();
  await bobPage.getByRole("option", { name: "Member", exact: true }).click();
  await expect(alertWith(bobPage, "A workspace needs at least one admin.")).toBeVisible();
  await expect(ownRole).toContainText("Admin");
  await expect(ownRole).toBeEnabled();
  await bobPage.reload();
  await expect(ownRole).toContainText("Admin");
  await bobPage.getByRole("button", { name: "Leave workspace", exact: true }).click();
  await expect(alertWith(bobPage, "A workspace needs at least one admin.")).toBeVisible();
  await bobPage.getByLabel("Type the workspace name to confirm", { exact: true }).fill("wrong");
  await bobPage.getByRole("button", { name: "Delete workspace", exact: true }).click();
  await expect(alertWith(bobPage, "Type the workspace name exactly to delete it.")).toBeVisible();
  await bobPage.getByLabel("Type the workspace name to confirm", { exact: true }).fill(team);
  await bobPage.getByRole("button", { name: "Delete workspace", exact: true }).click();
  await expect(bobPage).toHaveURL(/\/workspaces$/);
  await expect(bobPage.getByRole("link", { name: team, exact: true })).toHaveCount(0);
});

test("non-members see the not-found page", async ({ browser }) => {
  const owner = await signedIn(browser, uniqueIdpUser("wsown", "Olive Owner"));
  const stranger = await signedIn(browser, uniqueIdpUser("wsstr", "Sam Stranger"));

  const home = owner.url();
  await owner.goto(`${home}/settings`);
  await expect(owner).toHaveURL(home);
  // The not-found page renders inside the signed-in layout, so the sidebar stays.
  await stranger.goto(`${home}/settings`);
  await expect(notFoundHeading(stranger)).toBeVisible();
  await expect(sidebar(stranger)).toBeVisible();
  await stranger.goto(home);
  await expect(notFoundHeading(stranger)).toBeVisible();
  await expect(sidebar(stranger)).toBeVisible();
  await stranger.getByRole("main").getByRole("link", { name: "Go to your workspaces" }).click();
  await expect(stranger).toHaveURL(/\/workspaces$/);
});

const notFoundHeading = (page: Page) =>
  page.getByRole("heading", { level: 1, name: "Page not found", exact: true });
const sidebar = (page: Page) => page.getByRole("navigation", { name: "Main", exact: true });
const switcher = (page: Page) => sidebar(page).getByRole("button", { name: /^Workspace\b/ });

test("the sidebar's switcher lists the user's workspaces and switches between them", async ({
  browser,
}) => {
  const user = uniqueIdpUser("wssw", "Sue Switcher");
  const page = await signedIn(browser, user);
  const personalUrl = page.url();
  const team = `Switch ${user.sub}`;
  await page.goto("/workspaces");
  await page.getByLabel("Name", { exact: true }).fill(team);
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: team, exact: true })).toBeVisible();
  const teamUrl = page.url();

  // The trigger names the workspace in the URL; the menu marks it as the current one.
  await expect(switcher(page)).toContainText(team);
  await switcher(page).click();
  const items = page.getByRole("menuitem");
  await expect(items.nth(0)).toHaveText("Personal");
  await expect(items.filter({ hasText: team })).toHaveAttribute("aria-current", "page");
  await items.filter({ hasText: "Personal" }).click();
  await expect(page).toHaveURL(personalUrl);
  await expect(
    page.getByRole("heading", { level: 1, name: "Personal", exact: true }),
  ).toBeVisible();
  await expect(switcher(page)).toContainText("Personal");

  // Keyboard: open the menu, go to the team workspace and follow it.
  await switcher(page).focus();
  await page.keyboard.press("Enter");
  await expect(items.filter({ hasText: team })).toBeVisible();
  await items.filter({ hasText: team }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(teamUrl);
  await expect(page.getByRole("heading", { level: 1, name: team, exact: true })).toBeVisible();

  // The other entries lead to the workspace list and its create form.
  await switcher(page).click();
  await page.getByRole("menuitem", { name: "Create workspace", exact: true }).click();
  await expect(page).toHaveURL(/\/workspaces#create-workspace$/);
  await expect(page.getByLabel("Name", { exact: true })).toBeVisible();

  // Outside a workspace the trigger names none and the menu marks none.
  await expect(switcher(page)).toContainText("Workspaces");
  await expect(switcher(page)).not.toContainText("Personal");
  await switcher(page).click();
  await expect(items.filter({ hasText: team })).toBeVisible();
  await expect(items.and(page.locator("[aria-current]"))).toHaveCount(0);
});

test("an invitation shows as a badge in the sidebar's switcher", async ({ browser }) => {
  const ada = uniqueIdpUser("wsbada", "Ada Badge");
  const bob = uniqueIdpUser("wsbbob", "Bob Badge");
  const bobPage = await signedIn(browser, bob);
  const adaPage = await signedIn(browser, ada);
  await expect(switcher(bobPage)).not.toContainText("pending invitation");

  await adaPage.goto("/workspaces");
  await adaPage.getByLabel("Name", { exact: true }).fill(`Badge ${ada.sub}`);
  await adaPage.getByRole("button", { name: "Create", exact: true }).click();
  await settingsLink(adaPage).click();
  await adaPage.getByLabel("Search users", { exact: true }).fill(bob.email);
  await adaPage
    .getByRole("list", { name: "Search results" })
    .getByRole("listitem")
    .filter({ hasText: bob.email })
    .getByRole("button", { name: "Invite", exact: true })
    .click();
  await expect(
    adaPage.getByRole("list", { name: "Pending invitations" }).getByText(bob.email),
  ).toBeVisible();

  await bobPage.reload();
  await expect(switcher(bobPage)).toContainText("1 pending invitation");
  await expect(switcher(bobPage)).not.toContainText("pending invitations");
  await switcher(bobPage).click();
  await bobPage.getByRole("menuitem", { name: /^All workspaces & invitations/ }).click();
  await expect(bobPage).toHaveURL(/\/workspaces$/);
  await expect(bobPage.getByText("invited by Ada Badge")).toBeVisible();
});

test("on a small screen the sidebar flies in from the top bar and closes after navigating", async ({
  browser,
}) => {
  const page = await signedIn(browser, uniqueIdpUser("wsmob", "Mo Mobile"), {
    viewport: { width: 390, height: 844 },
  });
  await expect(sidebar(page)).toBeHidden();

  // Retried: a click before hydration opens nothing.
  const open = page.getByRole("button", { name: "Open sidebar", exact: true });
  await expect(async () => {
    await open.click();
    await expect(sidebar(page)).toBeVisible({ timeout: 1_000 });
  }).toPass();
  await page.keyboard.press("Escape");
  await expect(sidebar(page)).toBeHidden();

  await open.click();
  await switcher(page).click();
  await page.getByRole("menuitem", { name: /^All workspaces & invitations/ }).click();
  await expect(page).toHaveURL(/\/workspaces$/);
  await expect(
    page.getByRole("heading", { level: 1, name: "Workspaces", exact: true }),
  ).toBeVisible();
  await expect(sidebar(page)).toBeHidden();
});
