# M3 – Providers and agents: design

Status: draft · Date: 2026-10-10 · Branch: `feat/m3-providers-agents`

## Goal

Workspace admins store API keys for OpenAI, Anthropic and Google (Gemini API)
and create agents: a name, a description, a system prompt, a provider and model,
and optional model parameters. Members see everything read-only. Chatting with
agents is M4.

**Done when:** `task ci` passes locally and in GitHub Actions, the tests below
exist and pass, the PR has been reviewed by a reviewer subagent and the
maintainer has approved the merge.

## Non-goals

- Calling models: no chat, no "test this agent", and no AI SDK dependency yet (M4).
- Ollama or any other self-hosted or OpenAI-compatible endpoint (maintainer
  decision: a URL entered by a workspace admin would be an SSRF vector, since
  every user is admin of their personal workspace).
- Several keys per provider per workspace, named connections, key sharing
  between workspaces.
- Tools on agents (M5).
- A scheduled job that checks model availability (M4, on the Workflow SDK; M3
  checks lazily, see *Model availability*).
- Model parameters beyond temperature, top P and max output tokens.

## Rules

| # | Rule |
|---|---|
| P1 | A workspace has at most one API key per provider (`openai`, `anthropic`, `google`). Personal workspaces included. |
| P2 | Only admins set, replace and remove keys. Members see which providers are configured, nothing else. |
| P3 | A key is verified against the provider before it is stored (see *Verification*); a key that fails is not stored. |
| P4 | Keys are stored encrypted (AES-256-GCM, `AGENTS.md` rule 4). The plain key never leaves `src/server/providers` except as an argument to the provider's HTTP call, and never appears in responses, logs, error messages or the UI. The UI shows the last 4 characters only (`key_hint`), and to admins only. |
| P5 | Removing a key is always allowed. Agents of that provider stay, and show as "Provider not configured" until a key exists again. |
| A1 | Only admins create, edit and delete agents. Members see the agent list and every agent's settings read-only, including the system prompt. |
| A2 | Agent names are unique per workspace, ignoring case. |
| A3 | Creating an agent, or changing its provider or model, requires a key for that provider and a model in the provider's current list (see *Model availability*). Editing other fields of an agent whose model or key is gone is allowed. |
| A4 | Deleting a workspace deletes its keys and agents. |

## Encryption (`src/server/crypto`)

- Env `ENCRYPTION_KEYS` (required): comma-separated `id:base64` pairs. `id` is
  1–16 characters `[a-z0-9]`, unique; each key decodes to exactly 32 bytes. The
  first pair is the **active key**, used for all new ciphertexts; the others
  only decrypt. The env error names the problem, never a value.
- `encrypt(plaintext, aad)` / `decrypt(ciphertext, aad)`: AES-256-GCM, random
  12-byte IV, 16-byte tag. Format
  `v1.<keyId>.<iv>.<tag>.<ciphertext>` (base64url parts). The AAD for provider
  keys is `provider_key|<workspaceId>|<provider>`, so a ciphertext copied into
  another row does not decrypt.
- Decryption fails closed with a `CryptoError` without details (unknown key id,
  bad format, failed tag). Callers log the error class only.
- `needsRotation(ciphertext)`: key id differs from the active key.
- **Rotation:** add a new key at the front of `ENCRYPTION_KEYS`, restart, run
  `task crypto:rotate` (`scripts/rotate-keys.ts`, plain Node like
  `scripts/migrate.ts`, so the crypto core has no `server-only` import). It
  re-encrypts every `provider_key` row whose key id is not the active one, row
  by row with `select … for update`, and prints counts only. Then remove the old
  key. Removing a key that still has ciphertexts makes those keys unreadable:
  the workspace sees "Key unreadable, set it again" (see *Providers UI*).
- `.env.example` gets a development key; the CI workflow and the docker job get
  a fixed test key. A real one: `openssl rand -base64 32`.

## Providers (`src/server/providers`)

### Registry

`registry.ts` describes each provider: id, display name ("OpenAI",
"Anthropic", "Google Gemini"), base URL, how to list models, the chat-model
filter and the parameter limits (temperature max: OpenAI 2, Anthropic 1,
Google 2).

