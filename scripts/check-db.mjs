// Fails fast with a hint when the database behind DATABASE_URL is unreachable.
import postgres from "postgres";

const url = process.env.DATABASE_URL;
if (!url) {
  console.error("DATABASE_URL is not set. Run `task setup` to create .env from .env.example.");
  process.exit(1);
}

const sql = postgres(url, { max: 1, connect_timeout: 5, onnotice: () => {} });
try {
  await sql`select 1`;
} catch (error) {
  console.error(
    `Database not reachable (${error instanceof Error ? error.message : "unknown error"}).`,
    "Start it with `task db:up`.",
  );
  process.exitCode = 1;
} finally {
  await sql.end();
}
