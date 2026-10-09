import { defineConfig, devices } from "@playwright/test";

// Dedicated ports and no server reuse: E2E always tests the standalone production build, never a
// dev server that happens to be running.
const port = 3100;
/** Same build, but its identity provider is unreachable (tests/e2e/auth.spec.ts). */
const unreachableIdpPort = 3101;

function standaloneServer(serverPort: number, env: Record<string, string> = {}) {
  return {
    command: "node .next/standalone/server.js",
    url: `http://127.0.0.1:${serverPort}/api/health`,
    reuseExistingServer: false,
    timeout: 60 * 1000,
    env: {
      PORT: String(serverPort),
      HOSTNAME: "127.0.0.1",
      NODE_ENV: "production",
      DATABASE_URL: process.env.DATABASE_URL ?? "",
      DATABASE_MIGRATION_URL: process.env.DATABASE_MIGRATION_URL ?? "",
      BETTER_AUTH_URL: `http://127.0.0.1:${serverPort}`,
      ...env,
    },
  };
}

export default defineConfig({
  testDir: "tests/e2e",
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    standaloneServer(port),
    // Port 1 refuses connections, so the discovery pre-check fails at once.
    standaloneServer(unreachableIdpPort, { OIDC_DISCOVERY_URL: "http://localhost:1/x" }),
  ],
});
