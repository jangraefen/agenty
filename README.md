# Agenty

Self-hosted, multi-tenant AI agent builder with chat.

## Requirements

Node 24, pnpm, [go-task](https://taskfile.dev), Docker, and [opa](https://www.openpolicyagent.org/docs#running-opa) v1.21.1 (`brew install opa`).

## Quick start

    task setup     # tool check, dependencies, Playwright browser, .env
    task dev       # Postgres + dev server on http://localhost:3000 (migrates on start)

`task --list` shows all commands; `CLAUDE.md` describes the architecture and conventions.

## Production image

    task docker:build
    docker run -p 3000:3000 -e DATABASE_URL=... -e DATABASE_MIGRATION_URL=... agenty:local

The server applies pending database migrations on start (as the role in `DATABASE_MIGRATION_URL`)
and refuses to start if they fail. Several instances may start at once; they migrate one after another.

The database needs the roles from `docker/postgres/init.sql`. Create them with your own passwords: the values in that file are local-development defaults only.

## Troubleshooting

- **Port 5432 is already in use:** set `POSTGRES_PORT` in `.env` to a free port and use the same
  port in `DATABASE_URL` and `DATABASE_MIGRATION_URL`. Then run `task db:up` again.
- **`Database not reachable (ECONNREFUSED)`:** run `task db:up`.
- **Roles are missing after editing `docker/postgres/init.sql`:** it only runs on an empty volume;
  run `task db:reset` (deletes local data) and `task db:up`.
