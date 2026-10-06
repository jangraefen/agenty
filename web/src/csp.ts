// The Content-Security-Policy of the built frontend. The API token lives in
// localStorage, so the page runs only its own scripts and talks only to the
// API: injected markup can neither run code nor send the token elsewhere.
export function contentSecurityPolicy(apiUrl: string): string {
  const api = new URL(apiUrl);
  if (api.protocol !== "http:" && api.protocol !== "https:") {
    throw new Error(`the API URL ${apiUrl} must be http or https`);
  }
  return [
    "default-src 'none'",
    "script-src 'self'",
    "style-src 'self'",
    "img-src 'self' data:",
    "font-src 'self'",
    `connect-src ${api.origin}`,
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; ");
}
