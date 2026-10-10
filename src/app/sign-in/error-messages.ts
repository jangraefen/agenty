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

const LOG_VALUE_LIMIT = 200;

/**
 * The server log line for a sign-in error from the URL. Both values may be IdP or attacker text:
 * control, format and line-separator characters are removed (log injection) and each value is
 * cut to 200 characters.
 */
export function signInErrorLogLine(code: string, description?: string | undefined): string {
  const clean = (value: string) =>
    value.replace(/[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/gu, "").slice(0, LOG_VALUE_LIMIT);
  if (!description) return `Sign-in error: ${clean(code)}`;
  return `Sign-in error: ${clean(code)} (${clean(description)})`;
}
