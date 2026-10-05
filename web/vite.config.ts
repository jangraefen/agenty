import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

export default defineConfig({
	plugins: [react()],
	test: {
		environment: "jsdom",
		include: ["src/**/*.test.{ts,tsx}"],
		setupFiles: ["src/test-setup.ts"],
		// Undo vi.stubGlobal and restore vi.spyOn/vi.fn mocks after each test, so no test cleans up by hand.
		unstubGlobals: true,
		restoreMocks: true,
	},
})
