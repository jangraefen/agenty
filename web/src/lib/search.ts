// The router's search parameters are plain strings. The router's default
// parser reads values as JSON, which turns a harness named 123 or true into
// a number or a boolean; every search value of this app is a string.

export function parseSearch(search: string): Record<string, string> {
  return Object.fromEntries(new URLSearchParams(search));
}

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