Base URLs default to the official APIs. Optional operator env
`AGENTY_OPENAI_BASE_URL`, `AGENTY_ANTHROPIC_BASE_URL`,
`AGENTY_GOOGLE_BASE_URL` (http(s) URLs) override them, for corporate proxies
and for the E2E fake provider. Workspaces never enter URLs. Prefixed with
`AGENTY_` so the provider SDKs' own env fallbacks (`OPENAI_BASE_URL`, …) in M4
are never picked up by accident.

### Listing models

Plain `fetch`, no SDK (the AI SDK has no list helper): 10 s timeout,
`redirect: "error"`, response body limited to 5 MB, response validated with Zod
(unknown fields ignored). The key goes in a header, never in the URL.

| Provider | Request | Paging | Chat models |
|---|---|---|---|
| OpenAI | `GET /v1/models`, `Authorization: Bearer <key>` | none | id matches `^(gpt-\|o\d\|chatgpt-)` and not `embed\|tts\|whisper\|transcribe\|audio\|realtime\|dall-e\|image\|moderation\|search\|instruct\|davinci\|babbage` (heuristic; OpenAI returns no capability field) |
| Anthropic | `GET /v1/models?limit=1000`, `x-api-key`, `anthropic-version: 2023-06-01` | `after_id` while `has_more`, at most 10 pages | all |
| Google | `GET /v1beta/models?pageSize=1000`, `x-goog-api-key` | `pageToken` while `nextPageToken`, at most 10 pages | `supportedGenerationMethods` includes `generateContent`, and the name doesn't match `embedding\|imagen\|veo\|aqa\|tts\|image\|live`; the `models/` prefix is stripped |

Result: `{ id, label }[]` sorted by label (Anthropic `display_name`, Google
`displayName`, OpenAI the id).

### Verification

Listing models is the check. Outcomes map to fixed codes; provider response
bodies are never logged or shown (OpenAI echoes part of the key in its error
message). Logged: provider id, HTTP status, error class.

| Outcome | Code | Message |
|---|---|---|
| 2xx, valid body | – | stored |
| 401; Google 400 with `error.details[].reason == "API_KEY_INVALID"` | `key_rejected` | "The provider rejected this key." |
| timeout, network error, 429, 5xx | `provider_unavailable` | "The provider could not be reached. Try again later." |
| anything else (403, other 4xx, invalid body) | `key_unverified` | "The key could not be verified. Check that it may list models." |

### Keys

Table `provider_key` (see *Data model*). Service functions in
`src/server/providers/keys.ts`, `actorId` explicit, `WorkspaceError` codes as in
M2:

- `setProviderKey(actorId, workspaceId, provider, key)`: validates the key
  (trimmed, 8–500 characters, no whitespace or control characters), checks
  that the actor is admin (plain read, so non-admins can't use the server to
  test keys), verifies the key outside any transaction (no lock held during the
  HTTP call), then locks the workspace, checks admin again, upserts ciphertext, hint, `updated_by`,
  `updated_at`, and clears the model cache for that workspace and provider.
- `removeProviderKey(actorId, workspaceId, provider)`: lock, admin, delete,
  clear cache.
- `listProviderStatus(actorId, workspaceId)`: member; per provider
  `{ provider, configured, hint?, updatedAt?, readable }`; `hint` only for
  admins. `readable` is false when the ciphertext doesn't decrypt.
- `getProviderKey(workspaceId, provider)`: the only function returning a plain
  key; server-only, no actor (callers have checked access). M4's model wrapper
  calls it inside the workflow step.

### Model cache

