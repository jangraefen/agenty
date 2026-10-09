import { describe, expect, it } from "vitest";
import { parseEnv } from "./env";

const valid = { DATABASE_URL: "postgres://agenty_app:s3cret@localhost:5432/agenty" };

describe("parseEnv", () => {
  it("accepts a postgres URL and defaults NODE_ENV", () => {
    expect(parseEnv(valid)).toEqual({ ...valid, NODE_ENV: "development" });
  });

  it("accepts the postgresql:// scheme", () => {
    const env = { DATABASE_URL: "postgresql://u:p@db:5432/agenty" };
    expect(parseEnv(env).DATABASE_URL).toBe(env.DATABASE_URL);
  });

  it("rejects a missing DATABASE_URL with a readable message", () => {
    expect(() => parseEnv({})).toThrow(/DATABASE_URL/);
  });

  it("rejects a non-postgres URL", () => {
    expect(() => parseEnv({ DATABASE_URL: "mysql://u:p@db/agenty" })).toThrow(/DATABASE_URL/);
  });

  it("never echoes the value (it contains a password) in the error", () => {
    const secret = "mysql://agenty_app:hunter2-very-secret@db/agenty";
    let message = "";
    try {
      parseEnv({ DATABASE_URL: secret });
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
