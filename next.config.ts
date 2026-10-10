import type { NextConfig } from "next";

// Next's instant-navigation testing API (used by `instant()` in the E2E tests) is built in only when
// EXPOSE_TESTING_API=1 at `next build` time: `task build` sets it, the Docker image never does.
const exposeTestingApi = process.env.EXPOSE_TESTING_API === "1";

const nextConfig: NextConfig = {
  output: "standalone",
  cacheComponents: true,
  partialPrefetching: true,
  experimental: {
    exposeTestingApiInProductionBuild: exposeTestingApi,
  },
  // Read at runtime by src/server/policy/evaluate.ts; file tracing cannot see the path.
  outputFileTracingIncludes: {
    "/**": ["./build/policy/policy.wasm"],
  },
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
