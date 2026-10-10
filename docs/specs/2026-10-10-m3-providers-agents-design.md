# M3 – Providers and agents: design

Status: revised after maintainer feedback (several providers per workspace, base URLs), reviewed, awaiting maintainer approval · Date: 2026-10-10 · Branch: `feat/m3-providers-agents`

## Goal

Workspace admins add **providers** to a workspace: a named connection of a
type (OpenAI, Anthropic or Google Gemini API), a base URL and an API key. A
workspace can have several providers of the same type (e.g. two Anthropic
accounts, or the official API and a company gateway). Admins create agents: a
name, a description, a system prompt, a provider and model, and optional model
parameters. Members see everything read-only. Chatting with agents is M4.

**Done when:** `task ci` passes locally and in GitHub Actions, the tests below
exist and pass, the PR has been reviewed by a reviewer subagent and the
maintainer has approved the merge.

## Non-goals

- Calling models: no chat, no "test this agent", and no AI SDK dependency yet (M4).
- Provider types other than the three (Ollama, generic OpenAI-compatible). A
  gateway that speaks one of the three APIs works through the base URL.
- Sharing providers between workspaces; changing a provider's type (create a
  new one instead).
- Tools on agents (M5).
- A scheduled job that checks model availability (M4, on the Workflow SDK; M3
  checks lazily, see *Model availability*).
- Model parameters beyond temperature, top P and max output tokens.

## Rules

| # | Rule |
|---|---|
| P1 | A workspace (personal ones included) has any number of providers. A provider has a name (1–80 characters, unique per workspace ignoring case), a type (`openai`, `anthropic`, `google`, fixed after creation), a base URL and an API key. |
| P2 | Only admins create, edit and delete providers. Members see name, type, base URL and status, never the key hint. |
| P3 | The key is verified against the provider's base URL before it is stored (see *Verification*), on creation and whenever key or base URL change; a provider that fails is not stored or not changed. Renaming alone doesn't verify. |
| P4 | Keys are stored encrypted (AES-256-GCM, `AGENTS.md` rule 4). The plain key never leaves `src/server/providers` except as a header of the provider's HTTP call, and never appears in responses, logs, error messages, action state or the UI. Admins see a hint: the first 6 and the last 4 characters (`sk-ant…abcd`), or only the last 4 for keys shorter than 24 characters. |
| P5 | Changing a provider's base URL requires entering the key again (otherwise an admin, who can't read the key, could point the provider at their own server and receive it). |
| P6 | Base URLs are only checked for syntax (see *Outbound requests*). Hosts and addresses are not restricted (maintainer decision; can be added later): an admin, i.e. any user through their personal workspace, can make the server send requests to internal addresses. |
| P7 | Deleting a provider is always allowed. Its agents stay and show as "Provider removed" until an admin picks another provider. |
| A1 | Only admins create, edit and delete agents. Members see the agent list and every agent's settings read-only, including the system prompt. |
| A2 | Agent names are unique per workspace, ignoring case. |
| A3 | Creating an agent, or changing its provider or model, requires a provider of this workspace and a model in that provider's current list (see *Model availability*). Editing other fields of an agent whose provider or model is gone is allowed. |
| A4 | Deleting a workspace deletes its providers and agents. |

## Encryption (`src/server/crypto`)

- Env `ENCRYPTION_KEYS` (required): comma-separated `id:base64` pairs. `id` is
  1–16 characters `[a-z0-9]`, unique; each key is strict base64 (`z.base64()`)
  decoding to exactly 32 bytes. The first pair is the **active key**, used for
  all new ciphertexts; the others only decrypt. The env error names the
  problem, never a value. The parser and the crypto core
  (`src/server/crypto/keyring.ts`, `cipher.ts`) have no `server-only` import, so
  the rotation script can use them; `env.ts` reuses the parser through
  `z.string().transform`.
