const MESSAGES: Record<string, string> = {
  sign_in_unavailable: "Sign-in is temporarily unavailable. Please try again later.",
  access_denied: "Sign-in was cancelled.",
};
const FALLBACK = "Sign-in failed. Please try again.";

/** Maps an error code from the URL or API to fixed text; never shows IdP-provided text. */
export function signInErrorMessage(code: string | null | undefined): string | undefined {
  if (!code) return undefined;
  return Object.hasOwn(MESSAGES, code) ? MESSAGES[code] : FALLBACK;
}
