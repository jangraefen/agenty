import { pgSchema } from "drizzle-orm/pg-core";

/** All Agenty tables live in schema `app`, owned by role agenty_owner. */
export const app = pgSchema("app");
