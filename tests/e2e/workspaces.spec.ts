import { type Browser, type BrowserContext, expect, type Page, test } from "@playwright/test";
import { loginAtMockIdp, uniqueIdpUser } from "./support/mock-idp";

type IdpUser = ReturnType<typeof uniqueIdpUser>;

const contexts: BrowserContext[] = [];
test.afterEach(async () => {
  await Promise.all(contexts.splice(0).map((context) => context.close()));
});

/** A fresh browser session signed in as `user`, on their personal workspace home. */
async function signedIn(browser: Browser, user: IdpUser): Promise<Page> {
  const context = await browser.newContext();
  contexts.push(context);
  const page = await context.newPage();
  await page.goto("/sign-in");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await loginAtMockIdp(page, user);
  await expect(page).toHaveURL(/\/w\/[0-9a-f-]{36}$/);
  await expect(page.getByText("Agents arrive in the next milestone")).toBeVisible();
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
  await expect(
    page.getByRole("heading", { level: 1, name: "Personal", exact: true }),
  ).toBeVisible();

  await settingsLink(page).click();
  await expect(page.getByText("This is your personal workspace.")).toBeVisible();
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
  await adaPage.getByLabel("Search users", { exact: true }).fill(bob.email);
  await adaPage.getByRole("button", { name: "Search", exact: true }).click();
  const results = adaPage.getByRole("list", { name: "Search results" });
  await results
    .getByRole("listitem")
    .filter({ hasText: bob.email })
    .getByRole("button", { name: "Invite" })
    .click();
  await expect(
    adaPage.getByRole("list", { name: "Pending invitations" }).getByText(bob.email),
  ).toBeVisible();

  // Bob accepts and sees the workspace as a member.
  await bobPage.goto("/workspaces");
  const invitation = bobPage.getByRole("listitem").filter({ hasText: team });
  await expect(invitation).toContainText("invited by Ada Admin");
  await invitation.getByRole("button", { name: "Accept", exact: true }).click();
  await expect(bobPage.getByRole("heading", { level: 1, name: team, exact: true })).toBeVisible();
  await expect(bobPage.getByText("You are a member of this workspace.")).toBeVisible();
  await settingsLink(bobPage).click();
  await expect(bobPage.getByRole("button", { name: "Leave workspace", exact: true })).toBeVisible();
  await expect(bobPage.getByLabel("Workspace name", { exact: true })).toHaveCount(0);
  await expect(bobPage.getByLabel("Search users", { exact: true })).toHaveCount(0);

  // Ada makes Bob an admin and leaves.
  await adaPage.goto(`${teamUrl}/settings`);
  const bobRow = adaPage.getByRole("listitem").filter({ hasText: bob.email });
  await bobRow.getByLabel("Role of Bob Member", { exact: true }).selectOption("admin");
  // The page looks the same afterwards, so wait for the server action's response.
  const saved = adaPage.waitForResponse((response) => response.request().method() === "POST");
  await bobRow.getByRole("button", { name: "Save role", exact: true }).click();
  await saved;
  // Bob's own view proves the change was saved (the select alone keeps what the test chose).
  await bobPage.reload();
  await expect(bobPage.getByLabel("Search users", { exact: true })).toBeVisible();
  await adaPage.getByRole("button", { name: "Leave workspace", exact: true }).click();
  await expect(adaPage).toHaveURL(/\/workspaces$/);
  await expect(adaPage.getByRole("link", { name: team, exact: true })).toHaveCount(0);

  // Ada no longer gets in.
  await adaPage.goto(teamUrl);
  await expect(adaPage.getByText("This page could not be found.")).toBeVisible();

  // Bob, now the only admin, can't leave; deleting needs the exact name.
  await bobPage.goto(`${teamUrl}/settings`);
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

  const settings = `${owner.url()}/settings`;
  await owner.goto(settings);
  await expect(owner.getByText("This is your personal workspace.")).toBeVisible();
  await stranger.goto(settings);
  await expect(stranger.getByText("This page could not be found.")).toBeVisible();
  await expect(stranger.getByText("This is your personal workspace.")).toHaveCount(0);
});
