import { describe, expect, it } from "vitest";
import { signInErrorMessage } from "./error-messages";

const FALLBACK = "Sign-in failed. Please try again.";

describe("signInErrorMessage", () => {
  it.each([
    ["sign_in_unavailable", "Sign-in is temporarily unavailable. Please try again later."],
    ["access_denied", "Sign-in was cancelled."],
  ])("maps the known code %s to its fixed text", (code, text) => {
    expect(signInErrorMessage(code)).toBe(text);
  });

  it.each([
    "something_new",
    "state_mismatch",
    "The user denied access to the application",
    "<script>alert(1)</script>",
  ])("never shows unknown or IdP-provided text (%s) but the generic fallback", (code) => {
    expect(signInErrorMessage(code)).toBe(FALLBACK);
  });

  it("does not inherit object prototype keys as messages", () => {
    expect(signInErrorMessage("toString")).toBe(FALLBACK);
    expect(signInErrorMessage("__proto__")).toBe(FALLBACK);
    expect(signInErrorMessage("constructor")).toBe(FALLBACK);
  });

  it.each([null, undefined, ""])("shows no message without a code (%s)", (code) => {
    expect(signInErrorMessage(code)).toBeUndefined();
  });
});
