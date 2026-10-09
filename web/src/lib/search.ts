/**
 * The router's search parameters are plain strings. The router's default
 * parser reads values as JSON, which turns a harness named 123 or true into
 * a number or a boolean; every search value of this app is a string.
 *
 * router.tsx installs these two functions in place of the defaults; routes
 * then validate the strings they read (the audit filters check a status
 * with isRunStatus, for one).
 */

/** Reads a query string, with or without its "?", as a record of strings; a repeated key keeps its last value. */
export function parseSearch(search: string): Record<string, string> {
  return Object.fromEntries(new URLSearchParams(search));
}

/**
 * Writes search parameters as a query string, "" when there are none.
 * Undefined values are left out, so a cleared filter leaves the URL; any
 * other non-string throws, so a route that put one in is caught in tests
 * rather than written as JSON.
 */
export function stringifySearch(search: Record<string, unknown>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(search)) {
    if (value === undefined) {
      continue;
    }
    if (typeof value !== "string") {
      throw new Error(`search parameter ${key} must be a string`);
    }
    params.set(key, value);
  }
  const text = params.toString();
  return text === "" ? "" : `?${text}`;
}
