import { act, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

// main.tsx renders on import, so each test imports a fresh copy of the module.
async function importMain() {
	await act(async () => {
		await import("./main")
	})
}

describe("main", () => {
	beforeEach(() => {
		vi.resetModules()
	})

	afterEach(() => {
		document.body.innerHTML = ""
	})

	it("renders the app into the #root element", async () => {
		const root = document.createElement("div")
		root.id = "root"
		document.body.append(root)

		await importMain()

		expect(root.contains(screen.getByRole("heading", { level: 1, name: "Agenty" }))).toBe(true)
	})

	it("fails when the #root element is missing", async () => {
		await expect(importMain()).rejects.toThrow("root element #root not found")
	})
})