- `encrypt(plaintext, aad)` / `decrypt(ciphertext, aad)`: AES-256-GCM, random
  12-byte IV, 16-byte tag. Format `v1.<keyId>.<iv>.<tag>.<ciphertext>`
  (base64url parts). The AAD for a provider key is
  `provider|<providerId>|<type>|<baseUrl>`: a ciphertext copied into another
  row, or a base URL changed directly in the database, does not decrypt. The
  provider id is generated in the app (`crypto.randomUUID()`) before the insert,
  so the AAD can include it.
- Decryption fails closed with a `CryptoError` without details (unknown key id,
  bad format, failed tag). Callers log the error class only.
- `needsRotation(ciphertext)`: key id differs from the active key.
- **Rotation:** add a new key at the front of `ENCRYPTION_KEYS`, restart, run
  the rotation (`scripts/rotate-keys.ts`, plain Node with type stripping like
  `scripts/migrate.ts`; locally `task crypto:rotate`, and shipped in the
  production image: `docker run … agenty node scripts/rotate-keys.ts`; the CI
  docker job runs its `--check` in the image). `scripts/build.sh` copies the
  script and its imports into `.next/standalone`; those modules (keyring,
  cipher and the AAD builder `src/server/crypto/aad.ts`) use only relative
  `.ts` imports, Node built-ins, `zod` and `postgres`, no `@/` aliases and no
  `server-only`. The AAD is built from the stored `base_url` string verbatim,
  never re-normalised, so a later change to normalisation can't make keys
  unreadable. It
  connects with `DATABASE_URL` (the app role may update `provider`) through its
  own postgres client, re-encrypts every `provider` row whose key id is not the
  active one, row by row with `select … for update` on that row only (never the
  workspace row, so it can't deadlock with the provider service), leaves
  `updated_at`/`updated_by` alone, and prints counts only. `--check` only counts
  rows on non-active keys. When that is 0, remove the old key. Rows whose key id
  is no longer configured are counted as unreadable and make the script exit
  non-zero; the workspace sees "Key unreadable, enter it again".
- `.env.example` gets a development key; the CI workflow and the docker job get
  a fixed test key. A real one: `openssl rand -base64 32`.

## Providers (`src/server/providers`)

### Types

`types.ts` describes each provider type: id, display name ("OpenAI",
"Anthropic", "Google Gemini"), default base URL, how to list models and the
chat-model filter. Base URLs include the API version, as the AI SDK's `baseURL`
expects in M4: `https://api.openai.com/v1`, `https://api.anthropic.com/v1`,
`https://generativelanguage.googleapis.com/v1beta`. Request paths below are
relative to the provider's base URL.

### Outbound requests

Requests to a provider base URL use plain `fetch` (`cache: "no-store"`).

- **Base URL syntax** (Zod, on save; `base_url_not_allowed`): `http:` or
  `https:`, no username/password, no query or fragment, at most 500 characters;
  stored normalised (trailing slash removed) and compared normalised.
- **No host or address restrictions** (P6). Accepted risk, recorded in
  *Review focus*; a later milestone can add an address check and an operator
  allowlist in one place, since all provider requests go through the listing
  module now and the AI SDK's `fetch` option in M4.
- **Little to learn from failures:** `redirect: "error"`; DNS errors,
  connection errors, redirects and timeouts all fail as `provider_unreachable`,
  only the error class is logged, and response bodies are never shown.

### Listing models

No SDK (the AI SDK has no list helper): one 10 s deadline for the whole
listing (all pages), each body limited to 5 MB while streaming (not via
`Content-Length`), at most 10 pages and 5 000 models (more counts as an invalid
response), responses validated with Zod (unknown fields ignored). The key goes
in a header, never in the URL.

| Type | Request | Paging | Chat models |
|---|---|---|---|
| OpenAI | `GET /models`, `Authorization: Bearer <key>` | none | id matches `^(gpt-\|o\d\|chatgpt-)` and not `embed\|tts\|whisper\|transcribe\|audio\|realtime\|dall-e\|image\|moderation\|search\|instruct\|davinci\|babbage` (heuristic; OpenAI returns no capability field) |
| Anthropic | `GET /models?limit=1000`, `x-api-key`, `anthropic-version: 2023-06-01` | `after_id=<last_id>` while `has_more` | all |
| Google | `GET /models?pageSize=1000`, `x-goog-api-key` | `pageToken` while `nextPageToken` | `supportedGenerationMethods` includes `generateContent`, and the name doesn't match `embedding\|imagen\|veo\|aqa\|tts\|image\|live`; the `models/` prefix is stripped |

Model lists are untrusted (gateways): entries whose id fails the agent `model`
rule are dropped, labels are capped at 200 characters and rendered as text
only. Result: `{ id, label }[]` sorted by label (Anthropic `display_name`, Google
`displayName`, OpenAI the id). A gateway that answers in the type's format
works; one that doesn't fails verification.

### Verification

Listing models is the check. Outcomes map to fixed codes; response bodies are
never logged or shown (OpenAI echoes part of the key in its error message).
Logged: provider type, HTTP status, error class; never the base URL's
response.

| Outcome | Code | Message |
|---|---|---|
| 2xx, valid body | – | stored |
| 401; Google 400 with `error.details[].reason == "API_KEY_INVALID"` | `key_rejected` | "The provider rejected this key." |
| refused address, redirect, timeout, network error, 429, 5xx | `provider_unreachable` | "The provider could not be reached at this base URL." |
| anything else (403, other 4xx, invalid body) | `key_unverified` | "The key could not be verified. Check the base URL and that the key may list models." |

### Provider service

Table `provider` (see *Data model*). Functions in
`src/server/providers/providers.ts`, `actorId` explicit, `WorkspaceError` codes
as in M2. Writes that verify follow one order: validate input → plain admin
check (non-admins never trigger an outbound request) → verify outside any
transaction (no lock held during the HTTP call) → transaction: lock the
workspace, check admin again, write, seed the model cache.

- `createProvider(actorId, workspaceId, { name, type, baseUrl, key })`.
- `updateProvider(actorId, workspaceId, providerId, { name, baseUrl, key? })`:
  `key` empty keeps the stored key; a changed base URL (compared normalised)
  without a key fails with `key_required` (P5). Verifies the submitted
  (base URL, key) pair when either changes, then writes exactly that pair with
  a new ciphertext. Under the lock the row is read again: if its base URL or
  `updated_at` changed since the pre-check (another admin saved meanwhile), the
  update fails with `provider_changed` ("This provider was changed meanwhile.
  Reload and try again.") instead of writing a mismatched row. A rename alone
  updates only the name.
- `deleteProvider(actorId, workspaceId, providerId)`: lock, admin, delete
  (agents' `provider_id` becomes null, P7).
- `listProviders(actorId, workspaceId)` / `getProvider(...)`: member;
  `{ id, name, type, baseUrl, updatedAt, readable, hint? }`, `hint` only for
  admins. `readable` is false when the ciphertext doesn't decrypt (it decrypts
  to check; the plaintext is dropped at once and never returned).
- `getProviderSecret(workspaceId, providerId)`: the only function returning a
  plain key (with type and base URL); server-only, no actor (callers have
  checked access). M4's model wrapper calls it inside the workflow step.
- Name uniqueness is enforced by the unique index only: Postgres error `23505`
  on it maps to `provider_name_taken`.
- Key input: trimmed, `^[\x21-\x7E]{20,500}$` (printable ASCII only, since
  `fetch` rejects other header bytes).

### Model cache

`listModels(workspaceId, providerId)` returns
`{ status: "ok", models } | { status: "removed" | "unreadable" | "unreachable" }`.
In-memory cache per process, keyed by provider id, also storing the provider's
`updated_at`: successes live 10 minutes, failures 1 minute (so a broken
provider is not hit on every page view). Correctness rests on the `updated_at`
comparison (a changed provider never uses an old entry, even if module state is
duplicated across bundles); clearing on update/delete is only an optimisation.
A successful verification seeds the entry. Concurrent callers share one
in-flight promise per key. Expired entries are dropped on access and by a sweep
when the map exceeds 1 000 entries. The clock is injectable for tests. Single
instance is assumed (as for M4's queue); with several instances each has its own
cache, which is only slower.

### Model availability

An agent's status is computed when it is shown (lazily, from the cache):

| Status | When | UI |
|---|---|---|
| `ready` | key readable, model in the list | "Ready" |
| `provider_removed` | `provider_id` is null | "Provider removed" |
| `key_unreadable` | key does not decrypt | "Provider key unreadable" |
| `model_unavailable` | list fetched, model not in it | "Model unavailable" |
| `unknown` | list could not be fetched | "Couldn't check model" |

Only `ready` is usable for chat in M4; whether `unknown` blocks is decided
there. Nothing is stored; a model that returns is ready again. M4 replaces this
with a scheduled check.

## Agents (`src/server/agents`)

Service functions in `agents.ts`, `actorId` explicit, writes lock the
workspace row and check admin (M2 pattern), reads check membership:
`listAgents`, `getAgent`, `createAgent`, `updateAgent`, `deleteAgent`. Agent and
provider ids are lookup keys only: queries always filter by `workspace_id` too,
and a malformed id or one from another workspace is `not_found` (for a
`providerId` in agent input: `provider_removed`).

Advanced parameters are only checked for type and range, never for what a
provider or model accepts (maintainer decision: they are for users who know
what they are doing). Some combinations fail at call time in M4, e.g. Anthropic
temperature above 1, temperature together with top P on newer Claude models,
temperature on OpenAI reasoning models; M4 sends the stored values as they are
and shows the provider's failure as a fixed error.

Input (Zod, `validation.ts`, trimmed):

| Field | Rule |
|---|---|
| name | 1–80 characters; unique per workspace ignoring case → `agent_name_taken` |
| description | 0–500 characters |
| systemPrompt | 0–20 000 characters |
| providerId | uuid of a provider in this workspace |
| model | 1–200 characters, `^[A-Za-z0-9._:/@-]+$` |
| temperature | optional, 0–2 |
| topP | optional, > 0 and ≤ 1 |
| maxOutputTokens | optional, integer 1–1 000 000 |

A3 is checked in the service against `listModels` after a plain admin check
and before the transaction (which locks, checks admin again and re-reads the
provider row): `removed` → `provider_removed`, `unreadable` →
`provider_key_unreadable`, `unreachable` → `provider_unreachable`, model
missing → `model_unavailable`. For an update this only runs when provider or
model changed. Accepted race: a provider changed between this check and the
transaction still lets the save through; the lazy status shows the result, so
there is no second fetch under the lock. A2 is enforced by the unique index
only, as for providers.

## Data model

Drizzle, schema `app`, one generated migration, uuid ids, `timestamptz`.

| Table | Columns | Constraints |
|---|---|---|
| `provider` | `id` (set by the app), `workspace_id`, `name`, `type` text, `base_url` text, `encrypted_key` text, `key_hint` text, `created_by_user_id` null, `updated_by_user_id` null, `created_at`, `updated_at` | FK workspace on delete cascade, users on delete set null; unique index (`workspace_id`, `lower(name)`); check `type in ('openai','anthropic','google')`; length checks as backstops (as in M2). |
| `agent` | `id`, `workspace_id`, `provider_id` null, `name`, `description`, `system_prompt`, `model`, `temperature` real null, `top_p` real null, `max_output_tokens` integer null, `created_by_user_id` null, `created_at`, `updated_at` | FK workspace on delete cascade, provider on delete set null (P7), users on delete set null; unique index (`workspace_id`, `lower(name)`); length checks as backstops. The service guarantees agent and provider share the workspace (checked in the transaction); FK error `23503` maps to `provider_removed` as a backstop. |

## Errors

The services throw `WorkspaceError` with new codes, so delete actions keep
using `runWorkspaceAction`, and `error-messages.ts` gains fixed messages:
`invalid_provider` (name, type, base URL fields), `base_url_not_allowed`
(syntax), `provider_changed`, `key_invalid`,
`key_required`, `key_rejected`, `provider_unreachable`, `key_unverified`,
`provider_name_taken`, `agent_name_taken`, `provider_removed`,
`provider_key_unreadable`, `model_unavailable`, `invalid_parameters`
(temperature, top P, max output tokens), `invalid_agent` (every other agent
field).

## UI

Server components and server actions, like M2. Every new page has a
`loading.tsx` (shared spinner) and an `instant()` entry.

Workspace nav: **Start · Agents · Providers · Settings** (Settings stays
disabled for the personal workspace; Agents and Providers work everywhere).

| Route | Content |
|---|---|
| `/w/[id]/providers` | Table: name (link), type, base URL, key (hint, admins only; "Unreadable, enter it again" when it doesn't decrypt), updated. Admins: "Add provider" button. Empty state explaining that agents need a provider. |
| `/w/[id]/providers/new` | Admins: the provider form. Members are redirected to the provider list. |
| `/w/[id]/providers/[providerId]` | Admins: the provider form with the saved values (type read-only, key field empty with the hint as placeholder and "Leave empty to keep the current key"; required as soon as the base URL differs from the saved one), and a Danger zone card with "Delete provider" (confirmation dialog naming the number of agents that use it). Members: the same values read-only. |
| `/w/[id]/agents` | Table: name (link), provider name · model, status (see *Model availability*; the status cells stream in their own `<Suspense>`, so the table doesn't wait for provider calls). Admins: "New agent" button. Empty state with a hint to add a provider first when none exists. |
| `/w/[id]/agents/new` | Admins: the agent form. Members are redirected to the agent list (`redirect`, not the not-found page: the workspace exists for them). |
| `/w/[id]/agents/[agentId]` | Admins: the agent form with the saved values, and a Danger zone card with "Delete agent" (confirmation dialog). Members: the same values read-only. |

**Forms with `useActionState`** (maintainer decision; M2's other forms keep the
redirect pattern): the provider and agent forms are client components whose
create/update actions return `{ error, values }` on failure, so nothing typed
is lost (values are rendered back as `defaultValue`, since React 19 resets
uncontrolled forms after an action). The returned values never include the
key: after a failed provider save the key field is empty again. Success and
`not_found` redirect to the list; `refresh()` runs before the redirect as in
`runWorkspaceAction`. Saving a provider verifies first, so the button shows a
pending state.

**Provider form:** name; type (select on create: choosing a type fills the base
URL field with its default while the field is still untouched or holds another
type's default); base URL; API key (password field).

**Agent form:** name, description, system prompt (textarea), provider select
(the workspace's providers by name, with type; unreadable ones can't be
picked), model select (the live list of the selected provider, loaded
server-side for every provider in parallel and passed in; a provider whose list
failed shows "Models couldn't be loaded" and can't be picked for a new choice),
and a collapsible **Advanced** section with temperature, top P and max output
tokens (empty = provider default; the section starts open when any is set).
Editing an agent whose saved provider or model is not selectable shows the
saved value as the current option, so other fields can still be edited (A3);
an agent whose provider was removed shows "Provider removed" and must get a new
provider before provider or model can be saved.

## Access

As M2: pages call `requireWorkspaceMember`/`requireWorkspaceAdmin`; the
services check membership and role themselves inside their transaction;
non-members get the not-found page; ids are lookup keys only.

## Tests

- Unit:
  - crypto: round trip; tampered ciphertext, IV or tag fails; another
    provider's AAD (other id, type or base URL) fails; unknown key id fails;
    decrypting with an older key works; `ENCRYPTION_KEYS` parsing (wrong
    length, duplicate id, bad base64, error text contains no value);
  - base URL syntax and normalisation;
  - each type's list parsing, paging, limits and chat filter (fixtures);
    verification outcome mapping including Google's 400 `API_KEY_INVALID`;
  - key hint (long and short keys); agent and provider input validation.
- Integration (dev database, local fake provider HTTP server):
  - P2/A1: members and non-members can't write providers or agents;
    non-members get `not_found` for reads too;
  - P1: two providers of the same type in one workspace; duplicate names in
    different case fail; the same name in another workspace works;
  - P3: a rejected key is not stored; a stored key is verified with the header
    the type expects and never in the URL; renaming makes no request;
  - P4: `encrypted_key` does not contain the key; no read function except
    `getProviderSecret` returns it; a ciphertext copied to another provider row
    doesn't decrypt;
  - P5: changing the base URL without a key fails with `key_required` and makes
    no request; with a key it verifies against the new URL;
  - P6: an invalid base URL (credentials, query, other scheme) is refused
    without a request; a redirect from the provider gives
    `provider_unreachable`;
  - concurrent edits: a rename racing another admin's base URL + key change
    leaves a row that decrypts (one of them fails with `provider_changed`);
  - P7/availability: deleting a provider keeps its agents with
    `provider_removed`; a model missing from the list gives
    `model_unavailable`; an unreachable provider gives `unknown` and is retried
    after the failure TTL (injected clock);
  - A2, A3 (unknown provider id or another workspace's provider, unknown
    model; editing the name of an agent whose model disappeared works), A4;
  - rotation: after adding a key and running the rotation, all rows use the new
    key id and still decrypt, across providers with different types and base
    URLs; `--check` reports 0; the old key can be removed;
  - leakage: with the fake provider echoing the key in its 401 body, no
    `console.*` output, no `WorkspaceError` and no action state contains the
    key;
  - a non-admin's create/update sends zero requests to the fake provider; an
    admin demoted between verification and save gets `forbidden` and nothing is
    stored.
- E2E (fake provider server started by Playwright on `127.0.0.1`):
  admin adds an OpenAI provider with the fake server's base URL (hint shown)
  and a second one of the same type; a rejected key shows the fixed message and
  keeps name and base URL; changing the base URL without the key is refused;
  creates an agent with a model from the list and Advanced parameters; edits it;
  a member sees providers and agents read-only without hints and without New
  buttons; deleting the provider marks the agent "Provider removed"; deleting
  the agent; the personal workspace has working Agents and Providers tabs;
  neither the provider pages nor the agent form page contain the key anywhere
  in their HTML or RSC payload.

## Review focus

- Accepted (maintainer decision): no rate limit on key verification, although
  every user can verify keys through their personal workspace.
- Accepted (maintainer decision): no SSRF protection for base URLs; any user
  can make the server send requests (with a key they entered) to internal
  addresses and learn whether they answer like a provider. Revisit before
  exposing an instance to untrusted users.
- Key leakage: logs, error messages, redirects with `?error=`, action state,
  React props sent to the client (no form ever receives a key), provider error
  bodies, a base URL changed without re-entering the key.
- A model list that is huge, slow, paginates forever or returns garbage.
- A cache miss for several providers at once: the agents table must render
  without waiting for the status column (status in its own `<Suspense>`).
- Stale forms: an admin demoted while editing, an agent or provider deleted in
  another tab.
- Removing an encryption key that still has ciphertexts.

## M4 notes (not built here)

- `@workflow/ai`'s `DurableAgent` is deprecated and needs AI SDK 6; use
  `WorkflowAgent` from `@ai-sdk/workflow` (AI SDK 7).
- Never hand the agent a model built with a key (`createOpenAI({ apiKey })(id)`):
  the providers' workflow serialization stores resolved headers, i.e. the key,
  unencrypted in the workflow tables. Use a model wrapper that serializes only
  `(workspaceId, providerId, modelId)` and calls `getProviderSecret` inside the
  step; test that no key reaches the `workflow` schemas.
- Pass `apiKey` and `baseURL` explicitly; the SDKs fall
  back to `OPENAI_API_KEY`/`OPENAI_BASE_URL` and similar env vars.
- `openai(id)` uses the Responses API, which some gateways don't offer; decide
  there (probably `.chat(id)`).
- The scheduled model availability check.

## Docs

`AGENTS.md`: architecture (`src/server/crypto`, `providers`, `agents`,
routes), the Providers and Agents rules in short, the accepted SSRF risk (P6),
encryption and rotation, the M4 notes on key handling, new env vars
(`ENCRYPTION_KEYS`). README: providers and agents from the user's view,
rotation for operators.
