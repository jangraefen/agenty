import { describe, expect, it } from "vitest";
import { parseEnv, takeMigrationUrl } from "./env";

const valid = {
  DATABASE_URL: "postgres://agenty_app:s3cret@localhost:5432/agenty",
  BETTER_AUTH_SECRET: "test-secret-0123456789abcdef012345",
  BETTER_AUTH_URL: "http://localhost:3000",
  OIDC_DISCOVERY_URL: "http://localhost:8080/agenty/.well-known/openid-configuration",
  OIDC_CLIENT_ID: "agenty",
  OIDC_CLIENT_SECRET: "client-s3cret",
};

describe("parseEnv", () => {
  it("accepts a postgres URL and defaults NODE_ENV", () => {
    expect(parseEnv(valid)).toEqual({ ...valid, NODE_ENV: "development" });
  });

  it("accepts the postgresql:// scheme", () => {
    const env = { ...valid, DATABASE_URL: "postgresql://u:p@db:5432/agenty" };
    expect(parseEnv(env).DATABASE_URL).toBe(env.DATABASE_URL);
  });

  it("rejects a missing DATABASE_URL with a readable message", () => {
    expect(() => parseEnv({ ...valid, DATABASE_URL: undefined })).toThrow(/DATABASE_URL/);
  });

  it("rejects a non-postgres URL", () => {
    expect(() => parseEnv({ ...valid, DATABASE_URL: "mysql://u:p@db/agenty" })).toThrow(
      /DATABASE_URL/,
    );
  });

  it("never echoes the value (it contains a password) in the error", () => {
    const secret = "mysql://agenty_app:hunter2-very-secret@db/agenty";
    let message = "";
    try {
      parseEnv({ ...valid, DATABASE_URL: secret });
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

describe("parseEnv authentication settings", () => {
  it("accepts the Better Auth and OIDC variables", () => {
    expect(parseEnv(valid)).toMatchObject({
      BETTER_AUTH_URL: valid.BETTER_AUTH_URL,
      OIDC_DISCOVERY_URL: valid.OIDC_DISCOVERY_URL,
      OIDC_CLIENT_ID: "agenty",
    });
  });

  it("rejects a BETTER_AUTH_SECRET under 32 characters without echoing it", () => {
    const short = "too-short-secret";
    let message = "";
    try {
      parseEnv({ ...valid, BETTER_AUTH_SECRET: short });
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).toMatch(/BETTER_AUTH_SECRET/);
    expect(message).not.toContain(short);
  });

  it.each(["BETTER_AUTH_URL", "OIDC_DISCOVERY_URL"])("rejects a non-http(s) %s", (key) => {
    expect(() => parseEnv({ ...valid, [key]: "ftp://localhost/x" })).toThrow(new RegExp(key));
  });

  it.each(["OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET"])("requires a non-empty %s", (key) => {
    expect(() => parseEnv({ ...valid, [key]: "" })).toThrow(new RegExp(key));
    expect(() => parseEnv({ ...valid, [key]: undefined })).toThrow(new RegExp(key));
  });

  it("never echoes an invalid OIDC_CLIENT_SECRET", () => {
    let message = "";
    try {
      parseEnv({ ...valid, OIDC_CLIENT_SECRET: "" });
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).not.toContain(valid.OIDC_CLIENT_SECRET);
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
