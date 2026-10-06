/// <reference types="vitest/config" />
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv, type Plugin } from "vite";
import { defaultApiUrl } from "./src/api-url";
import { contentSecurityPolicy } from "./src/csp";

// Puts the Content-Security-Policy into the built index.html, right after the
// charset and before any script, as a policy covers only what follows it. The
// development server goes without: its hot reloading runs inline scripts.
function csp(apiUrl: string): Plugin {
  const charset = '<meta charset="UTF-8" />';
  return {
    name: "agenty-csp",
    apply: "build",
    transformIndexHtml: {
      order: "pre",
      handler: (html) => {
        if (!html.includes(charset)) {
          throw new Error(`index.html must contain ${charset}`);
        }
        const policy = contentSecurityPolicy(apiUrl).replaceAll("'", "&#39;");
        return html.replace(
          charset,
          `${charset}\n    <meta http-equiv="Content-Security-Policy" content="${policy}" />`,
        );
      },
    },
  };
}

export default defineConfig(({ mode }) => ({
  plugins: [
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    csp(loadEnv(mode, import.meta.dirname, "VITE_").VITE_AGENTY_API_URL ?? defaultApiUrl),
  ],
  resolve: {
    alias: { "@": new URL("./src", import.meta.url).pathname },
  },
  server: {
    // The origin agenty's operator config lists under cors.origins.
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    restoreMocks: true,
  },
}));
