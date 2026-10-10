import { sql } from "drizzle-orm";
import {
  check,
  index,
  pgSchema,
  primaryKey,
  text,
  timestamp,
  unique,
  uuid,
} from "drizzle-orm/pg-core";
import { user } from "./auth-schema";

/** All Agenty tables live in schema `app`, owned by role agenty_owner. */
export const app = pgSchema("app");

export const workspaceRoles = ["admin", "member"] as const;
export type WorkspaceRole = (typeof workspaceRoles)[number];

/** A workspace; `personalUserId` is set only for the personal workspace of that user (R1). */
export const workspace = app.table(
  "workspace",
  {
    id: uuid("id").defaultRandom().primaryKey(),
    name: text("name").notNull(),
    personalUserId: uuid("personal_user_id")
      .unique()
      .references(() => user.id, { onDelete: "cascade" }),
    createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
  },
  // Backstop for the Zod check (which counts UTF-16 code units and is the stricter one).
  () => [check("workspace_name_length", sql`char_length("name") between 1 and 80`)],
);

export const workspaceMember = app.table(
  "workspace_member",
  {
    workspaceId: uuid("workspace_id")
      .notNull()
      .references(() => workspace.id, { onDelete: "cascade" }),
    userId: uuid("user_id")
      .notNull()
      .references(() => user.id, { onDelete: "cascade" }),
    role: text("role", { enum: workspaceRoles }).notNull(),
    createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
  },
  (table) => [
    primaryKey({ columns: [table.workspaceId, table.userId] }),
    index("workspace_member_user_id_idx").on(table.userId),
    check("workspace_member_role", sql`"role" in ('admin', 'member')`),
  ],
);

export const workspaceInvitation = app.table(
  "workspace_invitation",
  {
    id: uuid("id").defaultRandom().primaryKey(),
    workspaceId: uuid("workspace_id")
      .notNull()
      .references(() => workspace.id, { onDelete: "cascade" }),
    userId: uuid("user_id")
      .notNull()
      .references(() => user.id, { onDelete: "cascade" }),
    invitedByUserId: uuid("invited_by_user_id").references(() => user.id, {
      onDelete: "set null",
    }),
    createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
  },
  (table) => [
    unique("workspace_invitation_workspace_user").on(table.workspaceId, table.userId),
    index("workspace_invitation_user_id_idx").on(table.userId),
  ],
);
