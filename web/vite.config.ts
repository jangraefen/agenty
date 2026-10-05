import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

export default defineConfig({
	plugins: [react()],
	test: {
		environment: "jsdom",
		include: ["src/**/*.test.{ts,tsx}"],
		// Coverage gate (ARCHITECTURE §15.3): `pnpm run test` (and so `task test:unit:web`) runs with
		// --coverage and fails below the threshold. Only the tests themselves are excluded; the generated
		// src/api/schema.gen.ts holds types only, so it has no statements to count.
		coverage: {
			provider: "v8",
			include: ["src/**/*.{ts,tsx}"],
			exclude: ["src/**/*.test.{ts,tsx}"],
			reportsDirectory: "coverage",
			reporter: ["text", "html"],
			// Per file, the closest Vitest equivalent of the per-package Go threshold (and stricter).
			thresholds: { statements: 90, perFile: true },
		},
	},
})