`listModelsForWorkspace(workspaceId, provider)` returns
`{ status: "ok", models } | { status: "not_configured" | "unreadable" | "unavailable" }`.
In-memory cache per process, keyed by workspace and provider, also storing the
key's `updated_at`: successes live 10 minutes, failures 1 minute (so a broken
provider is not hit on every page view). A changed `updated_at` or a
set/remove clears the entry. Single instance is assumed (as for M4's queue);
with several instances each has its own cache, which is only slower.

### Model availability

An agent's status is computed when it is shown (lazily, from the cache):

| Status | When | UI |
|---|---|---|
| `ready` | key readable, model in the list | "Ready" |
| `provider_not_configured` | no key | "Provider not configured" |
| `key_unreadable` | key does not decrypt | "Provider key unreadable" |
| `model_unavailable` | list fetched, model not in it | "Model unavailable" |
| `unknown` | list could not be fetched | "Couldn't check model" |

Only the first is usable for chat in M4; `unknown` does not block in M4 either
(decided there). Nothing is stored; a model that returns is ready again. M4
replaces this with a scheduled check.

## Agents (`src/server/agents`)

Service functions in `agents.ts`, `actorId` explicit, writes lock the
workspace row and check admin (M2 pattern), reads check membership:
`listAgents`, `getAgent`, `createAgent`, `updateAgent`, `deleteAgent`. An agent
id is a lookup key only: queries always filter by `workspace_id` too, and a
malformed id or one from another workspace is `not_found`.

Input (Zod, `validation.ts`, trimmed):

| Field | Rule |
|---|---|
| name | 1–80 characters; unique per workspace ignoring case → `agent_name_taken` |
| description | 0–500 characters |
| systemPrompt | 0–20 000 characters |
| provider | `openai` \| `anthropic` \| `google` |
| model | 1–200 characters, `^[A-Za-z0-9._:/@-]+$` |
| temperature | optional, 0 to the provider's max (`invalid_parameters`) |
| topP | optional, > 0 and ≤ 1 |
| maxOutputTokens | optional, integer 1–1 000 000 |

A3 is checked in the service against `listModelsForWorkspace` after a plain
admin check and before the transaction (which locks and checks admin again): `not_configured`/`unreadable` → `provider_not_configured`,
`unavailable` → `provider_unavailable`, model missing → `model_unavailable`.
For an update this only runs when provider or model changed.

## Data model

Drizzle, schema `app`, one generated migration, uuid ids, `timestamptz`.

| Table | Columns | Constraints |
|---|---|---|
| `provider_key` | `workspace_id`, `provider` text, `encrypted_key` text, `key_hint` text, `updated_by_user_id` null, `updated_at` | PK (`workspace_id`, `provider`); FK workspace on delete cascade, user on delete set null; check `provider in ('openai','anthropic','google')`. |
| `agent` | `id`, `workspace_id`, `name`, `description`, `system_prompt`, `provider`, `model`, `temperature` real null, `top_p` real null, `max_output_tokens` integer null, `created_by_user_id` null, `created_at`, `updated_at` | FK workspace on delete cascade, user on delete set null; unique index (`workspace_id`, `lower(name)`); provider check; length checks as backstops (as in M2). |

`agent.provider` has no FK to `provider_key` (P5).

## Errors

The services throw `WorkspaceError` with new codes, so server actions keep
using `runWorkspaceAction` and `error-messages.ts` gains fixed messages:
`key_invalid`, `key_rejected`, `provider_unavailable`, `key_unverified`,
`agent_name_taken`, `provider_not_configured`, `model_unavailable`,
`invalid_parameters`, `invalid_agent`.

## UI

Server components and server actions with plain `<form>`s, like M2. Every new
page has a `loading.tsx` (shared spinner) and an `instant()` entry.

Workspace nav: **Start · Agents · Providers · Settings** (Settings stays
disabled for the personal workspace; Agents and Providers work everywhere).

| Route | Content |
|---|---|
| `/w/[id]/providers` | A table with one row per provider: name, status ("Configured ••••abcd · updated <date>" for admins, "Configured" for members, "Not configured", "Key unreadable, set it again"). Admins: a password field with "Save" (set or replace) and "Remove" per row. Saving verifies first, so the button shows a pending state. |
| `/w/[id]/agents` | Table: name (link), provider · model, status (see *Model availability*). Admins: "New agent" button. Empty state with a hint to configure a provider first when none is. |
| `/w/[id]/agents/new` | Admins: the agent form. Members are redirected to the agent list. |
| `/w/[id]/agents/[agentId]` | Admins: the agent form with the saved values, and a Danger zone card with "Delete agent" (confirmation dialog). Members: the same fields read-only. |

**Agent form** (a client component inside a server `<form>`): name,
description, system prompt (textarea), provider select (only configured,
readable providers), model select (the live list of the selected provider,
loaded server-side for every configured provider in parallel and passed in;
a provider whose list failed shows "Models couldn't be loaded" and can't be
picked for a new choice), and a collapsible **Advanced** section with
temperature, top P and max output tokens (empty = provider default; the
section starts open when any is set). Editing an agent whose saved
provider or model is not selectable shows the saved value as the current
option, so other fields can still be edited (A3).

## Access

As M2: pages call `requireWorkspaceMember`/`requireWorkspaceAdmin`; the
services check membership and role themselves inside their transaction;
non-members get the not-found page; ids are lookup keys only.

## Tests

- Unit:
  - crypto: round trip; tampered ciphertext, IV or tag fails; another row's
    AAD fails; unknown key id fails; decrypting with an older key works;
    `ENCRYPTION_KEYS` parsing (wrong length, duplicate id, bad base64, error
    text contains no value);
  - each provider's list parsing, paging and chat filter (fixtures);
    verification outcome mapping including Google's 400 `API_KEY_INVALID`;
  - agent input validation including per-provider temperature limits.
- Integration (dev database, local fake provider HTTP server):
  - P2/A1: members and non-members can't set/remove keys or write agents;
    non-members get `not_found` for reads too;
  - P3: a rejected key is not stored; a stored key is verified with the header
    the provider expects and never in the URL;
  - P4: `encrypted_key` does not contain the key; no read function except
    `getProviderKey` returns it; a ciphertext copied to another workspace's
    row doesn't decrypt;
  - P5/availability: removing a key keeps agents with `provider_not_configured`;
    a model missing from the list gives `model_unavailable`; an unreachable
    provider gives `unknown` and is retried after the failure TTL;
  - A2: duplicate names in different case fail; the same name in another
    workspace works;
  - A3: creating with an unconfigured provider or unknown model fails; editing
    the name of an agent whose model disappeared works;
  - A4: deleting a workspace removes keys and agents;
  - rotation: after adding a key and running the rotation, all rows use the new
    key id and still decrypt; the old key can be removed.
- E2E (fake provider server started by Playwright, base URLs via
  `AGENTY_*_BASE_URL`): admin sets an OpenAI key (hint shown); a rejected key
  shows the fixed message; creates an agent with a model from the list and
  Advanced parameters; edits it; a member sees it read-only and has no New
  agent button; removing the key marks the agent "Provider not configured";
  deleting the agent; personal workspace has working Agents and Providers tabs.

## Review focus

- Key leakage: logs, error messages, redirects with `?error=`, React props sent
  to the client (the agent form must never receive a key), provider error
  bodies.
- A model list that is huge, slow, paginates forever or returns garbage.
- Stale forms: an admin demoted while editing, an agent deleted in another tab,
  a key removed between loading the form and saving.
- Removing an encryption key that still has ciphertexts.

## M4 notes (not built here)

- `@workflow/ai`'s `DurableAgent` is deprecated and needs AI SDK 6; use
  `WorkflowAgent` from `@ai-sdk/workflow` (AI SDK 7).
- Never hand the agent a model built with a key (`createOpenAI({ apiKey })(id)`):
  the providers' workflow serialization stores resolved headers, i.e. the key,
  unencrypted in the workflow tables. Use a model wrapper that serializes only
  `(workspaceId, provider, modelId)` and calls `getProviderKey` inside the step;
  test that no key reaches the `workflow` schemas.
- Always pass `apiKey` explicitly; the SDKs fall back to `OPENAI_API_KEY` and
  similar env vars.
- `openai(id)` uses the Responses API.
- The scheduled model availability check.

## Docs

`AGENTS.md`: architecture (`src/server/crypto`, `providers`, `agents`, routes),
the Providers and Agents rules in short, encryption and rotation, the M4 notes
on key handling, new env vars. README: providers and agents from the user's view.
