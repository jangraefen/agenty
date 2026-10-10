// Creates users directly in the database for workspace tests and removes them (and every
// workspace they belong to) afterwards.
import { randomUUID } from "node:crypto";
import { inArray } from "drizzle-orm";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { workspace, workspaceMember } from "@/server/db/schema";

export function testUsers() {
  const ids: string[] = [];
  return {
    async create(name = "Test User") {
      const id = randomUUID();
      const email = `w-${id}@example.test`;
      await getDb().insert(user).values({ id, name, email });
      ids.push(id);
      return { id, name, email };
    },
    async cleanup() {
      if (ids.length === 0) return;
      const db = getDb();
      await db
        .delete(workspace)
        .where(
          inArray(
            workspace.id,
            db
              .select({ id: workspaceMember.workspaceId })
              .from(workspaceMember)
              .where(inArray(workspaceMember.userId, ids)),
          ),
        );
      await db.delete(user).where(inArray(user.id, ids));
    },
  };
}
