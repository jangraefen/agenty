// The Content-Security-Policy of the built frontend. The API token lives in
// localStorage, so the page runs only its own scripts and talks only to the
// API: injected markup can neither run code nor send the token elsewhere.
// Inline styles are allowed for Radix's modal pieces, which lock scrolling
// with styles they compute; images and fonts come from the app only, so a
// style cannot load anything from elsewhere.
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
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; ");
}
