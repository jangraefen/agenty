export async function register() {
  // Runs once when the Node.js server starts, but not while `next build` runs.
  if (
    process.env.NEXT_RUNTIME === "nodejs" &&
    process.env.NEXT_PHASE !== "phase-production-build"
  ) {
    try {
      const { getEnv, takeMigrationUrl } = await import("./server/env");
      getEnv();
      // Migrate before serving. The owner credentials are removed from process.env first and the
      // owner connection is closed afterwards, so the running server only holds agenty_app's.
      const migrationUrl = takeMigrationUrl();
      const path = await import("node:path");
      const { migrateDatabase } = await import("./server/db/migrate");
      await migrateDatabase(migrationUrl, path.join(process.cwd(), "src/server/db/migrations"));
    } catch (error) {
      // Next.js only logs a failing register() and keeps serving, so abort explicitly.
      console.error(error instanceof Error ? error.message : "Server startup failed");
      process.exit(1);
    }
  }
}
