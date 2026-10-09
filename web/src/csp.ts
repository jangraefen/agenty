// Imported by vite.config.ts, whose build plugin writes the policy into
// index.html as a meta element; csp.test.ts checks it.
//
// The Content-Security-Policy of the built frontend. The API token lives in
// localStorage, so the page runs only its own scripts and talks only to the
// API: injected markup can neither run code nor send the token elsewhere.
// Inline styles are allowed for Radix's modal pieces, which lock scrolling
// with styles they compute. As stylesheets, images and fonts load only from
// the app itself, an injected style cannot send data to another origin; it
// could still restyle the page, approvals included, misleadingly, or request
// the app's own URLs, so no field may hold a secret in its value attribute.
// A nonce or hash in style-src would make browsers ignore 'unsafe-inline',
// and Radix's styles would need setNonce.
/**
 * Returns the policy for a frontend that calls the API at apiUrl. Only the
 * API's origin is allowed for connections, so the URL must parse and be
 * http or https: anything else fails the build instead of producing a
 * policy that allows an unintended source.
 */
export function contentSecurityPolicy(apiUrl: string): string {
  const api = new URL(apiUrl);
  if (api.protocol !== "http:" && api.protocol !== "https:") {
    throw new Error(`the API URL ${apiUrl} must be http or https`);
  }
  return [
    "default-src 'none'",
    "script-src 'self'",
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:",
    "font-src 'self'",
    `connect-src ${api.origin}`,
    // No <base> and no form submission: injected markup can neither redirect
    // the app's relative URLs nor post a form, with what it holds, elsewhere.
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; ");
}
