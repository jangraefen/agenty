# M3 – Providers and agents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Workspace admins store verified, encrypted API keys for OpenAI, Anthropic and Google Gemini and create agents (name, description, system prompt, provider and model, optional parameters); members see everything read-only.

**Architecture:** A crypto core (`src/server/crypto`, AES-256-GCM, keyring from `ENCRYPTION_KEYS`) without `server-only` so a plain-Node rotation script can use it. `src/server/providers` lists models with plain `fetch` (bounded, one deadline, key in a header only); listing is also key verification. Keys and agents are workspace-owned tables behind service functions that follow the M2 pattern (explicit `actorId`, lock the workspace row, `WorkspaceError` codes). Model availability is computed lazily from a per-process cache. Pages are server components; the agent form is the one client form with `useActionState`.

**Tech Stack:** Next.js 16.4 (App Router, Cache Components, Partial Prefetching), React 19.3, Drizzle ORM 0.45 + postgres-js, Zod 4, `node:crypto`, Vitest, Playwright + `@next/playwright`, Biome, shadcn/ui (Base UI preset `base-nova`).

**Spec:** `docs/specs/2026-10-10-m3-providers-agents-design.md` (rules P1–P5 and A1–A4 are numbered there).

## Global Constraints

- Every `src/server/**` module starts with `import "server-only";`, except `src/server/crypto/keyring.ts`, `cipher.ts` and `rotate.ts` (plain Node runs them through `scripts/rotate-keys.ts`; only erasable TypeScript syntax and relative `.ts` imports there).
- Configuration only via `getEnv()`; database only via `getDb()` (the rotation script uses its own postgres client).
- Providers exactly `openai`, `anthropic`, `google`; display names exactly "OpenAI", "Anthropic", "Google Gemini".
- Base URLs: `https://api.openai.com/v1`, `https://api.anthropic.com/v1`, `https://generativelanguage.googleapis.com/v1beta`; overrides `AGENTY_OPENAI_BASE_URL`, `AGENTY_ANTHROPIC_BASE_URL`, `AGENTY_GOOGLE_BASE_URL` (https; http only for `localhost`/`127.0.0.1`).
- `ENCRYPTION_KEYS`: comma-separated `id:base64`; id `[a-z0-9]{1,16}`, unique; strict base64 (`z.base64()`) of exactly 32 bytes; the first is active. Ciphertext format `v1.<keyId>.<iv>.<tag>.<ciphertext>` (base64url parts), 12-byte IV, 16-byte tag, AAD `provider_key|<workspaceId>|<provider>`.
- Listing: one 10 s deadline for all pages, `redirect: "error"`, 5 MB per body while streaming, at most 5 000 models and 10 pages, Zod-validated, key in a header only.
- Provider key input: trimmed, `^[\x21-\x7E]{20,500}$`; the hint is the last 4 characters, shown to admins only.
- Agent input (trimmed): name 1–80, description 0–500, system prompt 0–20 000, model 1–200 matching `^[A-Za-z0-9._:/@-]+$`, temperature 0–2, top P > 0 and ≤ 1, max output tokens integer 1–1 000 000 (empty = provider default).
- New error codes (exact): `key_invalid`, `key_rejected`, `provider_unavailable`, `key_unverified`, `agent_name_taken`, `provider_not_configured`, `model_unavailable`, `invalid_parameters`, `invalid_agent`.
- Verification messages (exact): "The provider rejected this key.", "The provider could not be reached. Try again later.", "The key could not be verified. Check that it may list models."
- Agent statuses (exact UI text): "Ready", "Provider not configured", "Provider key unreadable", "Model unavailable", "Couldn't check model".
- Model cache: successes 10 minutes, failures 1 minute, keyed by workspace and provider and checked against the key's `updated_at`; one in-flight promise per key; sweep above 1 000 entries.
- The plain key never leaves `src/server/providers` except as the HTTP header; it never appears in responses, logs, error messages, redirects or client props. Provider response bodies are never logged or shown; logs carry provider id, HTTP status and error class only.
- Ids from requests are lookup keys only: every agent query filters by `workspace_id`; malformed ids are `not_found`.
- Every new page has a `loading.tsx` that only re-exports `@/components/page-loading` and an entry in `tests/e2e/instant.spec.ts`.
- Biome for lint/format (`task format`), TypeScript strict + `noUncheckedIndexedAccess`; no `!` non-null assertions.
- Tests named after the guarantee they protect; unit `*.test.ts`, integration `*.int.test.ts`; Playwright queries use `exact: true` where a name could match twice.
- Run tests through `task` (it loads `.env`); `task db:up` must be running for integration and E2E tests.
- `task ci` passes before every commit. Commit trailer exactly: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Never `git add -A` / `git add .` (agent worktrees live under `.claude/worktrees`); add paths explicitly. Never look at git branch `legacy`.

## Review Focus

1. **A key pasted with a trailing newline or spaces, or with whitespace or non-ASCII inside** → surrounding whitespace is trimmed and the key works; anything else is `key_invalid` before any request (`fetch` would throw on such header bytes). Test in Task 3 ("keys are trimmed; …").
2. **An encryption key removed from `ENCRYPTION_KEYS` while rows still use it** → the providers page says "Key unreadable, set it again", agents show "Provider key unreadable", setting the key again recovers, and rotation counts such rows as unreadable instead of failing. Tests in Task 3 (unreadable then recovered), Task 5 (rotation leaves and counts them) and Task 6 (`key_unreadable` status).
3. **The agent being edited is deleted in another tab** → saving lands on the agent list, not an error page. E2E test in Task 8.
4. **The provider key is removed between loading the agent form and saving** → the fixed message "Set an API key for this provider first." and everything typed (name, system prompt, chosen model) is still in the form. E2E test in Task 8.
5. **A provider that answers with endless pages, a huge body, garbage, or never** → a fixed code within the 10 s deadline, nothing stored, nothing of the body logged. Unit tests in Task 2 ("bad answers").

---

## File structure

```
src/lib/providers.ts                       provider ids, display names, model id rule, ModelOption (shared with client code)
src/server/crypto/keyring.ts               ENCRYPTION_KEYS parser (plain Node)
src/server/crypto/cipher.ts                AES-256-GCM with an explicit keyring, AAD helper, CryptoError (plain Node)
src/server/crypto/crypto.ts                server-only encrypt/decrypt/needsRotation with getEnv()'s keyring
src/server/crypto/rotate.ts                re-encrypts provider_key rows (plain Node; used by scripts/rotate-keys.ts)
src/server/providers/registry.ts           base URL per provider (defaults + operator overrides)
src/server/providers/listing.ts            listModels(): fetch, paging, filters, verification outcome mapping
src/server/providers/validation.ts         provider id and key input parsing
src/server/providers/keys.ts               setProviderKey, removeProviderKey, listProviderStatus, getProviderKey
src/server/providers/model-cache.ts        per-process cache primitives (TTLs, in-flight sharing, sweep, clock)
src/server/providers/models.ts             listModelsForWorkspace()
src/server/agents/validation.ts            agent input schema
src/server/agents/agents.ts                listAgents, getAgent, createAgent, updateAgent, deleteAgent
src/server/agents/status.ts                agent availability status
src/server/db/errors.ts                    isUniqueViolation()
src/components/submit-button.tsx           submit button with a pending label
src/app/(signed-in)/w/[workspaceId]/providers/   page, actions, loading
src/app/(signed-in)/w/[workspaceId]/agents/      list page, actions, form (client), delete dialog (client), form values, provider choices, new/, [agentId]/
scripts/rotate-keys.ts                     `task crypto:rotate`
tests/support/fake-provider.ts             fake model-listing server (in-process for integration tests)
tests/support/fake-provider-server.ts      the same as a process for Playwright
tests/support/workspaces.ts                sharedWorkspace(), expectWorkspaceError()
tests/e2e/support/sessions.ts              signed-in browser sessions, workspace helpers
tests/e2e/support/providers.ts             provider key helpers, response body collector
tests/e2e/support/agents.ts                agent form helpers
```

---

### Task 1: Encryption core and `ENCRYPTION_KEYS`

**Files:**
- Create: `src/server/crypto/keyring.ts`, `src/server/crypto/cipher.ts`, `src/server/crypto/crypto.ts`
- Modify: `src/server/env.ts`, `src/server/env.test.ts`
- Modify: `.env.example`, `.github/workflows/ci.yml`, `playwright.config.ts`
- Test: `src/server/crypto/keyring.test.ts`, `src/server/crypto/cipher.test.ts`

**Interfaces:**
- Produces:
  - `type Keyring = { activeId: string; keys: ReadonlyMap<string, Buffer> }`, `type KeyringResult = { ok: true; keyring: Keyring } | { ok: false; problem: string }`, `parseKeyring(value: string): KeyringResult` (keyring.ts)
  - `class CryptoError extends Error`, `providerKeyAad(workspaceId: string, provider: string): string`, `encryptWith(keyring, plaintext, aad): string`, `decryptWith(keyring, ciphertext, aad): string`, `keyIdOf(ciphertext): string | null`, `needsRotationWith(keyring, ciphertext): boolean` (cipher.ts)
  - `encrypt(plaintext: string, aad: string): string`, `decrypt(ciphertext: string, aad: string): string`, `needsRotation(ciphertext: string): boolean`, re-exports `CryptoError`, `providerKeyAad` (crypto.ts, server-only)
  - `Env.ENCRYPTION_KEYS: Keyring`

- [ ] **Step 1: Write the failing keyring test** — `src/server/crypto/keyring.test.ts`:

```ts
import { randomBytes } from "node:crypto";
import { describe, expect, it } from "vitest";
import { parseKeyring } from "./keyring";

const key = (bytes = 32) => randomBytes(bytes).toString("base64");

/** The problem text for an invalid value (fails the test if the value parses). */
function problemOf(value: string): string {
  const result = parseKeyring(value);
  if (result.ok) throw new Error("expected a problem");
  return result.problem;
}

describe("ENCRYPTION_KEYS parsing", () => {
  it("makes the first key the active one and keeps the others for decryption", () => {
    const result = parseKeyring(`new:${key()}, old:${key()}`);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.keyring.activeId).toBe("new");
    expect([...result.keyring.keys.keys()]).toEqual(["new", "old"]);
    expect(result.keyring.keys.get("old")?.length).toBe(32);
  });

  it("rejects keys that don't decode to exactly 32 bytes", () => {
    expect(problemOf(`a:${key(16)}`)).toBe("entry 1: the key must decode to 32 bytes");
    expect(problemOf(`a:${key()},b:${key(33)}`)).toBe("entry 2: the key must decode to 32 bytes");
  });

  it("rejects duplicate ids", () => {
    expect(problemOf(`a:${key()},a:${key()}`)).toBe("entry 2: duplicate id");
  });

  it("rejects malformed base64 and ids", () => {
    expect(problemOf("a:not base64!")).toBe("entry 1: the key must be base64");
    expect(problemOf(`Upper:${key()}`)).toBe("entry 1: the id must be 1-16 characters a-z or 0-9");
    expect(problemOf(`${"a".repeat(17)}:${key()}`)).toBe(
      "entry 1: the id must be 1-16 characters a-z or 0-9",
    );
    expect(problemOf(`:${key()}`)).toBe("entry 1: the id must be 1-16 characters a-z or 0-9");
    expect(problemOf("")).toBe("entry 1 must be id:base64");
    expect(problemOf(key())).toBe("entry 1 must be id:base64");
  });

  it("never puts key material in the problem text", () => {
    const short = key(16);
    const values = [`a:${short}`, `a:${short}x`, `a:${key()},a:${short}`, short];
    for (const value of values) expect(problemOf(value)).not.toContain(short);
  });
});
```

- [ ] **Step 2: Write the failing cipher test** — `src/server/crypto/cipher.test.ts`:

```ts
import { randomBytes } from "node:crypto";
import { describe, expect, it } from "vitest";
import {
  CryptoError,
  decryptWith,
  encryptWith,
  keyIdOf,
  needsRotationWith,
  providerKeyAad,
} from "./cipher";
import { type Keyring, parseKeyring } from "./keyring";

const secret = () => randomBytes(32).toString("base64");
function keyring(value: string): Keyring {
  const result = parseKeyring(value);
  if (!result.ok) throw new Error(result.problem);
  return result.keyring;
}

const oldSpec = `old:${secret()}`;
const newSpec = `new:${secret()}`;
const current = keyring(oldSpec);
const aad = providerKeyAad("6f1c1f7e-3d4b-4c55-9a43-1b2a5c6d7e8f", "openai");
const plaintext = "sk-test-0123456789abcdefghij";

/** Flips one bit in a base64url part (2 = IV, 3 = tag, 4 = ciphertext). */
function tamper(ciphertext: string, part: 2 | 3 | 4): string {
  const parts = ciphertext.split(".");
  const bytes = Buffer.from(parts[part] ?? "", "base64url");
  bytes[0] = (bytes[0] ?? 0) ^ 1;
  parts[part] = bytes.toString("base64url");
  return parts.join(".");
}

const fails = (run: () => unknown) => expect(run).toThrow(CryptoError);

describe("AES-256-GCM", () => {
  it("round-trips with the active key in the v1 format, with a fresh IV every time", () => {
    const first = encryptWith(current, plaintext, aad);
    const second = encryptWith(current, plaintext, aad);
    expect(first).toMatch(/^v1\.old\.[A-Za-z0-9_-]{16}\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]+$/);
    expect(first).not.toBe(second);
    expect(first).not.toContain(plaintext);
    expect(decryptWith(current, first, aad)).toBe(plaintext);
  });

  it("fails closed when the ciphertext, IV or tag was changed", () => {
    const ciphertext = encryptWith(current, plaintext, aad);
    for (const part of [2, 3, 4] as const) fails(() => decryptWith(current, tamper(ciphertext, part), aad));
  });

  it("does not decrypt a ciphertext copied to another row (different AAD)", () => {
    const ciphertext = encryptWith(current, plaintext, aad);
    fails(() => decryptWith(current, ciphertext, providerKeyAad("another-workspace", "openai")));
    fails(() => decryptWith(current, ciphertext, providerKeyAad("6f1c1f7e-3d4b-4c55-9a43-1b2a5c6d7e8f", "google")));
  });

  it("fails for an unknown key id and for malformed input", () => {
    const ciphertext = encryptWith(current, plaintext, aad);
    fails(() => decryptWith(keyring(newSpec), ciphertext, aad));
    for (const bad of ["", "v1.old", ciphertext.replace(/^v1/, "v2"), `${ciphertext}.x`, ciphertext.replace(/\.([^.]+)$/, ".a+b/")])
      fails(() => decryptWith(current, bad, aad));
  });

  it("decrypts with an older key after a new active key was added", () => {
    const ciphertext = encryptWith(current, plaintext, aad);
    const rotated = keyring(`${newSpec},${oldSpec}`);
    expect(decryptWith(rotated, ciphertext, aad)).toBe(plaintext);
    expect(needsRotationWith(rotated, ciphertext)).toBe(true);
    const reencrypted = encryptWith(rotated, plaintext, aad);
    expect(keyIdOf(reencrypted)).toBe("new");
    expect(needsRotationWith(rotated, reencrypted)).toBe(false);
  });

  it("reports failures without details", () => {
    const ciphertext = encryptWith(current, plaintext, aad);
    try {
      decryptWith(current, tamper(ciphertext, 4), aad);
      throw new Error("expected a CryptoError");
    } catch (error) {
      expect(error).toBeInstanceOf(CryptoError);
      expect((error as Error).message).toBe("Decryption failed");
    }
  });
});
```

- [ ] **Step 3: Run both to see them fail**

Run: `task test -- --project unit src/server/crypto`
Expected: FAIL (cannot resolve `./keyring` / `./cipher`).

- [ ] **Step 4: Write the keyring parser** — `src/server/crypto/keyring.ts`:

```ts
// Parses ENCRYPTION_KEYS. No "server-only" import: scripts/rotate-keys.ts runs this file in plain
// Node (type stripping), so only erasable TypeScript syntax and relative `.ts` imports here.
import { z } from "zod";

/** Encryption keys by id; `activeId` (the first one listed) encrypts, every key decrypts. */
export type Keyring = { activeId: string; keys: ReadonlyMap<string, Buffer> };
export type KeyringResult = { ok: true; keyring: Keyring } | { ok: false; problem: string };

const KEY_ID = /^[a-z0-9]{1,16}$/;
const KEY_BYTES = 32;
const base64 = z.base64();

/**
 * Comma-separated `id:base64` pairs. A problem names the entry by position, never a value: the
 * value is key material.
 */
export function parseKeyring(value: string): KeyringResult {
  const keys = new Map<string, Buffer>();
  for (const [index, entry] of value.split(",").entries()) {
    const position = `entry ${index + 1}`;
    const pair = entry.trim();
    const separator = pair.indexOf(":");
    if (separator < 0) return { ok: false, problem: `${position} must be id:base64` };
    const id = pair.slice(0, separator);
    const encoded = pair.slice(separator + 1);
    if (!KEY_ID.test(id)) {
      return { ok: false, problem: `${position}: the id must be 1-16 characters a-z or 0-9` };
    }
    if (keys.has(id)) return { ok: false, problem: `${position}: duplicate id` };
    if (!base64.safeParse(encoded).success) {
      return { ok: false, problem: `${position}: the key must be base64` };
    }
    const key = Buffer.from(encoded, "base64");
    if (key.length !== KEY_BYTES) {
      return { ok: false, problem: `${position}: the key must decode to 32 bytes` };
    }
    keys.set(id, key);
  }
  const [activeId] = keys.keys();
  // Unreachable (an empty value fails as "entry 1"), but keeps the type honest.
  if (activeId === undefined) return { ok: false, problem: "must list at least one id:base64 key" };
  return { ok: true, keyring: { activeId, keys } };
}
```

- [ ] **Step 5: Write the cipher** — `src/server/crypto/cipher.ts`:

```ts
// AES-256-GCM for secrets at rest. No "server-only" import: scripts/rotate-keys.ts runs this file
// in plain Node (type stripping), so only erasable TypeScript syntax and relative `.ts` imports.
import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";
import type { Keyring } from "./keyring.ts";

const VERSION = "v1";
const IV_BYTES = 12;
const TAG_BYTES = 16;
const BASE64URL = /^[A-Za-z0-9_-]*$/;

/** Decryption failed (unknown key id, malformed input, failed tag). Carries no details on purpose. */
export class CryptoError extends Error {
  constructor() {
    super("Decryption failed");
    this.name = "CryptoError";
  }
}

/** Binds a provider key's ciphertext to its row: a copy in another row doesn't decrypt. */
export function providerKeyAad(workspaceId: string, provider: string): string {
  return `provider_key|${workspaceId}|${provider}`;
}

/** `v1.<keyId>.<iv>.<tag>.<ciphertext>` (base64url parts), always with the active key. */
export function encryptWith(keyring: Keyring, plaintext: string, aad: string): string {
  const key = keyring.keys.get(keyring.activeId);
  if (!key) throw new Error("The active encryption key is missing");
  const iv = randomBytes(IV_BYTES);
  const cipher = createCipheriv("aes-256-gcm", key, iv, { authTagLength: TAG_BYTES });
  cipher.setAAD(Buffer.from(aad, "utf8"));
  const data = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
  return [
    VERSION,
    keyring.activeId,
    iv.toString("base64url"),
    cipher.getAuthTag().toString("base64url"),
    data.toString("base64url"),
  ].join(".");
}

/** One base64url part, in canonical spelling only, optionally of a fixed length. */
function decode(part: string, length?: number): Buffer {
  if (!BASE64URL.test(part)) throw new CryptoError();
  const bytes = Buffer.from(part, "base64url");
  if (bytes.toString("base64url") !== part) throw new CryptoError();
  if (length !== undefined && bytes.length !== length) throw new CryptoError();
  return bytes;
}

/** The key id of a v1 ciphertext, or null when it isn't one. */
export function keyIdOf(ciphertext: string): string | null {
  const parts = ciphertext.split(".");
  return parts.length === 5 && parts[0] === VERSION ? (parts[1] ?? null) : null;
}

/** Fails closed with CryptoError for anything but an intact ciphertext of a known key and `aad`. */
export function decryptWith(keyring: Keyring, ciphertext: string, aad: string): string {
  const parts = ciphertext.split(".");
  if (parts.length !== 5 || parts[0] !== VERSION) throw new CryptoError();
  const [, keyId = "", iv = "", tag = "", data = ""] = parts;
  const key = keyring.keys.get(keyId);
  if (!key) throw new CryptoError();
  try {
    const decipher = createDecipheriv("aes-256-gcm", key, decode(iv, IV_BYTES), {
      authTagLength: TAG_BYTES,
    });
    decipher.setAuthTag(decode(tag, TAG_BYTES));
    decipher.setAAD(Buffer.from(aad, "utf8"));
    return Buffer.concat([decipher.update(decode(data)), decipher.final()]).toString("utf8");
  } catch {
    throw new CryptoError();
  }
}

/** True when the ciphertext was not made with the active key (or isn't a v1 ciphertext). */
export function needsRotationWith(keyring: Keyring, ciphertext: string): boolean {
  return keyIdOf(ciphertext) !== keyring.activeId;
}
```

- [ ] **Step 6: Run the crypto unit tests**

Run: `task test -- --project unit src/server/crypto`
Expected: PASS (keyring 5, cipher 6 tests).

- [ ] **Step 7: Server wrapper** — `src/server/crypto/crypto.ts`:

```ts
import "server-only";
import { getEnv } from "@/server/env";
import { decryptWith, encryptWith, needsRotationWith } from "./cipher";

export { CryptoError, providerKeyAad } from "./cipher";

/** AES-256-GCM with the active ENCRYPTION_KEYS key; `aad` binds the ciphertext to its row. */
export function encrypt(plaintext: string, aad: string): string {
  return encryptWith(getEnv().ENCRYPTION_KEYS, plaintext, aad);
}

/** Throws CryptoError (no details) for unknown key ids, malformed input or a failed tag. */
export function decrypt(ciphertext: string, aad: string): string {
  return decryptWith(getEnv().ENCRYPTION_KEYS, ciphertext, aad);
}

export function needsRotation(ciphertext: string): boolean {
  return needsRotationWith(getEnv().ENCRYPTION_KEYS, ciphertext);
}
```

- [ ] **Step 8: Failing env tests** — in `src/server/env.test.ts`, add `ENCRYPTION_KEYS` to `valid` and replace the first test of `describe("parseEnv", …)`; then add a new describe block at the end of the file:

```ts
const TEST_KEY = Buffer.alloc(32, 7).toString("base64");

const valid = {
  DATABASE_URL: "postgres://agenty_app:s3cret@localhost:5432/agenty",
  BETTER_AUTH_SECRET: "test-secret-0123456789abcdef012345",
  BETTER_AUTH_URL: "http://localhost:3000",
  OIDC_DISCOVERY_URL: "http://localhost:8080/agenty/.well-known/openid-configuration",
  OIDC_CLIENT_ID: "agenty",
  OIDC_CLIENT_SECRET: "client-s3cret",
  ENCRYPTION_KEYS: `test:${TEST_KEY}`,
};

describe("parseEnv", () => {
  it("accepts a postgres URL and defaults NODE_ENV", () => {
    const { ENCRYPTION_KEYS, ...rest } = parseEnv(valid);
    const { ENCRYPTION_KEYS: _raw, ...expected } = valid;
    expect(rest).toEqual({ ...expected, NODE_ENV: "development" });
    expect(ENCRYPTION_KEYS.activeId).toBe("test");
  });
  // (the other tests of this block stay unchanged)
```

```ts
describe("parseEnv ENCRYPTION_KEYS", () => {
  it("parses the keyring with the first key active", () => {
    const other = Buffer.alloc(32, 9).toString("base64");
    const env = parseEnv({ ...valid, ENCRYPTION_KEYS: `new:${other},test:${TEST_KEY}` });
    expect(env.ENCRYPTION_KEYS.activeId).toBe("new");
    expect(env.ENCRYPTION_KEYS.keys.size).toBe(2);
  });

  it("is required", () => {
    expect(() => parseEnv({ ...valid, ENCRYPTION_KEYS: undefined })).toThrow(/ENCRYPTION_KEYS/);
  });

  it("names the problem without echoing key material", () => {
    const short = Buffer.alloc(16, 3).toString("base64");
    let message = "";
    try {
      parseEnv({ ...valid, ENCRYPTION_KEYS: `test:${short}` });
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).toContain("ENCRYPTION_KEYS: entry 1: the key must decode to 32 bytes");
    expect(message).not.toContain(short);
  });
});
```

Run: `task test -- --project unit src/server/env.test.ts`
Expected: FAIL (`ENCRYPTION_KEYS` is not parsed: `activeId` of a string is undefined).

- [ ] **Step 9: Parse `ENCRYPTION_KEYS` in env** — in `src/server/env.ts`, add the import and schema and the field:

```ts
import "server-only";
import { z } from "zod";
import { parseKeyring } from "./crypto/keyring";

const postgresUrl = z.url({ protocol: /^postgres(ql)?$/, error: "must be a postgres:// URL" });
const httpUrl = z.url({ protocol: /^https?$/, error: "must be an http(s) URL" });

/** The keyring from ENCRYPTION_KEYS; problems name the entry, never key material. */
const encryptionKeys = z.string({ error: "must be set" }).transform((value, ctx) => {
  const parsed = parseKeyring(value);
  if (!parsed.ok) {
    ctx.addIssue({ code: "custom", message: parsed.problem });
    return z.NEVER;
  }
  return parsed.keyring;
});

const envSchema = z.object({
  DATABASE_URL: postgresUrl,
  BETTER_AUTH_SECRET: z.string().min(32, "must be at least 32 characters"),
  BETTER_AUTH_URL: httpUrl,
  OIDC_DISCOVERY_URL: httpUrl,
  OIDC_CLIENT_ID: z.string().min(1, "must be set"),
  OIDC_CLIENT_SECRET: z.string().min(1, "must be set"),
  ENCRYPTION_KEYS: encryptionKeys,
  NODE_ENV: z.enum(["development", "test", "production"]).default("development"),
});
```

Run: `task test -- --project unit src/server/env.test.ts`
Expected: PASS.

- [ ] **Step 10: Configure the key everywhere the server starts**

1. `.env.example`, after the OIDC block:

```sh
# Encryption of stored secrets (provider API keys): comma-separated id:base64 keys of 32 bytes.
# The first key encrypts, the others only decrypt (rotation: README). Development value only;
# generate a real key with `openssl rand -base64 32`.
ENCRYPTION_KEYS=dev:ZGV2LW9ubHktZW5jcnlwdGlvbi1rZXktMzItYnl0ZXM=
```

