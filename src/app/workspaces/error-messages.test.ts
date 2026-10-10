import { describe, expect, it } from "vitest";
import { workspaceErrorMessage } from "./error-messages";

describe("workspace error messages", () => {
  it("are fixed text per code", () => {
    expect(workspaceErrorMessage("last_admin")).toBe(
      "A workspace needs at least one admin. Make someone else an admin first, or delete the workspace.",
    );
  });

  it("fall back to a generic message for unknown codes, never echoing them", () => {
    expect(workspaceErrorMessage("<script>")).toBe("Something went wrong. Please try again.");
  });

  it("are absent without a code", () => {
    expect(workspaceErrorMessage(undefined)).toBeUndefined();
  });
});
