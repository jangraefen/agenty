// Vitest setup for every test file (vite.config.ts `test.setupFiles`).
import { cleanup } from "@testing-library/react"
import { afterEach } from "vitest"

// Unmount everything Testing Library rendered, so no test sees another test's DOM.
afterEach(cleanup)
