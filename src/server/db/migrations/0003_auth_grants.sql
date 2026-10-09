GRANT USAGE ON SCHEMA "auth" TO agenty_app;
--> statement-breakpoint
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA "auth" TO agenty_app;
--> statement-breakpoint
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA "auth" TO agenty_app;
--> statement-breakpoint
ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "auth"
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO agenty_app;
--> statement-breakpoint
ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "auth"
  GRANT USAGE, SELECT ON SEQUENCES TO agenty_app;
