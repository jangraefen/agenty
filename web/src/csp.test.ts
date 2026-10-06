import { expect, test } from "vitest";
import { contentSecurityPolicy } from "./csp";

test("the policy allows only the app's own code and calls to the API", () => {
  const policy = contentSecurityPolicy("https://agenty.example.com/base/");

  expect(policy.split("; ")).toEqual([
    "default-src 'none'",
    "script-src 'self'",
    "style-src 'self'",
    "img-src 'self' data:",
    "font-src 'self'",
    "connect-src https://agenty.example.com",
    "base-uri 'none'",
    "form-action 'none'",
  ]);
});

test("the policy refuses an API URL that is not http or https", () => {
  expect(() => contentSecurityPolicy("javascript:alert(1)")).toThrow(/http or https/);
});
