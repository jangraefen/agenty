import { defineConfig, devices } from "@playwright/test";

// A dedicated port and no server reuse: E2E always tests the standalone production build,
// never a dev server that happens to be running.
const port = 3100;

export default defineConfig({
  testDir: "tests/e2e",
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "node .next/standalone/server.js",
    url: `http://127.0.0.1:${port}/api/health`,
    reuseExistingServer: false,
    timeout: 60_000,
    env: {
      PORT: String(port),
      HOSTNAME: "127.0.0.1",
      NODE_ENV: "production",
      DATABASE_URL: process.env.DATABASE_URL ?? "",
      DATABASE_MIGRATION_URL: process.env.DATABASE_MIGRATION_URL ?? "",
      BETTER_AUTH_SECRET: process.env.BETTER_AUTH_SECRET ?? "",
      BETTER_AUTH_URL: `http://127.0.0.1:${port}`,
      BETTER_AUTH_TRUSTED_ORIGINS: "http://localhost:8080",
    },
  },
});
