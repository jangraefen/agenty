export async function register() {
  // Validate configuration when the Node.js server starts, but not while `next build` runs.
  if (
    process.env.NEXT_RUNTIME === "nodejs" &&
    process.env.NEXT_PHASE !== "phase-production-build"
  ) {
    try {
      const { getEnv } = await import("./server/env");
      getEnv();
    } catch (error) {
      // Next.js only logs a failing register() and keeps serving, so abort explicitly.
      console.error(error instanceof Error ? error.message : "Invalid environment configuration");
      process.exit(1);
    }
  }
}