2. Your local `.env` (not committed) needs it too: `grep -q '^ENCRYPTION_KEYS=' .env || grep '^ENCRYPTION_KEYS=' .env.example >> .env`.

3. `.github/workflows/ci.yml`: extend the top-level `env:` (Task's dotenv doesn't override variables that are already set, so the `ci` job uses this fixed test key):

```yaml
env:
  POSTGRES_URL_SUPERUSER: postgres://postgres:postgres@localhost:5432/agenty
  # Test-only encryption key (not a secret).
  ENCRYPTION_KEYS: ci:Y2ktb25seS1lbmNyeXB0aW9uLWtleS0zMi1ieXRlcyE=
```

and in the `docker` job's smoke test add `-e ENCRYPTION_KEYS \` after `-e OIDC_CLIENT_SECRET=ci \` (passes the value from the job environment).

4. `playwright.config.ts`, in `standaloneServer`'s `env`, after `DATABASE_MIGRATION_URL`:

```ts
      ENCRYPTION_KEYS: process.env.ENCRYPTION_KEYS ?? "",
```

- [ ] **Step 11: Full check and commit**

Run: `task format`, then `task ci` → all green (the E2E server starts only with a valid `ENCRYPTION_KEYS`).

```bash
git add src/server/crypto/keyring.ts src/server/crypto/cipher.ts src/server/crypto/crypto.ts src/server/crypto/keyring.test.ts src/server/crypto/cipher.test.ts src/server/env.ts src/server/env.test.ts .env.example .github/workflows/ci.yml playwright.config.ts
git commit -m "feat(crypto): AES-256-GCM keyring from ENCRYPTION_KEYS

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Provider registry and model listing

**Files:**
- Create: `src/lib/providers.ts`
- Create: `src/server/providers/registry.ts`, `src/server/providers/listing.ts`
- Modify: `src/server/env.ts`, `src/server/env.test.ts`
- Test: `src/server/providers/listing.test.ts`

**Interfaces:**
- Produces:
  - `PROVIDER_IDS = ["openai", "anthropic", "google"] as const`, `type ProviderId`, `PROVIDER_NAMES: Record<ProviderId, string>`, `MODEL_ID_PATTERN`, `MODEL_ID_MAX_LENGTH = 200`, `type ModelOption = { id: string; label: string }` (`src/lib/providers.ts`, no `server-only`: client components use it)
  - `DEFAULT_BASE_URLS`, `providerBaseUrl(provider: ProviderId): string` (registry.ts)
  - `type VerificationCode = "key_rejected" | "provider_unavailable" | "key_unverified"`, `type ListModelsResult = { ok: true; models: ModelOption[] } | { ok: false; code: VerificationCode }`, `listModels(provider, baseUrl, key, options?: { fetch?: typeof fetch; timeoutMs?: number }): Promise<ListModelsResult>`, `classifyFailure(provider, status, body): VerificationCode`, `isOpenAiChatModel(id)`, `isGoogleChatModel(model)`, constants `LIST_TIMEOUT_MS`, `MAX_BODY_BYTES`, `MAX_MODELS`, `MAX_PAGES` (listing.ts)
  - `Env.AGENTY_OPENAI_BASE_URL`, `Env.AGENTY_ANTHROPIC_BASE_URL`, `Env.AGENTY_GOOGLE_BASE_URL` (`string | undefined`, no trailing slash)

- [ ] **Step 1: Shared provider constants** — `src/lib/providers.ts`:

```ts
// Provider ids and names, shared by server and client code. No secrets and no server imports here.

export const PROVIDER_IDS = ["openai", "anthropic", "google"] as const;
export type ProviderId = (typeof PROVIDER_IDS)[number];

export const PROVIDER_NAMES: Record<ProviderId, string> = {
  openai: "OpenAI",
  anthropic: "Anthropic",
  google: "Google Gemini",
};

/** Model ids an agent may store; listings leave out anything else. */
export const MODEL_ID_PATTERN = /^[A-Za-z0-9._:/@-]+$/;
export const MODEL_ID_MAX_LENGTH = 200;

/** A model a provider lists for a key: `id` is what agents store, `label` what the UI shows. */
export type ModelOption = { id: string; label: string };
```

- [ ] **Step 2: Write the failing listing test** — `src/server/providers/listing.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { classifyFailure, listModels, MAX_BODY_BYTES, MAX_MODELS } from "./listing";

const BASE = "https://provider.test/v1";
const KEY = "sk-test-0123456789abcdefghij";

type Call = { url: string; headers: Headers; init: RequestInit };

/** A fetch that answers with `responses` in order (an Error is thrown instead) and records calls. */
function fakeFetch(...responses: Array<Response | Error>) {
  const calls: Call[] = [];
  const impl = (async (input: RequestInfo | URL, init: RequestInit = {}) => {
    calls.push({ url: String(input), headers: new Headers(init.headers), init });
    const next = responses.shift();
    if (next === undefined) throw new Error("unexpected request");
    if (next instanceof Error) throw next;
    return next;
  }) as typeof fetch;
  return { impl, calls };
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
const warnings = () => vi.mocked(console.warn).mock.calls.flat().map(String).join("\n");

beforeEach(() => {
  vi.spyOn(console, "warn").mockImplementation(() => {});
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("OpenAI", () => {
  it("lists chat models only, sorted, with the key in the Authorization header", async () => {
    const ids = [
      "gpt-4o",
      "o3",
      "chatgpt-4o-latest",
      "gpt-4o-mini",
      "o4-mini",
      "gpt-4o-realtime-preview",
      "gpt-4o-mini-tts",
      "gpt-4o-transcribe",
      "gpt-4o-audio-preview",
      "gpt-4o-search-preview",
      "gpt-image-1",
      "gpt-3.5-turbo-instruct",
      "text-embedding-3-large",
      "whisper-1",
      "dall-e-3",
      "omni-moderation-latest",
      "babbage-002",
      "davinci-002",
    ];
    const { impl, calls } = fakeFetch(
      json({ object: "list", data: ids.map((id) => ({ id, object: "model", owned_by: "openai" })) }),
    );

    expect(await listModels("openai", BASE, KEY, { fetch: impl })).toEqual({
      ok: true,
      models: ["chatgpt-4o-latest", "gpt-4o", "gpt-4o-mini", "o3", "o4-mini"].map((id) => ({
        id,
        label: id,
      })),
    });
    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toBe(`${BASE}/models`);
    expect(calls[0]?.headers.get("authorization")).toBe(`Bearer ${KEY}`);
    expect(calls[0]?.init.redirect).toBe("error");
    expect(calls[0]?.url).not.toContain(KEY);
  });

  it("leaves out model ids an agent couldn't store", async () => {
    const { impl } = fakeFetch(
      json({ data: [{ id: "gpt-5" }, { id: "gpt-5 beta" }, { id: `gpt-${"x".repeat(200)}` }] }),
    );
    expect(await listModels("openai", BASE, KEY, { fetch: impl })).toEqual({
      ok: true,
      models: [{ id: "gpt-5", label: "gpt-5" }],
    });
  });
});

describe("Anthropic", () => {
  it("follows after_id while has_more, with x-api-key and anthropic-version", async () => {
    const { impl, calls } = fakeFetch(
      json({
        data: [{ id: "claude-opus-4-1", display_name: "Claude Opus 4.1", type: "model" }],
        has_more: true,
        first_id: "claude-opus-4-1",
        last_id: "claude-opus-4-1",
      }),
      json({
        data: [{ id: "claude-haiku-4-5", display_name: "Claude Haiku 4.5", type: "model" }],
        has_more: false,
        last_id: "claude-haiku-4-5",
      }),
    );

    expect(await listModels("anthropic", BASE, KEY, { fetch: impl })).toEqual({
      ok: true,
      models: [
        { id: "claude-haiku-4-5", label: "Claude Haiku 4.5" },
        { id: "claude-opus-4-1", label: "Claude Opus 4.1" },
      ],
    });
    expect(calls.map((call) => call.url)).toEqual([
      `${BASE}/models?limit=1000`,
      `${BASE}/models?limit=1000&after_id=claude-opus-4-1`,
    ]);
    expect(calls[0]?.headers.get("x-api-key")).toBe(KEY);
    expect(calls[0]?.headers.get("anthropic-version")).toBe("2023-06-01");
  });
});

describe("Google", () => {
  it("follows pageToken, keeps generateContent models only and strips models/", async () => {
    const model = (name: string, displayName: string, methods: string[]) => ({
      name: `models/${name}`,
      displayName,
      supportedGenerationMethods: methods,
    });
    const { impl, calls } = fakeFetch(
      json({
        models: [
          model("gemini-2.5-pro", "Gemini 2.5 Pro", ["generateContent", "countTokens"]),
          model("text-embedding-004", "Text Embedding 004", ["embedContent"]),
          model("gemini-embedding-001", "Gemini Embedding", ["generateContent"]),
          model("imagen-4.0-generate-001", "Imagen 4", ["predict"]),
        ],
        nextPageToken: "page 2",
      }),
      json({
        models: [
          model("gemini-2.5-flash", "Gemini 2.5 Flash", ["generateContent"]),
          model("gemini-2.5-flash-image", "Nano Banana", ["generateContent"]),
          model("gemini-2.0-flash-live-001", "Live", ["bidiGenerateContent"]),
          model("aqa", "AQA", ["generateAnswer", "generateContent"]),
          model("gemini-2.5-flash-preview-tts", "TTS", ["generateContent"]),
        ],
      }),
    );

    expect(await listModels("google", BASE, KEY, { fetch: impl })).toEqual({
      ok: true,
      models: [
        { id: "gemini-2.5-flash", label: "Gemini 2.5 Flash" },
        { id: "gemini-2.5-pro", label: "Gemini 2.5 Pro" },
      ],
    });
    expect(calls.map((call) => call.url)).toEqual([
      `${BASE}/models?pageSize=1000`,
      `${BASE}/models?pageSize=1000&pageToken=page%202`,
    ]);
    expect(calls[0]?.headers.get("x-goog-api-key")).toBe(KEY);
  });

  it("treats a 400 with API_KEY_INVALID as a rejected key", async () => {
    const { impl } = fakeFetch(
      json(
        {
          error: {
            code: 400,
            message: `API key not valid: ${KEY}`,
            details: [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "API_KEY_INVALID" }],
          },
        },
        400,
      ),
    );
    expect(await listModels("google", BASE, KEY, { fetch: impl })).toEqual({
      ok: false,
      code: "key_rejected",
    });
  });
});

describe("verification outcomes", () => {
  const invalidKey = { error: { details: [{ reason: "API_KEY_INVALID" }] } };
  it.each([
    ["openai", 401, null, "key_rejected"],
    ["anthropic", 401, null, "key_rejected"],
    ["google", 401, null, "key_rejected"],
    ["google", 400, invalidKey, "key_rejected"],
    ["google", 400, { error: { details: [{ reason: "OTHER" }] } }, "key_unverified"],
    ["openai", 400, invalidKey, "key_unverified"],
    ["openai", 403, null, "key_unverified"],
    ["anthropic", 404, null, "key_unverified"],
    ["openai", 429, null, "provider_unavailable"],
    ["anthropic", 500, null, "provider_unavailable"],
    ["google", 503, null, "provider_unavailable"],
  ] as const)("%s %i maps to %s", (provider, status, body, code) => {
    expect(classifyFailure(provider, status, body)).toBe(code);
  });
});

describe("bad answers end with a fixed code", () => {
  it("a network error or a refused redirect: provider_unavailable", async () => {
    const { impl } = fakeFetch(new TypeError("fetch failed"));
    expect(await listModels("openai", BASE, KEY, { fetch: impl })).toEqual({
      ok: false,
      code: "provider_unavailable",
    });
  });

  it("no answer before the deadline: provider_unavailable", async () => {
    const hanging = ((_input: RequestInfo | URL, init: RequestInit = {}) =>
      new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener("abort", () => reject(init.signal?.reason));
      })) as typeof fetch;
    expect(await listModels("openai", BASE, KEY, { fetch: hanging, timeoutMs: 20 })).toEqual({
      ok: false,
      code: "provider_unavailable",
    });
  });

  it("a body over 5 MB: key_unverified", async () => {
    const { impl } = fakeFetch(new Response("x".repeat(MAX_BODY_BYTES + 1)));
    expect(await listModels("openai", BASE, KEY, { fetch: impl })).toEqual({
      ok: false,
      code: "key_unverified",
    });
  });

  it("garbage or the wrong shape: key_unverified", async () => {
    for (const response of [new Response("<html>"), json({ models: [] }), json({ data: "x" })]) {
      const { impl } = fakeFetch(response);
      expect(await listModels("openai", BASE, KEY, { fetch: impl })).toEqual({
        ok: false,
        code: "key_unverified",
      });
    }
  });

  it("more than 5 000 models: key_unverified", async () => {
    const data = Array.from({ length: MAX_MODELS + 1 }, (_, i) => ({ id: `gpt-${i}` }));
    const { impl } = fakeFetch(json({ data }));
    expect(await listModels("openai", BASE, KEY, { fetch: impl })).toEqual({
      ok: false,
      code: "key_unverified",
    });
  });

  it("pages that never end: key_unverified after 10 requests", async () => {
    const pages = Array.from({ length: 11 }, (_, i) =>
      json({ data: [{ id: `claude-${i}` }], has_more: true, last_id: `claude-${i}` }),
    );
    const { impl, calls } = fakeFetch(...pages);
    expect(await listModels("anthropic", BASE, KEY, { fetch: impl })).toEqual({
      ok: false,
      code: "key_unverified",
    });
    expect(calls).toHaveLength(10);
  });

  it("logs provider, status and error class, never the body or the key", async () => {
    const { impl } = fakeFetch(
      json({ error: { message: `Incorrect API key provided: ${KEY}` } }, 401),
    );
    expect(await listModels("openai", BASE, KEY, { fetch: impl })).toEqual({
      ok: false,
      code: "key_rejected",
    });
    expect(warnings()).toContain("provider=openai status=401");
    expect(warnings()).not.toContain(KEY);
    expect(warnings()).not.toContain("Incorrect");
  });
});
```

- [ ] **Step 3: Run it to see it fail**

Run: `task test -- --project unit src/server/providers/listing.test.ts`
Expected: FAIL (cannot resolve `./listing`).

- [ ] **Step 4: Write the listing module** — `src/server/providers/listing.ts`:

```ts
import "server-only";
import { z } from "zod";
import {
  MODEL_ID_MAX_LENGTH,
  MODEL_ID_PATTERN,
  type ModelOption,
  type ProviderId,
} from "@/lib/providers";

export type VerificationCode = "key_rejected" | "provider_unavailable" | "key_unverified";
export type ListModelsResult =
  | { ok: true; models: ModelOption[] }
  | { ok: false; code: VerificationCode };
export type ListModelsOptions = { fetch?: typeof fetch; timeoutMs?: number };

export const LIST_TIMEOUT_MS = 10_000;
export const MAX_BODY_BYTES = 5 * 1024 * 1024;
export const MAX_MODELS = 5_000;
export const MAX_PAGES = 10;

/** A listing that ended with a known outcome. Carries no provider text. */
class ListingFailure extends Error {
  readonly code: VerificationCode;
  readonly status: number | undefined;
  constructor(code: VerificationCode, status?: number) {
    super(code);
    this.name = "ListingFailure";
    this.code = code;
    this.status = status;
  }
}

const OPENAI_CHAT = /^(gpt-|o\d|chatgpt-)/;
const OPENAI_NOT_CHAT =
  /embed|tts|whisper|transcribe|audio|realtime|dall-e|image|moderation|search|instruct|davinci|babbage/;
const GOOGLE_NOT_CHAT = /embedding|imagen|veo|aqa|tts|image|live/;

/** OpenAI returns no capabilities, so chat models are recognised by their id (a heuristic). */
export function isOpenAiChatModel(id: string): boolean {
  return OPENAI_CHAT.test(id) && !OPENAI_NOT_CHAT.test(id);
}

export function isGoogleChatModel(model: {
  name: string;
  supportedGenerationMethods?: string[] | undefined;
}): boolean {
  return (
    (model.supportedGenerationMethods ?? []).includes("generateContent") &&
    !GOOGLE_NOT_CHAT.test(model.name)
  );
}

const googleErrorSchema = z.object({
  error: z.object({ details: z.array(z.object({ reason: z.string().optional() })) }),
});

/** A non-2xx answer as a fixed code. `body` is only read for Google's 400. */
export function classifyFailure(
  provider: ProviderId,
  status: number,
  body: unknown,
): VerificationCode {
  if (status === 401) return "key_rejected";
  if (provider === "google" && status === 400) {
    const parsed = googleErrorSchema.safeParse(body);
    if (parsed.success && parsed.data.error.details.some((d) => d.reason === "API_KEY_INVALID")) {
      return "key_rejected";
    }
  }
  if (status === 429 || status >= 500) return "provider_unavailable";
  return "key_unverified";
}

function authHeaders(provider: ProviderId, key: string): Record<string, string> {
  switch (provider) {
    case "openai":
      return { authorization: `Bearer ${key}` };
    case "anthropic":
      return { "x-api-key": key, "anthropic-version": "2023-06-01" };
    case "google":
      return { "x-goog-api-key": key };
  }
}

