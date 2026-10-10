# Agenty

Self-hosted AI agent builder with chat. Users sign in through your OIDC identity provider.

## Requirements

Node 24, pnpm, [go-task](https://taskfile.dev), Docker, and [opa](https://www.openpolicyagent.org/docs#running-opa) v1.21.1 (`brew install opa`).

## Quick start

    task setup     # tool check, dependencies, Playwright browser, .env
    task dev       # Postgres + mock IdP + dev server on http://localhost:3000 (migrates on start)

Sign in at the mock IdP with any username and this claims JSON:

    {"email":"alice@example.test","name":"Alice"}

`task --list` shows all commands; `AGENTS.md` describes the architecture and conventions.

## Production image

    task docker:build
    docker run -p 3000:3000 \
      -e DATABASE_URL=... -e DATABASE_MIGRATION_URL=... \
      -e BETTER_AUTH_SECRET=... -e BETTER_AUTH_URL=... \
      -e OIDC_DISCOVERY_URL=... -e OIDC_CLIENT_ID=... -e OIDC_CLIENT_SECRET=... \
      agenty:local

The server applies pending database migrations on start (as the role in `DATABASE_MIGRATION_URL`)
and refuses to start if they fail. Several instances may start at once; they migrate one after another.

The database needs the roles from `docker/postgres/init.sql`. Create them with your own passwords: the values in that file are local-development defaults only.

### Sign-in (OIDC)

Set these environment variables:

| Variable | Value |
|---|---|
| `BETTER_AUTH_SECRET` | random secret, e.g. `openssl rand -base64 32` |
| `BETTER_AUTH_URL` | public URL of Agenty, e.g. `https://agenty.example.com` |
| `OIDC_DISCOVERY_URL` | the IdP's `.well-known/openid-configuration` URL |
| `OIDC_CLIENT_ID` | client id from the IdP |
| `OIDC_CLIENT_SECRET` | client secret from the IdP |

Register Agenty at the IdP as a confidential client with redirect URI
`<BETTER_AUTH_URL>/api/auth/callback/oidc` and scopes `openid email profile`. The ID token or the
userinfo endpoint must provide `email` (and `name`).

Access is controlled in the IdP: everyone it authenticates can sign in. Sessions last 12 hours, so
users removed at the IdP lose access within 12 hours. Signing out ends only the Agenty session.
`BETTER_AUTH_SECRET` also encrypts stored OAuth tokens and signs sessions: rotating it signs
everyone out and makes stored tokens unreadable.

### Workspaces

Every user gets a personal workspace at their first sign-in. Under **Workspaces** they can create
more workspaces and invite people who have signed in before; invited users accept or decline in the
app (no email). Admins manage a workspace, members use it. Agents, tools and provider keys will
belong to workspaces; chats stay private.

## Troubleshooting

- **Port 8080 is already in use:** the mock IdP needs it; stop the other process, then `task db:up`.
- **Sign-in is unavailable or env errors after updating:** an existing `.env` lacks the new keys;
  copy them from `.env.example`.
- **Port 5432 is already in use:** set `POSTGRES_PORT` in `.env` to a free port and use the same
  port in `DATABASE_URL` and `DATABASE_MIGRATION_URL`. Then run `task db:up` again.
- **`Database not reachable (ECONNREFUSED)`:** run `task db:up`.
- **A returning user's sign-in fails with the generic message:** Better Auth may have refused to
  link a new IdP subject to an existing email (`account_not_linked` in the server log).
- **Roles are missing after editing `docker/postgres/init.sql`:** it only runs on an empty volume;
  run `task db:reset` (deletes local data) and `task db:up`.
