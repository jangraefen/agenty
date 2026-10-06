import { defineConfig } from "@playwright/test";

// The smoke test runs the built frontend against a real agenty server, on
// ports of their own. It needs the agenty binary at the repository root
// (task build) and a PostgreSQL database in AGENTY_E2E_DATABASE_URL.
export const api = "http://127.0.0.1:18080";
const app = "http://127.0.0.1:4173";

// The smoke user's token: local to the test, never a secret.
export const token = "agenty-e2e-smoke-token-not-a-secret-0123";

const database = process.env.AGENTY_E2E_DATABASE_URL;
if (database === undefined || database === "") {
  throw new Error("set AGENTY_E2E_DATABASE_URL to a PostgreSQL database for the smoke test");
}

export default defineConfig({
  testDir: "e2e",
  forbidOnly: process.env.CI !== undefined,
  reporter: process.env.CI === undefined ? "list" : [["list"], ["html", { open: "never" }]],
  timeout: 60_000,
  use: {
    baseURL: app,
    // The installed Google Chrome, which GitHub's runners have too, so the
    // test downloads no browser.
    channel: "chrome",
    trace: "retain-on-failure",
  },
  webServer: [
    {
      command: "../agenty serve --config e2e/agenty.yaml --addr 127.0.0.1:18080",
      url: `${api}/v1/me`,
      env: {
        AGENTY_E2E_DATABASE_URL: database,
        AGENTY_E2E_TOKEN: token,
        AGENTY_E2E_ANTHROPIC_API_KEY: "e2e-dummy-key-the-model-refuses",
      },
      stdout: "pipe",
    },
    {
      command: "pnpm build && pnpm exec vite preview --host 127.0.0.1 --port 4173 --strictPort",
      url: app,
      env: { VITE_AGENTY_API_URL: api },
    },
  ],
});