/** Reads at most MAX_BODY_BYTES while streaming; Content-Length is not trusted. */
async function readText(response: Response): Promise<string> {
  if (!response.body) return "";
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > MAX_BODY_BYTES) throw new ListingFailure("key_unverified", response.status);
      chunks.push(value);
    }
  } finally {
    void reader.cancel().catch(() => {});
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function readJson(response: Response): Promise<unknown> {
  const text = await readText(response);
  try {
    return JSON.parse(text);
  } catch {
    throw new ListingFailure("key_unverified", response.status);
  }
}

function parse<T>(schema: z.ZodType<T>, body: unknown): T {
  const parsed = schema.safeParse(body);
  if (!parsed.success) throw new ListingFailure("key_unverified");
  return parsed.data;
}

type Get = (path: string) => Promise<unknown>;

const openAiSchema = z.object({ data: z.array(z.object({ id: z.string() })).max(MAX_MODELS) });

async function listOpenAi(get: Get): Promise<ModelOption[]> {
  const { data } = parse(openAiSchema, await get("/models"));
  return data.filter((m) => isOpenAiChatModel(m.id)).map((m) => ({ id: m.id, label: m.id }));
}

const anthropicSchema = z.object({
  data: z.array(z.object({ id: z.string(), display_name: z.string().optional() })),
  has_more: z.boolean(),
  last_id: z.string().nullable().optional(),
});

async function listAnthropic(get: Get): Promise<ModelOption[]> {
  const models: ModelOption[] = [];
  let afterId: string | null = null;
  for (let page = 0; page < MAX_PAGES; page++) {
    const query = afterId === null ? "" : `&after_id=${encodeURIComponent(afterId)}`;
    const body = parse(anthropicSchema, await get(`/models?limit=1000${query}`));
    for (const m of body.data) models.push({ id: m.id, label: m.display_name || m.id });
    if (models.length > MAX_MODELS) throw new ListingFailure("key_unverified");
    if (!body.has_more) return models;
    if (!body.last_id) throw new ListingFailure("key_unverified");
    afterId = body.last_id;
  }
  throw new ListingFailure("key_unverified"); // still more pages after MAX_PAGES
}

const googleSchema = z.object({
  models: z
    .array(
      z.object({
        name: z.string(),
        displayName: z.string().optional(),
        supportedGenerationMethods: z.array(z.string()).optional(),
      }),
    )
    .optional(),
  nextPageToken: z.string().optional(),
});

async function listGoogle(get: Get): Promise<ModelOption[]> {
  const models: ModelOption[] = [];
  let seen = 0;
  let pageToken: string | undefined;
  for (let page = 0; page < MAX_PAGES; page++) {
    const query = pageToken === undefined ? "" : `&pageToken=${encodeURIComponent(pageToken)}`;
    const body = parse(googleSchema, await get(`/models?pageSize=1000${query}`));
    const listed = body.models ?? [];
    seen += listed.length;
    if (seen > MAX_MODELS) throw new ListingFailure("key_unverified");
    for (const m of listed) {
      if (!isGoogleChatModel(m)) continue;
      const id = m.name.replace(/^models\//, "");
      models.push({ id, label: m.displayName || id });
    }
    if (!body.nextPageToken) return models;
    pageToken = body.nextPageToken;
  }
  throw new ListingFailure("key_unverified"); // still more pages after MAX_PAGES
}

const LISTERS: Record<ProviderId, (get: Get) => Promise<ModelOption[]>> = {
  openai: listOpenAi,
  anthropic: listAnthropic,
  google: listGoogle,
};

/** Ids an agent can store, each once, sorted by label. */
function finish(models: ModelOption[]): ModelOption[] {
  const byId = new Map<string, ModelOption>();
  for (const m of models) {
    if (m.id.length <= MODEL_ID_MAX_LENGTH && MODEL_ID_PATTERN.test(m.id) && !byId.has(m.id)) {
      byId.set(m.id, m);
    }
  }
  return [...byId.values()].sort(
    (a, b) => a.label.localeCompare(b.label, "en") || a.id.localeCompare(b.id, "en"),
  );
}

/**
 * The chat models `key` may use; listing is also how a key is verified. One deadline for all
 * pages, no redirects, bounded bodies, the key in a header only. Failures are logged as provider,
 * status and error class: provider bodies can echo the key, so they are never logged or returned.
 */
export async function listModels(
  provider: ProviderId,
  baseUrl: string,
  key: string,
  options: ListModelsOptions = {},
): Promise<ListModelsResult> {
  const fetchImpl = options.fetch ?? fetch;
  const signal = AbortSignal.timeout(options.timeoutMs ?? LIST_TIMEOUT_MS);
  const headers = { accept: "application/json", ...authHeaders(provider, key) };
  const get: Get = async (path) => {
    const response = await fetchImpl(`${baseUrl}${path}`, { headers, redirect: "error", signal });
    if (!response.ok) {
      const body =
        provider === "google" && response.status === 400
          ? await readJson(response).catch(() => null)
          : null;
      void response.body?.cancel().catch(() => {});
      throw new ListingFailure(classifyFailure(provider, response.status, body), response.status);
    }
    return readJson(response);
  };

  try {
    return { ok: true, models: finish(await LISTERS[provider](get)) };
  } catch (error) {
    // Anything without a known outcome (network error, deadline, refused redirect) means the
    // provider could not be reached.
    const failure = error instanceof ListingFailure ? error : null;
    const code = failure?.code ?? "provider_unavailable";
    const errorClass = error instanceof Error ? error.name : "unknown";
    console.warn(
      `Model listing failed: provider=${provider} status=${failure?.status ?? "none"} code=${code} error=${errorClass}`,
    );
    return { ok: false, code };
  }
}
```

- [ ] **Step 5: Run the listing test**

Run: `task test -- --project unit src/server/providers/listing.test.ts`
Expected: PASS (OpenAI 2, Anthropic 1, Google 2, outcomes 11, bad answers 7).

- [ ] **Step 6: Failing env tests for the base URL overrides** — append to `src/server/env.test.ts` (add `import type { Env } from "./env";` next to the existing import):

```ts
describe("parseEnv provider base URLs", () => {
  const names = [
    "AGENTY_OPENAI_BASE_URL",
    "AGENTY_ANTHROPIC_BASE_URL",
    "AGENTY_GOOGLE_BASE_URL",
  ] as const satisfies readonly (keyof Env)[];

  it.each(names)("%s is optional, and an empty value counts as unset", (name) => {
    expect(parseEnv(valid)[name]).toBeUndefined();
    expect(parseEnv({ ...valid, [name]: "" })[name]).toBeUndefined();
  });

  it.each(names)("%s accepts https and drops a trailing slash", (name) => {
    expect(parseEnv({ ...valid, [name]: "https://proxy.example.com/openai/v1/" })[name]).toBe(
      "https://proxy.example.com/openai/v1",
    );
  });

  it.each(names)("%s allows http only for localhost and 127.0.0.1", (name) => {
    expect(parseEnv({ ...valid, [name]: "http://127.0.0.1:3199/x" })[name]).toBe(
      "http://127.0.0.1:3199/x",
    );
    expect(parseEnv({ ...valid, [name]: "http://localhost:3199/x" })[name]).toBe(
      "http://localhost:3199/x",
    );
    expect(() => parseEnv({ ...valid, [name]: "http://proxy.example.com/x" })).toThrow(
      new RegExp(`${name}: must use https`),
    );
    expect(() => parseEnv({ ...valid, [name]: "ftp://localhost/x" })).toThrow(new RegExp(name));
  });
});
```

Run: `task test -- --project unit src/server/env.test.ts`
Expected: FAIL (the variables are not part of the schema yet).

- [ ] **Step 7: Add the overrides to env** — in `src/server/env.ts`, after `encryptionKeys`:

```ts
const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1"]);

/** Operator override of a provider's base URL: https, or http for localhost; "" counts as unset. */
const providerBaseUrl = z.preprocess(
  (value) => (value === "" ? undefined : value),
  z
    .url({ protocol: /^https?$/, error: "must be an http(s) URL" })
    .refine((value) => {
      const url = new URL(value);
      return url.protocol === "https:" || LOCAL_HOSTS.has(url.hostname);
    }, "must use https (http only for localhost and 127.0.0.1)")
    .transform((value) => value.replace(/\/+$/, ""))
    .optional(),
);
```

and in `envSchema`, after `ENCRYPTION_KEYS`:

```ts
  AGENTY_OPENAI_BASE_URL: providerBaseUrl,
  AGENTY_ANTHROPIC_BASE_URL: providerBaseUrl,
  AGENTY_GOOGLE_BASE_URL: providerBaseUrl,
```

Run: `task test -- --project unit src/server/env.test.ts`
Expected: PASS.

- [ ] **Step 8: Registry** — `src/server/providers/registry.ts`:

```ts
import "server-only";
import type { ProviderId } from "@/lib/providers";
import { getEnv } from "@/server/env";

/**
 * Base URLs including the API version (as the AI SDK's `baseURL` expects in M4); request paths
 * are relative to them. Workspaces never enter URLs; only the operator can override them.
 */
export const DEFAULT_BASE_URLS: Record<ProviderId, string> = {
  openai: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com/v1",
  google: "https://generativelanguage.googleapis.com/v1beta",
};

export function providerBaseUrl(provider: ProviderId): string {
  const env = getEnv();
  const overrides: Record<ProviderId, string | undefined> = {
    openai: env.AGENTY_OPENAI_BASE_URL,
    anthropic: env.AGENTY_ANTHROPIC_BASE_URL,
    google: env.AGENTY_GOOGLE_BASE_URL,
  };
  return overrides[provider] ?? DEFAULT_BASE_URLS[provider];
}
```

(The override is exercised by every integration test from Task 3 on.)

- [ ] **Step 9: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/lib/providers.ts src/server/providers/registry.ts src/server/providers/listing.ts src/server/providers/listing.test.ts src/server/env.ts src/server/env.test.ts
git commit -m "feat(providers): model listing and key verification for OpenAI, Anthropic and Google

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 3: Tables and the provider key service

**Files:**
- Modify: `src/server/db/schema.ts`
- Create: `src/server/db/migrations/0004_providers_agents.sql` and its `meta/` snapshot (generated)
- Modify: `src/server/workspaces/errors.ts`, `src/server/workspaces/internal.ts`
- Create: `src/server/providers/validation.ts`, `src/server/providers/keys.ts`
- Create: `tests/support/fake-provider.ts`, `tests/support/workspaces.ts`
- Test: `src/server/providers/validation.test.ts`, `src/server/providers/keys.int.test.ts`

**Interfaces:**
- Consumes: `PROVIDER_IDS`, `ProviderId` (Task 2); `listModels`, `providerBaseUrl` (Task 2); `encrypt`, `decrypt`, `CryptoError`, `providerKeyAad` (Task 1); `encryptWith`, `parseKeyring` (Task 1, in tests); `lockForAdmin`, `requireMembership`, `WorkspaceError`, `testUsers()`, `createWorkspace` (M2).
- Produces:
  - tables `providerKey` (`app.provider_key`) and `agent` (`app.agent`, used from Task 6), unique index `agent_workspace_name_unique`
  - `WorkspaceErrorCode` gains `key_invalid`, `key_rejected`, `provider_unavailable`, `key_unverified`
  - `requireAdminRole(userId: string, workspaceId: unknown): Promise<WorkspaceAccess>` (internal.ts; plain read, `forbidden` for members)
  - `parseProvider(value: unknown): ProviderId` (`not_found`), `parseProviderKey(value: unknown): string` (`key_invalid`) (providers/validation.ts)
  - `type ProviderStatus = { provider: ProviderId; configured: boolean; readable: boolean; hint?: string; updatedAt?: Date }`
  - `type ProviderKeyLookup = { status: "ok"; key: string; updatedAt: Date } | { status: "not_configured" } | { status: "unreadable" }`
  - `setProviderKey(actorId: string, workspaceId: string, provider: string, key: string): Promise<void>`
  - `removeProviderKey(actorId: string, workspaceId: string, provider: string): Promise<void>`
  - `listProviderStatus(actorId: string, workspaceId: string): Promise<ProviderStatus[]>` (in `PROVIDER_IDS` order)
  - `getProviderKey(workspaceId: string, provider: ProviderId): Promise<ProviderKeyLookup>` (the only function returning a plain key; no actor)
  - test support: `startFakeProvider(port?: number): Promise<FakeProvider>` with `baseUrl(provider)`, `requests`, `models`, `failure`, `beforeRespond`, `useAsProviderEnv()`, `close()`; `DEFAULT_FAKE_MODELS`; `sharedWorkspace(users, name?)` → `{ id, admin, member, outsider }`; `expectWorkspaceError(promise, code)`

- [ ] **Step 1: Add the tables** — in `src/server/db/schema.ts`, extend the imports and append both tables:

```ts
import { sql } from "drizzle-orm";
import {
  check,
  index,
  integer,
  pgSchema,
  primaryKey,
  real,
  text,
  timestamp,
  unique,
  uniqueIndex,
  uuid,
} from "drizzle-orm/pg-core";
// Relative import: drizzle-kit loads this file without the `@/` alias.
import { PROVIDER_IDS } from "../../lib/providers";
import { user } from "./auth-schema";
```

```ts
/** P1: at most one key per provider and workspace. `encryptedKey` is AES-256-GCM (src/server/crypto). */
export const providerKey = app.table(
  "provider_key",
  {
    workspaceId: uuid("workspace_id")
      .notNull()
      .references(() => workspace.id, { onDelete: "cascade" }),
    provider: text("provider", { enum: PROVIDER_IDS }).notNull(),
    encryptedKey: text("encrypted_key").notNull(),
    keyHint: text("key_hint").notNull(),
    updatedByUserId: uuid("updated_by_user_id").references(() => user.id, {
      onDelete: "set null",
    }),
    updatedAt: timestamp("updated_at", { withTimezone: true }).defaultNow().notNull(),
  },
  (table) => [
    primaryKey({ columns: [table.workspaceId, table.provider] }),
    check("provider_key_provider", sql`"provider" in ('openai', 'anthropic', 'google')`),
    check("provider_key_hint_length", sql`char_length("key_hint") = 4`),
  ],
);

/**
 * An agent of a workspace. No foreign key to provider_key (P5: removing a key keeps agents).
 * Length checks are backstops for the Zod rules in src/server/agents/validation.ts.
 */
export const agent = app.table(
  "agent",
  {
    id: uuid("id").defaultRandom().primaryKey(),
    workspaceId: uuid("workspace_id")
      .notNull()
      .references(() => workspace.id, { onDelete: "cascade" }),
    name: text("name").notNull(),
    description: text("description").notNull().default(""),
    systemPrompt: text("system_prompt").notNull().default(""),
    provider: text("provider", { enum: PROVIDER_IDS }).notNull(),
    model: text("model").notNull(),
    temperature: real("temperature"),
    topP: real("top_p"),
    maxOutputTokens: integer("max_output_tokens"),
    createdByUserId: uuid("created_by_user_id").references(() => user.id, {
      onDelete: "set null",
    }),
    createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).defaultNow().notNull(),
  },
  (table) => [
    // A2: names are unique per workspace ignoring case; also serves listing by workspace.
    uniqueIndex("agent_workspace_name_unique").on(table.workspaceId, sql`lower(${table.name})`),
    check("agent_provider", sql`"provider" in ('openai', 'anthropic', 'google')`),
    check("agent_name_length", sql`char_length("name") between 1 and 80`),
    check("agent_description_length", sql`char_length("description") <= 500`),
    check("agent_system_prompt_length", sql`char_length("system_prompt") <= 20000`),
    check("agent_model_length", sql`char_length("model") between 1 and 200`),
  ],
);
```

- [ ] **Step 2: Generate and apply the migration**

Run: `task db:generate -- --name providers_agents`
Expected: `src/server/db/migrations/0004_providers_agents.sql` with `CREATE TABLE "app"."provider_key"` and `"app"."agent"`, the primary key, the five check constraints of `agent` and two of `provider_key`, the foreign keys (`ON DELETE cascade` / `set null`) and `CREATE UNIQUE INDEX "agent_workspace_name_unique" ON "app"."agent" USING btree ("workspace_id",lower("name"))`. Open it and confirm the index expression reads `lower("name")` (unqualified); if drizzle-kit wrote a table-qualified column, change the schema to `` sql`lower("name")` `` and generate again. It must not touch other tables. Then `task db:migrate`. The app role gets access through the default privileges of migration 0001; nothing to grant.

- [ ] **Step 3: Error codes and the pre-transaction admin check**

In `src/server/workspaces/errors.ts`, append to `WORKSPACE_ERROR_CODES` (after `"confirmation_mismatch"`):

```ts
  // M3: provider keys
  "key_invalid",
  "key_rejected",
  "provider_unavailable",
  "key_unverified",
```

In `src/server/workspaces/internal.ts`, after `requireMembership`:

```ts
/**
 * Admins only, without a lock: for checks before slow work outside a transaction (a provider
 * call). The transaction that follows must check again with lockForAdmin.
 */
export async function requireAdminRole(
  userId: string,
  workspaceId: unknown,
): Promise<WorkspaceAccess> {
  const access = await requireMembership(userId, workspaceId);
  if (access.role !== "admin") throw new WorkspaceError("forbidden");
  return access;
}
```

- [ ] **Step 4: Failing validation test** — `src/server/providers/validation.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { WorkspaceError } from "@/server/workspaces/errors";
import { parseProvider, parseProviderKey } from "./validation";

const codeOf = (run: () => unknown) => {
  try {
    run();
    return "ok";
  } catch (error) {
    return error instanceof WorkspaceError ? error.code : "other";
  }
};

describe("provider keys", () => {
  it("are trimmed", () => {
    expect(parseProviderKey("  sk-0123456789abcdefghij \n")).toBe("sk-0123456789abcdefghij");
  });

  it("need 20 to 500 printable ASCII characters without spaces", () => {
    expect(codeOf(() => parseProviderKey("x".repeat(20)))).toBe("ok");
    expect(codeOf(() => parseProviderKey("x".repeat(500)))).toBe("ok");
    for (const bad of [
      "x".repeat(19),
      "x".repeat(501),
      "sk-with space-0123456789",
      "sk-tab\tinside-0123456789",
      "sk-ünïcode-0123456789abc",
      42,
      undefined,
    ]) {
      expect(codeOf(() => parseProviderKey(bad))).toBe("key_invalid");
    }
  });
});

describe("provider ids", () => {
  it("are the three supported providers; anything else is not_found", () => {
    expect(parseProvider("google")).toBe("google");
    for (const bad of ["ollama", "OpenAI", "", null]) expect(codeOf(() => parseProvider(bad))).toBe("not_found");
  });
});
```

Run: `task test -- --project unit src/server/providers/validation.test.ts` → FAIL (module missing).

- [ ] **Step 5: Validation module** — `src/server/providers/validation.ts`:

```ts
import "server-only";
import { z } from "zod";
import { PROVIDER_IDS, type ProviderId } from "@/lib/providers";
import { WorkspaceError } from "@/server/workspaces/errors";

const providerSchema = z.enum(PROVIDER_IDS);
// Printable ASCII only (fetch rejects other header bytes); 20+ characters, so the 4-character
// hint reveals little.
const providerKeySchema = z.string().trim().regex(/^[\x21-\x7E]{20,500}$/);

/** A provider id from request input; anything else is not_found (it's a lookup key). */
export function parseProvider(value: unknown): ProviderId {
  const parsed = providerSchema.safeParse(value);
  if (!parsed.success) throw new WorkspaceError("not_found");
  return parsed.data;
}

export function parseProviderKey(value: unknown): string {
  const parsed = providerKeySchema.safeParse(value);
  if (!parsed.success) throw new WorkspaceError("key_invalid");
  return parsed.data;
}
```

Run the unit test again → PASS.

- [ ] **Step 6: Fake provider for tests** — `tests/support/fake-provider.ts`:

```ts
// A stand-in for the model-listing endpoints of OpenAI, Anthropic and Google, used in-process by
// integration tests and as a process by Playwright (tests/support/fake-provider-server.ts).
// Keys starting with "good-" are accepted; any other key is rejected the way the real provider
// does it, echoing the key in the body like OpenAI does, so tests can prove it never leaks.
// Plain Node (type stripping): erasable TypeScript only, relative `.ts` imports.
import { createServer, type IncomingHttpHeaders, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import type { ProviderId } from "../../src/lib/providers.ts";

export type RecordedRequest = { provider: ProviderId; url: string; headers: IncomingHttpHeaders };

export type FakeProvider = {
  baseUrl(provider: ProviderId): string;
  /** Every listing request, in arrival order. */
  requests: RecordedRequest[];
  /** The model ids each provider lists (replace to change the list). */
  models: Record<ProviderId, string[]>;
  /** A status each provider answers with instead of its list; null for normal behaviour. */
  failure: Record<ProviderId, number | null>;
  /** Runs before each listing response, e.g. to change the database during a verification. */
  beforeRespond: (() => Promise<void>) | null;
  /** Points AGENTY_*_BASE_URL at this server. Call before anything reads the configuration. */
  useAsProviderEnv(): void;
  close(): Promise<void>;
};

export const DEFAULT_FAKE_MODELS: Record<ProviderId, string[]> = {
  openai: ["gpt-5", "gpt-5-mini", "text-embedding-3-small"],
  anthropic: ["claude-sonnet-4-5", "claude-haiku-4-5"],
  google: ["gemini-2.5-pro", "gemini-2.5-flash", "text-embedding-004"],
};

const BASE_PATHS: Record<ProviderId, string> = {
  openai: "/openai/v1",
  anthropic: "/anthropic/v1",
  google: "/google/v1beta",
};
const LIST_PATHS = new Map<string, ProviderId>([
  ["/openai/v1/models", "openai"],
  ["/anthropic/v1/models", "anthropic"],
  ["/google/v1beta/models", "google"],
]);

function keyOf(provider: ProviderId, headers: IncomingHttpHeaders): string {
  const value =
    provider === "openai"
      ? headers.authorization?.replace(/^Bearer /, "")
      : provider === "anthropic"
        ? headers["x-api-key"]
        : headers["x-goog-api-key"];
  return typeof value === "string" ? value : "";
}

function listing(provider: ProviderId, ids: string[]): unknown {
  switch (provider) {
    case "openai":
      return { object: "list", data: ids.map((id) => ({ id, object: "model", owned_by: "openai" })) };
    case "anthropic":
      return {
        data: ids.map((id) => ({ id, type: "model", display_name: id })),
        has_more: false,
        first_id: ids[0] ?? null,
        last_id: ids.at(-1) ?? null,
      };
    case "google":
      return {
        models: ids.map((id) => ({
          name: `models/${id}`,
          displayName: id,
          supportedGenerationMethods: id.includes("embedding")
            ? ["embedContent"]
            : ["generateContent", "countTokens"],
        })),
      };
  }
}

function rejection(provider: ProviderId, key: string): { status: number; body: unknown } {
  switch (provider) {
    case "openai":
      return {
        status: 401,
        body: { error: { message: `Incorrect API key provided: ${key}`, code: "invalid_api_key" } },
      };
    case "anthropic":
      return {
        status: 401,
        body: { type: "error", error: { type: "authentication_error", message: `invalid x-api-key ${key}` } },
      };
    case "google":
      return {
        status: 400,
        body: {
          error: {
            code: 400,
            message: `API key not valid: ${key}`,
            status: "INVALID_ARGUMENT",
            details: [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "API_KEY_INVALID" }],
          },
        },
      };
  }
}

function send(response: ServerResponse, status: number, body: unknown) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify(body));
}

export async function startFakeProvider(port = 0): Promise<FakeProvider> {
  let origin = "";
  const fake: FakeProvider = {
    baseUrl: (provider) => `${origin}${BASE_PATHS[provider]}`,
    requests: [],
    models: structuredClone(DEFAULT_FAKE_MODELS),
    failure: { openai: null, anthropic: null, google: null },
    beforeRespond: null,
    useAsProviderEnv() {
      process.env.AGENTY_OPENAI_BASE_URL = fake.baseUrl("openai");
      process.env.AGENTY_ANTHROPIC_BASE_URL = fake.baseUrl("anthropic");
      process.env.AGENTY_GOOGLE_BASE_URL = fake.baseUrl("google");
    },
    close: () =>
      new Promise<void>((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };

  const server = createServer((request, response) => {
    const url = new URL(request.url ?? "/", "http://fake.test");
    if (url.pathname === "/health") return send(response, 200, { ok: true });
    const provider = LIST_PATHS.get(url.pathname);
    if (!provider || request.method !== "GET") return send(response, 404, { error: "not found" });
    fake.requests.push({ provider, url: request.url ?? "", headers: request.headers });
    const respond = () => {
      const key = keyOf(provider, request.headers);
      const failure = fake.failure[provider];
      if (failure !== null) return send(response, failure, { error: { message: `failed for ${key}` } });
      if (provider === "anthropic" && request.headers["anthropic-version"] !== "2023-06-01") {
        return send(response, 400, { error: { message: "anthropic-version missing" } });
      }
      if (!key.startsWith("good-")) {
        const { status, body } = rejection(provider, key);
        return send(response, status, body);
      }
      send(response, 200, listing(provider, fake.models[provider]));
    };
    const before = fake.beforeRespond;
    if (before) before().then(respond, () => send(response, 500, {}));
    else respond();
  });

  await new Promise<void>((resolve) => server.listen(port, "127.0.0.1", resolve));
  origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  return fake;
}
```

- [ ] **Step 7: Workspace test helpers** — `tests/support/workspaces.ts`:

```ts
// Shared fixtures for service integration tests.
import { expect } from "vitest";
import { getDb } from "@/server/db/client";
import { workspaceMember } from "@/server/db/schema";
import { WorkspaceError, type WorkspaceErrorCode } from "@/server/workspaces/errors";
import { createWorkspace } from "@/server/workspaces/workspaces";
import type { testUsers } from "./users";

/** A shared workspace with an admin and a member, plus a user who belongs to neither. */
export async function sharedWorkspace(users: ReturnType<typeof testUsers>, name = "Team") {
  const admin = await users.create("Admin");
  const member = await users.create("Member");
  const outsider = await users.create("Outsider");
  const id = await createWorkspace(admin.id, name);
  await getDb()
    .insert(workspaceMember)
    .values({ workspaceId: id, userId: member.id, role: "member" });
  return { id, admin, member, outsider };
}

export const expectWorkspaceError = (promise: Promise<unknown>, code: WorkspaceErrorCode) =>
  expect(promise).rejects.toSatisfy((e) => e instanceof WorkspaceError && e.code === code);
```

- [ ] **Step 8: Write the failing integration test** — `src/server/providers/keys.int.test.ts`:

```ts
import { randomBytes, randomUUID } from "node:crypto";
import { and, eq } from "drizzle-orm";
import { afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { PROVIDER_IDS, type ProviderId } from "@/lib/providers";
import { encryptWith, providerKeyAad } from "@/server/crypto/cipher";
import { parseKeyring } from "@/server/crypto/keyring";
import { getDb } from "@/server/db/client";
import { providerKey, workspaceMember } from "@/server/db/schema";
import { WorkspaceError } from "@/server/workspaces/errors";
import { startFakeProvider } from "../../../tests/support/fake-provider";
import { testUsers } from "../../../tests/support/users";
import { expectWorkspaceError, sharedWorkspace } from "../../../tests/support/workspaces";
import { getProviderKey, listProviderStatus, removeProviderKey, setProviderKey } from "./keys";
import { providerBaseUrl } from "./registry";

// Before anything reads the configuration: getEnv() caches it once per test file.
const fake = await startFakeProvider();
fake.useAsProviderEnv();

const users = testUsers();
const db = getDb();
afterEach(() => {
  fake.failure = { openai: null, anthropic: null, google: null };
  fake.beforeRespond = null;
  vi.restoreAllMocks();
});
afterAll(async () => {
  await users.cleanup();
  await fake.close();
});

const goodKey = (label = "key") => `good-${label}-${randomUUID()}`;
const storedRows = (workspaceId: string) =>
  db.select().from(providerKey).where(eq(providerKey.workspaceId, workspaceId));
const statusOf = async (actorId: string, workspaceId: string, provider: ProviderId) =>
  (await listProviderStatus(actorId, workspaceId)).find((s) => s.provider === provider);

it("sends provider requests to the operator's base URL", () => {
  expect(providerBaseUrl("openai")).toBe(fake.baseUrl("openai"));
});

describe("P2: only admins change keys", () => {
  it("members get forbidden, non-members not_found, and neither reaches the provider", async () => {
    const { id, member, outsider } = await sharedWorkspace(users);
    const before = fake.requests.length;

    await expectWorkspaceError(setProviderKey(member.id, id, "openai", goodKey()), "forbidden");
    await expectWorkspaceError(removeProviderKey(member.id, id, "openai"), "forbidden");
    await expectWorkspaceError(setProviderKey(outsider.id, id, "openai", goodKey()), "not_found");
    await expectWorkspaceError(removeProviderKey(outsider.id, id, "openai"), "not_found");
    await expectWorkspaceError(listProviderStatus(outsider.id, id), "not_found");
    expect(fake.requests.length).toBe(before);
  });

  it("members see which providers are configured, without the hint", async () => {
    const { id, admin, member } = await sharedWorkspace(users);
    const key = goodKey();
    await setProviderKey(admin.id, id, "openai", key);

    const memberView = await listProviderStatus(member.id, id);
    expect(memberView.map((s) => [s.provider, s.configured])).toEqual([
      ["openai", true],
      ["anthropic", false],
      ["google", false],
    ]);
    expect(memberView[0]).not.toHaveProperty("hint");
    expect(await statusOf(admin.id, id, "openai")).toMatchObject({
      configured: true,
      readable: true,
      hint: key.slice(-4),
    });
  });

  it("treats an unknown provider as not_found", async () => {
    const { id, admin } = await sharedWorkspace(users);
    await expectWorkspaceError(setProviderKey(admin.id, id, "ollama", goodKey()), "not_found");
  });

  it("an admin demoted while the key is verified gets forbidden and nothing is stored", async () => {
    const { id, admin } = await sharedWorkspace(users);
    fake.beforeRespond = async () => {
      await db
        .update(workspaceMember)
        .set({ role: "member" })
        .where(and(eq(workspaceMember.workspaceId, id), eq(workspaceMember.userId, admin.id)));
    };
    await expectWorkspaceError(setProviderKey(admin.id, id, "openai", goodKey()), "forbidden");
    expect(await storedRows(id)).toHaveLength(0);
  });
});

describe("P3: keys are verified before they are stored", () => {
  it.each(PROVIDER_IDS)("%s: a rejected key is not stored", async (provider) => {
    const { id, admin } = await sharedWorkspace(users);
    await expectWorkspaceError(
      setProviderKey(admin.id, id, provider, `bad-${randomUUID()}`),
      "key_rejected",
    );
    expect(await storedRows(id)).toHaveLength(0);
  });

  const expectedHeaders: Record<ProviderId, (key: string) => Record<string, string>> = {
    openai: (key) => ({ authorization: `Bearer ${key}` }),
    anthropic: (key) => ({ "x-api-key": key, "anthropic-version": "2023-06-01" }),
    google: (key) => ({ "x-goog-api-key": key }),
  };

  it.each(PROVIDER_IDS)("%s: the key goes in the header the provider expects, never in the URL", async (provider) => {
    const { id, admin } = await sharedWorkspace(users);
    const key = goodKey(provider);
    const before = fake.requests.length;
    await setProviderKey(admin.id, id, provider, key);

    const sent = fake.requests.slice(before);
    expect(sent).toHaveLength(1);
    expect(sent[0]?.headers).toMatchObject(expectedHeaders[provider](key));
    expect(sent[0]?.url).not.toContain(key);
    expect(await storedRows(id)).toHaveLength(1);
  });

  it.each([
    [503, "provider_unavailable"],
    [429, "provider_unavailable"],
    [403, "key_unverified"],
  ] as const)("a provider answering %i gives %s and stores nothing", async (status, code) => {
    const { id, admin } = await sharedWorkspace(users);
    fake.failure.openai = status;
    await expectWorkspaceError(setProviderKey(admin.id, id, "openai", goodKey()), code);
    expect(await storedRows(id)).toHaveLength(0);
  });

  it("keys are trimmed; whitespace or non-ASCII inside, or a wrong length, is refused before any request", async () => {
    const { id, admin } = await sharedWorkspace(users);
    const key = goodKey("trim");
    await setProviderKey(admin.id, id, "openai", `  ${key}\n`);
    expect(await getProviderKey(id, "openai")).toMatchObject({ status: "ok", key });

    const before = fake.requests.length;
    for (const bad of [
      "good-short",
      `good-${"x".repeat(500)}`,
      "good-with space-0123456789",
      "good-tab\tinside-0123456789",
      "good-ünïcode-0123456789",
    ]) {
      await expectWorkspaceError(setProviderKey(admin.id, id, "openai", bad), "key_invalid");
    }
    expect(fake.requests.length).toBe(before);
  });

  it("replacing a key updates its hint; removing deletes it (also when there is none)", async () => {
    const { id, admin } = await sharedWorkspace(users);
    await setProviderKey(admin.id, id, "google", goodKey());
    const second = goodKey();
    await setProviderKey(admin.id, id, "google", second);
    const rows = await storedRows(id);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ keyHint: second.slice(-4), updatedByUserId: admin.id });

    await removeProviderKey(admin.id, id, "google");
    await removeProviderKey(admin.id, id, "google");
    expect(await storedRows(id)).toHaveLength(0);
    expect(await getProviderKey(id, "google")).toEqual({ status: "not_configured" });
  });
});

describe("P4: keys are stored encrypted and only getProviderKey returns them", () => {
  it("the ciphertext doesn't contain the key, and reads return only the hint", async () => {
    const { id, admin, member } = await sharedWorkspace(users);
    const key = goodKey("secret");
    await setProviderKey(admin.id, id, "anthropic", key);

    const [row] = await storedRows(id);
    expect(row?.encryptedKey).toMatch(/^v1\./);
    expect(row?.encryptedKey).not.toContain(key);
    expect(row?.keyHint).toBe(key.slice(-4));
    expect(JSON.stringify(await listProviderStatus(admin.id, id))).not.toContain(key);
    expect(JSON.stringify(await listProviderStatus(member.id, id))).not.toContain(key);
    expect(await getProviderKey(id, "anthropic")).toEqual({
      status: "ok",
      key,
      updatedAt: row?.updatedAt,
    });
  });

  it("a ciphertext copied into another workspace's row doesn't decrypt", async () => {
    const a = await sharedWorkspace(users);
    const b = await sharedWorkspace(users);
    await setProviderKey(a.admin.id, a.id, "openai", goodKey());
    const [row] = await storedRows(a.id);
    if (!row) throw new Error("key not stored");
    await db
      .insert(providerKey)
      .values({ workspaceId: b.id, provider: "openai", encryptedKey: row.encryptedKey, keyHint: row.keyHint });

    expect(await getProviderKey(b.id, "openai")).toEqual({ status: "unreadable" });
    expect(await statusOf(b.admin.id, b.id, "openai")).toMatchObject({ configured: true, readable: false });
  });

  it("a key encrypted with a key no longer in ENCRYPTION_KEYS is unreadable until it is set again", async () => {
    const { id, admin } = await sharedWorkspace(users);
    const removed = parseKeyring(`gone:${randomBytes(32).toString("base64")}`);
    if (!removed.ok) throw new Error(removed.problem);
    await db.insert(providerKey).values({
      workspaceId: id,
      provider: "google",
      encryptedKey: encryptWith(removed.keyring, goodKey(), providerKeyAad(id, "google")),
      keyHint: "gone",
    });
    expect(await statusOf(admin.id, id, "google")).toMatchObject({ configured: true, readable: false });

    await setProviderKey(admin.id, id, "google", goodKey());
    expect(await statusOf(admin.id, id, "google")).toMatchObject({ configured: true, readable: true });
  });
});

describe("leakage", () => {
  /** Silences console output and returns a function that reads everything written so far. */
  function consoleOutput(): () => string {
    const spies = [
      vi.spyOn(console, "log").mockImplementation(() => {}),
      vi.spyOn(console, "info").mockImplementation(() => {}),
      vi.spyOn(console, "warn").mockImplementation(() => {}),
      vi.spyOn(console, "error").mockImplementation(() => {}),
      vi.spyOn(console, "debug").mockImplementation(() => {}),
    ];
    return () =>
      spies
        .flatMap((spy) => spy.mock.calls)
        .map((args) => args.map(String).join(" "))
        .join("\n");
  }

  it.each(PROVIDER_IDS)("%s: a key the provider echoes is neither logged nor in the error", async (provider) => {
    const { id, admin } = await sharedWorkspace(users);
    const logged = consoleOutput();
    const key = `bad-echo-${randomUUID()}`;

    const error = await setProviderKey(admin.id, id, provider, key).then(
      () => null,
      (e: unknown) => e,
    );
    expect(error).toBeInstanceOf(WorkspaceError);
    expect((error as WorkspaceError).code).toBe("key_rejected");
    expect(`${(error as Error).message} ${(error as Error).stack}`).not.toContain(key);
    expect(logged()).toContain(`provider=${provider}`);
    expect(logged()).not.toContain(key);
  });

  it("a failing provider's body (which echoes the key) is not logged either", async () => {
    const { id, admin } = await sharedWorkspace(users);
    const logged = consoleOutput();
    const key = goodKey("echo");
    fake.failure.openai = 500;
    await expectWorkspaceError(setProviderKey(admin.id, id, "openai", key), "provider_unavailable");
    expect(logged()).toContain("status=500");
    expect(logged()).not.toContain(key);
  });
});
```

Run: `task test -- --project integration src/server/providers/keys.int.test.ts`
Expected: FAIL (cannot resolve `./keys`).

- [ ] **Step 9: The key service** — `src/server/providers/keys.ts`:

```ts
import "server-only";
import { and, eq } from "drizzle-orm";
import { PROVIDER_IDS, type ProviderId } from "@/lib/providers";
import { CryptoError, decrypt, encrypt, providerKeyAad } from "@/server/crypto/crypto";
import { getDb } from "@/server/db/client";
import { providerKey } from "@/server/db/schema";
import { WorkspaceError } from "@/server/workspaces/errors";
import { lockForAdmin, requireAdminRole, requireMembership } from "@/server/workspaces/internal";
import { listModels } from "./listing";
import { providerBaseUrl } from "./registry";
import { parseProvider, parseProviderKey } from "./validation";

export type ProviderStatus = {
  provider: ProviderId;
  configured: boolean;
  /** False when the stored ciphertext doesn't decrypt (e.g. its encryption key was removed). */
  readable: boolean;
  /** Last 4 characters of the key; admins only. */
  hint?: string;
  updatedAt?: Date;
};

export type ProviderKeyLookup =
  | { status: "ok"; key: string; updatedAt: Date }
  | { status: "not_configured" }
  | { status: "unreadable" };

/**
 * P1–P4: verifies the key with the provider (outside any transaction, so no lock is held during
 * the HTTP call), then stores it encrypted. Admins only, checked before the call (members can't
 * use the server to test keys) and again under the workspace lock.
 */
export async function setProviderKey(
  actorId: string,
  workspaceId: string,
  provider: string,
  key: string,
): Promise<void> {
  const id = parseProvider(provider);
  const plain = parseProviderKey(key);
  await requireAdminRole(actorId, workspaceId);
  const verified = await listModels(id, providerBaseUrl(id), plain);
  if (!verified.ok) throw new WorkspaceError(verified.code);

  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    const values = {
      encryptedKey: encrypt(plain, providerKeyAad(ws.id, id)),
      keyHint: plain.slice(-4),
      updatedByUserId: actorId,
      updatedAt: new Date(),
    };
    await tx
      .insert(providerKey)
      .values({ workspaceId: ws.id, provider: id, ...values })
      .onConflictDoUpdate({ target: [providerKey.workspaceId, providerKey.provider], set: values });
  });
}

