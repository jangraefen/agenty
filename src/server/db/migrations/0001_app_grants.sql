-- The runtime role may use schema app and read/write tables and sequences agenty_owner creates
-- there. It cannot create objects, and owns nothing, so RLS always applies to it.
GRANT USAGE ON SCHEMA "app" TO agenty_app;
--> statement-breakpoint
ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "app"
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO agenty_app;
--> statement-breakpoint
ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "app"
  GRANT USAGE, SELECT ON SEQUENCES TO agenty_app;
