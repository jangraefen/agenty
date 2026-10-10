import { describe, expect, it } from "vitest";
import {
  likePattern,
  userSearchQuerySchema,
  workspaceIdSchema,
  workspaceNameSchema,
  workspaceRoleSchema,
} from "./validation";

describe("workspace names", () => {
  it("are trimmed", () => {
    expect(workspaceNameSchema.parse("  Team  ")).toBe("Team");
  });

  it("must not be empty after trimming", () => {
    expect(workspaceNameSchema.safeParse("   ").success).toBe(false);
  });

  it("allow at most 80 characters", () => {
    expect(workspaceNameSchema.safeParse("a".repeat(80)).success).toBe(true);
    expect(workspaceNameSchema.safeParse("a".repeat(81)).success).toBe(false);
  });
});

describe("user search queries", () => {
  it("need 2 to 100 characters after trimming", () => {
    expect(userSearchQuerySchema.safeParse(" a ").success).toBe(false);
    expect(userSearchQuerySchema.parse(" ab ")).toBe("ab");
    expect(userSearchQuerySchema.safeParse("a".repeat(101)).success).toBe(false);
  });

  it("match wildcards literally", () => {
    expect(likePattern("50%_off\\")).toBe("%50\\%\\_off\\\\%");
  });
});

describe("ids and roles", () => {
  it("reject malformed workspace ids before any query", () => {
    expect(workspaceIdSchema.safeParse("not-a-uuid").success).toBe(false);
    expect(workspaceIdSchema.safeParse("6f1c1f7e-3d4b-4c55-9a43-1b2a5c6d7e8f").success).toBe(true);
  });

  it("know only admin and member", () => {
    expect(workspaceRoleSchema.safeParse("admin").success).toBe(true);
    expect(workspaceRoleSchema.safeParse("owner").success).toBe(false);
  });
});
