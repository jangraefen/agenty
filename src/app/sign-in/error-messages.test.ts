import { describe, expect, it } from "vitest";
import { signInErrorLogLine, signInErrorMessage } from "./error-messages";

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

describe("signInErrorLogLine", () => {
  it("logs the code and the description on one line", () => {
    expect(signInErrorLogLine("access_denied", "User cancelled")).toBe(
      "Sign-in error: access_denied (User cancelled)",
    );
    expect(signInErrorLogLine("access_denied")).toBe("Sign-in error: access_denied");
  });

  it("removes newlines and other control characters, so IdP text cannot forge log lines", () => {
    const line = signInErrorLogLine(
      "bad\r\ncode",
      "x\nSign-in error: forged\u2028\u0000\u001b[31m\u202e",
    );

    expect(line).toBe("Sign-in error: badcode (xSign-in error: forged[31m)");
  });

  it("cuts code and description to 200 characters each", () => {
    const line = signInErrorLogLine("c".repeat(500), "d".repeat(500));

    expect(line).toBe(`Sign-in error: ${"c".repeat(200)} (${"d".repeat(200)})`);
  });
});
