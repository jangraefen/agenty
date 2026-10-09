import { describe, expect, it } from "vitest";
import { parseEnv, takeMigrationUrl } from "./env";

const base = {
  DATABASE_URL: "postgres://agenty_app:s3cret@localhost:5432/agenty",
  BETTER_AUTH_SECRET: "x".repeat(32),
  BETTER_AUTH_URL: "http://localhost:3000",
};
const valid = base;

describe("parseEnv", () => {
  it("accepts a postgres URL and defaults NODE_ENV", () => {
    expect(parseEnv(valid)).toMatchObject({ ...valid, NODE_ENV: "development" });
  });

  it("accepts the postgresql:// scheme", () => {
    const env = { ...base, DATABASE_URL: "postgresql://u:p@db:5432/agenty" };
    expect(parseEnv(env).DATABASE_URL).toBe(env.DATABASE_URL);
  });

  it("rejects a missing DATABASE_URL with a readable message", () => {
    expect(() => parseEnv({})).toThrow(/DATABASE_URL/);
  });

  it("rejects a non-postgres URL", () => {
    expect(() => parseEnv({ ...base, DATABASE_URL: "mysql://u:p@db/agenty" })).toThrow(
      /DATABASE_URL/,
    );
  });

  it("never echoes the value (it contains a password) in the error", () => {
    const secret = "mysql://agenty_app:hunter2-very-secret@db/agenty";
    let message = "";
    try {
      parseEnv({ ...base, DATABASE_URL: secret });
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).not.toBe("");
    expect(message).not.toContain("hunter2");
    expect(message).not.toContain(secret);
  });

  it("rejects an unknown NODE_ENV", () => {
    expect(() => parseEnv({ ...valid, NODE_ENV: "staging" })).toThrow(/NODE_ENV/);
  });
});

describe("parseEnv auth settings", () => {
  it("accepts the auth settings, trusted origins default to none", () => {
    expect(parseEnv(base)).toMatchObject({ BETTER_AUTH_TRUSTED_ORIGINS: [] });
  });

  it("parses comma-separated trusted origins", () => {
    expect(
      parseEnv({
        ...base,
        BETTER_AUTH_TRUSTED_ORIGINS: "http://localhost:8080, https://idp.internal",
      }),
    ).toMatchObject({
      BETTER_AUTH_TRUSTED_ORIGINS: ["http://localhost:8080", "https://idp.internal"],
    });
  });

  it("rejects a trusted origin with a path", () => {
    expect(() =>
      parseEnv({ ...base, BETTER_AUTH_TRUSTED_ORIGINS: "http://localhost:8080/corp" }),
    ).toThrow(/BETTER_AUTH_TRUSTED_ORIGINS/);
  });

  it("rejects a short BETTER_AUTH_SECRET without echoing it", () => {
    const secret = "short-secret-value";
    expect(() => parseEnv({ ...base, BETTER_AUTH_SECRET: secret })).toThrow(/BETTER_AUTH_SECRET/);
    expect(() => parseEnv({ ...base, BETTER_AUTH_SECRET: secret })).not.toThrow(new RegExp(secret));
  });

  it("rejects a non-http BETTER_AUTH_URL", () => {
    expect(() => parseEnv({ ...base, BETTER_AUTH_URL: "ftp://x" })).toThrow(/BETTER_AUTH_URL/);
  });
});

describe("takeMigrationUrl", () => {
  const url = "postgres://agenty_owner:owner-s3cret@localhost:5432/agenty";

  it("returns the URL and removes it from the environment", () => {
    const env: Record<string, string | undefined> = { DATABASE_MIGRATION_URL: url };
    expect(takeMigrationUrl(env)).toBe(url);
    expect("DATABASE_MIGRATION_URL" in env).toBe(false);
  });

  it("rejects a missing URL with a readable message", () => {
    expect(() => takeMigrationUrl({})).toThrow(/DATABASE_MIGRATION_URL/);
  });

  it("removes an invalid URL too, without echoing it", () => {
    const secret = "mysql://agenty_owner:hunter2-owner@db/agenty";
    const env: Record<string, string | undefined> = { DATABASE_MIGRATION_URL: secret };
    let message = "";
    try {
      takeMigrationUrl(env);
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).toMatch(/DATABASE_MIGRATION_URL/);
    expect(message).not.toContain("hunter2");
    expect("DATABASE_MIGRATION_URL" in env).toBe(false);
  });
});