/** P5: always allowed for admins; agents of that provider stay. */
export async function removeProviderKey(
  actorId: string,
  workspaceId: string,
  provider: string,
): Promise<void> {
  const id = parseProvider(provider);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    await tx
      .delete(providerKey)
      .where(and(eq(providerKey.workspaceId, ws.id), eq(providerKey.provider, id)));
  });
}

function canDecrypt(ciphertext: string, aad: string): boolean {
  try {
    decrypt(ciphertext, aad); // the plaintext is dropped at once
    return true;
  } catch (error) {
    if (error instanceof CryptoError) return false;
    throw error;
  }
}

/** P2: members see which providers are configured; the hint is for admins only. */
export async function listProviderStatus(
  actorId: string,
  workspaceId: string,
): Promise<ProviderStatus[]> {
  const { workspace: ws, role } = await requireMembership(actorId, workspaceId);
  const rows = await getDb()
    .select({
      provider: providerKey.provider,
      encryptedKey: providerKey.encryptedKey,
      keyHint: providerKey.keyHint,
      updatedAt: providerKey.updatedAt,
    })
    .from(providerKey)
    .where(eq(providerKey.workspaceId, ws.id));
  return PROVIDER_IDS.map((provider): ProviderStatus => {
    const row = rows.find((r) => r.provider === provider);
    if (!row) return { provider, configured: false, readable: false };
    return {
      provider,
      configured: true,
      readable: canDecrypt(row.encryptedKey, providerKeyAad(ws.id, provider)),
      updatedAt: row.updatedAt,
      ...(role === "admin" ? { hint: row.keyHint } : {}),
    };
  });
}

/**
 * The plain key: the only function that returns one. No actor; callers have checked access
 * (M4's model wrapper calls it inside the workflow step). Never pass the result to the client.
 */
export async function getProviderKey(
  workspaceId: string,
  provider: ProviderId,
): Promise<ProviderKeyLookup> {
  const [row] = await getDb()
    .select({
      workspaceId: providerKey.workspaceId,
      encryptedKey: providerKey.encryptedKey,
      updatedAt: providerKey.updatedAt,
    })
    .from(providerKey)
    .where(and(eq(providerKey.workspaceId, workspaceId), eq(providerKey.provider, provider)));
  if (!row) return { status: "not_configured" };
  try {
    const key = decrypt(row.encryptedKey, providerKeyAad(row.workspaceId, provider));
    return { status: "ok", key, updatedAt: row.updatedAt };
  } catch (error) {
    if (!(error instanceof CryptoError)) throw error;
    console.warn(`Provider key unreadable: provider=${provider} error=${error.name}`);
    return { status: "unreadable" };
  }
}
```

- [ ] **Step 10: Run the tests**

Run: `task test -- --project integration src/server/providers/keys.int.test.ts`
Expected: PASS. If the first test fails with the default OpenAI URL, something read `getEnv()` before `fake.useAsProviderEnv()`: keep both lines at the very top of the file, before `getDb()`.

- [ ] **Step 11: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/db/schema.ts src/server/db/migrations src/server/workspaces/errors.ts src/server/workspaces/internal.ts src/server/providers/validation.ts src/server/providers/validation.test.ts src/server/providers/keys.ts src/server/providers/keys.int.test.ts tests/support/fake-provider.ts tests/support/workspaces.ts
git commit -m "feat(providers): provider_key and agent tables, encrypted and verified provider keys

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Model cache and `listModelsForWorkspace`

**Files:**
- Create: `src/server/providers/model-cache.ts`, `src/server/providers/models.ts`
- Modify: `src/server/providers/keys.ts` (seed on set, clear on remove)
- Test: `src/server/providers/model-cache.test.ts`, `src/server/providers/models.int.test.ts`

**Interfaces:**
- Consumes: `getProviderKey` (Task 3), `listModels`, `providerBaseUrl` (Task 2), `ModelOption`, `ProviderId` (Task 2).
- Produces:
  - `type ModelListResult = { status: "ok"; models: ModelOption[] } | { status: "not_configured" | "unreadable" | "unavailable" }`
  - model-cache.ts: `SUCCESS_TTL_MS = 600_000`, `FAILURE_TTL_MS = 60_000`, `SWEEP_THRESHOLD = 1_000`, `cachedModels(workspaceId, provider, keyUpdatedAt: Date, load: () => Promise<FetchedModels>): Promise<FetchedModels>` (`FetchedModels` = the `ok` and `unavailable` cases), `seedModelCache(workspaceId, provider, keyUpdatedAt, models)`, `clearModelCache(workspaceId, provider)`, tests only: `setModelCacheClock(now?: () => number)`, `resetModelCache()`, `modelCacheSize()`
  - models.ts: `listModelsForWorkspace(workspaceId: string, provider: ProviderId): Promise<ModelListResult>` (no actor; callers have checked access)

- [ ] **Step 1: Failing unit test for the cache** — `src/server/providers/model-cache.test.ts`:

```ts
import { afterEach, describe, expect, it } from "vitest";
import {
  cachedModels,
  clearModelCache,
  FAILURE_TTL_MS,
  type FetchedModels,
  modelCacheSize,
  resetModelCache,
  SUCCESS_TTL_MS,
  SWEEP_THRESHOLD,
  seedModelCache,
  setModelCacheClock,
} from "./model-cache";

const START = 1_000_000;
let now = START;
setModelCacheClock(() => now);
afterEach(() => {
  resetModelCache();
  now = START;
});

const stamp = new Date(500);
const models = [{ id: "gpt-5", label: "gpt-5" }];
const ok: FetchedModels = { status: "ok", models };
const unavailable: FetchedModels = { status: "unavailable" };

/** A load function that counts its calls. */
function loader(result: FetchedModels | Error) {
  let calls = 0;
  return {
    load: async () => {
      calls++;
      if (result instanceof Error) throw result;
      return result;
    },
    calls: () => calls,
  };
}

describe("model cache", () => {
  it("shares one load between concurrent callers", async () => {
    const l = loader(ok);
    const results = await Promise.all(
      Array.from({ length: 5 }, () => cachedModels("ws", "openai", stamp, l.load)),
    );
    expect(results).toEqual(Array(5).fill(ok));
    expect(l.calls()).toBe(1);
  });

  it("keeps a success for 10 minutes", async () => {
    const l = loader(ok);
    await cachedModels("ws", "openai", stamp, l.load);
    now += SUCCESS_TTL_MS - 1;
    await cachedModels("ws", "openai", stamp, l.load);
    expect(l.calls()).toBe(1);
    now += 2;
    await cachedModels("ws", "openai", stamp, l.load);
    expect(l.calls()).toBe(2);
  });

  it("keeps a failure for 1 minute, and a load that throws counts as a failure", async () => {
    const l = loader(new Error("boom"));
    expect(await cachedModels("ws", "openai", stamp, l.load)).toEqual(unavailable);
    now += FAILURE_TTL_MS - 1;
    await cachedModels("ws", "openai", stamp, l.load);
    expect(l.calls()).toBe(1);
    now += 2;
    await cachedModels("ws", "openai", stamp, l.load);
    expect(l.calls()).toBe(2);
  });

  it("never uses an entry stored for another version of the key", async () => {
    const l = loader(ok);
    await cachedModels("ws", "openai", stamp, l.load);
    await cachedModels("ws", "openai", new Date(501), l.load);
    expect(l.calls()).toBe(2);
  });

  it("serves seeded entries and forgets cleared ones", async () => {
    const l = loader(unavailable);
    seedModelCache("ws", "google", stamp, models);
    expect(await cachedModels("ws", "google", stamp, l.load)).toEqual(ok);
    clearModelCache("ws", "google");
    expect(await cachedModels("ws", "google", stamp, l.load)).toEqual(unavailable);
    expect(l.calls()).toBe(1);
  });

  it("drops expired entries once it holds more than the threshold", async () => {
    for (let i = 0; i < SWEEP_THRESHOLD; i++) seedModelCache(`ws-${i}`, "openai", stamp, []);
    expect(modelCacheSize()).toBe(SWEEP_THRESHOLD);
    now += SUCCESS_TTL_MS + 1;
    await cachedModels("fresh", "openai", stamp, loader(ok).load);
    expect(modelCacheSize()).toBe(1);
  });
});
```

Run: `task test -- --project unit src/server/providers/model-cache.test.ts` → FAIL (module missing).

- [ ] **Step 2: The cache** — `src/server/providers/model-cache.ts`:

```ts
import "server-only";
import type { ModelOption, ProviderId } from "@/lib/providers";

export type ModelListResult =
  | { status: "ok"; models: ModelOption[] }
  | { status: "not_configured" | "unreadable" | "unavailable" };
/** What a provider call can produce (the other statuses come from the database). */
export type FetchedModels = Extract<ModelListResult, { status: "ok" | "unavailable" }>;

export const SUCCESS_TTL_MS = 10 * 60_000;
export const FAILURE_TTL_MS = 60_000;
export const SWEEP_THRESHOLD = 1_000;

type Entry = { stamp: number; expiresAt: number; result: Promise<FetchedModels> };

/**
 * Per process; several instances each have their own (only slower). Correctness rests on `stamp`
 * (the key's updated_at): an entry for another version of the key is never used, even if this
 * module exists twice in different bundles. Clearing on set/remove is only an optimisation.
 */
const entries = new Map<string, Entry>();
let clock: () => number = Date.now;
const UNAVAILABLE: FetchedModels = { status: "unavailable" };

const cacheKey = (workspaceId: string, provider: ProviderId) => `${workspaceId}|${provider}`;

function sweep(now: number) {
  for (const [key, entry] of entries) if (entry.expiresAt <= now) entries.delete(key);
}

/** The cached list for this version of the key, or `load()` shared by concurrent callers. */
export function cachedModels(
  workspaceId: string,
  provider: ProviderId,
  keyUpdatedAt: Date,
  load: () => Promise<FetchedModels>,
): Promise<FetchedModels> {
  const key = cacheKey(workspaceId, provider);
  const stamp = keyUpdatedAt.getTime();
  const now = clock();
  const existing = entries.get(key);
  if (existing && existing.stamp === stamp && existing.expiresAt > now) return existing.result;
  if (entries.size >= SWEEP_THRESHOLD) sweep(now);

  // In flight: never expires until it settles, so concurrent callers share it.
  const entry: Entry = {
    stamp,
    expiresAt: Number.POSITIVE_INFINITY,
    result: Promise.resolve(UNAVAILABLE),
  };
  entry.result = load()
    .catch(() => UNAVAILABLE)
    .then((result) => {
      entry.expiresAt = clock() + (result.status === "ok" ? SUCCESS_TTL_MS : FAILURE_TTL_MS);
      return result;
    });
  entries.set(key, entry);
  return entry.result;
}

/** A successful verification already listed the models: store them for the new key version. */
export function seedModelCache(
  workspaceId: string,
  provider: ProviderId,
  keyUpdatedAt: Date,
  models: ModelOption[],
) {
  entries.set(cacheKey(workspaceId, provider), {
    stamp: keyUpdatedAt.getTime(),
    expiresAt: clock() + SUCCESS_TTL_MS,
    result: Promise.resolve({ status: "ok", models }),
  });
}

export function clearModelCache(workspaceId: string, provider: ProviderId) {
  entries.delete(cacheKey(workspaceId, provider));
}

/** Tests only: replaces the clock (no argument restores Date.now). */
export function setModelCacheClock(now?: () => number) {
  clock = now ?? Date.now;
}

/** Tests only. */
export function resetModelCache() {
  entries.clear();
}

/** Tests only. */
export function modelCacheSize(): number {
  return entries.size;
}
```

Run: `task test -- --project unit src/server/providers/model-cache.test.ts` → PASS (6 tests).

- [ ] **Step 3: Failing integration test** — `src/server/providers/models.int.test.ts`:

```ts
import { randomUUID } from "node:crypto";
import { and, eq } from "drizzle-orm";
import { afterAll, afterEach, describe, expect, it } from "vitest";
import { getDb } from "@/server/db/client";
import { providerKey } from "@/server/db/schema";
import { startFakeProvider } from "../../../tests/support/fake-provider";
import { testUsers } from "../../../tests/support/users";
import { sharedWorkspace } from "../../../tests/support/workspaces";
import { removeProviderKey, setProviderKey } from "./keys";
import {
  FAILURE_TTL_MS,
  resetModelCache,
  SUCCESS_TTL_MS,
  setModelCacheClock,
} from "./model-cache";
import { listModelsForWorkspace } from "./models";

// Before anything reads the configuration: getEnv() caches it once per test file.
const fake = await startFakeProvider();
fake.useAsProviderEnv();

const users = testUsers();
const db = getDb();
let now = Date.now();
setModelCacheClock(() => now);
afterEach(() => {
  fake.failure.openai = null;
  now = Date.now();
});
afterAll(async () => {
  setModelCacheClock();
  await users.cleanup();
  await fake.close();
});

const goodKey = () => `good-models-${randomUUID()}`;

async function workspaceWithKey() {
  const ws = await sharedWorkspace(users);
  await setProviderKey(ws.admin.id, ws.id, "openai", goodKey());
  return ws;
}

/** Requests the fake provider got while `run` ran. */
async function requestsDuring(run: () => Promise<unknown>): Promise<number> {
  const before = fake.requests.length;
  await run();
  return fake.requests.length - before;
}

describe("listModelsForWorkspace", () => {
  it("lists the provider's chat models; a successful verification seeds the cache", async () => {
    const { id } = await workspaceWithKey();
    let result: unknown;
    expect(await requestsDuring(async () => { result = await listModelsForWorkspace(id, "openai"); })).toBe(0);
    expect(result).toEqual({
      status: "ok",
      models: [
        { id: "gpt-5", label: "gpt-5" },
        { id: "gpt-5-mini", label: "gpt-5-mini" },
      ],
    });
  });

  it("reports not_configured without a key and unreadable for a key that doesn't decrypt", async () => {
    const { id, admin } = await workspaceWithKey();
    await db
      .update(providerKey)
      .set({ encryptedKey: "v1.gone.AAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAA.AAAA" })
      .where(and(eq(providerKey.workspaceId, id), eq(providerKey.provider, "openai")));
    expect(await listModelsForWorkspace(id, "openai")).toEqual({ status: "unreadable" });
    await removeProviderKey(admin.id, id, "openai");
    expect(await listModelsForWorkspace(id, "openai")).toEqual({ status: "not_configured" });
    expect(await listModelsForWorkspace(id, "anthropic")).toEqual({ status: "not_configured" });
  });

  it("an unreachable provider is unavailable, and is asked again only after the failure TTL", async () => {
    const { id } = await workspaceWithKey();
    resetModelCache();
    fake.failure.openai = 503;
    expect(await listModelsForWorkspace(id, "openai")).toEqual({ status: "unavailable" });

    fake.failure.openai = null;
    now += FAILURE_TTL_MS - 1_000;
    expect(await requestsDuring(() => listModelsForWorkspace(id, "openai"))).toBe(0);
    expect(await listModelsForWorkspace(id, "openai")).toEqual({ status: "unavailable" });

    now += 2_000;
    let result: unknown;
    expect(await requestsDuring(async () => { result = await listModelsForWorkspace(id, "openai"); })).toBe(1);
    expect(result).toMatchObject({ status: "ok" });
  });

  it("keeps a list for 10 minutes", async () => {
    const { id } = await workspaceWithKey();
    now += SUCCESS_TTL_MS - 1_000;
    expect(await requestsDuring(() => listModelsForWorkspace(id, "openai"))).toBe(0);
    now += 2_000;
    expect(await requestsDuring(() => listModelsForWorkspace(id, "openai"))).toBe(1);
  });

  it("asks again when the key changed in another process (updated_at differs)", async () => {
    const { id } = await workspaceWithKey();
    await db
      .update(providerKey)
      .set({ updatedAt: new Date(Date.now() + 60_000) })
      .where(and(eq(providerKey.workspaceId, id), eq(providerKey.provider, "openai")));
    expect(await requestsDuring(() => listModelsForWorkspace(id, "openai"))).toBe(1);
  });

  it("several pages asking at once share one provider request", async () => {
    const { id } = await workspaceWithKey();
    resetModelCache();
    expect(
      await requestsDuring(() =>
        Promise.all(Array.from({ length: 5 }, () => listModelsForWorkspace(id, "openai"))),
      ),
    ).toBe(1);
  });
});
```

Run: `task test -- --project integration src/server/providers/models.int.test.ts` → FAIL (cannot resolve `./models`).

- [ ] **Step 4: `listModelsForWorkspace`** — `src/server/providers/models.ts`:

```ts
import "server-only";
import type { ProviderId } from "@/lib/providers";
import { getProviderKey } from "./keys";
import { listModels } from "./listing";
import { cachedModels, type ModelListResult } from "./model-cache";
import { providerBaseUrl } from "./registry";

export type { ModelListResult } from "./model-cache";

/**
 * The models the workspace's key for `provider` may use, from the per-process cache. No actor:
 * callers have checked access. The result never contains the key.
 */
export async function listModelsForWorkspace(
  workspaceId: string,
  provider: ProviderId,
): Promise<ModelListResult> {
  const key = await getProviderKey(workspaceId, provider);
  if (key.status !== "ok") return { status: key.status };
  return cachedModels(workspaceId, provider, key.updatedAt, async () => {
    const result = await listModels(provider, providerBaseUrl(provider), key.key);
    return result.ok ? { status: "ok", models: result.models } : { status: "unavailable" };
  });
}
```

- [ ] **Step 5: Seed and clear from the key service** — in `src/server/providers/keys.ts`, import the cache:

```ts
import { clearModelCache, seedModelCache } from "./model-cache";
```

In `setProviderKey`, replace the transaction block with one that returns what was stored, followed by the seeding:

```ts
  const saved = await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    const values = {
      encryptedKey: encrypt(plain, providerKeyAad(ws.id, id)),
      keyHint: plain.slice(-4),
      updatedByUserId: actorId,
      updatedAt: new Date(),
    };
    await tx
      .insert(providerKey)
      .values({ workspaceId: ws.id, provider: id, ...values })
      .onConflictDoUpdate({ target: [providerKey.workspaceId, providerKey.provider], set: values });
    return { workspaceId: ws.id, updatedAt: values.updatedAt };
  });
  // The verification listed the models already.
  seedModelCache(saved.workspaceId, id, saved.updatedAt, verified.models);
```

In `removeProviderKey`, make the transaction return `ws.id` and clear afterwards:

```ts
  const removedFrom = await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    await tx
      .delete(providerKey)
      .where(and(eq(providerKey.workspaceId, ws.id), eq(providerKey.provider, id)));
    return ws.id;
  });
  clearModelCache(removedFrom, id);
```

- [ ] **Step 6: Run the tests**

Run: `task test -- --project unit src/server/providers` and `task test -- --project integration src/server/providers`
Expected: PASS (the Task 3 tests still pass).

- [ ] **Step 7: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/providers/model-cache.ts src/server/providers/model-cache.test.ts src/server/providers/models.ts src/server/providers/models.int.test.ts src/server/providers/keys.ts
git commit -m "feat(providers): per-process model list cache with key-version check

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 5: Encryption key rotation (`task crypto:rotate`)

**Files:**
- Create: `src/server/crypto/rotate.ts`, `scripts/rotate-keys.ts`
- Modify: `Taskfile.yml`
- Test: `src/server/crypto/rotate.int.test.ts`

**Interfaces:**
- Consumes: `Keyring`, `parseKeyring` (Task 1), `encryptWith`, `decryptWith`, `keyIdOf`, `providerKeyAad`, `CryptoError` (Task 1), table `app.provider_key` (Task 3).
- Produces: `type RotationCounts = { rotated: number; unreadable: number; pending: number }`, `rotateProviderKeys(sql: Sql, keyring: Keyring, options?: { check?: boolean; workspaceIds?: string[] }): Promise<RotationCounts>`; task `crypto:rotate` (`-- --check` only counts).

- [ ] **Step 1: Write the failing integration test** — `src/server/crypto/rotate.int.test.ts`:

```ts
import { randomBytes } from "node:crypto";
import { inArray } from "drizzle-orm";
import postgres from "postgres";
import { afterAll, describe, expect, it } from "vitest";
import { getDb } from "@/server/db/client";
import { providerKey } from "@/server/db/schema";
import { testUsers } from "../../../tests/support/users";
import { sharedWorkspace } from "../../../tests/support/workspaces";
import { decryptWith, encryptWith, keyIdOf, providerKeyAad } from "./cipher";
import { type Keyring, parseKeyring } from "./keyring";
import { rotateProviderKeys } from "./rotate";

const users = testUsers();
const db = getDb();
// The script's own connection, as the app role (DATABASE_URL), like `task crypto:rotate`.
const sql = postgres(process.env.DATABASE_URL ?? "", { max: 1, onnotice: () => {} });
afterAll(async () => {
  await users.cleanup();
  await sql.end();
});

function keyring(value: string): Keyring {
  const result = parseKeyring(value);
  if (!result.ok) throw new Error(result.problem);
  return result.keyring;
}
const oldSpec = `old:${randomBytes(32).toString("base64")}`;
const newSpec = `new:${randomBytes(32).toString("base64")}`;

/** Two workspaces with an OpenAI key each, encrypted with `encryptingKeyring`. */
async function keysEncryptedWith(encryptingKeyring: Keyring) {
  const plain = new Map<string, string>();
  for (const label of ["a", "b"]) {
    const { id } = await sharedWorkspace(users);
    const key = `good-rotate-${label}-${randomBytes(8).toString("hex")}`;
    plain.set(id, key);
    await db.insert(providerKey).values({
      workspaceId: id,
      provider: "openai",
      encryptedKey: encryptWith(encryptingKeyring, key, providerKeyAad(id, "openai")),
      keyHint: key.slice(-4),
      updatedAt: new Date("2026-01-01T00:00:00Z"),
    });
  }
  const workspaceIds = [...plain.keys()];
  const rows = () =>
    db.select().from(providerKey).where(inArray(providerKey.workspaceId, workspaceIds));
  return { plain, workspaceIds, rows };
}

