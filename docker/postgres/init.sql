-- Creates Agenty's database roles. Runs once as the superuser: by the postgres image on an
-- empty data directory, and by CI via psql. Passwords are local-development values.
-- Schemas and grants are created by migrations (src/server/db/migrations), not here.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'agenty_owner') THEN
    CREATE ROLE agenty_owner LOGIN PASSWORD 'agenty_owner'
      NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'agenty_app') THEN
    CREATE ROLE agenty_app LOGIN PASSWORD 'agenty_app'
      NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  END IF;
END
$$;

-- The migrator runs CREATE SCHEMA IF NOT EXISTS, which needs CREATE on the database.
SELECT format('GRANT CREATE ON DATABASE %I TO agenty_owner', current_database()) \gexec
