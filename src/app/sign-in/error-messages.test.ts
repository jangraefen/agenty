import { describe, expect, it } from "vitest";
import { signInErrorMessage } from "./error-messages";

const FALLBACK = "Sign-in failed. Please try again.";

describe("signInErrorMessage", () => {
  it.each([
    ["unknown_domain", "No identity provider is configured for this email domain."],
    [
      "idp_unavailable",
      "Your organization's sign-in service is unavailable. Please try again later.",
    ],
    [
      "email_domain_mismatch",
      "Your identity provider returned an account outside its allowed email domains.",
    ],
    [
      "account_bound_to_other_provider",
      "This account signs in through a different identity provider.",
    ],
    [
      "organization_claim_missing",
      "Your account isn't assigned to an organization. Contact your administrator.",
    ],
    [
      "organization_owned_by_other_provider",
      "This organization belongs to a different identity provider.",
    ],
    [
      "organization_changed",
      "Your account belongs to a different organization. Contact your administrator.",
    ],
    ["try_again", "Sign-in could not be completed. Please try again."],
    ["invalid_request", "The sign-in request was invalid. Please try again."],
  ])("maps the known code %s to its fixed text", (code, text) => {
    expect(signInErrorMessage(code)).toBe(text);
  });

  it.each([
    "something_new",
    "unable to create session",
    "provisioning_failed",
    "<script>alert(1)</script>",
  ])("never shows unknown or IdP-provided text (%s) but the generic fallback", (code) => {
    expect(signInErrorMessage(code)).toBe(FALLBACK);
  });

  it("does not inherit object prototype keys as messages", () => {
    expect(signInErrorMessage("toString")).toBe(FALLBACK);
    expect(signInErrorMessage("__proto__")).toBe(FALLBACK);
  });

  it.each([null, undefined, ""])("shows no message without a code (%s)", (code) => {
    expect(signInErrorMessage(code)).toBeUndefined();
  });
});