describe("rotation", () => {
  it("re-encrypts rows on an older key with the active one; afterwards the old key can go", async () => {
    const { plain, workspaceIds, rows } = await keysEncryptedWith(keyring(oldSpec));
    const both = keyring(`${newSpec},${oldSpec}`);

    expect(await rotateProviderKeys(sql, both, { workspaceIds, check: true })).toEqual({
      rotated: 0,
      unreadable: 0,
      pending: 2,
    });
    expect(await rotateProviderKeys(sql, both, { workspaceIds })).toEqual({
      rotated: 2,
      unreadable: 0,
      pending: 0,
    });
    expect(await rotateProviderKeys(sql, both, { workspaceIds, check: true })).toMatchObject({
      pending: 0,
    });

    const onlyNew = keyring(newSpec);
    for (const row of await rows()) {
      expect(keyIdOf(row.encryptedKey)).toBe("new");
      expect(decryptWith(onlyNew, row.encryptedKey, providerKeyAad(row.workspaceId, "openai"))).toBe(
        plain.get(row.workspaceId),
      );
      // Rotation is not a change by a user.
      expect(row.updatedAt).toEqual(new Date("2026-01-01T00:00:00Z"));
    }
  });

  it("leaves rows whose key id is not in the keyring and counts them as unreadable", async () => {
    const { workspaceIds, rows } = await keysEncryptedWith(keyring(oldSpec));
    const ciphertexts = async () => (await rows()).map((row) => row.encryptedKey).sort();
    const before = await ciphertexts();

    expect(await rotateProviderKeys(sql, keyring(newSpec), { workspaceIds })).toEqual({
      rotated: 0,
      unreadable: 2,
      pending: 2,
    });
    expect(await ciphertexts()).toEqual(before);
  });
});
```

Run: `task test -- --project integration src/server/crypto/rotate.int.test.ts` → FAIL (cannot resolve `./rotate`).

- [ ] **Step 2: Rotation** — `src/server/crypto/rotate.ts`:

```ts
// Re-encrypts provider keys with the active key. No "server-only" import: scripts/rotate-keys.ts
// runs this file in plain Node (type stripping), so only erasable syntax and `.ts` imports.
import type { Sql } from "postgres";
import { CryptoError, decryptWith, encryptWith, keyIdOf, providerKeyAad } from "./cipher.ts";
import type { Keyring } from "./keyring.ts";

/** `pending`: rows still on a non-active key afterwards (with `check`: before, nothing changed). */
export type RotationCounts = { rotated: number; unreadable: number; pending: number };

/**
 * Row by row, each in its own transaction that locks only that row (never the workspace row, so
 * it can't deadlock with setProviderKey). updated_at and updated_by stay: rotation is no user
 * change. Rows whose key id isn't in the keyring are left alone and counted as unreadable.
 * `workspaceIds` limits the run to some workspaces (tests only).
 */
export async function rotateProviderKeys(
  sql: Sql,
  keyring: Keyring,
  options: { check?: boolean; workspaceIds?: string[] } = {},
): Promise<RotationCounts> {
  const { workspaceIds } = options;
  if (workspaceIds?.length === 0) return { rotated: 0, unreadable: 0, pending: 0 };
  const scope = workspaceIds ? sql`and workspace_id in ${sql(workspaceIds)}` : sql``;
  const candidates = await sql<{ workspace_id: string; provider: string }[]>`
    select workspace_id, provider from app.provider_key
    where split_part(encrypted_key, '.', 2) <> ${keyring.activeId} ${scope}
    order by workspace_id, provider`;
  if (options.check) return { rotated: 0, unreadable: 0, pending: candidates.length };

  let rotated = 0;
  let unreadable = 0;
  for (const { workspace_id: workspaceId, provider } of candidates) {
    await sql.begin(async (tx) => {
      const [row] = await tx<{ encrypted_key: string }[]>`
        select encrypted_key from app.provider_key
        where workspace_id = ${workspaceId} and provider = ${provider}
        for update`;
      if (!row || keyIdOf(row.encrypted_key) === keyring.activeId) return;
      const aad = providerKeyAad(workspaceId, provider);
      let plaintext: string;
      try {
        plaintext = decryptWith(keyring, row.encrypted_key, aad);
      } catch (error) {
        if (!(error instanceof CryptoError)) throw error;
        unreadable++;
        return;
      }
      await tx`
        update app.provider_key set encrypted_key = ${encryptWith(keyring, plaintext, aad)}
        where workspace_id = ${workspaceId} and provider = ${provider}`;
      rotated++;
    });
  }
  return { rotated, unreadable, pending: unreadable };
}
```

Run: `task test -- --project integration src/server/crypto/rotate.int.test.ts` → PASS (2 tests).

- [ ] **Step 3: The script** — `scripts/rotate-keys.ts`:

```ts
// Re-encrypts stored provider keys with the active (first) ENCRYPTION_KEYS key:
// `task crypto:rotate`; `task crypto:rotate -- --check` only counts rows on older keys.
// Connects as DATABASE_URL (the app role may update provider_key). Prints counts only.
import postgres from "postgres";
import { parseKeyring } from "../src/server/crypto/keyring.ts";
import { rotateProviderKeys } from "../src/server/crypto/rotate.ts";

const check = process.argv.includes("--check");
const parsed = parseKeyring(process.env.ENCRYPTION_KEYS ?? "");
if (!parsed.ok) {
  console.error(`Invalid ENCRYPTION_KEYS: ${parsed.problem}`);
  process.exit(1);
}
const url = process.env.DATABASE_URL;
if (!url) {
  console.error("DATABASE_URL is not set.");
  process.exit(1);
}

const sql = postgres(url, { max: 1, onnotice: () => {} });
try {
  const counts = await rotateProviderKeys(sql, parsed.keyring, { check });
  if (check) {
    console.log(`Provider keys on an older encryption key: ${counts.pending}`);
  } else {
    console.log(`Re-encrypted: ${counts.rotated}. Unreadable (key id not in ENCRYPTION_KEYS): ${counts.unreadable}.`);
  }
  if (counts.unreadable > 0) process.exitCode = 1;
} catch (error) {
  console.error(`Rotation failed: ${error instanceof Error ? error.name : "unknown error"}`);
  process.exitCode = 1;
} finally {
  await sql.end();
}
```

- [ ] **Step 4: The task** — in `Taskfile.yml`, after `db:migrate`:

```yaml
  crypto:rotate:
    desc: Re-encrypt stored provider keys with the active ENCRYPTION_KEYS key (`-- --check` only counts rows on older keys)
    cmds:
      - node scripts/rotate-keys.ts {{.CLI_ARGS}}
```

Run: `task crypto:rotate -- --check`
Expected: `Provider keys on an older encryption key: 0` (the dev database only has keys made with the `dev` key; rows the tests left behind are removed with their workspaces).

- [ ] **Step 5: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/crypto/rotate.ts src/server/crypto/rotate.int.test.ts scripts/rotate-keys.ts Taskfile.yml
git commit -m "feat(crypto): re-encrypt provider keys with task crypto:rotate

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Agent service and availability status

**Files:**
- Modify: `src/server/workspaces/errors.ts`
- Create: `src/server/db/errors.ts`, `src/server/agents/validation.ts`, `src/server/agents/agents.ts`, `src/server/agents/status.ts`
- Test: `src/server/db/errors.test.ts`, `src/server/agents/validation.test.ts`, `src/server/agents/status.test.ts`, `src/server/agents/agents.int.test.ts`

**Interfaces:**
- Consumes: table `agent`, index `agent_workspace_name_unique` (Task 3); `listModelsForWorkspace`, `ModelListResult` (Task 4); `setProviderKey`, `removeProviderKey` (Task 3, in tests); `requireAdminRole`, `requireMembership`, `lockForAdmin`, `parseId` (M2/Task 3); `startFakeProvider`, `sharedWorkspace`, `expectWorkspaceError` (Task 3).
- Produces:
  - `WorkspaceErrorCode` gains `agent_name_taken`, `provider_not_configured`, `model_unavailable`, `invalid_parameters`, `invalid_agent`
  - `isUniqueViolation(error: unknown, constraint: string): boolean` (db/errors.ts)
  - `type AgentValues`, `parseAgentInput(input: unknown): AgentValues` (agents/validation.ts; strings or numbers for the parameters, `""` = null)
  - `type AgentSummary = { id: string; name: string; provider: ProviderId; model: string }`, `type Agent = AgentSummary & { description: string; systemPrompt: string; temperature: number | null; topP: number | null; maxOutputTokens: number | null; updatedAt: Date }`
  - `listAgents(actorId, workspaceId): Promise<AgentSummary[]>` (by name, case-insensitive), `getAgent(actorId, workspaceId, agentId): Promise<Agent>`, `createAgent(actorId, workspaceId, input: unknown): Promise<string>`, `updateAgent(actorId, workspaceId, agentId, input: unknown): Promise<void>`, `deleteAgent(actorId, workspaceId, agentId): Promise<void>`
  - `type AgentStatus = "ready" | "provider_not_configured" | "key_unreadable" | "model_unavailable" | "unknown"`, `AGENT_STATUS_LABELS`, `statusFromModels(result: ModelListResult, model: string): AgentStatus`, `agentStatus(workspaceId, provider, model): Promise<AgentStatus>` (no actor; callers have checked access)

- [ ] **Step 1: Error codes** — in `src/server/workspaces/errors.ts`, append to `WORKSPACE_ERROR_CODES` (after the M3 key codes):

```ts
  // M3: agents
  "agent_name_taken",
  "provider_not_configured",
  "model_unavailable",
  "invalid_parameters",
  "invalid_agent",
```

- [ ] **Step 2: Failing unit tests** — `src/server/db/errors.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { isUniqueViolation } from "./errors";

/** Shaped like postgres-js's PostgresError, wrapped like Drizzle's DrizzleQueryError. */
const postgresError = (code: string, constraint: string) =>
  Object.assign(new Error("duplicate key"), { code, constraint_name: constraint });
const wrapped = (cause: Error) => new Error("Failed query", { cause });

describe("unique violations", () => {
  it("are recognised through Drizzle's wrapper, for the named constraint only", () => {
    const error = wrapped(postgresError("23505", "agent_workspace_name_unique"));
    expect(isUniqueViolation(error, "agent_workspace_name_unique")).toBe(true);
    expect(isUniqueViolation(error, "other_unique")).toBe(false);
  });

  it("are not other errors", () => {
    expect(isUniqueViolation(wrapped(postgresError("23514", "agent_workspace_name_unique")), "agent_workspace_name_unique")).toBe(false);
    expect(isUniqueViolation(new Error("x"), "agent_workspace_name_unique")).toBe(false);
    expect(isUniqueViolation("23505", "agent_workspace_name_unique")).toBe(false);
  });
});
```

`src/server/agents/validation.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { WorkspaceError } from "@/server/workspaces/errors";
import { parseAgentInput } from "./validation";

const base = { name: "Helper", provider: "openai", model: "gpt-5" };
const codeOf = (input: unknown) => {
  try {
    parseAgentInput(input);
    return "ok";
  } catch (error) {
    return error instanceof WorkspaceError ? error.code : "other";
  }
};

describe("agent input", () => {
  it("is trimmed, with empty texts and parameters as defaults", () => {
    expect(
      parseAgentInput({
        ...base,
        name: "  Helper ",
        description: " ",
        systemPrompt: "\nYou are helpful.\n",
        temperature: "",
        topP: " ",
      }),
    ).toEqual({
      name: "Helper",
      description: "",
      systemPrompt: "You are helpful.",
      provider: "openai",
      model: "gpt-5",
      temperature: null,
      topP: null,
      maxOutputTokens: null,
    });
  });

  it("reads parameters from form strings", () => {
    expect(
      parseAgentInput({ ...base, temperature: "0.7", topP: "1", maxOutputTokens: "1000" }),
    ).toMatchObject({ temperature: 0.7, topP: 1, maxOutputTokens: 1000 });
  });

  it("checks parameter ranges only (invalid_parameters)", () => {
    for (const ok of [{ temperature: "0" }, { temperature: "2" }, { topP: "0.0001" }, { maxOutputTokens: "1" }, { maxOutputTokens: "1000000" }])
      expect(codeOf({ ...base, ...ok })).toBe("ok");
    for (const bad of [
      { temperature: "2.01" },
      { temperature: "-0.1" },
      { temperature: "warm" },
      { topP: "0" },
      { topP: "1.1" },
      { maxOutputTokens: "0" },
      { maxOutputTokens: "1000001" },
      { maxOutputTokens: "1.5" },
    ])
      expect(codeOf({ ...base, ...bad })).toBe("invalid_parameters");
  });

  it("checks names, texts, provider and model (invalid_agent)", () => {
    expect(codeOf({ ...base, name: "a".repeat(80), description: "d".repeat(500), systemPrompt: "s".repeat(20_000) })).toBe("ok");
    expect(codeOf({ ...base, model: "models/gemini-2.5-pro" })).toBe("ok");
    expect(codeOf({ ...base, model: "claude@2025:v1_x" })).toBe("ok");
    for (const bad of [
      { name: " " },
      { name: "a".repeat(81) },
      { description: "d".repeat(501) },
      { systemPrompt: "s".repeat(20_001) },
      { provider: "ollama" },
      { model: "" },
      { model: "gpt 5" },
      { model: "m".repeat(201) },
    ])
      expect(codeOf({ ...base, ...bad })).toBe("invalid_agent");
    expect(codeOf(null)).toBe("invalid_agent");
  });

  it("reports invalid_agent when other fields are wrong too", () => {
    expect(codeOf({ ...base, name: "", temperature: "9" })).toBe("invalid_agent");
  });
});
```

`src/server/agents/status.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import type { ModelListResult } from "@/server/providers/models";
import { type AgentStatus, statusFromModels } from "./status";

const models = [{ id: "gpt-5", label: "gpt-5" }];

describe("agent status", () => {
  it.each<[ModelListResult, AgentStatus]>([
    [{ status: "ok", models }, "ready"],
    [{ status: "ok", models: [] }, "model_unavailable"],
    [{ status: "not_configured" }, "provider_not_configured"],
    [{ status: "unreadable" }, "key_unreadable"],
    [{ status: "unavailable" }, "unknown"],
  ])("%o is %s", (result, expected) => {
    expect(statusFromModels(result, "gpt-5")).toBe(expected);
  });
});
```

Run: `task test -- --project unit src/server/db/errors.test.ts src/server/agents` → FAIL (modules missing).

- [ ] **Step 3: Unique violation helper** — `src/server/db/errors.ts`:

```ts
import "server-only";

/**
 * True if `error`, or an error it wraps (Drizzle wraps driver errors in DrizzleQueryError with
 * the PostgresError as `cause`), is a unique violation (23505) of `constraint`.
 */
export function isUniqueViolation(error: unknown, constraint: string): boolean {
  let current: unknown = error;
  for (let depth = 0; depth < 5 && current instanceof Error; depth++) {
    const details = current as Error & { code?: unknown; constraint_name?: unknown };
    if (details.code === "23505" && details.constraint_name === constraint) return true;
    current = current.cause;
  }
  return false;
}
```

- [ ] **Step 4: Agent input validation** — `src/server/agents/validation.ts`:

```ts
import "server-only";
import { z } from "zod";
import { MODEL_ID_MAX_LENGTH, MODEL_ID_PATTERN, PROVIDER_IDS } from "@/lib/providers";
import { WorkspaceError } from "@/server/workspaces/errors";

/** An optional parameter from a form: "" or missing means the provider's default (null). */
const parameter = (schema: z.ZodNumber) =>
  z.preprocess((value) => {
    if (value === undefined || value === null) return null;
    if (typeof value !== "string") return value;
    const trimmed = value.trim();
    return trimmed === "" ? null : Number(trimmed);
  }, schema.nullable());

/**
 * Parameters are checked for type and range only, never against what a provider or model
 * accepts (maintainer decision; M4 reports the provider's failure).
 */
const agentInputSchema = z.object({
  name: z.string().trim().min(1).max(80),
  description: z.string().trim().max(500).default(""),
  systemPrompt: z.string().trim().max(20_000).default(""),
  provider: z.enum(PROVIDER_IDS),
  model: z.string().trim().min(1).max(MODEL_ID_MAX_LENGTH).regex(MODEL_ID_PATTERN),
  temperature: parameter(z.number().min(0).max(2)),
  topP: parameter(z.number().gt(0).max(1)),
  maxOutputTokens: parameter(z.number().int().min(1).max(1_000_000)),
});

export type AgentValues = z.infer<typeof agentInputSchema>;

const PARAMETERS = new Set<PropertyKey>(["temperature", "topP", "maxOutputTokens"]);

/** invalid_parameters when only parameters are wrong, invalid_agent otherwise. */
export function parseAgentInput(input: unknown): AgentValues {
  const parsed = agentInputSchema.safeParse(input);
  if (parsed.success) return parsed.data;
  const onlyParameters = parsed.error.issues.every((issue) => PARAMETERS.has(issue.path[0] ?? ""));
  throw new WorkspaceError(onlyParameters ? "invalid_parameters" : "invalid_agent");
}
```

- [ ] **Step 5: Agent status** — `src/server/agents/status.ts`:

```ts
import "server-only";
import type { ProviderId } from "@/lib/providers";
import { listModelsForWorkspace, type ModelListResult } from "@/server/providers/models";

export type AgentStatus =
  | "ready"
  | "provider_not_configured"
  | "key_unreadable"
  | "model_unavailable"
  | "unknown";

export const AGENT_STATUS_LABELS: Record<AgentStatus, string> = {
  ready: "Ready",
  provider_not_configured: "Provider not configured",
  key_unreadable: "Provider key unreadable",
  model_unavailable: "Model unavailable",
  unknown: "Couldn't check model",
};

export function statusFromModels(result: ModelListResult, model: string): AgentStatus {
  switch (result.status) {
    case "ok":
      return result.models.some((m) => m.id === model) ? "ready" : "model_unavailable";
    case "not_configured":
      return "provider_not_configured";
    case "unreadable":
      return "key_unreadable";
    case "unavailable":
      return "unknown";
  }
}

/**
 * Computed when shown, from the model cache; nothing is stored, so a model that returns is ready
 * again. No actor: callers have checked access. (M4 replaces this with a scheduled check.)
 */
export async function agentStatus(
  workspaceId: string,
  provider: ProviderId,
  model: string,
): Promise<AgentStatus> {
  return statusFromModels(await listModelsForWorkspace(workspaceId, provider), model);
}
```

Run: `task test -- --project unit src/server/db/errors.test.ts src/server/agents` → PASS.

- [ ] **Step 6: Write the failing integration test** — `src/server/agents/agents.int.test.ts`:

```ts
import { randomUUID } from "node:crypto";
import { and, eq } from "drizzle-orm";
import { afterAll, afterEach, describe, expect, it } from "vitest";
import { getDb } from "@/server/db/client";
import { agent, providerKey, workspaceMember } from "@/server/db/schema";
import { removeProviderKey, setProviderKey } from "@/server/providers/keys";
import { resetModelCache } from "@/server/providers/model-cache";
import { deleteWorkspace } from "@/server/workspaces/workspaces";
import { DEFAULT_FAKE_MODELS, startFakeProvider } from "../../../tests/support/fake-provider";
import { testUsers } from "../../../tests/support/users";
import { expectWorkspaceError, sharedWorkspace } from "../../../tests/support/workspaces";
import { createAgent, deleteAgent, getAgent, listAgents, updateAgent } from "./agents";
import { agentStatus } from "./status";

// Before anything reads the configuration: getEnv() caches it once per test file.
const fake = await startFakeProvider();
fake.useAsProviderEnv();

const users = testUsers();
const db = getDb();
afterEach(() => {
  fake.models = structuredClone(DEFAULT_FAKE_MODELS);
  fake.failure = { openai: null, anthropic: null, google: null };
});
afterAll(async () => {
  await users.cleanup();
  await fake.close();
});

/** Form-like input (strings, as the agent form sends them). */
const input = (overrides: Record<string, unknown> = {}) => ({
  name: "Helper",
  description: "Answers questions",
  systemPrompt: "You are helpful.",
  provider: "openai",
  model: "gpt-5",
  temperature: "",
  topP: "",
  maxOutputTokens: "",
  ...overrides,
});

/** A shared workspace with an OpenAI key (the fake lists gpt-5 and gpt-5-mini). */
async function workspaceWithKey() {
  const ws = await sharedWorkspace(users);
  await setProviderKey(ws.admin.id, ws.id, "openai", `good-agents-${randomUUID()}`);
  return ws;
}

describe("A1: admins write agents, members read them", () => {
  it("members see the list and every setting, including the system prompt, but can't write", async () => {
    const { id, admin, member } = await workspaceWithKey();
    const agentId = await createAgent(admin.id, id, input());

    expect(await listAgents(member.id, id)).toEqual([
      { id: agentId, name: "Helper", provider: "openai", model: "gpt-5" },
    ]);
    expect(await getAgent(member.id, id, agentId)).toMatchObject({
      name: "Helper",
      description: "Answers questions",
      systemPrompt: "You are helpful.",
      temperature: null,
    });
    await expectWorkspaceError(createAgent(member.id, id, input({ name: "Mine" })), "forbidden");
    await expectWorkspaceError(updateAgent(member.id, id, agentId, input({ name: "Mine" })), "forbidden");
    await expectWorkspaceError(deleteAgent(member.id, id, agentId), "forbidden");
  });

  it("non-members get not_found for reads and writes", async () => {
    const { id, admin, outsider } = await workspaceWithKey();
    const agentId = await createAgent(admin.id, id, input());
    await expectWorkspaceError(listAgents(outsider.id, id), "not_found");
    await expectWorkspaceError(getAgent(outsider.id, id, agentId), "not_found");
    await expectWorkspaceError(createAgent(outsider.id, id, input({ name: "Mine" })), "not_found");
    await expectWorkspaceError(updateAgent(outsider.id, id, agentId, input()), "not_found");
    await expectWorkspaceError(deleteAgent(outsider.id, id, agentId), "not_found");
  });

  it("an agent id is a lookup key only: malformed or from another workspace is not_found", async () => {
    const a = await workspaceWithKey();
    const b = await workspaceWithKey();
    const agentId = await createAgent(a.admin.id, a.id, input());
    for (const bad of ["not-a-uuid", agentId]) {
      await expectWorkspaceError(getAgent(b.admin.id, b.id, bad), "not_found");
      await expectWorkspaceError(updateAgent(b.admin.id, b.id, bad, input()), "not_found");
      await expectWorkspaceError(deleteAgent(b.admin.id, b.id, bad), "not_found");
    }
    expect(await getAgent(a.admin.id, a.id, agentId)).toMatchObject({ name: "Helper" });
  });

  it("an admin demoted while editing gets forbidden", async () => {
    const { id, admin } = await workspaceWithKey();
    const agentId = await createAgent(admin.id, id, input());
    await db
      .update(workspaceMember)
      .set({ role: "member" })
      .where(and(eq(workspaceMember.workspaceId, id), eq(workspaceMember.userId, admin.id)));
    await expectWorkspaceError(updateAgent(admin.id, id, agentId, input({ name: "New" })), "forbidden");
  });

  it("deleting removes the agent; deleting it again is not_found", async () => {
    const { id, admin } = await workspaceWithKey();
    const agentId = await createAgent(admin.id, id, input());
    await deleteAgent(admin.id, id, agentId);
    expect(await listAgents(admin.id, id)).toEqual([]);
    await expectWorkspaceError(deleteAgent(admin.id, id, agentId), "not_found");
  });
});

describe("input", () => {
  it("stores the parameters as given and rejects values out of range", async () => {
    const { id, admin } = await workspaceWithKey();
    const agentId = await createAgent(
      admin.id,
      id,
      input({ temperature: "0.7", topP: "0.9", maxOutputTokens: "1000" }),
    );
    expect(await getAgent(admin.id, id, agentId)).toMatchObject({
      temperature: 0.7,
      topP: 0.9,
      maxOutputTokens: 1000,
    });
    await expectWorkspaceError(createAgent(admin.id, id, input({ name: "Hot", temperature: "2.5" })), "invalid_parameters");
    await expectWorkspaceError(createAgent(admin.id, id, input({ name: "" })), "invalid_agent");
  });
});

describe("A2: names are unique per workspace, ignoring case", () => {
  it("refuses the same name in other case, on create and rename", async () => {
    const { id, admin } = await workspaceWithKey();
    await createAgent(admin.id, id, input({ name: "Helper" }));
    await expectWorkspaceError(createAgent(admin.id, id, input({ name: " HELPER " })), "agent_name_taken");
    const other = await createAgent(admin.id, id, input({ name: "Other" }));
    await expectWorkspaceError(updateAgent(admin.id, id, other, input({ name: "helper" })), "agent_name_taken");
  });

  it("allows the same name in another workspace", async () => {
    const a = await workspaceWithKey();
    const b = await workspaceWithKey();
    await createAgent(a.admin.id, a.id, input());
    await expect(createAgent(b.admin.id, b.id, input())).resolves.toEqual(expect.any(String));
  });
});

describe("A3: provider and model must be usable when chosen", () => {
  it("creating needs a key for the provider and a model from its list", async () => {
    const { id, admin } = await workspaceWithKey();
    await expectWorkspaceError(
      createAgent(admin.id, id, input({ provider: "anthropic", model: "claude-sonnet-4-5" })),
      "provider_not_configured",
    );
    await expectWorkspaceError(createAgent(admin.id, id, input({ model: "gpt-unknown" })), "model_unavailable");
    resetModelCache();
    fake.failure.openai = 503;
    await expectWorkspaceError(createAgent(admin.id, id, input()), "provider_unavailable");
  });

  it("an agent whose model disappeared can still be edited, but not moved to a missing model", async () => {
    const { id, admin } = await workspaceWithKey();
    const agentId = await createAgent(admin.id, id, input());
    fake.models.openai = ["gpt-5-mini"];
    resetModelCache();

    await updateAgent(admin.id, id, agentId, input({ name: "Renamed" }));
    expect((await getAgent(admin.id, id, agentId)).name).toBe("Renamed");
    await expectWorkspaceError(
      updateAgent(admin.id, id, agentId, input({ name: "Renamed", model: "gpt-4.1" })),
      "model_unavailable",
    );
    await updateAgent(admin.id, id, agentId, input({ name: "Renamed", model: "gpt-5-mini" }));
    expect((await getAgent(admin.id, id, agentId)).model).toBe("gpt-5-mini");
  });
});

