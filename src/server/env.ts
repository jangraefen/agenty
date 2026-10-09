import "server-only";
import { z } from "zod";

const postgresUrl = z.url({ protocol: /^postgres(ql)?$/, error: "must be a postgres:// URL" });

const envSchema = z.object({
  DATABASE_URL: postgresUrl,
  NODE_ENV: z.enum(["development", "test", "production"]).default("development"),
});

export type Env = z.infer<typeof envSchema>;

/** Parses configuration. The error lists field paths and messages only, never values. */
export function parseEnv(source: Record<string, string | undefined>): Env {
  const result = envSchema.safeParse(source);
  if (!result.success) {
    const problems = result.error.issues
      .map((issue) => `  - ${issue.path.join(".")}: ${issue.message}`)
      .join("\n");
    throw new Error(`Invalid environment configuration:\n${problems}`);
  }
  return result.data;
}

let cached: Env | undefined;

/** Parsed lazily so `next build` never needs runtime configuration. */
export function getEnv(): Env {
  cached ??= parseEnv(process.env);
  return cached;
}

/**
 * Reads DATABASE_MIGRATION_URL (owner credentials) once and removes it from the environment, valid
 * or not, so the running server keeps no copy of credentials that bypass row-level security.
 */
export function takeMigrationUrl(source: Record<string, string | undefined> = process.env): string {
  const result = postgresUrl.safeParse(source.DATABASE_MIGRATION_URL);
  delete source.DATABASE_MIGRATION_URL;
  if (!result.success) {
    const message = result.error.issues[0]?.message ?? "invalid";
    throw new Error(`Invalid environment configuration:\n  - DATABASE_MIGRATION_URL: ${message}`);
  }
  return result.data;
}
