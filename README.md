# Agenty

Self-hosted, multi-tenant AI agent builder with chat.

## Requirements

Node 24, pnpm, [go-task](https://taskfile.dev), Docker, and [opa](https://www.openpolicyagent.org/docs#running-opa) v1.21.1 (`brew install opa`).

## Quick start

    task setup     # tool check, dependencies, Playwright browser, .env
    task dev       # Postgres + dev server on http://localhost:3000 (migrates on start)

`task --list` shows all commands; `AGENTS.md` describes the architecture and conventions.

## Production image

    task docker:build
    docker run -p 3000:3000 -e DATABASE_URL=... -e DATABASE_MIGRATION_URL=... agenty:local

The server applies pending database migrations on start (as the role in `DATABASE_MIGRATION_URL`)
and refuses to start if they fail. Several instances may start at once; they migrate one after another.

The database needs the roles from `docker/postgres/init.sql`. Create them with your own passwords: the values in that file are local-development defaults only.

## Production setup

Set these in the container environment:

- `BETTER_AUTH_SECRET`: at least 32 characters (`openssl rand -base64 32`).
- `BETTER_AUTH_URL`: the public URL of Agenty (https enables secure cookies).
- `BETTER_AUTH_TRUSTED_ORIGINS` (optional): comma-separated origins of identity providers on
  private or internal networks. Public IdPs need nothing.

Users sign in only through an OIDC identity provider (IdP). Providers are rows in `auth.sso_provider`;
until there is a UI you manage them with SQL, connected as a role that may write schema `auth`
(e.g. `agenty_owner`).

### Register Agenty at the IdP

- Redirect URI: `<BETTER_AUTH_URL>/api/auth/sso/callback/<provider id>`
- Flow: authorization code with PKCE, scopes `openid email profile`.
- The **ID token** must contain `email`, `name`, the organization claim (a slug matching
  `^[a-z0-9][a-z0-9-]{1,62}$`, e.g. `org`) and optionally a groups claim for admin rights. Claims
  that only appear at the userinfo endpoint are ignored.

### Add a provider

```sql
insert into auth.sso_provider
  (provider_id, issuer, domain, oidc_config, organization_claim, role_claim, admin_values)
values (
  'keycloak',
  'https://idp.example.com/realms/main',
  'example.com',
  '{"clientId":"agenty","clientSecret":"...","pkce":true,"scopes":["openid","email","profile"]}',
  'org',
  'groups',
  'agenty-admins'
);
```

`domain` lists the email domains served by this provider. Keep domains of different providers
non-overlapping, and without commas. `oidc_config` accepts exactly the keys shown plus an optional
`discoveryEndpoint`; anything else makes the provider fail with a readable error. `role_claim` and
`admin_values` are optional: users whose claim contains one of the values (comma-separated) become
admins, all others members. The client secret is stored in plain text.

The first sign-in discovers the IdP's endpoints and stores them in `oidc_config`. To discover again
(for example after the IdP changed its endpoints), remove the three endpoint keys:

```sql
update auth.sso_provider
set oidc_config = ((oidc_config::jsonb) - 'authorizationEndpoint' - 'tokenEndpoint' - 'jwksEndpoint')::text
where provider_id = 'keycloak';
```

Avoid changing a provider row while people may be signing in; their in-flight sign-ins fail.

### Remove a provider

```sql
delete from auth.account where provider_id = 'keycloak';
delete from auth.sso_provider where provider_id = 'keycloak';
```

Its organizations and data stay but are orphaned: their users cannot use them until an operator
sets a new `provider_id` on the organization.

### Rules

- Never change an existing provider's `issuer` and never re-use a deleted provider id: accounts
  are keyed by provider id and the IdP's `sub`, so either could bind foreign identities to
  existing users. Register a new provider id instead.
- Organizations are created on the first sign-in that names them and belong to that provider; a
  user belongs to one organization.
- Roles and memberships are read at sign-in and sessions last 12 hours without refresh: removing
  or demoting someone at the IdP takes effect within 12 hours.

### Development sign-in

`task dev` starts a mock IdP on `http://localhost:8080` and registers the providers `corp`
(`@corp.test`) and `partner` (`@partner.test`). Enter an email on the sign-in page; the mock IdP
then shows a form where you choose any username and paste the claims as JSON, for example:

```json
{ "email": "alice@corp.test", "name": "Alice", "org": "acme", "groups": ["agenty-admins"] }
```

## Troubleshooting

- **Port 5432 is already in use:** set `POSTGRES_PORT` in `.env` to a free port and use the same
  port in `DATABASE_URL` and `DATABASE_MIGRATION_URL`. Then run `task db:up` again.
- **`Database not reachable (ECONNREFUSED)`:** run `task db:up`.
- **Roles are missing after editing `docker/postgres/init.sql`:** it only runs on an empty volume;
  run `task db:reset` (deletes local data) and `task db:up`.
- **Port 8080 is already in use:** the mock IdP needs it (the providers are registered with
  `http://localhost:8080`); stop the other process.
- **Sign-in or startup complains about missing `BETTER_AUTH_*` variables:** your `.env` predates
  SSO; copy the new keys from `.env.example`.