describe("P5 and availability", () => {
  it("removing a key keeps its agents, which then show provider_not_configured", async () => {
    const { id, admin } = await workspaceWithKey();
    const agentId = await createAgent(admin.id, id, input());
    expect(await agentStatus(id, "openai", "gpt-5")).toBe("ready");

    await removeProviderKey(admin.id, id, "openai");
    expect(await getAgent(admin.id, id, agentId)).toMatchObject({ provider: "openai", model: "gpt-5" });
    expect(await agentStatus(id, "openai", "gpt-5")).toBe("provider_not_configured");
    // Editing other fields still works (P5, A3).
    await updateAgent(admin.id, id, agentId, input({ description: "Still here" }));
  });

  it("reports model_unavailable, unknown and key_unreadable", async () => {
    const { id } = await workspaceWithKey();
    fake.models.openai = ["gpt-5-mini"];
    resetModelCache();
    expect(await agentStatus(id, "openai", "gpt-5")).toBe("model_unavailable");

    resetModelCache();
    fake.failure.openai = 503;
    expect(await agentStatus(id, "openai", "gpt-5")).toBe("unknown");

    await db
      .update(providerKey)
      .set({ encryptedKey: "v1.gone.AAAAAAAAAAAAAAAA.AAAAAAAAAAAAAAAAAAAAAA.AAAA" })
      .where(and(eq(providerKey.workspaceId, id), eq(providerKey.provider, "openai")));
    expect(await agentStatus(id, "openai", "gpt-5")).toBe("key_unreadable");
  });
});

describe("A4: deleting a workspace", () => {
  it("deletes its keys and agents", async () => {
    const { id, admin } = await workspaceWithKey();
    await createAgent(admin.id, id, input());
    await deleteWorkspace(admin.id, id, "Team");
    expect(await db.$count(agent, eq(agent.workspaceId, id))).toBe(0);
    expect(await db.$count(providerKey, eq(providerKey.workspaceId, id))).toBe(0);
  });
});
```

Run: `task test -- --project integration src/server/agents/agents.int.test.ts` → FAIL (cannot resolve `./agents`).

- [ ] **Step 7: The agent service** — `src/server/agents/agents.ts`:

```ts
import "server-only";
import { and, asc, eq, sql } from "drizzle-orm";
import type { ProviderId } from "@/lib/providers";
import { getDb } from "@/server/db/client";
import { isUniqueViolation } from "@/server/db/errors";
import { agent } from "@/server/db/schema";
import { listModelsForWorkspace } from "@/server/providers/models";
import { WorkspaceError } from "@/server/workspaces/errors";
import {
  lockForAdmin,
  parseId,
  requireAdminRole,
  requireMembership,
} from "@/server/workspaces/internal";
import { parseAgentInput } from "./validation";

export type AgentSummary = { id: string; name: string; provider: ProviderId; model: string };
export type Agent = AgentSummary & {
  description: string;
  systemPrompt: string;
  temperature: number | null;
  topP: number | null;
  maxOutputTokens: number | null;
  updatedAt: Date;
};

const NAME_INDEX = "agent_workspace_name_unique";

const agentColumns = {
  id: agent.id,
  name: agent.name,
  description: agent.description,
  systemPrompt: agent.systemPrompt,
  provider: agent.provider,
  model: agent.model,
  temperature: agent.temperature,
  topP: agent.topP,
  maxOutputTokens: agent.maxOutputTokens,
  updatedAt: agent.updatedAt,
};

/** Always by workspace and id: an id from another workspace is not_found. */
async function findAgent(workspaceId: string, agentId: string): Promise<Agent> {
  const [row] = await getDb()
    .select(agentColumns)
    .from(agent)
    .where(and(eq(agent.id, agentId), eq(agent.workspaceId, workspaceId)));
  if (!row) throw new WorkspaceError("not_found");
  return row;
}

/**
 * A3: the provider has a readable key and lists the model. Runs before the transaction (it may
 * call the provider). Accepted race: a key removed between this check and the save still lets the
 * save through; the agent then shows "Provider not configured" (P5).
 */
async function assertModelSelectable(workspaceId: string, provider: ProviderId, model: string) {
  const result = await listModelsForWorkspace(workspaceId, provider);
  switch (result.status) {
    case "ok":
      if (!result.models.some((m) => m.id === model)) throw new WorkspaceError("model_unavailable");
      return;
    case "not_configured":
    case "unreadable":
      throw new WorkspaceError("provider_not_configured");
    case "unavailable":
      throw new WorkspaceError("provider_unavailable");
  }
}

/** A2 rests on the unique index alone (Unicode case rules differ between JS and lower()). */
async function mapNameConflict<T>(operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    if (isUniqueViolation(error, NAME_INDEX)) throw new WorkspaceError("agent_name_taken");
    throw error;
  }
}

export async function listAgents(actorId: string, workspaceId: string): Promise<AgentSummary[]> {
  const { workspace: ws } = await requireMembership(actorId, workspaceId);
  return getDb()
    .select({ id: agent.id, name: agent.name, provider: agent.provider, model: agent.model })
    .from(agent)
    .where(eq(agent.workspaceId, ws.id))
    .orderBy(asc(sql`lower(${agent.name})`), asc(agent.id));
}

/** A1: members see every setting, including the system prompt. */
export async function getAgent(
  actorId: string,
  workspaceId: string,
  agentId: string,
): Promise<Agent> {
  const id = parseId(agentId);
  const { workspace: ws } = await requireMembership(actorId, workspaceId);
  return findAgent(ws.id, id);
}

export async function createAgent(
  actorId: string,
  workspaceId: string,
  input: unknown,
): Promise<string> {
  const values = parseAgentInput(input);
  const { workspace: ws } = await requireAdminRole(actorId, workspaceId);
  await assertModelSelectable(ws.id, values.provider, values.model);
  return mapNameConflict(() =>
    getDb().transaction(async (tx) => {
      await lockForAdmin(tx, ws.id, actorId);
      const [row] = await tx
        .insert(agent)
        .values({ ...values, workspaceId: ws.id, createdByUserId: actorId })
        .returning({ id: agent.id });
      if (!row) throw new Error("agent insert returned nothing");
      return row.id;
    }),
  );
}

/** A3 is only checked when provider or model change, so an agent whose model is gone stays editable. */
export async function updateAgent(
  actorId: string,
  workspaceId: string,
  agentId: string,
  input: unknown,
): Promise<void> {
  const id = parseId(agentId);
  const values = parseAgentInput(input);
  const { workspace: ws } = await requireAdminRole(actorId, workspaceId);
  const current = await findAgent(ws.id, id);
  if (current.provider !== values.provider || current.model !== values.model) {
    await assertModelSelectable(ws.id, values.provider, values.model);
  }
  await mapNameConflict(() =>
    getDb().transaction(async (tx) => {
      await lockForAdmin(tx, ws.id, actorId);
      const [row] = await tx
        .update(agent)
        .set({ ...values, updatedAt: new Date() })
        .where(and(eq(agent.id, id), eq(agent.workspaceId, ws.id)))
        .returning({ id: agent.id });
      if (!row) throw new WorkspaceError("not_found");
    }),
  );
}

export async function deleteAgent(
  actorId: string,
  workspaceId: string,
  agentId: string,
): Promise<void> {
  const id = parseId(agentId);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    const [row] = await tx
      .delete(agent)
      .where(and(eq(agent.id, id), eq(agent.workspaceId, ws.id)))
      .returning({ id: agent.id });
    if (!row) throw new WorkspaceError("not_found");
  });
}
```

- [ ] **Step 8: Run the tests**

Run: `task test -- --project integration src/server/agents/agents.int.test.ts`
Expected: PASS. If the duplicate-name test reports an unexpected error instead of `agent_name_taken`, print `error.cause` once (locally, not committed) and check that the postgres error's `constraint_name` is `agent_workspace_name_unique`.

- [ ] **Step 9: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/workspaces/errors.ts src/server/db/errors.ts src/server/db/errors.test.ts src/server/agents
git commit -m "feat(agents): agent service with name, provider and model rules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 7: Workspace navigation and the providers page

**Files:**
- Modify: `src/app/(signed-in)/w/[workspaceId]/layout.tsx` (nav), `src/app/(signed-in)/w/[workspaceId]/page.tsx` (home text)
- Modify: `src/app/(signed-in)/workspaces/error-messages.ts`, `src/app/(signed-in)/workspaces/error-messages.test.ts`
- Create: `src/components/submit-button.tsx`
- Create: `src/app/(signed-in)/w/[workspaceId]/providers/page.tsx`, `actions.ts`, `loading.tsx`
- Create: `tests/support/fake-provider-server.ts`
- Modify: `playwright.config.ts`
- Create: `tests/e2e/support/sessions.ts`, `tests/e2e/support/providers.ts`
- Modify: `tests/e2e/auth.spec.ts`, `tests/e2e/workspaces.spec.ts`, `tests/e2e/instant.spec.ts`
- Test: `tests/e2e/providers.spec.ts`

**Interfaces:**
- Consumes: `setProviderKey`, `removeProviderKey`, `listProviderStatus`, `ProviderStatus` (Tasks 3–4); `PROVIDER_NAMES` (Task 2); `WORKSPACE_ERROR_CODES` (Tasks 3, 6); `runWorkspaceAction`, `field`, `workspaceErrorMessage`, `requireWorkspaceMember`, `requireUser` (M2); `startFakeProvider` (Task 3).
- Produces:
  - route `/w/[workspaceId]/providers`; nav links Start · Agents · Providers · Settings (the Agents link leads to Task 8's page)
  - `SubmitButton` (`src/components/submit-button.tsx`): Button props plus `pendingLabel: string`
  - `setProviderKeyAction(workspaceId: string, provider: string, formData: FormData): Promise<never>`, `removeProviderKeyAction(workspaceId: string, provider: string): Promise<never>`
  - E2E support: `HOME_CARD_TEXT`, `browserSessions()` → `{ signedIn(browser, user, options?): Promise<Page>, closeAll() }`, `createWorkspace(page, name): Promise<string>` (path `/w/<id>`), `addMember(adminPage, memberPage, workspacePath, member, workspaceName)`, `workspaceNav(page)`, `alertWith(page, text)` (sessions.ts); `providerRow(page, name)`, `saveProviderKey(page, workspacePath, name, key)`, `collectResponseBodies(page): () => Promise<string[]>` (providers.ts)
  - Playwright starts the fake provider on port 3199; the app servers get `AGENTY_*_BASE_URL` pointing at it

- [ ] **Step 1: Failing error-message test** — add to `src/app/(signed-in)/workspaces/error-messages.test.ts`:

```ts
import { WORKSPACE_ERROR_CODES } from "@/server/workspaces/errors";

describe("service error codes", () => {
  it("each have fixed text (not_found shows the not-found page instead)", () => {
    for (const code of WORKSPACE_ERROR_CODES.filter((c) => c !== "not_found")) {
      expect(workspaceErrorMessage(code), code).not.toBe("Something went wrong. Please try again.");
    }
  });

  it("use the spec's wording for key verification", () => {
    expect(workspaceErrorMessage("key_rejected")).toBe("The provider rejected this key.");
    expect(workspaceErrorMessage("provider_unavailable")).toBe(
      "The provider could not be reached. Try again later.",
    );
    expect(workspaceErrorMessage("key_unverified")).toBe(
      "The key could not be verified. Check that it may list models.",
    );
  });
});
```

Run: `task test -- --project unit "src/app/(signed-in)/workspaces/error-messages.test.ts"` → FAIL (the M3 codes fall back to the generic text).

- [ ] **Step 2: Messages** — in `src/app/(signed-in)/workspaces/error-messages.ts`, add to `MESSAGES` after `confirmation_mismatch`:

```ts
  key_invalid: "API keys have 20 to 500 characters, without spaces.",
  key_rejected: "The provider rejected this key.",
  provider_unavailable: "The provider could not be reached. Try again later.",
  key_unverified: "The key could not be verified. Check that it may list models.",
  agent_name_taken: "An agent with this name already exists in this workspace.",
  provider_not_configured: "Set an API key for this provider first.",
  model_unavailable: "This model isn't available with the provider's key. Choose another one.",
  invalid_parameters:
    "Temperature must be 0 to 2, top P above 0 up to 1, and max output tokens a whole number from 1 to 1,000,000.",
  invalid_agent:
    "Check the fields: a name of 1 to 80 characters, a description up to 500, a system prompt up to 20,000, and a provider and model.",
```

Run the unit test again → PASS.

- [ ] **Step 3: Submit button with a pending state** — `src/components/submit-button.tsx`:

```tsx
"use client";

import type { ComponentProps } from "react";
import { useFormStatus } from "react-dom";
import { Button } from "@/components/ui/button";

/** Its form's submit button: disabled and relabelled while the form's action runs. */
export function SubmitButton({
  children,
  pendingLabel,
  disabled,
  ...props
}: Omit<ComponentProps<typeof Button>, "type"> & { pendingLabel: string }) {
  const { pending } = useFormStatus();
  return (
    <Button {...props} disabled={disabled || pending} type="submit">
      {pending ? pendingLabel : children}
    </Button>
  );
}
```

- [ ] **Step 4: Navigation and home text** — in `src/app/(signed-in)/w/[workspaceId]/layout.tsx`, add the two links after Start:

```tsx
        <Link className="hover:underline" href={base}>
          Start
        </Link>
        <Link className="hover:underline" href={`${base}/agents`}>
          Agents
        </Link>
        <Link className="hover:underline" href={`${base}/providers`}>
          Providers
        </Link>
```

In `src/app/(signed-in)/w/[workspaceId]/page.tsx`, the card title becomes `Chat arrives in the next milestone` (the description stays). Update the tests that wait for the old text:

Run: `grep -rl "Agents arrive in the next milestone" tests | xargs perl -pi -e 's/Agents arrive in the next milestone/Chat arrives in the next milestone/g'`
Then: `grep -rn "Agents arrive" src tests` → no matches.

- [ ] **Step 5: Server actions** — `src/app/(signed-in)/w/[workspaceId]/providers/actions.ts`:

```ts
"use server";

import { redirect } from "next/navigation";
import { field, runWorkspaceAction } from "@/app/(signed-in)/workspaces/run-action";
import { requireUser } from "@/server/auth/session";
import { removeProviderKey, setProviderKey } from "@/server/providers/keys";

// workspaceId and provider are bound in the page but still client input: the service checks both.
const providersPath = (workspaceId: string) =>
  `/w/${encodeURIComponent(String(workspaceId))}/providers`;

/** The key is only passed on to the service; it is never part of a redirect or a response. */
export async function setProviderKeyAction(
  workspaceId: string,
  provider: string,
  formData: FormData,
): Promise<never> {
  const user = await requireUser();
  const path = providersPath(workspaceId);
  await runWorkspaceAction(path, () =>
    setProviderKey(user.id, workspaceId, provider, field(formData, "key")),
  );
  redirect(path);
}

export async function removeProviderKeyAction(
  workspaceId: string,
  provider: string,
): Promise<never> {
  const user = await requireUser();
  const path = providersPath(workspaceId);
  await runWorkspaceAction(path, () => removeProviderKey(user.id, workspaceId, provider));
  redirect(path);
}
```

- [ ] **Step 6: The page** — `src/app/(signed-in)/w/[workspaceId]/providers/loading.tsx`:

```tsx
export { default } from "@/components/page-loading";
```

`src/app/(signed-in)/w/[workspaceId]/providers/page.tsx`:

```tsx
import type { Metadata } from "next";
import { workspaceErrorMessage } from "@/app/(signed-in)/workspaces/error-messages";
import { SubmitButton } from "@/components/submit-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { PROVIDER_NAMES } from "@/lib/providers";
import { listProviderStatus, type ProviderStatus } from "@/server/providers/keys";
import { requireWorkspaceMember } from "@/server/workspaces/access";
import { removeProviderKeyAction, setProviderKeyAction } from "./actions";

export const metadata: Metadata = { title: "Providers · Agenty" };

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);
const dateFormat = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeZone: "UTC" });

function statusText(status: ProviderStatus): string {
  if (!status.configured) return "Not configured";
  if (!status.readable) return "Key unreadable, set it again";
  // Only admins get a hint from listProviderStatus.
  if (status.hint && status.updatedAt) {
    return `Configured ••••${status.hint} · updated ${dateFormat.format(status.updatedAt)}`;
  }
  return "Configured";
}

