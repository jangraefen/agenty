import { describe, expect, it } from "vitest";
import { withError } from "./run-action";

describe("error redirects", () => {
  it("add the error code to a relative path", () => {
    expect(withError("/workspaces", "last_admin")).toBe("/workspaces?error=last_admin");
  });

  it("keep other query parameters and replace an earlier error", () => {
    expect(withError("/w/x/settings?q=ada%20l&error=old", "already_invited")).toBe(
      "/w/x/settings?q=ada+l&error=already_invited",
    );
  });
});
