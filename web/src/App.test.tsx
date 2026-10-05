import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { App } from "./App"

describe("App", () => {
	it("renders the product name as the main heading", () => {
		render(<App />)

		const heading = screen.getByRole("heading", { level: 1 })
		expect(heading.textContent).toBe("Agenty")
	})
})