export default async function ProvidersPage({
  params,
  searchParams,
}: PageProps<"/w/[workspaceId]/providers">) {
  const { workspaceId } = await params;
  const access = await requireWorkspaceMember(workspaceId);
  const query = await searchParams;
  const error = workspaceErrorMessage(first(query.error));
  const isAdmin = access.role === "admin";
  const id = access.workspace.id;
  const statuses = await listProviderStatus(access.user.id, id);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="font-semibold text-xl">Providers</h2>
        <p className="text-muted-foreground text-sm">
          {isAdmin
            ? "API keys for this workspace's agents. A key is checked with the provider before it is saved, and it is never shown again."
            : "API keys for this workspace's agents. Only admins can change them."}
        </p>
      </div>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      <Table aria-label="Providers">
        <TableHeader>
          <TableRow>
            <TableHead>Provider</TableHead>
            <TableHead>Status</TableHead>
            {isAdmin ? <TableHead>API key</TableHead> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {statuses.map((status) => {
            const name = PROVIDER_NAMES[status.provider];
            const inputId = `key-${status.provider}`;
            return (
              <TableRow key={status.provider}>
                <TableCell className="font-medium">{name}</TableCell>
                <TableCell
                  className={status.configured && status.readable ? undefined : "text-muted-foreground"}
                >
                  {statusText(status)}
                </TableCell>
                {isAdmin ? (
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-2">
                      <form
                        action={setProviderKeyAction.bind(null, id, status.provider)}
                        className="flex items-center gap-2"
                      >
                        <Label className="sr-only" htmlFor={inputId}>
                          {name} API key
                        </Label>
                        <Input
                          autoComplete="off"
                          className="w-56"
                          id={inputId}
                          maxLength={500}
                          minLength={20}
                          name="key"
                          placeholder={status.configured ? "New key" : "API key"}
                          required
                          type="password"
                        />
                        <SubmitButton pendingLabel="Checking…" size="sm">
                          Save
                        </SubmitButton>
                      </form>
                      {status.configured ? (
                        <form action={removeProviderKeyAction.bind(null, id, status.provider)}>
                          <SubmitButton pendingLabel="Removing…" size="sm" variant="outline">
                            Remove
                          </SubmitButton>
                        </form>
                      ) : null}
                    </div>
                  </TableCell>
                ) : null}
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
```

- [ ] **Step 7: Fake provider for Playwright** — `tests/support/fake-provider-server.ts`:

```ts
// The fake model provider as a process, started by Playwright (playwright.config.ts) so the E2E
// app servers can verify keys without real provider accounts. Plain Node (type stripping).
import { startFakeProvider } from "./fake-provider.ts";

const port = Number(process.env.FAKE_PROVIDER_PORT ?? "3199");
await startFakeProvider(port);
console.log(`Fake model provider listening on http://127.0.0.1:${port}`);
```

In `playwright.config.ts`:

```ts
/** The fake model provider (tests/support/fake-provider-server.ts); keys starting "good-" work. */
const fakeProviderPort = 3199;
const fakeProvider = `http://127.0.0.1:${fakeProviderPort}`;
```

add to `standaloneServer`'s `env`, before `...env`:

```ts
      AGENTY_OPENAI_BASE_URL: `${fakeProvider}/openai/v1`,
      AGENTY_ANTHROPIC_BASE_URL: `${fakeProvider}/anthropic/v1`,
      AGENTY_GOOGLE_BASE_URL: `${fakeProvider}/google/v1beta`,
```

and make it the first `webServer` entry:

```ts
  webServer: [
    {
      command: "node tests/support/fake-provider-server.ts",
      url: `${fakeProvider}/health`,
      reuseExistingServer: false,
      timeout: 10 * 1000,
      env: { FAKE_PROVIDER_PORT: String(fakeProviderPort) },
    },
    standaloneServer(port),
    // Port 1 refuses connections, so the discovery pre-check fails at once.
    standaloneServer(unreachableIdpPort, { OIDC_DISCOVERY_URL: "http://localhost:1/x" }),
  ],
```

- [ ] **Step 8: E2E support** — `tests/e2e/support/sessions.ts`:

```ts
import {
  type Browser,
  type BrowserContext,
  type BrowserContextOptions,
  expect,
  type Page,
} from "@playwright/test";
import { loginAtMockIdp, type uniqueIdpUser } from "./mock-idp";

type IdpUser = ReturnType<typeof uniqueIdpUser>;

/** The workspace home's card; shows once a signed-in page has streamed. */
export const HOME_CARD_TEXT = "Chat arrives in the next milestone";

export const workspaceNav = (page: Page) =>
  page.getByRole("navigation", { name: "Workspace", exact: true });
// Next's route announcer is a role=alert element too, so match the text.
export const alertWith = (page: Page, text: string) =>
  page.getByRole("alert").filter({ hasText: text });

/** Signed-in browser sessions for one test file; call closeAll() in afterEach. */
export function browserSessions() {
  const contexts: BrowserContext[] = [];
  return {
    /** A fresh browser session signed in as `user`, on their personal workspace home. */
    async signedIn(
      browser: Browser,
      user: IdpUser,
      options: BrowserContextOptions = {},
    ): Promise<Page> {
      const context = await browser.newContext(options);
      contexts.push(context);
      const page = await context.newPage();
      await page.goto("/sign-in");
      await page.getByRole("button", { name: "Sign in", exact: true }).click();
      await loginAtMockIdp(page, user);
      await expect(page).toHaveURL(/\/w\/[0-9a-f-]{36}$/);
      // Scoped to <main>: streamed content briefly sits in a hidden copy at the end of <body>.
      await expect(page.getByRole("main").getByText(HOME_CARD_TEXT)).toBeVisible();
      return page;
    },
    async closeAll() {
      await Promise.all(contexts.splice(0).map((context) => context.close()));
    },
  };
}

/** Creates a shared workspace as the user of `page`; returns its path (`/w/<id>`). */
export async function createWorkspace(page: Page, name: string): Promise<string> {
  await page.goto("/workspaces");
  await page.getByLabel("Name", { exact: true }).fill(name);
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name, exact: true })).toBeVisible();
  return new URL(page.url()).pathname;
}

/** The admin invites `member` (who has signed in before); the member accepts. */
export async function addMember(
  adminPage: Page,
  memberPage: Page,
  workspacePath: string,
  member: IdpUser,
  workspaceName: string,
) {
  await adminPage.goto(`${workspacePath}/settings`);
  const search = adminPage.getByLabel("Search users", { exact: true });
  const invite = adminPage
    .getByRole("list", { name: "Search results" })
    .getByRole("listitem")
    .filter({ hasText: member.email })
    .getByRole("button", { name: "Invite", exact: true });
  // Retried: typing before hydration is reset by the client component.
  await expect(async () => {
    await search.fill(member.email);
    await expect(invite).toBeVisible({ timeout: 2_000 });
  }).toPass();
  await invite.click();
  await expect(
    adminPage.getByRole("list", { name: "Pending invitations" }).getByText(member.email),
  ).toBeVisible();

  await memberPage.goto("/workspaces");
  await memberPage
    .getByRole("listitem")
    .filter({ hasText: workspaceName })
    .getByRole("button", { name: "Accept", exact: true })
    .click();
  await expect(
    memberPage.getByRole("heading", { level: 1, name: workspaceName, exact: true }),
  ).toBeVisible();
}
```

`tests/e2e/support/providers.ts`:

```ts
import { expect, type Page } from "@playwright/test";

export const providerRow = (page: Page, name: string) =>
  page.getByRole("table", { name: "Providers", exact: true }).getByRole("row").filter({ hasText: name });

/** Saves `key` for the provider named `name` (admins only). The fake accepts keys starting "good-". */
export async function saveProviderKey(page: Page, workspacePath: string, name: string, key: string) {
  await page.goto(`${workspacePath}/providers`);
  const row = providerRow(page, name);
  await row.getByLabel(`${name} API key`, { exact: true }).fill(key);
  await row.getByRole("button", { name: "Save", exact: true }).click();
  await expect(row).toContainText(`Configured ••••${key.slice(-4)}`);
}

/** Collects the bodies of every response the page gets from now on (HTML, RSC payloads, actions). */
export function collectResponseBodies(page: Page): () => Promise<string[]> {
  const bodies: Promise<string>[] = [];
  page.on("response", (response) => {
    // Redirects have no body to read.
    bodies.push(response.text().catch(() => ""));
  });
  return () => Promise.all(bodies);
}
```

- [ ] **Step 9: The E2E test** — `tests/e2e/providers.spec.ts`:

```ts
import { expect, type Page, test } from "@playwright/test";
import { uniqueIdpUser } from "./support/mock-idp";
import { collectResponseBodies, providerRow, saveProviderKey } from "./support/providers";
import {
  addMember,
  alertWith,
  browserSessions,
  createWorkspace,
  HOME_CARD_TEXT,
  workspaceNav,
} from "./support/sessions";

const sessions = browserSessions();
test.afterEach(() => sessions.closeAll());

const providersHeading = (page: Page) =>
  page.getByRole("heading", { level: 2, name: "Providers", exact: true });

test("admins set, replace and remove keys; members only see which providers are configured", async ({
  browser,
}) => {
  const ada = uniqueIdpUser("pvada", "Ada Keys");
  const bob = uniqueIdpUser("pvbob", "Bob Keys");
  const bobPage = await sessions.signedIn(browser, bob);
  const adaPage = await sessions.signedIn(browser, ada);
  const team = `Keys ${ada.sub}`;
  const teamPath = await createWorkspace(adaPage, team);
  await addMember(adaPage, bobPage, teamPath, bob, team);

  await workspaceNav(adaPage).getByRole("link", { name: "Providers", exact: true }).click();
  await expect(providersHeading(adaPage)).toBeVisible();
  for (const name of ["OpenAI", "Anthropic", "Google Gemini"]) {
    await expect(providerRow(adaPage, name)).toContainText("Not configured");
  }

  // A key the provider rejects is not stored; the message is fixed text.
  const openai = providerRow(adaPage, "OpenAI");
  await openai.getByLabel("OpenAI API key", { exact: true }).fill("bad-key-e2e-000000000000");
  await openai.getByRole("button", { name: "Save", exact: true }).click();
  await expect(alertWith(adaPage, "The provider rejected this key.")).toBeVisible();
  await expect(openai).toContainText("Not configured");

  await saveProviderKey(adaPage, teamPath, "OpenAI", `good-e2e-${ada.sub}-abcd`);
  await expect(providerRow(adaPage, "OpenAI")).toContainText("Configured ••••abcd · updated");
  await saveProviderKey(adaPage, teamPath, "OpenAI", `good-e2e-${ada.sub}-wxyz`);

  // Bob sees that OpenAI is configured, nothing more.
  await bobPage.goto(`${teamPath}/providers`);
  await expect(providerRow(bobPage, "OpenAI")).toContainText("Configured");
  await expect(providerRow(bobPage, "OpenAI")).not.toContainText("••••");
  await expect(bobPage.locator("input[name=key]")).toHaveCount(0);
  await expect(bobPage.getByRole("button", { name: "Remove", exact: true })).toHaveCount(0);

  await providerRow(adaPage, "OpenAI").getByRole("button", { name: "Remove", exact: true }).click();
  await expect(providerRow(adaPage, "OpenAI")).toContainText("Not configured");
  await bobPage.reload();
  await expect(providerRow(bobPage, "OpenAI")).toContainText("Not configured");
});

test("the personal workspace has a working Providers tab", async ({ browser }) => {
  const page = await sessions.signedIn(browser, uniqueIdpUser("pvpers", "Pat Providers"));
  await workspaceNav(page).getByRole("link", { name: "Providers", exact: true }).click();
  await expect(providersHeading(page)).toBeVisible();
  await expect(page.getByLabel("OpenAI API key", { exact: true })).toBeVisible();
});

test("a saved key never reaches the browser", async ({ browser }) => {
  const user = uniqueIdpUser("pvleak", "Lee Leak");
  const page = await sessions.signedIn(browser, user);
  const home = new URL(page.url()).pathname;
  const key = `good-leak-${user.sub}-9876`;
  const bodies = collectResponseBodies(page);

  // The save action's response, a full page load and a client navigation (RSC payload).
  await saveProviderKey(page, home, "OpenAI", key);
  await page.reload();
  await expect(providerRow(page, "OpenAI")).toContainText("Configured ••••9876");
  await workspaceNav(page).getByRole("link", { name: "Start", exact: true }).click();
  await expect(page.getByRole("main").getByText(HOME_CARD_TEXT)).toBeVisible();
  await workspaceNav(page).getByRole("link", { name: "Providers", exact: true }).click();
  await expect(providerRow(page, "OpenAI")).toContainText("Configured ••••9876");

  const all = await bodies();
  expect(all.length).toBeGreaterThan(3);
  for (const body of all) expect(body).not.toContain(key);
  expect(await page.content()).not.toContain(key);
});
```

- [ ] **Step 10: Instant navigation entries** — in `tests/e2e/instant.spec.ts`, add to the initial-load list:

```ts
    ["team/providers", "Providers · Agenty"],
```

and to `describe("client navigations commit at once and keep the sidebar", …)`:

```ts
  test("into /w/[workspaceId]/providers", async ({ page }) => {
    await page.goto(teamUrl);
    await expect(homeCard(page)).toBeVisible();
    await instant(page, async () => {
      await workspaceNav(page).getByRole("link", { name: "Providers", exact: true }).click();
      await page.waitForURL((url) => url.pathname === `${teamUrl}/providers`, {
        timeout: navigationTimeout,
      });
      await expect(homeCard(page)).toBeHidden();
      // The workspace layout is shared with the previous page and stays.
      await expect(sidebar(page)).toBeVisible();
      await expect(heading(page, team)).toBeVisible();
      await expect(page.getByRole("heading", { name: "Providers", exact: true })).toBeHidden();
    });
    await expect(page.getByRole("heading", { name: "Providers", exact: true })).toBeVisible();
  });
```

- [ ] **Step 11: Build and run the E2E tests**

Run: `task build`, then `task test:e2e`
Expected: all pass, including the new `providers.spec.ts` and instant entries. A `Route "/w/[workspaceId]/providers": … uncached data` error means the page reads request-time data outside its `loading.tsx` boundary: check that `loading.tsx` exists next to `page.tsx`.

- [ ] **Step 12: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add "src/app/(signed-in)/w/[workspaceId]/layout.tsx" "src/app/(signed-in)/w/[workspaceId]/page.tsx" "src/app/(signed-in)/w/[workspaceId]/providers" "src/app/(signed-in)/workspaces/error-messages.ts" "src/app/(signed-in)/workspaces/error-messages.test.ts" src/components/submit-button.tsx tests/support/fake-provider-server.ts playwright.config.ts tests/e2e/support/sessions.ts tests/e2e/support/providers.ts tests/e2e/providers.spec.ts tests/e2e/auth.spec.ts tests/e2e/workspaces.spec.ts tests/e2e/instant.spec.ts
git commit -m "feat(providers): providers page and workspace navigation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 8: Agent pages

**Files:**
- Create: `src/components/ui/textarea.tsx`, `src/components/ui/collapsible.tsx`, `src/components/ui/alert-dialog.tsx` (shadcn CLI)
- Create in `src/app/(signed-in)/w/[workspaceId]/agents/`: `form-values.ts`, `form-values.test.ts`, `provider-choices.ts`, `actions.ts`, `agent-form.tsx`, `delete-agent-button.tsx`, `page.tsx`, `loading.tsx`, `new/page.tsx`, `new/loading.tsx`, `[agentId]/page.tsx`, `[agentId]/loading.tsx`
- Create: `tests/e2e/support/agents.ts`
- Modify: `tests/e2e/instant.spec.ts`
- Test: `tests/e2e/agents.spec.ts`

**Interfaces:**
- Consumes: `listAgents`, `getAgent`, `createAgent`, `updateAgent`, `deleteAgent`, `Agent` (Task 6); `agentStatus`, `AGENT_STATUS_LABELS` (Task 6); `listProviderStatus` (Task 3); `listModelsForWorkspace` (Task 4); `PROVIDER_NAMES`, `ProviderId`, `ModelOption` (Task 2); `workspaceErrorMessage`, `runWorkspaceAction`, `WorkspaceError`, `requireWorkspaceMember`, `requireUser` (M2); E2E support from Task 7.
- Produces:
  - routes `/w/[workspaceId]/agents`, `/w/[workspaceId]/agents/new`, `/w/[workspaceId]/agents/[agentId]`
  - `type AgentFormValues` (all fields as strings), `type AgentFormState = { error: string | null; values: AgentFormValues }`, `type ProviderChoice = { id: ProviderId; name: string; models: ModelOption[] | null }`, `EMPTY_AGENT_VALUES`, `readAgentForm(formData: unknown): AgentFormValues`, `agentToValues(agent): AgentFormValues` (form-values.ts)
  - `loadProviderChoices(actorId, workspaceId): Promise<ProviderChoice[]>` (provider-choices.ts)
  - `saveAgentAction(workspaceId: string, agentId: string | null, previous: AgentFormState, formData: FormData): Promise<AgentFormState>`, `deleteAgentAction(workspaceId: string, agentId: string): Promise<never>`
  - E2E support: `agentsTable(page)`, `chooseOption(page, label, option)`, `createAgentViaForm(page, workspacePath, agent)`, `deleteAgentViaDialog(page)`

- [ ] **Step 1: Add the shadcn components**

Run: `pnpm exec shadcn add textarea collapsible alert-dialog --yes`
Expected: `src/components/ui/textarea.tsx`, `collapsible.tsx` and `alert-dialog.tsx` (Base UI preset from `components.json`). `git status --short src/components/ui` must list only these three; if the CLI changed `button.tsx`, restore it with `git checkout src/components/ui/button.tsx` (it is customised: it exports `buttonVariants`). Run `task format` so Biome accepts them.

- [ ] **Step 2: Failing unit test for the form values** — `src/app/(signed-in)/w/[workspaceId]/agents/form-values.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { agentToValues, EMPTY_AGENT_VALUES, readAgentForm } from "./form-values";

describe("agent form values", () => {
  it("echo only the agent's own fields, as strings", () => {
    const data = new FormData();
    data.set("name", "Helper");
    data.set("temperature", "0.7");
    data.set("key", "good-secret-0123456789");
    data.set("$ACTION_ID_abc", "");
    const values = readAgentForm(data);
    expect(values).toEqual({ ...EMPTY_AGENT_VALUES, name: "Helper", temperature: "0.7" });
    expect(JSON.stringify(values)).not.toContain("good-secret");
  });

  it("are empty for a call without FormData", () => {
    expect(readAgentForm(null)).toEqual(EMPTY_AGENT_VALUES);
  });

  it("show stored parameters, and empty fields for provider defaults", () => {
    expect(
      agentToValues({
        name: "Helper",
        description: "",
        systemPrompt: "Be brief.",
        provider: "anthropic",
        model: "claude-sonnet-4-5",
        temperature: 0.5,
        topP: null,
        maxOutputTokens: 2048,
      }),
    ).toEqual({
      name: "Helper",
      description: "",
      systemPrompt: "Be brief.",
      provider: "anthropic",
      model: "claude-sonnet-4-5",
      temperature: "0.5",
      topP: "",
      maxOutputTokens: "2048",
    });
  });
});
```

Run: `task test -- --project unit "src/app/(signed-in)/w/[workspaceId]/agents"` → FAIL (module missing).

- [ ] **Step 3: Form values** — `src/app/(signed-in)/w/[workspaceId]/agents/form-values.ts`:

```ts
// Shared by the agent pages, their server actions and the client form. No secrets: the form
// never receives a provider key, only provider ids, names and model lists.
import type { ModelOption, ProviderId } from "@/lib/providers";

const FIELDS = [
  "name",
  "description",
  "systemPrompt",
  "provider",
  "model",
  "temperature",
  "topP",
  "maxOutputTokens",
] as const;

/** Everything the form submits, as typed (numbers too): rendered back after a failed save. */
export type AgentFormValues = Record<(typeof FIELDS)[number], string>;
export type AgentFormState = { error: string | null; values: AgentFormValues };
/** A provider the form offers; `models` is null when its list couldn't be loaded. */
export type ProviderChoice = { id: ProviderId; name: string; models: ModelOption[] | null };

export const EMPTY_AGENT_VALUES: AgentFormValues = {
  name: "",
  description: "",
  systemPrompt: "",
  provider: "",
  model: "",
  temperature: "",
  topP: "",
  maxOutputTokens: "",
};

/** The agent fields of a submission; anything else in the form data is ignored. */
export function readAgentForm(formData: unknown): AgentFormValues {
  const values = { ...EMPTY_AGENT_VALUES };
  if (!(formData instanceof FormData)) return values;
  for (const name of FIELDS) values[name] = String(formData.get(name) ?? "");
  return values;
}

const numberText = (value: number | null) => (value === null ? "" : String(value));

export function agentToValues(agent: {
  name: string;
  description: string;
  systemPrompt: string;
  provider: ProviderId;
  model: string;
  temperature: number | null;
  topP: number | null;
  maxOutputTokens: number | null;
}): AgentFormValues {
  return {
    name: agent.name,
    description: agent.description,
    systemPrompt: agent.systemPrompt,
    provider: agent.provider,
    model: agent.model,
    temperature: numberText(agent.temperature),
    topP: numberText(agent.topP),
    maxOutputTokens: numberText(agent.maxOutputTokens),
  };
}
```

Run the unit test again → PASS (3 tests).

- [ ] **Step 4: Provider choices and actions** — `src/app/(signed-in)/w/[workspaceId]/agents/provider-choices.ts`:

```ts
import "server-only";
import { PROVIDER_NAMES } from "@/lib/providers";
import { listProviderStatus } from "@/server/providers/keys";
import { listModelsForWorkspace } from "@/server/providers/models";
import type { ProviderChoice } from "./form-values";

/**
 * Configured, readable providers with their live model lists, loaded in parallel. Contains no
 * key (the agent form is a client component).
 */
export async function loadProviderChoices(
  actorId: string,
  workspaceId: string,
): Promise<ProviderChoice[]> {
  const statuses = await listProviderStatus(actorId, workspaceId);
  const usable = statuses.filter((status) => status.configured && status.readable);
  return Promise.all(
    usable.map(async ({ provider }) => {
      const result = await listModelsForWorkspace(workspaceId, provider);
      return {
        id: provider,
        name: PROVIDER_NAMES[provider],
        models: result.status === "ok" ? result.models : null,
      };
    }),
  );
}
```

`src/app/(signed-in)/w/[workspaceId]/agents/actions.ts`:

```ts
"use server";

import { refresh } from "next/cache";
import { redirect } from "next/navigation";
import { runWorkspaceAction } from "@/app/(signed-in)/workspaces/run-action";
import { createAgent, deleteAgent, updateAgent } from "@/server/agents/agents";
import { requireUser } from "@/server/auth/session";
import { WorkspaceError } from "@/server/workspaces/errors";
import { type AgentFormState, readAgentForm } from "./form-values";

// workspaceId and agentId are bound in the page but still client input: the services check both.
const agentsPath = (workspaceId: string) => `/w/${encodeURIComponent(String(workspaceId))}/agents`;

/**
 * Create (agentId null) or update, for useActionState. A rule violation returns its code with the
 * submitted values, so nothing typed is lost; success and not_found (the agent was deleted
 * meanwhile, or the workspace is gone for this user) go to the agent list.
 */
export async function saveAgentAction(
  workspaceId: string,
  agentId: string | null,
  _previous: AgentFormState,
  formData: FormData,
): Promise<AgentFormState> {
  const user = await requireUser();
  const values = readAgentForm(formData);
  try {
    if (agentId === null) await createAgent(user.id, workspaceId, values);
    else await updateAgent(user.id, workspaceId, agentId, values);
  } catch (error) {
    if (!(error instanceof WorkspaceError)) {
      console.error(`Agent action failed: ${error instanceof Error ? error.name : typeof error}`);
      return { error: "unexpected", values };
    }
    if (error.code === "not_found") redirect(agentsPath(workspaceId));
    return { error: error.code, values };
  }
  refresh();
  redirect(agentsPath(workspaceId));
}

/** An agent that is already gone (deleted in another tab) counts as deleted. */
export async function deleteAgentAction(workspaceId: string, agentId: string): Promise<never> {
  const user = await requireUser();
  const list = agentsPath(workspaceId);
  await runWorkspaceAction(`${list}/${encodeURIComponent(String(agentId))}`, async () => {
    try {
      await deleteAgent(user.id, workspaceId, agentId);
    } catch (error) {
      if (!(error instanceof WorkspaceError && error.code === "not_found")) throw error;
    }
  });
  redirect(list);
}
```

- [ ] **Step 5: The agent form** — `src/app/(signed-in)/w/[workspaceId]/agents/agent-form.tsx`:

```tsx
"use client";

import { ChevronDown } from "lucide-react";
import { type ReactNode, useActionState, useState } from "react";
import { workspaceErrorMessage } from "@/app/(signed-in)/workspaces/error-messages";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { PROVIDER_NAMES, type ProviderId } from "@/lib/providers";
import type { AgentFormState, AgentFormValues, ProviderChoice } from "./form-values";

type Option = { value: string; label: string };
/** The stored provider and model of the agent being edited (null for a new agent). */
type Saved = { provider: ProviderId; model: string } | null;

type Props = {
  action: (state: AgentFormState, formData: FormData) => Promise<AgentFormState>;
  initial: AgentFormValues;
  providers: ProviderChoice[];
  saved: Saved;
  submitLabel: string;
};

/** Configured providers, plus the saved one if it isn't (so other fields stay editable, A3). */
function providerOptions(providers: ProviderChoice[], saved: Saved): Option[] {
  const options = providers.map((p) => ({ value: p.id, label: p.name }));
  if (saved && !providers.some((p) => p.id === saved.provider)) {
    options.push({
      value: saved.provider,
      label: `${PROVIDER_NAMES[saved.provider]} (not configured)`,
    });
  }
  return options;
}

/** The provider's live list, plus the saved model if the list lacks it or couldn't load. */
function modelOptions(choice: ProviderChoice | undefined, saved: Saved, provider: string): Option[] {
  const options = (choice?.models ?? []).map((m) => ({ value: m.id, label: m.label }));
  if (saved && provider === saved.provider && !options.some((o) => o.value === saved.model)) {
    options.unshift({ value: saved.model, label: `${saved.model} (current)` });
  }
  return options;
}

/**
 * The only client form (maintainer decision): useActionState keeps everything typed when a save
 * fails. Uncontrolled fields render the returned values as defaultValue, because React resets
 * uncontrolled forms after an action; the two selects are controlled and submit through hidden
 * inputs.
 */
export function AgentForm({ action, initial, providers, saved, submitLabel }: Props) {
  const [state, formAction, pending] = useActionState(action, {
    error: null,
    // A new agent starts with the first usable provider.
    values: { ...initial, provider: initial.provider || (providers[0]?.id ?? "") },
  });
  const [shown, setShown] = useState(state.values);
  const [provider, setProvider] = useState(state.values.provider);
  const [model, setModel] = useState(state.values.model);
  // Adjusting state while rendering: a returned state brings back the submitted choices.
  if (state.values !== shown) {
    setShown(state.values);
    setProvider(state.values.provider);
    setModel(state.values.model);
  }

  const values = state.values;
  const error = workspaceErrorMessage(state.error);
  const providerItems = providerOptions(providers, saved);
  const choice = providers.find((p) => p.id === provider);
  const modelItems = modelOptions(choice, saved, provider);
  const advancedOpen =
    values.temperature !== "" || values.topP !== "" || values.maxOutputTokens !== "";

  return (
    <form action={formAction} className="flex flex-col gap-5">
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      <Field id="agent-name" label="Name">
        <Input defaultValue={values.name} id="agent-name" maxLength={80} name="name" required />
      </Field>
      <Field id="agent-description" label="Description">
        <Input
          defaultValue={values.description}
          id="agent-description"
          maxLength={500}
          name="description"
        />
      </Field>
      <Field id="agent-system-prompt" label="System prompt">
        <Textarea
          className="min-h-40"
          defaultValue={values.systemPrompt}
          id="agent-system-prompt"
          maxLength={20000}
          name="systemPrompt"
        />
      </Field>
      <div className="grid gap-5 sm:grid-cols-2">
        <div className="flex flex-col gap-2">
          <span className="font-medium text-sm" id="agent-provider-label">
            Provider
          </span>
          <Select
            items={providerItems}
            onValueChange={(next) => {
              const value = typeof next === "string" ? next : "";
              setProvider(value);
              setModel(saved && value === saved.provider ? saved.model : "");
            }}
            value={provider || null}
          >
            <SelectTrigger aria-labelledby="agent-provider-label" className="w-full">
              <SelectValue placeholder="Choose a provider" />
            </SelectTrigger>
            <SelectContent>
              {providerItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <input name="provider" type="hidden" value={provider} />
        </div>
        <div className="flex flex-col gap-2">
          <span className="font-medium text-sm" id="agent-model-label">
            Model
          </span>
          <Select
            disabled={modelItems.length === 0}
            items={modelItems}
            onValueChange={(next) => setModel(typeof next === "string" ? next : "")}
            value={model || null}
          >
            <SelectTrigger aria-labelledby="agent-model-label" className="w-full">
              <SelectValue placeholder="Choose a model" />
            </SelectTrigger>
            <SelectContent>
              {modelItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <input name="model" type="hidden" value={model} />
          {choice && choice.models === null ? (
            <p className="text-muted-foreground text-xs">Models couldn't be loaded. Try again later.</p>
          ) : null}
        </div>
      </div>
      <Collapsible defaultOpen={advancedOpen}>
        <CollapsibleTrigger
          render={<Button className="w-fit" size="sm" type="button" variant="ghost" />}
        >
          Advanced
          <ChevronDown aria-hidden="true" />
        </CollapsibleTrigger>
        {/* keepMounted: the fields are submitted while the section is closed, too. */}
        <CollapsibleContent className="mt-3 grid gap-4 sm:grid-cols-3" keepMounted>
          <Field hint="0 to 2" id="agent-temperature" label="Temperature">
            <Input
              defaultValue={values.temperature}
              id="agent-temperature"
              max={2}
              min={0}
              name="temperature"
              step="any"
              type="number"
            />
          </Field>
          <Field hint="Above 0, up to 1" id="agent-top-p" label="Top P">
            <Input
              defaultValue={values.topP}
              id="agent-top-p"
              max={1}
              min={0}
              name="topP"
              step="any"
              type="number"
            />
          </Field>
          <Field hint="1 to 1,000,000" id="agent-max-output-tokens" label="Max output tokens">
            <Input
              defaultValue={values.maxOutputTokens}
              id="agent-max-output-tokens"
              max={1_000_000}
              min={1}
              name="maxOutputTokens"
              step={1}
              type="number"
            />
          </Field>
          <p className="text-muted-foreground text-xs sm:col-span-3">
            Empty fields use the provider's default. Values are not checked against what a model
            accepts.
          </p>
        </CollapsibleContent>
      </Collapsible>
      <Button className="w-fit" disabled={pending} type="submit">
        {pending ? "Saving…" : submitLabel}
      </Button>
    </form>
  );
}

function Field({
  id,
  label,
  hint,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint ? <p className="text-muted-foreground text-xs">{hint}</p> : null}
    </div>
  );
}
```

`src/app/(signed-in)/w/[workspaceId]/agents/delete-agent-button.tsx`:

```tsx
"use client";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";

/** "Delete agent" with a confirmation dialog; `action` is the bound server action. */
export function DeleteAgentButton({ action, name }: { action: () => Promise<void>; name: string }) {
  return (
    <AlertDialog>
      <AlertDialogTrigger render={<Button variant="destructive" />}>Delete agent</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {name}?</AlertDialogTitle>
          <AlertDialogDescription>
            The agent is deleted for everyone in this workspace. This can't be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <form action={action}>
            <AlertDialogAction type="submit" variant="destructive">
              Delete
            </AlertDialogAction>
          </form>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
```

- [ ] **Step 6: The pages** — the three `loading.tsx` files (`agents/loading.tsx`, `agents/new/loading.tsx`, `agents/[agentId]/loading.tsx`) each contain only:

```tsx
export { default } from "@/components/page-loading";
```

`src/app/(signed-in)/w/[workspaceId]/agents/page.tsx`:

```tsx
import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { PROVIDER_NAMES, type ProviderId } from "@/lib/providers";
import { listAgents } from "@/server/agents/agents";
import { AGENT_STATUS_LABELS, agentStatus } from "@/server/agents/status";
import { listProviderStatus } from "@/server/providers/keys";
import { requireWorkspaceMember } from "@/server/workspaces/access";

export const metadata: Metadata = { title: "Agents · Agenty" };

export default async function AgentsPage({ params }: PageProps<"/w/[workspaceId]/agents">) {
  const { workspaceId } = await params;
  const access = await requireWorkspaceMember(workspaceId);
  const id = access.workspace.id;
  const isAdmin = access.role === "admin";
  const [agents, providers] = await Promise.all([
    listAgents(access.user.id, id),
    listProviderStatus(access.user.id, id),
  ]);
  const hasProvider = providers.some((p) => p.configured && p.readable);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <h2 className="font-semibold text-xl">Agents</h2>
        {isAdmin ? (
          <Link className={buttonVariants()} href={`/w/${id}/agents/new`}>
            New agent
          </Link>
        ) : null}
      </div>
      {agents.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>No agents yet</CardTitle>
            <CardDescription>
              {hasProvider ? (
                isAdmin ? (
                  "Create the first one with New agent."
                ) : (
                  "Admins create the agents of this workspace."
                )
              ) : (
                <>
                  Agents need a provider key first.{" "}
                  <Link className="underline underline-offset-4" href={`/w/${id}/providers`}>
                    Configure a provider
                  </Link>
                </>
              )}
            </CardDescription>
          </CardHeader>
        </Card>
      ) : (
        <Table aria-label="Agents">
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Model</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map((agent) => (
              <TableRow key={agent.id}>
                <TableCell className="font-medium">
                  <Link className="hover:underline" href={`/w/${id}/agents/${agent.id}`}>
                    {agent.name}
                  </Link>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {PROVIDER_NAMES[agent.provider]} · {agent.model}
                </TableCell>
                <TableCell>
                  <Suspense fallback={<span className="text-muted-foreground">Checking…</span>}>
                    <AgentStatusCell model={agent.model} provider={agent.provider} workspaceId={id} />
                  </Suspense>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

/** Streams in on its own: a slow or failing provider never holds back the table. */
async function AgentStatusCell({
  workspaceId,
  provider,
  model,
}: {
  workspaceId: string;
  provider: ProviderId;
  model: string;
}) {
  const status = await agentStatus(workspaceId, provider, model);
  return (
    <span className={status === "ready" ? undefined : "text-muted-foreground"}>
      {AGENT_STATUS_LABELS[status]}
    </span>
  );
}
```

`src/app/(signed-in)/w/[workspaceId]/agents/new/page.tsx`:

```tsx
import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { Card, CardContent } from "@/components/ui/card";
import { requireWorkspaceMember } from "@/server/workspaces/access";
import { saveAgentAction } from "../actions";
import { AgentForm } from "../agent-form";
import { EMPTY_AGENT_VALUES } from "../form-values";
import { loadProviderChoices } from "../provider-choices";

export const metadata: Metadata = { title: "New agent · Agenty" };

export default async function NewAgentPage({ params }: PageProps<"/w/[workspaceId]/agents/new">) {
  const { workspaceId } = await params;
  const access = await requireWorkspaceMember(workspaceId);
  const id = access.workspace.id;
  // Members can't create agents; the list is theirs to see (the workspace exists for them).
  if (access.role !== "admin") redirect(`/w/${id}/agents`);
  const providers = await loadProviderChoices(access.user.id, id);

  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-semibold text-xl">New agent</h2>
      {providers.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No provider has a key yet.{" "}
          <Link className="underline underline-offset-4" href={`/w/${id}/providers`}>
            Configure a provider
          </Link>{" "}
          first.
        </p>
      ) : null}
      <Card>
        <CardContent>
          <AgentForm
            action={saveAgentAction.bind(null, id, null)}
            initial={EMPTY_AGENT_VALUES}
            providers={providers}
            saved={null}
            submitLabel="Create agent"
          />
        </CardContent>
      </Card>
    </div>
  );
}
```

`src/app/(signed-in)/w/[workspaceId]/agents/[agentId]/page.tsx`:

```tsx
import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { workspaceErrorMessage } from "@/app/(signed-in)/workspaces/error-messages";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { PROVIDER_NAMES } from "@/lib/providers";
import { type Agent, getAgent } from "@/server/agents/agents";
import { requireWorkspaceMember } from "@/server/workspaces/access";
import { WorkspaceError } from "@/server/workspaces/errors";
import { deleteAgentAction, saveAgentAction } from "../actions";
import { AgentForm } from "../agent-form";
import { DeleteAgentButton } from "../delete-agent-button";
import { agentToValues } from "../form-values";
import { loadProviderChoices } from "../provider-choices";

export const metadata: Metadata = { title: "Agent · Agenty" };

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);

export default async function AgentPage({
  params,
  searchParams,
}: PageProps<"/w/[workspaceId]/agents/[agentId]">) {
  const { workspaceId, agentId } = await params;
  const access = await requireWorkspaceMember(workspaceId);
  const id = access.workspace.id;
  let agent: Agent;
  try {
    agent = await getAgent(access.user.id, id, agentId);
  } catch (error) {
    if (error instanceof WorkspaceError && error.code === "not_found") notFound();
    throw error;
  }

  // A1: members see every setting read-only.
  if (access.role !== "admin") {
    return (
      <div className="flex flex-col gap-6">
        <h2 className="font-semibold text-xl">{agent.name}</h2>
        <AgentDetails agent={agent} />
      </div>
    );
  }

  const [query, providers] = await Promise.all([
    searchParams,
    loadProviderChoices(access.user.id, id),
  ]);
  const error = workspaceErrorMessage(first(query.error));
  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-semibold text-xl">{agent.name}</h2>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      <Card>
        <CardContent>
          <AgentForm
            action={saveAgentAction.bind(null, id, agent.id)}
            initial={agentToValues(agent)}
            providers={providers}
            saved={{ provider: agent.provider, model: agent.model }}
            submitLabel="Save changes"
          />
        </CardContent>
      </Card>
      <Card className="ring-destructive/40">
        <CardHeader>
          <CardTitle className="text-destructive">Danger zone</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-muted-foreground text-sm">
            Deletes the agent for everyone in this workspace. This can't be undone.
          </p>
          <DeleteAgentButton action={deleteAgentAction.bind(null, id, agent.id)} name={agent.name} />
        </CardContent>
      </Card>
    </div>
  );
}

const parameterText = (value: number | null) => (value === null ? "Provider default" : String(value));

function AgentDetails({ agent }: { agent: Agent }) {
  const rows: [string, string][] = [
    ["Description", agent.description || "None"],
    ["Provider", PROVIDER_NAMES[agent.provider]],
    ["Model", agent.model],
    ["Temperature", parameterText(agent.temperature)],
    ["Top P", parameterText(agent.topP)],
    ["Max output tokens", parameterText(agent.maxOutputTokens)],
  ];
  return (
    <Card>
      <CardContent className="flex flex-col gap-6">
        <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[12rem_1fr]">
          {rows.map(([term, value]) => (
            <div className="contents" key={term}>
              <dt className="text-muted-foreground">{term}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
        <div className="flex flex-col gap-2">
          <h3 className="font-medium text-sm">System prompt</h3>
          <p className="whitespace-pre-wrap rounded-lg border p-3 text-sm">
            {agent.systemPrompt || "None"}
          </p>
        </div>
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 7: E2E support** — `tests/e2e/support/agents.ts`:

```ts
import { expect, type Page } from "@playwright/test";

export const agentsTable = (page: Page) =>
  page.getByRole("table", { name: "Agents", exact: true });

/** Opens the select labelled `label` (retrying until the form is hydrated) and picks `option`. */
export async function chooseOption(page: Page, label: string, option: string) {
  const trigger = page.getByLabel(label, { exact: true });
  const item = page.getByRole("option", { name: option, exact: true });
  await expect(async () => {
    await trigger.click();
    await expect(item).toBeVisible({ timeout: 1_000 });
  }).toPass();
  await item.click();
  await expect(trigger).toContainText(option);
}

/** Creates an agent through the form (admins only) and waits for it in the agent list. */
export async function createAgentViaForm(
  page: Page,
  workspacePath: string,
  agent: { name: string; provider: string; model: string; systemPrompt?: string },
) {
  await page.goto(`${workspacePath}/agents/new`);
  await page.getByLabel("Name", { exact: true }).fill(agent.name);
  if (agent.systemPrompt) {
    await page.getByLabel("System prompt", { exact: true }).fill(agent.systemPrompt);
  }
  await chooseOption(page, "Provider", agent.provider);
  await chooseOption(page, "Model", agent.model);
  await page.getByRole("button", { name: "Create agent", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${workspacePath}/agents$`));
  await expect(agentsTable(page).getByRole("link", { name: agent.name, exact: true })).toBeVisible();
}

/** Deletes the agent whose page is open, through the confirmation dialog. */
export async function deleteAgentViaDialog(page: Page) {
  const open = page.getByRole("button", { name: "Delete agent", exact: true });
  const confirm = page.getByRole("alertdialog").getByRole("button", { name: "Delete", exact: true });
  await expect(async () => {
    await open.click();
    await expect(confirm).toBeVisible({ timeout: 1_000 });
  }).toPass();
  await confirm.click();
  await expect(page).toHaveURL(/\/agents$/);
}
```

- [ ] **Step 8: The E2E test** — `tests/e2e/agents.spec.ts`:

```ts
import { expect, type Page, test } from "@playwright/test";
import {
  agentsTable,
  chooseOption,
  createAgentViaForm,
  deleteAgentViaDialog,
} from "./support/agents";
import { uniqueIdpUser } from "./support/mock-idp";
import { collectResponseBodies, providerRow, saveProviderKey } from "./support/providers";
import {
  addMember,
  alertWith,
  browserSessions,
  createWorkspace,
  workspaceNav,
} from "./support/sessions";

const sessions = browserSessions();
test.afterEach(() => sessions.closeAll());

const subheading = (page: Page, name: string) =>
  page.getByRole("heading", { level: 2, name, exact: true });
const agentsUrl = (workspacePath: string) => new RegExp(`${workspacePath}/agents$`);

test("admins create, edit and delete agents; members see them read-only", async ({ browser }) => {
  const ada = uniqueIdpUser("agada", "Ada Agents");
  const bob = uniqueIdpUser("agbob", "Bob Agents");
  const bobPage = await sessions.signedIn(browser, bob);
  const adaPage = await sessions.signedIn(browser, ada);
  const team = `Agents ${ada.sub}`;
  const teamPath = await createWorkspace(adaPage, team);
  await addMember(adaPage, bobPage, teamPath, bob, team);
  await saveProviderKey(adaPage, teamPath, "OpenAI", `good-agents-${ada.sub}`);

  // Ada creates an agent with a model from the provider's list and advanced parameters.
  await workspaceNav(adaPage).getByRole("link", { name: "Agents", exact: true }).click();
  await expect(adaPage.getByText("No agents yet", { exact: true })).toBeVisible();
  await adaPage.getByRole("link", { name: "New agent", exact: true }).click();
  await expect(subheading(adaPage, "New agent")).toBeVisible();
  await adaPage.getByLabel("Name", { exact: true }).fill("Helper");
  await adaPage.getByLabel("Description", { exact: true }).fill("Answers questions");
  await adaPage.getByLabel("System prompt", { exact: true }).fill("You are helpful.");
  await chooseOption(adaPage, "Provider", "OpenAI");
  // Only chat models are offered: the fake provider also lists an embedding model.
  const model = adaPage.getByLabel("Model", { exact: true });
  await model.click();
  await expect(adaPage.getByRole("option", { name: "gpt-5-mini", exact: true })).toBeVisible();
  await expect(adaPage.getByRole("option", { name: "text-embedding-3-small" })).toHaveCount(0);
  await adaPage.getByRole("option", { name: "gpt-5", exact: true }).click();
  await expect(model).toContainText("gpt-5");
  await adaPage.getByRole("button", { name: "Advanced", exact: true }).click();
  await adaPage.getByLabel("Temperature", { exact: true }).fill("0.7");
  await adaPage.getByLabel("Max output tokens", { exact: true }).fill("1000");
  await adaPage.getByRole("button", { name: "Create agent", exact: true }).click();
  await expect(adaPage).toHaveURL(agentsUrl(teamPath));
  const helperRow = agentsTable(adaPage).getByRole("row").filter({ hasText: "Helper" });
  await expect(helperRow).toContainText("OpenAI · gpt-5");
  await expect(helperRow).toContainText("Ready");

  // The same name in other case is refused, and everything typed stays in the form.
  await adaPage.getByRole("link", { name: "New agent", exact: true }).click();
  await adaPage.getByLabel("Name", { exact: true }).fill("helper");
  await adaPage.getByLabel("System prompt", { exact: true }).fill("A second prompt");
  await chooseOption(adaPage, "Provider", "OpenAI");
  await chooseOption(adaPage, "Model", "gpt-5-mini");
  await adaPage.getByRole("button", { name: "Create agent", exact: true }).click();
  await expect(
    alertWith(adaPage, "An agent with this name already exists in this workspace."),
  ).toBeVisible();
  await expect(adaPage.getByLabel("Name", { exact: true })).toHaveValue("helper");
  await expect(adaPage.getByLabel("System prompt", { exact: true })).toHaveValue("A second prompt");
  await expect(adaPage.getByLabel("Model", { exact: true })).toContainText("gpt-5-mini");

  // Editing: Advanced starts open because parameters are set.
  await workspaceNav(adaPage).getByRole("link", { name: "Agents", exact: true }).click();
  await agentsTable(adaPage).getByRole("link", { name: "Helper", exact: true }).click();
  await expect(subheading(adaPage, "Helper")).toBeVisible();
  const helperUrl = adaPage.url();
  await expect(adaPage.getByLabel("Temperature", { exact: true })).toHaveValue("0.7");
  await adaPage.getByLabel("Description", { exact: true }).fill("Answers every question");
  await adaPage.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(adaPage).toHaveURL(agentsUrl(teamPath));
  await adaPage.goto(helperUrl);
  await expect(adaPage.getByLabel("Description", { exact: true })).toHaveValue(
    "Answers every question",
  );

  // Bob sees the agent and all its settings, read-only.
  await bobPage.goto(`${teamPath}/agents`);
  await expect(bobPage.getByRole("link", { name: "New agent", exact: true })).toHaveCount(0);
  await agentsTable(bobPage).getByRole("link", { name: "Helper", exact: true }).click();
  await expect(subheading(bobPage, "Helper")).toBeVisible();
  await expect(bobPage.getByRole("main").getByText("You are helpful.", { exact: true })).toBeVisible();
  await expect(bobPage.getByRole("main").getByText("Answers every question", { exact: true })).toBeVisible();
  await expect(bobPage.getByRole("main").getByRole("textbox")).toHaveCount(0);
  await expect(bobPage.getByRole("button", { name: "Save changes", exact: true })).toHaveCount(0);
  await expect(bobPage.getByRole("button", { name: "Delete agent", exact: true })).toHaveCount(0);
  await bobPage.goto(`${teamPath}/agents/new`);
  await expect(bobPage).toHaveURL(agentsUrl(teamPath));

  // Removing the key keeps the agent, which then shows that the provider is not configured.
  await adaPage.goto(`${teamPath}/providers`);
  await providerRow(adaPage, "OpenAI").getByRole("button", { name: "Remove", exact: true }).click();
  await expect(providerRow(adaPage, "OpenAI")).toContainText("Not configured");
  await workspaceNav(adaPage).getByRole("link", { name: "Agents", exact: true }).click();
  await expect(
    agentsTable(adaPage).getByRole("row").filter({ hasText: "Helper" }),
  ).toContainText("Provider not configured");

  // Deleting, through the confirmation dialog.
  await adaPage.goto(helperUrl);
  await deleteAgentViaDialog(adaPage);
  await expect(adaPage.getByText("No agents yet", { exact: true })).toBeVisible();
  await expect(adaPage.getByRole("link", { name: "Configure a provider", exact: true })).toBeVisible();
});

test("the personal workspace has working Agents and Providers tabs", async ({ browser }) => {
  const page = await sessions.signedIn(browser, uniqueIdpUser("agpers", "Pat Agents"));
  await workspaceNav(page).getByRole("link", { name: "Agents", exact: true }).click();
  await expect(subheading(page, "Agents")).toBeVisible();
  await expect(page.getByRole("link", { name: "New agent", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Configure a provider", exact: true }).click();
  await expect(subheading(page, "Providers")).toBeVisible();
  await expect(page.getByLabel("OpenAI API key", { exact: true })).toBeVisible();
});

test("saving an agent that was deleted in another tab goes back to the agent list", async ({
  browser,
}) => {
  const user = uniqueIdpUser("agstale", "Sam Stale");
  const page = await sessions.signedIn(browser, user);
  const home = new URL(page.url()).pathname;
  await saveProviderKey(page, home, "OpenAI", `good-stale-${user.sub}`);
  await createAgentViaForm(page, home, { name: "Stale", provider: "OpenAI", model: "gpt-5" });
  await agentsTable(page).getByRole("link", { name: "Stale", exact: true }).click();
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Stale");

  const other = await page.context().newPage();
  await other.goto(page.url());
  await deleteAgentViaDialog(other);

  await page.getByLabel("Description", { exact: true }).fill("Too late");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(page).toHaveURL(agentsUrl(home));
  await expect(page.getByText("No agents yet", { exact: true })).toBeVisible();
});

test("a key removed while the form is open gives a fixed message and keeps what was typed", async ({
  browser,
}) => {
  const user = uniqueIdpUser("agkeep", "Kim Keep");
  const page = await sessions.signedIn(browser, user);
  const home = new URL(page.url()).pathname;
  await saveProviderKey(page, home, "OpenAI", `good-keep-${user.sub}`);
  await page.goto(`${home}/agents/new`);
  await page.getByLabel("Name", { exact: true }).fill("Kept");
  await page.getByLabel("System prompt", { exact: true }).fill("Keep this prompt");
  await chooseOption(page, "Provider", "OpenAI");
  await chooseOption(page, "Model", "gpt-5");

  const other = await page.context().newPage();
  await other.goto(`${home}/providers`);
  await providerRow(other, "OpenAI").getByRole("button", { name: "Remove", exact: true }).click();
  await expect(providerRow(other, "OpenAI")).toContainText("Not configured");

  await page.getByRole("button", { name: "Create agent", exact: true }).click();
  await expect(alertWith(page, "Set an API key for this provider first.")).toBeVisible();
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Kept");
  await expect(page.getByLabel("System prompt", { exact: true })).toHaveValue("Keep this prompt");
  await expect(page.getByLabel("Model", { exact: true })).toContainText("gpt-5");
});

test("the key never reaches the browser with the agent pages", async ({ browser }) => {
  const user = uniqueIdpUser("agleak", "Lou Leak");
  const page = await sessions.signedIn(browser, user);
  const home = new URL(page.url()).pathname;
  const key = `good-leak-${user.sub}-5678`;
  const bodies = collectResponseBodies(page);

  await saveProviderKey(page, home, "OpenAI", key);
  await createAgentViaForm(page, home, { name: "Leak check", provider: "OpenAI", model: "gpt-5" });
  // A client navigation (RSC payload) and a full load of the agent form.
  await agentsTable(page).getByRole("link", { name: "Leak check", exact: true }).click();
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Leak check");
  await page.reload();
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Leak check");

  const all = await bodies();
  expect(all.length).toBeGreaterThan(5);
  for (const body of all) expect(body).not.toContain(key);
  expect(await page.content()).not.toContain(key);
});
```

- [ ] **Step 9: Instant navigation entries** — in `tests/e2e/instant.spec.ts`, add the imports and helpers at the top:

```ts
import { agentsTable, createAgentViaForm } from "./support/agents";
import { saveProviderKey } from "./support/providers";
```

```ts
const subheading = (page: Page, name: string) =>
  page.getByRole("heading", { level: 2, name, exact: true });
```

add to the initial-load list:

```ts
    ["team/agents", "Agents · Agenty"],
    ["team/agents/new", "New agent · Agenty"],
```

and to `describe("client navigations commit at once and keep the sidebar", …)`:

```ts
  test("into /w/[workspaceId]/agents", async ({ page }) => {
    await page.goto(teamUrl);
    await expect(homeCard(page)).toBeVisible();
    await instant(page, async () => {
      await workspaceNav(page).getByRole("link", { name: "Agents", exact: true }).click();
      await page.waitForURL((url) => url.pathname === `${teamUrl}/agents`, {
        timeout: navigationTimeout,
      });
      await expect(homeCard(page)).toBeHidden();
      await expect(sidebar(page)).toBeVisible();
      await expect(heading(page, team)).toBeVisible();
      await expect(subheading(page, "Agents")).toBeHidden();
    });
    await expect(subheading(page, "Agents")).toBeVisible();
  });

  test("into /w/[workspaceId]/agents/new (from the agent list)", async ({ page }) => {
    await page.goto(`${teamUrl}/agents`);
    await expect(subheading(page, "Agents")).toBeVisible();
    await instant(page, async () => {
      await page.getByRole("link", { name: "New agent", exact: true }).click();
      await page.waitForURL((url) => url.pathname === `${teamUrl}/agents/new`, {
        timeout: navigationTimeout,
      });
      await expect(subheading(page, "Agents")).toBeHidden();
      await expect(sidebar(page)).toBeVisible();
      await expect(heading(page, team)).toBeVisible();
      await expect(subheading(page, "New agent")).toBeHidden();
    });
    await expect(subheading(page, "New agent")).toBeVisible();
  });

  test("into /w/[workspaceId]/agents/[agentId], and its initial load", async ({
    page,
    baseURL,
  }) => {
    await saveProviderKey(page, teamUrl, "OpenAI", `good-instant-${Date.now()}-key`);
    await createAgentViaForm(page, teamUrl, { name: "Instant", provider: "OpenAI", model: "gpt-5" });
    await instant(page, async () => {
      await agentsTable(page).getByRole("link", { name: "Instant", exact: true }).click();
      await page.waitForURL((url) => /\/agents\/[0-9a-f-]{36}$/.test(url.pathname), {
        timeout: navigationTimeout,
      });
      await expect(agentsTable(page)).toBeHidden();
      await expect(sidebar(page)).toBeVisible();
      await expect(heading(page, team)).toBeVisible();
      await expect(subheading(page, "Instant")).toBeHidden();
    });
    await expect(subheading(page, "Instant")).toBeVisible();

    const agentUrl = page.url();
    await instant(
      page,
      async () => {
        await page.goto(agentUrl);
        await expect(page).toHaveTitle("Agent · Agenty");
        await expect(sidebar(page)).toBeVisible();
        await expect(page.getByRole("main").getByRole("heading")).toBeHidden();
      },
      { baseURL },
    );
    await expect(subheading(page, "Instant")).toBeVisible();
  });
```

- [ ] **Step 10: Run the tests**

Run: `task test -- --project unit`, then `task build`, then `task test:e2e`
Expected: all pass. Typical failures: `Route "/w/[workspaceId]/agents…": uncached data` → a `loading.tsx` is missing next to a page; a select that never opens → the trigger's label (`aria-labelledby`) doesn't match `getByLabel("Provider")`/`("Model")`.

- [ ] **Step 11: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/components/ui/textarea.tsx src/components/ui/collapsible.tsx src/components/ui/alert-dialog.tsx "src/app/(signed-in)/w/[workspaceId]/agents" tests/e2e/support/agents.ts tests/e2e/agents.spec.ts tests/e2e/instant.spec.ts
git commit -m "feat(agents): agent list, form and detail pages

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Documentation

**Files:**
- Modify: `AGENTS.md`, `README.md`

**Interfaces:**
- Consumes: the final names from Tasks 1–8.

- [ ] **Step 1: AGENTS.md** — make these edits (keep the file's style: short sentences, bullets; leave the `nextjs-agent-rules` block alone):

1. Commands table, after the `task db:studio` row:

```markdown
| `task crypto:rotate` | Re-encrypt stored provider keys with the active `ENCRYPTION_KEYS` key; `-- --check` only counts rows on older keys |
```

2. Architecture tree:
   - `w/[workspaceId]/  workspace home and settings (every page checks membership itself)` becomes `w/[workspaceId]/  workspace home, agents, providers and settings (every page checks membership itself)`.
   - The `src/components/` entry gains `submit-button.tsx (pending label while its form's action runs)`.
   - The `src/lib/` entry gains `providers.ts (provider ids and names, model id rule; shared with client code)`.
   - Under `src/server/`, add:

```
  crypto/           AES-256-GCM (cipher.ts), ENCRYPTION_KEYS parser (keyring.ts), rotation (rotate.ts);
                    these three have no server-only import (scripts/rotate-keys.ts runs them)
  providers/        registry (base URLs), model listing = key verification (listing.ts), keys service
                    (keys.ts), per-process model cache (model-cache.ts, models.ts)
  agents/           agent rules (agents.ts), input validation, availability status (status.ts)
```

   - The `scripts/` entry gains `key rotation (rotate-keys.ts)`.
   - The `tests/support/` entry gains `fake-provider.ts (model-listing endpoints of all three providers; in-process for integration tests, as a process for Playwright via fake-provider-server.ts)` and `workspaces.ts (sharedWorkspace, expectWorkspaceError)`.
   - "Folders `src/server/{agents,tools,workflows,crypto}` are created by the milestones that need them." becomes "Folders `src/server/{tools,workflows}` are created by the milestones that need them."

3. New subsections after `### Workspaces`:

```markdown
### Encryption

`ENCRYPTION_KEYS` (required): comma-separated `id:base64` pairs, id `[a-z0-9]{1,16}`, each key
exactly 32 bytes (`openssl rand -base64 32`). The first key encrypts; the others only decrypt.
`src/server/crypto/crypto.ts` (`encrypt`, `decrypt`, `needsRotation`) uses `getEnv()`'s keyring;
`cipher.ts` takes the keyring explicitly. AES-256-GCM, random 12-byte IV, format
`v1.<keyId>.<iv>.<tag>.<ciphertext>`. The AAD binds a ciphertext to its row
(`provider_key|<workspaceId>|<provider>`). Decryption fails closed with a detail-free
`CryptoError`; log its class only.

Rotation: put a new key first in `ENCRYPTION_KEYS`, restart, run `task crypto:rotate` (as
`DATABASE_URL`; locks one row at a time, never the workspace row; keeps `updated_at`), and remove
the old key once `task crypto:rotate -- --check` reports 0. Removing a key that still has
ciphertexts makes those provider keys unreadable ("Key unreadable, set it again").

### Providers and agents

Spec: `docs/specs/2026-10-10-m3-providers-agents-design.md` (rules P1–P5, A1–A4).

- One API key per provider (`openai`, `anthropic`, `google`) and workspace, personal workspaces
  included. Only admins set, replace and remove keys; members see which providers are configured.
- A key is verified by listing models before it is stored (`listModels`: plain `fetch`, one 10 s
  deadline, no redirects, 5 MB per body, at most 5 000 models and 10 pages, key in a header
  only). Outcomes map to `key_rejected`, `provider_unavailable`, `key_unverified`. Provider
  bodies are never logged (OpenAI echoes part of the key); logs carry provider, status and error
  class.
- The plain key never leaves `src/server/providers` except as that header: `getProviderKey` is the
  only function returning it. The UI shows the last 4 characters to admins only.
- Base URLs come from `src/server/providers/registry.ts`; operators may override them with
  `AGENTY_OPENAI_BASE_URL`, `AGENTY_ANTHROPIC_BASE_URL`, `AGENTY_GOOGLE_BASE_URL` (https; http
  only for localhost). Workspaces never enter URLs (SSRF). E2E points them at the fake provider.
- Model lists are cached per process (10 minutes, failures 1 minute), checked against the key's
  `updated_at`. An agent's status (`ready`, `provider_not_configured`, `key_unreadable`,
  `model_unavailable`, `unknown`) is computed when shown; the agent list streams it per row.
- Only admins create, edit and delete agents; members see every setting read-only. Names are
  unique per workspace ignoring case (unique index; `23505` → `agent_name_taken`). Creating an
  agent or changing its provider/model needs a readable key and a listed model (A3); removing a
  key keeps its agents (P5).
- The agent form is the one client form: `useActionState`, values returned on failure and
  rendered back as `defaultValue`. Every other form redirects with `?error=` as in M2.

### M4 notes (key handling)

- Use `WorkflowAgent` from `@ai-sdk/workflow` (AI SDK 7); `@workflow/ai`'s `DurableAgent` is
  deprecated.
- Never hand the agent a model built with a key (`createOpenAI({ apiKey })(id)`): workflow
  serialization would store the key unencrypted. Use a model wrapper that serializes only
  `(workspaceId, provider, modelId)` and calls `getProviderKey` inside the step; test that no key
  reaches the `workflow` schemas.
- Always pass `apiKey` explicitly (the SDKs fall back to `OPENAI_API_KEY` and similar).
  `openai(id)` uses the Responses API.
- Replace the lazy availability check with a scheduled one.
```

4. Non-negotiable rule 4: "(master key from env)" becomes "(`src/server/crypto`, keys from `ENCRYPTION_KEYS`)".

5. Conventions, "Configuration is read only via `getEnv()`" bullet: append "(`scripts/rotate-keys.ts` parses `ENCRYPTION_KEYS` itself with the same parser)".

- [ ] **Step 2: README.md**

1. In `## Production image`, add `-e ENCRYPTION_KEYS=... \` to the `docker run` example after the OIDC line.
2. After `### Sign-in (OIDC)`, add:

```markdown
### Encryption keys

Provider API keys are stored encrypted. Set `ENCRYPTION_KEYS` to `id:key`, e.g.
`main:$(openssl rand -base64 32)`; ids are 1–16 characters `a-z0-9`. Keep the key safe: without
it, stored provider keys can't be read.

To rotate: put a new key first (`new:...,main:...`) and restart; new keys are encrypted with it.
Then, from a checkout of the same version with `DATABASE_URL` and `ENCRYPTION_KEYS` set, run
`task crypto:rotate`. When `task crypto:rotate -- --check` reports 0, remove the old key and
restart. Provider keys whose encryption key was removed show as "Key unreadable, set it again".

### Model providers

Agenty talks to OpenAI, Anthropic and the Google Gemini API. For a corporate proxy, override
their base URLs (https):

| Variable | Default |
|---|---|
| `AGENTY_OPENAI_BASE_URL` | `https://api.openai.com/v1` |
| `AGENTY_ANTHROPIC_BASE_URL` | `https://api.anthropic.com/v1` |
| `AGENTY_GOOGLE_BASE_URL` | `https://generativelanguage.googleapis.com/v1beta` |
```

3. Replace the last sentence of `### Workspaces` ("Agents, tools and provider keys will belong to workspaces; chats stay private.") with:

```markdown
Under **Providers**, admins store one API key per provider for the workspace; each key is checked
with the provider first and is never shown again (only its last four characters, to admins).
Under **Agents**, admins create agents: a name, description, system prompt, a provider and one of
its models, and optional temperature, top P and max output tokens. Members see agents and their
settings read-only. Chatting with agents follows in the next milestone; chats stay private.
```

4. Troubleshooting, the "Sign-in is unavailable or env errors after updating" bullet: mention `ENCRYPTION_KEYS` as an example of a new key.

- [ ] **Step 3: Check for stale statements**

Run: `grep -n -i "agents arrive\|will belong\|master key\|{agents,tools" AGENTS.md README.md`
Expected: no matches.

- [ ] **Step 4: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add AGENTS.md README.md
git commit -m "docs(m3): providers, agents and encryption in AGENTS.md and README

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
