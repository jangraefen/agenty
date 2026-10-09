const MESSAGES: Record<string, string> = {
  unknown_domain: "No identity provider is configured for this email domain.",
  idp_unavailable: "Your organization's sign-in service is unavailable. Please try again later.",
  email_domain_mismatch:
    "Your identity provider returned an account outside its allowed email domains.",
  account_bound_to_other_provider: "This account signs in through a different identity provider.",
  organization_claim_missing:
    "Your account isn't assigned to an organization. Contact your administrator.",
  organization_owned_by_other_provider:
    "This organization belongs to a different identity provider.",
  organization_changed:
    "Your account belongs to a different organization. Contact your administrator.",
  try_again: "Sign-in could not be completed. Please try again.",
  invalid_request: "The sign-in request was invalid. Please try again.",
};
const FALLBACK = "Sign-in failed. Please try again.";

/** Maps an error code from the URL or API to fixed text; never shows IdP-provided text. */
export function signInErrorMessage(code: string | null | undefined): string | undefined {
  if (!code) return undefined;
  return Object.hasOwn(MESSAGES, code) ? MESSAGES[code] : FALLBACK;
}
